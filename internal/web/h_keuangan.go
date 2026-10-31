package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/yogaprasetia/simrs-go/internal/core"
)

var (
	rLogistik = []string{"farmasi"}
	rKeu      = []string{"keuangan"}
	rKeuLihat = []string{"keuangan", "manajemen"}
)

func (s *Server) routesKeuangan() {
	m, p := s.mux, s.Prefix
	m.HandleFunc("GET "+p+"/logistik", s.guard(rLogistik, s.logPage))
	m.HandleFunc("GET "+p+"/logistik/faktur", s.guard([]string{"farmasi", "keuangan"}, s.logFaktur))
	m.HandleFunc("GET "+p+"/logistik/sp", s.guard(rLogistik, s.logSP))
	m.HandleFunc("POST "+p+"/logistik/{aksi}", s.guard(rLogistik, s.logAksi))
	m.HandleFunc("GET "+p+"/keuangan", s.guard(rKeuLihat, s.keuPage))
	m.HandleFunc("POST "+p+"/keuangan/{aksi}", s.guard(rKeu, s.keuAksi))
}

func kode(v string) string { return strings.TrimSpace(strings.SplitN(v, " ", 2)[0]) }

// pfx: angka dengan desimal titik (input type=number).
func pfx(v string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
	return f
}

func itemsBeli(r *http.Request) []core.ItemBeli {
	var out []core.ItemBeli
	k := r.Form["kode_brng"]
	for i := range k {
		kd := kode(k[i])
		if kd == "" {
			continue
		}
		out = append(out, core.ItemBeli{KodeBrng: kd, Jumlah: pfx(get(r.Form["jumlah"], i)), Harga: pfx(get(r.Form["harga"], i)), Diskon: pfx(get(r.Form["dis"], i)),
			NoBatch: get(r.Form["no_batch"], i), Kadaluarsa: get(r.Form["kadaluarsa"], i)})
	}
	return out
}

// ------------------------------------------------------------------ logistik

func (s *Server) logPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	tab := q.Get("tab")
	if tab == "" {
		tab = "penerimaan"
	}
	dari, sampai := q.Get("dari"), q.Get("sampai")
	if dari == "" {
		dari = core.Now().AddDate(0, 0, -30).Format("2006-01-02")
	}
	if sampai == "" {
		sampai = core.Today()
	}
	d := map[string]any{"Title": "Logistik Farmasi", "Tab": tab, "Dari": dari, "Sampai": sampai,
		"Suplier": s.rows(ctx, "SELECT * FROM datasuplier ORDER BY nama_suplier"),
		"Bangsal": s.rows(ctx, "SELECT kd_bangsal, nm_bangsal FROM bangsal WHERE kd_bangsal IN ('GF','AP') OR kd_bangsal IN (SELECT DISTINCT kd_bangsal FROM gudangbarang) ORDER BY kd_bangsal='GF' DESC, kd_bangsal"),
		"Rows":    []int{1, 2, 3, 4, 5, 6}, "Tempo": core.Now().AddDate(0, 0, 30).Format("2006-01-02")}
	switch tab {
	case "sp":
		d["SP"] = s.rows(ctx, `SELECT s.*, d.nama_suplier, (SELECT COUNT(*) FROM detail_surat_pemesanan_medis x WHERE x.no_pemesanan=s.no_pemesanan) item FROM surat_pemesanan_medis s JOIN datasuplier d ON d.kode_suplier=s.kode_suplier
			WHERE s.tanggal BETWEEN ? AND ? OR s.status='Proses Pesan' ORDER BY s.tanggal DESC, s.no_pemesanan DESC`, dari, sampai)
	case "penerimaan":
		d["Faktur"] = s.rows(ctx, `SELECT p.*, d.nama_suplier, b.nm_bangsal, IFNULL((SELECT SUM(besar_bayar) FROM bayar_pemesanan x WHERE x.no_faktur=p.no_faktur),0) dibayar
			FROM pemesanan p JOIN datasuplier d ON d.kode_suplier=p.kode_suplier JOIN bangsal b ON b.kd_bangsal=p.kd_bangsal WHERE p.tgl_faktur BETWEEN ? AND ? ORDER BY p.tgl_faktur DESC`, dari, sampai)
		d["SPOpen"] = s.rows(ctx, "SELECT s.no_pemesanan, s.kode_suplier, d.nama_suplier FROM surat_pemesanan_medis s JOIN datasuplier d ON d.kode_suplier=s.kode_suplier WHERE s.status='Proses Pesan' ORDER BY s.no_pemesanan")
		if no := q.Get("sp"); no != "" {
			d["SPSel"] = s.row(ctx, "SELECT * FROM surat_pemesanan_medis WHERE no_pemesanan=? AND status='Proses Pesan'", no)
			d["SPItems"] = s.rows(ctx, "SELECT x.*, b.nama_brng FROM detail_surat_pemesanan_medis x JOIN databarang b ON b.kode_brng=x.kode_brng WHERE no_pemesanan=?", no)
		}
	case "mutasi":
		d["Mutasi"] = s.rows(ctx, `SELECT m.*, b.nama_brng, b.kode_sat FROM mutasibarang m JOIN databarang b ON b.kode_brng=m.kode_brng WHERE DATE(m.tanggal) BETWEEN ? AND ? ORDER BY m.tanggal DESC LIMIT 200`, dari, sampai)
		d["Stok"] = s.rows(ctx, `SELECT b.kode_brng, b.nama_brng, b.kode_sat, b.stokminimal,
			IFNULL(SUM(IF(g.kd_bangsal='GF',g.stok,0)),0) gf, IFNULL(SUM(IF(g.kd_bangsal='AP',g.stok,0)),0) ap
			FROM databarang b LEFT JOIN gudangbarang g ON g.kode_brng=b.kode_brng WHERE b.status='1' GROUP BY b.kode_brng HAVING ap<=b.stokminimal ORDER BY b.nama_brng`)
	case "opname":
		d["Opname"] = s.rows(ctx, `SELECT o.*, b.nama_brng, b.kode_sat, bg.nm_bangsal FROM opname o JOIN databarang b ON b.kode_brng=o.kode_brng JOIN bangsal bg ON bg.kd_bangsal=o.kd_bangsal WHERE o.tanggal BETWEEN ? AND ? ORDER BY o.tanggal DESC`, dari, sampai)
	case "kartu":
		kd, bg := kode(q.Get("kode_brng")), q.Get("bangsal")
		d["KodeSel"], d["BangsalSel"], d["KodeIn"] = kd, bg, q.Get("kode_brng")
		if kd != "" {
			where, args := "r.kode_brng=? AND r.tanggal BETWEEN ? AND ?", []any{kd, dari, sampai}
			if bg != "" {
				where += " AND r.kd_bangsal=?"
				args = append(args, bg)
			}
			d["Barang"] = s.row(ctx, "SELECT * FROM databarang WHERE kode_brng=?", kd)
			d["Kartu"] = s.rows(ctx, "SELECT r.*, bg.nm_bangsal FROM riwayat_barang_medis r JOIN bangsal bg ON bg.kd_bangsal=r.kd_bangsal WHERE "+where+" ORDER BY r.tanggal, r.jam", args...)
		}
	case "ed":
		d["ED"] = s.rows(ctx, `SELECT x.kode_brng, b.nama_brng, x.no_batch, x.kadaluarsa, x.jumlah, p.no_faktur, p.tgl_faktur, DATEDIFF(x.kadaluarsa, CURDATE()) sisa_hari,
			IFNULL((SELECT SUM(stok) FROM gudangbarang g WHERE g.kode_brng=x.kode_brng),0) stok
			FROM detailpesan x JOIN pemesanan p ON p.no_faktur=x.no_faktur JOIN databarang b ON b.kode_brng=x.kode_brng
			WHERE x.kadaluarsa IS NOT NULL AND x.kadaluarsa <= DATE_ADD(CURDATE(), INTERVAL 180 DAY) ORDER BY x.kadaluarsa`)
	case "suplier":
	}
	s.render(w, r, "logistik", d, 200)
}

func (s *Server) logFaktur(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	no := r.URL.Query().Get("no")
	f := s.row(ctx, "SELECT p.*, d.nama_suplier, d.alamat, d.kota, d.nama_bank, d.rekening, b.nm_bangsal FROM pemesanan p JOIN datasuplier d ON d.kode_suplier=p.kode_suplier JOIN bangsal b ON b.kd_bangsal=p.kd_bangsal WHERE p.no_faktur=?", no)
	if f == nil {
		s.back(w, r, s.Prefix+"/logistik", errors.New("faktur tidak ditemukan"), "")
		return
	}
	s.render(w, r, "logistik_faktur", map[string]any{"Title": "Faktur " + no, "F": f,
		"Items": s.rows(ctx, "SELECT x.*, b.nama_brng FROM detailpesan x JOIN databarang b ON b.kode_brng=x.kode_brng WHERE x.no_faktur=?", no),
		"Bayar": s.rows(ctx, "SELECT * FROM bayar_pemesanan WHERE no_faktur=? ORDER BY tgl_bayar", no)}, 200)
}

func (s *Server) logSP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	no := r.URL.Query().Get("no")
	sp := s.row(ctx, "SELECT s.*, d.nama_suplier, d.alamat, d.kota FROM surat_pemesanan_medis s JOIN datasuplier d ON d.kode_suplier=s.kode_suplier WHERE s.no_pemesanan=?", no)
	if sp == nil {
		s.fail(w, r, errors.New("surat pemesanan tidak ditemukan"))
		return
	}
	s.render(w, r, "print_sp", map[string]any{"Title": "Surat Pemesanan " + no, "S": sp, "Print": true,
		"Items": s.rows(ctx, "SELECT x.*, b.nama_brng FROM detail_surat_pemesanan_medis x JOIN databarang b ON b.kode_brng=x.kode_brng WHERE x.no_pemesanan=?", no)}, 200)
}

func (s *Server) logAksi(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.ParseForm()
	f := r.FormValue
	u := userOf(r)
	tab := map[string]string{"sp": "sp", "terima": "penerimaan", "mutasi": "mutasi", "opname": "opname", "suplier": "suplier"}[r.PathValue("aksi")]
	to := s.Prefix + "/logistik?tab=" + tab
	var err error
	var msg string
	switch r.PathValue("aksi") {
	case "sp":
		var no string
		err = s.tx(r, func(tx *sql.Tx) (e error) {
			no, e = core.BuatSP(ctx, tx, f("kode_suplier"), u.ID, itemsBeli(r), pfx(f("ppn")))
			return
		})
		msg = "Surat pemesanan " + no + " dibuat"
	case "terima":
		var fk core.Faktur
		p := core.Penerimaan{NoFaktur: f("no_faktur"), NoOrder: f("no_order"), Supplier: f("kode_suplier"), TglFaktur: f("tgl_faktur"), TglTempo: f("tgl_tempo"),
			Bangsal: f("kd_bangsal"), Petugas: u.ID, PPNPersen: pfx(f("ppn")), Meterai: pfx(f("meterai")), Items: itemsBeli(r), UpdateHargaBeli: f("update_harga") == "1"}
		err = s.tx(r, func(tx *sql.Tx) (e error) { fk, e = core.TerimaBarang(ctx, tx, p); return })
		msg = "Faktur " + p.NoFaktur + " diterima, stok bertambah · tagihan " + rupiah(fk.Tagihan)
		if err == nil {
			to = s.Prefix + "/logistik/faktur?no=" + p.NoFaktur
		}
	case "mutasi":
		var items []core.ItemMutasi
		for i, k := range r.Form["kode_brng"] {
			if kd := kode(k); kd != "" {
				items = append(items, core.ItemMutasi{KodeBrng: kd, Jumlah: pfx(get(r.Form["jumlah"], i))})
			}
		}
		err = s.tx(r, func(tx *sql.Tx) error {
			return core.MutasiBarang(ctx, tx, f("dari"), f("ke"), f("keterangan"), u.ID, items)
		})
		msg = strconv.Itoa(len(items)) + " barang dimutasi dari " + f("dari") + " ke " + f("ke")
	case "opname":
		var sel float64
		err = s.tx(r, func(tx *sql.Tx) (e error) {
			sel, e = core.StokOpname(ctx, tx, kode(f("kode_brng")), f("kd_bangsal"), pfx(f("real")), f("keterangan"), u.ID)
			return
		})
		msg = "Opname tersimpan, selisih " + strconv.FormatFloat(sel, 'f', -1, 64)
	case "suplier":
		kd := strings.ToUpper(strings.TrimSpace(f("kode_suplier")))
		if len(kd) == 0 || len(kd) > 5 || strings.TrimSpace(f("nama_suplier")) == "" {
			err = errors.New("kode (maks. 5 karakter) dan nama supplier wajib diisi")
			break
		}
		_, err = s.DB.ExecContext(ctx, `INSERT INTO datasuplier VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE nama_suplier=VALUES(nama_suplier), alamat=VALUES(alamat), kota=VALUES(kota), no_telp=VALUES(no_telp), nama_bank=VALUES(nama_bank), rekening=VALUES(rekening)`,
			kd, f("nama_suplier"), f("alamat"), f("kota"), f("no_telp"), f("nama_bank"), f("rekening"))
		msg = "Supplier " + kd + " tersimpan"
	default:
		http.NotFound(w, r)
		return
	}
	if err == nil {
		s.audit(r, "", "logistik_"+r.PathValue("aksi"), f("no_faktur"), msg)
	}
	s.back(w, r, to, err, msg)
}

// ------------------------------------------------------------------ keuangan

func (s *Server) keuPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	tab := q.Get("tab")
	if tab == "" {
		tab = "piutang"
	}
	dari, sampai := q.Get("dari"), q.Get("sampai")
	now := core.Now()
	if dari == "" {
		dari = now.Format("2006-01") + "-01"
	}
	if sampai == "" {
		sampai = core.Today()
	}
	d := map[string]any{"Title": "Keuangan & Akuntansi", "Tab": tab, "Dari": dari, "Sampai": sampai,
		"Rek": s.rows(ctx, "SELECT * FROM rekening ORDER BY kd_rek"), "Rows": []int{1, 2, 3, 4}}
	switch tab {
	case "piutang":
		st := q.Get("st")
		if st == "" {
			st = "Belum Lunas"
		}
		d["St"] = st
		list := s.rows(ctx, `SELECT pp.*, p.nm_pasien, dp.nama_bayar, dp.kd_pj, DATEDIFF(CURDATE(), pp.tgl_piutang) umur, r.status_lanjut,
			(SELECT no_sep FROM bridging_sep b WHERE b.no_rawat=pp.no_rawat LIMIT 1) no_sep
			FROM piutang_pasien pp JOIN pasien p ON p.no_rkm_medis=pp.no_rkm_medis JOIN reg_periksa r ON r.no_rawat=pp.no_rawat LEFT JOIN detail_piutang_pasien dp ON dp.no_rawat=pp.no_rawat
			WHERE pp.status=? ORDER BY pp.tgl_piutang`, st)
		d["List"] = list
		aging := map[string]float64{}
		for _, x := range list {
			u := toF(x["umur"])
			k := "0–30"
			switch {
			case u > 90:
				k = ">90"
			case u > 60:
				k = "61–90"
			case u > 30:
				k = "31–60"
			}
			aging[k] += toF(x["sisapiutang"])
			aging["Total"] += toF(x["sisapiutang"])
		}
		d["Aging"] = aging
		d["PerPJ"] = s.rows(ctx, `SELECT dp.nama_bayar, COUNT(*) jml, SUM(pp.sisapiutang) sisa FROM piutang_pasien pp JOIN detail_piutang_pasien dp ON dp.no_rawat=pp.no_rawat WHERE pp.status='Belum Lunas' GROUP BY dp.nama_bayar ORDER BY sisa DESC`)
		if no := q.Get("no"); no != "" {
			d["Riwayat"] = s.rows(ctx, "SELECT * FROM bayar_piutang WHERE no_rawat=? ORDER BY tgl_bayar", no)
			d["NoSel"] = no
		}
	case "hutang":
		d["List"] = s.rows(ctx, `SELECT p.*, d.nama_suplier, d.nama_bank, d.rekening, IFNULL((SELECT SUM(besar_bayar) FROM bayar_pemesanan x WHERE x.no_faktur=p.no_faktur),0) dibayar, DATEDIFF(p.tgl_tempo, CURDATE()) hari
			FROM pemesanan p JOIN datasuplier d ON d.kode_suplier=p.kode_suplier WHERE p.status<>'Sudah Dibayar' ORDER BY p.tgl_tempo`)
		d["Lunas"] = s.rows(ctx, `SELECT b.*, p.kode_suplier, d.nama_suplier FROM bayar_pemesanan b JOIN pemesanan p ON p.no_faktur=b.no_faktur JOIN datasuplier d ON d.kode_suplier=p.kode_suplier WHERE b.tgl_bayar BETWEEN ? AND ? ORDER BY b.tgl_bayar DESC`, dari, sampai)
	case "jurnal":
		js := s.rows(ctx, "SELECT * FROM jurnal WHERE tgl_jurnal BETWEEN ? AND ? ORDER BY no_jurnal DESC LIMIT 300", dari, sampai)
		det := s.rows(ctx, "SELECT d.*, r.nm_rek FROM detailjurnal d JOIN jurnal j ON j.no_jurnal=d.no_jurnal JOIN rekening r ON r.kd_rek=d.kd_rek WHERE j.tgl_jurnal BETWEEN ? AND ? ORDER BY d.debet=0, d.kd_rek", dari, sampai)
		by := map[string][]Row{}
		for _, x := range det {
			by[x["no_jurnal"]] = append(by[x["no_jurnal"]], x)
		}
		type J struct {
			H Row
			D []Row
		}
		var out []J
		for _, j := range js {
			out = append(out, J{j, by[j["no_jurnal"]]})
		}
		d["Jurnal"] = out
	case "bukubesar":
		kd := q.Get("kd_rek")
		if kd == "" {
			kd = "111010"
		}
		d["KdSel"] = kd
		rek := s.row(ctx, "SELECT * FROM rekening WHERE kd_rek=?", kd)
		d["RekSel"] = rek
		if rek != nil {
			sign := 1.0
			if rek["balance"] == "K" {
				sign = -1
			}
			awal := 0.0
			if rek["tipe"] != "R" {
				awal = sign * toF(s.val(ctx, "SELECT IFNULL(SUM(d.debet-d.kredit),0) FROM detailjurnal d JOIN jurnal j ON j.no_jurnal=d.no_jurnal WHERE d.kd_rek=? AND j.tgl_jurnal<?", kd, dari))
			}
			list := s.rows(ctx, "SELECT j.tgl_jurnal, j.no_jurnal, j.no_bukti, j.keterangan, d.debet, d.kredit FROM detailjurnal d JOIN jurnal j ON j.no_jurnal=d.no_jurnal WHERE d.kd_rek=? AND j.tgl_jurnal BETWEEN ? AND ? ORDER BY j.tgl_jurnal, j.no_jurnal", kd, dari, sampai)
			run := awal
			for _, x := range list {
				run += sign * (toF(x["debet"]) - toF(x["kredit"]))
				x["saldo"] = strconv.FormatFloat(run, 'f', 2, 64)
			}
			d["Awal"], d["BB"], d["Akhir"] = awal, list, run
		}
	case "neracasaldo", "labarugi", "neraca":
		ns, err := core.NeracaSaldo(ctx, s.DB, dari, sampai)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		d["NS"] = ns
		var td, tk, pend, beban, aset, kewajiban, modal float64
		var pendL, bebanL, asetL, kewL, modalL []core.Saldo
		for _, x := range ns {
			td += x.Debet
			tk += x.Kredit
			if x.Debet == 0 && x.Kredit == 0 {
				continue
			}
			switch {
			case x.Tipe == "R" && x.Balance == "K":
				pend += x.Saldo
				pendL = append(pendL, x)
			case x.Tipe == "R":
				beban += x.Saldo
				bebanL = append(bebanL, x)
			case x.Tipe == "M":
				modal += x.Saldo
				modalL = append(modalL, x)
			case x.Balance == "D":
				aset += x.Saldo
				asetL = append(asetL, x)
			default:
				kewajiban += x.Saldo
				kewL = append(kewL, x)
			}
		}
		d["TD"], d["TK"] = td, tk
		d["Pend"], d["Beban"], d["Laba"] = pend, beban, pend-beban
		d["PendL"], d["BebanL"] = pendL, bebanL
		// laba ditahan = laba seluruh periode sebelum 'dari' (akun R kumulatif)
		labaLalu := 0.0
		if all, err := core.NeracaSaldo(ctx, s.DB, "1900-01-01", sampai); err == nil {
			l := 0.0
			for _, x := range all {
				if x.Tipe == "R" {
					if x.Balance == "K" {
						l += x.Saldo
					} else {
						l -= x.Saldo
					}
				}
			}
			labaLalu = l - (pend - beban)
		}
		d["LabaLalu"] = labaLalu
		d["Aset"], d["Kewajiban"], d["Modal"] = aset, kewajiban, modal
		d["AsetL"], d["KewL"], d["ModalL"] = asetL, kewL, modalL
		d["Pasiva"] = kewajiban + modal + labaLalu + pend - beban
	case "setelan":
		d["Set"] = s.rows(ctx, "SELECT a.*, r.nm_rek FROM app_set_akun a LEFT JOIN rekening r ON r.kd_rek=a.kd_rek ORDER BY a.nama")
	}
	s.render(w, r, "keuangan", d, 200)
}

func (s *Server) keuAksi(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.ParseForm()
	f := r.FormValue
	u := userOf(r)
	aksi := r.PathValue("aksi")
	to := s.Prefix + "/keuangan?tab=" + f("tab")
	var err error
	var msg string
	switch aksi {
	case "bayar-piutang":
		var sisa float64
		err = s.tx(r, func(tx *sql.Tx) (e error) {
			sisa, e = core.BayarPiutang(ctx, tx, core.BayarPiutangInput{NoRawat: f("no_rawat"), Cicilan: pfx(f("cicilan")), Diskon: pfx(f("diskon")), Hapus: pfx(f("hapus")), Via: f("via"), Catatan: f("catatan")})
			return
		})
		msg = "Pembayaran piutang " + f("no_rawat") + " tersimpan, sisa " + rupiah(sisa)
		to += "&no=" + f("no_rawat")
	case "bayar-hutang":
		var sisa float64
		err = s.tx(r, func(tx *sql.Tx) (e error) {
			sisa, e = core.BayarHutang(ctx, tx, f("no_faktur"), pfx(f("besar")), f("via"), f("no_bukti"), f("keterangan"), u.ID)
			return
		})
		msg = "Pembayaran faktur " + f("no_faktur") + " tersimpan, sisa " + rupiah(sisa)
	case "jurnal":
		var lines []core.Baris
		for i, k := range r.Form["kd_rek"] {
			if kd := kode(k); kd != "" {
				lines = append(lines, core.Baris{KdRek: kd, Debet: pfx(get(r.Form["debet"], i)), Kredit: pfx(get(r.Form["kredit"], i))})
			}
		}
		var no string
		err = s.tx(r, func(tx *sql.Tx) (e error) {
			no, e = core.JurnalManual(ctx, tx, f("no_bukti"), f("keterangan"), lines)
			return
		})
		msg = "Jurnal " + no + " tersimpan"
	case "rekening":
		kd := strings.TrimSpace(f("kd_rek"))
		if kd == "" || strings.TrimSpace(f("nm_rek")) == "" || !strings.Contains("NMR", f("tipe")) || !strings.Contains("DK", f("balance")) || len(f("tipe")) != 1 || len(f("balance")) != 1 {
			err = errors.New("kode, nama, tipe (N/M/R) dan saldo normal (D/K) wajib diisi")
			break
		}
		_, err = s.DB.ExecContext(ctx, "INSERT INTO rekening VALUES (?,?,?,?,'0') ON DUPLICATE KEY UPDATE nm_rek=VALUES(nm_rek), tipe=VALUES(tipe), balance=VALUES(balance)", kd, f("nm_rek"), f("tipe"), f("balance"))
		msg = "Rekening " + kd + " tersimpan"
	case "setelan":
		kd := kode(f("kd_rek"))
		if s.val(ctx, "SELECT COUNT(*) FROM rekening WHERE kd_rek=?", kd) == "0" {
			err = errors.New("rekening " + kd + " tidak ada")
			break
		}
		_, err = s.DB.ExecContext(ctx, "UPDATE app_set_akun SET kd_rek=? WHERE nama=?", kd, f("nama"))
		msg = "Setelan " + f("nama") + " → " + kd
	default:
		http.NotFound(w, r)
		return
	}
	if err == nil {
		s.audit(r, "", "keuangan_"+aksi, f("no_rawat")+f("no_faktur"), msg)
	}
	s.back(w, r, to, err, msg)
}
