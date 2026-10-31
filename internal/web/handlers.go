package web

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yogaprasetia/simrs-go/internal/bpjs"
	"github.com/yogaprasetia/simrs-go/internal/core"
	"github.com/yogaprasetia/simrs-go/internal/db"
)

var (
	rDaftar  = []string{"pendaftaran"}
	rKlinis  = []string{"dokter", "perawat"}
	rDokter  = []string{"dokter"}
	rRanap   = []string{"dokter", "perawat", "pendaftaran"}
	rFarmasi = []string{"farmasi"}
	rLab     = []string{"lab"}
	rKasir   = []string{"kasir"}
	rBPJS    = []string{"bpjs", "pendaftaran"}
	rLaporan = []string{"manajemen"}
	rAdmin   = []string{"admin"}
	rAny     = []string{}
)

func (s *Server) routes() {
	m := s.mux
	p := s.Prefix
	st, _ := fs.Sub(staticFS, "static")
	m.Handle("GET "+p+"/static/", http.StripPrefix(p+"/static/", http.FileServerFS(st)))
	m.HandleFunc(p+"/login", s.loginPage)
	m.HandleFunc("POST "+p+"/logout", s.logout)
	m.HandleFunc("GET "+p+"/{$}", s.guard(rAny, s.dashboard))
	m.HandleFunc("GET "+p+"/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.DB.PingContext(r.Context()); err != nil {
			http.Error(w, "db down", 503)
			return
		}
		w.Write([]byte("ok"))
	})

	// API pencarian (autocomplete)
	m.HandleFunc("GET "+p+"/api/pasien", s.guard(rAny, s.apiPasien))
	m.HandleFunc("GET "+p+"/api/penyakit", s.guard(rAny, s.apiLookup("SELECT kd_penyakit AS kode, nm_penyakit AS nama FROM penyakit WHERE kd_penyakit LIKE ? OR nm_penyakit LIKE ? ORDER BY kd_penyakit LIMIT 20")))
	m.HandleFunc("GET "+p+"/api/icd9", s.guard(rAny, s.apiLookup("SELECT kode, deskripsi_panjang AS nama FROM icd9 WHERE kode LIKE ? OR deskripsi_panjang LIKE ? ORDER BY kode LIMIT 20")))
	m.HandleFunc("GET "+p+"/api/obat", s.guard(rAny, s.apiLookup("SELECT b.kode_brng AS kode, CONCAT(b.nama_brng,' (stok ',FORMAT(IFNULL(SUM(g.stok),0),0),' ',b.kode_sat,')') AS nama FROM databarang b LEFT JOIN gudangbarang g ON g.kode_brng=b.kode_brng AND g.kd_bangsal='AP' WHERE b.status='1' AND (b.kode_brng LIKE ? OR b.nama_brng LIKE ?) GROUP BY b.kode_brng ORDER BY b.nama_brng LIMIT 20")))
	m.HandleFunc("GET "+p+"/api/dokter", s.guard(rAny, s.apiDokter))

	m.HandleFunc("GET "+p+"/pasien", s.guard([]string{"pendaftaran", "dokter", "perawat"}, s.pasienList))
	m.HandleFunc("GET "+p+"/pasien/form", s.guard(rDaftar, s.pasienForm))
	m.HandleFunc("POST "+p+"/pasien/simpan", s.guard(rDaftar, s.pasienSimpan))
	m.HandleFunc("GET "+p+"/pasien/riwayat", s.guard([]string{"pendaftaran", "dokter", "perawat"}, s.pasienRiwayat))

	m.HandleFunc("GET "+p+"/registrasi", s.guard(rDaftar, s.registrasiPage))
	m.HandleFunc("POST "+p+"/registrasi", s.guard(rDaftar, s.registrasiSimpan))
	m.HandleFunc("POST "+p+"/registrasi/batal", s.guard(rDaftar, s.registrasiBatal))
	m.HandleFunc("GET "+p+"/registrasi/bukti", s.guard(rDaftar, s.registrasiBukti))

	m.HandleFunc("GET "+p+"/ralan", s.guard(rKlinis, s.ralanList))
	m.HandleFunc("GET "+p+"/rawat", s.guard(rRanap, s.rawatPage))
	m.HandleFunc("POST "+p+"/rawat/{aksi}", s.guard(rRanap, s.rawatAksi))

	m.HandleFunc("GET "+p+"/ranap", s.guard(rRanap, s.ranapPage))

	m.HandleFunc("GET "+p+"/farmasi", s.guard(rFarmasi, s.farmasiList))
	m.HandleFunc("GET "+p+"/farmasi/resep", s.guard(rFarmasi, s.farmasiResep))
	m.HandleFunc("POST "+p+"/farmasi/{aksi}", s.guard(rFarmasi, s.farmasiAksi))
	m.HandleFunc("GET "+p+"/farmasi/stok", s.guard(rFarmasi, s.farmasiStok))

	m.HandleFunc("GET "+p+"/lab", s.guard(rLab, s.labList))
	m.HandleFunc("GET "+p+"/lab/hasil", s.guard(rLab, s.labHasil))
	m.HandleFunc("POST "+p+"/lab/hasil", s.guard(rLab, s.labSimpan))
	m.HandleFunc("GET "+p+"/lab/cetak", s.guard([]string{"lab", "dokter", "perawat"}, s.labCetak))

	m.HandleFunc("GET "+p+"/kasir", s.guard(rKasir, s.kasirList))
	m.HandleFunc("GET "+p+"/kasir/billing", s.guard(rKasir, s.kasirBilling))
	m.HandleFunc("POST "+p+"/kasir/{aksi}", s.guard(rKasir, s.kasirAksi))
	m.HandleFunc("GET "+p+"/kasir/nota", s.guard(rKasir, s.kasirNota))

	m.HandleFunc("GET "+p+"/bpjs", s.guard(rBPJS, s.bpjsPage))
	m.HandleFunc("GET "+p+"/bpjs/sep", s.guard(rBPJS, s.bpjsSEPForm))
	m.HandleFunc("POST "+p+"/bpjs/{aksi}", s.guard(rBPJS, s.bpjsAksi))
	m.HandleFunc("GET "+p+"/bpjs/sep/cetak", s.guard(rBPJS, s.bpjsSEPCetak))

	m.HandleFunc("GET "+p+"/laporan", s.guard(rLaporan, s.laporan))
	s.routesPenunjang()
	m.HandleFunc("GET "+p+"/admin/users", s.guard(rAdmin, s.usersPage))
	m.HandleFunc("POST "+p+"/admin/users", s.guard(rAdmin, s.usersSimpan))
}

func (s *Server) tx(r *http.Request, fn func(*sql.Tx) error) error {
	return core.WithTx(r.Context(), s.DB, fn)
}

// ------------------------------------------------------------------ dashboard

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx, t := r.Context(), core.Today()
	d := map[string]any{"Title": "Dashboard"}
	d["Kunjungan"] = s.val(ctx, "SELECT COUNT(*) FROM reg_periksa WHERE tgl_registrasi=? AND stts<>'Batal'", t)
	d["BelumPeriksa"] = s.val(ctx, "SELECT COUNT(*) FROM reg_periksa WHERE tgl_registrasi=? AND stts='Belum'", t)
	d["Ranap"] = s.val(ctx, "SELECT COUNT(*) FROM kamar_inap WHERE stts_pulang='-'")
	d["Bed"] = s.val(ctx, "SELECT COUNT(*) FROM kamar WHERE statusdata='1'")
	d["Resep"] = s.val(ctx, "SELECT COUNT(*) FROM resep_obat WHERE tgl_perawatan IS NULL")
	d["Lab"] = s.val(ctx, "SELECT COUNT(*) FROM permintaan_lab WHERE tgl_hasil IS NULL")
	d["Rad"] = s.val(ctx, "SELECT COUNT(*) FROM permintaan_radiologi WHERE tgl_hasil IS NULL")
	d["OK"] = s.val(ctx, "SELECT COUNT(*) FROM booking_operasi WHERE tanggal=? AND status<>'Selesai'", t)
	d["BelumBayar"] = s.val(ctx, "SELECT COUNT(*) FROM reg_periksa WHERE status_bayar='Belum Bayar' AND stts NOT IN ('Batal','Belum') AND tgl_registrasi>=DATE_SUB(?, INTERVAL 30 DAY)", t)
	d["Pendapatan"] = s.val(ctx, "SELECT IFNULL(SUM(totalbiaya),0) FROM billing WHERE tgl_byr=? AND status='Tagihan'", t)
	d["PerPoli"] = s.rows(ctx, `SELECT pl.nm_poli, COUNT(*) jml, SUM(r.stts='Belum') belum FROM reg_periksa r JOIN poliklinik pl ON pl.kd_poli=r.kd_poli WHERE r.tgl_registrasi=? AND r.stts<>'Batal' GROUP BY pl.nm_poli ORDER BY jml DESC`, t)
	d["PerBayar"] = s.rows(ctx, `SELECT pj.png_jawab, COUNT(*) jml FROM reg_periksa r JOIN penjab pj ON pj.kd_pj=r.kd_pj WHERE r.tgl_registrasi=? AND r.stts<>'Batal' GROUP BY pj.png_jawab ORDER BY jml DESC`, t)
	s.render(w, r, "dashboard", d, 200)
}

// ------------------------------------------------------------------ API

func (s *Server) apiPasien(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		s.json(w, []Row{})
		return
	}
	like := "%" + q + "%"
	s.json(w, s.rows(r.Context(), `SELECT no_rkm_medis, nm_pasien, jk, tgl_lahir, alamat, no_ktp, no_peserta, kd_pj FROM pasien
		WHERE no_rkm_medis=? OR no_ktp=? OR no_peserta=? OR nm_pasien LIKE ? ORDER BY nm_pasien LIMIT 20`, q, q, q, like))
}

func (s *Server) apiLookup(query string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		s.json(w, s.rows(r.Context(), query, q+"%", "%"+q+"%"))
	}
}

var hariID = []string{"AKHAD", "SENIN", "SELASA", "RABU", "KAMIS", "JUMAT", "SABTU"}

// apiDokter: dokter yang praktik di poli pada hari ini (jadwal); fallback semua dokter.
func (s *Server) apiDokter(w http.ResponseWriter, r *http.Request) {
	poli := r.URL.Query().Get("poli")
	hari := hariID[core.Now().Weekday()]
	list := s.rows(r.Context(), `SELECT d.kd_dokter, d.nm_dokter, CONCAT(TIME_FORMAT(j.jam_mulai,'%H:%i'),'-',TIME_FORMAT(j.jam_selesai,'%H:%i')) jam, j.kuota,
		(SELECT COUNT(*) FROM reg_periksa r WHERE r.kd_dokter=d.kd_dokter AND r.kd_poli=j.kd_poli AND r.tgl_registrasi=CURDATE() AND r.stts<>'Batal') terdaftar
		FROM jadwal j JOIN dokter d ON d.kd_dokter=j.kd_dokter WHERE j.kd_poli=? AND j.hari_kerja=? AND d.status='1' ORDER BY d.nm_dokter`, poli, hari)
	if len(list) == 0 {
		list = s.rows(r.Context(), "SELECT kd_dokter, nm_dokter, '' jam, '0' kuota, '0' terdaftar FROM dokter WHERE status='1' ORDER BY nm_dokter")
	}
	s.json(w, list)
}

// ------------------------------------------------------------------ pasien

func (s *Server) pasienList(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var list []Row
	if q != "" {
		list = s.rows(r.Context(), `SELECT p.*, pj.png_jawab FROM pasien p JOIN penjab pj ON pj.kd_pj=p.kd_pj WHERE p.no_rkm_medis=? OR p.no_ktp=? OR p.no_peserta=? OR p.nm_pasien LIKE ? OR p.alamat LIKE ? ORDER BY p.nm_pasien LIMIT 100`, q, q, q, "%"+q+"%", "%"+q+"%")
	} else {
		list = s.rows(r.Context(), `SELECT p.*, pj.png_jawab FROM pasien p JOIN penjab pj ON pj.kd_pj=p.kd_pj ORDER BY p.tgl_daftar DESC, p.no_rkm_medis DESC LIMIT 50`)
	}
	s.render(w, r, "pasien", map[string]any{"Title": "Data Pasien", "List": list, "Q": q}, 200)
}

func (s *Server) masters(ctx context.Context) map[string]any {
	return map[string]any{
		"Penjab": s.rows(ctx, "SELECT kd_pj, png_jawab FROM penjab WHERE status='1' ORDER BY png_jawab"),
		"Poli":   s.rows(ctx, "SELECT kd_poli, nm_poli, registrasi, registrasilama FROM poliklinik WHERE status='1' ORDER BY nm_poli"),
		"Dokter": s.rows(ctx, "SELECT kd_dokter, nm_dokter FROM dokter WHERE status='1' ORDER BY nm_dokter"),
	}
}

func (s *Server) pasienForm(w http.ResponseWriter, r *http.Request) {
	d := s.masters(r.Context())
	d["Title"] = "Pasien Baru"
	d["P_"] = Row{"jk": "L", "kd_pj": "A09", "stts_nikah": "BELUM MENIKAH", "keluarga": "DIRI SENDIRI", "gol_darah": "-", "pnd": "-"}
	if rm := r.URL.Query().Get("rm"); rm != "" {
		if p := s.row(r.Context(), "SELECT * FROM pasien WHERE no_rkm_medis=?", rm); p != nil {
			d["P_"], d["Title"] = p, "Ubah Pasien "+rm
		}
	}
	d["Next"] = r.URL.Query().Get("next")
	s.render(w, r, "pasien_form", d, 200)
}

func formPasien(r *http.Request) *core.Pasien {
	f := r.FormValue
	return &core.Pasien{NoRM: f("no_rkm_medis"), Nama: f("nm_pasien"), NoKTP: strings.TrimSpace(f("no_ktp")), JK: f("jk"), TmpLahir: f("tmp_lahir"), TglLahir: f("tgl_lahir"),
		NmIbu: f("nm_ibu"), Alamat: f("alamat"), GolDarah: f("gol_darah"), Pekerjaan: f("pekerjaan"), SttsNikah: f("stts_nikah"), Agama: f("agama"), NoTlp: f("no_tlp"),
		Pnd: f("pnd"), Keluarga: f("keluarga"), NamaKeluarga: f("namakeluarga"), KdPJ: f("kd_pj"), NoPeserta: strings.TrimSpace(f("no_peserta"))}
}

func (s *Server) pasienSimpan(w http.ResponseWriter, r *http.Request) {
	p := formPasien(r)
	baru := p.NoRM == ""
	err := s.tx(r, func(tx *sql.Tx) error { return core.SimpanPasien(r.Context(), tx, p) })
	if err != nil {
		d := s.masters(r.Context())
		d["Title"], d["Err"] = "Pasien", err.Error()
		row := Row{}
		for k, v := range r.PostForm {
			row[k] = v[0]
		}
		d["P_"] = row
		d["Next"] = r.FormValue("next")
		s.render(w, r, "pasien_form", d, 400)
		return
	}
	s.audit(r, "", map[bool]string{true: "pasien_baru", false: "pasien_ubah"}[baru], p.NoRM, p.Nama)
	if r.FormValue("next") == "registrasi" {
		http.Redirect(w, r, s.Prefix+"/registrasi?rm="+p.NoRM+"&msg="+"Pasien+"+p.NoRM+"+tersimpan", http.StatusSeeOther)
		return
	}
	s.back(w, r, s.Prefix+"/pasien?q="+p.NoRM, nil, "Data pasien "+p.NoRM+" tersimpan")
}

func (s *Server) pasienRiwayat(w http.ResponseWriter, r *http.Request) {
	rm := r.URL.Query().Get("rm")
	ctx := r.Context()
	p := s.row(ctx, "SELECT p.*, pj.png_jawab FROM pasien p JOIN penjab pj ON pj.kd_pj=p.kd_pj WHERE no_rkm_medis=?", rm)
	if p == nil {
		s.back(w, r, s.Prefix+"/pasien", errors.New("pasien tidak ditemukan"), "")
		return
	}
	visits := s.rows(ctx, `SELECT r.no_rawat, r.tgl_registrasi, r.status_lanjut, pl.nm_poli, d.nm_dokter, pj.png_jawab, r.stts, r.status_bayar,
		(SELECT GROUP_CONCAT(CONCAT(dp.kd_penyakit,' ',py.nm_penyakit) ORDER BY dp.prioritas SEPARATOR '; ') FROM diagnosa_pasien dp JOIN penyakit py ON py.kd_penyakit=dp.kd_penyakit WHERE dp.no_rawat=r.no_rawat) diagnosa
		FROM reg_periksa r JOIN poliklinik pl ON pl.kd_poli=r.kd_poli JOIN dokter d ON d.kd_dokter=r.kd_dokter JOIN penjab pj ON pj.kd_pj=r.kd_pj WHERE r.no_rkm_medis=? ORDER BY r.tgl_registrasi DESC, r.jam_reg DESC`, rm)
	s.render(w, r, "pasien_riwayat", map[string]any{"Title": "Riwayat " + p["nm_pasien"], "P_": p, "Visits": visits}, 200)
}

// ------------------------------------------------------------------ registrasi

func (s *Server) registrasiPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := s.masters(ctx)
	d["Title"] = "Registrasi Pasien"
	tgl := r.URL.Query().Get("tgl")
	if tgl == "" {
		tgl = core.Today()
	}
	d["Tgl"] = tgl
	if rm := r.URL.Query().Get("rm"); rm != "" {
		d["Pasien"] = s.row(ctx, "SELECT no_rkm_medis, nm_pasien, jk, tgl_lahir, alamat, kd_pj, no_peserta, namakeluarga, keluarga FROM pasien WHERE no_rkm_medis=?", rm)
	}
	d["List"] = s.rows(ctx, `SELECT r.no_rawat, r.no_reg, r.jam_reg, r.no_rkm_medis, p.nm_pasien, pl.nm_poli, d.nm_dokter, pj.png_jawab, r.stts, r.stts_daftar, r.status_lanjut, r.biaya_reg, r.kd_pj,
		(SELECT no_sep FROM bridging_sep b WHERE b.no_rawat=r.no_rawat LIMIT 1) no_sep
		FROM reg_periksa r JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis JOIN poliklinik pl ON pl.kd_poli=r.kd_poli JOIN dokter d ON d.kd_dokter=r.kd_dokter JOIN penjab pj ON pj.kd_pj=r.kd_pj
		WHERE r.tgl_registrasi=? ORDER BY r.jam_reg DESC`, tgl)
	s.render(w, r, "registrasi", d, 200)
}

func (s *Server) registrasiSimpan(w http.ResponseWriter, r *http.Request) {
	in := core.RegInput{NoRM: r.FormValue("no_rkm_medis"), KdPoli: r.FormValue("kd_poli"), KdDokter: r.FormValue("kd_dokter"), KdPJ: r.FormValue("kd_pj"),
		PJawab: r.FormValue("p_jawab"), AlmtPJ: r.FormValue("almt_pj"), HubunganPJ: r.FormValue("hubunganpj")}
	var res core.RegResult
	err := s.tx(r, func(tx *sql.Tx) (e error) { res, e = core.Registrasi(r.Context(), tx, in); return })
	if err != nil {
		s.back(w, r, s.Prefix+"/registrasi?rm="+in.NoRM, err, "")
		return
	}
	s.audit(r, "", "registrasi", res.NoRawat, in.NoRM+" "+in.KdPoli)
	msg := "Terdaftar: " + res.NoRawat + " · antrean " + res.NoReg + " · " + rupiah(res.Biaya)
	// antrean Mobile JKN untuk pasien BPJS (task 1-3 dicatat sebagai check-in)
	if in.KdPJ == "BPJ" && s.BPJS.Cfg.Enabled() {
		if err := s.kirimAntrean(r.Context(), res.NoRawat); err != nil {
			msg += " · Antrean BPJS gagal: " + err.Error()
		} else {
			msg += " · Antrean BPJS terkirim"
		}
	}
	http.Redirect(w, r, s.Prefix+"/registrasi?msg="+urlq(msg)+"&last="+urlq(res.NoRawat), http.StatusSeeOther)
}

func urlq(s string) string {
	return strings.NewReplacer(" ", "+", "&", "%26", "#", "%23", "/", "%2F").Replace(s)
}

func (s *Server) registrasiBatal(w http.ResponseWriter, r *http.Request) {
	no := r.FormValue("no_rawat")
	err := s.tx(r, func(tx *sql.Tx) error { return core.BatalRegistrasi(r.Context(), tx, no) })
	if err == nil {
		s.audit(r, "", "registrasi_batal", no, "")
		if s.BPJS.Cfg.Enabled() && s.val(r.Context(), "SELECT COUNT(*) FROM referensi_mobilejkn_bpjs_taskid WHERE no_rawat=?", no) != "0" {
			go func() {
				ctx, c := context.WithTimeout(context.Background(), 20*time.Second)
				defer c()
				if e := s.BPJS.BatalAntrean(ctx, no, "Batal registrasi"); e == nil {
					core.CatatTask(ctx, s.DB, no, "99")
				}
			}()
		}
	}
	s.back(w, r, "", err, "Registrasi "+no+" dibatalkan")
}

func (s *Server) registrasiBukti(w http.ResponseWriter, r *http.Request) {
	reg, err := core.GetReg(r.Context(), s.DB, r.URL.Query().Get("no"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, "print_bukti", map[string]any{"Title": "Bukti Registrasi", "R": reg, "Print": true}, 200)
}

// ------------------------------------------------------------------ BPJS antrean helper

func (s *Server) kirimAntrean(ctx context.Context, noRawat string) error {
	reg, err := core.GetReg(ctx, s.DB, noRawat)
	if err != nil {
		return err
	}
	x := s.row(ctx, `SELECT p.no_ktp, p.no_tlp, pl.kd_poli_bpjs, d.kd_dokter_bpjs, r.stts_daftar,
		IFNULL((SELECT CONCAT(TIME_FORMAT(jam_mulai,'%H:%i'),'-',TIME_FORMAT(jam_selesai,'%H:%i')) FROM jadwal j WHERE j.kd_dokter=r.kd_dokter AND j.kd_poli=r.kd_poli LIMIT 1),'08:00-14:00') jam,
		IFNULL((SELECT kuota FROM jadwal j WHERE j.kd_dokter=r.kd_dokter AND j.kd_poli=r.kd_poli LIMIT 1),30) kuota
		FROM reg_periksa r JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis JOIN poliklinik pl ON pl.kd_poli=r.kd_poli JOIN dokter d ON d.kd_dokter=r.kd_dokter WHERE r.no_rawat=?`, noRawat)
	kd, _ := strconv.Atoi(x["kd_dokter_bpjs"])
	angka, _ := strconv.Atoi(reg.NoReg)
	kuota, _ := strconv.Atoi(x["kuota"])
	jamMulai, _ := time.ParseInLocation("2006-01-02 15:04", reg.Tgl+" "+strings.Split(x["jam"], "-")[0], core.Loc)
	estimasi := jamMulai.Add(time.Duration(angka-1) * 10 * time.Minute)
	sep := s.row(ctx, "SELECT no_rujukan FROM bridging_sep WHERE no_rawat=? LIMIT 1", noRawat)
	ref := ""
	if sep != nil {
		ref = sep["no_rujukan"]
	}
	a := bpjs.AntreanAdd{KodeBooking: noRawat, JenisPasien: "JKN", NomorKartu: reg.NoPeserta, NIK: x["no_ktp"], NoHP: x["no_tlp"], KodePoli: x["kd_poli_bpjs"], NamaPoli: reg.NmPoli,
		PasienBaru: map[bool]int{true: 1, false: 0}[x["stts_daftar"] == "Baru"], NoRM: reg.NoRM, TanggalPeriksa: reg.Tgl, KodeDokter: kd, NamaDokter: reg.NmDokter, JamPraktek: x["jam"],
		JenisKunjungan: 1, NomorReferensi: ref, NomorAntrean: reg.KdPoli + "-" + reg.NoReg, AngkaAntrean: angka, EstimasiDilayani: estimasi.UnixMilli(),
		SisaKuotaJKN: kuota - angka, KuotaJKN: kuota, SisaKuotaNonJKN: kuota - angka, KuotaNonJKN: kuota, Keterangan: "Peserta harap datang 30 menit sebelum pelayanan"}
	if err := s.BPJS.TambahAntrean(ctx, a); err != nil {
		return err
	}
	now := core.Now()
	for _, t := range []int{1, 2, 3} {
		if e := s.BPJS.UpdateWaktu(ctx, noRawat, t, now); e == nil {
			core.CatatTask(ctx, s.DB, noRawat, strconv.Itoa(t))
		}
	}
	return nil
}

// task kirim task id antrean (async) bila pasien punya antrean BPJS.
func (s *Server) task(noRawat string, id int) {
	if !s.BPJS.Cfg.Enabled() {
		return
	}
	ctx := context.Background()
	if s.val(ctx, "SELECT COUNT(*) FROM referensi_mobilejkn_bpjs_taskid WHERE no_rawat=? AND taskid='3'", noRawat) == "0" {
		return
	}
	if s.val(ctx, "SELECT COUNT(*) FROM referensi_mobilejkn_bpjs_taskid WHERE no_rawat=? AND taskid=?", noRawat, strconv.Itoa(id)) != "0" {
		return
	}
	go func() {
		c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.BPJS.UpdateWaktu(c, noRawat, id, core.Now()); err != nil {
			log.Printf("task %d %s: %v", id, noRawat, err)
			return
		}
		core.CatatTask(c, s.DB, noRawat, strconv.Itoa(id))
	}()
}

// ------------------------------------------------------------------ users

func (s *Server) usersPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "users", map[string]any{"Title": "Pengguna", "List": s.rows(r.Context(), "SELECT u.*, d.nm_dokter FROM app_user u LEFT JOIN dokter d ON d.kd_dokter=u.kd_dokter ORDER BY id_user"),
		"Roles": db.Roles, "Dokter": s.rows(r.Context(), "SELECT kd_dokter, nm_dokter FROM dokter ORDER BY nm_dokter")}, 200)
}

func (s *Server) usersSimpan(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	id := strings.TrimSpace(r.FormValue("id_user"))
	if r.FormValue("hapus") == "1" {
		if id == userOf(r).ID {
			s.back(w, r, "", errors.New("tidak bisa menonaktifkan diri sendiri"), "")
			return
		}
		_, err := s.DB.ExecContext(r.Context(), "UPDATE app_user SET aktif=IF(aktif='1','0','1') WHERE id_user=?", id)
		s.DB.ExecContext(r.Context(), "DELETE FROM app_session WHERE id_user=?", id)
		s.back(w, r, "", err, "Status user "+id+" diubah")
		return
	}
	roles := strings.Join(r.Form["roles"], ",")
	pw := r.FormValue("password")
	if id == "" || roles == "" {
		s.back(w, r, "", errors.New("ID user dan minimal satu peran wajib diisi"), "")
		return
	}
	exists := s.val(r.Context(), "SELECT COUNT(*) FROM app_user WHERE id_user=?", id) != "0"
	if pw == "" && exists {
		_, err := s.DB.ExecContext(r.Context(), "UPDATE app_user SET nama=?, roles=?, kd_dokter=? WHERE id_user=?", r.FormValue("nama"), roles, r.FormValue("kd_dokter"), id)
		s.back(w, r, "", err, "User "+id+" diperbarui")
		return
	}
	if len(pw) < 8 {
		s.back(w, r, "", errors.New("password minimal 8 karakter"), "")
		return
	}
	err := db.UpsertUser(r.Context(), s.DB, id, r.FormValue("nama"), pw, roles, r.FormValue("kd_dokter"))
	s.audit(r, "", "user_simpan", id, roles)
	s.back(w, r, "", err, "User "+id+" tersimpan")
}
