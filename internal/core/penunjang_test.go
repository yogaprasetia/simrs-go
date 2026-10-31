package core_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/yogaprasetia/simrs-go/internal/core"
)

func TestRadiologiDanOperasi(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	setNow("2026-11-09 08:00:00") // Senin
	defer func() { core.Now = func() time.Time { return time.Now().In(core.Loc) } }()

	p := &core.Pasien{Nama: "Joko", JK: "L", TglLahir: "1985-03-03"}
	tx(t, d, func(x *sql.Tx) error { return core.SimpanPasien(ctx, x, p) })
	var r core.RegResult
	tx(t, d, func(x *sql.Tx) (err error) {
		r, err = core.Registrasi(ctx, x, core.RegInput{NoRM: p.NoRM, KdPoli: "IGDK", KdDokter: "D0000001", KdPJ: "A09"})
		return
	})
	no := r.NoRawat

	// ---------------- radiologi
	var ord string
	tx(t, d, func(x *sql.Tx) (err error) {
		ord, err = core.PermintaanRadiologi(ctx, x, no, "D0000001", "Susp. appendisitis", "nyeri perut kanan bawah", []string{"RAD003", "RAD005"})
		return
	})
	if ord != "PR202611090001" {
		t.Fatalf("noorder %s", ord)
	}
	if err := txErr(d, func(x *sql.Tx) error { return core.SimpanExpertise(ctx, x, ord, "x") }); err == nil {
		t.Fatal("expertise sebelum pemeriksaan seharusnya ditolak")
	}
	if err := txErr(d, func(x *sql.Tx) error { return core.PeriksaRadiologi(ctx, x, ord, "NOPE", "D0000007", nil) }); err == nil {
		t.Fatal("radiografer tak dikenal seharusnya ditolak")
	}
	tx(t, d, func(x *sql.Tx) error {
		return core.PeriksaRadiologi(ctx, x, ord, "P006", "D0000007", map[string]core.Paparan{"RAD003": {Proyeksi: "AP", KV: "75", MAS: "20", FFD: "100"}})
	})
	if err := txErr(d, func(x *sql.Tx) error { return core.PeriksaRadiologi(ctx, x, ord, "P006", "D0000007", nil) }); err == nil {
		t.Fatal("periksa 2x seharusnya ditolak")
	}
	if kv := q1[string](t, d, "SELECT kV FROM periksa_radiologi WHERE kd_jenis_prw='RAD003'"); kv != "75" {
		t.Fatalf("kV %s", kv)
	}
	tx(t, d, func(x *sql.Tx) error { return core.TambahGambar(ctx, x, ord, "radiologi/abc.png") })
	// kasir menolak sebelum expertise
	if err := txErr(d, func(x *sql.Tx) error { _, err := core.BayarBilling(ctx, x, no); return err }); err == nil || !strings.Contains(err.Error(), "radiologi") {
		t.Fatalf("bayar sebelum expertise: %v", err)
	}
	tx(t, d, func(x *sql.Tx) error {
		return core.SimpanExpertise(ctx, x, ord, "USG abdomen: tampak struktur tubular non-compressible di RLQ diameter 9 mm.\nKesan: appendisitis akut.")
	})
	if n := q1[int](t, d, "SELECT COUNT(*) FROM hasil_radiologi WHERE no_rawat=?", no); n != 1 {
		t.Fatal("hasil radiologi")
	}

	// ---------------- operasi: booking & bentrok ruang
	tx(t, d, func(x *sql.Tx) error { return core.MasukKamar(ctx, x, no, "MLT.01", "Appendisitis akut", "D0000006") })
	tx(t, d, func(x *sql.Tx) error {
		return core.BookingOperasi(ctx, x, no, "OP001", "2026-11-09", "10:00", "11:30", "D0000006", "OK1")
	})
	// pasien lain, jam bentrok di OK1
	p2 := &core.Pasien{Nama: "Rina", JK: "P", TglLahir: "1995-01-01"}
	var r2 core.RegResult
	tx(t, d, func(x *sql.Tx) (err error) {
		if err = core.SimpanPasien(ctx, x, p2); err != nil {
			return
		}
		r2, err = core.Registrasi(ctx, x, core.RegInput{NoRM: p2.NoRM, KdPoli: "IGDK", KdDokter: "D0000001", KdPJ: "A09"})
		return
	})
	if err := txErr(d, func(x *sql.Tx) error {
		return core.BookingOperasi(ctx, x, r2.NoRawat, "OP004", "2026-11-09", "11:00", "12:00", "D0000006", "OK1")
	}); err == nil || !strings.Contains(err.Error(), "sudah dipakai") {
		t.Fatalf("bentrok ruang seharusnya ditolak: %v", err)
	}
	tx(t, d, func(x *sql.Tx) error { // ruang lain boleh
		return core.BookingOperasi(ctx, x, r2.NoRawat, "OP004", "2026-11-09", "11:00", "12:00", "D0000006", "OK2")
	})
	tx(t, d, func(x *sql.Tx) error { // tepat setelah selesai boleh
		return core.BookingOperasi(ctx, x, r2.NoRawat, "OP004", "2026-11-09", "11:30", "12:00", "D0000006", "OK1")
	})
	tx(t, d, func(x *sql.Tx) error {
		return core.StatusBooking(ctx, x, r2.NoRawat, "OP004", "2026-11-09", "11:30", "batal")
	})
	if err := txErr(d, func(x *sql.Tx) error {
		return core.BookingOperasi(ctx, x, no, "OP001", "2026-11-01", "10:00", "11:00", "D0000006", "OK1")
	}); err == nil {
		t.Fatal("tanggal lewat seharusnya ditolak")
	}
	tx(t, d, func(x *sql.Tx) error { return core.StatusBooking(ctx, x, no, "OP001", "2026-11-09", "10:00", "mulai") })
	setNow("2026-11-09 11:40:00")
	if err := txErr(d, func(x *sql.Tx) error { _, err := core.BayarBilling(ctx, x, no); return err }); err == nil {
		t.Fatal("bayar saat operasi berlangsung seharusnya ditolak")
	}
	if err := txErr(d, func(x *sql.Tx) error {
		_, err := core.SimpanOperasi(ctx, x, core.OperasiInput{NoRawat: no, KodePaket: "OP001", TglOperasi: "2026-11-09 10:05", Tim: map[string]string{}})
		return err
	}); err == nil || !strings.Contains(err.Error(), "operator 1") {
		t.Fatalf("tanpa operator: %v", err)
	}
	var tot float64
	tx(t, d, func(x *sql.Tx) (err error) {
		tot, err = core.SimpanOperasi(ctx, x, core.OperasiInput{NoRawat: no, KodePaket: "OP001", TglOperasi: "2026-11-09T10:05", JenisAnestesi: "SAB", Kategori: "Sedang",
			Tim: map[string]string{"operator1": "D0000006", "asisten_operator1": "P001", "instrumen": "P002", "dokter_anestesi": "D0000008", "asisten_anestesi": "P003"}})
		return
	})
	// OP001: op 2.500.000 + asisten 350.000 + instrumen 200.000 + anestesi 900.000 + ast anestesi 250.000
	//        (perawat luar & omloop tidak diisi -> 0) + sewa 1.200.000 + alat 600.000 + bagian RS 800.000 + sarpras 200.000
	wantOp := 2500000.0 + 350000 + 200000 + 900000 + 250000 + 1200000 + 600000 + 800000 + 200000
	if tot != wantOp {
		t.Fatalf("total operasi %v want %v", tot, wantOp)
	}
	if st := q1[string](t, d, "SELECT status FROM booking_operasi WHERE no_rawat=?", no); st != "Selesai" {
		t.Fatalf("booking %s", st)
	}
	if err := txErr(d, func(x *sql.Tx) error {
		_, err := core.SimpanOperasi(ctx, x, core.OperasiInput{NoRawat: no, KodePaket: "OP001", TglOperasi: "2026-11-09 10:05", Tim: map[string]string{"operator1": "D0000006"}})
		return err
	}); err == nil {
		t.Fatal("operasi duplikat seharusnya ditolak")
	}
	if err := txErr(d, func(x *sql.Tx) error {
		_, err := core.SimpanOperasi(ctx, x, core.OperasiInput{NoRawat: no, KodePaket: "OP002", TglOperasi: "2026-11-09 10:05", Tim: map[string]string{"operator1": "P001"}})
		return err
	}); err == nil {
		t.Fatal("perawat sebagai operator seharusnya ditolak")
	}
	tx(t, d, func(x *sql.Tx) error {
		return core.SimpanLaporanOperasi(ctx, x, core.LaporanOperasi{NoRawat: no, Tanggal: "2026-11-09 10:05", Selesai: "2026-11-09 11:10", DxPre: "Appendisitis akut", DxPost: "Appendisitis akut perforasi (-)",
			Jaringan: "Appendiks", PermintaanPA: "Ya", Laporan: "Insisi McBurney, appendiks dipotong, diikat, luka dijahit lapis demi lapis."})
	})
	if err := txErr(d, func(x *sql.Tx) error {
		return core.SimpanLaporanOperasi(ctx, x, core.LaporanOperasi{NoRawat: no, Tanggal: "2026-11-09 10:05", Selesai: "2026-11-09 09:00", DxPre: "a", DxPost: "b", Laporan: "c"})
	}); err == nil {
		t.Fatal("selesai < mulai seharusnya ditolak")
	}
	tx(t, d, func(x *sql.Tx) error { return core.BeriObatOperasi(ctx, x, no, "OK001", 2) })
	tx(t, d, func(x *sql.Tx) error { return core.BeriObatOperasi(ctx, x, no, "OK004", 1) })

	// ---------------- pulang & billing
	setNow("2026-11-11 09:00:00")
	tx(t, d, func(x *sql.Tx) error { return core.PulangRanap(ctx, x, no, "Membaik", "K35.8") })
	b, err := core.HitungBilling(ctx, d, no)
	if err != nil {
		t.Fatal(err)
	}
	// reg IGD 25.000 + RAD003 150.000 + RAD005 275.000 + operasi + BHP (2x95.000 + 75.000) + kamar MLT 2 hari x 150.000
	want := 25000 + 150000 + 275000 + wantOp + 190000 + 75000 + 300000
	if b.Total != want {
		for _, it := range b.Items {
			t.Logf("%+v", it)
		}
		t.Fatalf("total %v want %v", b.Total, want)
	}
	tx(t, d, func(x *sql.Tx) error { _, err := core.BayarBilling(ctx, x, no); return err })
	if st := q1[string](t, d, "SELECT stts_bayar FROM permintaan_pemeriksaan_radiologi WHERE kd_jenis_prw='RAD005'"); st != "Sudah" {
		t.Fatal("stts_bayar radiologi")
	}
	if err := txErr(d, func(x *sql.Tx) error { return core.BeriObatOperasi(ctx, x, no, "OK005", 1) }); err != core.ErrPaid {
		t.Fatalf("terkunci: %v", err)
	}
	// batal registrasi ditolak bila sudah ada booking
	tx(t, d, func(x *sql.Tx) error {
		return core.BookingOperasi(ctx, x, r2.NoRawat, "OP004", "2026-11-12", "08:00", "09:00", "D0000006", "OK1")
	})
	if err := txErr(d, func(x *sql.Tx) error { return core.BatalRegistrasi(ctx, x, r2.NoRawat) }); err == nil {
		t.Fatal("batal registrasi dengan jadwal operasi seharusnya ditolak")
	}
}
