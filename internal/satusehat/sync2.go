package satusehat

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// SATUSEHAT tahap 2: obat (Medication, MedicationRequest, MedicationDispense),
// laboratorium (ServiceRequest, Specimen, Observation, DiagnosticReport) dan
// radiologi (ServiceRequest, DiagnosticReport). Mengikuti SatuSehatKirim*.java Khanza.
// Item yang belum di-mapping (KFA/LOINC) dilewati dengan peringatan; tidak menghalangi Encounter finished.

const (
	SysKFA        = "http://sys-ids.kemkes.go.id/kfa"
	SysMedForm    = "http://terminology.kemkes.go.id/CodeSystem/medication-form"
	SysMedType    = "http://terminology.kemkes.go.id/CodeSystem/medication-type"
	SysRoute      = "http://www.whocc.no/atc"
	SysSNOMED     = "http://snomed.info/sct"
	SysV2_0074    = "http://terminology.hl7.org/CodeSystem/v2-0074"
	SysMRCategory = "http://terminology.hl7.org/CodeSystem/medicationrequest-category"
	SysMDCategory = "http://terminology.hl7.org/fhir/CodeSystem/medicationdispense-category"
)

type stage2 struct {
	c                  *Client
	db                 *sql.DB
	v                  *visit
	pasien, dokter     string
	pRef, encRef       map[string]any
	res                *Result
	cacheIHS           map[string]string
	medStatus, medDisp string
}

func q2(ctx context.Context, db *sql.DB, query string, n int, args ...any) [][]string {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		ns := make([]sql.NullString, n)
		p := make([]any, n)
		for i := range ns {
			p[i] = &ns[i]
		}
		rows.Scan(p...)
		r := make([]string, n)
		for i := range ns {
			r[i] = ns[i].String
		}
		out = append(out, r)
	}
	return out
}

// practitioner: IHS dokter berdasarkan kd_dokter (NIK dari pegawai.no_ktp); fallback ke DPJP kunjungan.
func (s *stage2) practitioner(ctx context.Context, kd string) (string, string) {
	if kd == "" {
		return s.dokter, s.v.NmDokter
	}
	var nik, nama string
	s.db.QueryRowContext(ctx, "SELECT IFNULL(pg.no_ktp,''), d.nm_dokter FROM dokter d LEFT JOIN pegawai pg ON pg.nik=d.kd_dokter WHERE d.kd_dokter=?", kd).Scan(&nik, &nama)
	if id, ok := s.cacheIHS[nik]; ok {
		return id, nama
	}
	id, err := s.c.IHS(ctx, s.db, "Practitioner", nik)
	if err != nil {
		s.res.warn("dokter %s belum punya IHS (%v), dipakai DPJP", nama, err)
		return s.dokter, s.v.NmDokter
	}
	s.cacheIHS[nik] = id
	return id, nama
}

// aturanRe membaca aturan pakai Khanza seperti "3 x 1", "2x1 tablet", "1 x 1/2".
var aturanRe = regexp.MustCompile(`(\d+)\s*[xX×]\s*(\d+(?:[.,]\d+)?(?:/\d+)?)`)

func parseAturan(a string) (freq int, dose float64, ok bool) {
	m := aturanRe.FindStringSubmatch(a)
	if m == nil {
		return 0, 0, false
	}
	freq, _ = strconv.Atoi(m[1])
	d := strings.ReplaceAll(m[2], ",", ".")
	if p := strings.SplitN(d, "/", 2); len(p) == 2 {
		a, _ := strconv.ParseFloat(p[0], 64)
		b, _ := strconv.ParseFloat(p[1], 64)
		if b == 0 {
			return 0, 0, false
		}
		dose = a / b
	} else {
		dose, _ = strconv.ParseFloat(d, 64)
	}
	return freq, dose, freq > 0 && dose > 0
}

type obatMap struct {
	kode, nama, kfa, kfaDisp, form, formDisp, satuan, satuanSys, route, routeSys, routeDisp string
}

func (s *stage2) medication(ctx context.Context, o obatMap) (string, error) {
	if id := val(ctx, s.db, "SELECT IFNULL(id_medication,'') FROM satu_sehat_medication WHERE kode_brng=?", o.kode); id != "" {
		return id, nil
	}
	body := map[string]any{
		"resourceType": "Medication",
		"meta":         map[string]any{"profile": []any{"https://fhir.kemkes.go.id/r4/StructureDefinition/Medication"}},
		"identifier":   []any{map[string]any{"system": "http://sys-ids.kemkes.go.id/medication/" + s.c.Cfg.OrgID, "use": "official", "value": o.kode}},
		"code":         coding(SysKFA, o.kfa, o.kfaDisp),
		"status":       "active",
		"manufacturer": ref("Organization", s.c.Cfg.OrgID, ""),
		"extension": []any{map[string]any{"url": "https://fhir.kemkes.go.id/r4/StructureDefinition/MedicationType",
			"valueCodeableConcept": coding(SysMedType, "NC", "Non-compound")}},
	}
	if o.form != "" {
		body["form"] = coding(SysMedForm, o.form, o.formDisp)
	}
	id, err := s.c.Create(ctx, body)
	if err != nil {
		return "", err
	}
	s.db.ExecContext(ctx, "INSERT INTO satu_sehat_medication VALUES (?,?) ON DUPLICATE KEY UPDATE id_medication=VALUES(id_medication)", o.kode, id)
	s.res.ok("Medication %s", o.kode)
	return id, nil
}

func (s *stage2) dosage(o obatMap, aturan string, seqText bool) []any {
	d := map[string]any{"sequence": 1}
	if seqText {
		d["text"] = aturan
	} else {
		d["patientInstruction"] = aturan
	}
	if f, dose, ok := parseAturan(aturan); ok {
		d["timing"] = map[string]any{"repeat": map[string]any{"frequency": f, "period": 1, "periodUnit": "d"}}
		if o.satuan != "" {
			d["doseAndRate"] = []any{map[string]any{"doseQuantity": map[string]any{"value": dose, "unit": o.satuan, "system": o.satuanSys, "code": o.satuan}}}
		}
	}
	if o.route != "" {
		d["route"] = coding(o.routeSys, o.route, o.routeDisp)
	}
	return []any{d}
}

func (s *stage2) obat(ctx context.Context) {
	cat, catDisp := "outpatient", "Outpatient"
	if s.v.StatusLanjut == "Ranap" {
		cat, catDisp = "inpatient", "Inpatient"
	}
	// resep: no_resep, kd_dokter, tgl/jam peresepan, tgl/jam validasi, kode, jml, aturan, nama, mapping...
	rows := q2(ctx, s.db, `SELECT r.no_resep, r.kd_dokter, r.tgl_peresepan, r.jam_peresepan, IFNULL(r.tgl_perawatan,''), IFNULL(r.jam,''),
		d.kode_brng, d.jml, IFNULL(d.aturan_pakai,''), b.nama_brng,
		IFNULL(m.obat_code,''), IFNULL(m.obat_display,''), IFNULL(m.form_code,''), IFNULL(m.form_display,''), IFNULL(m.denominator_code,''), IFNULL(m.denominator_system,''),
		IFNULL(m.route_code,''), IFNULL(m.route_system,''), IFNULL(m.route_display,''),
		IFNULL(mr.id_medicationrequest,''), IFNULL(r.tgl_penyerahan,''), IFNULL(r.jam_penyerahan,'')
		FROM resep_obat r JOIN resep_dokter d ON d.no_resep=r.no_resep JOIN databarang b ON b.kode_brng=d.kode_brng
		LEFT JOIN satu_sehat_mapping_obat m ON m.kode_brng=d.kode_brng LEFT JOIN satu_sehat_medicationrequest mr ON mr.no_resep=r.no_resep AND mr.kode_brng=d.kode_brng
		WHERE r.no_rawat=? ORDER BY r.no_resep, d.kode_brng`, 22, s.v.NoRawat)
	depo := ""
	for _, x := range rows {
		o := obatMap{kode: x[6], nama: x[9], kfa: x[10], kfaDisp: x[11], form: x[12], formDisp: x[13], satuan: x[14], satuanSys: x[15], route: x[16], routeSys: x[17], routeDisp: x[18]}
		if o.kfa == "" {
			s.res.warn("obat %s belum di-mapping ke KFA, dilewati", o.nama)
			continue
		}
		if o.routeSys == "" {
			o.routeSys = SysRoute
		}
		medID, err := s.medication(ctx, o)
		if err != nil {
			s.res.bad("Medication %s: %v", o.kode, err)
			continue
		}
		noResep, aturan := x[0], x[8]
		jml, _ := strconv.ParseFloat(x[7], 64)
		mrID := x[19]
		if mrID == "" {
			reqID, reqNm := s.practitioner(ctx, x[1])
			body := map[string]any{
				"resourceType": "MedicationRequest",
				"identifier": []any{
					map[string]any{"system": "http://sys-ids.kemkes.go.id/prescription/" + s.c.Cfg.OrgID, "use": "official", "value": noResep},
					map[string]any{"system": "http://sys-ids.kemkes.go.id/prescription-item/" + s.c.Cfg.OrgID, "use": "official", "value": noResep + "-" + o.kode},
				},
				"status": "completed", "intent": "order",
				"category":            []any{coding(SysMRCategory, cat, catDisp)},
				"medicationReference": ref("Medication", medID, o.kfaDisp),
				"subject":             s.pRef, "encounter": s.encRef,
				"authoredOn":        fhirTime(x[2], x[3]),
				"requester":         ref("Practitioner", reqID, reqNm),
				"dosageInstruction": s.dosage(o, aturan, false),
				"dispenseRequest": map[string]any{"quantity": map[string]any{"value": jml, "unit": o.satuan, "system": o.satuanSys, "code": o.satuan},
					"performer": ref("Organization", s.c.Cfg.OrgID, "")},
			}
			if o.satuan == "" {
				body["dispenseRequest"] = map[string]any{"quantity": map[string]any{"value": jml}, "performer": ref("Organization", s.c.Cfg.OrgID, "")}
			}
			mrID, err = s.c.Create(ctx, body)
			if err != nil {
				s.res.bad("MedicationRequest %s %s: %v", noResep, o.kode, err)
				continue
			}
			s.db.ExecContext(ctx, "INSERT INTO satu_sehat_medicationrequest VALUES (?,?,?) ON DUPLICATE KEY UPDATE id_medicationrequest=VALUES(id_medicationrequest)", noResep, o.kode, mrID)
			s.res.ok("MedicationRequest %s %s", noResep, o.kode)
		}
		// dispense: setelah resep divalidasi farmasi
		tgl, jam := x[4], x[5]
		if tgl == "" {
			continue
		}
		disp := q2(ctx, s.db, `SELECT p.no_batch, p.no_faktur, p.jml, IFNULL(s.id_medicationdispanse,'') FROM detail_pemberian_obat p
			LEFT JOIN satu_sehat_medicationdispense s ON s.no_rawat=p.no_rawat AND s.tgl_perawatan=p.tgl_perawatan AND s.jam=p.jam AND s.kode_brng=p.kode_brng AND s.no_batch=p.no_batch AND s.no_faktur=p.no_faktur
			WHERE p.no_rawat=? AND p.tgl_perawatan=? AND p.jam=? AND p.kode_brng=?`, 4, s.v.NoRawat, tgl, jam, o.kode)
		for _, dp := range disp {
			if dp[3] != "" {
				continue
			}
			if depo == "" {
				depo = val(ctx, s.db, "SELECT IFNULL(id_lokasi_satusehat,'') FROM satu_sehat_mapping_lokasi_depo_farmasi WHERE kd_bangsal='AP'")
				if depo == "" {
					s.res.warn("lokasi depo farmasi (AP) belum di-mapping, MedicationDispense dilewati")
					depo = "-"
				}
			}
			if depo == "-" {
				continue
			}
			q, _ := strconv.ParseFloat(dp[2], 64)
			serah := fhirTime(tgl, jam)
			if x[20] != "" {
				serah = fhirTime(x[20], x[21])
			}
			body := map[string]any{
				"resourceType": "MedicationDispense",
				"identifier": []any{
					map[string]any{"system": "http://sys-ids.kemkes.go.id/prescription/" + s.c.Cfg.OrgID, "use": "official", "value": noResep},
					map[string]any{"system": "http://sys-ids.kemkes.go.id/prescription-item/" + s.c.Cfg.OrgID, "use": "official", "value": noResep + "-" + o.kode},
				},
				"status":              "completed",
				"category":            coding(SysMDCategory, cat, catDisp),
				"medicationReference": ref("Medication", medID, o.kfaDisp),
				"subject":             s.pRef, "context": s.encRef,
				"performer":               []any{map[string]any{"actor": ref("Practitioner", s.dokter, s.v.NmDokter)}},
				"location":                ref("Location", depo, "Depo Farmasi"),
				"authorizingPrescription": []any{ref("MedicationRequest", mrID, "")},
				"quantity":                map[string]any{"system": o.satuanSys, "code": o.satuan, "value": q},
				"whenPrepared":            fhirTime(tgl, jam),
				"whenHandedOver":          serah,
				"dosageInstruction":       s.dosage(o, aturan, true),
			}
			id, err := s.c.Create(ctx, body)
			if err != nil {
				s.res.bad("MedicationDispense %s: %v", o.kode, err)
				continue
			}
			s.db.ExecContext(ctx, "INSERT INTO satu_sehat_medicationdispense VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id_medicationdispanse=VALUES(id_medicationdispanse)", s.v.NoRawat, tgl, jam, o.kode, dp[0], dp[1], id)
			s.res.ok("MedicationDispense %s", o.kode)
		}
	}
}

func idOf(ctx context.Context, db *sql.DB, table, col string, keys ...any) string {
	where := "noorder=? AND kd_jenis_prw=?"
	if len(keys) == 3 {
		where += " AND id_template=?"
	}
	return val(ctx, db, "SELECT IFNULL("+col+",'') FROM "+table+" WHERE "+where, keys...)
}

func saveID(ctx context.Context, db *sql.DB, table, col, id string, keys ...any) {
	ph := strings.TrimSuffix(strings.Repeat("?,", len(keys)+1), ",")
	db.ExecContext(ctx, "INSERT INTO "+table+" VALUES ("+ph+") ON DUPLICATE KEY UPDATE "+col+"=VALUES("+col+")", append(keys, id)...)
}

var ucumFix = strings.NewReplacer("10^3", "10*3", "10^6", "10*6", "^", "*")

func (s *stage2) lab(ctx context.Context) {
	rows := q2(ctx, s.db, `SELECT pl.noorder, pl.dokter_perujuk, pl.tgl_permintaan, pl.jam_permintaan, IFNULL(pl.tgl_sampel,''), IFNULL(pl.jam_sampel,''), IFNULL(pl.tgl_hasil,''), IFNULL(pl.jam_hasil,''),
		pp.kd_jenis_prw, j.nm_perawatan, t.id_template, t.Pemeriksaan, IFNULL(t.satuan,''),
		IFNULL(m.code,''), IFNULL(m.display,''), IFNULL(m.sampel_code,''), IFNULL(m.sampel_system,''), IFNULL(m.sampel_display,''), IFNULL(pl.diagnosa_klinis,'')
		FROM permintaan_lab pl JOIN permintaan_pemeriksaan_lab pp ON pp.noorder=pl.noorder JOIN jns_perawatan_lab j ON j.kd_jenis_prw=pp.kd_jenis_prw
		JOIN template_laboratorium t ON t.kd_jenis_prw=pp.kd_jenis_prw LEFT JOIN satu_sehat_mapping_lab m ON m.id_template=t.id_template
		WHERE pl.no_rawat=? ORDER BY pl.noorder, pp.kd_jenis_prw, t.urut`, 19, s.v.NoRawat)
	for _, x := range rows {
		no, kd, idt, nama := x[0], x[8], x[10], x[11]
		loinc, disp := x[13], x[14]
		if loinc == "" {
			s.res.warn("pemeriksaan lab %s belum di-mapping ke LOINC, dilewati", nama)
			continue
		}
		code := coding(SysLOINC, loinc, disp)
		sr := idOf(ctx, s.db, "satu_sehat_servicerequest_lab", "id_servicerequest", no, kd, idt)
		if sr == "" {
			reqID, reqNm := s.practitioner(ctx, x[1])
			body := map[string]any{
				"resourceType": "ServiceRequest",
				"identifier":   []any{map[string]any{"system": "http://sys-ids.kemkes.go.id/servicerequest/" + s.c.Cfg.OrgID, "value": no}},
				"status":       "active", "intent": "original-order", "priority": "routine",
				"category": []any{coding(SysSNOMED, "108252007", "Laboratory procedure")},
				"code":     code, "subject": s.pRef, "encounter": s.encRef,
				"occurrenceDateTime": fhirTime(x[2], x[3]), "authoredOn": fhirTime(x[2], x[3]),
				"requester": ref("Practitioner", reqID, reqNm),
				"performer": []any{ref("Organization", s.c.Cfg.OrgID, "Laboratorium")},
			}
			if x[18] != "" {
				body["reasonCode"] = []any{map[string]any{"text": x[18]}}
			}
			id, err := s.c.Create(ctx, body)
			if err != nil {
				s.res.bad("ServiceRequest lab %s %s: %v", no, nama, err)
				continue
			}
			sr = id
			saveID(ctx, s.db, "satu_sehat_servicerequest_lab", "id_servicerequest", sr, no, kd, idt)
			s.res.ok("ServiceRequest lab %s", nama)
		}
		if x[4] == "" { // sampel belum diambil
			continue
		}
		sp := idOf(ctx, s.db, "satu_sehat_specimen_lab", "id_specimen", no, kd, idt)
		if sp == "" {
			if x[15] == "" {
				s.res.warn("jenis spesimen %s belum di-mapping, dilewati", nama)
				continue
			}
			id, err := s.c.Create(ctx, map[string]any{
				"resourceType": "Specimen",
				"identifier":   []any{map[string]any{"system": "http://sys-ids.kemkes.go.id/specimen/" + s.c.Cfg.OrgID, "value": no + "." + idt, "assigner": ref("Organization", s.c.Cfg.OrgID, "")}},
				"status":       "available",
				"type":         coding(x[16], x[15], x[17]),
				"collection":   map[string]any{"collectedDateTime": fhirTime(x[4], x[5])},
				"subject":      s.pRef,
				"request":      []any{ref("ServiceRequest", sr, "")},
				"receivedTime": fhirTime(x[4], x[5]),
			})
			if err != nil {
				s.res.bad("Specimen %s: %v", nama, err)
				continue
			}
			sp = id
			saveID(ctx, s.db, "satu_sehat_specimen_lab", "id_specimen", sp, no, kd, idt)
			s.res.ok("Specimen %s", nama)
		}
		if x[6] == "" { // hasil belum ada
			continue
		}
		ob := idOf(ctx, s.db, "satu_sehat_observation_lab", "id_observation", no, kd, idt)
		if ob == "" {
			var nilai, rujukan, ket string
			s.db.QueryRowContext(ctx, "SELECT nilai, IFNULL(nilai_rujukan,''), IFNULL(keterangan,'') FROM detail_periksa_lab WHERE no_rawat=? AND kd_jenis_prw=? AND tgl_periksa=? AND jam=? AND id_template=?",
				s.v.NoRawat, kd, x[6], x[7], idt).Scan(&nilai, &rujukan, &ket)
			if strings.TrimSpace(nilai) == "" {
				s.res.warn("hasil %s kosong, dilewati", nama)
				continue
			}
			body := map[string]any{
				"resourceType": "Observation",
				"identifier":   []any{map[string]any{"system": "http://sys-ids.kemkes.go.id/observation/" + s.c.Cfg.OrgID, "value": no + "." + idt}},
				"status":       "final",
				"category":     []any{coding(SysObsCat, "laboratory", "Laboratory")},
				"code":         code, "subject": s.pRef, "encounter": s.encRef,
				"performer":         []any{ref("Practitioner", s.dokter, s.v.NmDokter), ref("Organization", s.c.Cfg.OrgID, "")},
				"specimen":          ref("Specimen", sp, ""),
				"basedOn":           []any{ref("ServiceRequest", sr, "")},
				"effectiveDateTime": fhirTime(x[4], x[5]),
				"issued":            fhirTime(x[6], x[7]),
			}
			if f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(nilai), ",", "."), 64); err == nil && x[12] != "" {
				u := ucumFix.Replace(x[12])
				body["valueQuantity"] = map[string]any{"value": f, "unit": x[12], "system": SysUCUM, "code": u}
			} else {
				body["valueString"] = nilai
			}
			if rujukan != "" {
				body["referenceRange"] = []any{map[string]any{"text": rujukan}}
			}
			if i := map[string][2]string{"H": {"H", "High"}, "L": {"L", "Low"}, "HH": {"HH", "Critical high"}, "LL": {"LL", "Critical low"}}[strings.ToUpper(ket)]; i[0] != "" {
				body["interpretation"] = []any{coding("http://terminology.hl7.org/CodeSystem/v3-ObservationInterpretation", i[0], i[1])}
			}
			id, err := s.c.Create(ctx, body)
			if err != nil {
				s.res.bad("Observation lab %s: %v", nama, err)
				continue
			}
			ob = id
			saveID(ctx, s.db, "satu_sehat_observation_lab", "id_observation", ob, no, kd, idt)
			s.res.ok("Observation lab %s", nama)
		}
		if idOf(ctx, s.db, "satu_sehat_diagnosticreport_lab", "id_diagnosticreport", no, kd, idt) == "" {
			id, err := s.c.Create(ctx, map[string]any{
				"resourceType": "DiagnosticReport",
				"identifier":   []any{map[string]any{"system": "http://sys-ids.kemkes.go.id/diagnostic/" + s.c.Cfg.OrgID + "/lab", "use": "official", "value": no + "." + idt}},
				"status":       "final",
				"category":     []any{coding(SysV2_0074, "LAB", "Laboratory")},
				"code":         code, "subject": s.pRef, "encounter": s.encRef,
				"effectiveDateTime": fhirTime(x[4], x[5]), "issued": fhirTime(x[6], x[7]),
				"performer": []any{ref("Practitioner", s.dokter, s.v.NmDokter), ref("Organization", s.c.Cfg.OrgID, "")},
				"result":    []any{ref("Observation", ob, "")},
				"specimen":  []any{ref("Specimen", sp, "")},
				"basedOn":   []any{ref("ServiceRequest", sr, "")},
			})
			if err != nil {
				s.res.bad("DiagnosticReport lab %s: %v", nama, err)
				continue
			}
			saveID(ctx, s.db, "satu_sehat_diagnosticreport_lab", "id_diagnosticreport", id, no, kd, idt)
			s.res.ok("DiagnosticReport lab %s", nama)
		}
	}
}

func (s *stage2) radiologi(ctx context.Context) {
	rows := q2(ctx, s.db, `SELECT pr.noorder, pr.dokter_perujuk, pr.tgl_permintaan, pr.jam_permintaan, IFNULL(pr.tgl_hasil,''), IFNULL(pr.jam_hasil,''),
		pp.kd_jenis_prw, j.nm_perawatan, IFNULL(m.code,''), IFNULL(m.display,''), IFNULL(pr.diagnosa_klinis,''),
		IFNULL((SELECT h.hasil FROM app_radiologi_order o JOIN hasil_radiologi h ON h.no_rawat=pr.no_rawat AND h.tgl_periksa=o.tgl_periksa AND h.jam=o.jam WHERE o.noorder=pr.noorder),''),
		IFNULL(pr.tgl_sampel,''), IFNULL(pr.jam_sampel,'')
		FROM permintaan_radiologi pr JOIN permintaan_pemeriksaan_radiologi pp ON pp.noorder=pr.noorder JOIN jns_perawatan_radiologi j ON j.kd_jenis_prw=pp.kd_jenis_prw
		LEFT JOIN satu_sehat_mapping_radiologi m ON m.kd_jenis_prw=pp.kd_jenis_prw WHERE pr.no_rawat=? ORDER BY pr.noorder`, 14, s.v.NoRawat)
	for _, x := range rows {
		no, kd, nama := x[0], x[6], x[7]
		if x[8] == "" {
			s.res.warn("pemeriksaan radiologi %s belum di-mapping ke LOINC, dilewati", nama)
			continue
		}
		code := coding(SysLOINC, x[8], x[9])
		sr := idOf(ctx, s.db, "satu_sehat_servicerequest_radiologi", "id_servicerequest", no, kd)
		if sr == "" {
			reqID, reqNm := s.practitioner(ctx, x[1])
			body := map[string]any{
				"resourceType": "ServiceRequest",
				"identifier": []any{map[string]any{"system": "http://sys-ids.kemkes.go.id/servicerequest/" + s.c.Cfg.OrgID, "value": no},
					map[string]any{"use": "usual", "type": coding("http://terminology.hl7.org/CodeSystem/v2-0203", "ACSN", ""), "system": "http://sys-ids.kemkes.go.id/acsn/" + s.c.Cfg.OrgID, "value": no}},
				"status": "active", "intent": "original-order", "priority": "routine",
				"category": []any{coding(SysSNOMED, "363679005", "Imaging")},
				"code":     code, "subject": s.pRef, "encounter": s.encRef,
				"occurrenceDateTime": fhirTime(x[2], x[3]), "authoredOn": fhirTime(x[2], x[3]),
				"requester": ref("Practitioner", reqID, reqNm),
				"performer": []any{ref("Organization", s.c.Cfg.OrgID, "Radiologi")},
			}
			if x[10] != "" {
				body["reasonCode"] = []any{map[string]any{"text": x[10]}}
			}
			id, err := s.c.Create(ctx, body)
			if err != nil {
				s.res.bad("ServiceRequest radiologi %s: %v", nama, err)
				continue
			}
			sr = id
			saveID(ctx, s.db, "satu_sehat_servicerequest_radiologi", "id_servicerequest", sr, no, kd)
			s.res.ok("ServiceRequest radiologi %s", nama)
		}
		if x[4] == "" || strings.TrimSpace(x[11]) == "" {
			continue
		}
		if idOf(ctx, s.db, "satu_sehat_diagnosticreport_radiologi", "id_diagnosticreport", no, kd) != "" {
			continue
		}
		eff := fhirTime(x[4], x[5])
		if x[12] != "" {
			eff = fhirTime(x[12], x[13])
		}
		id, err := s.c.Create(ctx, map[string]any{
			"resourceType": "DiagnosticReport",
			"identifier":   []any{map[string]any{"system": "http://sys-ids.kemkes.go.id/diagnostic/" + s.c.Cfg.OrgID + "/rad", "use": "official", "value": no}},
			"status":       "final",
			"category":     []any{coding(SysV2_0074, "RAD", "Radiology")},
			"code":         code, "subject": s.pRef, "encounter": s.encRef,
			"effectiveDateTime": eff, "issued": fhirTime(x[4], x[5]),
			"performer":  []any{ref("Practitioner", s.dokter, s.v.NmDokter), ref("Organization", s.c.Cfg.OrgID, "")},
			"basedOn":    []any{ref("ServiceRequest", sr, "")},
			"conclusion": x[11],
		})
		if err != nil {
			s.res.bad("DiagnosticReport radiologi %s: %v", nama, err)
			continue
		}
		saveID(ctx, s.db, "satu_sehat_diagnosticreport_radiologi", "id_diagnosticreport", id, no, kd)
		s.res.ok("DiagnosticReport radiologi %s", nama)
	}
}

// syncStage2 dipanggil dari sync setelah Encounter tersedia.
func (c *Client) syncStage2(ctx context.Context, db *sql.DB, v *visit, pasien, dokter string, pRef, encRef map[string]any, res *Result) {
	s := &stage2{c: c, db: db, v: v, pasien: pasien, dokter: dokter, pRef: pRef, encRef: encRef, res: res, cacheIHS: map[string]string{}}
	s.obat(ctx)
	s.lab(ctx)
	s.radiologi(ctx)
}

var _ = fmt.Sprint
