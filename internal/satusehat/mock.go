package satusehat

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Mock adalah server tiruan SATUSEHAT (OAuth2 + FHIR) untuk demo/pengujian tanpa kredensial.
// Validasinya meniru aturan penting server asli: token Bearer, referensi harus ada,
// identifier Encounter unik per organisasi, kode sistem (ICD-10, ICD-9-CM, LOINC, UCUM),
// dan Encounter finished wajib punya diagnosis + period.end.
type Mock struct {
	ClientID, Secret, OrgID string
	TokenTTL                time.Duration

	mu        sync.Mutex
	tokens    map[string]time.Time
	resources map[string]map[string]map[string]any // type -> id -> resource
	encIdent  map[string]string
	Calls     map[string]int
}

func NewMock(clientID, secret, orgID string) *Mock {
	return &Mock{ClientID: clientID, Secret: secret, OrgID: orgID, TokenTTL: time.Hour,
		tokens: map[string]time.Time{}, resources: map[string]map[string]map[string]any{}, encIdent: map[string]string{}, Calls: map[string]int{}}
}

// Start: base auth & fhir URL.
func (m *Mock) Start() (auth, fhir string, err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", err
	}
	go http.Serve(ln, m)
	b := "http://" + ln.Addr().String()
	return b + "/oauth2/v1", b + "/fhir-r4/v1", nil
}

func uuid() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func ihs(prefix, nik string) string {
	s := sha256.Sum256([]byte(prefix + nik))
	n := uint64(0)
	for _, c := range s[:8] {
		n = n<<8 | uint64(c)
	}
	return fmt.Sprintf("%s%010d", prefix, n%10000000000)
}

func oo(w http.ResponseWriter, status int, code, text string, expr ...string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"resourceType": "OperationOutcome", "issue": []map[string]any{
		{"severity": "error", "code": code, "details": map[string]any{"text": text}, "expression": expr}}})
}

func (m *Mock) Count(rt string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.resources[rt])
}

func (m *Mock) Get(rt, id string) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.resources[rt][id]
}

// ExpireTokens memaksa semua token kedaluwarsa (uji refresh token).
func (m *Mock) ExpireTokens() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens = map[string]time.Time{}
}

func (m *Mock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := r.URL.Path
	if strings.HasSuffix(p, "/oauth2/v1/accesstoken") {
		r.ParseForm()
		if r.URL.Query().Get("grant_type") != "client_credentials" || r.PostForm.Get("client_id") != m.ClientID || r.PostForm.Get("client_secret") != m.Secret {
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]any{"fault": map[string]any{"faultstring": "Invalid client identifier"}})
			return
		}
		tok := uuid()
		m.tokens[tok] = time.Now().Add(m.TokenTTL)
		json.NewEncoder(w).Encode(map[string]any{"access_token": tok, "expires_in": fmt.Sprint(int(m.TokenTTL.Seconds())), "token_type": "BearerToken", "organization_name": "RS Tiruan"})
		return
	}
	i := strings.Index(p, "/fhir-r4/v1/")
	if i < 0 {
		oo(w, 404, "not-found", "endpoint tidak ada")
		return
	}
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if exp, ok := m.tokens[tok]; !ok || time.Now().After(exp) {
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]any{"fault": map[string]any{"faultstring": "Invalid access token"}})
		return
	}
	parts := strings.Split(strings.Trim(p[i+len("/fhir-r4/v1/"):], "/"), "/")
	rt := parts[0]
	m.Calls[r.Method+" "+rt]++
	switch {
	case r.Method == "GET" && len(parts) == 1 && (rt == "Patient" || rt == "Practitioner"):
		idf := r.URL.Query().Get("identifier")
		nik := strings.TrimPrefix(idf, SysNIK+"|")
		bundle := map[string]any{"resourceType": "Bundle", "type": "searchset", "total": 0, "entry": []any{}}
		if nik != idf && len(nik) == 16 && !strings.HasPrefix(nik, "9999") {
			pre := map[string]string{"Patient": "P", "Practitioner": "N"}[rt]
			bundle["total"] = 1
			bundle["entry"] = []any{map[string]any{"resource": map[string]any{"resourceType": rt, "id": ihs(pre, nik), "name": []any{map[string]any{"text": "NAMA " + nik[12:]}}}}}
		}
		json.NewEncoder(w).Encode(bundle)
	case r.Method == "GET" && len(parts) == 2:
		res, ok := m.resources[rt][parts[1]]
		if !ok {
			oo(w, 404, "not-found", rt+" tidak ditemukan")
			return
		}
		json.NewEncoder(w).Encode(res)
	case r.Method == "POST" && len(parts) == 1, r.Method == "PUT" && len(parts) == 2:
		body, _ := io.ReadAll(r.Body)
		var res map[string]any
		if json.Unmarshal(body, &res) != nil {
			oo(w, 400, "structure", "JSON tidak valid")
			return
		}
		if res["resourceType"] != rt {
			oo(w, 400, "invalid", "resourceType tidak sesuai endpoint", "resourceType")
			return
		}
		id := uuid()
		if r.Method == "PUT" {
			id = parts[1]
			if _, ok := m.resources[rt][id]; !ok {
				oo(w, 404, "not-found", rt+"/"+id+" tidak ditemukan")
				return
			}
			if res["id"] != id {
				oo(w, 400, "invalid", "id pada body harus sama dengan URL", "id")
				return
			}
		}
		if code, msg, expr := m.validate(rt, res, r.Method == "PUT"); code != "" {
			oo(w, 400, code, msg, expr)
			return
		}
		if rt == "Encounter" {
			key := identValue(res)
			if old, ok := m.encIdent[key]; ok && old != id {
				oo(w, 400, "duplicate", "Encounter dengan identifier "+key+" sudah ada", "identifier")
				return
			}
			m.encIdent[key] = id
		}
		res["id"] = id
		res["meta"] = map[string]any{"versionId": uuid(), "lastUpdated": time.Now().Format(time.RFC3339)}
		if m.resources[rt] == nil {
			m.resources[rt] = map[string]map[string]any{}
		}
		m.resources[rt][id] = res
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			w.WriteHeader(201)
		}
		json.NewEncoder(w).Encode(res)
	default:
		oo(w, 405, "not-supported", "operasi tidak didukung server tiruan")
	}
}

func identValue(res map[string]any) string {
	ids, _ := res["identifier"].([]any)
	for _, x := range ids {
		if m, ok := x.(map[string]any); ok {
			return fmt.Sprint(m["system"]) + "|" + fmt.Sprint(m["value"])
		}
	}
	return ""
}

func str(v any, path ...string) string {
	for _, k := range path {
		switch x := v.(type) {
		case map[string]any:
			v = x[k]
		case []any:
			if len(x) == 0 {
				return ""
			}
			v = x[0]
			if m, ok := v.(map[string]any); ok {
				v = m[k]
			}
		default:
			return ""
		}
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (m *Mock) refOK(ref, want string) bool {
	if !strings.HasPrefix(ref, want+"/") {
		return false
	}
	id := strings.TrimPrefix(ref, want+"/")
	switch want {
	case "Patient":
		return strings.HasPrefix(id, "P") && len(id) == 11
	case "Practitioner":
		return strings.HasPrefix(id, "N") && len(id) == 11
	case "Organization":
		return id == m.OrgID
	}
	_, ok := m.resources[want][id]
	return ok
}

func validDT(s string) bool {
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

var vitalUnits = map[string]string{"8867-4": "/min", "9279-1": "/min", "8310-5": "Cel", "8302-2": "cm", "29463-7": "kg", "8280-0": "cm"}

func (m *Mock) validate(rt string, r map[string]any, put bool) (string, string, string) {
	switch rt {
	case "Location":
		if str(r, "name") == "" {
			return "required", "name wajib diisi", "name"
		}
		if !m.refOK(str(r, "managingOrganization", "reference"), "Organization") {
			return "invalid", "managingOrganization harus Organization milik RS", "managingOrganization"
		}
	case "Encounter":
		st := str(r, "status")
		if st != "arrived" && st != "in-progress" && st != "finished" {
			return "code-invalid", "status Encounter tidak valid: " + st, "status"
		}
		if str(r, "class", "system") != SysActCode || !map[string]bool{"AMB": true, "IMP": true, "EMER": true}[str(r, "class", "code")] {
			return "code-invalid", "class harus v3-ActCode AMB/IMP/EMER", "class"
		}
		if !m.refOK(str(r, "subject", "reference"), "Patient") {
			return "invalid", "subject harus Patient IHS yang valid", "subject"
		}
		if !m.refOK(str(r, "participant", "individual", "reference"), "Practitioner") {
			return "invalid", "participant.individual harus Practitioner", "participant"
		}
		if !m.refOK(str(r, "location", "location", "reference"), "Location") {
			return "invalid", "Location tidak terdaftar di organisasi", "location"
		}
		if !m.refOK(str(r, "serviceProvider", "reference"), "Organization") {
			return "invalid", "serviceProvider harus Organization RS", "serviceProvider"
		}
		if !strings.HasPrefix(identValue(r), "http://sys-ids.kemkes.go.id/encounter/"+m.OrgID+"|") {
			return "invalid", "identifier.system harus http://sys-ids.kemkes.go.id/encounter/{org}", "identifier"
		}
		if !validDT(str(r, "period", "start")) {
			return "invalid", "period.start harus datetime ISO-8601 dengan zona waktu", "period.start"
		}
		hist, _ := r["statusHistory"].([]any)
		if len(hist) == 0 {
			return "required", "statusHistory wajib diisi", "statusHistory"
		}
		if last, _ := hist[len(hist)-1].(map[string]any); str(last, "status") != st {
			return "invalid", "statusHistory terakhir harus sama dengan status", "statusHistory"
		}
		if st == "finished" {
			if !put {
				return "invalid", "Encounter dibuat dulu (arrived) lalu diperbarui ke finished", "status"
			}
			if !validDT(str(r, "period", "end")) {
				return "required", "Encounter finished wajib period.end", "period.end"
			}
			dx, _ := r["diagnosis"].([]any)
			if len(dx) == 0 {
				return "required", "Encounter finished wajib memiliki diagnosis", "diagnosis"
			}
			for _, d := range dx {
				if !m.refOK(str(d, "condition", "reference"), "Condition") {
					return "invalid", "diagnosis.condition tidak ditemukan", "diagnosis"
				}
			}
		}
	case "Condition":
		if str(r, "code", "coding", "system") != SysICD10 || str(r, "code", "coding", "code") == "" {
			return "code-invalid", "code harus ICD-10", "code"
		}
		if str(r, "category", "coding", "code") != "encounter-diagnosis" {
			return "code-invalid", "category harus encounter-diagnosis", "category"
		}
		if !m.refOK(str(r, "subject", "reference"), "Patient") || !m.refOK(str(r, "encounter", "reference"), "Encounter") {
			return "invalid", "subject/encounter tidak valid", "encounter"
		}
	case "Procedure":
		if str(r, "code", "coding", "system") != SysICD9CM {
			return "code-invalid", "code harus ICD-9-CM", "code"
		}
		if !m.refOK(str(r, "subject", "reference"), "Patient") || !m.refOK(str(r, "encounter", "reference"), "Encounter") {
			return "invalid", "subject/encounter tidak valid", "encounter"
		}
	case "Observation":
		if str(r, "category", "coding", "code") == "laboratory" {
			return m.validateLabObs(r)
		}
		if str(r, "status") != "final" || str(r, "category", "coding", "code") != "vital-signs" {
			return "invalid", "status final & category vital-signs wajib", "category"
		}
		if !m.refOK(str(r, "subject", "reference"), "Patient") || !m.refOK(str(r, "encounter", "reference"), "Encounter") || !m.refOK(str(r, "performer", "reference"), "Practitioner") {
			return "invalid", "subject/encounter/performer tidak valid", "encounter"
		}
		if !validDT(str(r, "effectiveDateTime")) {
			return "invalid", "effectiveDateTime tidak valid", "effectiveDateTime"
		}
		code := str(r, "code", "coding", "code")
		if str(r, "code", "coding", "system") != SysLOINC {
			return "code-invalid", "code harus LOINC", "code"
		}
		if code == "35094-2" {
			comp, _ := r["component"].([]any)
			if len(comp) != 2 {
				return "invalid", "tekanan darah harus 2 komponen (sistolik & diastolik)", "component"
			}
			for _, c := range comp {
				if str(c, "valueQuantity", "code") != "mm[Hg]" {
					return "invalid", "satuan tekanan darah harus mm[Hg]", "component"
				}
			}
		} else if u, ok := vitalUnits[code]; ok {
			if str(r, "valueQuantity", "code") != u || str(r, "valueQuantity", "system") != SysUCUM {
				return "invalid", "satuan UCUM untuk " + code + " harus " + u, "valueQuantity"
			}
			if _, ok := r["valueQuantity"].(map[string]any)["value"].(float64); !ok {
				return "invalid", "valueQuantity.value harus angka", "valueQuantity"
			}
		} else {
			return "code-invalid", "kode LOINC tanda vital tidak dikenal: " + code, "code"
		}
	case "Medication", "MedicationRequest", "MedicationDispense", "ServiceRequest", "Specimen", "DiagnosticReport":
		return m.validate2(rt, r)
	default:
		return "not-supported", "resource " + rt + " tidak didukung server tiruan", "resourceType"
	}
	return "", "", ""
}

func isKFA(code string) bool {
	return len(code) >= 8 && strings.Trim(code, "0123456789") == ""
}

func (m *Mock) basic(r map[string]any, encKey string) (string, string, string) {
	if !m.refOK(str(r, "subject", "reference"), "Patient") {
		return "invalid", "subject harus Patient IHS yang valid", "subject"
	}
	if encKey != "" && !m.refOK(str(r, encKey, "reference"), "Encounter") {
		return "invalid", encKey + " harus Encounter yang ada", encKey
	}
	return "", "", ""
}

func (m *Mock) validateLabObs(r map[string]any) (string, string, string) {
	if str(r, "status") != "final" {
		return "invalid", "status harus final", "status"
	}
	if c, msg, e := m.basic(r, "encounter"); c != "" {
		return c, msg, e
	}
	if str(r, "code", "coding", "system") != SysLOINC || str(r, "code", "coding", "code") == "" {
		return "code-invalid", "code pemeriksaan lab harus LOINC", "code"
	}
	if !m.refOK(str(r, "specimen", "reference"), "Specimen") {
		return "invalid", "specimen harus Specimen yang ada", "specimen"
	}
	if !m.refOK(str(r, "basedOn", "reference"), "ServiceRequest") {
		return "invalid", "basedOn harus ServiceRequest", "basedOn"
	}
	if !validDT(str(r, "effectiveDateTime")) || !validDT(str(r, "issued")) {
		return "invalid", "effectiveDateTime/issued tidak valid", "issued"
	}
	if vq, ok := r["valueQuantity"].(map[string]any); ok {
		if _, ok := vq["value"].(float64); !ok || str(vq, "system") != SysUCUM || str(vq, "code") == "" || strings.Contains(str(vq, "code"), "^") {
			return "invalid", "valueQuantity harus angka dengan kode UCUM", "valueQuantity"
		}
	} else if str(r, "valueString") == "" {
		return "required", "Observation lab wajib valueQuantity atau valueString", "value[x]"
	}
	return "", "", ""
}

func (m *Mock) validate2(rt string, r map[string]any) (string, string, string) {
	switch rt {
	case "Medication":
		if str(r, "code", "coding", "system") != SysKFA || !isKFA(str(r, "code", "coding", "code")) {
			return "code-invalid", "code Medication harus kode KFA", "code"
		}
		if !m.refOK(str(r, "manufacturer", "reference"), "Organization") {
			return "invalid", "manufacturer harus Organization RS", "manufacturer"
		}
		if str(r, "extension", "valueCodeableConcept", "coding", "code") == "" {
			return "required", "extension medicationType wajib", "extension"
		}
	case "MedicationRequest":
		if str(r, "status") != "completed" && str(r, "status") != "active" || str(r, "intent") != "order" {
			return "invalid", "status/intent MedicationRequest tidak valid", "status"
		}
		if c, msg, e := m.basic(r, "encounter"); c != "" {
			return c, msg, e
		}
		if !m.refOK(str(r, "medicationReference", "reference"), "Medication") {
			return "invalid", "medicationReference harus Medication yang ada", "medicationReference"
		}
		if !m.refOK(str(r, "requester", "reference"), "Practitioner") {
			return "invalid", "requester harus Practitioner", "requester"
		}
		if !validDT(str(r, "authoredOn")) {
			return "invalid", "authoredOn tidak valid", "authoredOn"
		}
		if !strings.HasPrefix(identValue(r), "http://sys-ids.kemkes.go.id/prescription/"+m.OrgID+"|") {
			return "invalid", "identifier prescription/{org} wajib", "identifier"
		}
	case "MedicationDispense":
		if c, msg, e := m.basic(r, "context"); c != "" {
			return c, msg, e
		}
		if !m.refOK(str(r, "medicationReference", "reference"), "Medication") {
			return "invalid", "medicationReference harus Medication yang ada", "medicationReference"
		}
		if !m.refOK(str(r, "authorizingPrescription", "reference"), "MedicationRequest") {
			return "invalid", "authorizingPrescription harus MedicationRequest", "authorizingPrescription"
		}
		if !m.refOK(str(r, "location", "reference"), "Location") {
			return "invalid", "location depo farmasi tidak terdaftar", "location"
		}
		if !validDT(str(r, "whenPrepared")) || !validDT(str(r, "whenHandedOver")) {
			return "invalid", "whenPrepared/whenHandedOver tidak valid", "whenHandedOver"
		}
	case "ServiceRequest":
		if str(r, "intent") != "original-order" && str(r, "intent") != "order" {
			return "invalid", "intent ServiceRequest tidak valid", "intent"
		}
		if c, msg, e := m.basic(r, "encounter"); c != "" {
			return c, msg, e
		}
		if str(r, "code", "coding", "system") != SysLOINC || str(r, "code", "coding", "code") == "" {
			return "code-invalid", "code ServiceRequest harus LOINC", "code"
		}
		if str(r, "category", "coding", "system") != SysSNOMED {
			return "code-invalid", "category harus SNOMED-CT", "category"
		}
		if !m.refOK(str(r, "requester", "reference"), "Practitioner") {
			return "invalid", "requester harus Practitioner", "requester"
		}
	case "Specimen":
		if c, msg, e := m.basic(r, ""); c != "" {
			return c, msg, e
		}
		if str(r, "type", "coding", "system") != SysSNOMED || str(r, "type", "coding", "code") == "" {
			return "code-invalid", "type spesimen harus SNOMED-CT", "type"
		}
		if !m.refOK(str(r, "request", "reference"), "ServiceRequest") {
			return "invalid", "request harus ServiceRequest", "request"
		}
		if !validDT(str(r, "collection", "collectedDateTime")) {
			return "invalid", "collection.collectedDateTime tidak valid", "collection"
		}
	case "DiagnosticReport":
		if str(r, "status") != "final" {
			return "invalid", "status harus final", "status"
		}
		if c, msg, e := m.basic(r, "encounter"); c != "" {
			return c, msg, e
		}
		cat := str(r, "category", "coding", "code")
		if str(r, "category", "coding", "system") != SysV2_0074 || (cat != "LAB" && cat != "RAD") {
			return "code-invalid", "category harus v2-0074 LAB/RAD", "category"
		}
		if !m.refOK(str(r, "basedOn", "reference"), "ServiceRequest") {
			return "invalid", "basedOn harus ServiceRequest", "basedOn"
		}
		if cat == "LAB" && !m.refOK(str(r, "result", "reference"), "Observation") {
			return "invalid", "result harus Observation", "result"
		}
		if cat == "RAD" && strings.TrimSpace(str(r, "conclusion")) == "" {
			return "required", "conclusion (expertise) wajib untuk radiologi", "conclusion"
		}
	default:
		return "not-supported", "resource " + rt + " tidak didukung server tiruan", "resourceType"
	}
	return "", "", ""
}
