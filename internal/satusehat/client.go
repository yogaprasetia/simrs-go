// Package satusehat: bridging SATUSEHAT Kemenkes (FHIR R4).
//
// Alur mengikuti modul "Kirim SATUSEHAT" SIMKES Khanza: Encounter (arrived) -> Condition (ICD-10)
// -> Observation TTV -> Procedure (ICD-9-CM) -> Encounter diperbarui menjadi finished + diagnosis.
package satusehat

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Mode                   string // off | mock | dev | stg | prod
	ClientID, ClientSecret string
	OrgID                  string
	AuthURL, BaseURL       string
}

func (c Config) Enabled() bool { return c.Mode != "" && c.Mode != "off" }

var Endpoints = map[string][2]string{
	"dev":  {"https://api-satusehat-dev.dto.kemkes.go.id/oauth2/v1", "https://api-satusehat-dev.dto.kemkes.go.id/fhir-r4/v1"},
	"stg":  {"https://api-satusehat-stg.dto.kemkes.go.id/oauth2/v1", "https://api-satusehat-stg.dto.kemkes.go.id/fhir-r4/v1"},
	"prod": {"https://api-satusehat.kemkes.go.id/oauth2/v1", "https://api-satusehat.kemkes.go.id/fhir-r4/v1"},
}

const (
	SysNIK       = "https://fhir.kemkes.go.id/id/nik"
	SysICD10     = "http://hl7.org/fhir/sid/icd-10"
	SysICD9CM    = "http://hl7.org/fhir/sid/icd-9-cm"
	SysLOINC     = "http://loinc.org"
	SysUCUM      = "http://unitsofmeasure.org"
	SysActCode   = "http://terminology.hl7.org/CodeSystem/v3-ActCode"
	SysObsCat    = "http://terminology.hl7.org/CodeSystem/observation-category"
	SysCondCat   = "http://terminology.hl7.org/CodeSystem/condition-category"
	SysCondClin  = "http://terminology.hl7.org/CodeSystem/condition-clinical"
	SysDiagRole  = "http://terminology.hl7.org/CodeSystem/diagnosis-role"
	SysPartType  = "http://terminology.hl7.org/CodeSystem/v3-ParticipationType"
	SysLocPhys   = "http://terminology.hl7.org/CodeSystem/location-physical-type"
	SysLocMode   = "http://terminology.hl7.org/CodeSystem/location-mode"
	SysLocStatus = "http://terminology.hl7.org/CodeSystem/v3-RoleCode"
)

type Client struct {
	Cfg  Config
	HTTP *http.Client
	DB   *sql.DB

	mu      sync.Mutex
	token   string
	expires time.Time
}

func New(cfg Config, db *sql.DB) *Client {
	return &Client{Cfg: cfg, DB: db, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Error: galat dari SATUSEHAT (OperationOutcome) atau HTTP.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return fmt.Sprintf("SATUSEHAT %d: %s", e.Status, e.Msg) }

func (c *Client) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expires) {
		return c.token, nil
	}
	if !c.Cfg.Enabled() {
		return "", errors.New("bridging SATUSEHAT tidak aktif (SATUSEHAT_MODE=off)")
	}
	form := url.Values{"client_id": {c.Cfg.ClientID}, "client_secret": {c.Cfg.ClientSecret}}
	req, _ := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.Cfg.AuthURL, "/")+"/accesstoken?grant_type=client_credentials", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	start := time.Now()
	resp, err := c.HTTP.Do(req)
	if err != nil {
		c.log("POST", "oauth2/accesstoken", "", 0, err.Error(), start)
		return "", fmt.Errorf("gagal menghubungi SATUSEHAT: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   any    `json:"expires_in"`
	}
	if resp.StatusCode != 200 || json.Unmarshal(body, &t) != nil || t.AccessToken == "" {
		msg := outcome(body)
		c.log("POST", "oauth2/accesstoken", "", resp.StatusCode, msg, start)
		return "", &Error{resp.StatusCode, "autentikasi gagal (cek client id/secret): " + msg}
	}
	c.log("POST", "oauth2/accesstoken", "", 200, "token OK", start)
	sec := 3599
	switch v := t.ExpiresIn.(type) {
	case string:
		sec, _ = strconv.Atoi(v)
	case float64:
		sec = int(v)
	}
	if sec < 120 {
		sec = 120
	}
	c.token, c.expires = t.AccessToken, time.Now().Add(time.Duration(sec-60)*time.Second)
	return c.token, nil
}

func (c *Client) resetToken() { c.mu.Lock(); c.token = ""; c.mu.Unlock() }

// outcome mengambil pesan galat yang mudah dibaca dari OperationOutcome / respons lain.
func outcome(b []byte) string {
	var oo struct {
		ResourceType string `json:"resourceType"`
		Issue        []struct {
			Code        string `json:"code"`
			Diagnostics string `json:"diagnostics"`
			Details     struct {
				Text string `json:"text"`
			} `json:"details"`
			Expression []string `json:"expression"`
		} `json:"issue"`
		Fault struct {
			FaultString string `json:"faultstring"`
		} `json:"fault"`
		ErrorDesc string `json:"error_description"`
	}
	if json.Unmarshal(b, &oo) == nil {
		var parts []string
		for _, i := range oo.Issue {
			m := i.Details.Text
			if m == "" {
				m = i.Diagnostics
			}
			if m == "" {
				m = i.Code
			}
			if len(i.Expression) > 0 {
				m += " (" + strings.Join(i.Expression, ", ") + ")"
			}
			parts = append(parts, m)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
		if oo.Fault.FaultString != "" {
			return oo.Fault.FaultString
		}
		if oo.ErrorDesc != "" {
			return oo.ErrorDesc
		}
	}
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// Do memanggil FHIR API. path relatif terhadap BaseURL (mis. "Encounter", "Patient?identifier=...").
func (c *Client) Do(ctx context.Context, method, path string, body any, out any) error {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := c.Token(ctx)
		if err != nil {
			return err
		}
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, _ := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Cfg.BaseURL, "/")+"/"+strings.TrimLeft(path, "/"), rd)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		start := time.Now()
		resp, err := c.HTTP.Do(req)
		if err != nil {
			c.log(method, path, string(payload), 0, err.Error(), start)
			return fmt.Errorf("gagal menghubungi SATUSEHAT: %w", err)
		}
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if resp.StatusCode == 401 && attempt == 0 {
			c.log(method, path, string(payload), 401, "token kedaluwarsa, ulang", start)
			c.resetToken()
			continue
		}
		if resp.StatusCode >= 300 {
			msg := outcome(rb)
			c.log(method, path, string(payload), resp.StatusCode, msg, start)
			return &Error{resp.StatusCode, msg}
		}
		c.log(method, path, string(payload), resp.StatusCode, "OK", start)
		if out != nil {
			if err := json.Unmarshal(rb, out); err != nil {
				return fmt.Errorf("respons SATUSEHAT tidak valid: %w", err)
			}
		}
		return nil
	}
	return &Error{401, "autentikasi ditolak"}
}

func (c *Client) log(method, path, req string, code int, msg string, start time.Time) {
	if c.DB == nil {
		return
	}
	if len(req) > 4000 {
		req = req[:4000]
	}
	if len(msg) > 490 {
		msg = msg[:490]
	}
	if len(path) > 250 {
		path = path[:250]
	}
	_, _ = c.DB.Exec("INSERT INTO app_satusehat_log (waktu,method,url,request,code,message,durasi_ms) VALUES (NOW(),?,?,?,?,?,?)",
		method, path, req, code, msg, int(time.Since(start).Milliseconds()))
}

// Create POST resource, mengembalikan id baru.
func (c *Client) Create(ctx context.Context, resource map[string]any) (string, error) {
	rt, _ := resource["resourceType"].(string)
	var out struct {
		ID string `json:"id"`
	}
	if err := c.Do(ctx, "POST", rt, resource, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("SATUSEHAT tidak mengembalikan id " + rt)
	}
	return out.ID, nil
}

// SearchByNIK mencari Patient/Practitioner berdasarkan NIK, mengembalikan IHS number.
func (c *Client) SearchByNIK(ctx context.Context, resource, nik string) (string, string, error) {
	var b struct {
		Total int `json:"total"`
		Entry []struct {
			Resource struct {
				ID   string `json:"id"`
				Name []struct {
					Text string `json:"text"`
				} `json:"name"`
			} `json:"resource"`
		} `json:"entry"`
	}
	if err := c.Do(ctx, "GET", resource+"?identifier="+url.QueryEscape(SysNIK+"|"+nik), nil, &b); err != nil {
		return "", "", err
	}
	if len(b.Entry) == 0 {
		return "", "", fmt.Errorf("%s dengan NIK %s tidak ditemukan di SATUSEHAT", map[string]string{"Patient": "pasien", "Practitioner": "tenaga kesehatan"}[resource], nik)
	}
	nama := ""
	if len(b.Entry[0].Resource.Name) > 0 {
		nama = b.Entry[0].Resource.Name[0].Text
	}
	return b.Entry[0].Resource.ID, nama, nil
}
