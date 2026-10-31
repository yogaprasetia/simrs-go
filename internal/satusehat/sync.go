package satusehat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Waktu lokal RS (WIB) dalam format FHIR.
func fhirTime(tgl, jam string) string {
	if len(jam) == 5 {
		jam += ":00"
	}
	if len(tgl) > 10 {
		tgl = tgl[:10]
	}
	return tgl + "T" + jam + "+07:00"
}

func ref(rt, id, display string) map[string]any {
	m := map[string]any{"reference": rt + "/" + id}
	if display != "" {
		m["display"] = display
	}
	return m
}

func coding(system, code, display string) map[string]any {
	return map[string]any{"coding": []any{map[string]any{"system": system, "code": code, "display": display}}}
}

func val(ctx context.Context, db *sql.DB, q string, args ...any) string {
	var v sql.NullString
	_ = db.QueryRowContext(ctx, q, args...).Scan(&v)
	return v.String
}

// IHS mencari & meng-cache IHS number Patient/Practitioner berdasarkan NIK.
func (c *Client) IHS(ctx context.Context, db *sql.DB, jenis, nik string) (string, error) {
	nik = strings.TrimSpace(nik)
	if len(nik) != 16 {
		return "", fmt.Errorf("NIK %s kosong/tidak 16 digit", map[string]string{"Patient": "pasien", "Practitioner": "dokter"}[jenis])
	}
	if id := val(ctx, db, "SELECT ihs FROM app_satusehat_ihs WHERE jenis=? AND nik=?", jenis, nik); id != "" {
		return id, nil
	}
	id, nama, err := c.SearchByNIK(ctx, jenis, nik)
	if err != nil {
		return "", err
	}
	_, _ = db.ExecContext(ctx, "INSERT INTO app_satusehat_ihs VALUES (?,?,?,?,NOW()) ON DUPLICATE KEY UPDATE ihs=VALUES(ihs), nama=VALUES(nama), waktu=NOW()", jenis, nik, id, nama)
	return id, nil
}

// CreateLocation membuat Location di SATUSEHAT untuk poli (ralan) atau kamar (ranap) lalu menyimpan mapping-nya.
func (c *Client) CreateLocation(ctx context.Context, db *sql.DB, jenis, kode string) (string, error) {
	var nama, table, key string
	switch jenis {
	case "ralan":
		nama, table, key = val(ctx, db, "SELECT nm_poli FROM poliklinik WHERE kd_poli=?", kode), "satu_sehat_mapping_lokasi_ralan", "kd_poli"
	case "ranap":
		nama, table, key = val(ctx, db, "SELECT CONCAT(b.nm_bangsal,' ',k.kd_kamar) FROM kamar k JOIN bangsal b ON b.kd_bangsal=k.kd_bangsal WHERE k.kd_kamar=?", kode), "satu_sehat_mapping_lokasi_ranap", "kd_kamar"
	default:
		return "", errors.New("jenis lokasi tidak dikenal")
	}
	if nama == "" {
		return "", errors.New("kode " + kode + " tidak ditemukan")
	}
	if c.Cfg.OrgID == "" {
		return "", errors.New("SATUSEHAT_ORG_ID belum diisi")
	}
	phys := "ro"
	if jenis == "ranap" {
		phys = "bd"
	}
	id, err := c.Create(ctx, map[string]any{
		"resourceType": "Location",
		"identifier":   []any{map[string]any{"system": "http://sys-ids.kemkes.go.id/location/" + c.Cfg.OrgID, "value": kode}},
		"status":       "active", "name": nama, "description": nama, "mode": "instance",
		"physicalType":         coding(SysLocPhys, phys, map[string]string{"ro": "Room", "bd": "Bed"}[phys]),
		"managingOrganization": ref("Organization", c.Cfg.OrgID, ""),
	})
	if err != nil {
		return "", err
	}
	_, err = db.ExecContext(ctx, "INSERT INTO "+table+" ("+key+",id_organisasi_satusehat,id_lokasi_satusehat) VALUES (?,?,?) ON DUPLICATE KEY UPDATE id_organisasi_satusehat=VALUES(id_organisasi_satusehat), id_lokasi_satusehat=VALUES(id_lokasi_satusehat)", kode, c.Cfg.OrgID, id)
	return id, err
}

// Result ringkasan satu kali kirim.
type Result struct {
	Sent     []string
	Errors   []string
	Warnings []string // data tidak valid yang dilewati (tidak menghalangi Encounter finished)
}

func (r *Result) warn(f string, a ...any) { r.Warnings = append(r.Warnings, fmt.Sprintf(f, a...)) }

func (r *Result) ok(f string, a ...any)  { r.Sent = append(r.Sent, fmt.Sprintf(f, a...)) }
func (r *Result) bad(f string, a ...any) { r.Errors = append(r.Errors, fmt.Sprintf(f, a...)) }
func (r Result) Err() error {
	if len(r.Errors) == 0 {
		return nil
	}
	return errors.New(strings.Join(r.Errors, "; "))
}

type visit struct {
	NoRawat, NoRM, Nama, NIK, Tgl, Jam, KdPoli, NmPoli, KdDokter, NmDokter, KTPDokter, StatusLanjut, StatusBayar, Stts string
}

func loadVisit(ctx context.Context, db *sql.DB, no string) (*visit, error) {
	v := &visit{}
	err := db.QueryRowContext(ctx, `SELECT r.no_rawat, r.no_rkm_medis, p.nm_pasien, IFNULL(p.no_ktp,''), r.tgl_registrasi, r.jam_reg, r.kd_poli, pl.nm_poli,
		IFNULL((SELECT kd_dokter FROM dpjp_ranap x WHERE x.no_rawat=r.no_rawat LIMIT 1), r.kd_dokter) kd, '' , '', r.status_lanjut, r.status_bayar, r.stts
		FROM reg_periksa r JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis JOIN poliklinik pl ON pl.kd_poli=r.kd_poli WHERE r.no_rawat=?`, no).
		Scan(&v.NoRawat, &v.NoRM, &v.Nama, &v.NIK, &v.Tgl, &v.Jam, &v.KdPoli, &v.NmPoli, &v.KdDokter, &v.NmDokter, &v.KTPDokter, &v.StatusLanjut, &v.StatusBayar, &v.Stts)
	if err != nil {
		return nil, errors.New("no. rawat tidak ditemukan")
	}
	_ = db.QueryRowContext(ctx, "SELECT d.nm_dokter, IFNULL(pg.no_ktp,'') FROM dokter d LEFT JOIN pegawai pg ON pg.nik=d.kd_dokter WHERE d.kd_dokter=?", v.KdDokter).Scan(&v.NmDokter, &v.KTPDokter)
	return v, nil
}

// vital: jenis -> tabel Khanza, kolom pemeriksaan, LOINC, display, satuan UCUM, satuan tampilan.
type vital struct{ Jenis, Kolom, Loinc, Display, Code, Unit string }

var Vitals = []vital{
	{"nadi", "nadi", "8867-4", "Heart rate", "/min", "beats/minute"},
	{"respirasi", "respirasi", "9279-1", "Respiratory rate", "/min", "breaths/minute"},
	{"suhu", "suhu_tubuh", "8310-5", "Body temperature", "Cel", "C"},
	{"tensi", "tensi", "35094-2", "Blood pressure panel", "mm[Hg]", "mmHg"},
	{"tb", "tinggi", "8302-2", "Body height", "cm", "cm"},
	{"bb", "berat", "29463-7", "Body weight", "kg", "kg"},
	{"lp", "lingkar_perut", "8280-0", "Waist Circumference at umbilicus by Tape measure", "cm", "cm"},
}

func num(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil && f > 0
}

func classOf(v *visit) (string, string) {
	if v.StatusLanjut == "Ranap" {
		return "IMP", "inpatient encounter"
	}
	if strings.HasPrefix(strings.ToUpper(v.KdPoli), "IGD") {
		return "EMER", "emergency"
	}
	return "AMB", "ambulatory"
}

// selesai: waktu akhir kunjungan bila sudah selesai (ralan: dibayar; ranap: pulang).
func selesai(ctx context.Context, db *sql.DB, v *visit) string {
	if v.StatusLanjut == "Ranap" {
		if val(ctx, db, "SELECT COUNT(*) FROM kamar_inap WHERE no_rawat=? AND stts_pulang='-'", v.NoRawat) != "0" {
			return ""
		}
		t := val(ctx, db, "SELECT CONCAT(tgl_keluar,' ',jam_keluar) FROM kamar_inap WHERE no_rawat=? AND stts_pulang NOT IN ('-','Pindah Kamar') ORDER BY tgl_keluar DESC, jam_keluar DESC LIMIT 1", v.NoRawat)
		if t == "" {
			return ""
		}
		return fhirTime(t[:10], t[11:])
	}
	if v.StatusBayar != "Sudah Bayar" {
		return ""
	}
	t := val(ctx, db, "SELECT CONCAT(tanggal,' ',jam) FROM nota_jalan WHERE no_rawat=?", v.NoRawat)
	if t == "" {
		return ""
	}
	return fhirTime(t[:10], t[11:])
}

func (c *Client) location(ctx context.Context, db *sql.DB, v *visit) (string, string, error) {
	if v.StatusLanjut == "Ranap" {
		var id, nm string
		_ = db.QueryRowContext(ctx, `SELECT m.id_lokasi_satusehat, CONCAT(b.nm_bangsal,' ',k.kd_kamar) FROM kamar_inap ki JOIN satu_sehat_mapping_lokasi_ranap m ON m.kd_kamar=ki.kd_kamar
			JOIN kamar k ON k.kd_kamar=ki.kd_kamar JOIN bangsal b ON b.kd_bangsal=k.kd_bangsal WHERE ki.no_rawat=? ORDER BY ki.tgl_masuk, ki.jam_masuk LIMIT 1`, v.NoRawat).Scan(&id, &nm)
		if id != "" {
			return id, nm, nil
		}
	}
	id := val(ctx, db, "SELECT id_lokasi_satusehat FROM satu_sehat_mapping_lokasi_ralan WHERE kd_poli=?", v.KdPoli)
	if id == "" {
		return "", "", fmt.Errorf("lokasi %s belum di-mapping ke SATUSEHAT", v.NmPoli)
	}
	return id, v.NmPoli, nil
}

func (c *Client) encounterBody(ctx context.Context, db *sql.DB, v *visit, pasien, dokter, lokID, lokNama, status, end string) map[string]any {
	start := fhirTime(v.Tgl, v.Jam)
	cls, clsDisp := classOf(v)
	// arrived s/d pemeriksaan pertama, in-progress s/d selesai
	first := val(ctx, db, `SELECT MIN(CONCAT(tgl_perawatan,' ',jam_rawat)) FROM (SELECT tgl_perawatan, jam_rawat FROM pemeriksaan_ralan WHERE no_rawat=? UNION ALL SELECT tgl_perawatan, jam_rawat FROM pemeriksaan_ranap WHERE no_rawat=?) x`, v.NoRawat, v.NoRawat)
	mid := start
	if first != "" {
		mid = fhirTime(first[:10], first[11:])
	}
	body := map[string]any{
		"resourceType": "Encounter",
		"identifier":   []any{map[string]any{"system": "http://sys-ids.kemkes.go.id/encounter/" + c.Cfg.OrgID, "value": v.NoRawat}},
		"status":       status,
		"class":        map[string]any{"system": SysActCode, "code": cls, "display": clsDisp},
		"subject":      ref("Patient", pasien, v.Nama),
		"participant": []any{map[string]any{
			"type":       []any{coding(SysPartType, "ATND", "attender")},
			"individual": ref("Practitioner", dokter, v.NmDokter)}},
		"location":        []any{map[string]any{"location": ref("Location", lokID, lokNama)}},
		"serviceProvider": ref("Organization", c.Cfg.OrgID, ""),
	}
	if status == "arrived" {
		body["period"] = map[string]any{"start": start}
		body["statusHistory"] = []any{map[string]any{"status": "arrived", "period": map[string]any{"start": start}}}
		return body
	}
	body["period"] = map[string]any{"start": start, "end": end}
	body["statusHistory"] = []any{
		map[string]any{"status": "arrived", "period": map[string]any{"start": start, "end": mid}},
		map[string]any{"status": "in-progress", "period": map[string]any{"start": mid, "end": end}},
		map[string]any{"status": "finished", "period": map[string]any{"start": end, "end": end}},
	}
	return body
}

// Sync mengirim semua data kunjungan yang belum terkirim. Aman dipanggil berulang (idempoten).
// Galat terakhir disimpan di app_satusehat_error agar tampil di daftar kirim.
func (c *Client) Sync(ctx context.Context, db *sql.DB, noRawat string) Result {
	res := c.sync(ctx, db, noRawat)
	if err := res.Err(); err != nil {
		msg := err.Error()
		if len(msg) > 1000 {
			msg = msg[:1000]
		}
		db.ExecContext(ctx, "INSERT INTO app_satusehat_error VALUES (?,?,NOW()) ON DUPLICATE KEY UPDATE pesan=VALUES(pesan), waktu=NOW()", noRawat, msg)
	} else {
		db.ExecContext(ctx, "DELETE FROM app_satusehat_error WHERE no_rawat=?", noRawat)
	}
	return res
}

func (c *Client) sync(ctx context.Context, db *sql.DB, noRawat string) Result {
	var res Result
	v, err := loadVisit(ctx, db, noRawat)
	if err != nil {
		res.bad("%v", err)
		return res
	}
	if v.Stts == "Batal" {
		res.bad("kunjungan batal, tidak dikirim")
		return res
	}
	if c.Cfg.OrgID == "" {
		res.bad("SATUSEHAT_ORG_ID belum diisi")
		return res
	}
	pasien, err := c.IHS(ctx, db, "Patient", v.NIK)
	if err != nil {
		res.bad("Patient: %v", err)
		return res
	}
	dokter, err := c.IHS(ctx, db, "Practitioner", v.KTPDokter)
	if err != nil {
		res.bad("Practitioner %s: %v", v.NmDokter, err)
		return res
	}
	// ---- Encounter
	enc := val(ctx, db, "SELECT id_encounter FROM satu_sehat_encounter WHERE no_rawat=?", noRawat)
	if enc == "" {
		lokID, lokNama, err := c.location(ctx, db, v)
		if err != nil {
			res.bad("Encounter: %v", err)
			return res
		}
		enc, err = c.Create(ctx, c.encounterBody(ctx, db, v, pasien, dokter, lokID, lokNama, "arrived", ""))
		if err != nil {
			res.bad("Encounter: %v", err)
			return res
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO satu_sehat_encounter VALUES (?,?)", noRawat, enc); err != nil {
			res.bad("simpan id Encounter %s: %v", enc, err)
			return res
		}
		db.ExecContext(ctx, "INSERT INTO app_satusehat_encounter_status VALUES (?, 'arrived', NOW()) ON DUPLICATE KEY UPDATE status='arrived', waktu=NOW()", noRawat)
		res.ok("Encounter %s", enc)
	}
	encRef := ref("Encounter", enc, "Kunjungan "+v.Nama)
	pRef := ref("Patient", pasien, v.Nama)

	// ---- Condition (diagnosa ICD-10)
	rows, _ := db.QueryContext(ctx, `SELECT d.kd_penyakit, p.nm_penyakit, d.status FROM diagnosa_pasien d JOIN penyakit p ON p.kd_penyakit=d.kd_penyakit
		LEFT JOIN satu_sehat_condition s ON s.no_rawat=d.no_rawat AND s.kd_penyakit=d.kd_penyakit AND s.status=d.status
		WHERE d.no_rawat=? AND (s.id_condition IS NULL OR s.id_condition='') ORDER BY d.status, d.prioritas`, noRawat)
	type dx struct{ kd, nm, st string }
	var dxs []dx
	for rows != nil && rows.Next() {
		var d dx
		rows.Scan(&d.kd, &d.nm, &d.st)
		dxs = append(dxs, d)
	}
	if rows != nil {
		rows.Close()
	}
	for _, d := range dxs {
		id, err := c.Create(ctx, map[string]any{
			"resourceType":   "Condition",
			"clinicalStatus": coding(SysCondClin, "active", "Active"),
			"category":       []any{coding(SysCondCat, "encounter-diagnosis", "Encounter Diagnosis")},
			"code":           coding(SysICD10, d.kd, d.nm),
			"subject":        pRef, "encounter": encRef,
		})
		if err != nil {
			res.bad("Condition %s: %v", d.kd, err)
			continue
		}
		db.ExecContext(ctx, "INSERT INTO satu_sehat_condition VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE id_condition=VALUES(id_condition)", noRawat, d.kd, d.st, id)
		res.ok("Condition %s", d.kd)
	}

	// ---- Procedure (ICD-9-CM)
	rows, _ = db.QueryContext(ctx, `SELECT d.kode, i.deskripsi_panjang, d.status FROM prosedur_pasien d JOIN icd9 i ON i.kode=d.kode
		LEFT JOIN satu_sehat_procedure s ON s.no_rawat=d.no_rawat AND s.kode=d.kode AND s.status=d.status
		WHERE d.no_rawat=? AND (s.id_procedure IS NULL OR s.id_procedure='') ORDER BY d.status, d.prioritas`, noRawat)
	var prs []dx
	for rows != nil && rows.Next() {
		var d dx
		rows.Scan(&d.kd, &d.nm, &d.st)
		prs = append(prs, d)
	}
	if rows != nil {
		rows.Close()
	}
	for _, d := range prs {
		id, err := c.Create(ctx, map[string]any{
			"resourceType": "Procedure", "status": "completed",
			"category": coding("http://snomed.info/sct", "103693007", "Diagnostic procedure"),
			"code":     coding(SysICD9CM, d.kd, d.nm),
			"subject":  pRef, "encounter": encRef,
			"performer": []any{map[string]any{"actor": ref("Practitioner", dokter, v.NmDokter)}},
		})
		if err != nil {
			res.bad("Procedure %s: %v", d.kd, err)
			continue
		}
		db.ExecContext(ctx, "INSERT INTO satu_sehat_procedure VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE id_procedure=VALUES(id_procedure)", noRawat, d.kd, d.st, id)
		res.ok("Procedure %s", d.kd)
	}

	// ---- Observation tanda vital per baris pemeriksaan
	for _, src := range []struct{ table, st string }{{"pemeriksaan_ralan", "Ralan"}, {"pemeriksaan_ranap", "Ranap"}} {
		cols := []string{"tgl_perawatan", "jam_rawat"}
		for _, vt := range Vitals {
			cols = append(cols, vt.Kolom)
		}
		rows, err := db.QueryContext(ctx, "SELECT "+strings.Join(cols, ",")+" FROM "+src.table+" WHERE no_rawat=? ORDER BY tgl_perawatan, jam_rawat", noRawat)
		if err != nil {
			res.bad("baca %s: %v", src.table, err)
			continue
		}
		type rowT struct {
			tgl, jam string
			vals     []string
		}
		var list []rowT
		for rows.Next() {
			r := rowT{vals: make([]string, len(Vitals))}
			ptr := []any{&r.tgl, &r.jam}
			ns := make([]sql.NullString, len(Vitals))
			for i := range ns {
				ptr = append(ptr, &ns[i])
			}
			rows.Scan(ptr...)
			for i := range ns {
				r.vals[i] = ns[i].String
			}
			list = append(list, r)
		}
		rows.Close()
		for _, r := range list {
			for i, vt := range Vitals {
				table := "satu_sehat_observationttv" + vt.Jenis
				if val(ctx, db, "SELECT IFNULL(id_observation,'') FROM "+table+" WHERE no_rawat=? AND tgl_perawatan=? AND jam_rawat=? AND status=?", noRawat, r.tgl, r.jam, src.st) != "" {
					continue
				}
				obs := map[string]any{
					"resourceType": "Observation", "status": "final",
					"category": []any{coding(SysObsCat, "vital-signs", "Vital Signs")},
					"code":     coding(SysLOINC, vt.Loinc, vt.Display),
					"subject":  pRef, "encounter": encRef,
					"performer":         []any{ref("Practitioner", dokter, v.NmDokter)},
					"effectiveDateTime": fhirTime(r.tgl, r.jam),
					"issued":            fhirTime(r.tgl, r.jam),
				}
				raw := strings.TrimSpace(r.vals[i])
				if vt.Jenis == "tensi" {
					p := strings.SplitN(raw, "/", 2)
					if len(p) != 2 {
						if raw != "" && raw != "-" {
							res.warn("tensi %s %s: format %q harus sistolik/diastolik, dilewati", r.tgl, r.jam, raw)
						}
						continue
					}
					sys, ok1 := num(p[0])
					dia, ok2 := num(p[1])
					if !ok1 || !ok2 {
						res.warn("tensi %s %s: nilai %q tidak valid, dilewati", r.tgl, r.jam, raw)
						continue
					}
					obs["component"] = []any{
						map[string]any{"code": coding(SysLOINC, "8480-6", "Systolic blood pressure"), "valueQuantity": map[string]any{"value": sys, "unit": "mmHg", "system": SysUCUM, "code": "mm[Hg]"}},
						map[string]any{"code": coding(SysLOINC, "8462-4", "Diastolic blood pressure"), "valueQuantity": map[string]any{"value": dia, "unit": "mmHg", "system": SysUCUM, "code": "mm[Hg]"}},
					}
				} else {
					f, ok := num(raw)
					if !ok {
						if raw != "" && raw != "-" {
							res.warn("%s %s %s: nilai %q bukan angka, dilewati", vt.Jenis, r.tgl, r.jam, raw)
						}
						continue
					}
					obs["valueQuantity"] = map[string]any{"value": f, "unit": vt.Unit, "system": SysUCUM, "code": vt.Code}
				}
				id, err := c.Create(ctx, obs)
				if err != nil {
					res.bad("Observation %s %s %s: %v", vt.Jenis, r.tgl, r.jam, err)
					continue
				}
				db.ExecContext(ctx, "INSERT INTO "+table+" VALUES (?,?,?,?,?) ON DUPLICATE KEY UPDATE id_observation=VALUES(id_observation)", noRawat, r.tgl, r.jam, src.st, id)
				res.ok("Observation %s %s", vt.Jenis, r.jam)
			}
		}
	}

	// ---- Encounter finished
	if val(ctx, db, "SELECT status FROM app_satusehat_encounter_status WHERE no_rawat=?", noRawat) == "finished" {
		return res
	}
	end := selesai(ctx, db, v)
	if end == "" {
		return res // kunjungan belum selesai
	}
	if len(res.Errors) > 0 {
		res.bad("Encounter belum diselesaikan karena ada data yang gagal terkirim")
		return res
	}
	conds, _ := db.QueryContext(ctx, `SELECT s.id_condition, p.nm_penyakit FROM satu_sehat_condition s JOIN diagnosa_pasien d ON d.no_rawat=s.no_rawat AND d.kd_penyakit=s.kd_penyakit AND d.status=s.status
		JOIN penyakit p ON p.kd_penyakit=s.kd_penyakit WHERE s.no_rawat=? AND s.id_condition<>'' ORDER BY d.status, d.prioritas`, noRawat)
	var diag []any
	for conds != nil && conds.Next() {
		var id, nm string
		conds.Scan(&id, &nm)
		diag = append(diag, map[string]any{"condition": ref("Condition", id, nm), "use": coding(SysDiagRole, "DD", "Discharge diagnosis"), "rank": len(diag) + 1})
	}
	if conds != nil {
		conds.Close()
	}
	if len(diag) == 0 {
		res.bad("Encounter belum bisa diselesaikan: kunjungan belum memiliki diagnosa ICD-10")
		return res
	}
	lokID, lokNama, err := c.location(ctx, db, v)
	if err != nil {
		res.bad("Encounter finished: %v", err)
		return res
	}
	body := c.encounterBody(ctx, db, v, pasien, dokter, lokID, lokNama, "finished", end)
	body["id"] = enc
	body["diagnosis"] = diag
	if err := c.Do(ctx, "PUT", "Encounter/"+enc, body, nil); err != nil {
		res.bad("Encounter finished: %v", err)
		return res
	}
	db.ExecContext(ctx, "INSERT INTO app_satusehat_encounter_status VALUES (?, 'finished', NOW()) ON DUPLICATE KEY UPDATE status='finished', waktu=NOW()", noRawat)
	res.ok("Encounter %s finished", enc)
	return res
}

// Kandidat mengembalikan no_rawat dalam rentang tanggal yang belum tuntas dikirim.
func Kandidat(ctx context.Context, db *sql.DB, dari, sampai string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT r.no_rawat FROM reg_periksa r LEFT JOIN app_satusehat_encounter_status s ON s.no_rawat=r.no_rawat
		WHERE r.tgl_registrasi BETWEEN ? AND ? AND r.stts<>'Batal' AND IFNULL(s.status,'')<>'finished'
		AND (EXISTS (SELECT 1 FROM pemeriksaan_ralan e WHERE e.no_rawat=r.no_rawat) OR EXISTS (SELECT 1 FROM pemeriksaan_ranap e WHERE e.no_rawat=r.no_rawat) OR EXISTS (SELECT 1 FROM diagnosa_pasien d WHERE d.no_rawat=r.no_rawat))
		ORDER BY r.tgl_registrasi, r.jam_reg`, dari, sampai)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out, nil
}

// AutoSync menjalankan pengiriman berkala untuk kunjungan 3 hari terakhir.
func (c *Client) AutoSync(ctx context.Context, db *sql.DB, every time.Duration, logf func(string, ...any)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := time.Now()
		list, err := Kandidat(ctx, db, now.AddDate(0, 0, -3).Format("2006-01-02"), now.Format("2006-01-02"))
		if err != nil {
			logf("satusehat auto: %v", err)
			continue
		}
		ok, fail := 0, 0
		for _, no := range list {
			r := c.Sync(ctx, db, no)
			ok += len(r.Sent)
			if len(r.Errors) > 0 {
				fail++
			}
		}
		if len(list) > 0 {
			logf("satusehat auto: %d kunjungan diproses, %d resource terkirim, %d kunjungan bermasalah", len(list), ok, fail)
		}
	}
}
