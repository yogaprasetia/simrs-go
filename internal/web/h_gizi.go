package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/yogaprasetia/simrs-go/internal/core"
)

var rGizi = []string{"gizi", "perawat"}

func (s *Server) routesGizi() {
	m, p := s.mux, s.Prefix
	m.HandleFunc("GET "+p+"/gizi", s.guard(rGizi, s.giziPage))
	m.HandleFunc("GET "+p+"/gizi/etiket", s.guard(rGizi, s.giziEtiket))
	m.HandleFunc("POST "+p+"/gizi/{aksi}", s.guard(rGizi, s.giziAksi))
}

func (s *Server) giziPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	tgl := q.Get("tgl")
	if _, err := time.Parse("2006-01-02", tgl); err != nil {
		tgl = core.Today()
	}
	tab := q.Get("tab")
	if tab == "" {
		tab = "pasien"
	}
	waktu := s.rows(ctx, "SELECT * FROM jam_diet_pasien ORDER BY jam")
	d := map[string]any{"Title": "Gizi", "Tgl": tgl, "Tab": tab, "Waktu": waktu, "Diet": s.rows(ctx, "SELECT * FROM diet ORDER BY kd_diet")}
	// pasien yang dirawat pada tanggal itu (baris kamar terbaru per no_rawat)
	pas := s.rows(ctx, `SELECT k.no_rawat, k.kd_kamar, b.nm_bangsal, m.kelas, p.nm_pasien, p.no_rkm_medis, p.jk, CONCAT(r.umurdaftar,' ',r.sttsumur) umur, k.diagnosa_awal,
		(SELECT GROUP_CONCAT(DISTINCT e.alergi SEPARATOR ', ') FROM pemeriksaan_ranap e JOIN reg_periksa rr ON rr.no_rawat=e.no_rawat WHERE rr.no_rkm_medis=p.no_rkm_medis AND e.alergi<>'') alergi
		FROM kamar_inap k JOIN kamar m ON m.kd_kamar=k.kd_kamar JOIN bangsal b ON b.kd_bangsal=m.kd_bangsal JOIN reg_periksa r ON r.no_rawat=k.no_rawat JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis
		WHERE k.tgl_masuk<=? AND (k.stts_pulang='-' OR k.tgl_keluar>=?)
		AND NOT EXISTS (SELECT 1 FROM kamar_inap k2 WHERE k2.no_rawat=k.no_rawat AND k2.tgl_masuk<=? AND (k2.stts_pulang='-' OR k2.tgl_keluar>=?) AND (k2.tgl_masuk>k.tgl_masuk OR (k2.tgl_masuk=k.tgl_masuk AND k2.jam_masuk>k.jam_masuk)))
		ORDER BY b.nm_bangsal, k.kd_kamar`, tgl, tgl, tgl, tgl)
	diet := s.rows(ctx, "SELECT d.no_rawat, d.waktu, d.kd_diet, t.nama_diet FROM detail_beri_diet d JOIN diet t ON t.kd_diet=d.kd_diet WHERE d.tanggal=?", tgl)
	set := map[string]map[string]string{}
	for _, x := range diet {
		if set[x["no_rawat"]] == nil {
			set[x["no_rawat"]] = map[string]string{}
		}
		set[x["no_rawat"]][x["waktu"]] = x["kd_diet"]
	}
	belum := 0
	for _, p := range pas {
		if len(set[p["no_rawat"]]) == 0 {
			belum++
		}
	}
	d["Pasien"], d["Set"], d["Belum"] = pas, set, belum
	d["Rekap"] = s.rows(ctx, `SELECT d.waktu, j.jam, t.nama_diet, b.nm_bangsal, COUNT(*) jml FROM detail_beri_diet d JOIN diet t ON t.kd_diet=d.kd_diet JOIN jam_diet_pasien j ON j.waktu=d.waktu
		JOIN kamar m ON m.kd_kamar=d.kd_kamar JOIN bangsal b ON b.kd_bangsal=m.kd_bangsal WHERE d.tanggal=? GROUP BY d.waktu, j.jam, t.nama_diet, b.nm_bangsal ORDER BY j.jam, t.nama_diet, b.nm_bangsal`, tgl)
	if t, err := time.Parse("2006-01-02", tgl); err == nil {
		d["Kemarin"] = t.AddDate(0, 0, -1).Format("2006-01-02")
		d["Besok"] = t.AddDate(0, 0, 1).Format("2006-01-02")
	}
	s.render(w, r, "gizi", d, 200)
}

func (s *Server) giziEtiket(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tgl, waktu := q.Get("tgl"), q.Get("waktu")
	list := s.rows(r.Context(), `SELECT d.*, t.nama_diet, p.nm_pasien, p.no_rkm_medis, p.tgl_lahir, b.nm_bangsal, j.jam FROM detail_beri_diet d JOIN diet t ON t.kd_diet=d.kd_diet
		JOIN reg_periksa r ON r.no_rawat=d.no_rawat JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis JOIN kamar m ON m.kd_kamar=d.kd_kamar JOIN bangsal b ON b.kd_bangsal=m.kd_bangsal
		JOIN jam_diet_pasien j ON j.waktu=d.waktu WHERE d.tanggal=? AND d.waktu=? ORDER BY b.nm_bangsal, d.kd_kamar`, tgl, waktu)
	s.render(w, r, "print_gizi", map[string]any{"Title": "Etiket diet", "List": list, "Tgl": tgl, "WaktuSel": waktu, "Print": true}, 200)
}

func (s *Server) giziAksi(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.ParseForm()
	f := r.FormValue
	tgl := f("tgl")
	to := s.Prefix + "/gizi?tgl=" + tgl + "&tab=" + f("tab")
	var err error
	var msg string
	switch r.PathValue("aksi") {
	case "simpan":
		no := f("no_rawat")
		n := 0
		err = s.tx(r, func(tx *sql.Tx) error {
			for _, wk := range s.rows(ctx, "SELECT waktu FROM jam_diet_pasien") {
				v := f("w_" + wk["waktu"])
				if v == "" {
					if e := core.HapusDiet(ctx, tx, no, tgl, wk["waktu"]); e != nil {
						return e
					}
					continue
				}
				if e := core.SetDiet(ctx, tx, no, tgl, wk["waktu"], v); e != nil {
					return e
				}
				n++
			}
			return nil
		})
		msg = "Diet " + no + " tersimpan (" + strconv.Itoa(n) + " waktu makan)"
	case "salin":
		var n int
		err = s.tx(r, func(tx *sql.Tx) (e error) { n, e = core.SalinDiet(ctx, tx, f("dari"), tgl); return })
		msg = strconv.Itoa(n) + " diet disalin dari " + f("dari")
	case "diet":
		if !userOf(r).Has("gizi") {
			err = errors.New("hanya ahli gizi yang dapat mengubah master diet")
			break
		}
		kd, nm := f("kd_diet"), f("nama_diet")
		if len(kd) == 0 || len(kd) > 3 || nm == "" {
			err = errors.New("kode (maks. 3) dan nama diet wajib diisi")
			break
		}
		_, err = s.DB.ExecContext(ctx, "INSERT INTO diet VALUES (?,?) ON DUPLICATE KEY UPDATE nama_diet=VALUES(nama_diet)", kd, nm)
		msg = "Diet " + kd + " tersimpan"
	default:
		http.NotFound(w, r)
		return
	}
	if err == nil {
		s.audit(r, "", "gizi_"+r.PathValue("aksi"), f("no_rawat"), msg)
	}
	s.back(w, r, to, err, msg)
}
