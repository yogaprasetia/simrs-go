package core_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yogaprasetia/simrs-go/internal/core"
	"github.com/yogaprasetia/simrs-go/internal/db"
)

func testDB(t *testing.T) *sql.DB {
	dsn := os.Getenv("SIMRS_TEST_DSN")
	if dsn == "" {
		pw, err := os.ReadFile("/data/workspace/.simrs-db-pass")
		if err != nil {
			t.Skip("SIMRS_TEST_DSN tidak diset")
		}
		dsn = "simrs:" + strings.TrimSpace(string(pw)) + "@tcp(127.0.0.1:3306)/sik_test?charset=utf8mb4&multiStatements=false"
	}
	d, err := db.Open(dsn)
	if err != nil {
		t.Skip("database tes tidak tersedia: ", err)
	}
	ctx := context.Background()
	rows, _ := d.QueryContext(ctx, "SHOW TABLES")
	var tabs []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		tabs = append(tabs, s)
	}
	rows.Close()
	for _, tb := range tabs {
		d.ExecContext(ctx, "DROP TABLE `"+tb+"`")
	}
	if err := db.Migrate(ctx, d, true); err != nil {
		t.Fatal(err)
	}
	return d
}

func tx(t *testing.T, d *sql.DB, fn func(*sql.Tx) error) {
	t.Helper()
	if err := core.WithTx(context.Background(), d, fn); err != nil {
		t.Fatal(err)
	}
}

func txErr(d *sql.DB, fn func(*sql.Tx) error) error {
	return core.WithTx(context.Background(), d, fn)
}

func q1[T any](t *testing.T, d *sql.DB, query string, args ...any) T {
	t.Helper()
	var v T
	if err := d.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

func setNow(s string) {
	tm, _ := time.ParseInLocation("2006-01-02 15:04:05", s, core.Loc)
	core.Now = func() time.Time { return tm }
}

func TestUmurDanFlag(t *testing.T) {
	cases := []struct {
		lahir, ref, txt string
		n               int
		sat             string
	}{
		{"1990-05-05", "2026-10-31", "36 Th 5 Bl 26 Hr", 36, "Th"},
		{"2026-03-15", "2026-10-31", "0 Th 7 Bl 16 Hr", 7, "Bl"},
		{"2026-10-20", "2026-10-31", "0 Th 0 Bl 11 Hr", 11, "Hr"},
		{"2000-02-29", "2026-02-28", "25 Th 11 Bl 30 Hr", 25, "Th"},
	}
	for _, c := range cases {
		n, s, txt := core.Umur(c.lahir, c.ref)
		if n != c.n || s != c.sat || txt != c.txt {
			t.Errorf("Umur(%s,%s)=%d %s %q", c.lahir, c.ref, n, s, txt)
		}
	}
	for _, c := range [][3]string{{"18", "13.0-17.0", "H"}, {"11,5", "12.0-15.0", "L"}, {"14", "13.0-17.0", ""}, {"250", "<200", "H"}, {"Negatif", "Negatif", ""}} {
		if f := core.Flag(c[0], c[1]); f != c[2] {
			t.Errorf("Flag(%s,%s)=%q want %q", c[0], c[1], f, c[2])
		}
	}
	if core.LamaInap("2026-10-31", "2026-10-31") != 1 || core.LamaInap("2026-10-29", "2026-10-31") != 2 {
		t.Error("LamaInap salah")
	}
}

func TestAlurLengkap(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	setNow("2026-11-02 08:00:00") // Senin
	defer func() { core.Now = func() time.Time { return time.Now().In(core.Loc) } }()

	// ---- pasien baru
	p := &core.Pasien{Nama: "budi hartono", JK: "L", TglLahir: "1980-01-01", NoKTP: "3372010101800001", Alamat: "Jl. Mawar 1", KdPJ: "BPJ", NoPeserta: "0001234567890", NamaKeluarga: "SRI", Keluarga: "ISTRI"}
	tx(t, d, func(x *sql.Tx) error { return core.SimpanPasien(ctx, x, p) })
	if p.NoRM != "000001" || p.Nama != "BUDI HARTONO" {
		t.Fatalf("norm/nama: %s %s", p.NoRM, p.Nama)
	}
	p2 := &core.Pasien{Nama: "Siti", JK: "P", TglLahir: "1990-05-05"}
	tx(t, d, func(x *sql.Tx) error { return core.SimpanPasien(ctx, x, p2) })
	if p2.NoRM != "000002" {
		t.Fatalf("norm2 %s", p2.NoRM)
	}
	dup := &core.Pasien{Nama: "Dobel", JK: "L", TglLahir: "1980-01-01", NoKTP: "3372010101800001"}
	if err := txErr(d, func(x *sql.Tx) error { return core.SimpanPasien(ctx, x, dup) }); err == nil || !strings.Contains(err.Error(), "000001") {
		t.Fatalf("NIK dobel seharusnya ditolak: %v", err)
	}

	// ---- registrasi
	var r1, r2, r3 core.RegResult
	tx(t, d, func(x *sql.Tx) (err error) {
		r1, err = core.Registrasi(ctx, x, core.RegInput{NoRM: "000001", KdPoli: "INT", KdDokter: "D0000002", KdPJ: "BPJ"})
		return
	})
	if r1.NoRawat != "2026/11/02/000001" || r1.NoReg != "001" || r1.SttsDaftar != "Baru" || r1.StatusPoli != "Baru" || r1.Biaya != 30000 {
		t.Fatalf("reg1 %+v", r1)
	}
	tx(t, d, func(x *sql.Tx) (err error) {
		r2, err = core.Registrasi(ctx, x, core.RegInput{NoRM: "000002", KdPoli: "INT", KdDokter: "D0000002", KdPJ: "A09"})
		return
	})
	if r2.NoRawat != "2026/11/02/000002" || r2.NoReg != "002" {
		t.Fatalf("reg2 %+v", r2)
	}
	if err := txErr(d, func(x *sql.Tx) error {
		_, err := core.Registrasi(ctx, x, core.RegInput{NoRM: "000001", KdPoli: "INT", KdDokter: "D0000002", KdPJ: "BPJ"})
		return err
	}); err == nil {
		t.Fatal("daftar 2x di poli sama seharusnya ditolak")
	}
	tx(t, d, func(x *sql.Tx) (err error) {
		r3, err = core.Registrasi(ctx, x, core.RegInput{NoRM: "000002", KdPoli: "U0001", KdDokter: "D0000001", KdPJ: "A09"})
		return
	})
	if r3.NoReg != "001" || r3.SttsDaftar != "Lama" || r3.StatusPoli != "Baru" || r3.Biaya != 15000 {
		t.Fatalf("reg3 %+v", r3)
	}
	if umur := q1[string](t, d, "SELECT CONCAT(umurdaftar,sttsumur) FROM reg_periksa WHERE no_rawat=?", r1.NoRawat); umur != "46Th" {
		t.Fatalf("umur daftar %s", umur)
	}
	// batal registrasi r3 (belum ada data)
	tx(t, d, func(x *sql.Tx) error { return core.BatalRegistrasi(ctx, x, r3.NoRawat) })

	// ---- pemeriksaan & ERM (rawat jalan)
	tx(t, d, func(x *sql.Tx) error {
		_, err := core.SimpanSOAP(ctx, x, core.SOAP{NoRawat: r1.NoRawat, Tensi: "160/100", Nadi: "88", Suhu: "36.7", Keluhan: "pusing, tengkuk berat", Pemeriksaan: "TD 160/100", Penilaian: "Hipertensi", RTL: "Amlodipine 5mg 1x1, cek lab", NIP: "D0000002"})
		return err
	})
	if st := q1[string](t, d, "SELECT stts FROM reg_periksa WHERE no_rawat=?", r1.NoRawat); st != "Sudah" {
		t.Fatalf("stts setelah SOAP: %s", st)
	}
	tx(t, d, func(x *sql.Tx) error { return core.TambahDiagnosa(ctx, x, r1.NoRawat, "I10", false) })
	tx(t, d, func(x *sql.Tx) error { return core.TambahDiagnosa(ctx, x, r1.NoRawat, "E78.5", false) })
	if pr := q1[int](t, d, "SELECT prioritas FROM diagnosa_pasien WHERE no_rawat=? AND kd_penyakit='E78.5'", r1.NoRawat); pr != 2 {
		t.Fatalf("prioritas %d", pr)
	}
	if err := txErr(d, func(x *sql.Tx) error { return core.TambahDiagnosa(ctx, x, r1.NoRawat, "ZZZ", false) }); err == nil {
		t.Fatal("ICD tak dikenal seharusnya ditolak")
	}
	tx(t, d, func(x *sql.Tx) error { return core.TambahTindakan(ctx, x, r1.NoRawat, "RJ002", "D0000002", false) })
	tx(t, d, func(x *sql.Tx) error { return core.TambahTindakan(ctx, x, r1.NoRawat, "RJ007", "D0000002", false) })

	// ---- resep
	var noResep string
	tx(t, d, func(x *sql.Tx) (err error) {
		noResep, err = core.BuatResep(ctx, x, r1.NoRawat, "D0000002", []core.ItemResep{{"B000006", 30, "1x1 pagi"}, {"B000001", 10, "3x1 bila pusing"}})
		return
	})
	if noResep != "202611020001" {
		t.Fatalf("no resep %s", noResep)
	}
	// stok kurang -> resep lain gagal divalidasi
	var noResep2 string
	tx(t, d, func(x *sql.Tx) (err error) {
		noResep2, err = core.BuatResep(ctx, x, r2.NoRawat, "D0000002", []core.ItemResep{{"B000009", 500, "3x1 sdm"}})
		return
	})
	if err := txErr(d, func(x *sql.Tx) error { return core.ValidasiResep(ctx, x, noResep2, "farmasi") }); err == nil || !strings.Contains(err.Error(), "stok") {
		t.Fatalf("stok kurang seharusnya ditolak: %v", err)
	}
	if stok := q1[float64](t, d, "SELECT stok FROM gudangbarang WHERE kode_brng='B000009'"); stok != 80 {
		t.Fatalf("stok berubah walau rollback: %v", stok)
	}
	tx(t, d, func(x *sql.Tx) error { return core.HapusResep(ctx, x, noResep2) })
	tx(t, d, func(x *sql.Tx) error { return core.ValidasiResep(ctx, x, noResep, "farmasi") })
	if stok := q1[float64](t, d, "SELECT stok FROM gudangbarang WHERE kode_brng='B000006'"); stok != 970 {
		t.Fatalf("stok amlodipine %v", stok)
	}
	if err := txErr(d, func(x *sql.Tx) error { return core.ValidasiResep(ctx, x, noResep, "farmasi") }); err == nil {
		t.Fatal("validasi ganda seharusnya ditolak")
	}
	if n := q1[int](t, d, "SELECT COUNT(*) FROM riwayat_barang_medis WHERE posisi='Pemberian Obat'"); n != 2 {
		t.Fatalf("riwayat %d", n)
	}
	tx(t, d, func(x *sql.Tx) error { return core.SerahkanResep(ctx, x, noResep) })

	// ---- lab
	var noorder string
	tx(t, d, func(x *sql.Tx) (err error) {
		noorder, err = core.PermintaanLab(ctx, x, r1.NoRawat, "D0000002", "Hipertensi", []string{"LAB001", "LAB003"})
		return
	})
	if noorder != "PK202611020001" {
		t.Fatalf("noorder %s", noorder)
	}
	// kasir menolak bila lab belum ada hasil
	if err := txErr(d, func(x *sql.Tx) error { _, err := core.BayarBilling(ctx, x, r1.NoRawat); return err }); err == nil || !strings.Contains(err.Error(), "lab") {
		t.Fatalf("bayar sebelum hasil lab seharusnya ditolak: %v", err)
	}
	tx(t, d, func(x *sql.Tx) error {
		return core.SimpanHasilLab(ctx, x, noorder, "lab1", "D0000001", map[int]string{1: "18.2", 2: "7.5", 3: "45", 4: "250", 5: "5.0", 7: "245"})
	})
	if f := q1[string](t, d, "SELECT keterangan FROM detail_periksa_lab WHERE id_template=1"); f != "H" {
		t.Fatalf("flag Hb %q", f)
	}
	if f := q1[string](t, d, "SELECT keterangan FROM detail_periksa_lab WHERE id_template=7"); f != "H" {
		t.Fatalf("flag kolesterol %q", f)
	}

	// ---- billing rawat jalan
	b, err := core.HitungBilling(ctx, d, r1.NoRawat)
	if err != nil {
		t.Fatal(err)
	}
	// 30000 reg + 75000 + 85000 tindakan + amlodipine 30*1000 + paracetamol 10*500 + lab 85000+45000
	want := 30000.0 + 75000 + 85000 + 30000 + 5000 + 85000 + 45000
	if b.Total != want {
		for _, it := range b.Items {
			t.Logf("%+v", it)
		}
		t.Fatalf("total %v want %v", b.Total, want)
	}
	var nota string
	tx(t, d, func(x *sql.Tx) (err error) { nota, err = core.BayarBilling(ctx, x, r1.NoRawat); return })
	if nota != "2026/11/02/RJ/0001" {
		t.Fatalf("nota %s", nota)
	}
	if tot := q1[float64](t, d, "SELECT totalbiaya FROM billing WHERE no_rawat=? AND status='Tagihan'", r1.NoRawat); tot != want {
		t.Fatalf("billing total %v", tot)
	}
	if err := txErr(d, func(x *sql.Tx) error { return core.TambahTindakan(ctx, x, r1.NoRawat, "RJ003", "D0000002", false) }); err != core.ErrPaid {
		t.Fatalf("transaksi terkunci setelah bayar: %v", err)
	}

	// ---- rawat inap: IGD -> kamar -> pindah -> pulang -> bayar
	setNow("2026-11-02 21:00:00")
	var rIGD core.RegResult
	tx(t, d, func(x *sql.Tx) (err error) {
		rIGD, err = core.Registrasi(ctx, x, core.RegInput{NoRM: "000002", KdPoli: "IGDK", KdDokter: "D0000001", KdPJ: "A09"})
		return
	})
	tx(t, d, func(x *sql.Tx) error { return core.TambahTindakan(ctx, x, rIGD.NoRawat, "RJ006", "D0000001", false) })
	tx(t, d, func(x *sql.Tx) error { return core.MasukKamar(ctx, x, rIGD.NoRawat, "MLT.01", "DHF", "D0000002") })
	if st := q1[string](t, d, "SELECT status FROM kamar WHERE kd_kamar='MLT.01'"); st != "ISI" {
		t.Fatalf("kamar %s", st)
	}
	if err := txErr(d, func(x *sql.Tx) error { return core.MasukKamar(ctx, x, rIGD.NoRawat, "MLT.02", "", "") }); err == nil {
		t.Fatal("masuk kamar 2x seharusnya ditolak")
	}
	var rLain core.RegResult
	tx(t, d, func(x *sql.Tx) (err error) {
		rLain, err = core.Registrasi(ctx, x, core.RegInput{NoRM: "000001", KdPoli: "U0001", KdDokter: "D0000001", KdPJ: "A09"})
		return
	})
	if err := txErr(d, func(x *sql.Tx) error { return core.MasukKamar(ctx, x, rLain.NoRawat, "MLT.01", "", "") }); err == nil {
		t.Fatal("kamar terisi seharusnya ditolak")
	}
	setNow("2026-11-03 09:00:00")
	tx(t, d, func(x *sql.Tx) error {
		_, err := core.SimpanSOAP(ctx, x, core.SOAP{NoRawat: rIGD.NoRawat, Keluhan: "demam hari ke-4", Penilaian: "DHF gr I", NIP: "D0000002", Ranap: true})
		return err
	})
	tx(t, d, func(x *sql.Tx) error { return core.TambahTindakan(ctx, x, rIGD.NoRawat, "RI002", "D0000002", true) })
	tx(t, d, func(x *sql.Tx) error { return core.PindahKamar(ctx, x, rIGD.NoRawat, "MWR.01") })
	if st := q1[string](t, d, "SELECT status FROM kamar WHERE kd_kamar='MLT.01'"); st != "KOSONG" {
		t.Fatalf("kamar lama %s", st)
	}
	var noResepRI string
	tx(t, d, func(x *sql.Tx) (err error) {
		noResepRI, err = core.BuatResep(ctx, x, rIGD.NoRawat, "D0000002", []core.ItemResep{{"B000013", 4, "20 tpm"}})
		return
	})
	tx(t, d, func(x *sql.Tx) error { return core.ValidasiResep(ctx, x, noResepRI, "farmasi") })
	if err := txErr(d, func(x *sql.Tx) error { _, err := core.BayarBilling(ctx, x, rIGD.NoRawat); return err }); err == nil || !strings.Contains(err.Error(), "dirawat") {
		t.Fatalf("bayar saat masih dirawat seharusnya ditolak: %v", err)
	}
	setNow("2026-11-05 10:00:00")
	tx(t, d, func(x *sql.Tx) error { return core.PulangRanap(ctx, x, rIGD.NoRawat, "Membaik", "A91") })
	b2, err := core.HitungBilling(ctx, d, rIGD.NoRawat)
	if err != nil {
		t.Fatal(err)
	}
	// reg 25000 + infus 75000 + visite 100000 + RL 4*22000 + kamar MLT 1 hari*150000 + MWR (3->5) 2 hari*250000
	want2 := 25000.0 + 75000 + 100000 + 88000 + 150000 + 500000
	if b2.Total != want2 || !b2.Ranap {
		for _, it := range b2.Items {
			t.Logf("%+v", it)
		}
		t.Fatalf("total ranap %v want %v", b2.Total, want2)
	}
	tx(t, d, func(x *sql.Tx) (err error) { nota, err = core.BayarBilling(ctx, x, rIGD.NoRawat); return })
	if !strings.HasPrefix(nota, "2026/11/05/RI/") {
		t.Fatalf("nota ranap %s", nota)
	}
	if st := q1[string](t, d, "SELECT status FROM kamar WHERE kd_kamar='MWR.01'"); st != "KOSONG" {
		t.Fatalf("kamar setelah pulang %s", st)
	}

	// ---- konkurensi: 20 registrasi paralel -> no_rawat unik
	setNow("2026-11-06 08:00:00")
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func(i int) {
			pp := &core.Pasien{Nama: "PARALEL", JK: "L", TglLahir: "2000-01-01"}
			errs <- core.WithTx(ctx, d, func(x *sql.Tx) error {
				if err := core.SimpanPasien(ctx, x, pp); err != nil {
					return err
				}
				_, err := core.Registrasi(ctx, x, core.RegInput{NoRM: pp.NoRM, KdPoli: "U0001", KdDokter: "D0000001", KdPJ: "A09"})
				return err
			})
		}(i)
	}
	fails := 0
	for i := 0; i < 20; i++ {
		if err := <-errs; err != nil {
			fails++
			t.Logf("paralel: %v", err)
		}
	}
	n := q1[int](t, d, "SELECT COUNT(DISTINCT no_rawat) FROM reg_periksa WHERE tgl_registrasi='2026-11-06'")
	if n != 20-fails || fails > 2 {
		t.Fatalf("registrasi paralel: %d unik, %d gagal", n, fails)
	}
}
