package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
)

// ================================================================ AKUNTANSI
// Penjurnalan otomatis mengikuti pola Khanza (tabel jurnal/detailjurnal, akun dari set_akun):
// setiap transaksi keuangan menghasilkan satu jurnal seimbang (debet = kredit).

type Baris struct {
	KdRek         string
	Debet, Kredit float64
}

func r2(f float64) float64 { return math.Round(f*100) / 100 }

// Akun mengambil kd_rek untuk nama setelan (mis. "Kas", "Pendapatan_Obat").
func Akun(ctx context.Context, q Q, nama string) (string, error) {
	kd, err := one[string](ctx, q, "SELECT kd_rek FROM app_set_akun WHERE nama=?", nama)
	if err != nil || kd == "" {
		return "", fmt.Errorf("setelan akun %q belum diisi (menu Akuntansi → Setelan akun)", nama)
	}
	return kd, nil
}

func mustAkun(ctx context.Context, q Q, nama ...string) (map[string]string, error) {
	m := map[string]string{}
	for _, n := range nama {
		kd, err := Akun(ctx, q, n)
		if err != nil {
			return nil, err
		}
		m[n] = kd
	}
	return m, nil
}

func NoJurnal(ctx context.Context, tx *sql.Tx) (string, error) {
	return seqDaily(ctx, tx, "jurnal", "no_jurnal", "JR"+Now().Format("20060102"), 6)
}

// PostJurnal menyimpan jurnal; menolak bila tidak seimbang atau akun tidak ada. Baris nol dilewati,
// baris dengan akun sama digabung.
func PostJurnal(ctx context.Context, tx *sql.Tx, noBukti, jenis, ket string, lines []Baris) (string, error) {
	agg := map[string]*Baris{}
	var order []string
	var d, k float64
	for _, l := range lines {
		l.Debet, l.Kredit = r2(l.Debet), r2(l.Kredit)
		if l.Debet < 0 || l.Kredit < 0 {
			return "", errors.New("nilai jurnal tidak boleh negatif")
		}
		if l.Debet == 0 && l.Kredit == 0 {
			continue
		}
		if b, ok := agg[l.KdRek]; ok {
			b.Debet += l.Debet
			b.Kredit += l.Kredit
		} else {
			c := l
			agg[l.KdRek] = &c
			order = append(order, l.KdRek)
		}
		d += l.Debet
		k += l.Kredit
	}
	if len(order) == 0 {
		return "", nil // tidak ada nilai
	}
	if math.Abs(d-k) > 0.005 {
		return "", fmt.Errorf("jurnal tidak seimbang: debet %.2f ≠ kredit %.2f", d, k)
	}
	for _, kd := range order {
		if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM rekening WHERE kd_rek=?", kd); n == 0 {
			return "", fmt.Errorf("akun %s tidak ada di daftar rekening", kd)
		}
	}
	if jenis == "" {
		jenis = "U"
	}
	no, err := NoJurnal(ctx, tx)
	if err != nil {
		return "", err
	}
	if len(ket) > 350 {
		ket = ket[:350]
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO jurnal VALUES (?,?,?,?,?,?)", no, noBukti, Today(), NowTime(), jenis, ket); err != nil {
		return "", err
	}
	for _, kd := range order {
		b := agg[kd]
		// akun yang muncul di dua sisi dinetralkan
		db, kr := b.Debet, b.Kredit
		if db > 0 && kr > 0 {
			if db >= kr {
				db, kr = r2(db-kr), 0
			} else {
				db, kr = 0, r2(kr-db)
			}
			if db == 0 && kr == 0 {
				continue
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO detailjurnal VALUES (?,?,?,?)", no, kd, db, kr); err != nil {
			return "", err
		}
	}
	return no, nil
}

// BalikJurnal membuat jurnal pembalik untuk semua jurnal dengan no_bukti tertentu (dipakai saat pembatalan).
func BalikJurnal(ctx context.Context, tx *sql.Tx, noBukti, ket string) error {
	rows, err := tx.QueryContext(ctx, "SELECT d.kd_rek, SUM(d.debet), SUM(d.kredit) FROM detailjurnal d JOIN jurnal j ON j.no_jurnal=d.no_jurnal WHERE j.no_bukti=? GROUP BY d.kd_rek", noBukti)
	if err != nil {
		return err
	}
	var lines []Baris
	for rows.Next() {
		var kd string
		var db, kr float64
		rows.Scan(&kd, &db, &kr)
		lines = append(lines, Baris{kd, kr, db})
	}
	rows.Close()
	_, err = PostJurnal(ctx, tx, noBukti, "U", ket, lines)
	return err
}

// ---------------------------------------------------------------- posting kasir & piutang

// postKasir: dipanggil BayarBilling. Umum -> Kas; selain itu -> Piutang Pasien (+ data piutang_pasien).
func postKasir(ctx context.Context, tx *sql.Tx, r *Reg, b *Bill, nota string) error {
	per := map[string]float64{}
	for _, it := range b.Items {
		per[it.Status] += it.Total
	}
	var lines []Baris
	for st, v := range per {
		kd, err := Akun(ctx, tx, "Pendapatan_"+st)
		if err != nil {
			return err
		}
		lines = append(lines, Baris{KdRek: kd, Kredit: v})
	}
	debit := "Kas"
	if r.KdPJ != "A09" {
		debit = "Piutang_Pasien"
	}
	kd, err := Akun(ctx, tx, debit)
	if err != nil {
		return err
	}
	lines = append([]Baris{{KdRek: kd, Debet: b.Total}}, lines...)
	if _, err := PostJurnal(ctx, tx, r.NoRawat, "U", "Pembayaran "+r.StatusLanjut+" "+r.NoRawat+" "+r.Nama+" ("+r.PngJawab+") nota "+nota, lines); err != nil {
		return err
	}
	if debit == "Piutang_Pasien" && b.Total > 0 {
		tempo := Now().AddDate(0, 0, 30).Format("2006-01-02")
		if _, err := tx.ExecContext(ctx, "INSERT INTO piutang_pasien VALUES (?,?,?,'Belum Lunas',?,0,?,?)", r.NoRawat, Today(), r.NoRM, b.Total, b.Total, tempo); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO detail_piutang_pasien VALUES (?,?,?,?,?,?)", r.NoRawat, r.PngJawab, r.KdPJ, b.Total, b.Total, tempo); err != nil {
			return err
		}
	}
	return nil
}

// BatalBayar membuka kembali transaksi: hapus billing/nota, jurnal pembalik, hapus piutang (bila belum dicicil).
func BatalBayar(ctx context.Context, tx *sql.Tx, noRawat string) error {
	r, err := GetReg(ctx, tx, noRawat)
	if err != nil {
		return err
	}
	if r.StatusBayar != "Sudah Bayar" {
		return errors.New("transaksi belum dibayar")
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM bayar_piutang WHERE no_rawat=?", noRawat); n > 0 {
		return errors.New("piutang pasien ini sudah ada pembayaran cicilan; batalkan cicilannya dulu")
	}
	if err := BalikJurnal(ctx, tx, noRawat, "Batal pembayaran "+noRawat); err != nil {
		return err
	}
	for _, q := range []string{"DELETE FROM billing WHERE no_rawat=?", "DELETE FROM nota_jalan WHERE no_rawat=?", "DELETE FROM nota_inap WHERE no_rawat=?",
		"DELETE FROM piutang_pasien WHERE no_rawat=?", "DELETE FROM detail_piutang_pasien WHERE no_rawat=?",
		"UPDATE rawat_jl_dr SET stts_bayar='Belum' WHERE no_rawat=?", "UPDATE reg_periksa SET status_bayar='Belum Bayar' WHERE no_rawat=?"} {
		if _, err := tx.ExecContext(ctx, q, noRawat); err != nil {
			return err
		}
	}
	return nil
}

type BayarPiutangInput struct {
	NoRawat                string
	Cicilan, Diskon, Hapus float64 // Hapus = piutang tidak terbayar (dihapusbukukan)
	Via, Catatan           string  // Via: "Kas" | "Bank"
}

func BayarPiutang(ctx context.Context, tx *sql.Tx, in BayarPiutangInput) (float64, error) {
	var noRM, st string
	var sisa float64
	err := tx.QueryRowContext(ctx, "SELECT no_rkm_medis,status,sisapiutang FROM piutang_pasien WHERE no_rawat=? FOR UPDATE", in.NoRawat).Scan(&noRM, &st, &sisa)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errors.New("piutang tidak ditemukan")
	} else if err != nil {
		return 0, err
	}
	if st == "Lunas" {
		return 0, errors.New("piutang sudah lunas")
	}
	in.Cicilan, in.Diskon, in.Hapus = r2(in.Cicilan), r2(in.Diskon), r2(in.Hapus)
	if in.Cicilan < 0 || in.Diskon < 0 || in.Hapus < 0 {
		return 0, errors.New("nilai tidak boleh negatif")
	}
	tot := r2(in.Cicilan + in.Diskon + in.Hapus)
	if tot <= 0 {
		return 0, errors.New("isi besar pembayaran")
	}
	if tot > r2(sisa)+0.004 {
		return 0, fmt.Errorf("pembayaran %s melebihi sisa piutang %s", rp(tot), rp(sisa))
	}
	if in.Via != "Bank" {
		in.Via = "Kas"
	}
	ak, err := mustAkun(ctx, tx, in.Via, "Piutang_Pasien", "Diskon_Piutang", "Piutang_Tidak_Terbayar")
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO bayar_piutang VALUES (?,?,?,?,?,?,?,?,?,?,?)", Today(), noRM, in.Cicilan, in.Catatan, in.NoRawat,
		ak[in.Via], ak["Piutang_Pasien"], in.Diskon, ak["Diskon_Piutang"], in.Hapus, ak["Piutang_Tidak_Terbayar"]); err != nil {
		return 0, err
	}
	baru := r2(sisa - tot)
	status := "Belum Lunas"
	if baru <= 0.004 {
		baru, status = 0, "Lunas"
	}
	if _, err := tx.ExecContext(ctx, "UPDATE piutang_pasien SET sisapiutang=?, status=? WHERE no_rawat=?", baru, status, in.NoRawat); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE detail_piutang_pasien SET sisapiutang=? WHERE no_rawat=?", baru, in.NoRawat); err != nil {
		return 0, err
	}
	_, err = PostJurnal(ctx, tx, in.NoRawat, "U", "Bayar piutang "+in.NoRawat+" "+in.Catatan, []Baris{
		{KdRek: ak[in.Via], Debet: in.Cicilan}, {KdRek: ak["Diskon_Piutang"], Debet: in.Diskon}, {KdRek: ak["Piutang_Tidak_Terbayar"], Debet: in.Hapus},
		{KdRek: ak["Piutang_Pasien"], Kredit: tot}})
	return baru, err
}

func rp(f float64) string {
	s := fmt.Sprintf("%.0f", math.Round(f))
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 && s[i-1] != '-' {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	return "Rp " + b.String()
}

// JurnalManual: jurnal umum yang diinput petugas keuangan (mis. saldo awal, beban operasional).
func JurnalManual(ctx context.Context, tx *sql.Tx, noBukti, ket string, lines []Baris) (string, error) {
	if strings.TrimSpace(ket) == "" {
		return "", errors.New("keterangan jurnal wajib diisi")
	}
	n := 0
	for _, l := range lines {
		if l.Debet != 0 || l.Kredit != 0 {
			n++
			if l.Debet != 0 && l.Kredit != 0 {
				return "", errors.New("satu baris hanya boleh debet atau kredit")
			}
		}
	}
	if n < 2 {
		return "", errors.New("jurnal minimal 2 baris")
	}
	no, err := PostJurnal(ctx, tx, noBukti, "U", ket, lines)
	return no, err
}

// ---------------------------------------------------------------- laporan akuntansi

type Saldo struct {
	KdRek, NmRek, Tipe, Balance string
	Debet, Kredit, Saldo        float64 // Saldo sesuai saldo normal akun
}

// NeracaSaldo menghitung mutasi debet/kredit per akun dalam rentang tanggal (sampai = inklusif).
// Untuk akun neraca (N/M) dihitung kumulatif sejak awal; akun R hanya periode.
func NeracaSaldo(ctx context.Context, q Q, dari, sampai string) ([]Saldo, error) {
	rows, err := q.QueryContext(ctx, `SELECT r.kd_rek, r.nm_rek, r.tipe, r.balance, IFNULL(x.db,0), IFNULL(x.kr,0)
		FROM rekening r LEFT JOIN (SELECT d.kd_rek, SUM(IF(rr.tipe='R' AND j.tgl_jurnal<?,0,d.debet)) db, SUM(IF(rr.tipe='R' AND j.tgl_jurnal<?,0,d.kredit)) kr
			FROM detailjurnal d JOIN jurnal j ON j.no_jurnal=d.no_jurnal JOIN rekening rr ON rr.kd_rek=d.kd_rek WHERE j.tgl_jurnal<=? GROUP BY d.kd_rek) x ON x.kd_rek=r.kd_rek
		ORDER BY r.kd_rek`, dari, dari, sampai)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Saldo
	for rows.Next() {
		var s Saldo
		if err := rows.Scan(&s.KdRek, &s.NmRek, &s.Tipe, &s.Balance, &s.Debet, &s.Kredit); err != nil {
			return nil, err
		}
		s.Debet, s.Kredit = r2(s.Debet), r2(s.Kredit)
		if s.Balance == "D" {
			s.Saldo = r2(s.Debet - s.Kredit)
		} else {
			s.Saldo = r2(s.Kredit - s.Debet)
		}
		out = append(out, s)
	}
	return out, nil
}
