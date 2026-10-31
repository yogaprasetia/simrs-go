package core_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/yogaprasetia/simrs-go/internal/core"
)

// saldo akun (sesuai saldo normal) dari seluruh jurnal.
func saldo(t *testing.T, d *sql.DB, kd string) float64 {
	t.Helper()
	ns, err := core.NeracaSaldo(context.Background(), d, "2000-01-01", "2100-01-01")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ns {
		if s.KdRek == kd {
			return s.Saldo
		}
	}
	t.Fatalf("akun %s tidak ada", kd)
	return 0
}

func cekSeimbang(t *testing.T, d *sql.DB) {
	t.Helper()
	var bad int
	d.QueryRow("SELECT COUNT(*) FROM (SELECT no_jurnal, ROUND(SUM(debet)-SUM(kredit),2) s FROM detailjurnal GROUP BY no_jurnal HAVING s<>0) x").Scan(&bad)
	if bad != 0 {
		t.Fatalf("%d jurnal tidak seimbang", bad)
	}
}

func TestLogistikDanKeuangan(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	setNow("2026-11-20 09:00:00")
	defer func() { core.Now = func() time.Time { return time.Now().In(core.Loc) } }()

	// ---- surat pemesanan
	var sp string
	tx(t, d, func(x *sql.Tx) (err error) {
		sp, err = core.BuatSP(ctx, x, "S0001", "apt", []core.ItemBeli{{KodeBrng: "B000006", Jumlah: 500, Harga: 300}, {KodeBrng: "B000001", Jumlah: 1000, Harga: 200, Diskon: 10}}, 11)
		return
	})
	if sp != "SPM20261120001" {
		t.Fatalf("no SP %s", sp)
	}
	// ---- penerimaan: 500 x 300 = 150.000; 1000 x 200 = 200.000 - 10% = 180.000 -> total2 330.000; PPN 11% 36.300; meterai 10.000 -> 376.300
	var f core.Faktur
	pen := core.Penerimaan{NoFaktur: "FK-001", NoOrder: sp, Supplier: "S0001", TglFaktur: "2026-11-20", TglTempo: "2026-12-20", Petugas: "apt", PPNPersen: 11, Meterai: 10000, UpdateHargaBeli: true,
		Items: []core.ItemBeli{{KodeBrng: "B000006", Jumlah: 500, Harga: 300, NoBatch: "AML2611", Kadaluarsa: "2028-06-30"}, {KodeBrng: "B000001", Jumlah: 1000, Harga: 200, Diskon: 10, NoBatch: "PCT01", Kadaluarsa: "2027-01-31"}}}
	tx(t, d, func(x *sql.Tx) (err error) { f, err = core.TerimaBarang(ctx, x, pen); return })
	if f.Total2 != 330000 || f.PPN != 36300 || f.Tagihan != 376300 {
		t.Fatalf("faktur %+v", f)
	}
	if err := txErr(d, func(x *sql.Tx) error { _, err := core.TerimaBarang(ctx, x, pen); return err }); err == nil {
		t.Fatal("faktur ganda seharusnya ditolak")
	}
	pen2 := pen
	pen2.NoFaktur, pen2.NoOrder = "FK-EXP", ""
	pen2.Items = []core.ItemBeli{{KodeBrng: "B000002", Jumlah: 10, Harga: 100, Kadaluarsa: "2026-11-01"}}
	if err := txErr(d, func(x *sql.Tx) error { _, err := core.TerimaBarang(ctx, x, pen2); return err }); err == nil || !strings.Contains(err.Error(), "kedaluwarsa") {
		t.Fatalf("barang kedaluwarsa: %v", err)
	}
	if st := q1[string](t, d, "SELECT status FROM surat_pemesanan_medis WHERE no_pemesanan=?", sp); st != "Sudah Datang" {
		t.Fatal("status SP")
	}
	if s := q1[float64](t, d, "SELECT stok FROM gudangbarang WHERE kode_brng='B000001' AND kd_bangsal='GF'"); s != 1000 {
		t.Fatalf("stok gudang %v", s)
	}
	if hb := q1[float64](t, d, "SELECT h_beli FROM databarang WHERE kode_brng='B000001'"); hb != 180 {
		t.Fatalf("h_beli baru %v", hb)
	}
	if v := saldo(t, d, "211010"); v != 376300 {
		t.Fatalf("hutang %v", v)
	}
	if v := saldo(t, d, "114010"); v != 340000 {
		t.Fatalf("persediaan %v", v)
	}

	// ---- mutasi gudang -> apotek
	if err := txErr(d, func(x *sql.Tx) error {
		return core.MutasiBarang(ctx, x, "GF", "AP", "", "apt", []core.ItemMutasi{{"B000006", 600}})
	}); err == nil || !strings.Contains(err.Error(), "tidak cukup") {
		t.Fatalf("mutasi melebihi stok: %v", err)
	}
	tx(t, d, func(x *sql.Tx) error {
		return core.MutasiBarang(ctx, x, "GF", "AP", "permintaan depo", "apt", []core.ItemMutasi{{"B000006", 200}, {"B000001", 300}})
	})
	if s := q1[float64](t, d, "SELECT stok FROM gudangbarang WHERE kode_brng='B000006' AND kd_bangsal='AP'"); s != 1200 {
		t.Fatalf("stok AP %v", s)
	}
	if n := q1[int](t, d, "SELECT COUNT(*) FROM riwayat_barang_medis WHERE posisi='Mutasi'"); n != 4 {
		t.Fatalf("kartu stok mutasi %d", n)
	}

	// ---- bayar hutang dicicil
	tx(t, d, func(x *sql.Tx) error {
		_, err := core.BayarHutang(ctx, x, "FK-001", 200000, "Bank", "TRF-1", "", "keu")
		return err
	})
	if err := txErr(d, func(x *sql.Tx) error {
		_, err := core.BayarHutang(ctx, x, "FK-001", 200000, "Bank", "", "", "keu")
		return err
	}); err == nil {
		t.Fatal("bayar melebihi sisa seharusnya ditolak")
	}
	tx(t, d, func(x *sql.Tx) error {
		_, err := core.BayarHutang(ctx, x, "FK-001", 176300, "Kas", "", "", "keu")
		return err
	})
	if st := q1[string](t, d, "SELECT status FROM pemesanan WHERE no_faktur='FK-001'"); st != "Sudah Dibayar" {
		t.Fatal(st)
	}
	if v := saldo(t, d, "211010"); v != 0 {
		t.Fatalf("hutang setelah lunas %v", v)
	}

	// ---- opname: sistem AP B000009 = 80, fisik 75 (h_beli 6000) -> selisih -5 = 30.000
	var sel float64
	tx(t, d, func(x *sql.Tx) (err error) {
		sel, err = core.StokOpname(ctx, x, "B000009", "AP", 75, "rusak", "apt")
		return
	})
	if sel != -5 || saldo(t, d, "520300") != 30000 {
		t.Fatalf("opname %v selisih akun %v", sel, saldo(t, d, "520300"))
	}

	// ---- kasir: pasien umum (kas) & BPJS (piutang), HPP saat validasi resep
	pu := &core.Pasien{Nama: "Umum", JK: "L", TglLahir: "1980-01-01"}
	pb := &core.Pasien{Nama: "Bpjs", JK: "P", TglLahir: "1985-01-01", KdPJ: "BPJ", NoPeserta: "0001234567890"}
	var ru, rb core.RegResult
	tx(t, d, func(x *sql.Tx) (err error) {
		core.SimpanPasien(ctx, x, pu)
		core.SimpanPasien(ctx, x, pb)
		if ru, err = core.Registrasi(ctx, x, core.RegInput{NoRM: pu.NoRM, KdPoli: "U0001", KdDokter: "D0000001", KdPJ: "A09"}); err != nil {
			return
		}
		rb, err = core.Registrasi(ctx, x, core.RegInput{NoRM: pb.NoRM, KdPoli: "INT", KdDokter: "D0000002", KdPJ: "BPJ"})
		return
	})
	for _, no := range []string{ru.NoRawat, rb.NoRawat} {
		no := no
		tx(t, d, func(x *sql.Tx) error { return core.TambahTindakan(ctx, x, no, "RJ002", "D0000001", false) })
		var nr string
		tx(t, d, func(x *sql.Tx) (err error) {
			nr, err = core.BuatResep(ctx, x, no, "D0000001", []core.ItemResep{{KodeBrng: "B000001", Jml: 10, Aturan: "3x1"}})
			return
		})
		tx(t, d, func(x *sql.Tx) error { return core.ValidasiResep(ctx, x, nr, "apt") })
	}
	// HPP 2 x 10 x 180
	if v := saldo(t, d, "510100"); v != 3600 {
		t.Fatalf("HPP %v", v)
	}
	kasAwal := saldo(t, d, "111010")
	tx(t, d, func(x *sql.Tx) error { _, err := core.BayarBilling(ctx, x, ru.NoRawat); return err })
	tx(t, d, func(x *sql.Tx) error { _, err := core.BayarBilling(ctx, x, rb.NoRawat); return err })
	// umum: reg 15.000 + 75.000 + obat 10 x 500 = 95.000 ke kas
	if v := saldo(t, d, "111010") - kasAwal; v != 95000 {
		t.Fatalf("kas masuk %v", v)
	}
	// BPJS: reg 30.000 + 75.000 + 5.000 = 110.000 ke piutang
	if v := saldo(t, d, "113010"); v != 110000 {
		t.Fatalf("piutang %v", v)
	}
	if v := saldo(t, d, "410700"); v != 10000 {
		t.Fatalf("pendapatan obat %v", v)
	}
	if st := q1[string](t, d, "SELECT status FROM piutang_pasien WHERE no_rawat=?", rb.NoRawat); st != "Belum Lunas" {
		t.Fatal("piutang belum tercatat")
	}
	// batal bayar umum -> jurnal pembalik
	tx(t, d, func(x *sql.Tx) error { return core.BatalBayar(ctx, x, ru.NoRawat) })
	if v := saldo(t, d, "111010") - kasAwal; v != 0 {
		t.Fatalf("kas setelah batal %v", v)
	}
	tx(t, d, func(x *sql.Tx) error { _, err := core.BayarBilling(ctx, x, ru.NoRawat); return err })

	// ---- pembayaran klaim BPJS: 100.000 diterima, 10.000 tidak dibayar (selisih klaim)
	if err := txErr(d, func(x *sql.Tx) error {
		_, err := core.BayarPiutang(ctx, x, core.BayarPiutangInput{NoRawat: rb.NoRawat, Cicilan: 120000, Via: "Bank"})
		return err
	}); err == nil {
		t.Fatal("bayar melebihi piutang seharusnya ditolak")
	}
	tx(t, d, func(x *sql.Tx) error {
		_, err := core.BayarPiutang(ctx, x, core.BayarPiutangInput{NoRawat: rb.NoRawat, Cicilan: 60000, Via: "Bank", Catatan: "klaim tahap 1"})
		return err
	})
	if err := txErr(d, func(x *sql.Tx) error { return core.BatalBayar(ctx, x, rb.NoRawat) }); err == nil {
		t.Fatal("batal bayar setelah dicicil seharusnya ditolak")
	}
	var sisa float64
	tx(t, d, func(x *sql.Tx) (err error) {
		sisa, err = core.BayarPiutang(ctx, x, core.BayarPiutangInput{NoRawat: rb.NoRawat, Cicilan: 40000, Hapus: 10000, Via: "Bank"})
		return
	})
	if sisa != 0 || q1[string](t, d, "SELECT status FROM piutang_pasien WHERE no_rawat=?", rb.NoRawat) != "Lunas" {
		t.Fatal("piutang belum lunas")
	}
	if saldo(t, d, "113010") != 0 || saldo(t, d, "520200") != 10000 {
		t.Fatal("akun piutang/hapus buku")
	}

	// ---- jurnal manual & keseimbangan total
	if err := txErr(d, func(x *sql.Tx) error {
		_, err := core.JurnalManual(ctx, x, "BKK-1", "listrik", []core.Baris{{KdRek: "530100", Debet: 500000}, {KdRek: "111010", Kredit: 400000}})
		return err
	}); err == nil {
		t.Fatal("jurnal tidak seimbang seharusnya ditolak")
	}
	tx(t, d, func(x *sql.Tx) error {
		_, err := core.JurnalManual(ctx, x, "BKK-1", "Bayar listrik November", []core.Baris{{KdRek: "530100", Debet: 500000}, {KdRek: "111020", Kredit: 500000}})
		return err
	})
	cekSeimbang(t, d)
	ns, _ := core.NeracaSaldo(ctx, d, "2026-11-01", "2026-11-30")
	var td, tk float64
	for _, s := range ns {
		td += s.Debet
		tk += s.Kredit
	}
	if td != tk {
		t.Fatalf("neraca saldo tidak seimbang %v %v", td, tk)
	}
	// akun R tidak terbawa ke periode berikutnya, akun N terbawa
	ns, _ = core.NeracaSaldo(ctx, d, "2026-12-01", "2026-12-31")
	for _, s := range ns {
		if s.Tipe == "R" && (s.Debet != 0 || s.Kredit != 0) {
			t.Fatalf("akun R %s terbawa ke periode baru", s.KdRek)
		}
		// bank: -200.000 (bayar hutang) + 60.000 + 40.000 (klaim) - 500.000 (listrik)
		if s.KdRek == "111020" && s.Saldo != -600000 {
			t.Fatalf("saldo bank kumulatif %v", s.Saldo)
		}
	}
}
