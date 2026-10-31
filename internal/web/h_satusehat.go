package web

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yogaprasetia/simrs-go/internal/core"
	"github.com/yogaprasetia/simrs-go/internal/satusehat"
)

var rSS = []string{"bpjs", "manajemen"}

var loincRe = regexp.MustCompile(`^\d{1,7}-\d$`)

func (s *Server) routesSatuSehat() {
	m, p := s.mux, s.Prefix
	m.HandleFunc("GET "+p+"/satusehat", s.guard(rSS, s.ssPage))
	m.HandleFunc("POST "+p+"/satusehat/{aksi}", s.guard([]string{"bpjs"}, s.ssAksi))
}

func (s *Server) ssPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	tab := q.Get("tab")
	if tab == "" {
		tab = "kirim"
	}
	dari, sampai := q.Get("dari"), q.Get("sampai")
	if dari == "" {
		dari = core.Now().AddDate(0, 0, -2).Format("2006-01-02")
	}
	if sampai == "" {
		sampai = core.Today()
	}
	d := map[string]any{"Title": "SATUSEHAT", "Tab": tab, "Dari": dari, "Sampai": sampai, "Cfg": s.SS.Cfg}
	switch tab {
	case "kirim":
		st := q.Get("st")
		d["St"] = st
		where := ""
		switch st {
		case "belum":
			where = " AND IFNULL(es.status,'')<>'finished'"
		case "selesai":
			where = " AND es.status='finished'"
		}
		list := s.rows(ctx, `SELECT r.no_rawat, r.tgl_registrasi, r.jam_reg, r.status_lanjut, r.status_bayar, p.nm_pasien, p.no_rkm_medis, p.no_ktp, pl.nm_poli, dk.nm_dokter,
			IFNULL(pg.no_ktp,'') ktp_dokter, e.id_encounter, es.status enc_status,
			(SELECT COUNT(*) FROM diagnosa_pasien x WHERE x.no_rawat=r.no_rawat) dx_total,
			(SELECT COUNT(*) FROM satu_sehat_condition x WHERE x.no_rawat=r.no_rawat AND x.id_condition<>'') dx_kirim,
			(SELECT COUNT(*) FROM prosedur_pasien x WHERE x.no_rawat=r.no_rawat) pr_total,
			(SELECT COUNT(*) FROM satu_sehat_procedure x WHERE x.no_rawat=r.no_rawat AND x.id_procedure<>'') pr_kirim,
			(`+obsCount()+`) obs_kirim,
			(SELECT COUNT(*) FROM resep_obat ro JOIN satu_sehat_medicationrequest x ON x.no_resep=ro.no_resep WHERE ro.no_rawat=r.no_rawat AND x.id_medicationrequest<>'') obat_kirim,
			(SELECT COUNT(*) FROM permintaan_lab pl JOIN satu_sehat_diagnosticreport_lab x ON x.noorder=pl.noorder WHERE pl.no_rawat=r.no_rawat AND x.id_diagnosticreport<>'')
			+ (SELECT COUNT(*) FROM permintaan_radiologi pr JOIN satu_sehat_diagnosticreport_radiologi x ON x.noorder=pr.noorder WHERE pr.no_rawat=r.no_rawat AND x.id_diagnosticreport<>'') hasil_kirim,
			IFNULL(m.id_lokasi_satusehat,'') lokasi, er.pesan galat
			FROM reg_periksa r JOIN pasien p ON p.no_rkm_medis=r.no_rkm_medis JOIN poliklinik pl ON pl.kd_poli=r.kd_poli JOIN dokter dk ON dk.kd_dokter=r.kd_dokter
			LEFT JOIN pegawai pg ON pg.nik=r.kd_dokter LEFT JOIN satu_sehat_encounter e ON e.no_rawat=r.no_rawat LEFT JOIN app_satusehat_encounter_status es ON es.no_rawat=r.no_rawat
			LEFT JOIN satu_sehat_mapping_lokasi_ralan m ON m.kd_poli=r.kd_poli LEFT JOIN app_satusehat_error er ON er.no_rawat=r.no_rawat
			WHERE r.tgl_registrasi BETWEEN ? AND ? AND r.stts<>'Batal'`+where+` ORDER BY r.tgl_registrasi DESC, r.jam_reg DESC LIMIT 500`, dari, sampai)
		d["List"] = list
		selesai := 0
		for _, x := range list {
			if x["enc_status"] == "finished" {
				selesai++
			}
		}
		d["Selesai"], d["Total"] = selesai, len(list)
	case "mapping":
		d["Poli"] = s.rows(ctx, `SELECT pl.kd_poli, pl.nm_poli, IFNULL(m.id_lokasi_satusehat,'') id_lokasi FROM poliklinik pl LEFT JOIN satu_sehat_mapping_lokasi_ralan m ON m.kd_poli=pl.kd_poli WHERE pl.status='1' ORDER BY pl.nm_poli`)
		d["Kamar"] = s.rows(ctx, `SELECT k.kd_kamar, b.nm_bangsal, k.kelas, IFNULL(m.id_lokasi_satusehat,'') id_lokasi FROM kamar k JOIN bangsal b ON b.kd_bangsal=k.kd_bangsal LEFT JOIN satu_sehat_mapping_lokasi_ranap m ON m.kd_kamar=k.kd_kamar WHERE k.statusdata='1' ORDER BY b.nm_bangsal, k.kd_kamar`)
		d["Dokter"] = s.rows(ctx, `SELECT d.kd_dokter, d.nm_dokter, IFNULL(pg.no_ktp,'') no_ktp, IFNULL(i.ihs,'') ihs FROM dokter d LEFT JOIN pegawai pg ON pg.nik=d.kd_dokter
			LEFT JOIN app_satusehat_ihs i ON i.jenis='Practitioner' AND i.nik=pg.no_ktp WHERE d.status='1' ORDER BY d.nm_dokter`)
	case "terminologi":
		d["Obat"] = s.rows(ctx, `SELECT b.kode_brng, b.nama_brng, b.kode_sat, IFNULL(m.obat_code,'') kfa, IFNULL(m.obat_display,'') kfa_display, IFNULL(m.form_code,'') form_code, IFNULL(m.form_display,'') form_display,
			IFNULL(m.route_code,'') route_code, IFNULL(m.route_display,'') route_display, IFNULL(m.denominator_code,'') satuan
			FROM databarang b LEFT JOIN satu_sehat_mapping_obat m ON m.kode_brng=b.kode_brng WHERE b.status='1' ORDER BY m.obat_code IS NOT NULL, b.nama_brng`)
		d["Lab"] = s.rows(ctx, `SELECT t.id_template, t.Pemeriksaan, j.nm_perawatan, IFNULL(m.code,'') code, IFNULL(m.display,'') display, IFNULL(m.sampel_code,'') sampel_code, IFNULL(m.sampel_display,'') sampel_display
			FROM template_laboratorium t JOIN jns_perawatan_lab j ON j.kd_jenis_prw=t.kd_jenis_prw LEFT JOIN satu_sehat_mapping_lab m ON m.id_template=t.id_template ORDER BY j.nm_perawatan, t.urut`)
		d["Rad"] = s.rows(ctx, `SELECT j.kd_jenis_prw, j.nm_perawatan, IFNULL(m.code,'') code, IFNULL(m.display,'') display FROM jns_perawatan_radiologi j LEFT JOIN satu_sehat_mapping_radiologi m ON m.kd_jenis_prw=j.kd_jenis_prw WHERE j.status='1' ORDER BY j.nm_perawatan`)
		d["Depo"] = s.rows(ctx, `SELECT b.kd_bangsal, b.nm_bangsal, IFNULL(m.id_lokasi_satusehat,'') id_lokasi FROM bangsal b LEFT JOIN satu_sehat_mapping_lokasi_depo_farmasi m ON m.kd_bangsal=b.kd_bangsal WHERE b.kd_bangsal IN ('AP','GF')`)
	case "log":
		d["Log"] = s.rows(ctx, "SELECT * FROM app_satusehat_log ORDER BY id DESC LIMIT 100")
	}
	s.render(w, r, "satusehat", d, 200)
}

func obsCount() string {
	var parts []string
	for _, v := range satusehat.Vitals {
		parts = append(parts, "(SELECT COUNT(*) FROM satu_sehat_observationttv"+v.Jenis+" x WHERE x.no_rawat=r.no_rawat AND x.id_observation<>'')")
	}
	return strings.Join(parts, "+")
}

func ringkas(res satusehat.Result) string {
	msg := strconv.Itoa(len(res.Sent)) + " resource terkirim"
	if len(res.Sent) > 0 && len(res.Sent) <= 6 {
		msg += " (" + strings.Join(res.Sent, ", ") + ")"
	}
	if len(res.Warnings) > 0 {
		msg += " · dilewati: " + strings.Join(res.Warnings, "; ")
	}
	return msg
}

func (s *Server) ssAksi(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	f := r.FormValue
	back := s.Prefix + "/satusehat?tab=" + f("tab") + "&dari=" + f("dari") + "&sampai=" + f("sampai") + "&st=" + f("st")
	if !s.SS.Cfg.Enabled() && r.PathValue("aksi") != "nik-dokter" {
		s.back(w, r, back, errors.New("bridging SATUSEHAT tidak aktif — isi SATUSEHAT_MODE dan kredensial di simrs.env"), "")
		return
	}
	switch r.PathValue("aksi") {
	case "token":
		_, err := s.SS.Token(ctx)
		s.back(w, r, back, err, "Koneksi & autentikasi SATUSEHAT berhasil")
	case "kirim":
		no := f("no_rawat")
		res := s.SS.Sync(ctx, s.DB, no)
		s.audit(r, "", "satusehat_kirim", no, strconv.Itoa(len(res.Sent)))
		if err := res.Err(); err != nil {
			s.back(w, r, back, errors.New(no+": "+err.Error()+" ("+ringkas(res)+")"), "")
			return
		}
		s.back(w, r, back, nil, no+": "+ringkas(res))
	case "kirim-semua":
		list, err := satusehat.Kandidat(ctx, s.DB, f("dari"), f("sampai"))
		if err != nil {
			s.back(w, r, back, err, "")
			return
		}
		if len(list) > 100 {
			list = list[:100]
		}
		sent, gagal := 0, []string{}
		for _, no := range list {
			res := s.SS.Sync(ctx, s.DB, no)
			sent += len(res.Sent)
			if res.Err() != nil {
				gagal = append(gagal, no)
			}
		}
		s.audit(r, "", "satusehat_kirim_semua", f("dari")+".."+f("sampai"), strconv.Itoa(sent))
		msg := strconv.Itoa(len(list)) + " kunjungan diproses, " + strconv.Itoa(sent) + " resource terkirim"
		if len(gagal) > 0 {
			s.back(w, r, back, errors.New(msg+"; "+strconv.Itoa(len(gagal))+" kunjungan masih bermasalah — sebabnya tertulis di baris yang ditandai"), "")
			return
		}
		s.back(w, r, back, nil, msg)
	case "lokasi":
		jenis, kode, id := f("jenis"), f("kode"), strings.TrimSpace(f("id_lokasi"))
		var err error
		if f("auto") == "1" {
			id, err = s.SS.CreateLocation(ctx, s.DB, jenis, kode)
		} else {
			table, key := "satu_sehat_mapping_lokasi_ralan", "kd_poli"
			if jenis == "ranap" {
				table, key = "satu_sehat_mapping_lokasi_ranap", "kd_kamar"
			} else if jenis == "depo" {
				table, key = "satu_sehat_mapping_lokasi_depo_farmasi", "kd_bangsal"
			}
			if id == "" {
				_, err = s.DB.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+key+"=?", kode)
			} else if lain := s.val(ctx, "SELECT kd FROM (SELECT kd_poli kd, id_lokasi_satusehat i FROM satu_sehat_mapping_lokasi_ralan UNION ALL SELECT kd_kamar, id_lokasi_satusehat FROM satu_sehat_mapping_lokasi_ranap) x WHERE i=? AND kd<>? LIMIT 1", id, kode); lain != "" {
				err = errors.New("ID lokasi itu sudah dipakai untuk " + lain)
			} else {
				_, err = s.DB.ExecContext(ctx, "INSERT INTO "+table+" ("+key+",id_organisasi_satusehat,id_lokasi_satusehat) VALUES (?,?,?) ON DUPLICATE KEY UPDATE id_lokasi_satusehat=VALUES(id_lokasi_satusehat), id_organisasi_satusehat=VALUES(id_organisasi_satusehat)", kode, s.SS.Cfg.OrgID, id)
			}
		}
		s.back(w, r, back, err, "Mapping lokasi "+kode+" tersimpan "+id)
	case "map-obat":
		kd, kfa := f("kode_brng"), strings.TrimSpace(f("kfa"))
		var err error
		if kfa == "" {
			_, err = s.DB.ExecContext(ctx, "DELETE FROM satu_sehat_mapping_obat WHERE kode_brng=?", kd)
		} else if len(kfa) < 8 || strings.Trim(kfa, "0123456789") != "" {
			err = errors.New("kode KFA harus angka (min. 8 digit, mis. 93xxxxxx untuk produk/POA)")
		} else {
			_, err = s.DB.ExecContext(ctx, `INSERT INTO satu_sehat_mapping_obat VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE obat_code=VALUES(obat_code), obat_display=VALUES(obat_display),
				form_code=VALUES(form_code), form_display=VALUES(form_display), denominator_code=VALUES(denominator_code), route_code=VALUES(route_code), route_display=VALUES(route_display)`,
				kd, kfa, satusehat.SysKFA, f("kfa_display"), f("form_code"), satusehat.SysMedForm, f("form_display"), "", "", f("satuan"), "http://terminology.hl7.org/CodeSystem/v3-orderableDrugForm", f("route_code"), satusehat.SysRoute, f("route_display"))
			s.DB.ExecContext(ctx, "DELETE FROM satu_sehat_medication WHERE kode_brng=?", kd) // kirim ulang Medication dengan kode baru
		}
		s.back(w, r, back, err, "Mapping obat "+kd+" tersimpan")
	case "map-lab", "map-rad":
		code := strings.TrimSpace(f("code"))
		var err error
		if code != "" && !loincRe.MatchString(code) {
			s.back(w, r, back, errors.New("format kode LOINC tidak valid (contoh 718-7)"), "")
			return
		}
		if r.PathValue("aksi") == "map-lab" {
			if code == "" {
				_, err = s.DB.ExecContext(ctx, "DELETE FROM satu_sehat_mapping_lab WHERE id_template=?", f("id"))
			} else {
				_, err = s.DB.ExecContext(ctx, "INSERT INTO satu_sehat_mapping_lab VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE code=VALUES(code), display=VALUES(display), sampel_code=VALUES(sampel_code), sampel_display=VALUES(sampel_display)",
					f("id"), code, satusehat.SysLOINC, f("display"), f("sampel_code"), satusehat.SysSNOMED, f("sampel_display"))
			}
		} else {
			if code == "" {
				_, err = s.DB.ExecContext(ctx, "DELETE FROM satu_sehat_mapping_radiologi WHERE kd_jenis_prw=?", f("id"))
			} else {
				_, err = s.DB.ExecContext(ctx, "INSERT INTO satu_sehat_mapping_radiologi (kd_jenis_prw,code,system,display) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE code=VALUES(code), display=VALUES(display)", f("id"), code, satusehat.SysLOINC, f("display"))
			}
		}
		s.back(w, r, back, err, "Mapping "+f("id")+" tersimpan")
	case "nik-dokter":
		kd, nik := f("kd_dokter"), strings.TrimSpace(f("no_ktp"))
		if nik != "" && (len(nik) != 16 || strings.Trim(nik, "0123456789") != "") {
			s.back(w, r, back, errors.New("NIK harus 16 digit angka"), "")
			return
		}
		nama := s.val(ctx, "SELECT nm_dokter FROM dokter WHERE kd_dokter=?", kd)
		if nama == "" {
			s.back(w, r, back, errors.New("dokter tidak ditemukan"), "")
			return
		}
		_, err := s.DB.ExecContext(ctx, "INSERT INTO pegawai (nik,nama,no_ktp) VALUES (?,?,?) ON DUPLICATE KEY UPDATE no_ktp=VALUES(no_ktp)", kd, nama, nik)
		msg := "NIK " + nama + " tersimpan"
		if err == nil && nik != "" && s.SS.Cfg.Enabled() {
			if ihs, e := s.SS.IHS(ctx, s.DB, "Practitioner", nik); e != nil {
				msg += " · cek SATUSEHAT: " + e.Error()
			} else {
				msg += " · IHS " + ihs
			}
		}
		s.back(w, r, back, err, msg)
	default:
		http.NotFound(w, r)
	}
}
