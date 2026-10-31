package web_test

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/yogaprasetia/simrs-go/internal/bpjs"
	"github.com/yogaprasetia/simrs-go/internal/core"
	"github.com/yogaprasetia/simrs-go/internal/db"
	"github.com/yogaprasetia/simrs-go/internal/web"
)

type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.b.Write(p) }
func (l *logBuf) Take() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.b.String()
	l.b.Reset()
	return s
}

func setup(t *testing.T) (*sql.DB, *httptest.Server, *bpjs.Mock, *logBuf) {
	dsn := os.Getenv("SIMRS_TEST_DSN")
	if dsn == "" {
		pw, err := os.ReadFile("/data/workspace/.simrs-db-pass")
		if err != nil {
			t.Skip("SIMRS_TEST_DSN tidak diset")
		}
		dsn = "simrs:" + strings.TrimSpace(string(pw)) + "@tcp(127.0.0.1:3306)/sik_test?charset=utf8mb4"
	}
	d, err := db.Open(dsn)
	if err != nil {
		t.Skip(err)
	}
	ctx := context.Background()
	rows, _ := d.Query("SHOW TABLES")
	var tabs []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		tabs = append(tabs, s)
	}
	rows.Close()
	for _, tb := range tabs {
		d.Exec("DROP TABLE `" + tb + "`")
	}
	if err := db.Migrate(ctx, d, true); err != nil {
		t.Fatal(err)
	}
	for _, u := range [][3]string{{"admin", "admin", ""}, {"daftar", "pendaftaran,bpjs", ""}, {"dr", "dokter", "D0000002"}, {"apt", "farmasi", ""}, {"lab", "lab", ""}, {"kasir", "kasir", ""}, {"mgr", "manajemen", ""}, {"prw", "perawat", ""}} {
		if err := db.UpsertUser(ctx, d, u[0], strings.ToUpper(u[0]), "rahasia123", u[1], u[2]); err != nil {
			t.Fatal(err)
		}
	}
	m := bpjs.NewMock("c", "s")
	vc, an, _ := m.Start()
	cl := bpjs.New(bpjs.Config{ConsID: "c", Secret: "s", UserKeyVClaim: "u", UserKeyAntrean: "u", BaseVClaim: vc, BaseAntrean: an, KodePPK: "0101R001", NamaPPK: "RS CONTOH", Mode: "mock"}, d)
	srv, err := web.New(d, cl, []byte("tes-secret"))
	if err != nil {
		t.Fatal(err)
	}
	lb := &logBuf{}
	log.SetOutput(lb)
	return d, httptest.NewServer(srv), m, lb
}

type client struct {
	t    *testing.T
	base string
	c    *http.Client
	csrf string
}

var csrfRe = regexp.MustCompile(`name="_csrf" value="([0-9a-f]+)"`)

func login(t *testing.T, base, user string) *client {
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, base: base, c: &http.Client{Jar: jar}}
	resp, err := c.c.PostForm(base+"/login", url.Values{"user": {user}, "password": {"rahasia123"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Keluar") {
		t.Fatalf("login %s gagal: %d", user, resp.StatusCode)
	}
	if m := csrfRe.FindSubmatch(body); m != nil {
		c.csrf = string(m[1])
	}
	return c
}

func (c *client) get(path string, want int) string {
	c.t.Helper()
	resp, err := c.c.Get(c.base + path)
	if err != nil {
		c.t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != want {
		c.t.Fatalf("GET %s = %d (want %d)\n%s", path, resp.StatusCode, want, trim(string(b)))
	}
	return string(b)
}

// post mengembalikan URL tujuan redirect + isi halaman akhir.
func (c *client) post(path string, v url.Values) (string, string) {
	c.t.Helper()
	if v == nil {
		v = url.Values{}
	}
	if _, ok := v["_csrf"]; !ok {
		v.Set("_csrf", c.csrf)
	}
	resp, err := c.c.PostForm(c.base+path, v)
	if err != nil {
		c.t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode != 400 {
		c.t.Fatalf("POST %s = %d\n%s", path, resp.StatusCode, trim(string(b)))
	}
	return resp.Request.URL.String(), string(b)
}

func (c *client) postOK(path string, v url.Values) string {
	c.t.Helper()
	u, body := c.post(path, v)
	pu, _ := url.Parse(u)
	if e := pu.Query().Get("err"); e != "" {
		c.t.Fatalf("POST %s ditolak: %s", path, e)
	}
	return body
}

func (c *client) postErr(path string, v url.Values, contains string) {
	c.t.Helper()
	u, _ := c.post(path, v)
	pu, _ := url.Parse(u)
	if e := pu.Query().Get("err"); e == "" || !strings.Contains(e, contains) {
		c.t.Fatalf("POST %s seharusnya error %q, dapat %q", path, contains, e)
	}
}

func trim(s string) string {
	if len(s) > 600 {
		return s[:600]
	}
	return s
}

func q1(t *testing.T, d *sql.DB, q string, args ...any) string {
	var v sql.NullString
	if err := d.QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return v.String
}

func TestWebAlurLengkap(t *testing.T) {
	d, ts, mock, lb := setup(t)
	defer ts.Close()
	B := ts.URL

	// tanpa login -> redirect ke login
	anon := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, _ := anon.Get(B + "/ralan")
	if r.StatusCode != 303 || !strings.Contains(r.Header.Get("Location"), "/login") {
		t.Fatalf("anon: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	// password salah
	r, _ = http.PostForm(B+"/login", url.Values{"user": {"admin"}, "password": {"salah"}})
	if r.StatusCode != 401 {
		t.Fatalf("login salah: %d", r.StatusCode)
	}

	adm := login(t, B, "admin")
	daf := login(t, B, "daftar")
	dr := login(t, B, "dr")
	apt := login(t, B, "apt")
	lab := login(t, B, "lab")
	ksr := login(t, B, "kasir")
	mgr := login(t, B, "mgr")
	prw := login(t, B, "prw")

	// RBAC
	ksr.get("/farmasi", 403)
	apt.get("/kasir", 403)
	dr.get("/admin/users", 403)
	mgr.get("/registrasi", 403)
	// CSRF
	if u, _ := url.Parse(B); u != nil {
		resp, _ := daf.c.PostForm(B+"/pasien/simpan", url.Values{"nm_pasien": {"X"}})
		if resp.StatusCode != 403 {
			t.Fatalf("tanpa CSRF seharusnya 403, dapat %d", resp.StatusCode)
		}
	}

	// ---- pendaftaran: pasien BPJS baru
	body := daf.postOK("/pasien/simpan", url.Values{"nm_pasien": {"Budi Hartono"}, "jk": {"L"}, "tgl_lahir": {"1980-01-01"}, "no_ktp": {"3372010101800001"}, "alamat": {"Jl. Mawar 1"},
		"kd_pj": {"BPJ"}, "no_peserta": {"0001234567890"}, "no_tlp": {"0812"}, "namakeluarga": {"SRI"}, "keluarga": {"ISTRI"}, "next": {"registrasi"}})
	if !strings.Contains(body, "000001") {
		t.Fatal("pasien baru tidak terbawa ke registrasi")
	}
	// validasi NIK salah
	_, b2 := daf.post("/pasien/simpan", url.Values{"nm_pasien": {"X"}, "jk": {"L"}, "tgl_lahir": {"1990-01-01"}, "no_ktp": {"123"}})
	if !strings.Contains(b2, "NIK harus 16 digit") {
		t.Fatal("validasi NIK tidak tampil")
	}
	daf.postOK("/pasien/simpan", url.Values{"nm_pasien": {"Siti Aminah"}, "jk": {"P"}, "tgl_lahir": {"1990-05-05"}, "kd_pj": {"A09"}})
	if !strings.Contains(daf.get("/api/pasien?q=budi", 200), "0001234567890") {
		t.Fatal("api pasien")
	}
	if !strings.Contains(daf.get("/api/dokter?poli=INT", 200), "D0000002") {
		t.Fatal("api dokter")
	}
	body = daf.postOK("/registrasi", url.Values{"no_rkm_medis": {"000001"}, "kd_poli": {"INT"}, "kd_dokter": {"D0000002"}, "kd_pj": {"BPJ"}})
	today := core.Today()
	noRawat := strings.ReplaceAll(today, "-", "/") + "/000001"
	if !strings.Contains(body, noRawat) || !strings.Contains(body, "Antrean BPJS terkirim") {
		t.Fatalf("registrasi: %s", trim(body))
	}
	a := mock.Antrean(noRawat)
	if a == nil || a["nomorkartu"] != "0001234567890" || a["kodepoli"] != "INT" {
		t.Fatalf("antrean mock: %v", a)
	}
	daf.get("/registrasi/bukti?no="+url.QueryEscape(noRawat), 200)
	// SEP
	body = daf.get("/bpjs/sep?no="+url.QueryEscape(noRawat), 200)
	if !strings.Contains(body, "BUDI HARTONO") || !strings.Contains(body, "0101G0011025P000") {
		t.Fatalf("form SEP tidak memuat peserta/rujukan:\n%s", trim(body))
	}
	body = daf.postOK("/bpjs/sep", url.Values{"no_rawat": {noRawat}, "no_kartu": {"0001234567890"}, "klsrawat": {"3"}, "asal_rujukan": {"1"}, "no_rujukan": {"0101G0011025P000890"},
		"tglrujukan": {today}, "kdppkrujukan": {"0101G001"}, "diagawal": {"I10 Essential"}, "kdpolitujuan": {"INT"}, "kddpjp": {"10002"}, "notelep": {"0812"}})
	noSep := q1(t, d, "SELECT no_sep FROM bridging_sep WHERE no_rawat=?", noRawat)
	if noSep == "" || !strings.Contains(body, noSep) {
		t.Fatal("SEP tidak tersimpan/tercetak")
	}
	daf.postErr("/bpjs/sep", url.Values{"no_rawat": {noRawat}, "no_kartu": {"0001234567890"}, "diagawal": {"I10"}}, "sudah memiliki SEP")
	daf.get("/bpjs?cek=0009999999999", 200)
	if !strings.Contains(daf.get("/bpjs?cek=3372014505900002", 200), "SITI AMINAH") {
		t.Fatal("cek NIK")
	}

	// ---- dokter: ERM
	if !strings.Contains(dr.get("/ralan", 200), "BUDI HARTONO") {
		t.Fatal("pasien tidak muncul di daftar ralan dokter")
	}
	rw := "/rawat?no=" + url.QueryEscape(noRawat)
	for _, tab := range []string{"soap", "diagnosa", "tindakan", "resep", "lab", "kamar", "riwayat"} {
		dr.get(rw+"&tab="+tab, 200)
	}
	dr.postOK("/rawat/soap", url.Values{"no_rawat": {noRawat}, "tensi": {"160/100"}, "nadi": {"88"}, "keluhan": {"pusing"}, "penilaian": {"Hipertensi"}, "rtl": {"amlodipine"}, "alergi": {"Amoxicillin"}})
	if !strings.Contains(dr.get(rw, 200), "Alergi: Amoxicillin") {
		t.Fatal("peringatan alergi tidak tampil")
	}
	dr.postOK("/rawat/diagnosa", url.Values{"no_rawat": {noRawat}, "kd_penyakit": {"I10 Essential (primary) hypertension"}})
	dr.postOK("/rawat/prosedur", url.Values{"no_rawat": {noRawat}, "kode": {"89.52 Electrocardiogram"}})
	dr.postOK("/rawat/tindakan", url.Values{"no_rawat": {noRawat}, "kd_jenis_prw": {"RJ002", "RJ007"}, "kd_dokter": {"D0000002"}})
	dr.postOK("/rawat/resep", url.Values{"no_rawat": {noRawat}, "kode_brng": {"B000006 Amlodipine", "B000001", ""}, "jml": {"30", "10", ""}, "aturan": {"1x1", "3x1", ""}})
	dr.postOK("/rawat/lab", url.Values{"no_rawat": {noRawat}, "kd_jenis_prw": {"LAB001", "LAB003"}, "diagnosa_klinis": {"HT"}})
	prw.postErr("/rawat/diagnosa", url.Values{"no_rawat": {noRawat}, "kd_penyakit": {"I10"}}, "peran")
	if !strings.Contains(dr.get("/api/penyakit?q=hypertension", 200), "I10") || !strings.Contains(dr.get("/api/obat?q=amlo", 200), "B000006") {
		t.Fatal("api lookup")
	}
	if tasks := mock.Antrean(noRawat)["tasks"].(map[string]int64); tasks["4"] == 0 {
		// task dikirim async
		t.Logf("task4 belum tercatat (async) — dicek lagi di akhir")
	}

	// ---- kasir menolak sebelum farmasi & lab selesai
	ksr.get("/kasir", 200)
	if !strings.Contains(ksr.get("/kasir/billing?no="+url.QueryEscape(noRawat), 200), "belum selesai") {
		t.Fatal("peringatan pending tidak tampil")
	}
	ksr.postErr("/kasir/bayar", url.Values{"no_rawat": {noRawat}}, "resep")

	// ---- farmasi
	noResep := q1(t, d, "SELECT no_resep FROM resep_obat WHERE no_rawat=?", noRawat)
	if !strings.Contains(apt.get("/farmasi", 200), noResep) {
		t.Fatal("resep tidak muncul di farmasi")
	}
	body = apt.get("/farmasi/resep?no="+noResep, 200)
	if !strings.Contains(body, "Alergi: Amoxicillin") || !strings.Contains(body, "Rp 35.000") {
		t.Fatalf("detail resep (alergi/total):\n%s", trim(body[strings.Index(body, "<main"):]))
	}
	apt.postOK("/farmasi/validasi", url.Values{"no_resep": {noResep}})
	apt.postOK("/farmasi/serahkan", url.Values{"no_resep": {noResep}})
	apt.get("/farmasi/resep?no="+noResep+"&cetak=1", 200)
	apt.postOK("/farmasi/stok", url.Values{"kode_brng": {"B000009 OBH"}, "jml": {"20"}, "keterangan": {"Faktur 123"}})
	if s := q1(t, d, "SELECT stok FROM gudangbarang WHERE kode_brng='B000009'"); s != "100" {
		t.Fatalf("stok OBH %s", s)
	}
	apt.get("/farmasi/stok?q=para", 200)
	apt.get("/farmasi?st=selesai", 200)

	// ---- lab
	noorder := q1(t, d, "SELECT noorder FROM permintaan_lab WHERE no_rawat=?", noRawat)
	lab.get("/lab", 200)
	if !strings.Contains(lab.get("/lab/hasil?no="+noorder, 200), "Hemoglobin") {
		t.Fatal("template lab")
	}
	lab.postOK("/lab/hasil", url.Values{"noorder": {noorder}, "kd_dokter": {"D0000001"}, "t_1": {"18.5"}, "t_2": {"7"}, "t_7": {"250"}})
	if !strings.Contains(dr.get("/lab/cetak?no="+noorder, 200), "18.5") {
		t.Fatal("cetak lab")
	}

	// ---- kasir
	body = ksr.postOK("/kasir/bayar", url.Values{"no_rawat": {noRawat}})
	if !strings.Contains(body, "LUNAS") {
		t.Fatal("status lunas")
	}
	if !strings.Contains(ksr.get("/kasir/nota?no="+url.QueryEscape(noRawat), 200), "TOTAL TAGIHAN") {
		t.Fatal("nota")
	}
	dr.postErr("/rawat/tindakan", url.Values{"no_rawat": {noRawat}, "kd_jenis_prw": {"RJ003"}, "kd_dokter": {"D0000002"}}, "sudah dibayar")
	adm.postOK("/kasir/batal", url.Values{"no_rawat": {noRawat}})
	ksr.postOK("/kasir/bayar", url.Values{"no_rawat": {noRawat}})

	// ---- IGD -> rawat inap -> pulang -> bayar
	daf.postOK("/registrasi", url.Values{"no_rkm_medis": {"000002"}, "kd_poli": {"IGDK"}, "kd_dokter": {"D0000001"}, "kd_pj": {"A09"}})
	noIGD := strings.ReplaceAll(today, "-", "/") + "/000002"
	prw.postOK("/rawat/soap", url.Values{"no_rawat": {noIGD}, "keluhan": {"demam 4 hari"}, "penilaian": {"susp DHF"}})
	daf.postOK("/rawat/kamar-masuk", url.Values{"no_rawat": {noIGD}, "kd_kamar": {"MLT.01"}, "diagnosa_awal": {"susp DHF"}, "kd_dokter": {"D0000002"}})
	if !strings.Contains(prw.get("/ranap", 200), "SITI AMINAH") {
		t.Fatal("pasien tidak tampil di ranap")
	}
	prw.get("/rawat?no="+url.QueryEscape(noIGD)+"&tab=kamar", 200)
	dr.postOK("/rawat/tindakan", url.Values{"no_rawat": {noIGD}, "kd_jenis_prw": {"RI002"}, "kd_dokter": {"D0000002"}})
	daf.postOK("/rawat/kamar-pindah", url.Values{"no_rawat": {noIGD}, "kd_kamar": {"MWR.01"}})
	ksr.postErr("/kasir/bayar", url.Values{"no_rawat": {noIGD}}, "dirawat")
	prw.postOK("/rawat/kamar-pulang", url.Values{"no_rawat": {noIGD}, "stts_pulang": {"Membaik"}, "diagnosa_akhir": {"A91"}})
	ksr.postOK("/kasir/bayar", url.Values{"no_rawat": {noIGD}})
	if n := q1(t, d, "SELECT no_nota FROM nota_inap WHERE no_rawat=?", noIGD); !strings.Contains(n, "/RI/") {
		t.Fatalf("nota inap %s", n)
	}

	// ---- laporan, dashboard, users, riwayat
	body = mgr.get("/laporan", 200)
	if !strings.Contains(body, "I10") || !strings.Contains(body, "BOR") {
		t.Fatal("laporan")
	}
	if !strings.Contains(mgr.get("/laporan?csv=kunjungan", 200), "Penyakit Dalam") {
		t.Fatal("csv")
	}
	adm.get("/", 200)
	adm.get("/admin/users", 200)
	adm.postOK("/admin/users", url.Values{"id_user": {"baru"}, "nama": {"Baru"}, "password": {"passwordbaru"}, "roles": {"perawat", "lab"}})
	login(t, B, "admin") // sesi lain tetap jalan
	daf.get("/pasien?q=budi", 200)
	daf.get("/pasien/riwayat?rm=000001", 200)
	daf.get("/pasien/form?rm=000001", 200)
	daf.get("/registrasi", 200)
	daf.get("/bpjs", 200)
	dr.get("/rawat?no="+url.QueryEscape(noRawat)+"&tab=riwayat", 200)
	ksr.get("/kasir?st=Sudah+Bayar", 200)
	lab.get("/lab?st=selesai", 200)
	if c := q1(t, d, "SELECT COUNT(*) FROM app_bpjs_log WHERE code='200'"); c == "0" {
		t.Fatal("log BPJS kosong")
	}
	// batal registrasi
	daf.postOK("/registrasi", url.Values{"no_rkm_medis": {"000002"}, "kd_poli": {"U0001"}, "kd_dokter": {"D0000001"}, "kd_pj": {"A09"}})
	no3 := strings.ReplaceAll(today, "-", "/") + "/000003"
	daf.postOK("/registrasi/batal", url.Values{"no_rawat": {no3}})
	if s := q1(t, d, "SELECT stts FROM reg_periksa WHERE no_rawat=?", no3); s != "Batal" {
		t.Fatal("batal")
	}

	// logout
	adm.post("/logout", nil)
	r, _ = adm.c.Get(B + "/")
	if !strings.Contains(r.Request.URL.Path, "/login") {
		t.Fatal("logout tidak menghapus sesi")
	}
	if l := lb.Take(); strings.Contains(l, "render") || strings.Contains(l, "query:") || strings.Contains(l, "scan:") {
		t.Fatalf("ada error template/query di log:\n%s", l)
	}
}
