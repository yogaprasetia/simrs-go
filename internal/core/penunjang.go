package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ================================================================ RADIOLOGI
// Alur Khanza: dokter membuat permintaan_radiologi (noorder PRyyyyMMddNNNN) ->
// radiografer melakukan pemeriksaan (periksa_radiologi, tarif disalin dari master) ->
// dokter radiologi menulis expertise (hasil_radiologi) + gambar (gambar_radiologi).

func NoOrderRad(ctx context.Context, tx *sql.Tx) (string, error) {
	return seqDaily(ctx, tx, "permintaan_radiologi", "noorder", "PR"+Now().Format("20060102"), 4)
}

func PermintaanRadiologi(ctx context.Context, tx *sql.Tx, noRawat, kdDokter, diagnosa, info string, jenis []string) (string, error) {
	r, err := mustOpen(ctx, tx, noRawat)
	if err != nil {
		return "", err
	}
	if len(jenis) == 0 {
		return "", errors.New("pilih minimal satu pemeriksaan radiologi")
	}
	no, err := NoOrderRad(ctx, tx)
	if err != nil {
		return "", err
	}
	st := "ralan"
	if r.StatusLanjut == "Ranap" {
		st = "ranap"
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO permintaan_radiologi (noorder,no_rawat,tgl_permintaan,jam_permintaan,dokter_perujuk,status,informasi_tambahan,diagnosa_klinis) VALUES (?,?,?,?,?,?,?,?)",
		no, noRawat, Today(), NowTime(), kdDokter, st, info, diagnosa); err != nil {
		return "", err
	}
	for _, j := range jenis {
		if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM jns_perawatan_radiologi WHERE kd_jenis_prw=? AND status='1'", j); n == 0 {
			return "", fmt.Errorf("pemeriksaan radiologi %s tidak ditemukan", j)
		}
		if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO permintaan_pemeriksaan_radiologi VALUES (?,?,'Belum')", no, j); err != nil {
			return "", err
		}
	}
	return no, nil
}

// Paparan = parameter eksposi per pemeriksaan (opsional, untuk catatan dosis).
type Paparan struct {
	Proyeksi, KV, MAS, FFD, BSF, Inak, JmlPenyinaran, Dosis string
}

type orderRad struct {
	NoRawat, Perujuk, Status string
	TglSampel, TglHasil      sql.NullString
}

func getOrderRad(ctx context.Context, tx *sql.Tx, noorder string) (*orderRad, error) {
	o := &orderRad{}
	err := tx.QueryRowContext(ctx, "SELECT no_rawat,dokter_perujuk,status,tgl_sampel,tgl_hasil FROM permintaan_radiologi WHERE noorder=? FOR UPDATE", noorder).
		Scan(&o.NoRawat, &o.Perujuk, &o.Status, &o.TglSampel, &o.TglHasil)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("order radiologi tidak ditemukan")
	}
	return o, err
}

// PeriksaRadiologi: radiografer menyimpan pelaksanaan pemeriksaan -> tarif masuk tagihan.
func PeriksaRadiologi(ctx context.Context, tx *sql.Tx, noorder, nip, kdDokterRad string, pap map[string]Paparan) error {
	o, err := getOrderRad(ctx, tx, noorder)
	if err != nil {
		return err
	}
	if o.TglSampel.Valid {
		return errors.New("pemeriksaan order ini sudah dilakukan")
	}
	if _, err := mustOpen(ctx, tx, o.NoRawat); err != nil {
		return err
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM petugas WHERE nip=? AND status='1'", nip); n == 0 {
		return errors.New("petugas radiografer tidak valid")
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM dokter WHERE kd_dokter=? AND status='1'", kdDokterRad); n == 0 {
		return errors.New("dokter radiologi tidak valid")
	}
	tgl, jam := Today(), NowTime()
	// jam unik per no_rawat (bila 2 order di detik yang sama)
	for i := 0; i < 5; i++ {
		if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM app_radiologi_order a JOIN permintaan_radiologi p ON p.noorder=a.noorder WHERE p.no_rawat=? AND a.tgl_periksa=? AND a.jam=?", o.NoRawat, tgl, jam); n == 0 {
			break
		}
		t, _ := time.Parse("15:04:05", jam)
		jam = t.Add(time.Second).Format("15:04:05")
	}
	st := "Ralan"
	if o.Status == "ranap" {
		st = "Ranap"
	}
	rows, err := tx.QueryContext(ctx, `SELECT j.kd_jenis_prw,j.bagian_rs,j.bhp,j.tarif_perujuk,j.tarif_tindakan_dokter,j.tarif_tindakan_petugas,j.kso,j.menejemen,j.total_byr
		FROM permintaan_pemeriksaan_radiologi p JOIN jns_perawatan_radiologi j ON j.kd_jenis_prw=p.kd_jenis_prw WHERE p.noorder=?`, noorder)
	if err != nil {
		return err
	}
	type tarif struct {
		kd                                  string
		rs, bhp, ruj, dr, ptg, kso, mn, tot float64
	}
	var list []tarif
	for rows.Next() {
		var t tarif
		if err := rows.Scan(&t.kd, &t.rs, &t.bhp, &t.ruj, &t.dr, &t.ptg, &t.kso, &t.mn, &t.tot); err != nil {
			rows.Close()
			return err
		}
		list = append(list, t)
	}
	rows.Close()
	for _, t := range list {
		p := pap[t.kd]
		if _, err := tx.ExecContext(ctx, `INSERT INTO periksa_radiologi (no_rawat,nip,kd_jenis_prw,tgl_periksa,jam,dokter_perujuk,bagian_rs,bhp,tarif_perujuk,tarif_tindakan_dokter,tarif_tindakan_petugas,kso,menejemen,biaya,kd_dokter,status,proyeksi,kV,mAS,FFD,BSF,inak,jml_penyinaran,dosis)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			o.NoRawat, nip, t.kd, tgl, jam, o.Perujuk, t.rs, t.bhp, t.ruj, t.dr, t.ptg, t.kso, t.mn, t.tot, kdDokterRad, st,
			p.Proyeksi, p.KV, p.MAS, p.FFD, p.BSF, p.Inak, p.JmlPenyinaran, p.Dosis); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO app_radiologi_order VALUES (?,?,?)", noorder, tgl, jam); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE permintaan_radiologi SET tgl_sampel=?, jam_sampel=? WHERE noorder=?", tgl, jam, noorder)
	return err
}

// SimpanExpertise: dokter radiologi menulis bacaan; order dianggap selesai.
func SimpanExpertise(ctx context.Context, tx *sql.Tx, noorder, hasil string) error {
	o, err := getOrderRad(ctx, tx, noorder)
	if err != nil {
		return err
	}
	if !o.TglSampel.Valid {
		return errors.New("pemeriksaan belum dilakukan radiografer")
	}
	if strings.TrimSpace(hasil) == "" {
		return errors.New("hasil bacaan (expertise) wajib diisi")
	}
	var tgl, jam string
	if err := tx.QueryRowContext(ctx, "SELECT tgl_periksa,jam FROM app_radiologi_order WHERE noorder=?", noorder).Scan(&tgl, &jam); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO hasil_radiologi VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE hasil=VALUES(hasil)", o.NoRawat, tgl, jam, hasil); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE permintaan_radiologi SET tgl_hasil=IFNULL(tgl_hasil,?), jam_hasil=IFNULL(jam_hasil,?) WHERE noorder=?", Today(), NowTime(), noorder)
	return err
}

// TambahGambar mencatat lokasi file gambar untuk order yang sudah diperiksa.
func TambahGambar(ctx context.Context, tx *sql.Tx, noorder, lokasi string) error {
	o, err := getOrderRad(ctx, tx, noorder)
	if err != nil {
		return err
	}
	var tgl, jam string
	if err := tx.QueryRowContext(ctx, "SELECT tgl_periksa,jam FROM app_radiologi_order WHERE noorder=?", noorder).Scan(&tgl, &jam); err != nil {
		return errors.New("pemeriksaan belum dilakukan radiografer")
	}
	_, err = tx.ExecContext(ctx, "INSERT IGNORE INTO gambar_radiologi VALUES (?,?,?,?)", o.NoRawat, tgl, jam, lokasi)
	return err
}

// ================================================================ OPERASI / VK

var KategoriOperasi = []string{"Kecil", "Sedang", "Besar", "Khusus", "Elektive", "Emergency", "Sedang Cito", "Besar Cito"}
var JenisAnestesi = []string{"GA", "SAB", "Epidural", "Blok", "Lokal", "Sedasi", "-"}

func cekPaket(ctx context.Context, tx *sql.Tx, kode string) error {
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM paket_operasi WHERE kode_paket=? AND status='1'", kode); n == 0 {
		return errors.New("paket operasi tidak ditemukan")
	}
	return nil
}

// BookingOperasi menjadwalkan operasi; menolak bentrok ruang OK pada rentang jam yang sama.
func BookingOperasi(ctx context.Context, tx *sql.Tx, noRawat, kodePaket, tanggal, jamMulai, jamSelesai, kdDokter, ruang string) error {
	if _, err := mustOpen(ctx, tx, noRawat); err != nil {
		return err
	}
	if err := cekPaket(ctx, tx, kodePaket); err != nil {
		return err
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM ruang_ok WHERE kd_ruang_ok=?", ruang); n == 0 {
		return errors.New("ruang operasi tidak valid")
	}
	if _, err := time.Parse("2006-01-02", tanggal); err != nil {
		return errors.New("tanggal operasi tidak valid")
	}
	jamMulai, jamSelesai = normJam(jamMulai), normJam(jamSelesai)
	m, e1 := time.Parse("15:04:05", jamMulai)
	s, e2 := time.Parse("15:04:05", jamSelesai)
	if e1 != nil || e2 != nil || !s.After(m) {
		return errors.New("jam mulai/selesai tidak valid (selesai harus setelah mulai)")
	}
	if tanggal < Today() {
		return errors.New("tidak bisa menjadwalkan operasi di tanggal yang sudah lewat")
	}
	var bentrok string
	err := tx.QueryRowContext(ctx, `SELECT CONCAT(b.no_rawat,' ',TIME_FORMAT(b.jam_mulai,'%H:%i'),'-',TIME_FORMAT(b.jam_selesai,'%H:%i')) FROM booking_operasi b
		WHERE b.kd_ruang_ok=? AND b.tanggal=? AND b.status<>'Selesai' AND b.jam_mulai < ? AND IFNULL(b.jam_selesai, ADDTIME(b.jam_mulai,'01:00:00')) > ? LIMIT 1 FOR UPDATE`,
		ruang, tanggal, jamSelesai, jamMulai).Scan(&bentrok)
	if err == nil {
		return fmt.Errorf("ruang %s sudah dipakai pada jam tersebut (%s)", ruang, bentrok)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO booking_operasi VALUES (?,?,?,?,?,'Menunggu',?,?)", noRawat, kodePaket, tanggal, jamMulai, jamSelesai, kdDokter, ruang)
	return err
}

func normJam(j string) string {
	j = strings.TrimSpace(j)
	if len(j) == 5 {
		return j + ":00"
	}
	return j
}

// StatusBooking: Menunggu -> Proses Operasi -> Selesai, atau batal (hapus) selama Menunggu.
func StatusBooking(ctx context.Context, tx *sql.Tx, noRawat, kodePaket, tanggal, jamMulai, aksi string) error {
	var st string
	err := tx.QueryRowContext(ctx, "SELECT status FROM booking_operasi WHERE no_rawat=? AND kode_paket=? AND tanggal=? AND jam_mulai=? FOR UPDATE", noRawat, kodePaket, tanggal, normJam(jamMulai)).Scan(&st)
	if err != nil {
		return errors.New("jadwal operasi tidak ditemukan")
	}
	where := " WHERE no_rawat=? AND kode_paket=? AND tanggal=? AND jam_mulai=?"
	args := []any{noRawat, kodePaket, tanggal, normJam(jamMulai)}
	switch aksi {
	case "mulai":
		if st != "Menunggu" {
			return errors.New("operasi sudah dimulai/selesai")
		}
		_, err = tx.ExecContext(ctx, "UPDATE booking_operasi SET status='Proses Operasi'"+where, args...)
	case "batal":
		if st != "Menunggu" {
			return errors.New("hanya jadwal berstatus Menunggu yang bisa dibatalkan")
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM booking_operasi"+where, args...)
	default:
		return errors.New("aksi tidak dikenal")
	}
	return err
}

// Tim operasi: field orang -> kolom biaya + kolom tarif paket. Dokter vs petugas sesuai Khanza.
type peranOK struct {
	Field, Biaya, Paket string
	Dokter              bool
	Label               string
}

var PeranOperasi = []peranOK{
	{"operator1", "biayaoperator1", "operator1", true, "Operator 1"},
	{"operator2", "biayaoperator2", "operator2", true, "Operator 2"},
	{"operator3", "biayaoperator3", "operator3", true, "Operator 3"},
	{"asisten_operator1", "biayaasisten_operator1", "asisten_operator1", false, "Asisten operator 1"},
	{"asisten_operator2", "biayaasisten_operator2", "asisten_operator2", false, "Asisten operator 2"},
	{"instrumen", "biayainstrumen", "instrumen", false, "Instrumen"},
	{"dokter_anestesi", "biayadokter_anestesi", "dokter_anestesi", true, "Dokter anestesi"},
	{"asisten_anestesi", "biayaasisten_anestesi", "asisten_anestesi", false, "Asisten anestesi"},
	{"dokter_anak", "biayadokter_anak", "dokter_anak", true, "Dokter anak"},
	{"perawaat_resusitas", "biayaperawaat_resusitas", "perawaat_resusitas", false, "Perawat resusitasi"},
	{"bidan", "biayabidan", "bidan", false, "Bidan"},
	{"bidan2", "biayabidan2", "bidan2", false, "Bidan 2"},
	{"perawat_luar", "biayaperawat_luar", "perawat_luar", false, "Perawat luar"},
	{"omloop", "biaya_omloop", "omloop", false, "Omloop"},
}

// komponen non-personal: selalu ditagihkan dari paket.
var komponenOK = [][2]string{{"biayaalat", "alat"}, {"biayasewaok", "sewa_ok"}, {"akomodasi", "akomodasi"}, {"bagian_rs", "bagian_rs"}, {"biayasarpras", "sarpras"}}

type OperasiInput struct {
	NoRawat, KodePaket, TglOperasi, JenisAnestesi, Kategori string            // TglOperasi "2006-01-02 15:04"
	Tim                                                     map[string]string // field -> kd_dokter / nip
}

// SimpanOperasi mencatat tindakan operasi + tim; jasa tiap peran ditagihkan hanya bila petugasnya diisi,
// komponen RS (alat, sewa OK, akomodasi, bagian RS, sarpras) selalu ditagihkan.
func SimpanOperasi(ctx context.Context, tx *sql.Tx, in OperasiInput) (float64, error) {
	r, err := mustOpen(ctx, tx, in.NoRawat)
	if err != nil {
		return 0, err
	}
	if err := cekPaket(ctx, tx, in.KodePaket); err != nil {
		return 0, err
	}
	t, err := time.ParseInLocation("2006-01-02 15:04", strings.Replace(strings.TrimSpace(in.TglOperasi), "T", " ", 1), Loc)
	if err != nil {
		return 0, errors.New("tanggal/jam operasi tidak valid")
	}
	if t.After(Now().Add(time.Hour)) {
		return 0, errors.New("tanggal operasi tidak boleh di masa depan")
	}
	var kategoriPaket string
	tx.QueryRowContext(ctx, "SELECT kategori FROM paket_operasi WHERE kode_paket=?", in.KodePaket).Scan(&kategoriPaket)
	if strings.TrimSpace(in.Tim["operator1"]) == "" || in.Tim["operator1"] == "-" {
		if kategoriPaket == "Kebidanan" && in.Tim["bidan"] != "" && in.Tim["bidan"] != "-" {
			// persalinan normal oleh bidan boleh tanpa operator dokter
		} else {
			return 0, errors.New("operator 1 wajib diisi")
		}
	}
	cols := []string{"no_rawat", "tgl_operasi", "jenis_anasthesi", "kategori", "kode_paket", "status"}
	st := "Ralan"
	if r.StatusLanjut == "Ranap" {
		st = "Ranap"
	}
	if in.JenisAnestesi == "" {
		in.JenisAnestesi = "-"
	}
	if in.Kategori == "" {
		in.Kategori = "-"
	}
	args := []any{in.NoRawat, t.Format("2006-01-02 15:04:05"), in.JenisAnestesi, in.Kategori, in.KodePaket, st}
	var total float64
	for _, p := range PeranOperasi {
		who := strings.TrimSpace(in.Tim[p.Field])
		if who == "" {
			who = "-"
		}
		var tarif float64
		if err := tx.QueryRowContext(ctx, "SELECT "+p.Paket+" FROM paket_operasi WHERE kode_paket=?", in.KodePaket).Scan(&tarif); err != nil {
			return 0, err
		}
		if who != "-" {
			q := "SELECT COUNT(*) FROM petugas WHERE nip=? AND status='1'"
			if p.Dokter {
				q = "SELECT COUNT(*) FROM dokter WHERE kd_dokter=? AND status='1'"
			}
			if n, _ := one[int](ctx, tx, q, who); n == 0 {
				return 0, fmt.Errorf("%s tidak valid", p.Label)
			}
		} else {
			tarif = 0
		}
		cols = append(cols, p.Field, p.Biaya)
		args = append(args, who, tarif)
		total += tarif
	}
	for _, k := range komponenOK {
		var v float64
		if err := tx.QueryRowContext(ctx, "SELECT "+k[1]+" FROM paket_operasi WHERE kode_paket=?", in.KodePaket).Scan(&v); err != nil {
			return 0, err
		}
		cols = append(cols, k[0])
		args = append(args, v)
		total += v
	}
	q := "INSERT INTO operasi (" + strings.Join(cols, ",") + ") VALUES (" + strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",") + ")"
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			return 0, errors.New("tindakan operasi yang sama pada waktu itu sudah tercatat")
		}
		return 0, err
	}
	// booking terkait -> Selesai
	_, err = tx.ExecContext(ctx, "UPDATE booking_operasi SET status='Selesai', jam_selesai=IF(status='Proses Operasi', ?, jam_selesai) WHERE no_rawat=? AND kode_paket=? AND status<>'Selesai'", NowTime(), in.NoRawat, in.KodePaket)
	return total, err
}

func HapusOperasi(ctx context.Context, tx *sql.Tx, noRawat, tgl, kodePaket string) error {
	if _, err := mustOpen(ctx, tx, noRawat); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM operasi WHERE no_rawat=? AND tgl_operasi=? AND kode_paket=?", noRawat, tgl, kodePaket)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("data operasi tidak ditemukan")
	}
	return nil
}

type LaporanOperasi struct {
	NoRawat, Tanggal, DxPre, DxPost, Jaringan, Selesai, PermintaanPA, NomorImplan, Laporan string
}

func SimpanLaporanOperasi(ctx context.Context, tx *sql.Tx, l LaporanOperasi) error {
	if _, err := mustOpen(ctx, tx, l.NoRawat); err != nil {
		return err
	}
	parse := func(s string) (time.Time, error) {
		return time.ParseInLocation("2006-01-02 15:04", strings.Replace(strings.TrimSpace(s), "T", " ", 1), Loc)
	}
	mulai, e1 := parse(l.Tanggal)
	selesai, e2 := parse(l.Selesai)
	if e1 != nil || e2 != nil {
		return errors.New("waktu mulai/selesai operasi tidak valid")
	}
	if selesai.Before(mulai) {
		return errors.New("waktu selesai sebelum waktu mulai")
	}
	if strings.TrimSpace(l.DxPre) == "" || strings.TrimSpace(l.DxPost) == "" || strings.TrimSpace(l.Laporan) == "" {
		return errors.New("diagnosa pra/pasca bedah dan uraian laporan wajib diisi")
	}
	if l.PermintaanPA != "Ya" {
		l.PermintaanPA = "Tidak"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO laporan_operasi VALUES (?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE diagnosa_preop=VALUES(diagnosa_preop), diagnosa_postop=VALUES(diagnosa_postop),
		jaringan_dieksekusi=VALUES(jaringan_dieksekusi), selesaioperasi=VALUES(selesaioperasi), permintaan_pa=VALUES(permintaan_pa), nomor_implan=VALUES(nomor_implan), laporan_operasi=VALUES(laporan_operasi)`,
		l.NoRawat, mulai.Format("2006-01-02 15:04:05"), l.DxPre, l.DxPost, l.Jaringan, selesai.Format("2006-01-02 15:04:05"), l.PermintaanPA, l.NomorImplan, l.Laporan)
	return err
}

// BeriObatOperasi: BHP/obat kamar operasi (harga dari master obatbhp_ok).
func BeriObatOperasi(ctx context.Context, tx *sql.Tx, noRawat, kdObat string, jumlah float64) error {
	if _, err := mustOpen(ctx, tx, noRawat); err != nil {
		return err
	}
	if jumlah <= 0 {
		return errors.New("jumlah harus > 0")
	}
	harga, err := one[float64](ctx, tx, "SELECT hargasatuan FROM obatbhp_ok WHERE kd_obat=?", kdObat)
	if err != nil {
		return errors.New("obat/BHP OK tidak ditemukan")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO beri_obat_operasi VALUES (?,?,?,?,?)", noRawat, Now().Format("2006-01-02 15:04:05"), kdObat, harga, jumlah)
	return err
}

func HapusObatOperasi(ctx context.Context, tx *sql.Tx, noRawat, tanggal, kdObat string) error {
	if _, err := mustOpen(ctx, tx, noRawat); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM beri_obat_operasi WHERE no_rawat=? AND tanggal=? AND kd_obat=? LIMIT 1", noRawat, tanggal, kdObat)
	return err
}

// biayaOperasiExpr: total semua komponen biaya pada satu baris operasi (alias tabel o).
const biayaOperasiExpr = `(o.biayaoperator1+o.biayaoperator2+o.biayaoperator3+o.biayaasisten_operator1+o.biayaasisten_operator2+IFNULL(o.biayaasisten_operator3,0)+IFNULL(o.biayainstrumen,0)+o.biayadokter_anak+o.biayaperawaat_resusitas+o.biayadokter_anestesi+o.biayaasisten_anestesi+IFNULL(o.biayaasisten_anestesi2,0)+o.biayabidan+IFNULL(o.biayabidan2,0)+IFNULL(o.biayabidan3,0)+o.biayaperawat_luar+o.biayaalat+o.biayasewaok+IFNULL(o.akomodasi,0)+o.bagian_rs+IFNULL(o.biaya_omloop,0)+IFNULL(o.biaya_omloop2,0)+IFNULL(o.biaya_omloop3,0)+IFNULL(o.biaya_omloop4,0)+IFNULL(o.biaya_omloop5,0)+IFNULL(o.biayasarpras,0)+IFNULL(o.biaya_dokter_pjanak,0)+IFNULL(o.biaya_dokter_umum,0))`

// BiayaOperasiExpr dipakai paket web untuk menampilkan total per baris.
func BiayaOperasiExpr() string { return biayaOperasiExpr }
