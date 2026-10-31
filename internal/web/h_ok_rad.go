package web

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/yogaprasetia/simrs-go/internal/core"
)

var (
	rRad      = []string{"radiologi"}
	rRadLihat = []string{"radiologi", "dokter", "perawat"}
	rOK       = []string{"ok", "dokter"}
	rOKLihat  = []string{"ok", "dokter", "perawat", "kasir"}
)

func (s *Server) routesPenunjang() {
	m, p := s.mux, s.Prefix
	m.HandleFunc("GET "+p+"/radiologi", s.guard(rRad, s.radList))
	m.HandleFunc("GET "+p+"/radiologi/periksa", s.guard(rRad, s.radPeriksa))
	m.HandleFunc("POST "+p+"/radiologi/{aksi}", s.limitBody(s.guard(rRad, s.radAksi)))
	m.HandleFunc("GET "+p+"/radiologi/gambar/{file}", s.guard(rRadLihat, s.radGambar))
	m.HandleFunc("GET "+p+"/radiologi/cetak", s.guard(rRadLihat, s.radCetak))

	m.HandleFunc("GET "+p+"/operasi", s.guard(rOKLihat, s.okJadwal))
	m.HandleFunc("GET "+p+"/operasi/input", s.guard(rOKLihat, s.okInput))
	m.HandleFunc("POST "+p+"/operasi/{aksi}", s.guard(rOK, s.okAksi))
	m.HandleFunc("GET "+p+"/operasi/laporan", s.guard(rOKLihat, s.okLaporanCetak))
}

// limitBody membatasi upload sebelum guard mem-parse form multipart.
func (s *Server) limitBody(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 21<<20)
		h(w, r)
	}
}

// ------------------------------------------------------------------ radiologi

func (s *Server) radList(w http.ResponseWriter, r *http.Request) {
	st := r.URL.Query().Get("st")
	where := map[string]string{
		"":          "l.tgl_sampel IS NULL",
		"expertise": "l.tgl_sampel IS NOT NULL AND l.tgl_hasil IS NULL",
		"selesai":   "l.tgl_hasil IS NOT NULL AND l.tgl_hasil>=DATE_SUB(CURDATE(), INTERVAL 7 DAY)",
	}[st]
	if where == "" {
		where, st = "l.tgl_sampel IS NULL", ""
	}
	list := s.rows(r.Context(), `SELECT l.noorder, l.no_rawat, l.tgl_permintaan, l.jam_permintaan, l.tgl_sampel, l.tgl_hasil, l.status, l.diagnosa_klinis, l.informasi_tambahan, p.nm_pasien, p.no_rkm_medis, p.jk, d.nm_dokter,
		(SELECT GROUP_CONCAT(j.nm_perawatan SEPARATOR ', ') FROM permintaan_pemeriksaan_radiologi x JOIN jns_perawatan_radiologi j ON j.kd_jenis_prw=x.kd_jenis_prw WHERE x.noorder=l.noorder) isi
		FROM permintaan_radiologi l JOIN reg_periksa r ON r.no_rawat=l.no_rawat JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis JOIN dokter d ON d.kd_dokter=l.dokter_perujuk
		WHERE `+where+` ORDER BY l.tgl_permintaan, l.jam_permintaan`)
	s.render(w, r, "radiologi", map[string]any{"Title": "Radiologi", "List": list, "St": st}, 200)
}

func (s *Server) radOrder(r *http.Request, no string) (Row, []Row, []Row, Row) {
	ctx := r.Context()
	o := s.row(ctx, `SELECT l.*, p.nm_pasien, p.no_rkm_medis, p.jk, p.tgl_lahir, CONCAT(r.umurdaftar,' ',r.sttsumur) umur, d.nm_dokter, pl.nm_poli, pj.png_jawab, r.status_bayar,
		a.tgl_periksa, a.jam jam_periksa FROM permintaan_radiologi l JOIN reg_periksa r ON r.no_rawat=l.no_rawat JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis
		JOIN dokter d ON d.kd_dokter=l.dokter_perujuk JOIN poliklinik pl ON pl.kd_poli=r.kd_poli JOIN penjab pj ON pj.kd_pj=r.kd_pj LEFT JOIN app_radiologi_order a ON a.noorder=l.noorder WHERE l.noorder=?`, no)
	if o == nil {
		return nil, nil, nil, nil
	}
	items := s.rows(ctx, `SELECT j.kd_jenis_prw, j.nm_perawatan, j.total_byr, pr.proyeksi, pr.kV, pr.mAS, pr.FFD, pr.BSF, pr.inak, pr.jml_penyinaran, pr.dosis, pr.biaya, pt.nama radiografer, dr.nm_dokter dokter_rad
		FROM permintaan_pemeriksaan_radiologi x JOIN jns_perawatan_radiologi j ON j.kd_jenis_prw=x.kd_jenis_prw
		LEFT JOIN periksa_radiologi pr ON pr.no_rawat=? AND pr.kd_jenis_prw=x.kd_jenis_prw AND pr.tgl_periksa=? AND pr.jam=?
		LEFT JOIN petugas pt ON pt.nip=pr.nip LEFT JOIN dokter dr ON dr.kd_dokter=pr.kd_dokter WHERE x.noorder=? ORDER BY j.nm_perawatan`, o["no_rawat"], o["tgl_periksa"], o["jam_periksa"], no)
	var img []Row
	var hasil Row
	if o["tgl_periksa"] != "" {
		img = s.rows(ctx, "SELECT lokasi_gambar FROM gambar_radiologi WHERE no_rawat=? AND tgl_periksa=? AND jam=?", o["no_rawat"], o["tgl_periksa"], o["jam_periksa"])
		for _, g := range img {
			g["file"] = filepath.Base(g["lokasi_gambar"])
			g["pdf"] = strconv.FormatBool(strings.HasSuffix(g["file"], ".pdf"))
		}
		hasil = s.row(ctx, "SELECT hasil FROM hasil_radiologi WHERE no_rawat=? AND tgl_periksa=? AND jam=?", o["no_rawat"], o["tgl_periksa"], o["jam_periksa"])
	}
	return o, items, img, hasil
}

func (s *Server) radPeriksa(w http.ResponseWriter, r *http.Request) {
	o, items, img, hasil := s.radOrder(r, r.URL.Query().Get("no"))
	if o == nil {
		s.back(w, r, s.Prefix+"/radiologi", errors.New("order tidak ditemukan"), "")
		return
	}
	ctx := r.Context()
	s.render(w, r, "radiologi_periksa", map[string]any{"Title": "Radiologi " + o["noorder"], "O": o, "Items": items, "Img": img, "Hasil": hasil,
		"Petugas": s.rows(ctx, "SELECT nip, nama FROM petugas WHERE status='1' AND jbtn LIKE '%Radiograf%' ORDER BY nama"),
		"Dokter":  s.rows(ctx, "SELECT kd_dokter, nm_dokter FROM dokter WHERE status='1' ORDER BY nm_dokter LIKE '%Sp.Rad%' DESC, nm_dokter")}, 200)
}

var imgName = regexp.MustCompile(`^[0-9a-f]{32}\.(jpg|png|webp|pdf)$`)

var allowedMime = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "application/pdf": ".pdf"}

func (s *Server) radAksi(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	no := r.FormValue("noorder")
	to := s.Prefix + "/radiologi/periksa?no=" + no
	var err error
	var msg string
	switch r.PathValue("aksi") {
	case "periksa":
		r.ParseForm()
		pap := map[string]core.Paparan{}
		for _, kd := range r.Form["kd"] {
			f := func(k string) string { return strings.TrimSpace(r.FormValue(k + "_" + kd)) }
			pap[kd] = core.Paparan{Proyeksi: f("proyeksi"), KV: f("kv"), MAS: f("mas"), FFD: f("ffd"), BSF: f("bsf"), Inak: f("inak"), JmlPenyinaran: f("jml"), Dosis: f("dosis")}
		}
		err = s.tx(r, func(tx *sql.Tx) error {
			return core.PeriksaRadiologi(ctx, tx, no, r.FormValue("nip"), r.FormValue("kd_dokter"), pap)
		})
		msg = "Pemeriksaan tersimpan, tarif masuk tagihan"
	case "expertise":
		err = s.tx(r, func(tx *sql.Tx) error { return core.SimpanExpertise(ctx, tx, no, r.FormValue("hasil")) })
		msg = "Hasil bacaan tersimpan"
	case "gambar":
		var n int
		n, err = s.simpanGambar(r, no)
		msg = strconv.Itoa(n) + " file gambar tersimpan"
	default:
		http.NotFound(w, r)
		return
	}
	if err == nil {
		s.audit(r, "", "radiologi_"+r.PathValue("aksi"), no, "")
	}
	s.back(w, r, to, err, msg)
}

func (s *Server) simpanGambar(r *http.Request, noorder string) (int, error) {
	if r.MultipartForm == nil || len(r.MultipartForm.File["gambar"]) == 0 {
		return 0, errors.New("pilih file gambar (JPG/PNG/WEBP/PDF, maks. 20 MB)")
	}
	dir := filepath.Join(s.UploadDir, "radiologi")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return 0, err
	}
	n := 0
	for _, fh := range r.MultipartForm.File["gambar"] {
		f, err := fh.Open()
		if err != nil {
			return n, err
		}
		head := make([]byte, 512)
		k, _ := io.ReadFull(f, head)
		ext, ok := allowedMime[http.DetectContentType(head[:k])]
		if !ok {
			f.Close()
			return n, errors.New(fh.Filename + ": format tidak didukung (hanya JPG/PNG/WEBP/PDF)")
		}
		b := make([]byte, 16)
		rand.Read(b)
		name := hex.EncodeToString(b) + ext
		path := filepath.Join(dir, name)
		out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
		if err != nil {
			f.Close()
			return n, err
		}
		_, err = out.Write(head[:k])
		if err == nil {
			_, err = io.Copy(out, f)
		}
		out.Close()
		f.Close()
		if err != nil {
			os.Remove(path)
			return n, err
		}
		if err := s.tx(r, func(tx *sql.Tx) error { return core.TambahGambar(r.Context(), tx, noorder, "radiologi/"+name) }); err != nil {
			os.Remove(path)
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Server) radGambar(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !imgName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	if s.val(r.Context(), "SELECT COUNT(*) FROM gambar_radiologi WHERE lokasi_gambar=?", "radiologi/"+name) == "0" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'")
	http.ServeFile(w, r, filepath.Join(s.UploadDir, "radiologi", name))
}

func (s *Server) radCetak(w http.ResponseWriter, r *http.Request) {
	o, items, img, hasil := s.radOrder(r, r.URL.Query().Get("no"))
	if o == nil || hasil == nil {
		s.fail(w, r, errors.New("hasil radiologi belum tersedia"))
		return
	}
	s.render(w, r, "print_radiologi", map[string]any{"Title": "Hasil Radiologi", "O": o, "Items": items, "Img": img, "Hasil": hasil, "Print": true}, 200)
}

// ------------------------------------------------------------------ kamar operasi / VK

func (s *Server) okJadwal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tgl := r.URL.Query().Get("tgl")
	if tgl == "" {
		tgl = core.Today()
	}
	list := s.rows(ctx, `SELECT b.*, TIME_FORMAT(b.jam_mulai,'%H:%i') mulai, TIME_FORMAT(b.jam_selesai,'%H:%i') selesai, p.nm_perawatan, p.kategori, o.nm_ruang_ok, d.nm_dokter,
		ps.nm_pasien, ps.no_rkm_medis, ps.jk, CONCAT(r.umurdaftar,' ',r.sttsumur) umur, r.status_lanjut, pj.png_jawab
		FROM booking_operasi b JOIN paket_operasi p ON p.kode_paket=b.kode_paket JOIN ruang_ok o ON o.kd_ruang_ok=b.kd_ruang_ok LEFT JOIN dokter d ON d.kd_dokter=b.kd_dokter
		JOIN reg_periksa r ON r.no_rawat=b.no_rawat JOIN pasien ps ON ps.no_rkm_medis=r.no_rkm_medis JOIN penjab pj ON pj.kd_pj=r.kd_pj
		WHERE b.tanggal=? ORDER BY b.kd_ruang_ok, b.jam_mulai`, tgl)
	ruang := s.rows(ctx, "SELECT * FROM ruang_ok ORDER BY kd_ruang_ok")
	// garis waktu 07:00-21:00 per ruang
	for _, b := range list {
		m := menit(b["mulai"])
		e := menit(b["selesai"])
		if e <= m {
			e = m + 60
		}
		b["left"] = strconv.FormatFloat(float64(m-420)*100/840, 'f', 2, 64)
		b["width"] = strconv.FormatFloat(float64(e-m)*100/840, 'f', 2, 64)
	}
	done := s.rows(ctx, `SELECT o.no_rawat, o.tgl_operasi, p.nm_perawatan, ps.nm_pasien, d.nm_dokter operator, o.jenis_anasthesi, `+core.BiayaOperasiExpr()+` total,
		(SELECT COUNT(*) FROM laporan_operasi l WHERE l.no_rawat=o.no_rawat AND l.tanggal=o.tgl_operasi) ada_laporan
		FROM operasi o JOIN paket_operasi p ON p.kode_paket=o.kode_paket JOIN reg_periksa r ON r.no_rawat=o.no_rawat JOIN pasien ps ON ps.no_rkm_medis=r.no_rkm_medis LEFT JOIN dokter d ON d.kd_dokter=o.operator1
		WHERE DATE(o.tgl_operasi)=? ORDER BY o.tgl_operasi`, tgl)
	s.render(w, r, "operasi", map[string]any{"Title": "Kamar Operasi / VK", "Tgl": tgl, "List": list, "Ruang": ruang, "Done": done,
		"Jam": []string{"07", "09", "11", "13", "15", "17", "19", "21"}}, 200)
}

func menit(hhmm string) int {
	if len(hhmm) < 5 {
		return 0
	}
	h, _ := strconv.Atoi(hhmm[:2])
	m, _ := strconv.Atoi(hhmm[3:5])
	return h*60 + m
}

func (s *Server) okInput(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	no := r.URL.Query().Get("no")
	reg, err := core.GetReg(ctx, s.DB, no)
	if err != nil {
		s.back(w, r, s.Prefix+"/operasi", err, "")
		return
	}
	d := map[string]any{"Title": "Operasi " + reg.Nama, "R": reg, "Locked": reg.StatusBayar == "Sudah Bayar" || reg.Stts == "Batal", "PaketSel": r.URL.Query().Get("paket")}
	d["Paket"] = s.rows(ctx, "SELECT kode_paket, nm_perawatan, kategori FROM paket_operasi WHERE status='1' ORDER BY kategori DESC, nm_perawatan")
	d["Dokter"] = s.rows(ctx, "SELECT kd_dokter id, nm_dokter nama FROM dokter WHERE status='1' ORDER BY nm_dokter")
	d["Petugas"] = s.rows(ctx, "SELECT nip id, CONCAT(nama,' (',jbtn,')') nama FROM petugas WHERE status='1' ORDER BY nama")
	d["Peran"] = core.PeranOperasi
	d["Kategori"] = core.KategoriOperasi
	d["Anestesi"] = core.JenisAnestesi
	d["Booking"] = s.rows(ctx, `SELECT b.*, TIME_FORMAT(b.jam_mulai,'%H:%i') mulai, TIME_FORMAT(b.jam_selesai,'%H:%i') selesai, p.nm_perawatan, o.nm_ruang_ok FROM booking_operasi b
		JOIN paket_operasi p ON p.kode_paket=b.kode_paket JOIN ruang_ok o ON o.kd_ruang_ok=b.kd_ruang_ok WHERE b.no_rawat=? ORDER BY b.tanggal, b.jam_mulai`, no)
	ops := s.rows(ctx, `SELECT o.*, p.nm_perawatan, `+core.BiayaOperasiExpr()+` total, DATE_FORMAT(o.tgl_operasi,'%Y-%m-%dT%H:%i') tgl_input FROM operasi o JOIN paket_operasi p ON p.kode_paket=o.kode_paket WHERE o.no_rawat=? ORDER BY o.tgl_operasi`, no)
	names := map[string]string{"-": ""}
	for _, x := range d["Dokter"].([]Row) {
		names[x["id"]] = x["nama"]
	}
	for _, x := range d["Petugas"].([]Row) {
		names[x["id"]] = x["nama"]
	}
	for _, o := range ops {
		var tim []string
		for _, pr := range core.PeranOperasi {
			if v := o[pr.Field]; v != "" && v != "-" {
				tim = append(tim, pr.Label+": "+names[v])
			}
		}
		o["tim"] = strings.Join(tim, " · ")
		if l := s.row(ctx, "SELECT *, DATE_FORMAT(selesaioperasi,'%Y-%m-%dT%H:%i') selesai_input FROM laporan_operasi WHERE no_rawat=? AND tanggal=?", no, o["tgl_operasi"]); l != nil {
			for k, v := range l {
				o["lap_"+k] = v
			}
		}
	}
	d["Operasi"] = ops
	d["BHP"] = s.rows(ctx, "SELECT b.*, k.nm_obat, k.kode_sat, b.hargasatuan*b.jumlah total FROM beri_obat_operasi b JOIN obatbhp_ok k ON k.kd_obat=b.kd_obat WHERE b.no_rawat=? ORDER BY b.tanggal", no)
	d["MasterBHP"] = s.rows(ctx, "SELECT * FROM obatbhp_ok ORDER BY nm_obat")
	d["Ruang"] = s.rows(ctx, "SELECT * FROM ruang_ok ORDER BY kd_ruang_ok")
	d["Now"] = core.Now().Format("2006-01-02T15:04")
	d["Dx"] = s.val(ctx, "SELECT CONCAT(d.kd_penyakit,' ',p.nm_penyakit) FROM diagnosa_pasien d JOIN penyakit p ON p.kd_penyakit=d.kd_penyakit WHERE d.no_rawat=? ORDER BY d.prioritas LIMIT 1", no)
	s.render(w, r, "operasi_input", d, 200)
}

func (s *Server) okAksi(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.ParseForm()
	f := r.FormValue
	no := f("no_rawat")
	to := s.Prefix + "/operasi/input?no=" + no
	u := userOf(r)
	var err error
	var msg string
	switch r.PathValue("aksi") {
	case "booking":
		err = s.tx(r, func(tx *sql.Tx) error {
			return core.BookingOperasi(ctx, tx, no, f("kode_paket"), f("tanggal"), f("jam_mulai"), f("jam_selesai"), f("kd_dokter"), f("kd_ruang_ok"))
		})
		msg = "Jadwal operasi tersimpan"
		if f("from") == "rawat" {
			to = s.Prefix + "/rawat?no=" + no + "&tab=operasi"
		}
	case "mulai", "batal":
		if !u.Has("ok") {
			err = errors.New("hanya petugas kamar operasi yang dapat mengubah status jadwal")
			break
		}
		err = s.tx(r, func(tx *sql.Tx) error {
			return core.StatusBooking(ctx, tx, no, f("kode_paket"), f("tanggal"), f("jam_mulai"), r.PathValue("aksi"))
		})
		msg = map[string]string{"mulai": "Operasi dimulai", "batal": "Jadwal dibatalkan"}[r.PathValue("aksi")]
		if f("from") == "jadwal" {
			to = s.Prefix + "/operasi?tgl=" + f("tanggal")
		}
	case "simpan":
		tim := map[string]string{}
		for _, p := range core.PeranOperasi {
			tim[p.Field] = f(p.Field)
		}
		var tot float64
		err = s.tx(r, func(tx *sql.Tx) (e error) {
			tot, e = core.SimpanOperasi(ctx, tx, core.OperasiInput{NoRawat: no, KodePaket: f("kode_paket"), TglOperasi: f("tgl_operasi"), JenisAnestesi: f("jenis_anasthesi"), Kategori: f("kategori"), Tim: tim})
			return
		})
		msg = "Tindakan operasi tersimpan · " + rupiah(tot)
	case "hapus":
		err = s.tx(r, func(tx *sql.Tx) error { return core.HapusOperasi(ctx, tx, no, f("tgl_operasi"), f("kode_paket")) })
		msg = "Tindakan operasi dihapus"
	case "laporan":
		err = s.tx(r, func(tx *sql.Tx) error {
			return core.SimpanLaporanOperasi(ctx, tx, core.LaporanOperasi{NoRawat: no, Tanggal: f("tanggal"), DxPre: f("diagnosa_preop"), DxPost: f("diagnosa_postop"), Jaringan: f("jaringan_dieksekusi"),
				Selesai: f("selesaioperasi"), PermintaanPA: f("permintaan_pa"), NomorImplan: f("nomor_implan"), Laporan: f("laporan_operasi")})
		})
		msg = "Laporan operasi tersimpan"
	case "bhp":
		j, _ := strconv.ParseFloat(strings.ReplaceAll(f("jumlah"), ",", "."), 64)
		err = s.tx(r, func(tx *sql.Tx) error { return core.BeriObatOperasi(ctx, tx, no, f("kd_obat"), j) })
		msg = "BHP operasi tersimpan"
	case "bhp-hapus":
		err = s.tx(r, func(tx *sql.Tx) error { return core.HapusObatOperasi(ctx, tx, no, f("tanggal"), f("kd_obat")) })
		msg = "BHP dihapus"
	default:
		http.NotFound(w, r)
		return
	}
	if err == nil {
		s.audit(r, "", "operasi_"+r.PathValue("aksi"), no, f("kode_paket"))
	}
	s.back(w, r, to, err, msg)
}

func (s *Server) okLaporanCetak(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	no, tgl := r.URL.Query().Get("no"), r.URL.Query().Get("tgl")
	reg, err := core.GetReg(ctx, s.DB, no)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	l := s.row(ctx, "SELECT * FROM laporan_operasi WHERE no_rawat=? AND tanggal=?", no, tgl)
	o := s.row(ctx, `SELECT o.*, p.nm_perawatan, d1.nm_dokter op1, d2.nm_dokter anes, pt.nama ast FROM operasi o JOIN paket_operasi p ON p.kode_paket=o.kode_paket
		LEFT JOIN dokter d1 ON d1.kd_dokter=o.operator1 LEFT JOIN dokter d2 ON d2.kd_dokter=o.dokter_anestesi LEFT JOIN petugas pt ON pt.nip=o.asisten_operator1 WHERE o.no_rawat=? AND o.tgl_operasi=? LIMIT 1`, no, tgl)
	if l == nil || o == nil {
		s.fail(w, r, errors.New("laporan operasi belum diisi"))
		return
	}
	s.render(w, r, "print_operasi", map[string]any{"Title": "Laporan Operasi", "R": reg, "L": l, "O": o, "Print": true}, 200)
}
