package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/yogaprasetia/simrs-go/internal/core"
)

// ------------------------------------------------------------------ rawat jalan / IGD

func (s *Server) ralanList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	tgl := q.Get("tgl")
	if tgl == "" {
		tgl = core.Today()
	}
	poli := q.Get("poli")
	dok := q.Get("dokter")
	u := userOf(r)
	if dok == "" && u.KdDokter != "" && q.Get("semua") == "" {
		dok = u.KdDokter
	}
	where := "r.tgl_registrasi=? AND r.stts<>'Batal' AND r.status_lanjut='Ralan'"
	args := []any{tgl}
	if poli != "" {
		where += " AND r.kd_poli=?"
		args = append(args, poli)
	}
	if dok != "" {
		where += " AND r.kd_dokter=?"
		args = append(args, dok)
	}
	list := s.rows(ctx, `SELECT r.no_rawat, r.no_reg, r.jam_reg, r.no_rkm_medis, p.nm_pasien, p.jk, CONCAT(r.umurdaftar,' ',r.sttsumur) umur, pl.nm_poli, d.nm_dokter, pj.png_jawab, r.stts, r.status_bayar
		FROM reg_periksa r JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis JOIN poliklinik pl ON pl.kd_poli=r.kd_poli JOIN dokter d ON d.kd_dokter=r.kd_dokter JOIN penjab pj ON pj.kd_pj=r.kd_pj
		WHERE `+where+` ORDER BY r.stts='Belum' DESC, r.kd_poli, CAST(r.no_reg AS UNSIGNED)`, args...)
	d := s.masters(ctx)
	d["Title"], d["List"], d["Tgl"], d["PoliSel"], d["DokSel"] = "Rawat Jalan / IGD", list, tgl, poli, dok
	s.render(w, r, "ralan", d, 200)
}

// rawatPage: layar ERM satu kunjungan (SOAP, diagnosa, tindakan, resep, lab, kamar).
func (s *Server) rawatPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	no := r.URL.Query().Get("no")
	reg, err := core.GetReg(ctx, s.DB, no)
	if err != nil {
		s.back(w, r, s.Prefix+"/ralan", err, "")
		return
	}
	ranap := reg.StatusLanjut == "Ranap"
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "soap"
	}
	d := map[string]any{"Title": "ERM " + reg.Nama, "R": reg, "Ranap": ranap, "Tab": tab, "Locked": reg.StatusBayar == "Sudah Bayar" || reg.Stts == "Batal"}
	d["Pasien"] = s.row(ctx, "SELECT * FROM pasien WHERE no_rkm_medis=?", reg.NoRM)
	d["Alergi"] = s.val(ctx, "SELECT GROUP_CONCAT(DISTINCT alergi SEPARATOR ', ') FROM (SELECT alergi FROM pemeriksaan_ralan e JOIN reg_periksa r ON r.no_rawat=e.no_rawat WHERE r.no_rkm_medis=? AND alergi<>'' UNION SELECT alergi FROM pemeriksaan_ranap e JOIN reg_periksa r ON r.no_rawat=e.no_rawat WHERE r.no_rkm_medis=? AND alergi<>'') x", reg.NoRM, reg.NoRM)
	d["SOAP"] = s.rows(ctx, `SELECT 'Ralan' jenis, e.* FROM pemeriksaan_ralan e WHERE no_rawat=? UNION ALL SELECT 'Ranap', e.* FROM pemeriksaan_ranap e WHERE no_rawat=? ORDER BY tgl_perawatan DESC, jam_rawat DESC`, no, no)
	d["Diagnosa"] = s.rows(ctx, "SELECT d.*, p.nm_penyakit FROM diagnosa_pasien d JOIN penyakit p ON p.kd_penyakit=d.kd_penyakit WHERE no_rawat=? ORDER BY status, prioritas", no)
	d["Prosedur"] = s.rows(ctx, "SELECT d.*, i.deskripsi_panjang FROM prosedur_pasien d JOIN icd9 i ON i.kode=d.kode WHERE no_rawat=? ORDER BY status, prioritas", no)
	d["Tindakan"] = s.rows(ctx, `SELECT 'Ralan' jenis, t.kd_jenis_prw, j.nm_perawatan, t.tgl_perawatan, t.jam_rawat, t.biaya_rawat, dk.nm_dokter FROM rawat_jl_dr t JOIN jns_perawatan j ON j.kd_jenis_prw=t.kd_jenis_prw JOIN dokter dk ON dk.kd_dokter=t.kd_dokter WHERE t.no_rawat=?
		UNION ALL SELECT 'Ranap', t.kd_jenis_prw, j.nm_perawatan, t.tgl_perawatan, t.jam_rawat, t.biaya_rawat, dk.nm_dokter FROM rawat_inap_dr t JOIN jns_perawatan_inap j ON j.kd_jenis_prw=t.kd_jenis_prw JOIN dokter dk ON dk.kd_dokter=t.kd_dokter WHERE t.no_rawat=? ORDER BY tgl_perawatan, jam_rawat`, no, no)
	if ranap {
		d["MasterTindakan"] = s.rows(ctx, "SELECT kd_jenis_prw, nm_perawatan, total_byrdr FROM jns_perawatan_inap WHERE status='1' ORDER BY nm_perawatan")
	} else {
		d["MasterTindakan"] = s.rows(ctx, "SELECT kd_jenis_prw, nm_perawatan, total_byrdr FROM jns_perawatan WHERE status='1' AND (kd_poli='-' OR kd_poli=?) AND (kd_pj='-' OR kd_pj=?) ORDER BY nm_perawatan", reg.KdPoli, reg.KdPJ)
	}
	d["Resep"] = s.rows(ctx, `SELECT o.no_resep, o.tgl_peresepan, o.jam_peresepan, o.tgl_perawatan, o.tgl_penyerahan, dk.nm_dokter,
		(SELECT GROUP_CONCAT(CONCAT(b.nama_brng,' × ',FORMAT(rd.jml,0),' — ',rd.aturan_pakai) SEPARATOR '\n') FROM resep_dokter rd JOIN databarang b ON b.kode_brng=rd.kode_brng WHERE rd.no_resep=o.no_resep) isi
		FROM resep_obat o JOIN dokter dk ON dk.kd_dokter=o.kd_dokter WHERE o.no_rawat=? ORDER BY o.no_resep DESC`, no)
	d["MasterLab"] = s.rows(ctx, "SELECT kd_jenis_prw, nm_perawatan, total_byr FROM jns_perawatan_lab WHERE status='1' ORDER BY nm_perawatan")
	d["Lab"] = s.rows(ctx, `SELECT l.noorder, l.tgl_permintaan, l.jam_permintaan, l.tgl_hasil, l.diagnosa_klinis,
		(SELECT GROUP_CONCAT(j.nm_perawatan SEPARATOR ', ') FROM permintaan_pemeriksaan_lab p JOIN jns_perawatan_lab j ON j.kd_jenis_prw=p.kd_jenis_prw WHERE p.noorder=l.noorder) isi
		FROM permintaan_lab l WHERE l.no_rawat=? ORDER BY l.noorder DESC`, no)
	d["Kamar"] = s.rows(ctx, `SELECT k.*, b.nm_bangsal FROM kamar_inap k JOIN kamar m ON m.kd_kamar=k.kd_kamar JOIN bangsal b ON b.kd_bangsal=m.kd_bangsal WHERE k.no_rawat=? ORDER BY tgl_masuk, jam_masuk`, no)
	d["KamarKosong"] = s.rows(ctx, `SELECT k.kd_kamar, b.nm_bangsal, k.kelas, k.trf_kamar FROM kamar k JOIN bangsal b ON b.kd_bangsal=k.kd_bangsal WHERE k.status='KOSONG' AND k.statusdata='1' ORDER BY k.kelas, k.kd_kamar`)
	d["Dokter"] = s.rows(ctx, "SELECT kd_dokter, nm_dokter FROM dokter WHERE status='1' ORDER BY nm_dokter")
	d["DPJP"] = s.rows(ctx, "SELECT d.nm_dokter FROM dpjp_ranap x JOIN dokter d ON d.kd_dokter=x.kd_dokter WHERE x.no_rawat=?", no)
	d["Riwayat"] = s.rows(ctx, `SELECT r.no_rawat, r.tgl_registrasi, pl.nm_poli, r.status_lanjut,
		(SELECT GROUP_CONCAT(CONCAT(dp.kd_penyakit,' ',py.nm_penyakit) SEPARATOR '; ') FROM diagnosa_pasien dp JOIN penyakit py ON py.kd_penyakit=dp.kd_penyakit WHERE dp.no_rawat=r.no_rawat) diagnosa,
		(SELECT CONCAT('S: ',keluhan,' | A: ',penilaian,' | P: ',rtl) FROM pemeriksaan_ralan e WHERE e.no_rawat=r.no_rawat ORDER BY tgl_perawatan DESC, jam_rawat DESC LIMIT 1) soap
		FROM reg_periksa r JOIN poliklinik pl ON pl.kd_poli=r.kd_poli WHERE r.no_rkm_medis=? AND r.no_rawat<>? AND r.stts<>'Batal' ORDER BY r.tgl_registrasi DESC LIMIT 10`, reg.NoRM, no)
	u := userOf(r)
	d["MyDokter"] = u.KdDokter
	if u.KdDokter == "" {
		d["MyDokter"] = reg.KdDokter
	}
	s.render(w, r, "rawat", d, 200)
}

func (s *Server) rawatAksi(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.ParseForm()
	no := r.FormValue("no_rawat")
	aksi := r.PathValue("aksi")
	u := userOf(r)
	reg, err := core.GetReg(ctx, s.DB, no)
	if err != nil {
		s.back(w, r, s.Prefix+"/ralan", err, "")
		return
	}
	ranap := reg.StatusLanjut == "Ranap"
	to := s.Prefix + "/rawat?no=" + no
	klinis := u.Has("dokter", "perawat")
	need := func(ok bool) bool {
		if !ok {
			s.back(w, r, to, errors.New("peran Anda tidak boleh melakukan aksi ini"), "")
		}
		return ok
	}
	var msg string
	switch aksi {
	case "soap":
		if !need(klinis) {
			return
		}
		f := r.FormValue
		err = s.tx(r, func(tx *sql.Tx) error {
			_, e := core.SimpanSOAP(ctx, tx, core.SOAP{NoRawat: no, Suhu: f("suhu_tubuh"), Tensi: f("tensi"), Nadi: f("nadi"), Respirasi: f("respirasi"), Tinggi: f("tinggi"), Berat: f("berat"),
				SpO2: f("spo2"), GCS: f("gcs"), Kesadaran: f("kesadaran"), Keluhan: f("keluhan"), Pemeriksaan: f("pemeriksaan"), Alergi: f("alergi"), LingkarPerut: f("lingkar_perut"),
				RTL: f("rtl"), Penilaian: f("penilaian"), Instruksi: f("instruksi"), Evaluasi: f("evaluasi"), NIP: u.ID, Ranap: ranap})
			return e
		})
		if err == nil && !ranap {
			s.task(no, 4) // mulai dilayani poli
		}
		msg, to = "Pemeriksaan tersimpan", to+"&tab=soap"
	case "diagnosa":
		if !need(u.Has("dokter")) {
			return
		}
		kd := strings.TrimSpace(strings.SplitN(r.FormValue("kd_penyakit"), " ", 2)[0])
		err = s.tx(r, func(tx *sql.Tx) error { return core.TambahDiagnosa(ctx, tx, no, kd, ranap) })
		msg, to = "Diagnosa "+kd+" ditambahkan", to+"&tab=diagnosa"
	case "diagnosa-hapus":
		if !need(u.Has("dokter")) {
			return
		}
		err = s.tx(r, func(tx *sql.Tx) error {
			return core.HapusDiagnosa(ctx, tx, no, r.FormValue("kd_penyakit"), r.FormValue("status"))
		})
		msg, to = "Diagnosa dihapus", to+"&tab=diagnosa"
	case "prosedur":
		if !need(u.Has("dokter")) {
			return
		}
		kd := strings.TrimSpace(strings.SplitN(r.FormValue("kode"), " ", 2)[0])
		err = s.tx(r, func(tx *sql.Tx) error { return core.TambahProsedur(ctx, tx, no, kd, ranap) })
		msg, to = "Prosedur "+kd+" ditambahkan", to+"&tab=diagnosa"
	case "tindakan":
		if !need(klinis) {
			return
		}
		dok := r.FormValue("kd_dokter")
		for _, kd := range r.Form["kd_jenis_prw"] {
			if err = s.tx(r, func(tx *sql.Tx) error { return core.TambahTindakan(ctx, tx, no, kd, dok, ranap) }); err != nil {
				break
			}
		}
		msg, to = "Tindakan tersimpan", to+"&tab=tindakan"
	case "tindakan-hapus":
		if !need(klinis) {
			return
		}
		err = s.tx(r, func(tx *sql.Tx) error {
			return core.HapusTindakan(ctx, tx, no, r.FormValue("kd_jenis_prw"), r.FormValue("tgl"), r.FormValue("jam"), r.FormValue("jenis") == "Ranap")
		})
		msg, to = "Tindakan dihapus", to+"&tab=tindakan"
	case "resep":
		if !need(u.Has("dokter")) {
			return
		}
		var items []core.ItemResep
		kode, jml, aturan := r.Form["kode_brng"], r.Form["jml"], r.Form["aturan"]
		for i := range kode {
			k := strings.TrimSpace(strings.SplitN(kode[i], " ", 2)[0])
			if k == "" {
				continue
			}
			j, _ := strconv.ParseFloat(strings.ReplaceAll(get(jml, i), ",", "."), 64)
			items = append(items, core.ItemResep{KodeBrng: k, Jml: j, Aturan: get(aturan, i)})
		}
		dok := u.KdDokter
		if dok == "" {
			dok = reg.KdDokter
		}
		var nr string
		err = s.tx(r, func(tx *sql.Tx) (e error) { nr, e = core.BuatResep(ctx, tx, no, dok, items); return })
		msg, to = "Resep "+nr+" dikirim ke farmasi", to+"&tab=resep"
		if err == nil && !ranap {
			s.task(no, 5) // selesai poli
		}
	case "resep-hapus":
		if !need(u.Has("dokter")) {
			return
		}
		err = s.tx(r, func(tx *sql.Tx) error { return core.HapusResep(ctx, tx, r.FormValue("no_resep")) })
		msg, to = "Resep dihapus", to+"&tab=resep"
	case "lab":
		if !need(u.Has("dokter")) {
			return
		}
		dok := u.KdDokter
		if dok == "" {
			dok = reg.KdDokter
		}
		var nr string
		err = s.tx(r, func(tx *sql.Tx) (e error) {
			nr, e = core.PermintaanLab(ctx, tx, no, dok, r.FormValue("diagnosa_klinis"), r.Form["kd_jenis_prw"])
			return
		})
		msg, to = "Permintaan lab "+nr+" terkirim", to+"&tab=lab"
	case "biaya":
		if !need(u.Has("kasir", "perawat")) {
			return
		}
		b, _ := strconv.ParseFloat(r.FormValue("besar_biaya"), 64)
		err = s.tx(r, func(tx *sql.Tx) error { return core.TambahBiaya(ctx, tx, no, r.FormValue("nama_biaya"), b) })
		msg, to = "Biaya tambahan tersimpan", to+"&tab=tindakan"
	case "kamar-masuk":
		err = s.tx(r, func(tx *sql.Tx) error {
			return core.MasukKamar(ctx, tx, no, r.FormValue("kd_kamar"), r.FormValue("diagnosa_awal"), r.FormValue("kd_dokter"))
		})
		if err == nil {
			s.task(no, 5)
		}
		msg, to = "Pasien masuk kamar "+r.FormValue("kd_kamar"), to+"&tab=kamar"
	case "kamar-pindah":
		err = s.tx(r, func(tx *sql.Tx) error { return core.PindahKamar(ctx, tx, no, r.FormValue("kd_kamar")) })
		msg, to = "Pasien pindah ke "+r.FormValue("kd_kamar"), to+"&tab=kamar"
	case "kamar-pulang":
		if !need(klinis) {
			return
		}
		err = s.tx(r, func(tx *sql.Tx) error {
			return core.PulangRanap(ctx, tx, no, r.FormValue("stts_pulang"), r.FormValue("diagnosa_akhir"))
		})
		msg, to = "Pasien dipulangkan ("+r.FormValue("stts_pulang")+")", to+"&tab=kamar"
	default:
		http.NotFound(w, r)
		return
	}
	if err == nil {
		s.audit(r, "", "rawat_"+aksi, no, "")
	}
	s.back(w, r, to, err, msg)
}

func get(a []string, i int) string {
	if i < len(a) {
		return strings.TrimSpace(a[i])
	}
	return ""
}

// ------------------------------------------------------------------ rawat inap (bed management)

func (s *Server) ranapPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := map[string]any{"Title": "Rawat Inap"}
	d["Pasien"] = s.rows(ctx, `SELECT k.no_rawat, k.kd_kamar, b.nm_bangsal, m.kelas, k.tgl_masuk, k.jam_masuk, k.diagnosa_awal, GREATEST(1, DATEDIFF(CURDATE(), k.tgl_masuk)) hari,
		p.no_rkm_medis, p.nm_pasien, p.jk, CONCAT(r.umurdaftar,' ',r.sttsumur) umur, pj.png_jawab,
		(SELECT GROUP_CONCAT(d.nm_dokter SEPARATOR ', ') FROM dpjp_ranap x JOIN dokter d ON d.kd_dokter=x.kd_dokter WHERE x.no_rawat=k.no_rawat) dpjp
		FROM kamar_inap k JOIN reg_periksa r ON r.no_rawat=k.no_rawat JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis JOIN kamar m ON m.kd_kamar=k.kd_kamar JOIN bangsal b ON b.kd_bangsal=m.kd_bangsal JOIN penjab pj ON pj.kd_pj=r.kd_pj
		WHERE k.stts_pulang='-' ORDER BY b.nm_bangsal, k.kd_kamar`)
	d["Bed"] = s.rows(ctx, `SELECT b.nm_bangsal, k.kelas, COUNT(*) total, SUM(k.status='ISI') isi, SUM(k.status='KOSONG') kosong, SUM(k.status NOT IN ('ISI','KOSONG')) lain, MIN(k.trf_kamar) tarif
		FROM kamar k JOIN bangsal b ON b.kd_bangsal=k.kd_bangsal WHERE k.statusdata='1' GROUP BY b.nm_bangsal, k.kelas ORDER BY k.kelas, b.nm_bangsal`)
	d["Kamar"] = s.rows(ctx, `SELECT k.kd_kamar, b.nm_bangsal, k.kelas, k.status, k.trf_kamar,
		(SELECT p.nm_pasien FROM kamar_inap ki JOIN reg_periksa r ON r.no_rawat=ki.no_rawat JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis WHERE ki.kd_kamar=k.kd_kamar AND ki.stts_pulang='-' LIMIT 1) pasien
		FROM kamar k JOIN bangsal b ON b.kd_bangsal=k.kd_bangsal WHERE k.statusdata='1' ORDER BY b.nm_bangsal, k.kd_kamar`)
	d["Pulang"] = s.rows(ctx, `SELECT k.no_rawat, p.nm_pasien, k.kd_kamar, k.tgl_masuk, k.tgl_keluar, k.stts_pulang, r.status_bayar FROM kamar_inap k JOIN reg_periksa r ON r.no_rawat=k.no_rawat JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis
		WHERE k.stts_pulang NOT IN ('-','Pindah Kamar') AND k.tgl_keluar>=DATE_SUB(CURDATE(), INTERVAL 7 DAY) ORDER BY k.tgl_keluar DESC, k.jam_keluar DESC`)
	s.render(w, r, "ranap", d, 200)
}
