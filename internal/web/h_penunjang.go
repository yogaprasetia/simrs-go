package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yogaprasetia/simrs-go/internal/bpjs"
	"github.com/yogaprasetia/simrs-go/internal/core"
)

// ------------------------------------------------------------------ farmasi

func (s *Server) farmasiList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st := r.URL.Query().Get("st")
	if st == "" {
		st = "baru"
	}
	where := map[string]string{"baru": "o.tgl_perawatan IS NULL", "siap": "o.tgl_perawatan IS NOT NULL AND o.tgl_penyerahan IS NULL", "selesai": "o.tgl_penyerahan IS NOT NULL AND o.tgl_penyerahan>=DATE_SUB(CURDATE(), INTERVAL 3 DAY)"}[st]
	if where == "" {
		where = "o.tgl_perawatan IS NULL"
	}
	list := s.rows(ctx, `SELECT o.no_resep, o.no_rawat, o.tgl_peresepan, o.jam_peresepan, o.status, p.nm_pasien, p.no_rkm_medis, d.nm_dokter, pl.nm_poli, pj.png_jawab,
		(SELECT COUNT(*) FROM resep_dokter x WHERE x.no_resep=o.no_resep) item
		FROM resep_obat o JOIN reg_periksa r ON r.no_rawat=o.no_rawat JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis JOIN dokter d ON d.kd_dokter=o.kd_dokter JOIN poliklinik pl ON pl.kd_poli=r.kd_poli JOIN penjab pj ON pj.kd_pj=r.kd_pj
		WHERE `+where+` ORDER BY o.tgl_peresepan, o.jam_peresepan`)
	s.render(w, r, "farmasi", map[string]any{"Title": "Farmasi", "List": list, "St": st}, 200)
}

func (s *Server) farmasiResep(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	no := r.URL.Query().Get("no")
	o := s.row(ctx, `SELECT o.*, p.nm_pasien, p.no_rkm_medis, p.tgl_lahir, d.nm_dokter, pj.png_jawab, r.kd_pj FROM resep_obat o JOIN reg_periksa r ON r.no_rawat=o.no_rawat JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis JOIN dokter d ON d.kd_dokter=o.kd_dokter JOIN penjab pj ON pj.kd_pj=r.kd_pj WHERE o.no_resep=?`, no)
	if o == nil {
		s.back(w, r, s.Prefix+"/farmasi", errors.New("resep tidak ditemukan"), "")
		return
	}
	items := s.rows(ctx, `SELECT rd.kode_brng, b.nama_brng, b.kode_sat, rd.jml, rd.aturan_pakai, IF(?='ranap',b.ranap,b.ralan) harga, IFNULL((SELECT SUM(stok) FROM gudangbarang g WHERE g.kode_brng=rd.kode_brng AND g.kd_bangsal='AP'),0) stok
		FROM resep_dokter rd JOIN databarang b ON b.kode_brng=rd.kode_brng WHERE rd.no_resep=?`, o["status"], no)
	var total float64
	for _, it := range items {
		total += toF(it["harga"]) * toF(it["jml"])
	}
	alergi := s.val(ctx, "SELECT GROUP_CONCAT(DISTINCT alergi SEPARATOR ', ') FROM pemeriksaan_ralan e JOIN reg_periksa r ON r.no_rawat=e.no_rawat WHERE r.no_rkm_medis=? AND alergi<>''", o["no_rkm_medis"])
	s.render(w, r, "farmasi_resep", map[string]any{"Title": "Resep " + no, "O": o, "Items": items, "Total": total, "Alergi": alergi, "Print": r.URL.Query().Get("cetak") != ""}, 200)
}

func (s *Server) farmasiAksi(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	no := r.FormValue("no_resep")
	u := userOf(r)
	var err error
	var msg string
	switch r.PathValue("aksi") {
	case "validasi":
		err = s.tx(r, func(tx *sql.Tx) error { return core.ValidasiResep(ctx, tx, no, u.ID) })
		msg = "Resep " + no + " divalidasi, stok terpotong"
		if err == nil {
			s.task(s.val(ctx, "SELECT no_rawat FROM resep_obat WHERE no_resep=?", no), 6)
		}
	case "serahkan":
		err = s.tx(r, func(tx *sql.Tx) error { return core.SerahkanResep(ctx, tx, no) })
		msg = "Obat resep " + no + " diserahkan"
		if err == nil {
			s.task(s.val(ctx, "SELECT no_rawat FROM resep_obat WHERE no_resep=?", no), 7)
		}
	case "stok":
		kode := strings.TrimSpace(strings.SplitN(r.FormValue("kode_brng"), " ", 2)[0])
		real, _ := strconv.ParseFloat(r.FormValue("real"), 64)
		var sel float64
		err = s.tx(r, func(tx *sql.Tx) (e error) {
			sel, e = core.StokOpname(ctx, tx, kode, "AP", real, r.FormValue("keterangan"), u.ID)
			return
		})
		s.back(w, r, s.Prefix+"/farmasi/stok", err, "Opname "+kode+" tersimpan, selisih "+strconv.FormatFloat(sel, 'f', -1, 64))
		return
	default:
		http.NotFound(w, r)
		return
	}
	if err == nil {
		s.audit(r, "", "farmasi_"+r.PathValue("aksi"), no, "")
	}
	s.back(w, r, s.Prefix+"/farmasi/resep?no="+no, err, msg)
}

func (s *Server) farmasiStok(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query().Get("q")
	list := s.rows(ctx, `SELECT b.kode_brng, b.nama_brng, b.kode_sat, b.h_beli, b.ralan, b.ranap, b.stokminimal, IFNULL(SUM(g.stok),0) stok
		FROM databarang b LEFT JOIN gudangbarang g ON g.kode_brng=b.kode_brng AND g.kd_bangsal='AP' WHERE b.status='1' AND (b.nama_brng LIKE ? OR b.kode_brng LIKE ?) GROUP BY b.kode_brng ORDER BY b.nama_brng`, "%"+q+"%", q+"%")
	riw := s.rows(ctx, `SELECT r.*, b.nama_brng FROM riwayat_barang_medis r JOIN databarang b ON b.kode_brng=r.kode_brng ORDER BY r.tanggal DESC, r.jam DESC LIMIT 30`)
	s.render(w, r, "farmasi_stok", map[string]any{"Title": "Stok Obat (Depo Apotek)", "List": list, "Riwayat": riw, "Q": q}, 200)
}

// ------------------------------------------------------------------ laboratorium

func (s *Server) labList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st := r.URL.Query().Get("st")
	where := "l.tgl_hasil IS NULL"
	if st == "selesai" {
		where = "l.tgl_hasil IS NOT NULL AND l.tgl_hasil>=DATE_SUB(CURDATE(), INTERVAL 7 DAY)"
	}
	list := s.rows(ctx, `SELECT l.noorder, l.no_rawat, l.tgl_permintaan, l.jam_permintaan, l.tgl_hasil, l.status, l.diagnosa_klinis, p.nm_pasien, p.no_rkm_medis, d.nm_dokter,
		(SELECT GROUP_CONCAT(j.nm_perawatan SEPARATOR ', ') FROM permintaan_pemeriksaan_lab x JOIN jns_perawatan_lab j ON j.kd_jenis_prw=x.kd_jenis_prw WHERE x.noorder=l.noorder) isi
		FROM permintaan_lab l JOIN reg_periksa r ON r.no_rawat=l.no_rawat JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis JOIN dokter d ON d.kd_dokter=l.dokter_perujuk
		WHERE `+where+` ORDER BY l.tgl_permintaan, l.jam_permintaan`)
	s.render(w, r, "lab", map[string]any{"Title": "Laboratorium", "List": list, "St": st}, 200)
}

func (s *Server) labOrder(ctx context.Context, no string) (Row, []Row) {
	o := s.row(ctx, `SELECT l.*, p.nm_pasien, p.no_rkm_medis, p.jk, p.tgl_lahir, CONCAT(r.umurdaftar,' ',r.sttsumur) umur, d.nm_dokter, pl.nm_poli, pj.png_jawab
		FROM permintaan_lab l JOIN reg_periksa r ON r.no_rawat=l.no_rawat JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis JOIN dokter d ON d.kd_dokter=l.dokter_perujuk JOIN poliklinik pl ON pl.kd_poli=r.kd_poli JOIN penjab pj ON pj.kd_pj=r.kd_pj WHERE l.noorder=?`, no)
	if o == nil {
		return nil, nil
	}
	items := s.rows(ctx, `SELECT j.kd_jenis_prw, j.nm_perawatan, t.id_template, t.Pemeriksaan, t.satuan, IF(?='P',t.nilai_rujukan_pd,t.nilai_rujukan_ld) rujukan,
		(SELECT nilai FROM detail_periksa_lab dl WHERE dl.no_rawat=? AND dl.id_template=t.id_template AND dl.tgl_periksa=? LIMIT 1) nilai,
		(SELECT keterangan FROM detail_periksa_lab dl WHERE dl.no_rawat=? AND dl.id_template=t.id_template AND dl.tgl_periksa=? LIMIT 1) flag
		FROM permintaan_pemeriksaan_lab x JOIN jns_perawatan_lab j ON j.kd_jenis_prw=x.kd_jenis_prw JOIN template_laboratorium t ON t.kd_jenis_prw=j.kd_jenis_prw
		WHERE x.noorder=? ORDER BY j.nm_perawatan, t.urut`, o["jk"], o["no_rawat"], o["tgl_hasil"], o["no_rawat"], o["tgl_hasil"], no)
	return o, items
}

func (s *Server) labHasil(w http.ResponseWriter, r *http.Request) {
	o, items := s.labOrder(r.Context(), r.URL.Query().Get("no"))
	if o == nil {
		s.back(w, r, s.Prefix+"/lab", errors.New("order tidak ditemukan"), "")
		return
	}
	s.render(w, r, "lab_hasil", map[string]any{"Title": "Hasil Lab " + o["noorder"], "O": o, "Items": items,
		"Dokter": s.rows(r.Context(), "SELECT kd_dokter, nm_dokter FROM dokter WHERE status='1' ORDER BY nm_dokter")}, 200)
}

func (s *Server) labSimpan(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	no := r.FormValue("noorder")
	nilai := map[int]string{}
	for k, v := range r.PostForm {
		if strings.HasPrefix(k, "t_") {
			id, _ := strconv.Atoi(k[2:])
			nilai[id] = v[0]
		}
	}
	err := s.tx(r, func(tx *sql.Tx) error {
		return core.SimpanHasilLab(r.Context(), tx, no, userOf(r).ID, r.FormValue("kd_dokter"), nilai)
	})
	if err == nil {
		s.audit(r, "", "lab_hasil", no, "")
	}
	s.back(w, r, s.Prefix+"/lab/hasil?no="+no, err, "Hasil lab tersimpan")
}

func (s *Server) labCetak(w http.ResponseWriter, r *http.Request) {
	o, items := s.labOrder(r.Context(), r.URL.Query().Get("no"))
	if o == nil {
		s.fail(w, r, errors.New("order tidak ditemukan"))
		return
	}
	s.render(w, r, "print_lab", map[string]any{"Title": "Hasil Lab", "O": o, "Items": items, "Print": true}, 200)
}

// ------------------------------------------------------------------ kasir

func (s *Server) kasirList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	st := q.Get("st")
	if st == "" {
		st = "Belum Bayar"
	}
	tgl1, tgl2 := q.Get("dari"), q.Get("sampai")
	if tgl1 == "" {
		tgl1 = time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	}
	if tgl2 == "" {
		tgl2 = core.Today()
	}
	list := s.rows(ctx, `SELECT r.no_rawat, r.tgl_registrasi, r.status_lanjut, p.nm_pasien, p.no_rkm_medis, pl.nm_poli, pj.png_jawab, r.status_bayar,
		(SELECT COUNT(*) FROM kamar_inap k WHERE k.no_rawat=r.no_rawat AND k.stts_pulang='-') dirawat,
		(SELECT totalbiaya FROM billing b WHERE b.no_rawat=r.no_rawat AND b.status='Tagihan' LIMIT 1) total,
		(SELECT no_sep FROM bridging_sep s WHERE s.no_rawat=r.no_rawat LIMIT 1) no_sep
		FROM reg_periksa r JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis JOIN poliklinik pl ON pl.kd_poli=r.kd_poli JOIN penjab pj ON pj.kd_pj=r.kd_pj
		WHERE r.status_bayar=? AND r.stts<>'Batal' AND r.tgl_registrasi BETWEEN ? AND ? ORDER BY r.tgl_registrasi DESC, r.jam_reg DESC`, st, tgl1, tgl2)
	s.render(w, r, "kasir", map[string]any{"Title": "Kasir", "List": list, "St": st, "Dari": tgl1, "Sampai": tgl2}, 200)
}

func (s *Server) kasirBilling(w http.ResponseWriter, r *http.Request) {
	no := r.URL.Query().Get("no")
	b, err := core.HitungBilling(r.Context(), s.DB, no)
	if err != nil {
		s.back(w, r, s.Prefix+"/kasir", err, "")
		return
	}
	d := map[string]any{"Title": "Billing " + no, "B": b}
	d["Nota"] = s.val(r.Context(), "SELECT no_nota FROM nota_jalan WHERE no_rawat=? UNION SELECT no_nota FROM nota_inap WHERE no_rawat=? LIMIT 1", no, no)
	d["SEP"] = s.val(r.Context(), "SELECT no_sep FROM bridging_sep WHERE no_rawat=? LIMIT 1", no)
	d["Tambahan"] = s.rows(r.Context(), "SELECT * FROM tambahan_biaya WHERE no_rawat=?", no)
	d["Pending"] = s.val(r.Context(), "SELECT (SELECT COUNT(*) FROM resep_obat WHERE no_rawat=? AND tgl_perawatan IS NULL)+(SELECT COUNT(*) FROM permintaan_lab WHERE no_rawat=? AND tgl_hasil IS NULL)+(SELECT COUNT(*) FROM permintaan_radiologi WHERE no_rawat=? AND tgl_hasil IS NULL)+(SELECT COUNT(*) FROM booking_operasi WHERE no_rawat=? AND status='Proses Operasi')", no, no, no, no)
	s.render(w, r, "kasir_billing", d, 200)
}

func (s *Server) kasirAksi(w http.ResponseWriter, r *http.Request) {
	no := r.FormValue("no_rawat")
	switch r.PathValue("aksi") {
	case "bayar":
		var nota string
		err := s.tx(r, func(tx *sql.Tx) (e error) { nota, e = core.BayarBilling(r.Context(), tx, no); return })
		if err == nil {
			s.audit(r, "", "kasir_bayar", no, nota)
		}
		s.back(w, r, s.Prefix+"/kasir/billing?no="+no, err, "Pembayaran tersimpan, nota "+nota)
	case "batal":
		if !userOf(r).Has("admin") {
			s.back(w, r, "", errors.New("hanya admin yang boleh membatalkan pembayaran"), "")
			return
		}
		err := s.tx(r, func(tx *sql.Tx) error { return core.BatalBayar(r.Context(), tx, no) })
		if err == nil {
			s.audit(r, "", "kasir_batal", no, "")
		}
		s.back(w, r, s.Prefix+"/kasir/billing?no="+no, err, "Pembayaran dibatalkan, transaksi dibuka kembali")
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) kasirNota(w http.ResponseWriter, r *http.Request) {
	no := r.URL.Query().Get("no")
	reg, err := core.GetReg(r.Context(), s.DB, no)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	items := s.rows(r.Context(), "SELECT * FROM billing WHERE no_rawat=? ORDER BY noindex", no)
	nota := s.row(r.Context(), "SELECT no_nota, tanggal, jam FROM nota_jalan WHERE no_rawat=? UNION SELECT no_nota, tanggal, jam FROM nota_inap WHERE no_rawat=? LIMIT 1", no, no)
	if nota == nil {
		s.back(w, r, s.Prefix+"/kasir/billing?no="+no, errors.New("belum dibayar"), "")
		return
	}
	s.render(w, r, "print_nota", map[string]any{"Title": "Nota", "R": reg, "Items": items, "Nota": nota, "Print": true}, 200)
}

// ------------------------------------------------------------------ BPJS

func (s *Server) bpjsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := map[string]any{"Title": "Bridging BPJS", "Cfg": s.BPJS.Cfg}
	d["SEP"] = s.rows(ctx, "SELECT b.*, r.status_lanjut FROM bridging_sep b JOIN reg_periksa r ON r.no_rawat=b.no_rawat ORDER BY b.tglsep DESC, b.no_sep DESC LIMIT 50")
	d["Log"] = s.rows(ctx, "SELECT * FROM app_bpjs_log ORDER BY id DESC LIMIT 30")
	d["Belum"] = s.rows(ctx, `SELECT r.no_rawat, r.tgl_registrasi, p.nm_pasien, p.no_peserta, pl.nm_poli FROM reg_periksa r JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis JOIN poliklinik pl ON pl.kd_poli=r.kd_poli
		WHERE r.kd_pj='BPJ' AND r.stts<>'Batal' AND r.tgl_registrasi>=DATE_SUB(CURDATE(), INTERVAL 3 DAY) AND NOT EXISTS (SELECT 1 FROM bridging_sep b WHERE b.no_rawat=r.no_rawat) ORDER BY r.tgl_registrasi DESC`)
	if q := strings.TrimSpace(r.URL.Query().Get("cek")); q != "" {
		c, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		var p map[string]any
		var err error
		if len(q) == 16 {
			p, err = s.BPJS.PesertaByNIK(c, q, core.Today())
		} else {
			p, err = s.BPJS.PesertaByKartu(c, q, core.Today())
		}
		d["Cek"], d["CekQ"] = p, q
		if err != nil {
			d["CekErr"] = err.Error()
		}
	}
	s.render(w, r, "bpjs", d, 200)
}

func (s *Server) bpjsSEPForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	no := r.URL.Query().Get("no")
	reg, err := core.GetReg(ctx, s.DB, no)
	if err != nil {
		s.back(w, r, s.Prefix+"/bpjs", err, "")
		return
	}
	d := map[string]any{"Title": "Buat SEP", "R": reg}
	d["X"] = s.row(ctx, "SELECT pl.kd_poli_bpjs, d.kd_dokter_bpjs, p.no_tlp FROM reg_periksa r JOIN poliklinik pl ON pl.kd_poli=r.kd_poli JOIN dokter d ON d.kd_dokter=r.kd_dokter JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis WHERE r.no_rawat=?", no)
	if reg.NoPeserta != "" && s.BPJS.Cfg.Enabled() {
		c, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		if p, err := s.BPJS.PesertaByKartu(c, reg.NoPeserta, reg.Tgl); err == nil {
			d["Peserta"] = p
		} else {
			d["PErr"] = err.Error()
		}
		if reg.StatusLanjut == "Ralan" {
			if ruj, err := s.BPJS.RujukanByKartu(c, reg.NoPeserta); err == nil {
				d["Rujukan"] = ruj
			} else {
				d["RErr"] = err.Error()
			}
		}
	}
	d["Diag"] = s.val(ctx, "SELECT kd_penyakit FROM diagnosa_pasien WHERE no_rawat=? ORDER BY prioritas LIMIT 1", no)
	s.render(w, r, "bpjs_sep", d, 200)
}

func (s *Server) bpjsAksi(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	u := userOf(r)
	f := r.FormValue
	switch r.PathValue("aksi") {
	case "sep":
		no := f("no_rawat")
		reg, err := core.GetReg(ctx, s.DB, no)
		if err != nil {
			s.back(w, r, "", err, "")
			return
		}
		if s.val(ctx, "SELECT COUNT(*) FROM bridging_sep WHERE no_rawat=?", no) != "0" {
			s.back(w, r, "", errors.New("no. rawat ini sudah memiliki SEP"), "")
			return
		}
		jns := "2"
		if reg.StatusLanjut == "Ranap" {
			jns = "1"
		}
		diag := strings.TrimSpace(strings.SplitN(f("diagawal"), " ", 2)[0])
		req := bpjs.SEPRequest{NoKartu: f("no_kartu"), TglSep: reg.Tgl, JnsPelayanan: jns, KlsRawatHak: f("klsrawat"), NoMR: reg.NoRM, AsalRujukan: f("asal_rujukan"),
			TglRujukan: f("tglrujukan"), NoRujukan: f("no_rujukan"), PPKRujukan: f("kdppkrujukan"), Catatan: f("catatan"), DiagAwal: diag, PoliTujuan: f("kdpolitujuan"),
			KodeDPJP: f("kddpjp"), DPJPLayan: f("kddpjp"), NoTelp: f("notelep"), User: u.ID, TujuanKunj: "0"}
		if jns == "1" {
			req.PoliTujuan, req.DPJPLayan = "", ""
		}
		sep, err := s.BPJS.InsertSEP(ctx, req)
		if err != nil {
			s.back(w, r, "", err, "")
			return
		}
		ps, _ := sep["peserta"].(map[string]any)
		asal := "1. Faskes 1"
		if req.AsalRujukan == "2" {
			asal = "2. Faskes 2(RS)"
		}
		tglruj := any(nil)
		if req.TglRujukan != "" {
			tglruj = req.TglRujukan
		}
		_, err = s.DB.ExecContext(ctx, `INSERT INTO bridging_sep (no_sep,no_rawat,tglsep,tglrujukan,no_rujukan,kdppkrujukan,nmppkrujukan,kdppkpelayanan,nmppkpelayanan,jnspelayanan,catatan,diagawal,nmdiagnosaawal,kdpolitujuan,nmpolitujuan,klsrawat,lakalantas,user,nomr,nama_pasien,tanggal_lahir,peserta,jkel,no_kartu,asal_rujukan,notelep,kddpjp,nmdpdjp)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'0',?,?,?,?,?,?,?,?,?,?,?)`,
			fmt.Sprint(sep["noSep"]), no, reg.Tgl, tglruj, req.NoRujukan, req.PPKRujukan, f("nmppkrujukan"), s.BPJS.Cfg.KodePPK, s.BPJS.Cfg.NamaPPK, jns, req.Catatan, diag, s.val(ctx, "SELECT nm_penyakit FROM penyakit WHERE kd_penyakit=?", diag),
			req.PoliTujuan, reg.NmPoli, req.KlsRawatHak, u.ID, reg.NoRM, reg.Nama, reg.TglLahir, str(ps["jnsPeserta"]), reg.JK, req.NoKartu, asal, req.NoTelp, req.KodeDPJP, reg.NmDokter)
		if err != nil {
			s.back(w, r, "", fmt.Errorf("SEP %v terbit di BPJS tapi gagal disimpan lokal: %w", sep["noSep"], err), "")
			return
		}
		s.audit(r, "", "sep_insert", no, fmt.Sprint(sep["noSep"]))
		s.back(w, r, s.Prefix+"/bpjs/sep/cetak?no="+fmt.Sprint(sep["noSep"]), nil, "SEP terbit: "+fmt.Sprint(sep["noSep"]))
	case "sep-hapus":
		no := f("no_sep")
		if err := s.BPJS.HapusSEP(ctx, no, u.ID); err != nil {
			s.back(w, r, "", err, "")
			return
		}
		_, err := s.DB.ExecContext(ctx, "DELETE FROM bridging_sep WHERE no_sep=?", no)
		s.audit(r, "", "sep_hapus", no, "")
		s.back(w, r, s.Prefix+"/bpjs", err, "SEP "+no+" dihapus")
	case "antrean":
		no := f("no_rawat")
		err := s.kirimAntrean(ctx, no)
		s.back(w, r, "", err, "Antrean "+no+" terkirim ke BPJS")
	default:
		http.NotFound(w, r)
	}
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	case map[string]any:
		if k, ok := x["keterangan"]; ok {
			return fmt.Sprint(k)
		}
	}
	return fmt.Sprint(v)
}

func (s *Server) bpjsSEPCetak(w http.ResponseWriter, r *http.Request) {
	sep := s.row(r.Context(), "SELECT b.*, r.no_reg FROM bridging_sep b JOIN reg_periksa r ON r.no_rawat=b.no_rawat WHERE b.no_sep=?", r.URL.Query().Get("no"))
	if sep == nil {
		s.fail(w, r, errors.New("SEP tidak ditemukan"))
		return
	}
	s.render(w, r, "print_sep", map[string]any{"Title": "SEP " + sep["no_sep"], "S": sep, "Print": true}, 200)
}

// ------------------------------------------------------------------ laporan

func (s *Server) laporan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	dari, sampai := q.Get("dari"), q.Get("sampai")
	now := core.Now()
	if dari == "" {
		dari = now.Format("2006-01") + "-01"
	}
	if sampai == "" {
		sampai = core.Today()
	}
	d := map[string]any{"Title": "Laporan", "Dari": dari, "Sampai": sampai}
	d["Kunjungan"] = s.rows(ctx, `SELECT pl.nm_poli, COUNT(*) total, SUM(r.stts_daftar='Baru') baru, SUM(r.stts_daftar='Lama') lama, SUM(r.kd_pj='BPJ') bpjs, SUM(r.kd_pj<>'BPJ') non_bpjs
		FROM reg_periksa r JOIN poliklinik pl ON pl.kd_poli=r.kd_poli WHERE r.stts<>'Batal' AND r.tgl_registrasi BETWEEN ? AND ? GROUP BY pl.nm_poli ORDER BY total DESC`, dari, sampai)
	d["Penyakit"] = s.rows(ctx, `SELECT d.kd_penyakit, p.nm_penyakit, COUNT(*) jml, SUM(d.status_penyakit='Baru') baru FROM diagnosa_pasien d JOIN penyakit p ON p.kd_penyakit=d.kd_penyakit JOIN reg_periksa r ON r.no_rawat=d.no_rawat
		WHERE d.prioritas=1 AND r.tgl_registrasi BETWEEN ? AND ? GROUP BY d.kd_penyakit, p.nm_penyakit ORDER BY jml DESC LIMIT 10`, dari, sampai)
	d["Pendapatan"] = s.rows(ctx, `SELECT b.status, SUM(b.totalbiaya) total FROM billing b WHERE b.tgl_byr BETWEEN ? AND ? AND b.status<>'Tagihan' GROUP BY b.status ORDER BY total DESC`, dari, sampai)
	d["PendapatanBayar"] = s.rows(ctx, `SELECT pj.png_jawab, COUNT(*) jml, SUM(b.totalbiaya) total FROM billing b JOIN reg_periksa r ON r.no_rawat=b.no_rawat JOIN penjab pj ON pj.kd_pj=r.kd_pj WHERE b.tgl_byr BETWEEN ? AND ? AND b.status='Tagihan' GROUP BY pj.png_jawab`, dari, sampai)
	d["Obat"] = s.rows(ctx, `SELECT b.nama_brng, b.kode_sat, SUM(d.jml) jml, SUM(d.total) total FROM detail_pemberian_obat d JOIN databarang b ON b.kode_brng=d.kode_brng WHERE d.tgl_perawatan BETWEEN ? AND ? GROUP BY b.kode_brng ORDER BY jml DESC LIMIT 10`, dari, sampai)
	d["Rad"] = s.rows(ctx, `SELECT j.nm_perawatan, COUNT(*) jml, SUM(p.biaya) total FROM periksa_radiologi p JOIN jns_perawatan_radiologi j ON j.kd_jenis_prw=p.kd_jenis_prw WHERE p.tgl_periksa BETWEEN ? AND ? GROUP BY j.nm_perawatan ORDER BY jml DESC`, dari, sampai)
	d["Ops"] = s.rows(ctx, `SELECT p.kategori, p.nm_perawatan, COUNT(*) jml, SUM(`+core.BiayaOperasiExpr()+`) total FROM operasi o JOIN paket_operasi p ON p.kode_paket=o.kode_paket WHERE DATE(o.tgl_operasi) BETWEEN ? AND ? GROUP BY p.kategori, p.nm_perawatan ORDER BY p.kategori, jml DESC`, dari, sampai)
	// indikator rawat inap: BOR, ALOS, BTO, TOI (Barber-Johnson)
	t1, _ := time.Parse("2006-01-02", dari)
	t2, _ := time.Parse("2006-01-02", sampai)
	periode := int(t2.Sub(t1).Hours()/24) + 1
	bed, _ := strconv.Atoi(s.val(ctx, "SELECT COUNT(*) FROM kamar WHERE statusdata='1'"))
	hp := 0
	end := t2.AddDate(0, 0, 1)
	for _, k := range s.rows(ctx, "SELECT tgl_masuk, IFNULL(tgl_keluar,'') tgl_keluar FROM kamar_inap WHERE tgl_masuk<=? AND (tgl_keluar IS NULL OR tgl_keluar>=?)", sampai, dari) {
		m, _ := time.Parse("2006-01-02", k["tgl_masuk"])
		kl := end
		if k["tgl_keluar"] != "" {
			kl, _ = time.Parse("2006-01-02", k["tgl_keluar"])
			if kl.Equal(m) {
				kl = kl.AddDate(0, 0, 1) // masuk & keluar hari sama = 1 hari
			}
		}
		if m.Before(t1) {
			m = t1
		}
		if kl.After(end) {
			kl = end
		}
		if n := int(kl.Sub(m).Hours() / 24); n > 0 {
			hp += n
		}
	}
	keluar, _ := strconv.Atoi(s.val(ctx, "SELECT COUNT(*) FROM kamar_inap WHERE stts_pulang NOT IN ('-','Pindah Kamar') AND tgl_keluar BETWEEN ? AND ?", dari, sampai))
	lama, _ := strconv.Atoi(s.val(ctx, `SELECT IFNULL(SUM(x.l),0) FROM (SELECT k.no_rawat, SUM(k.lama) l FROM kamar_inap k WHERE k.no_rawat IN (SELECT no_rawat FROM kamar_inap WHERE stts_pulang NOT IN ('-','Pindah Kamar') AND tgl_keluar BETWEEN ? AND ?) GROUP BY k.no_rawat) x`, dari, sampai))
	mati, _ := strconv.Atoi(s.val(ctx, "SELECT COUNT(*) FROM kamar_inap WHERE stts_pulang='Meninggal' AND tgl_keluar BETWEEN ? AND ?", dari, sampai))
	ind := map[string]string{"Periode": strconv.Itoa(periode), "Bed": strconv.Itoa(bed), "HP": strconv.Itoa(hp), "Keluar": strconv.Itoa(keluar), "Mati": strconv.Itoa(mati)}
	f1 := func(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }
	if bed > 0 && periode > 0 {
		ind["BOR"] = f1(float64(hp) * 100 / float64(bed*periode))
		ind["BTO"] = f1(float64(keluar) / float64(bed))
	}
	if keluar > 0 {
		ind["ALOS"] = f1(float64(lama) / float64(keluar))
		ind["TOI"] = f1(float64(bed*periode-hp) / float64(keluar))
		ind["GDR"] = f1(float64(mati) * 1000 / float64(keluar))
	}
	d["Ind"] = ind
	if q.Get("csv") == "kunjungan" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=kunjungan_"+dari+"_"+sampai+".csv")
		fmt.Fprintln(w, "Poli,Total,Baru,Lama,BPJS,Non BPJS")
		for _, k := range d["Kunjungan"].([]Row) {
			fmt.Fprintf(w, "%q,%s,%s,%s,%s,%s\n", k["nm_poli"], k["total"], k["baru"], k["lama"], k["bpjs"], k["non_bpjs"])
		}
		return
	}
	s.render(w, r, "laporan", d, 200)
}
