package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ================================================================ LOGISTIK FARMASI
// Alur Khanza: Surat Pemesanan (surat_pemesanan_medis) -> Penerimaan faktur (pemesanan/detailpesan,
// stok gudang bertambah, hutang supplier) -> Mutasi gudang -> depo (mutasibarang) -> Stok opname (opname)
// -> Bayar hutang (bayar_pemesanan). Kartu stok = riwayat_barang_medis.

const Gudang = "GF"

// ubahStok menambah (jml>0) / mengurangi (jml<0) stok bangsal dan mencatat kartu stok.
func ubahStok(ctx context.Context, tx *sql.Tx, kode, bangsal string, jml float64, posisi, petugas, ket string) (float64, error) {
	var awal float64
	err := tx.QueryRowContext(ctx, "SELECT stok FROM gudangbarang WHERE kode_brng=? AND kd_bangsal=? AND no_batch='' AND no_faktur='' FOR UPDATE", kode, bangsal).Scan(&awal)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err = tx.ExecContext(ctx, "INSERT INTO gudangbarang (kode_brng,kd_bangsal,stok) VALUES (?,?,0)", kode, bangsal); err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	if awal+jml < -0.0001 {
		nama, _ := one[string](ctx, tx, "SELECT nama_brng FROM databarang WHERE kode_brng=?", kode)
		return 0, fmt.Errorf("stok %s di %s tidak cukup (sisa %g, butuh %g)", nama, bangsal, awal, -jml)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE gudangbarang SET stok=stok+? WHERE kode_brng=? AND kd_bangsal=? AND no_batch='' AND no_faktur=''", jml, kode, bangsal); err != nil {
		return 0, err
	}
	masuk, keluar := jml, 0.0
	if jml < 0 {
		masuk, keluar = 0, -jml
	}
	if len(ket) > 100 {
		ket = ket[:100]
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO riwayat_barang_medis (kode_brng,stok_awal,masuk,keluar,stok_akhir,posisi,tanggal,jam,petugas,kd_bangsal,keterangan) VALUES (?,?,?,?,?,?,?,?,?,?,?)",
		kode, awal, masuk, keluar, awal+jml, posisi, Today(), NowTime(), petugas, bangsal, ket)
	return awal + jml, err
}

func cekBangsal(ctx context.Context, tx *sql.Tx, kd string) error {
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM bangsal WHERE kd_bangsal=?", kd); n == 0 {
		return fmt.Errorf("lokasi/depo %s tidak dikenal", kd)
	}
	return nil
}

type ItemBeli struct {
	KodeBrng   string
	Jumlah     float64
	Harga      float64 // harga per satuan sebelum diskon
	Diskon     float64 // persen
	NoBatch    string
	Kadaluarsa string // yyyy-mm-dd, opsional
}

type hitungBeli struct {
	subtotal, besardis, total float64
}

func hitungItem(it ItemBeli) (hitungBeli, error) {
	if it.Jumlah <= 0 {
		return hitungBeli{}, fmt.Errorf("jumlah %s harus > 0", it.KodeBrng)
	}
	if it.Harga < 0 || it.Diskon < 0 || it.Diskon > 100 {
		return hitungBeli{}, fmt.Errorf("harga/diskon %s tidak valid", it.KodeBrng)
	}
	sub := r2(it.Jumlah * it.Harga)
	dis := r2(sub * it.Diskon / 100)
	return hitungBeli{sub, dis, r2(sub - dis)}, nil
}

type Faktur struct {
	Total1, Potongan, Total2, PPN, Meterai, Tagihan float64
}

func hitungFaktur(items []ItemBeli, ppnPersen, meterai float64) (Faktur, []hitungBeli, error) {
	var f Faktur
	var hs []hitungBeli
	if len(items) == 0 {
		return f, nil, errors.New("isi minimal satu barang")
	}
	if ppnPersen < 0 || ppnPersen > 100 || meterai < 0 {
		return f, nil, errors.New("PPN/meterai tidak valid")
	}
	for _, it := range items {
		h, err := hitungItem(it)
		if err != nil {
			return f, nil, err
		}
		hs = append(hs, h)
		f.Total1 += h.subtotal
		f.Potongan += h.besardis
	}
	f.Total1, f.Potongan = r2(f.Total1), r2(f.Potongan)
	f.Total2 = r2(f.Total1 - f.Potongan)
	f.PPN = r2(f.Total2 * ppnPersen / 100)
	f.Meterai = r2(meterai)
	f.Tagihan = r2(f.Total2 + f.PPN + f.Meterai)
	return f, hs, nil
}

func cekBarang(ctx context.Context, tx *sql.Tx, kode string) (string, error) {
	sat, err := one[string](ctx, tx, "SELECT kode_sat FROM databarang WHERE kode_brng=?", kode)
	if err != nil {
		return "", fmt.Errorf("barang %s tidak ditemukan", kode)
	}
	return sat, nil
}

// ---------------------------------------------------------------- surat pemesanan (SP)

func NoSP(ctx context.Context, tx *sql.Tx) (string, error) {
	return seqDaily(ctx, tx, "surat_pemesanan_medis", "no_pemesanan", "SPM"+Now().Format("20060102"), 3)
}

func BuatSP(ctx context.Context, tx *sql.Tx, supplier, petugas string, items []ItemBeli, ppnPersen float64) (string, error) {
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM datasuplier WHERE kode_suplier=?", supplier); n == 0 {
		return "", errors.New("supplier tidak ditemukan")
	}
	f, hs, err := hitungFaktur(items, ppnPersen, 0)
	if err != nil {
		return "", err
	}
	no, err := NoSP(ctx, tx)
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO surat_pemesanan_medis VALUES (?,?,?,?,?,?,?,?,?,?,'Proses Pesan')", no, supplier, petugas, Today(), f.Total1, f.Potongan, f.Total2, f.PPN, 0, f.Tagihan); err != nil {
		return "", err
	}
	for i, it := range items {
		sat, err := cekBarang(ctx, tx, it.KodeBrng)
		if err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO detail_surat_pemesanan_medis VALUES (?,?,?,?,?,?,?,?,?,?)", no, it.KodeBrng, sat, it.Jumlah, it.Harga, hs[i].subtotal, it.Diskon, hs[i].besardis, hs[i].total, it.Jumlah); err != nil {
			if strings.Contains(err.Error(), "Duplicate") {
				return "", fmt.Errorf("barang %s tercantum dua kali", it.KodeBrng)
			}
			return "", err
		}
	}
	return no, nil
}

// ---------------------------------------------------------------- penerimaan faktur

type Penerimaan struct {
	NoFaktur, NoOrder, Supplier, TglFaktur, TglTempo, Bangsal, Petugas string
	PPNPersen, Meterai                                                 float64
	Items                                                              []ItemBeli
	UpdateHargaBeli                                                    bool
}

func TerimaBarang(ctx context.Context, tx *sql.Tx, p Penerimaan) (Faktur, error) {
	p.NoFaktur = strings.TrimSpace(p.NoFaktur)
	if p.NoFaktur == "" {
		return Faktur{}, errors.New("nomor faktur supplier wajib diisi")
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM pemesanan WHERE no_faktur=?", p.NoFaktur); n > 0 {
		return Faktur{}, errors.New("nomor faktur sudah pernah diterima")
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM datasuplier WHERE kode_suplier=?", p.Supplier); n == 0 {
		return Faktur{}, errors.New("supplier tidak ditemukan")
	}
	if p.Bangsal == "" {
		p.Bangsal = Gudang
	}
	if err := cekBangsal(ctx, tx, p.Bangsal); err != nil {
		return Faktur{}, err
	}
	if p.TglFaktur == "" {
		p.TglFaktur = Today()
	}
	tf, err1 := time.Parse("2006-01-02", p.TglFaktur)
	tt, err2 := time.Parse("2006-01-02", p.TglTempo)
	if err1 != nil || err2 != nil {
		return Faktur{}, errors.New("tanggal faktur/jatuh tempo tidak valid")
	}
	if tt.Before(tf) {
		return Faktur{}, errors.New("jatuh tempo tidak boleh sebelum tanggal faktur")
	}
	tglPesan := p.TglFaktur
	if p.NoOrder != "" {
		var st, sup, tgl string
		if err := tx.QueryRowContext(ctx, "SELECT status,kode_suplier,tanggal FROM surat_pemesanan_medis WHERE no_pemesanan=? FOR UPDATE", p.NoOrder).Scan(&st, &sup, &tgl); err != nil {
			return Faktur{}, errors.New("surat pemesanan tidak ditemukan")
		}
		if st != "Proses Pesan" {
			return Faktur{}, errors.New("surat pemesanan sudah diterima")
		}
		if sup != p.Supplier {
			return Faktur{}, errors.New("supplier faktur berbeda dengan surat pemesanan")
		}
		tglPesan = tgl
	}
	f, hs, err := hitungFaktur(p.Items, p.PPNPersen, p.Meterai)
	if err != nil {
		return f, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO pemesanan VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,'Belum Dibayar')",
		p.NoFaktur, p.NoOrder, p.Supplier, p.Petugas, tglPesan, p.TglFaktur, p.TglTempo, f.Total1, f.Potongan, f.Total2, f.PPN, f.Meterai, f.Tagihan, p.Bangsal); err != nil {
		return f, err
	}
	for i, it := range p.Items {
		sat, err := cekBarang(ctx, tx, it.KodeBrng)
		if err != nil {
			return f, err
		}
		var kd any
		if it.Kadaluarsa != "" {
			k, err := time.Parse("2006-01-02", it.Kadaluarsa)
			if err != nil {
				return f, fmt.Errorf("tanggal kedaluwarsa %s tidak valid", it.KodeBrng)
			}
			if !k.After(tf) {
				return f, fmt.Errorf("barang %s sudah kedaluwarsa saat diterima", it.KodeBrng)
			}
			kd = it.Kadaluarsa
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO detailpesan VALUES (?,?,?,?,?,?,?,?,?,?,?,?)", p.NoFaktur, it.KodeBrng, sat, it.Jumlah, it.Harga, hs[i].subtotal, it.Diskon, hs[i].besardis, hs[i].total, strings.TrimSpace(it.NoBatch), it.Jumlah, kd); err != nil {
			if strings.Contains(err.Error(), "Duplicate") {
				return f, fmt.Errorf("barang %s dengan batch sama tercantum dua kali", it.KodeBrng)
			}
			return f, err
		}
		if _, err := ubahStok(ctx, tx, it.KodeBrng, p.Bangsal, it.Jumlah, "Penerimaan", p.Petugas, "Faktur "+p.NoFaktur); err != nil {
			return f, err
		}
		if p.UpdateHargaBeli {
			if _, err := tx.ExecContext(ctx, "UPDATE databarang SET h_beli=? WHERE kode_brng=?", r2(hs[i].total/it.Jumlah), it.KodeBrng); err != nil {
				return f, err
			}
		}
	}
	if p.NoOrder != "" {
		if _, err := tx.ExecContext(ctx, "UPDATE surat_pemesanan_medis SET status='Sudah Datang' WHERE no_pemesanan=?", p.NoOrder); err != nil {
			return f, err
		}
	}
	ak, err := mustAkun(ctx, tx, "Persediaan_Obat", "PPN_Masukan", "Hutang_Obat")
	if err != nil {
		return f, err
	}
	_, err = PostJurnal(ctx, tx, p.NoFaktur, "U", "Penerimaan obat faktur "+p.NoFaktur, []Baris{
		{KdRek: ak["Persediaan_Obat"], Debet: f.Total2 + f.Meterai}, {KdRek: ak["PPN_Masukan"], Debet: f.PPN}, {KdRek: ak["Hutang_Obat"], Kredit: f.Tagihan}})
	return f, err
}

// BayarHutang mencatat pembayaran ke supplier (boleh dicicil).
func BayarHutang(ctx context.Context, tx *sql.Tx, noFaktur string, besar float64, via, noBukti, ket, petugas string) (float64, error) {
	var tagihan float64
	var st string
	if err := tx.QueryRowContext(ctx, "SELECT tagihan,status FROM pemesanan WHERE no_faktur=? FOR UPDATE", noFaktur).Scan(&tagihan, &st); err != nil {
		return 0, errors.New("faktur tidak ditemukan")
	}
	if st == "Sudah Dibayar" {
		return 0, errors.New("faktur sudah lunas")
	}
	besar = r2(besar)
	if besar <= 0 {
		return 0, errors.New("besar bayar harus > 0")
	}
	dibayar, _ := one[float64](ctx, tx, "SELECT IFNULL(SUM(besar_bayar),0) FROM bayar_pemesanan WHERE no_faktur=?", noFaktur)
	sisa := r2(tagihan - dibayar)
	if besar > sisa+0.004 {
		return 0, fmt.Errorf("pembayaran %s melebihi sisa hutang %s", rp(besar), rp(sisa))
	}
	if via != "Bank" {
		via = "Kas"
	}
	ak, err := mustAkun(ctx, tx, via, "Hutang_Obat")
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO bayar_pemesanan VALUES (?,?,?,?,?,?,?)", Today(), noFaktur, petugas, besar, ket, via, noBukti); err != nil {
		return 0, err
	}
	sisa = r2(sisa - besar)
	st = "Belum Lunas"
	if sisa <= 0.004 {
		sisa, st = 0, "Sudah Dibayar"
	}
	if _, err := tx.ExecContext(ctx, "UPDATE pemesanan SET status=? WHERE no_faktur=?", st, noFaktur); err != nil {
		return 0, err
	}
	_, err = PostJurnal(ctx, tx, noFaktur, "U", "Bayar hutang obat faktur "+noFaktur+" "+noBukti, []Baris{{KdRek: ak["Hutang_Obat"], Debet: besar}, {KdRek: ak[via], Kredit: besar}})
	return sisa, err
}

// ---------------------------------------------------------------- mutasi & opname

type ItemMutasi struct {
	KodeBrng string
	Jumlah   float64
}

func MutasiBarang(ctx context.Context, tx *sql.Tx, dari, ke, ket, petugas string, items []ItemMutasi) error {
	if dari == ke {
		return errors.New("lokasi asal dan tujuan sama")
	}
	for _, b := range []string{dari, ke} {
		if err := cekBangsal(ctx, tx, b); err != nil {
			return err
		}
	}
	if len(items) == 0 {
		return errors.New("isi minimal satu barang")
	}
	now := Now().Format("2006-01-02 15:04:05")
	for _, it := range items {
		if it.Jumlah <= 0 {
			return fmt.Errorf("jumlah %s harus > 0", it.KodeBrng)
		}
		hb, err := one[float64](ctx, tx, "SELECT h_beli FROM databarang WHERE kode_brng=?", it.KodeBrng)
		if err != nil {
			return fmt.Errorf("barang %s tidak ditemukan", it.KodeBrng)
		}
		if _, err := ubahStok(ctx, tx, it.KodeBrng, dari, -it.Jumlah, "Mutasi", petugas, "Mutasi ke "+ke+" "+ket); err != nil {
			return err
		}
		if _, err := ubahStok(ctx, tx, it.KodeBrng, ke, it.Jumlah, "Mutasi", petugas, "Mutasi dari "+dari+" "+ket); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO mutasibarang VALUES (?,?,?,?,?,?,?,'','')", it.KodeBrng, it.Jumlah, hb, dari, ke, now, ket); err != nil {
			return err
		}
	}
	return nil
}

// StokOpname: input hasil hitung fisik; selisih dicatat di tabel opname, kartu stok, dan jurnal.
func StokOpname(ctx context.Context, tx *sql.Tx, kode, bangsal string, real float64, ket, petugas string) (float64, error) {
	if real < 0 {
		return 0, errors.New("stok fisik tidak boleh negatif")
	}
	if err := cekBangsal(ctx, tx, bangsal); err != nil {
		return 0, err
	}
	hb, err := one[float64](ctx, tx, "SELECT h_beli FROM databarang WHERE kode_brng=?", kode)
	if err != nil {
		return 0, errors.New("barang tidak ditemukan")
	}
	var sistem float64
	err = tx.QueryRowContext(ctx, "SELECT stok FROM gudangbarang WHERE kode_brng=? AND kd_bangsal=? AND no_batch='' AND no_faktur='' FOR UPDATE", kode, bangsal).Scan(&sistem)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	sel := r2(real - sistem)
	hilang, lebih := 0.0, 0.0
	if sel < 0 {
		hilang = -sel
	} else {
		lebih = sel
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO opname VALUES (?,?,?,?,?,?,?,?,?,?,?,'','') ON DUPLICATE KEY UPDATE stok=VALUES(stok), `real`=VALUES(`real`), selisih=VALUES(selisih), nomihilang=VALUES(nomihilang), lebih=VALUES(lebih), nomilebih=VALUES(nomilebih), keterangan=VALUES(keterangan)",
		kode, hb, Today(), sistem, real, sel, r2(hilang*hb), lebih, r2(lebih*hb), ket, bangsal); err != nil {
		return 0, err
	}
	if sel == 0 {
		return 0, nil
	}
	if _, err := ubahStok(ctx, tx, kode, bangsal, sel, "Opname", petugas, "Opname: "+ket); err != nil {
		return 0, err
	}
	ak, err := mustAkun(ctx, tx, "Persediaan_Obat", "Selisih_Opname")
	if err != nil {
		return 0, err
	}
	nilai := r2(abs(sel) * hb)
	lines := []Baris{{KdRek: ak["Selisih_Opname"], Debet: nilai}, {KdRek: ak["Persediaan_Obat"], Kredit: nilai}}
	if sel > 0 {
		lines = []Baris{{KdRek: ak["Persediaan_Obat"], Debet: nilai}, {KdRek: ak["Selisih_Opname"], Kredit: nilai}}
	}
	_, err = PostJurnal(ctx, tx, "OPN-"+kode, "U", fmt.Sprintf("Selisih opname %s di %s (%+g)", kode, bangsal, sel), lines)
	return sel, err
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
