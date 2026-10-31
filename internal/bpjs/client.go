// Package bpjs: bridging BPJS Kesehatan (VClaim 2.0 & Antrean RS / Mobile JKN).
//
// Autentikasi: X-cons-id, X-timestamp (detik UTC), X-signature = base64(HMAC-SHA256(secret, consid&timestamp)),
// user_key. Respons terenkripsi AES-256-CBC (key = SHA256(consid+secret+timestamp), IV = 16 byte pertama key),
// lalu dikompresi LZString (EncodedURIComponent).
package bpjs

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ConsID, Secret, UserKeyVClaim, UserKeyAntrean string
	BaseVClaim, BaseAntrean                       string
	KodePPK, NamaPPK                              string
	Mode                                          string // "mock", "dev", "prod", "off"
}

func (c Config) Enabled() bool { return c.Mode != "" && c.Mode != "off" }

const (
	DevVClaim   = "https://apijkn-dev.bpjs-kesehatan.go.id/vclaim-rest-dev/"
	ProdVClaim  = "https://apijkn.bpjs-kesehatan.go.id/vclaim-rest/"
	DevAntrean  = "https://apijkn-dev.bpjs-kesehatan.go.id/antreanrs_dev/"
	ProdAntrean = "https://apijkn.bpjs-kesehatan.go.id/antreanrs/"
)

type Client struct {
	Cfg  Config
	HTTP *http.Client
	DB   *sql.DB // untuk log (opsional)
	now  func() time.Time
}

func New(cfg Config, db *sql.DB) *Client {
	return &Client{Cfg: cfg, HTTP: &http.Client{Timeout: 20 * time.Second}, DB: db, now: time.Now}
}

func Signature(consID, secret, ts string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(consID + "&" + ts))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

func deriveKey(consID, secret, ts string) []byte {
	k := sha256.Sum256([]byte(consID + secret + ts))
	return k[:]
}

// Decrypt membuka string respons terenkripsi BPJS menjadi JSON.
func Decrypt(consID, secret, ts, enc string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(enc))
	if err != nil {
		return "", fmt.Errorf("bpjs: base64: %w", err)
	}
	key := deriveKey(consID, secret, ts)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return "", errors.New("bpjs: panjang cipher tidak valid")
	}
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, key[:16]).CryptBlocks(out, raw)
	pad := int(out[len(out)-1])
	if pad < 1 || pad > aes.BlockSize || pad > len(out) {
		return "", errors.New("bpjs: padding tidak valid (cek consid/secret/timestamp)")
	}
	return DecompressFromEncodedURIComponent(string(out[:len(out)-pad]))
}

// Encrypt kebalikan Decrypt (dipakai server tiruan & pengujian).
func Encrypt(consID, secret, ts, plain string) string {
	comp := []byte(CompressToEncodedURIComponent(plain))
	pad := aes.BlockSize - len(comp)%aes.BlockSize
	comp = append(comp, bytes.Repeat([]byte{byte(pad)}, pad)...)
	key := deriveKey(consID, secret, ts)
	block, _ := aes.NewCipher(key)
	out := make([]byte, len(comp))
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(out, comp)
	return base64.StdEncoding.EncodeToString(out)
}

type Meta struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// UnmarshalJSON menerima code berupa angka (Antrean) maupun string (VClaim).
func (m *Meta) UnmarshalJSON(b []byte) error {
	var raw struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	m.Message = raw.Message
	switch v := raw.Code.(type) {
	case string:
		m.Code = v
	case float64:
		m.Code = strconv.Itoa(int(v))
	}
	return nil
}

type Error struct{ Meta }

func (e *Error) Error() string { return fmt.Sprintf("BPJS %s: %s", e.Code, e.Message) }

// Call melakukan permintaan ke layanan ("vclaim" / "antrean") dan mengembalikan JSON respons terdekripsi.
func (c *Client) Call(ctx context.Context, svc, method, path string, body any) (json.RawMessage, error) {
	if !c.Cfg.Enabled() {
		return nil, errors.New("bridging BPJS tidak aktif (BPJS_MODE=off)")
	}
	base, ukey := c.Cfg.BaseVClaim, c.Cfg.UserKeyVClaim
	if svc == "antrean" {
		base, ukey = c.Cfg.BaseAntrean, c.Cfg.UserKeyAntrean
	}
	var rd io.Reader
	var reqBody []byte
	if body != nil {
		reqBody, _ = json.Marshal(body)
		rd = bytes.NewReader(reqBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+"/"+strings.TrimLeft(path, "/"), rd)
	if err != nil {
		return nil, err
	}
	ts := strconv.FormatInt(c.now().UTC().Unix(), 10)
	req.Header.Set("X-cons-id", c.Cfg.ConsID)
	req.Header.Set("X-timestamp", ts)
	req.Header.Set("X-signature", Signature(c.Cfg.ConsID, c.Cfg.Secret, ts))
	req.Header.Set("user_key", ukey)
	req.Header.Set("Accept", "application/json")
	if svc == "vclaim" {
		req.Header.Set("Content-Type", "Application/x-www-form-urlencoded")
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	start := time.Now()
	resp, err := c.HTTP.Do(req)
	if err != nil {
		c.log(svc, method, req.URL.String(), reqBody, "ERR", err.Error(), start)
		return nil, fmt.Errorf("gagal menghubungi BPJS: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var env struct {
		MetaData *Meta           `json:"metaData"`
		Metadata *Meta           `json:"metadata"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		c.log(svc, method, req.URL.String(), reqBody, strconv.Itoa(resp.StatusCode), "respons bukan JSON", start)
		return nil, fmt.Errorf("respons BPJS tidak valid (HTTP %d)", resp.StatusCode)
	}
	meta := env.MetaData
	if meta == nil {
		meta = env.Metadata
	}
	if meta == nil {
		meta = &Meta{Code: strconv.Itoa(resp.StatusCode)}
	}
	c.log(svc, method, req.URL.String(), reqBody, meta.Code, meta.Message, start)
	if meta.Code != "200" && meta.Code != "1" {
		return nil, &Error{*meta}
	}
	if len(env.Response) == 0 || string(env.Response) == "null" {
		return json.RawMessage("null"), nil
	}
	var enc string
	if json.Unmarshal(env.Response, &enc) == nil {
		if enc == "" {
			return json.RawMessage("null"), nil
		}
		plain, err := Decrypt(c.Cfg.ConsID, c.Cfg.Secret, ts, enc)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(plain), nil
	}
	return env.Response, nil // tidak terenkripsi
}

func (c *Client) log(svc, method, url string, body []byte, code, msg string, start time.Time) {
	if c.DB == nil {
		return
	}
	b := string(body)
	if len(b) > 4000 {
		b = b[:4000]
	}
	if len(msg) > 290 {
		msg = msg[:290]
	}
	_, _ = c.DB.Exec("INSERT INTO app_bpjs_log (waktu,layanan,method,url,request,code,message,durasi_ms) VALUES (NOW(),?,?,?,?,?,?,?)",
		svc, method, url, b, code, msg, int(time.Since(start).Milliseconds()))
}

// ---------------------------------------------------------------- VClaim

type Peserta struct {
	NoKartu, NIK, Nama, Pisa, Sex, NoMR, TglLahir   string
	HakKelas, StatusPeserta, JenisPeserta, ProvUmum struct{ Kode, Keterangan string }
	Mr                                              struct{ NoMR, NoTelepon string }
	ProvUmumNama                                    string
	Raw                                             json.RawMessage
}

func (c *Client) peserta(ctx context.Context, path string) (map[string]any, error) {
	raw, err := c.Call(ctx, "vclaim", "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Peserta map[string]any `json:"peserta"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || r.Peserta == nil {
		return nil, errors.New("data peserta tidak ditemukan")
	}
	return r.Peserta, nil
}

func (c *Client) PesertaByKartu(ctx context.Context, noKartu, tgl string) (map[string]any, error) {
	return c.peserta(ctx, "Peserta/nokartu/"+noKartu+"/tglSEP/"+tgl)
}

func (c *Client) PesertaByNIK(ctx context.Context, nik, tgl string) (map[string]any, error) {
	return c.peserta(ctx, "Peserta/nik/"+nik+"/tglSEP/"+tgl)
}

// Rujukan by nomor (faskes: 1 = Faskes 1 / Puskesmas, 2 = RS)
func (c *Client) Rujukan(ctx context.Context, noRujukan string, faskes int) (map[string]any, error) {
	p := "Rujukan/" + noRujukan
	if faskes == 2 {
		p = "Rujukan/RS/" + noRujukan
	}
	raw, err := c.Call(ctx, "vclaim", "GET", p, nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Rujukan map[string]any `json:"rujukan"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || r.Rujukan == nil {
		return nil, errors.New("rujukan tidak ditemukan")
	}
	return r.Rujukan, nil
}

func (c *Client) RujukanByKartu(ctx context.Context, noKartu string) (map[string]any, error) {
	raw, err := c.Call(ctx, "vclaim", "GET", "Rujukan/Peserta/"+noKartu, nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Rujukan map[string]any `json:"rujukan"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || r.Rujukan == nil {
		return nil, errors.New("rujukan tidak ditemukan")
	}
	return r.Rujukan, nil
}

type SEPRequest struct {
	NoKartu, TglSep, JnsPelayanan, KlsRawatHak, NoMR, AsalRujukan, TglRujukan, NoRujukan, PPKRujukan string
	Catatan, DiagAwal, PoliTujuan, NoSurat, KodeDPJP, DPJPLayan, NoTelp, User, TujuanKunj            string
	LakaLantas                                                                                       string
}

// InsertSEP: SEP/2.0/insert. Mengembalikan objek "sep" dari respons.
func (c *Client) InsertSEP(ctx context.Context, s SEPRequest) (map[string]any, error) {
	if s.LakaLantas == "" {
		s.LakaLantas = "0"
	}
	if s.TujuanKunj == "" {
		s.TujuanKunj = "0"
	}
	poliEks := "0"
	body := map[string]any{"request": map[string]any{"t_sep": map[string]any{
		"noKartu": s.NoKartu, "tglSep": s.TglSep, "ppkPelayanan": c.Cfg.KodePPK, "jnsPelayanan": s.JnsPelayanan,
		"klsRawat": map[string]string{"klsRawatHak": s.KlsRawatHak, "klsRawatNaik": "", "pembiayaan": "", "penanggungJawab": ""},
		"noMR":     s.NoMR,
		"rujukan":  map[string]string{"asalRujukan": s.AsalRujukan, "tglRujukan": s.TglRujukan, "noRujukan": s.NoRujukan, "ppkRujukan": s.PPKRujukan},
		"catatan":  s.Catatan, "diagAwal": s.DiagAwal,
		"poli": map[string]string{"tujuan": s.PoliTujuan, "eksekutif": poliEks},
		"cob":  map[string]string{"cob": "0"}, "katarak": map[string]string{"katarak": "0"},
		"jaminan": map[string]any{"lakaLantas": s.LakaLantas, "noLP": "", "penjamin": map[string]any{"tglKejadian": "", "keterangan": "",
			"suplesi": map[string]any{"suplesi": "0", "noSepSuplesi": "", "lokasiLaka": map[string]string{"kdPropinsi": "", "kdKabupaten": "", "kdKecamatan": ""}}}},
		"tujuanKunj": s.TujuanKunj, "flagProcedure": "", "kdPenunjang": "", "assesmentPel": "",
		"skdp":      map[string]string{"noSurat": s.NoSurat, "kodeDPJP": s.KodeDPJP},
		"dpjpLayan": s.DPJPLayan, "noTelp": s.NoTelp, "user": s.User,
	}}}
	raw, err := c.Call(ctx, "vclaim", "POST", "SEP/2.0/insert", body)
	if err != nil {
		return nil, err
	}
	var r struct {
		Sep map[string]any `json:"sep"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || r.Sep == nil {
		return nil, errors.New("respons SEP tidak valid")
	}
	return r.Sep, nil
}

func (c *Client) HapusSEP(ctx context.Context, noSep, user string) error {
	_, err := c.Call(ctx, "vclaim", "DELETE", "SEP/2.0/delete", map[string]any{"request": map[string]any{"t_sep": map[string]string{"noSep": noSep, "user": user}}})
	return err
}

func (c *Client) RefDiagnosa(ctx context.Context, q string) ([]map[string]any, error) {
	raw, err := c.Call(ctx, "vclaim", "GET", "referensi/diagnosa/"+q, nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Diagnosa []map[string]any `json:"diagnosa"`
	}
	err = json.Unmarshal(raw, &r)
	return r.Diagnosa, err
}

func (c *Client) RefPoli(ctx context.Context, q string) ([]map[string]any, error) {
	raw, err := c.Call(ctx, "vclaim", "GET", "referensi/poli/"+q, nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Poli []map[string]any `json:"poli"`
	}
	err = json.Unmarshal(raw, &r)
	return r.Poli, err
}

// ---------------------------------------------------------------- Antrean RS (Mobile JKN)

type AntreanAdd struct {
	KodeBooking      string `json:"kodebooking"`
	JenisPasien      string `json:"jenispasien"` // JKN / NON JKN
	NomorKartu       string `json:"nomorkartu"`
	NIK              string `json:"nik"`
	NoHP             string `json:"nohp"`
	KodePoli         string `json:"kodepoli"`
	NamaPoli         string `json:"namapoli"`
	PasienBaru       int    `json:"pasienbaru"`
	NoRM             string `json:"norm"`
	TanggalPeriksa   string `json:"tanggalperiksa"`
	KodeDokter       int    `json:"kodedokter"`
	NamaDokter       string `json:"namadokter"`
	JamPraktek       string `json:"jampraktek"`
	JenisKunjungan   int    `json:"jeniskunjungan"` // 1 rujukan FKTP, 2 rujukan internal, 3 kontrol, 4 rujukan antar RS
	NomorReferensi   string `json:"nomorreferensi"`
	NomorAntrean     string `json:"nomorantrean"`
	AngkaAntrean     int    `json:"angkaantrean"`
	EstimasiDilayani int64  `json:"estimasidilayani"`
	SisaKuotaJKN     int    `json:"sisakuotajkn"`
	KuotaJKN         int    `json:"kuotajkn"`
	SisaKuotaNonJKN  int    `json:"sisakuotanonjkn"`
	KuotaNonJKN      int    `json:"kuotanonjkn"`
	Keterangan       string `json:"keterangan"`
}

func (c *Client) TambahAntrean(ctx context.Context, a AntreanAdd) error {
	_, err := c.Call(ctx, "antrean", "POST", "antrean/add", a)
	return err
}

// UpdateWaktu mengirim task id (1-7, 99) dengan waktu milidetik.
func (c *Client) UpdateWaktu(ctx context.Context, kodeBooking string, taskID int, waktu time.Time) error {
	_, err := c.Call(ctx, "antrean", "POST", "antrean/updatewaktu", map[string]any{"kodebooking": kodeBooking, "taskid": taskID, "waktu": waktu.UnixMilli()})
	return err
}

func (c *Client) BatalAntrean(ctx context.Context, kodeBooking, ket string) error {
	_, err := c.Call(ctx, "antrean", "POST", "antrean/batal", map[string]any{"kodebooking": kodeBooking, "keterangan": ket})
	return err
}
