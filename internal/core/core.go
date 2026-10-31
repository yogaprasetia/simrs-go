// Package core berisi proses bisnis SIMRS (mengikuti alur SIMKES Khanza):
// penomoran, registrasi, pemeriksaan (ERM), resep & farmasi, laboratorium,
// rawat inap (kamar), dan billing kasir. Semua fungsi bekerja di atas *sql.Tx.
package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

var Loc = mustLoc("Asia/Jakarta")

func mustLoc(n string) *time.Location {
	l, err := time.LoadLocation(n)
	if err != nil {
		return time.FixedZone("WIB", 7*3600)
	}
	return l
}

// Now bisa diganti saat pengujian.
var Now = func() time.Time { return time.Now().In(Loc) }

func Today() string   { return Now().Format("2006-01-02") }
func NowTime() string { return Now().Format("15:04:05") }

var ErrPaid = errors.New("transaksi sudah dibayar (dikunci kasir), tidak bisa diubah")

type Q interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

// WithTx menjalankan fn dalam transaksi.
func WithTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func one[T any](ctx context.Context, q Q, query string, args ...any) (T, error) {
	var v T
	err := q.QueryRowContext(ctx, query, args...).Scan(&v)
	return v, err
}

// ---------------------------------------------------------------- penomoran

// NoRM menghasilkan no_rkm_medis berikutnya (6 digit) memakai set_no_rkm_medis seperti Khanza.
func NoRM(ctx context.Context, tx *sql.Tx) (string, error) {
	last, err := one[string](ctx, tx, "SELECT no_rkm_medis FROM set_no_rkm_medis LIMIT 1 FOR UPDATE")
	if errors.Is(err, sql.ErrNoRows) {
		last, err = one[string](ctx, tx, "SELECT IFNULL(MAX(CAST(no_rkm_medis AS UNSIGNED)),0) FROM pasien")
		if err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO set_no_rkm_medis VALUES (?)", last); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	n, _ := strconv.Atoi(strings.TrimLeft(last, "0"))
	for {
		n++
		rm := fmt.Sprintf("%06d", n)
		exists, err := one[int](ctx, tx, "SELECT COUNT(*) FROM pasien WHERE no_rkm_medis=?", rm)
		if err != nil {
			return "", err
		}
		if exists == 0 {
			_, err = tx.ExecContext(ctx, "UPDATE set_no_rkm_medis SET no_rkm_medis=?", rm)
			return rm, err
		}
	}
}

// NoRawat: yyyy/MM/dd/NNNNNN, urut per tanggal registrasi.
func NoRawat(ctx context.Context, tx *sql.Tx, tgl string) (string, error) {
	prefix := strings.ReplaceAll(tgl, "-", "/") + "/"
	max, err := one[int](ctx, tx, "SELECT IFNULL(MAX(CAST(RIGHT(no_rawat,6) AS UNSIGNED)),0) FROM reg_periksa WHERE tgl_registrasi=? FOR UPDATE", tgl)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%06d", prefix, max+1), nil
}

// NoReg: nomor antrean 3 digit per dokter & poli per hari (default Khanza).
func NoReg(ctx context.Context, tx *sql.Tx, tgl, kdDokter, kdPoli string) (string, error) {
	max, err := one[int](ctx, tx, "SELECT IFNULL(MAX(CAST(no_reg AS UNSIGNED)),0) FROM reg_periksa WHERE tgl_registrasi=? AND kd_dokter=? AND kd_poli=? FOR UPDATE", tgl, kdDokter, kdPoli)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%03d", max+1), nil
}

func seqDaily(ctx context.Context, tx *sql.Tx, table, col, prefix string, width int) (string, error) {
	max, err := one[int](ctx, tx, fmt.Sprintf("SELECT IFNULL(MAX(CAST(SUBSTRING(%s,%d) AS UNSIGNED)),0) FROM %s WHERE %s LIKE ? FOR UPDATE", col, len(prefix)+1, table, col), prefix+"%")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%0*d", prefix, width, max+1), nil
}

// NoResep: yyyyMMddNNNN
func NoResep(ctx context.Context, tx *sql.Tx) (string, error) {
	return seqDaily(ctx, tx, "resep_obat", "no_resep", Now().Format("20060102"), 4)
}

// NoOrderLab: PKyyyyMMddNNNN
func NoOrderLab(ctx context.Context, tx *sql.Tx) (string, error) {
	return seqDaily(ctx, tx, "permintaan_lab", "noorder", "PK"+Now().Format("20060102"), 4)
}

// ---------------------------------------------------------------- umur

// Umur menghitung umur saat tanggal ref: (angka, satuan Th/Bl/Hr) dan teks "x Th y Bl z Hr".
func Umur(tglLahir, ref string) (int, string, string) {
	lahir, err1 := time.ParseInLocation("2006-01-02", tglLahir, Loc)
	now, err2 := time.ParseInLocation("2006-01-02", ref, Loc)
	if err1 != nil || err2 != nil || now.Before(lahir) {
		return 0, "Hr", "0 Hr"
	}
	y := now.Year() - lahir.Year()
	m := int(now.Month()) - int(lahir.Month())
	d := now.Day() - lahir.Day()
	if d < 0 {
		m--
		d += time.Date(now.Year(), now.Month(), 0, 0, 0, 0, 0, Loc).Day()
	}
	if m < 0 {
		y--
		m += 12
	}
	txt := fmt.Sprintf("%d Th %d Bl %d Hr", y, m, d)
	switch {
	case y > 0:
		return y, "Th", txt
	case m > 0:
		return m, "Bl", txt
	default:
		return d, "Hr", txt
	}
}

// ---------------------------------------------------------------- pasien

type Pasien struct {
	NoRM, Nama, NoKTP, JK, TmpLahir, TglLahir, NmIbu, Alamat, GolDarah, Pekerjaan, SttsNikah, Agama, NoTlp, Pnd, Keluarga, NamaKeluarga, KdPJ, NoPeserta string
}

func (p *Pasien) Validate() error {
	p.Nama = strings.ToUpper(strings.TrimSpace(p.Nama))
	if p.Nama == "" {
		return errors.New("nama pasien wajib diisi")
	}
	if p.JK != "L" && p.JK != "P" {
		return errors.New("jenis kelamin wajib L atau P")
	}
	if _, err := time.Parse("2006-01-02", p.TglLahir); err != nil {
		return errors.New("tanggal lahir tidak valid")
	}
	if p.NoKTP != "" && (len(p.NoKTP) != 16 || strings.Trim(p.NoKTP, "0123456789") != "") {
		return errors.New("NIK harus 16 digit angka")
	}
	if p.KdPJ == "" {
		p.KdPJ = "A09"
	}
	if p.GolDarah == "" {
		p.GolDarah = "-"
	}
	if p.Pnd == "" {
		p.Pnd = "-"
	}
	if p.SttsNikah == "" {
		p.SttsNikah = "BELUM MENIKAH"
	}
	if p.Keluarga == "" {
		p.Keluarga = "DIRI SENDIRI"
	}
	return nil
}

// SimpanPasien membuat pasien baru (NoRM kosong) atau memperbarui.
func SimpanPasien(ctx context.Context, tx *sql.Tx, p *Pasien) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.NoKTP != "" {
		dup, err := one[string](ctx, tx, "SELECT no_rkm_medis FROM pasien WHERE no_ktp=? AND no_rkm_medis<>? LIMIT 1", p.NoKTP, p.NoRM)
		if err == nil {
			return fmt.Errorf("NIK sudah terdaftar dengan No. RM %s", dup)
		}
	}
	_, _, umur := Umur(p.TglLahir, Today())
	if p.NoRM == "" {
		rm, err := NoRM(ctx, tx)
		if err != nil {
			return err
		}
		p.NoRM = rm
		_, err = tx.ExecContext(ctx, `INSERT INTO pasien (no_rkm_medis,nm_pasien,no_ktp,jk,tmp_lahir,tgl_lahir,nm_ibu,alamat,gol_darah,pekerjaan,stts_nikah,agama,tgl_daftar,no_tlp,umur,pnd,keluarga,namakeluarga,kd_pj,no_peserta)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.NoRM, p.Nama, p.NoKTP, p.JK, p.TmpLahir, p.TglLahir, p.NmIbu, p.Alamat, p.GolDarah, p.Pekerjaan, p.SttsNikah, p.Agama, Today(), p.NoTlp, umur, p.Pnd, p.Keluarga, p.NamaKeluarga, p.KdPJ, p.NoPeserta)
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE pasien SET nm_pasien=?,no_ktp=?,jk=?,tmp_lahir=?,tgl_lahir=?,nm_ibu=?,alamat=?,gol_darah=?,pekerjaan=?,stts_nikah=?,agama=?,no_tlp=?,umur=?,pnd=?,keluarga=?,namakeluarga=?,kd_pj=?,no_peserta=? WHERE no_rkm_medis=?`,
		p.Nama, p.NoKTP, p.JK, p.TmpLahir, p.TglLahir, p.NmIbu, p.Alamat, p.GolDarah, p.Pekerjaan, p.SttsNikah, p.Agama, p.NoTlp, umur, p.Pnd, p.Keluarga, p.NamaKeluarga, p.KdPJ, p.NoPeserta, p.NoRM)
	return err
}

// ---------------------------------------------------------------- registrasi

type RegInput struct {
	NoRM, KdPoli, KdDokter, KdPJ, PJawab, AlmtPJ, HubunganPJ string
	Tgl                                                      string // kosong = hari ini
}

type RegResult struct {
	NoRawat, NoReg, SttsDaftar, StatusPoli string
	Biaya                                  float64
}

// Registrasi mendaftarkan kunjungan (reg_periksa) mengikuti aturan Khanza:
// no_rawat harian, no_reg per dokter/poli, status daftar Baru/Lama, status poli Baru/Lama,
// biaya registrasi sesuai poliklinik.registrasi / registrasilama, umur saat daftar.
func Registrasi(ctx context.Context, tx *sql.Tx, in RegInput) (RegResult, error) {
	var r RegResult
	if in.Tgl == "" {
		in.Tgl = Today()
	}
	var tglLahir, alamat, nmKel, kel string
	err := tx.QueryRowContext(ctx, "SELECT tgl_lahir,alamat,namakeluarga,keluarga FROM pasien WHERE no_rkm_medis=?", in.NoRM).Scan(&tglLahir, &alamat, &nmKel, &kel)
	if errors.Is(err, sql.ErrNoRows) {
		return r, errors.New("pasien tidak ditemukan")
	} else if err != nil {
		return r, err
	}
	var biayaBaru, biayaLama float64
	if err := tx.QueryRowContext(ctx, "SELECT registrasi,registrasilama FROM poliklinik WHERE kd_poli=? AND status='1'", in.KdPoli).Scan(&biayaBaru, &biayaLama); err != nil {
		return r, errors.New("poliklinik tidak valid")
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM dokter WHERE kd_dokter=? AND status='1'", in.KdDokter); n == 0 {
		return r, errors.New("dokter tidak valid")
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM penjab WHERE kd_pj=?", in.KdPJ); n == 0 {
		return r, errors.New("cara bayar tidak valid")
	}
	// satu pasien tidak boleh terdaftar 2x di poli yang sama pada hari yang sama (kecuali batal)
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM reg_periksa WHERE no_rkm_medis=? AND kd_poli=? AND tgl_registrasi=? AND stts<>'Batal'", in.NoRM, in.KdPoli, in.Tgl); n > 0 {
		return r, errors.New("pasien sudah terdaftar di poli ini hari ini")
	}
	// masih dirawat inap?
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM kamar_inap k JOIN reg_periksa r ON r.no_rawat=k.no_rawat WHERE r.no_rkm_medis=? AND k.stts_pulang='-'", in.NoRM); n > 0 {
		return r, errors.New("pasien masih dirawat inap")
	}
	prev, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM reg_periksa WHERE no_rkm_medis=? AND stts<>'Batal'", in.NoRM)
	prevPoli, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM reg_periksa WHERE no_rkm_medis=? AND kd_poli=? AND stts<>'Batal'", in.NoRM, in.KdPoli)
	r.SttsDaftar, r.StatusPoli, r.Biaya = "Baru", "Baru", biayaBaru
	if prev > 0 {
		r.SttsDaftar = "Lama"
	}
	if prevPoli > 0 {
		r.StatusPoli, r.Biaya = "Lama", biayaLama
	}
	if r.NoRawat, err = NoRawat(ctx, tx, in.Tgl); err != nil {
		return r, err
	}
	if r.NoReg, err = NoReg(ctx, tx, in.Tgl, in.KdDokter, in.KdPoli); err != nil {
		return r, err
	}
	umur, sat, _ := Umur(tglLahir, in.Tgl)
	if in.PJawab == "" {
		in.PJawab = nmKel
		if in.HubunganPJ == "" {
			in.HubunganPJ = kel
		}
	}
	if in.AlmtPJ == "" {
		in.AlmtPJ = alamat
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO reg_periksa (no_reg,no_rawat,tgl_registrasi,jam_reg,kd_dokter,no_rkm_medis,kd_poli,p_jawab,almt_pj,hubunganpj,biaya_reg,stts,stts_daftar,status_lanjut,kd_pj,umurdaftar,sttsumur,status_bayar,status_poli)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,'Belum',?,'Ralan',?,?,?,'Belum Bayar',?)`,
		r.NoReg, r.NoRawat, in.Tgl, NowTime(), in.KdDokter, in.NoRM, in.KdPoli, in.PJawab, in.AlmtPJ, in.HubunganPJ, r.Biaya, r.SttsDaftar, in.KdPJ, umur, sat, r.StatusPoli)
	return r, err
}

// Kunjungan ringkas
type Reg struct {
	NoRawat, NoReg, Tgl, Jam, NoRM, Nama, JK, TglLahir, KdPoli, NmPoli, KdDokter, NmDokter, KdPJ, PngJawab, Stts, StatusLanjut, StatusBayar, NoPeserta, Alamat, Umur string
	Biaya                                                                                                                                                            float64
}

func GetReg(ctx context.Context, q Q, noRawat string) (*Reg, error) {
	r := &Reg{}
	err := q.QueryRowContext(ctx, `SELECT r.no_rawat,r.no_reg,r.tgl_registrasi,r.jam_reg,r.no_rkm_medis,p.nm_pasien,p.jk,p.tgl_lahir,r.kd_poli,pl.nm_poli,r.kd_dokter,d.nm_dokter,r.kd_pj,pj.png_jawab,r.stts,r.status_lanjut,r.status_bayar,p.no_peserta,p.alamat,CONCAT(r.umurdaftar,' ',r.sttsumur),r.biaya_reg
		FROM reg_periksa r JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis JOIN poliklinik pl ON pl.kd_poli=r.kd_poli JOIN dokter d ON d.kd_dokter=r.kd_dokter JOIN penjab pj ON pj.kd_pj=r.kd_pj WHERE r.no_rawat=?`, noRawat).
		Scan(&r.NoRawat, &r.NoReg, &r.Tgl, &r.Jam, &r.NoRM, &r.Nama, &r.JK, &r.TglLahir, &r.KdPoli, &r.NmPoli, &r.KdDokter, &r.NmDokter, &r.KdPJ, &r.PngJawab, &r.Stts, &r.StatusLanjut, &r.StatusBayar, &r.NoPeserta, &r.Alamat, &r.Umur, &r.Biaya)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("no. rawat tidak ditemukan")
	}
	return r, err
}

func mustOpen(ctx context.Context, tx *sql.Tx, noRawat string) (*Reg, error) {
	r, err := GetReg(ctx, tx, noRawat)
	if err != nil {
		return nil, err
	}
	if r.StatusBayar == "Sudah Bayar" {
		return nil, ErrPaid
	}
	if r.Stts == "Batal" {
		return nil, errors.New("kunjungan sudah dibatalkan")
	}
	return r, nil
}

// BatalRegistrasi: hanya bila belum ada tindakan/pemeriksaan/obat.
func BatalRegistrasi(ctx context.Context, tx *sql.Tx, noRawat string) error {
	if _, err := mustOpen(ctx, tx, noRawat); err != nil {
		return err
	}
	for _, t := range []string{"pemeriksaan_ralan", "rawat_jl_dr", "detail_pemberian_obat", "periksa_lab", "kamar_inap", "resep_obat", "diagnosa_pasien", "permintaan_lab", "permintaan_radiologi", "periksa_radiologi", "operasi", "booking_operasi", "beri_obat_operasi"} {
		if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM "+t+" WHERE no_rawat=?", noRawat); n > 0 {
			return fmt.Errorf("tidak bisa dibatalkan: sudah ada data di %s", t)
		}
	}
	_, err := tx.ExecContext(ctx, "UPDATE reg_periksa SET stts='Batal', biaya_reg=0 WHERE no_rawat=?", noRawat)
	return err
}

// ---------------------------------------------------------------- pemeriksaan (ERM / SOAP)

type SOAP struct {
	NoRawat, Suhu, Tensi, Nadi, Respirasi, Tinggi, Berat, SpO2, GCS, Kesadaran, Keluhan, Pemeriksaan, Alergi, LingkarPerut, RTL, Penilaian, Instruksi, Evaluasi, NIP string
	Ranap                                                                                                                                                            bool
}

func SimpanSOAP(ctx context.Context, tx *sql.Tx, s SOAP) (string, error) {
	r, err := mustOpen(ctx, tx, s.NoRawat)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(s.Keluhan+s.Pemeriksaan+s.Penilaian+s.RTL) == "" {
		return "", errors.New("isi minimal salah satu S/O/A/P")
	}
	table := "pemeriksaan_ralan"
	if s.Ranap {
		table = "pemeriksaan_ranap"
	}
	if s.Kesadaran == "" {
		s.Kesadaran = "Compos Mentis"
	}
	jam := NowTime()
	// hindari duplikat kunci bila disimpan 2x di detik yang sama
	for i := 0; i < 5; i++ {
		if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM "+table+" WHERE no_rawat=? AND tgl_perawatan=? AND jam_rawat=?", s.NoRawat, Today(), jam); n == 0 {
			break
		}
		t, _ := time.Parse("15:04:05", jam)
		jam = t.Add(time.Second).Format("15:04:05")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO "+table+` (no_rawat,tgl_perawatan,jam_rawat,suhu_tubuh,tensi,nadi,respirasi,tinggi,berat,spo2,gcs,kesadaran,keluhan,pemeriksaan,alergi,lingkar_perut,rtl,penilaian,instruksi,evaluasi,nip)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.NoRawat, Today(), jam, s.Suhu, s.Tensi, s.Nadi, s.Respirasi, s.Tinggi, s.Berat, s.SpO2, s.GCS, s.Kesadaran, s.Keluhan, s.Pemeriksaan, s.Alergi, s.LingkarPerut, s.RTL, s.Penilaian, s.Instruksi, s.Evaluasi, s.NIP)
	if err != nil {
		return "", err
	}
	if !s.Ranap && r.Stts == "Belum" {
		_, err = tx.ExecContext(ctx, "UPDATE reg_periksa SET stts='Sudah' WHERE no_rawat=?", s.NoRawat)
	}
	return jam, err
}

func TambahDiagnosa(ctx context.Context, tx *sql.Tx, noRawat, kd string, ranap bool) error {
	r, err := mustOpen(ctx, tx, noRawat)
	if err != nil {
		return err
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM penyakit WHERE kd_penyakit=?", kd); n == 0 {
		return errors.New("kode ICD-10 tidak ditemukan")
	}
	st := "Ralan"
	if ranap {
		st = "Ranap"
	}
	prio, _ := one[int](ctx, tx, "SELECT IFNULL(MAX(prioritas),0)+1 FROM diagnosa_pasien WHERE no_rawat=? AND status=?", noRawat, st)
	lama, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM diagnosa_pasien d JOIN reg_periksa r ON r.no_rawat=d.no_rawat WHERE r.no_rkm_medis=? AND d.kd_penyakit=? AND d.no_rawat<>?", r.NoRM, kd, noRawat)
	sp := "Baru"
	if lama > 0 {
		sp = "Lama"
	}
	_, err = tx.ExecContext(ctx, "INSERT IGNORE INTO diagnosa_pasien VALUES (?,?,?,?,?)", noRawat, kd, st, prio, sp)
	return err
}

func TambahProsedur(ctx context.Context, tx *sql.Tx, noRawat, kode string, ranap bool) error {
	if _, err := mustOpen(ctx, tx, noRawat); err != nil {
		return err
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM icd9 WHERE kode=?", kode); n == 0 {
		return errors.New("kode ICD-9 tidak ditemukan")
	}
	st := "Ralan"
	if ranap {
		st = "Ranap"
	}
	prio, _ := one[int](ctx, tx, "SELECT IFNULL(MAX(prioritas),0)+1 FROM prosedur_pasien WHERE no_rawat=? AND status=?", noRawat, st)
	_, err := tx.ExecContext(ctx, "INSERT IGNORE INTO prosedur_pasien VALUES (?,?,?,?)", noRawat, kode, st, prio)
	return err
}

func HapusDiagnosa(ctx context.Context, tx *sql.Tx, noRawat, kd, status string) error {
	if _, err := mustOpen(ctx, tx, noRawat); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM diagnosa_pasien WHERE no_rawat=? AND kd_penyakit=? AND status=?", noRawat, kd, status)
	return err
}

// TambahTindakan menyimpan rawat_jl_dr / rawat_inap_dr dengan tarif dari master.
func TambahTindakan(ctx context.Context, tx *sql.Tx, noRawat, kdJenis, kdDokter string, ranap bool) error {
	if _, err := mustOpen(ctx, tx, noRawat); err != nil {
		return err
	}
	jam := NowTime()
	if ranap {
		tarif, err := one[float64](ctx, tx, "SELECT total_byrdr FROM jns_perawatan_inap WHERE kd_jenis_prw=? AND status='1'", kdJenis)
		if err != nil {
			return errors.New("tindakan rawat inap tidak ditemukan")
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO rawat_inap_dr VALUES (?,?,?,?,?,?) ON DUPLICATE KEY UPDATE biaya_rawat=biaya_rawat+VALUES(biaya_rawat)", noRawat, kdJenis, kdDokter, Today(), jam, tarif)
		return err
	}
	var mat, dr, tot float64
	if err := tx.QueryRowContext(ctx, "SELECT material,tarif_tindakandr,total_byrdr FROM jns_perawatan WHERE kd_jenis_prw=? AND status='1'", kdJenis).Scan(&mat, &dr, &tot); err != nil {
		return errors.New("tindakan rawat jalan tidak ditemukan")
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO rawat_jl_dr VALUES (?,?,?,?,?,?,?,?,'Belum') ON DUPLICATE KEY UPDATE biaya_rawat=biaya_rawat+VALUES(biaya_rawat)", noRawat, kdJenis, kdDokter, Today(), jam, mat, dr, tot)
	return err
}

func HapusTindakan(ctx context.Context, tx *sql.Tx, noRawat, kdJenis, tgl, jam string, ranap bool) error {
	if _, err := mustOpen(ctx, tx, noRawat); err != nil {
		return err
	}
	t := "rawat_jl_dr"
	if ranap {
		t = "rawat_inap_dr"
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM "+t+" WHERE no_rawat=? AND kd_jenis_prw=? AND tgl_perawatan=? AND jam_rawat=?", noRawat, kdJenis, tgl, jam)
	return err
}

// ---------------------------------------------------------------- resep & farmasi

const Depo = "AP" // depo farmasi (bangsal Apotek)

type ItemResep struct {
	KodeBrng string
	Jml      float64
	Aturan   string
}

func BuatResep(ctx context.Context, tx *sql.Tx, noRawat, kdDokter string, items []ItemResep) (string, error) {
	r, err := mustOpen(ctx, tx, noRawat)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", errors.New("resep kosong")
	}
	no, err := NoResep(ctx, tx)
	if err != nil {
		return "", err
	}
	st := "ralan"
	if r.StatusLanjut == "Ranap" {
		st = "ranap"
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO resep_obat (no_resep,no_rawat,kd_dokter,tgl_peresepan,jam_peresepan,status) VALUES (?,?,?,?,?,?)", no, noRawat, kdDokter, Today(), NowTime(), st); err != nil {
		return "", err
	}
	for _, it := range items {
		if it.Jml <= 0 {
			return "", fmt.Errorf("jumlah %s harus > 0", it.KodeBrng)
		}
		if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM databarang WHERE kode_brng=? AND status='1'", it.KodeBrng); n == 0 {
			return "", fmt.Errorf("obat %s tidak ditemukan", it.KodeBrng)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO resep_dokter VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE jml=jml+VALUES(jml)", no, it.KodeBrng, it.Jml, it.Aturan); err != nil {
			return "", err
		}
	}
	return no, nil
}

// ValidasiResep (farmasi): resep -> detail_pemberian_obat, potong stok depo, riwayat_barang_medis.
func ValidasiResep(ctx context.Context, tx *sql.Tx, noResep, petugas string) error {
	var noRawat, status string
	var tglP sql.NullString
	err := tx.QueryRowContext(ctx, "SELECT no_rawat,status,tgl_perawatan FROM resep_obat WHERE no_resep=? FOR UPDATE", noResep).Scan(&noRawat, &status, &tglP)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("resep tidak ditemukan")
	} else if err != nil {
		return err
	}
	if tglP.Valid {
		return errors.New("resep sudah divalidasi")
	}
	if _, err := mustOpen(ctx, tx, noRawat); err != nil {
		return err
	}
	tgl, jam := Today(), NowTime()
	rows, err := tx.QueryContext(ctx, `SELECT d.kode_brng,d.jml,d.aturan_pakai,b.nama_brng,b.h_beli,IF(?='ranap',b.ranap,b.ralan) FROM resep_dokter d JOIN databarang b ON b.kode_brng=d.kode_brng WHERE d.no_resep=?`, status, noResep)
	if err != nil {
		return err
	}
	type it struct {
		kode, aturan, nama string
		jml, hb, harga     float64
	}
	var list []it
	for rows.Next() {
		var x it
		if err := rows.Scan(&x.kode, &x.jml, &x.aturan, &x.nama, &x.hb, &x.harga); err != nil {
			rows.Close()
			return err
		}
		list = append(list, x)
	}
	rows.Close()
	st := "Ralan"
	if status == "ranap" {
		st = "Ranap"
	}
	for _, x := range list {
		stok, err := one[float64](ctx, tx, "SELECT IFNULL(SUM(stok),0) FROM gudangbarang WHERE kode_brng=? AND kd_bangsal=? FOR UPDATE", x.kode, Depo)
		if err != nil {
			return err
		}
		if stok < x.jml {
			return fmt.Errorf("stok %s tidak cukup (tersedia %g, diminta %g)", x.nama, stok, x.jml)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE gudangbarang SET stok=stok-? WHERE kode_brng=? AND kd_bangsal=? AND no_batch='' AND no_faktur=''", x.jml, x.kode, Depo); err != nil {
			return err
		}
		total := math.Round(x.harga * x.jml)
		if _, err = tx.ExecContext(ctx, `INSERT INTO detail_pemberian_obat (tgl_perawatan,jam,no_rawat,kode_brng,h_beli,biaya_obat,jml,embalase,tuslah,total,status,kd_bangsal) VALUES (?,?,?,?,?,?,?,0,0,?,?,?)
			ON DUPLICATE KEY UPDATE jml=jml+VALUES(jml), total=total+VALUES(total)`, tgl, jam, noRawat, x.kode, x.hb, x.harga, x.jml, total, st, Depo); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO aturan_pakai VALUES (?,?,?,?,?) ON DUPLICATE KEY UPDATE aturan=VALUES(aturan)", tgl, jam, noRawat, x.kode, x.aturan); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO riwayat_barang_medis (kode_brng,stok_awal,masuk,keluar,stok_akhir,posisi,tanggal,jam,petugas,kd_bangsal,keterangan) VALUES (?,?,0,?,?,'Pemberian Obat',?,?,?,?,?)",
			x.kode, stok, x.jml, stok-x.jml, tgl, jam, petugas, Depo, "Resep "+noResep+" "+noRawat); err != nil {
			return err
		}
	}
	var hpp float64
	for _, x := range list {
		hpp += x.hb * x.jml
	}
	if hpp > 0 {
		ak, err := mustAkun(ctx, tx, "HPP_Obat", "Persediaan_Obat")
		if err != nil {
			return err
		}
		if _, err := PostJurnal(ctx, tx, noResep, "U", "HPP obat resep "+noResep+" "+noRawat, []Baris{{KdRek: ak["HPP_Obat"], Debet: hpp}, {KdRek: ak["Persediaan_Obat"], Kredit: hpp}}); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE resep_obat SET tgl_perawatan=?, jam=? WHERE no_resep=?", tgl, jam, noResep)
	return err
}

func SerahkanResep(ctx context.Context, tx *sql.Tx, noResep string) error {
	res, err := tx.ExecContext(ctx, "UPDATE resep_obat SET tgl_penyerahan=?, jam_penyerahan=? WHERE no_resep=? AND tgl_perawatan IS NOT NULL AND tgl_penyerahan IS NULL", Today(), NowTime(), noResep)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("resep belum divalidasi atau sudah diserahkan")
	}
	return nil
}

func HapusResep(ctx context.Context, tx *sql.Tx, noResep string) error {
	var noRawat string
	var tglP sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT no_rawat,tgl_perawatan FROM resep_obat WHERE no_resep=?", noResep).Scan(&noRawat, &tglP); err != nil {
		return errors.New("resep tidak ditemukan")
	}
	if tglP.Valid {
		return errors.New("resep sudah divalidasi farmasi, tidak bisa dihapus")
	}
	if _, err := mustOpen(ctx, tx, noRawat); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM resep_dokter WHERE no_resep=?", noResep); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM resep_obat WHERE no_resep=?", noResep)
	return err
}

// ---------------------------------------------------------------- laboratorium

func PermintaanLab(ctx context.Context, tx *sql.Tx, noRawat, kdDokter, diagnosa string, jenis []string) (string, error) {
	r, err := mustOpen(ctx, tx, noRawat)
	if err != nil {
		return "", err
	}
	if len(jenis) == 0 {
		return "", errors.New("pilih minimal satu pemeriksaan")
	}
	no, err := NoOrderLab(ctx, tx)
	if err != nil {
		return "", err
	}
	st := "ralan"
	if r.StatusLanjut == "Ranap" {
		st = "ranap"
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO permintaan_lab (noorder,no_rawat,tgl_permintaan,jam_permintaan,dokter_perujuk,status,diagnosa_klinis) VALUES (?,?,?,?,?,?,?)", no, noRawat, Today(), NowTime(), kdDokter, st, diagnosa); err != nil {
		return "", err
	}
	for _, j := range jenis {
		if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM jns_perawatan_lab WHERE kd_jenis_prw=?", j); n == 0 {
			return "", fmt.Errorf("pemeriksaan %s tidak ditemukan", j)
		}
		if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO permintaan_pemeriksaan_lab VALUES (?,?,'Belum')", no, j); err != nil {
			return "", err
		}
	}
	return no, nil
}

// Flag membandingkan nilai numerik dengan rujukan "a-b", "<x", ">x" -> "H"/"L"/"".
func Flag(nilai, rujukan string) string {
	v, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(nilai), ",", "."), 64)
	if err != nil {
		return ""
	}
	rj := strings.ReplaceAll(strings.TrimSpace(rujukan), ",", ".")
	switch {
	case strings.HasPrefix(rj, "<"):
		if lim, e := strconv.ParseFloat(strings.TrimSpace(rj[1:]), 64); e == nil && v >= lim {
			return "H"
		}
	case strings.HasPrefix(rj, ">"):
		if lim, e := strconv.ParseFloat(strings.TrimSpace(rj[1:]), 64); e == nil && v <= lim {
			return "L"
		}
	case strings.Contains(rj, "-"):
		p := strings.SplitN(rj, "-", 2)
		lo, e1 := strconv.ParseFloat(strings.TrimSpace(p[0]), 64)
		hi, e2 := strconv.ParseFloat(strings.TrimSpace(p[1]), 64)
		if e1 == nil && e2 == nil {
			if v < lo {
				return "L"
			}
			if v > hi {
				return "H"
			}
		}
	}
	return ""
}

// SimpanHasilLab: hasil per id_template untuk satu order.
func SimpanHasilLab(ctx context.Context, tx *sql.Tx, noorder, nip, kdDokterPJ string, nilai map[int]string) error {
	var noRawat, perujuk, status string
	var tglHasil sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT no_rawat,dokter_perujuk,status,tgl_hasil FROM permintaan_lab WHERE noorder=? FOR UPDATE", noorder).Scan(&noRawat, &perujuk, &status, &tglHasil); err != nil {
		return errors.New("order lab tidak ditemukan")
	}
	if tglHasil.Valid {
		return errors.New("hasil sudah disimpan")
	}
	r, err := mustOpen(ctx, tx, noRawat)
	if err != nil {
		return err
	}
	tgl, jam := Today(), NowTime()
	st := "Ralan"
	if status == "ranap" {
		st = "Ranap"
	}
	rows, err := tx.QueryContext(ctx, `SELECT p.kd_jenis_prw,j.total_byr FROM permintaan_pemeriksaan_lab p JOIN jns_perawatan_lab j ON j.kd_jenis_prw=p.kd_jenis_prw WHERE p.noorder=?`, noorder)
	if err != nil {
		return err
	}
	type jb struct {
		kd    string
		biaya float64
	}
	var jl []jb
	for rows.Next() {
		var x jb
		rows.Scan(&x.kd, &x.biaya)
		jl = append(jl, x)
	}
	rows.Close()
	for _, j := range jl {
		if _, err := tx.ExecContext(ctx, "INSERT INTO periksa_lab VALUES (?,?,?,?,?,?,?,?,?,'PK')", noRawat, nip, j.kd, tgl, jam, perujuk, j.biaya, kdDokterPJ, st); err != nil {
			return err
		}
		tr, err := tx.QueryContext(ctx, "SELECT id_template,IF(?='P',nilai_rujukan_pd,nilai_rujukan_ld) FROM template_laboratorium WHERE kd_jenis_prw=? ORDER BY urut", r.JK, j.kd)
		if err != nil {
			return err
		}
		type t struct {
			id  int
			ruj string
		}
		var ts []t
		for tr.Next() {
			var x t
			tr.Scan(&x.id, &x.ruj)
			ts = append(ts, x)
		}
		tr.Close()
		for _, x := range ts {
			v := strings.TrimSpace(nilai[x.id])
			if v == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO detail_periksa_lab VALUES (?,?,?,?,?,?,?,?)", noRawat, j.kd, tgl, jam, x.id, v, x.ruj, Flag(v, x.ruj)); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE permintaan_lab SET tgl_sampel=IFNULL(tgl_sampel,?), jam_sampel=IFNULL(jam_sampel,?), tgl_hasil=?, jam_hasil=? WHERE noorder=?", tgl, jam, tgl, jam, noorder)
	return err
}

// ---------------------------------------------------------------- rawat inap

// LamaInap: selisih hari, minimal 1 (aturan Khanza: masuk & keluar hari sama = 1 hari).
func LamaInap(tglMasuk, tglKeluar string) int {
	a, e1 := time.ParseInLocation("2006-01-02", tglMasuk, Loc)
	b, e2 := time.ParseInLocation("2006-01-02", tglKeluar, Loc)
	if e1 != nil || e2 != nil {
		return 1
	}
	d := int(math.Round(b.Sub(a).Hours() / 24))
	if d < 1 {
		d = 1
	}
	return d
}

func kamarKosong(ctx context.Context, tx *sql.Tx, kdKamar string) (float64, error) {
	var status string
	var trf float64
	err := tx.QueryRowContext(ctx, "SELECT status,trf_kamar FROM kamar WHERE kd_kamar=? AND statusdata='1' FOR UPDATE", kdKamar).Scan(&status, &trf)
	if err != nil {
		return 0, errors.New("kamar tidak ditemukan")
	}
	if status != "KOSONG" {
		return 0, fmt.Errorf("kamar %s berstatus %s", kdKamar, status)
	}
	return trf, nil
}

// MasukKamar: pasien (dari rawat jalan/IGD) masuk rawat inap.
func MasukKamar(ctx context.Context, tx *sql.Tx, noRawat, kdKamar, diagnosaAwal, kdDPJP string) error {
	r, err := mustOpen(ctx, tx, noRawat)
	if err != nil {
		return err
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM kamar_inap WHERE no_rawat=? AND stts_pulang='-'", noRawat); n > 0 {
		return errors.New("pasien sudah berada di kamar")
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM kamar_inap WHERE no_rawat=? AND stts_pulang NOT IN ('-','Pindah Kamar')", noRawat); n > 0 {
		return errors.New("pasien sudah pulang dari rawat inap pada no. rawat ini")
	}
	trf, err := kamarKosong(ctx, tx, kdKamar)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO kamar_inap (no_rawat,kd_kamar,trf_kamar,diagnosa_awal,tgl_masuk,jam_masuk,stts_pulang) VALUES (?,?,?,?,?,?,'-')", noRawat, kdKamar, trf, diagnosaAwal, Today(), NowTime()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE kamar SET status='ISI' WHERE kd_kamar=?", kdKamar); err != nil {
		return err
	}
	if kdDPJP == "" {
		kdDPJP = r.KdDokter
	}
	if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO dpjp_ranap VALUES (?,?)", noRawat, kdDPJP); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE reg_periksa SET status_lanjut='Ranap', stts=IF(stts='Belum','Sudah',stts) WHERE no_rawat=?", noRawat)
	return err
}

func tutupKamar(ctx context.Context, tx *sql.Tx, noRawat, stts, diagAkhir string) (string, error) {
	var kd, tglM, jamM string
	var trf float64
	err := tx.QueryRowContext(ctx, "SELECT kd_kamar,tgl_masuk,jam_masuk,trf_kamar FROM kamar_inap WHERE no_rawat=? AND stts_pulang='-' FOR UPDATE", noRawat).Scan(&kd, &tglM, &jamM, &trf)
	if err != nil {
		return "", errors.New("pasien tidak sedang dirawat di kamar")
	}
	tgl := Today()
	lama := LamaInap(tglM, tgl)
	_, err = tx.ExecContext(ctx, "UPDATE kamar_inap SET tgl_keluar=?, jam_keluar=?, lama=?, ttl_biaya=?, stts_pulang=?, diagnosa_akhir=? WHERE no_rawat=? AND tgl_masuk=? AND jam_masuk=?",
		tgl, NowTime(), lama, float64(lama)*trf, stts, diagAkhir, noRawat, tglM, jamM)
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, "UPDATE kamar SET status='KOSONG' WHERE kd_kamar=?", kd)
	return kd, err
}

func PindahKamar(ctx context.Context, tx *sql.Tx, noRawat, kdKamarBaru string) error {
	if _, err := mustOpen(ctx, tx, noRawat); err != nil {
		return err
	}
	trf, err := kamarKosong(ctx, tx, kdKamarBaru)
	if err != nil {
		return err
	}
	var diag string
	_ = tx.QueryRowContext(ctx, "SELECT diagnosa_awal FROM kamar_inap WHERE no_rawat=? AND stts_pulang='-'", noRawat).Scan(&diag)
	if _, err := tutupKamar(ctx, tx, noRawat, "Pindah Kamar", ""); err != nil {
		return err
	}
	jam := NowTime()
	// primary key (no_rawat,tgl_masuk,jam_masuk): pastikan jam unik
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM kamar_inap WHERE no_rawat=? AND tgl_masuk=? AND jam_masuk=?", noRawat, Today(), jam); n > 0 {
		t, _ := time.Parse("15:04:05", jam)
		jam = t.Add(time.Second).Format("15:04:05")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO kamar_inap (no_rawat,kd_kamar,trf_kamar,diagnosa_awal,tgl_masuk,jam_masuk,stts_pulang) VALUES (?,?,?,?,?,?,'-')", noRawat, kdKamarBaru, trf, diag, Today(), jam); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE kamar SET status='ISI' WHERE kd_kamar=?", kdKamarBaru)
	return err
}

func PulangRanap(ctx context.Context, tx *sql.Tx, noRawat, stts, diagAkhir string) error {
	if _, err := mustOpen(ctx, tx, noRawat); err != nil {
		return err
	}
	switch stts {
	case "Sehat", "Rujuk", "APS", "Meninggal", "Sembuh", "Membaik", "Pulang Paksa", "Atas Persetujuan Dokter", "Atas Permintaan Sendiri", "Lain-lain":
	default:
		return errors.New("status pulang tidak valid")
	}
	if _, err := tutupKamar(ctx, tx, noRawat, stts, diagAkhir); err != nil {
		return err
	}
	if stts == "Meninggal" {
		_, err := tx.ExecContext(ctx, "UPDATE reg_periksa SET stts='Meninggal' WHERE no_rawat=?", noRawat)
		return err
	}
	return nil
}

// ---------------------------------------------------------------- billing / kasir

type BillItem struct {
	Status, Nama string
	Biaya, Jml   float64
	Total        float64
}

type Bill struct {
	Reg   *Reg
	Items []BillItem
	Total float64
	// Ranap true bila billing rawat inap (termasuk kamar)
	Ranap      bool
	SudahBayar bool
}

// HitungBilling menyusun tagihan dari semua transaksi no_rawat (seperti DlgBilingRalan/Ranap Khanza).
func HitungBilling(ctx context.Context, q Q, noRawat string) (*Bill, error) {
	r, err := GetReg(ctx, q, noRawat)
	if err != nil {
		return nil, err
	}
	b := &Bill{Reg: r, Ranap: r.StatusLanjut == "Ranap", SudahBayar: r.StatusBayar == "Sudah Bayar"}
	add := func(st, nm string, biaya, jml float64) {
		t := math.Round(biaya * jml)
		b.Items = append(b.Items, BillItem{st, nm, biaya, jml, t})
		b.Total += t
	}
	if r.Biaya > 0 {
		add("Registrasi", "Registrasi "+r.NmPoli, r.Biaya, 1)
	}
	type src struct {
		status, query string
	}
	srcs := []src{
		{"Ralan Dokter", `SELECT j.nm_perawatan, t.biaya_rawat, COUNT(*) FROM rawat_jl_dr t JOIN jns_perawatan j ON j.kd_jenis_prw=t.kd_jenis_prw WHERE t.no_rawat=? GROUP BY j.nm_perawatan,t.biaya_rawat`},
		{"Ranap Dokter", `SELECT j.nm_perawatan, t.biaya_rawat, COUNT(*) FROM rawat_inap_dr t JOIN jns_perawatan_inap j ON j.kd_jenis_prw=t.kd_jenis_prw WHERE t.no_rawat=? GROUP BY j.nm_perawatan,t.biaya_rawat`},
		{"Laborat", `SELECT j.nm_perawatan, p.biaya, COUNT(*) FROM periksa_lab p JOIN jns_perawatan_lab j ON j.kd_jenis_prw=p.kd_jenis_prw WHERE p.no_rawat=? GROUP BY j.nm_perawatan,p.biaya`},
		{"Radiologi", `SELECT j.nm_perawatan, p.biaya, COUNT(*) FROM periksa_radiologi p JOIN jns_perawatan_radiologi j ON j.kd_jenis_prw=p.kd_jenis_prw WHERE p.no_rawat=? GROUP BY j.nm_perawatan,p.biaya`},
		{"Operasi", `SELECT CONCAT(p.nm_perawatan,' (',DATE_FORMAT(o.tgl_operasi,'%d-%m-%Y %H:%i'),')'), ` + biayaOperasiExpr + `, 1 FROM operasi o JOIN paket_operasi p ON p.kode_paket=o.kode_paket WHERE o.no_rawat=? ORDER BY o.tgl_operasi`},
		{"Operasi", `SELECT CONCAT('BHP OK: ',k.nm_obat), b.hargasatuan, SUM(b.jumlah) FROM beri_obat_operasi b JOIN obatbhp_ok k ON k.kd_obat=b.kd_obat WHERE b.no_rawat=? GROUP BY k.nm_obat,b.hargasatuan`},
		{"Obat", `SELECT b.nama_brng, d.biaya_obat, SUM(d.jml) FROM detail_pemberian_obat d JOIN databarang b ON b.kode_brng=d.kode_brng WHERE d.no_rawat=? GROUP BY b.nama_brng,d.biaya_obat`},
		{"Tambahan", `SELECT nama_biaya, besar_biaya, 1 FROM tambahan_biaya WHERE no_rawat=?`},
	}
	for _, s := range srcs {
		rows, err := q.QueryContext(ctx, s.query, noRawat)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var nm string
			var biaya, jml float64
			if err := rows.Scan(&nm, &biaya, &jml); err != nil {
				rows.Close()
				return nil, err
			}
			add(s.status, nm, biaya, jml)
		}
		rows.Close()
	}
	// kamar: baris tertutup pakai lama tersimpan, baris terbuka dihitung sampai hari ini
	rows, err := q.QueryContext(ctx, `SELECT k.kd_kamar, b.nm_bangsal, k.trf_kamar, k.tgl_masuk, IFNULL(k.tgl_keluar,''), k.lama, k.stts_pulang FROM kamar_inap k JOIN kamar m ON m.kd_kamar=k.kd_kamar JOIN bangsal b ON b.kd_bangsal=m.kd_bangsal WHERE k.no_rawat=? ORDER BY k.tgl_masuk,k.jam_masuk`, noRawat)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kd, nm, tm, tk, st string
		var trf, lama float64
		rows.Scan(&kd, &nm, &trf, &tm, &tk, &lama, &st)
		if st == "-" {
			lama = float64(LamaInap(tm, Today()))
		}
		add("Kamar", fmt.Sprintf("Kamar %s %s (%s s/d %s)", kd, nm, tm, map[bool]string{true: "sekarang", false: tk}[st == "-"]), trf, lama)
	}
	rows.Close()
	return b, nil
}

// BayarBilling menyimpan rincian ke tabel billing, membuat nota, dan mengunci transaksi.
func BayarBilling(ctx context.Context, tx *sql.Tx, noRawat string) (string, error) {
	r, err := mustOpen(ctx, tx, noRawat)
	if err != nil {
		return "", err
	}
	if r.StatusLanjut == "Ranap" {
		if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM kamar_inap WHERE no_rawat=? AND stts_pulang='-'", noRawat); n > 0 {
			return "", errors.New("pasien masih dirawat, pulangkan dulu sebelum pembayaran")
		}
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM resep_obat WHERE no_rawat=? AND tgl_perawatan IS NULL", noRawat); n > 0 {
		return "", errors.New("masih ada resep yang belum divalidasi farmasi")
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM permintaan_lab WHERE no_rawat=? AND tgl_hasil IS NULL", noRawat); n > 0 {
		return "", errors.New("masih ada permintaan lab yang belum ada hasilnya")
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM permintaan_radiologi WHERE no_rawat=? AND tgl_hasil IS NULL", noRawat); n > 0 {
		return "", errors.New("masih ada permintaan radiologi yang belum ada hasilnya")
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM booking_operasi WHERE no_rawat=? AND status='Proses Operasi'", noRawat); n > 0 {
		return "", errors.New("operasi pasien masih berlangsung")
	}
	b, err := HitungBilling(ctx, tx, noRawat)
	if err != nil {
		return "", err
	}
	tgl := Today()
	if _, err = tx.ExecContext(ctx, "DELETE FROM billing WHERE no_rawat=?", noRawat); err != nil {
		return "", err
	}
	for i, it := range b.Items {
		if _, err = tx.ExecContext(ctx, "INSERT INTO billing (no_rawat,tgl_byr,no,nm_perawatan,pemisah,biaya,jumlah,tambahan,totalbiaya,status) VALUES (?,?,?,?,':',?,?,0,?,?)",
			noRawat, tgl, strconv.Itoa(i+1), it.Nama, it.Biaya, it.Jml, it.Total, it.Status); err != nil {
			return "", err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO billing (no_rawat,tgl_byr,no,nm_perawatan,pemisah,biaya,jumlah,tambahan,totalbiaya,status) VALUES (?,?,'','TOTAL TAGIHAN',':',?,1,0,?,'Tagihan')", noRawat, tgl, b.Total, b.Total); err != nil {
		return "", err
	}
	kind, table := "RJ", "nota_jalan"
	if b.Ranap {
		kind, table = "RI", "nota_inap"
	}
	prefix := strings.ReplaceAll(tgl, "-", "/") + "/" + kind + "/"
	nota, err := seqDaily(ctx, tx, table, "no_nota", prefix, 4)
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO "+table+" (no_rawat,no_nota,tanggal,jam) VALUES (?,?,?,?)", noRawat, nota, tgl, NowTime()); err != nil {
		return "", err
	}
	if err = postKasir(ctx, tx, r, b, nota); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE rawat_jl_dr SET stts_bayar='Sudah' WHERE no_rawat=?", noRawat); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE permintaan_pemeriksaan_lab SET stts_bayar='Sudah' WHERE noorder IN (SELECT noorder FROM permintaan_lab WHERE no_rawat=?)", noRawat); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE permintaan_pemeriksaan_radiologi SET stts_bayar='Sudah' WHERE noorder IN (SELECT noorder FROM permintaan_radiologi WHERE no_rawat=?)", noRawat); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, "UPDATE reg_periksa SET status_bayar='Sudah Bayar', stts=IF(stts IN ('Belum'),'Sudah',stts) WHERE no_rawat=?", noRawat)
	return nota, err
}

func TambahBiaya(ctx context.Context, tx *sql.Tx, noRawat, nama string, besar float64) error {
	if _, err := mustOpen(ctx, tx, noRawat); err != nil {
		return err
	}
	if strings.TrimSpace(nama) == "" || besar == 0 {
		return errors.New("nama & besar biaya wajib diisi")
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO tambahan_biaya VALUES (?,?,?) ON DUPLICATE KEY UPDATE besar_biaya=VALUES(besar_biaya)", noRawat, nama, besar)
	return err
}

// ---------------------------------------------------------------- task id antrean BPJS

// CatatTask mencatat waktu task id antrean (1..7, 99) secara lokal; pengiriman ke BPJS oleh paket bpjs.
func CatatTask(ctx context.Context, q Q, noRawat, taskid string) error {
	_, err := q.ExecContext(ctx, "INSERT IGNORE INTO referensi_mobilejkn_bpjs_taskid VALUES (?,?,?)", noRawat, taskid, Now().Format("2006-01-02 15:04:05"))
	return err
}
