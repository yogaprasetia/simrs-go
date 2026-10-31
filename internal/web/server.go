package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/yogaprasetia/simrs-go/internal/bpjs"
	"github.com/yogaprasetia/simrs-go/internal/core"
	"github.com/yogaprasetia/simrs-go/internal/satusehat"
)

//go:embed templates/*.html
var tplFS embed.FS

//go:embed static
var staticFS embed.FS

type Server struct {
	DB        *sql.DB
	BPJS      *bpjs.Client
	Secret    []byte
	Prefix    string // mis. "" atau "/simrs"
	pages     map[string]*template.Template
	mux       *http.ServeMux
	Instansi  string
	UploadDir string
	SS        *satusehat.Client
}

type User struct {
	ID, Nama, KdDokter string
	Roles              map[string]bool
}

func (u *User) Has(roles ...string) bool {
	if u == nil {
		return false
	}
	if u.Roles["admin"] {
		return true
	}
	for _, r := range roles {
		if u.Roles[r] {
			return true
		}
	}
	return false
}

type ctxKey int

const userKey ctxKey = 1

func userOf(r *http.Request) *User { u, _ := r.Context().Value(userKey).(*User); return u }

// ------------------------------------------------------------------ setup

func New(db *sql.DB, b *bpjs.Client, secret []byte) (*Server, error) {
	s := &Server{DB: db, BPJS: b, Secret: secret, UploadDir: "uploads", SS: satusehat.New(satusehat.Config{Mode: "off"}, db), pages: map[string]*template.Template{}, mux: http.NewServeMux()}
	_ = db.QueryRow("SELECT nama_instansi FROM setting LIMIT 1").Scan(&s.Instansi)
	if s.Instansi == "" {
		s.Instansi = "Rumah Sakit"
	}
	funcs := template.FuncMap{
		"rp":    rupiah,
		"num":   func(v any) string { return fmtNum(toF(v)) },
		"tgl":   tglIndo,
		"eq2":   func(a, b any) bool { return fmt.Sprint(a) == fmt.Sprint(b) },
		"has":   func(u *User, roles ...string) bool { return u.Has(roles...) },
		"q":     url.QueryEscape,
		"json":  func(v any) template.JS { b, _ := json.Marshal(v); return template.JS(b) },
		"split": strings.Split,
		"add":   func(a, b int) int { return a + b },
		"pct": func(a, b any) string {
			x, y := toF(a), toF(b)
			if y == 0 {
				return "0"
			}
			return strconv.FormatFloat(x*100/y, 'f', 1, 64)
		},
		"nl": func(s string) template.HTML {
			return template.HTML(strings.ReplaceAll(template.HTMLEscapeString(s), "\n", "<br>"))
		},
		"upper": strings.ToUpper,
		"mul":   func(a, b any) float64 { return toF(a) * toF(b) },
		"toF":   toF,
		"sub":   func(a, b any) float64 { return toF(a) - toF(b) },
		"dict": func(kv ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i+1 < len(kv); i += 2 {
				m[fmt.Sprint(kv[i])] = kv[i+1]
			}
			return m
		},
		"blank": func(n int) []Row { return make([]Row, n) },
		"path":  func(p string) string { return s.Prefix + p },
	}
	layout, err := fs.ReadFile(tplFS, "templates/layout.html")
	if err != nil {
		return nil, err
	}
	entries, _ := fs.ReadDir(tplFS, "templates")
	for _, e := range entries {
		n := e.Name()
		if n == "layout.html" {
			continue
		}
		b, _ := fs.ReadFile(tplFS, "templates/"+n)
		t := template.New(n).Funcs(funcs)
		if _, err := t.Parse(string(layout)); err != nil {
			return nil, fmt.Errorf("layout: %w", err)
		}
		if _, err := t.Parse(string(b)); err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		s.pages[strings.TrimSuffix(n, ".html")] = t
	}
	s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	s.mux.ServeHTTP(w, r)
}

// ------------------------------------------------------------------ auth

func (s *Server) csrf(u *User, tok string) string {
	m := hmac.New(sha256.New, s.Secret)
	m.Write([]byte("csrf:" + tok))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

func (s *Server) loadUser(r *http.Request) (*User, string) {
	c, err := r.Cookie("simrs_sess")
	if err != nil || len(c.Value) != 64 {
		return nil, ""
	}
	u := &User{Roles: map[string]bool{}}
	var roles string
	err = s.DB.QueryRowContext(r.Context(), `SELECT u.id_user,u.nama,u.roles,u.kd_dokter FROM app_session s JOIN app_user u ON u.id_user=s.id_user
		WHERE s.token=? AND s.expires>NOW() AND u.aktif='1'`, c.Value).Scan(&u.ID, &u.Nama, &roles, &u.KdDokter)
	if err != nil {
		return nil, ""
	}
	for _, x := range strings.Split(roles, ",") {
		if x = strings.TrimSpace(x); x != "" {
			u.Roles[x] = true
		}
	}
	return u, c.Value
}

// guard membungkus handler: wajib login, cek peran, cek CSRF untuk POST.
func (s *Server) guard(roles []string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, tok := s.loadUser(r)
		if u == nil {
			if strings.HasPrefix(r.URL.Path, s.Prefix+"/api/") {
				http.Error(w, "unauthorized", 401)
				return
			}
			http.Redirect(w, r, s.Prefix+"/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		if len(roles) > 0 && !u.Has(roles...) {
			s.render(w, r.WithContext(context.WithValue(r.Context(), userKey, u)), "error", map[string]any{"Title": "Akses ditolak", "Msg": "Peran Anda tidak memiliki akses ke halaman ini."}, 403)
			return
		}
		if r.Method == http.MethodPost {
			if !hmac.Equal([]byte(r.FormValue("_csrf")), []byte(s.csrf(u, tok))) {
				http.Error(w, "Sesi formulir kedaluwarsa, muat ulang halaman.", http.StatusForbidden)
				return
			}
		}
		ctx := context.WithValue(r.Context(), userKey, u)
		ctx = context.WithValue(ctx, ctxKey(2), tok)
		h(w, r.WithContext(ctx))
	}
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.render(w, r, "login", map[string]any{"Next": r.URL.Query().Get("next")}, 200)
		return
	}
	id := strings.TrimSpace(r.FormValue("user"))
	pw := r.FormValue("password")
	var hash string
	err := s.DB.QueryRowContext(r.Context(), "SELECT password_hash FROM app_user WHERE id_user=? AND aktif='1'", id).Scan(&hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) != nil {
		time.Sleep(400 * time.Millisecond)
		s.render(w, r, "login", map[string]any{"Err": "User atau password salah", "Next": r.FormValue("next"), "LoginUser": id}, 401)
		return
	}
	b := make([]byte, 32)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	_, _ = s.DB.ExecContext(r.Context(), "DELETE FROM app_session WHERE expires<NOW()")
	if _, err := s.DB.ExecContext(r.Context(), "INSERT INTO app_session VALUES (?,?,DATE_ADD(NOW(), INTERVAL 12 HOUR))", tok, id); err != nil {
		s.fail(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "simrs_sess", Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", MaxAge: 12 * 3600})
	s.audit(r, id, "login", "", "")
	next := r.FormValue("next")
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = s.Prefix + "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("simrs_sess"); err == nil {
		s.DB.ExecContext(r.Context(), "DELETE FROM app_session WHERE token=?", c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "simrs_sess", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, s.Prefix+"/login", http.StatusSeeOther)
}

func (s *Server) audit(r *http.Request, user, aksi, ref, detail string) {
	if user == "" {
		if u := userOf(r); u != nil {
			user = u.ID
		}
	}
	if len(detail) > 490 {
		detail = detail[:490]
	}
	_, _ = s.DB.Exec("INSERT INTO app_audit (waktu,id_user,aksi,ref,detail) VALUES (NOW(),?,?,?,?)", user, aksi, ref, detail)
}

// ------------------------------------------------------------------ render

type MenuItem struct {
	Href, Label, Icon string
	Roles             []string
}

var menu = []MenuItem{
	{"/", "Dashboard", "🏠", nil},
	{"/registrasi", "Registrasi", "📝", []string{"pendaftaran"}},
	{"/pasien", "Data Pasien", "👤", []string{"pendaftaran", "dokter", "perawat"}},
	{"/ralan", "Rawat Jalan / IGD", "🩺", []string{"dokter", "perawat"}},
	{"/ranap", "Rawat Inap", "🛏", []string{"dokter", "perawat", "pendaftaran"}},
	{"/farmasi", "Farmasi", "💊", []string{"farmasi"}},
	{"/lab", "Laboratorium", "🧪", []string{"lab"}},
	{"/radiologi", "Radiologi", "🩻", []string{"radiologi"}},
	{"/operasi", "Kamar Operasi / VK", "⚕", []string{"ok", "dokter", "perawat", "kasir"}},
	{"/kasir", "Kasir", "💰", []string{"kasir"}},
	{"/logistik", "Logistik Farmasi", "📦", []string{"farmasi"}},
	{"/keuangan", "Keuangan & Akuntansi", "📒", []string{"keuangan", "manajemen"}},
	{"/bpjs", "Bridging BPJS", "🏥", []string{"bpjs", "pendaftaran"}},
	{"/satusehat", "SATUSEHAT", "🇮🇩", []string{"bpjs", "manajemen"}},
	{"/laporan", "Laporan", "📊", []string{"manajemen"}},
	{"/admin/users", "Pengguna", "⚙", nil},
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, page string, data map[string]any, code int) {
	t, ok := s.pages[page]
	if !ok {
		http.Error(w, "template "+page+" tidak ada", 500)
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	u := userOf(r)
	data["User"] = u
	data["Instansi"] = s.Instansi
	data["P"] = s.Prefix
	data["Path"] = strings.TrimPrefix(r.URL.Path, s.Prefix)
	data["Today"] = core.Today()
	data["Msg"] = r.URL.Query().Get("msg")
	if _, ok := data["Err"]; !ok {
		data["Err"] = r.URL.Query().Get("err")
	}
	data["BPJSMode"] = s.BPJS.Cfg.Mode
	data["SSMode"] = s.SS.Cfg.Mode
	if u != nil {
		tok, _ := r.Context().Value(ctxKey(2)).(string)
		data["CSRF"] = s.csrf(u, tok)
		var m []MenuItem
		for _, it := range menu {
			if it.Href == "/admin/users" && !u.Roles["admin"] {
				continue
			}
			if it.Roles == nil || u.Has(it.Roles...) {
				m = append(m, it)
			}
		}
		data["Menu"] = m
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		log.Printf("render %s: %v", page, err)
	}
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("error %s %s: %v", r.Method, r.URL.Path, err)
	s.render(w, r, "error", map[string]any{"Title": "Terjadi kesalahan", "Msg": err.Error()}, 500)
}

// back mengarahkan kembali dengan pesan sukses/galat.
func (s *Server) back(w http.ResponseWriter, r *http.Request, to string, err error, okMsg string) {
	if to == "" {
		to = r.Header.Get("Referer")
		if to == "" {
			to = s.Prefix + "/"
		}
	}
	u, _ := url.Parse(to)
	q := u.Query()
	q.Del("msg")
	q.Del("err")
	if err != nil {
		q.Set("err", err.Error())
	} else if okMsg != "" {
		q.Set("msg", okMsg)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func (s *Server) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// ------------------------------------------------------------------ query helpers

type Row map[string]string

func (s *Server) rows(ctx context.Context, q string, args ...any) []Row {
	rs, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("query: %v\n%s", err, q)
		return nil
	}
	defer rs.Close()
	cols, _ := rs.Columns()
	var out []Row
	for rs.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			log.Printf("scan: %v", err)
			return out
		}
		m := Row{}
		for i, c := range cols {
			m[c] = vals[i].String
		}
		out = append(out, m)
	}
	return out
}

func (s *Server) row(ctx context.Context, q string, args ...any) Row {
	r := s.rows(ctx, q, args...)
	if len(r) == 0 {
		return nil
	}
	return r[0]
}

func (s *Server) val(ctx context.Context, q string, args ...any) string {
	var v sql.NullString
	_ = s.DB.QueryRowContext(ctx, q, args...).Scan(&v)
	return v.String
}

// ------------------------------------------------------------------ format

func toF(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	f, _ := strconv.ParseFloat(fmt.Sprint(v), 64)
	return f
}

func fmtNum(f float64) string {
	neg := f < 0
	f = math.Abs(math.Round(f))
	s := strconv.FormatFloat(f, 'f', 0, 64)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func rupiah(v any) string { return "Rp " + fmtNum(toF(v)) }

var bulan = []string{"", "Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

func tglIndo(s string) string {
	if len(s) < 10 {
		return s
	}
	t, err := time.Parse("2006-01-02", s[:10])
	if err != nil {
		return s
	}
	out := fmt.Sprintf("%02d %s %d", t.Day(), bulan[t.Month()], t.Year())
	if len(s) > 10 {
		out += s[10:]
	}
	return out
}

func sortedKeys(m map[string]float64) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}
