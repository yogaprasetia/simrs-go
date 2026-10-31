package bpjs

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Mock adalah server tiruan VClaim & Antrean untuk demo/pengujian tanpa kredensial BPJS.
// Ia memverifikasi signature dan mengenkripsi respons persis seperti server BPJS.
type Mock struct {
	ConsID, Secret string
	mu             sync.Mutex
	seps           map[string]map[string]any
	antrean        map[string]map[string]any
	seq            int
}

var mockPeserta = map[string]map[string]any{
	"0001234567890": {"nama": "BUDI HARTONO", "nik": "3372010101800001", "sex": "L", "tglLahir": "1980-01-01", "kelas": "3", "ppk": "0101R001"},
	"0001234567891": {"nama": "SITI AMINAH", "nik": "3372014505900002", "sex": "P", "tglLahir": "1990-05-05", "kelas": "2", "ppk": "0101R001"},
	"0001234567892": {"nama": "AHMAD FAUZI", "nik": "3372011212150003", "sex": "L", "tglLahir": "2015-12-12", "kelas": "1", "ppk": "0101R001"},
	"0009999999999": {"nama": "PESERTA NONAKTIF", "nik": "3372010101700009", "sex": "L", "tglLahir": "1970-01-01", "kelas": "3", "ppk": "0101R001", "nonaktif": true},
}

func pesertaJSON(noka string, p map[string]any) map[string]any {
	status := map[string]string{"kode": "0", "keterangan": "AKTIF"}
	if p["nonaktif"] == true {
		status = map[string]string{"kode": "1", "keterangan": "TIDAK AKTIF KARENA PREMI"}
	}
	return map[string]any{
		"noKartu": noka, "nik": p["nik"], "nama": p["nama"], "sex": p["sex"], "tglLahir": p["tglLahir"], "pisa": "1",
		"hakKelas":      map[string]string{"kode": p["kelas"].(string), "keterangan": "KELAS " + map[string]string{"1": "I", "2": "II", "3": "III"}[p["kelas"].(string)]},
		"statusPeserta": status,
		"jenisPeserta":  map[string]string{"kode": "14", "keterangan": "PBI (APBN)"},
		"provUmum":      map[string]string{"kdProvider": "0101G001", "nmProvider": "PUSKESMAS CONTOH"},
		"mr":            map[string]string{"noMR": "", "noTelepon": "081234567890"},
		"umur":          map[string]string{"umurSekarang": "", "umurSaatPelayanan": ""},
	}
}

func NewMock(consID, secret string) *Mock {
	return &Mock{ConsID: consID, Secret: secret, seps: map[string]map[string]any{}, antrean: map[string]map[string]any{}}
}

// Start menjalankan server di 127.0.0.1 port acak; mengembalikan base URL vclaim & antrean.
func (m *Mock) Start() (vclaim, antrean string, err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", err
	}
	go http.Serve(ln, m)
	base := "http://" + ln.Addr().String()
	return base + "/vclaim-rest/", base + "/antreanrs/", nil
}

func (m *Mock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ts := r.Header.Get("X-timestamp")
	antreanSvc := strings.HasPrefix(r.URL.Path, "/antreanrs/")
	write := func(code, msg string, resp any) {
		meta := map[string]any{"code": code, "message": msg}
		key := "metaData"
		if antreanSvc {
			key = "metadata"
			var n int
			fmt.Sscan(code, &n)
			meta["code"] = n
		}
		out := map[string]any{key: meta}
		if resp != nil {
			b, _ := json.Marshal(resp)
			out["response"] = Encrypt(m.ConsID, m.Secret, ts, string(b))
		} else {
			out["response"] = nil
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
	if r.Header.Get("X-cons-id") != m.ConsID || r.Header.Get("X-signature") != Signature(m.ConsID, m.Secret, ts) || r.Header.Get("user_key") == "" {
		write("401", "Authentication failed", nil)
		return
	}
	var tsn int64
	fmt.Sscan(ts, &tsn)
	if d := time.Now().Unix() - tsn; d > 300 || d < -300 {
		write("401", "Timestamp tidak valid", nil)
		return
	}
	p := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(p, "/")
	body, _ := io.ReadAll(r.Body)
	m.mu.Lock()
	defer m.mu.Unlock()
	if antreanSvc {
		m.antreanRoute(parts[1:], body, write)
		return
	}
	parts = parts[1:]
	switch {
	case len(parts) >= 3 && parts[0] == "Peserta" && (parts[1] == "nokartu" || parts[1] == "nik"):
		for noka, ps := range mockPeserta {
			if (parts[1] == "nokartu" && noka == parts[2]) || (parts[1] == "nik" && ps["nik"] == parts[2]) {
				write("200", "OK", map[string]any{"peserta": pesertaJSON(noka, ps)})
				return
			}
		}
		write("201", "Peserta tidak ditemukan", nil)
	case len(parts) >= 2 && parts[0] == "Rujukan":
		no := parts[len(parts)-1]
		noka := ""
		if parts[1] == "Peserta" {
			noka = no
			no = "0101G0011025P000" + no[len(no)-3:]
		} else {
			for k := range mockPeserta {
				if strings.HasSuffix(no, k[len(k)-3:]) {
					noka = k
				}
			}
		}
		ps, ok := mockPeserta[noka]
		if !ok {
			write("201", "Data rujukan tidak ditemukan", nil)
			return
		}
		write("200", "OK", map[string]any{"rujukan": map[string]any{
			"noKunjungan": no, "tglKunjungan": time.Now().AddDate(0, 0, -3).Format("2006-01-02"),
			"provPerujuk": map[string]string{"kode": "0101G001", "nama": "PUSKESMAS CONTOH"},
			"poliRujukan": map[string]string{"kode": "INT", "nama": "PENYAKIT DALAM"},
			"diagnosa":    map[string]string{"kode": "I10", "nama": "Essential (primary) hypertension"},
			"keluhan":     "kontrol tekanan darah", "pelayanan": map[string]string{"kode": "2", "nama": "Rawat Jalan"},
			"peserta": pesertaJSON(noka, ps),
		}})
	case len(parts) >= 3 && parts[0] == "SEP" && parts[2] == "insert" && r.Method == "POST":
		var req struct {
			Request struct {
				TSep map[string]any `json:"t_sep"`
			} `json:"request"`
		}
		if json.Unmarshal(body, &req) != nil || req.Request.TSep == nil {
			write("201", "Format request salah", nil)
			return
		}
		t := req.Request.TSep
		noka, _ := t["noKartu"].(string)
		ps, ok := mockPeserta[noka]
		if !ok {
			write("201", "Peserta tidak ditemukan", nil)
			return
		}
		if ps["nonaktif"] == true {
			write("201", "Status Kepesertaan Tidak Aktif", nil)
			return
		}
		if t["diagAwal"] == "" {
			write("201", "Diagnosa Awal tidak boleh kosong", nil)
			return
		}
		m.seq++
		no := fmt.Sprintf("0101R0011%s%s%04d", time.Now().Format("01"), time.Now().Format("06"), m.seq)
		poli, _ := t["poli"].(map[string]any)
		sep := map[string]any{
			"noSep": no, "tglSep": t["tglSep"], "jnsPelayanan": map[string]string{"1": "Rawat Inap", "2": "Rawat Jalan"}[fmt.Sprint(t["jnsPelayanan"])],
			"kelasRawat": ps["kelas"], "diagnosa": t["diagAwal"], "noRujukan": t["rujukan"].(map[string]any)["noRujukan"],
			"poli": poli["tujuan"], "catatan": t["catatan"], "penjamin": "-",
			"peserta":   map[string]any{"noKartu": noka, "nama": ps["nama"], "tglLahir": ps["tglLahir"], "kelamin": ps["sex"], "hakKelas": "Kelas " + ps["kelas"].(string), "jnsPeserta": "PBI (APBN)", "noMr": t["noMR"], "asuransi": "-"},
			"informasi": map[string]any{"dinsos": nil, "prolanisPRB": nil, "noSKTM": nil},
		}
		m.seps[no] = sep
		write("200", "Sukses", map[string]any{"sep": sep})
	case len(parts) >= 3 && parts[0] == "SEP" && parts[2] == "delete":
		var req struct {
			Request struct {
				TSep map[string]string `json:"t_sep"`
			} `json:"request"`
		}
		json.Unmarshal(body, &req)
		no := req.Request.TSep["noSep"]
		if _, ok := m.seps[no]; !ok {
			write("201", "SEP tidak ditemukan", nil)
			return
		}
		delete(m.seps, no)
		write("200", "Sukses", no)
	case len(parts) >= 2 && parts[0] == "SEP":
		if s, ok := m.seps[parts[1]]; ok {
			write("200", "OK", s)
		} else {
			write("201", "SEP tidak ditemukan", nil)
		}
	case len(parts) >= 3 && parts[0] == "referensi" && parts[1] == "diagnosa":
		q := strings.ToLower(parts[2])
		var list []map[string]string
		for _, d := range [][2]string{{"I10", "Essential (primary) hypertension"}, {"E11.9", "Non-insulin-dependent diabetes mellitus without complications"}, {"J06.9", "Acute upper respiratory infection, unspecified"}, {"A09", "Diarrhoea and gastroenteritis"}, {"K29.7", "Gastritis, unspecified"}} {
			if strings.Contains(strings.ToLower(d[0]+" "+d[1]), q) {
				list = append(list, map[string]string{"kode": d[0], "nama": d[0] + " - " + d[1]})
			}
		}
		write("200", "OK", map[string]any{"diagnosa": list})
	case len(parts) >= 3 && parts[0] == "referensi" && parts[1] == "poli":
		write("200", "OK", map[string]any{"poli": []map[string]string{{"kode": "INT", "nama": "PENYAKIT DALAM"}, {"kode": "ANA", "nama": "ANAK"}, {"kode": "OBG", "nama": "OBGYN"}, {"kode": "GIG", "nama": "GIGI"}, {"kode": "UMU", "nama": "UMUM"}, {"kode": "BED", "nama": "BEDAH"}}})
	default:
		write("404", "Endpoint tidak ditemukan di server tiruan", nil)
	}
}

func (m *Mock) antreanRoute(parts []string, body []byte, write func(string, string, any)) {
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	path := strings.Join(parts, "/")
	switch path {
	case "antrean/add":
		kb, _ := req["kodebooking"].(string)
		if kb == "" {
			write("201", "Kode booking wajib diisi", nil)
			return
		}
		if _, ok := m.antrean[kb]; ok {
			write("208", "Terdapat duplikasi Kode Booking", nil)
			return
		}
		req["tasks"] = map[string]int64{}
		m.antrean[kb] = req
		write("200", "Ok.", nil)
	case "antrean/updatewaktu":
		kb, _ := req["kodebooking"].(string)
		a, ok := m.antrean[kb]
		if !ok {
			write("201", "Kode booking tidak ditemukan", nil)
			return
		}
		tid := fmt.Sprint(req["taskid"])
		w, _ := req["waktu"].(float64)
		a["tasks"].(map[string]int64)[tid] = int64(w)
		write("200", "Ok.", nil)
	case "antrean/batal":
		kb, _ := req["kodebooking"].(string)
		if _, ok := m.antrean[kb]; !ok {
			write("201", "Kode booking tidak ditemukan", nil)
			return
		}
		delete(m.antrean, kb)
		write("200", "Ok.", nil)
	case "ref/poli":
		write("200", "Ok", []map[string]string{{"kdpoli": "INT", "nmpoli": "PENYAKIT DALAM"}, {"kdpoli": "ANA", "nmpoli": "ANAK"}})
	default:
		write("201", "Endpoint tidak ditemukan", nil)
	}
}

// Antrean mengembalikan salinan data antrean tiruan (untuk pengujian).
func (m *Mock) Antrean(kb string) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.antrean[kb]
}
