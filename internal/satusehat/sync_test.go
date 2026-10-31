package satusehat_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yogaprasetia/simrs-go/internal/core"
	"github.com/yogaprasetia/simrs-go/internal/db"
	"github.com/yogaprasetia/simrs-go/internal/satusehat"
)

func testDB(t *testing.T) *sql.DB {
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
	if err := db.Migrate(context.Background(), d, true); err != nil {
		t.Fatal(err)
	}
	return d
}

func must(t *testing.T, d *sql.DB, fn func(*sql.Tx) error) {
	t.Helper()
	if err := core.WithTx(context.Background(), d, fn); err != nil {
		t.Fatal(err)
	}
}

func q(t *testing.T, d *sql.DB, query string, args ...any) string {
	t.Helper()
	var v sql.NullString
	if err := d.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v.String
}

func TestSyncRalanDanRanap(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	core.Now = func() time.Time { return time.Date(2026, 11, 16, 8, 0, 0, 0, core.Loc) }
	defer func() { core.Now = func() time.Time { return time.Now().In(core.Loc) } }()

	m := satusehat.NewMock("cid", "csecret", "100000001")
	auth, fhir, _ := m.Start()
	c := satusehat.New(satusehat.Config{Mode: "mock", ClientID: "cid", ClientSecret: "csecret", OrgID: "100000001", AuthURL: auth, BaseURL: fhir}, d)

	// kredensial salah
	bad := satusehat.New(satusehat.Config{Mode: "mock", ClientID: "cid", ClientSecret: "salah", OrgID: "100000001", AuthURL: auth, BaseURL: fhir}, nil)
	if _, err := bad.Token(ctx); err == nil || !strings.Contains(err.Error(), "client") {
		t.Fatalf("token dengan secret salah: %v", err)
	}

	p := &core.Pasien{Nama: "Budi", JK: "L", TglLahir: "1980-01-01", NoKTP: "3372010101800001"}
	tanpaNIK := &core.Pasien{Nama: "Tanpa NIK", JK: "P", TglLahir: "1990-01-01"}
	var r1, r2 core.RegResult
	must(t, d, func(x *sql.Tx) (err error) {
		if err = core.SimpanPasien(ctx, x, p); err != nil {
			return
		}
		if err = core.SimpanPasien(ctx, x, tanpaNIK); err != nil {
			return
		}
		if r1, err = core.Registrasi(ctx, x, core.RegInput{NoRM: p.NoRM, KdPoli: "INT", KdDokter: "D0000002", KdPJ: "A09"}); err != nil {
			return
		}
		r2, err = core.Registrasi(ctx, x, core.RegInput{NoRM: tanpaNIK.NoRM, KdPoli: "U0001", KdDokter: "D0000001", KdPJ: "A09"})
		return
	})
	must(t, d, func(x *sql.Tx) error {
		_, err := core.SimpanSOAP(ctx, x, core.SOAP{NoRawat: r1.NoRawat, Tensi: "150/95", Nadi: "88", Respirasi: "20", Suhu: "36,8", Tinggi: "170", Berat: "abc", Keluhan: "pusing", NIP: "D0000002"})
		return err
	})
	must(t, d, func(x *sql.Tx) error {
		_, err := core.SimpanSOAP(ctx, x, core.SOAP{NoRawat: r2.NoRawat, Keluhan: "batuk", NIP: "D0000001"})
		return err
	})

	// belum ada mapping lokasi -> gagal dengan pesan jelas
	res := c.Sync(ctx, d, r1.NoRawat)
	if res.Err() == nil || !strings.Contains(res.Err().Error(), "belum di-mapping") {
		t.Fatalf("tanpa mapping: %v", res)
	}
	if _, err := c.CreateLocation(ctx, d, "ralan", "INT"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateLocation(ctx, d, "ralan", "U0001"); err != nil {
		t.Fatal(err)
	}
	if m.Count("Location") != 2 {
		t.Fatal("location")
	}
	// pasien tanpa NIK
	if res := c.Sync(ctx, d, r2.NoRawat); res.Err() == nil || !strings.Contains(res.Err().Error(), "NIK pasien") {
		t.Fatalf("tanpa NIK: %v", res)
	}
	if !strings.Contains(q(t, d, "SELECT pesan FROM app_satusehat_error WHERE no_rawat=?", r2.NoRawat), "NIK pasien") {
		t.Fatal("galat tidak tersimpan")
	}

	res = c.Sync(ctx, d, r1.NoRawat)
	if res.Err() != nil {
		t.Fatalf("sync 1: %v", res.Errors)
	}
	if q(t, d, "SELECT COUNT(*) FROM app_satusehat_error WHERE no_rawat=?", r1.NoRawat) != "0" {
		t.Fatal("galat lama harus terhapus setelah sukses")
	}
	// encounter + nadi, respirasi, suhu, tensi, tb (bb "abc" dilewati dengan peringatan)
	if m.Count("Encounter") != 1 || m.Count("Observation") != 5 || len(res.Warnings) != 1 {
		t.Fatalf("enc %d obs %d warn %v", m.Count("Encounter"), m.Count("Observation"), res.Warnings)
	}
	enc := q(t, d, "SELECT id_encounter FROM satu_sehat_encounter WHERE no_rawat=?", r1.NoRawat)
	e := m.Get("Encounter", enc)
	if e["status"] != "arrived" || e["class"].(map[string]any)["code"] != "AMB" {
		t.Fatalf("encounter: %v", e)
	}
	// idempoten: kirim ulang tidak menggandakan
	if res := c.Sync(ctx, d, r1.NoRawat); len(res.Sent) != 0 || res.Err() != nil {
		t.Fatalf("sync ulang: %+v", res)
	}
	// token kedaluwarsa -> client memperbarui otomatis
	m.ExpireTokens()
	must(t, d, func(x *sql.Tx) error { return core.TambahDiagnosa(ctx, x, r1.NoRawat, "I10", false) })
	must(t, d, func(x *sql.Tx) error { return core.TambahDiagnosa(ctx, x, r1.NoRawat, "E78.5", false) })
	must(t, d, func(x *sql.Tx) error { return core.TambahProsedur(ctx, x, r1.NoRawat, "89.52", false) })
	res = c.Sync(ctx, d, r1.NoRawat)
	if res.Err() != nil || m.Count("Condition") != 2 || m.Count("Procedure") != 1 {
		t.Fatalf("sync 2: %v cond=%d", res.Errors, m.Count("Condition"))
	}
	if q(t, d, "SELECT status FROM app_satusehat_encounter_status WHERE no_rawat=?", r1.NoRawat) != "arrived" {
		t.Fatal("belum bayar -> harus tetap arrived")
	}
	core.Now = func() time.Time { return time.Date(2026, 11, 16, 9, 30, 0, 0, core.Loc) }
	must(t, d, func(x *sql.Tx) error { _, err := core.BayarBilling(ctx, x, r1.NoRawat); return err })
	res = c.Sync(ctx, d, r1.NoRawat)
	if res.Err() != nil {
		t.Fatalf("finish: %v", res.Errors)
	}
	e = m.Get("Encounter", enc)
	if e["status"] != "finished" || len(e["diagnosis"].([]any)) != 2 {
		t.Fatalf("encounter finished: %v", e)
	}
	if end := e["period"].(map[string]any)["end"]; end != "2026-11-16T09:30:00+07:00" {
		t.Fatalf("period.end %v", end)
	}
	if hist := e["statusHistory"].([]any); len(hist) != 3 {
		t.Fatalf("statusHistory %v", hist)
	}

	// ---------- IGD -> ranap: class IMP, lokasi kamar, finished saat pulang, wajib diagnosa
	p3 := &core.Pasien{Nama: "Siti", JK: "P", TglLahir: "1990-05-05", NoKTP: "3372014505900002"}
	var r3 core.RegResult
	must(t, d, func(x *sql.Tx) (err error) {
		if err = core.SimpanPasien(ctx, x, p3); err != nil {
			return
		}
		r3, err = core.Registrasi(ctx, x, core.RegInput{NoRM: p3.NoRM, KdPoli: "IGDK", KdDokter: "D0000001", KdPJ: "A09"})
		return
	})
	if _, err := c.CreateLocation(ctx, d, "ralan", "IGDK"); err != nil {
		t.Fatal(err)
	}
	must(t, d, func(x *sql.Tx) error {
		_, err := core.SimpanSOAP(ctx, x, core.SOAP{NoRawat: r3.NoRawat, Suhu: "39", Nadi: "110", Keluhan: "demam", NIP: "D0000001"})
		return err
	})
	if res := c.Sync(ctx, d, r3.NoRawat); res.Err() != nil {
		t.Fatal(res.Errors)
	}
	enc3 := q(t, d, "SELECT id_encounter FROM satu_sehat_encounter WHERE no_rawat=?", r3.NoRawat)
	if m.Get("Encounter", enc3)["class"].(map[string]any)["code"] != "EMER" {
		t.Fatal("IGD harus EMER")
	}
	if _, err := c.CreateLocation(ctx, d, "ranap", "MLT.01"); err != nil {
		t.Fatal(err)
	}
	must(t, d, func(x *sql.Tx) error { return core.MasukKamar(ctx, x, r3.NoRawat, "MLT.01", "DHF", "D0000002") })
	core.Now = func() time.Time { return time.Date(2026, 11, 17, 8, 0, 0, 0, core.Loc) }
	must(t, d, func(x *sql.Tx) error {
		_, err := core.SimpanSOAP(ctx, x, core.SOAP{NoRawat: r3.NoRawat, Suhu: "37,5", Tensi: "110/70", Keluhan: "membaik", NIP: "D0000002", Ranap: true})
		return err
	})
	core.Now = func() time.Time { return time.Date(2026, 11, 18, 10, 0, 0, 0, core.Loc) }
	must(t, d, func(x *sql.Tx) error { return core.PulangRanap(ctx, x, r3.NoRawat, "Membaik", "A91") })
	res = c.Sync(ctx, d, r3.NoRawat)
	if res.Err() == nil || !strings.Contains(res.Err().Error(), "diagnosa") {
		t.Fatalf("finish tanpa diagnosa seharusnya ditolak: %v", res)
	}
	if n := q(t, d, "SELECT COUNT(*) FROM satu_sehat_observationttvsuhu WHERE no_rawat=? AND status='Ranap'", r3.NoRawat); n != "1" {
		t.Fatalf("observasi ranap %s", n)
	}
	must(t, d, func(x *sql.Tx) error { return core.TambahDiagnosa(ctx, x, r3.NoRawat, "A91", true) })
	res = c.Sync(ctx, d, r3.NoRawat)
	if res.Err() != nil {
		t.Fatal(res.Errors)
	}
	e3 := m.Get("Encounter", enc3)
	if e3["status"] != "finished" || e3["class"].(map[string]any)["code"] != "IMP" {
		t.Fatalf("ranap finished: %v %v", e3["status"], e3["class"])
	}
	if !strings.Contains(e3["location"].([]any)[0].(map[string]any)["location"].(map[string]any)["reference"].(string), q(t, d, "SELECT id_lokasi_satusehat FROM satu_sehat_mapping_lokasi_ranap WHERE kd_kamar='MLT.01'")) {
		t.Fatal("lokasi ranap harus kamar")
	}
	if dpjp := e3["participant"].([]any)[0].(map[string]any)["individual"].(map[string]any)["display"]; dpjp != "dr. Sari Wulandari, Sp.PD" {
		t.Fatalf("participant ranap = DPJP, dapat %v", dpjp)
	}

	// Kandidat: r1 & r3 selesai, r2 tertahan (tanpa NIK)
	list, _ := satusehat.Kandidat(ctx, d, "2026-11-01", "2026-11-30")
	if len(list) != 1 || list[0] != r2.NoRawat {
		t.Fatalf("kandidat %v", list)
	}
	if n := q(t, d, "SELECT COUNT(*) FROM app_satusehat_log WHERE code>=400"); n == "0" {
		t.Fatal("log galat kosong")
	}
	// dokter tanpa NIK di pegawai
	d.Exec("UPDATE pegawai SET no_ktp='' WHERE nik='D0000001'")
	d.Exec("DELETE FROM app_satusehat_ihs WHERE jenis='Practitioner'")
	d.Exec("UPDATE pasien SET no_ktp='3372010202950009' WHERE no_rkm_medis=?", tanpaNIK.NoRM)
	if res := c.Sync(ctx, d, r2.NoRawat); res.Err() == nil || !strings.Contains(res.Err().Error(), "NIK dokter") {
		t.Fatalf("dokter tanpa NIK: %v", res)
	}
}

// Server tiruan harus menolak payload yang salah agar pengujian Sync bermakna.
func TestMockMenolakPayloadSalah(t *testing.T) {
	ctx := context.Background()
	m := satusehat.NewMock("a", "b", "100")
	auth, fhir, _ := m.Start()
	c := satusehat.New(satusehat.Config{Mode: "mock", ClientID: "a", ClientSecret: "b", OrgID: "100", AuthURL: auth, BaseURL: fhir}, nil)
	loc, err := c.Create(ctx, map[string]any{"resourceType": "Location", "name": "Poli", "managingOrganization": map[string]any{"reference": "Organization/100"}})
	if err != nil {
		t.Fatal(err)
	}
	enc := func(status string) map[string]any {
		return map[string]any{"resourceType": "Encounter", "status": status,
			"identifier":      []any{map[string]any{"system": "http://sys-ids.kemkes.go.id/encounter/100", "value": "X1"}},
			"class":           map[string]any{"system": satusehat.SysActCode, "code": "AMB"},
			"subject":         map[string]any{"reference": "Patient/P0000000001"},
			"participant":     []any{map[string]any{"individual": map[string]any{"reference": "Practitioner/N0000000001"}}},
			"location":        []any{map[string]any{"location": map[string]any{"reference": "Location/" + loc}}},
			"serviceProvider": map[string]any{"reference": "Organization/100"},
			"period":          map[string]any{"start": "2026-11-16T08:00:00+07:00"},
			"statusHistory":   []any{map[string]any{"status": status}}}
	}
	if _, err := c.Create(ctx, enc("finished")); err == nil {
		t.Fatal("POST finished seharusnya ditolak")
	}
	e := enc("arrived")
	e["serviceProvider"] = map[string]any{"reference": "Organization/999"}
	if _, err := c.Create(ctx, e); err == nil {
		t.Fatal("organisasi lain seharusnya ditolak")
	}
	id, err := c.Create(ctx, enc("arrived"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create(ctx, enc("arrived")); err == nil || !strings.Contains(err.Error(), "sudah ada") {
		t.Fatalf("identifier ganda: %v", err)
	}
	fin := enc("finished")
	fin["id"] = id
	fin["period"] = map[string]any{"start": "2026-11-16T08:00:00+07:00", "end": "2026-11-16T09:00:00+07:00"}
	if err := c.Do(ctx, "PUT", "Encounter/"+id, fin, nil); err == nil || !strings.Contains(err.Error(), "diagnosis") {
		t.Fatalf("finished tanpa diagnosis: %v", err)
	}
	obs := map[string]any{"resourceType": "Observation", "status": "final",
		"category":  []any{map[string]any{"coding": []any{map[string]any{"code": "vital-signs"}}}},
		"code":      map[string]any{"coding": []any{map[string]any{"system": satusehat.SysLOINC, "code": "8310-5"}}},
		"subject":   map[string]any{"reference": "Patient/P0000000001"},
		"encounter": map[string]any{"reference": "Encounter/" + id}, "performer": []any{map[string]any{"reference": "Practitioner/N0000000001"}},
		"effectiveDateTime": "2026-11-16T08:10:00+07:00",
		"valueQuantity":     map[string]any{"value": 37.0, "system": satusehat.SysUCUM, "code": "F"}}
	if _, err := c.Create(ctx, obs); err == nil || !strings.Contains(err.Error(), "UCUM") {
		t.Fatalf("satuan salah: %v", err)
	}
	obs["valueQuantity"].(map[string]any)["code"] = "Cel"
	if _, err := c.Create(ctx, obs); err != nil {
		t.Fatal(err)
	}
}
