#!/usr/bin/env python3
"""Isi data demo SIMRS lewat HTTP (memakai akun demo-*), seperti pengguna sungguhan.
Jalankan setelah DB kosong + -migrate. Usage: python3 seed_demo.py"""
import http.cookiejar, urllib.request, urllib.parse, re, subprocess, datetime, sys, os, struct, zlib

B = "http://127.0.0.1:8092"
ENV = dict(l.split("=", 1) for l in open("/data/workspace/simrs-demo/simrs.env").read().split("\n") if "=" in l)
PW = ENV["DEMO_PASSWORD"]
DBPASS = ENV["DB_PASS"]
TODAY = datetime.date.today().isoformat()
fails = []


def sql(q):
    out = subprocess.run(["mariadb", "-usimrs", "-p" + DBPASS, "-N", "-B", "sik_demo", "-e", q], capture_output=True, text=True)
    if out.returncode:
        raise SystemExit(out.stderr)
    return [l.split("\t") for l in out.stdout.strip().split("\n") if l]


def one(q):
    r = sql(q)
    return r[0][0] if r else ""


class C:
    def __init__(s, user):
        s.cj = http.cookiejar.CookieJar()
        s.op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(s.cj))
        s.req("/login", {"user": user, "password": PW}, csrf=False)
        s.csrf = re.search(r'name="_csrf" value="([^"]+)"', s.get("/")).group(1)

    def get(s, p):
        return s.op.open(B + p).read().decode()

    def req(s, p, data, csrf=True):
        pairs = []
        for k, v in data.items():
            for x in (v if isinstance(v, list) else [v]):
                pairs.append((k, x))
        if csrf:
            pairs.append(("_csrf", s.csrf))
        r = s.op.open(B + p, urllib.parse.urlencode(pairs).encode())
        return r.geturl(), r.read().decode()

    def post(s, p, data):
        url, body = s.req(p, data)
        m = re.search(r'class="flash (ok|err)"[^>]*>([^<]*)', body)
        msg = m.group(2) if m else url
        if m and m.group(1) == "err":
            fails.append(f"{p}: {msg}")
            print("  GAGAL", p, msg)
        return msg

    def upload(s, p, fields, fname, data, ctype):
        bnd = "----demo" + os.urandom(6).hex()
        body = b""
        for k, v in list(fields.items()) + [("_csrf", s.csrf)]:
            body += f"--{bnd}\r\nContent-Disposition: form-data; name=\"{k}\"\r\n\r\n{v}\r\n".encode()
        body += f"--{bnd}\r\nContent-Disposition: form-data; name=\"gambar\"; filename=\"{fname}\"\r\nContent-Type: {ctype}\r\n\r\n".encode() + data + f"\r\n--{bnd}--\r\n".encode()
        rq = urllib.request.Request(B + p, body, {"Content-Type": "multipart/form-data; boundary=" + bnd})
        return s.op.open(rq).geturl()


def png_xray(w=320, h=320):
    # gambar abu-abu sederhana mirip rontgen dada (gradasi + 2 "paru")
    rows = b""
    for y in range(h):
        rows += b"\x00"
        for x in range(w):
            v = 40 + int(60 * (1 - abs(x - w / 2) / (w / 2)))
            for cx in (w * 0.32, w * 0.68):
                d = ((x - cx) / (w * 0.15)) ** 2 + ((y - h * 0.5) / (h * 0.33)) ** 2
                if d < 1:
                    v = 20 + int(25 * d)
            if abs(x - w / 2) < w * 0.04:
                v = 200
            rows += bytes([min(255, v)])
    def chunk(t, d):
        return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xffffffff)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 0, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b"")


sql("INSERT IGNORE INTO penyakit (kd_penyakit,nm_penyakit) VALUES ('K40.9','Unilateral or unspecified inguinal hernia, without obstruction or gangrene')")
adm, daf, dr, prw, apt, lab, rad, ok, gz, ksr, keu = (C("demo-" + u) for u in ["admin", "daftar", "dokter", "perawat", "farmasi", "lab", "rad", "ok", "gizi", "kasir", "keu"])
print("login 11 peran OK")

# ---------------------------------------------------------------- setelan dasar
print(keu.post("/keuangan/jurnal", {"tab": "jurnal", "no_bukti": "SA-2026", "keterangan": "Saldo awal: kas, bank, persediaan, modal",
      "kd_rek": ["111010", "111020", "114010", "310100"], "debet": ["25000000", "750000000", "60000000", ""], "kredit": ["", "", "", "835000000"]}))
# NIK dokter & lokasi SATUSEHAT
for kd, nik in sql("SELECT kd_dokter, CONCAT('33720202029000', RIGHT(kd_dokter,2)) FROM dokter"):
    daf.post("/satusehat/nik-dokter", {"tab": "mapping", "kd_dokter": kd, "no_ktp": nik})
for kd, in sql("SELECT kd_poli FROM poliklinik WHERE status='1'"):
    daf.post("/satusehat/lokasi", {"tab": "mapping", "jenis": "ralan", "kode": kd, "auto": "1"})
for kd, in sql("SELECT kd_kamar FROM kamar"):
    daf.post("/satusehat/lokasi", {"tab": "mapping", "jenis": "ranap", "kode": kd, "auto": "1"})
daf.post("/satusehat/lokasi", {"tab": "terminologi", "jenis": "depo", "kode": "AP", "auto": "1"})
# contoh mapping KFA (kode contoh untuk server tiruan)
for kode, kfa, nama in [("B000006", "93000101", "Amlodipine 5 mg Tablet"), ("B000001", "93000102", "Paracetamol 500 mg Tablet"), ("B000002", "93000103", "Amoxicillin 500 mg Kapsul")]:
    daf.post("/satusehat/map-obat", {"tab": "terminologi", "kode_brng": kode, "kfa": kfa, "kfa_display": nama + " (contoh)", "form_code": "BS066", "form_display": "Tablet", "route_code": "O", "route_display": "Oral", "satuan": "TAB"})
print("setelan akun, NIK dokter, lokasi & mapping SATUSEHAT OK")

# ---------------------------------------------------------------- logistik
print(apt.post("/logistik/suplier", {"kode_suplier": "S0003", "nama_suplier": "PT Anugrah Argon Medica", "alamat": "Jl. Industri 12", "kota": "Semarang", "no_telp": "024-7612345", "nama_bank": "BCA", "rekening": "1234567890"}))
apt.post("/logistik/sp", {"kode_suplier": "S0002", "ppn": "11", "kode_brng": ["B000006", "B000001", "B000002", "B000003"], "jumlah": ["2000", "3000", "1500", "1000"], "harga": ["280", "150", "350", "250"], "dis": ["", "5", "", "2"]})
sp = one("SELECT no_pemesanan FROM surat_pemesanan_medis ORDER BY no_pemesanan LIMIT 1")
d40 = (datetime.date.today() - datetime.timedelta(days=40)).isoformat()
print(apt.post("/logistik/terima", {"no_faktur": "AAM/2610/0881", "no_order": sp, "kode_suplier": "S0002", "kd_bangsal": "GF", "tgl_faktur": d40,
      "tgl_tempo": (datetime.date.today() - datetime.timedelta(days=10)).isoformat(), "ppn": "11", "meterai": "10000", "update_harga": "1",
      "kode_brng": ["B000006", "B000001", "B000002", "B000003"], "jumlah": ["2000", "3000", "1500", "1000"], "harga": ["280", "150", "350", "250"], "dis": ["0", "5", "0", "2"],
      "no_batch": ["AML2604", "PCT2611", "AMX2603", "IBU2512"], "kadaluarsa": ["2028-09-30", "2028-01-31", "2027-03-31", (datetime.date.today() + datetime.timedelta(days=75)).isoformat()]}))
print(apt.post("/logistik/terima", {"no_faktur": "INV-KF-55120", "kode_suplier": "S0003", "kd_bangsal": "GF", "tgl_faktur": TODAY,
      "tgl_tempo": (datetime.date.today() + datetime.timedelta(days=30)).isoformat(), "ppn": "11", "meterai": "10000",
      "kode_brng": ["B000004", "B000005", "B000007"], "jumlah": ["500", "300", "200"], "harga": ["900", "1200", "2500"], "no_batch": ["X1", "X2", "X3"], "kadaluarsa": ["2028-05-31", "2028-06-30", "2027-12-31"]}))
apt.post("/logistik/sp", {"kode_suplier": "S0003", "ppn": "11", "kode_brng": ["B000009", "B000010"], "jumlah": ["200", "300"], "harga": ["4500", "800"]})
print(apt.post("/logistik/mutasi", {"dari": "GF", "ke": "AP", "keterangan": "Permintaan depo mingguan", "kode_brng": ["B000006", "B000001", "B000002", "B000003", "B000004"], "jumlah": ["500", "800", "400", "200", "100"]}))
print(apt.post("/logistik/opname", {"kd_bangsal": "GF", "kode_brng": "B000002", "real": "1095", "keterangan": "5 strip rusak kemasan"}))
print(keu.post("/keuangan/bayar-hutang", {"tab": "hutang", "no_faktur": "AAM/2610/0881", "besar": "800000", "via": "Bank", "no_bukti": "TRF-0081", "keterangan": "termin 1"}))

# ---------------------------------------------------------------- pasien
pasien = [
    ("Budi Hartono", "L", "1968-04-12", "3372011204680001", "Jl. Slamet Riyadi 21, Surakarta", "BPJ", "0001234567890"),
    ("Siti Aminah", "P", "1958-03-12", "3372015203580002", "Jl. Veteran 5, Surakarta", "A09", ""),
    ("Agus Prasetyo", "L", "1990-11-02", "3372010211900003", "Kartasura, Sukoharjo", "A09", ""),
    ("Rina Wulandari", "P", "1994-07-19", "3372015907940004", "Jl. Adi Sucipto 88", "BPJ", "0001234567891"),
    ("Joko Susilo", "L", "1975-01-25", "3372012501750005", "Colomadu, Karanganyar", "A09", ""),
    ("Dewi Lestari", "P", "1987-09-09", "3372014909870006", "Jl. Yos Sudarso 14", "BPJ", "0001234567892"),
    ("Bambang Sutrisno", "L", "1949-06-30", "3372013006490007", "Grogol, Sukoharjo", "A09", ""),
    ("Ani Rahmawati", "P", "2019-02-14", "3372015402190008", "Jl. Kapten Mulyadi 3", "A09", ""),
    ("Hendra Gunawan", "L", "1982-12-01", "3372010112820009", "Jebres, Surakarta", "A09", ""),
    ("Sri Mulyani", "P", "1963-05-21", "3372016105630010", "Laweyan, Surakarta", "BPJ", "0001234567890"),
]
for nm, jk, tl, nik, al, pj, kartu in pasien:
    daf.post("/pasien/simpan", {"nm_pasien": nm, "jk": jk, "tgl_lahir": tl, "no_ktp": nik, "alamat": al, "kd_pj": pj, "no_peserta": kartu, "no_tlp": "08123" + nik[-6:]})
rm = {r[1].title(): r[0] for r in sql("SELECT no_rkm_medis, nm_pasien FROM pasien")}
print(len(rm), "pasien")


def daftar(nama, poli, dokter, pj):
    daf.post("/registrasi", {"no_rkm_medis": rm[nama.title()], "kd_poli": poli, "kd_dokter": dokter, "kd_pj": pj})
    return one(f"SELECT no_rawat FROM reg_periksa WHERE no_rkm_medis='{rm[nama.title()]}' ORDER BY no_rawat DESC LIMIT 1")


# 1) Budi — BPJS, poli dalam, SEP, lengkap sampai lunas (piutang BPJS)
n1 = daftar("Budi Hartono", "INT", "D0000002", "BPJ")
daf.post("/bpjs/sep", {"no_rawat": n1, "no_kartu": "0001234567890", "klsrawat": "3", "asal_rujukan": "1", "no_rujukan": "0101G0011025P000890", "tglrujukan": TODAY, "kdppkrujukan": "0101G001", "diagawal": "I10 Essential (primary) hypertension", "kdpolitujuan": "INT", "kddpjp": "10002", "notelep": "081234567890"})
dr.post("/rawat/soap", {"no_rawat": n1, "tensi": "160/100", "nadi": "88", "respirasi": "20", "suhu_tubuh": "36.7", "tinggi": "168", "berat": "74", "keluhan": "Pusing, tengkuk berat 3 hari", "pemeriksaan": "KU baik, CM", "penilaian": "Hipertensi stage 2", "rtl": "Amlodipine, cek lab lipid & GDS", "alergi": "Amoxicillin"})
dr.post("/rawat/diagnosa", {"no_rawat": n1, "kd_penyakit": "I10"})
dr.post("/rawat/prosedur", {"no_rawat": n1, "kode": "89.52"})
dr.post("/rawat/tindakan", {"no_rawat": n1, "kd_jenis_prw": ["RJ002", "RJ007"], "kd_dokter": "D0000002"})
dr.post("/rawat/resep", {"no_rawat": n1, "kode_brng": ["B000006", "B000001", ""], "jml": ["30", "10", ""], "aturan": ["1x1 pagi", "3x1 bila pusing", ""]})
dr.post("/rawat/lab", {"no_rawat": n1, "kd_jenis_prw": ["LAB002", "LAB003"], "diagnosa_klinis": "Hipertensi"})
r1 = one(f"SELECT no_resep FROM resep_obat WHERE no_rawat='{n1}'")
apt.post("/farmasi/validasi", {"no_resep": r1}); apt.post("/farmasi/serahkan", {"no_resep": r1})
o1 = one(f"SELECT noorder FROM permintaan_lab WHERE no_rawat='{n1}'")
lab.post("/lab/hasil", {"noorder": o1, "kd_dokter": "D0000001", "t_6": "245", "t_7": "232"})
print("Budi:", ksr.post("/kasir/bayar", {"no_rawat": n1}))

# 2) Agus — umum, poli umum, lunas tunai
n2 = daftar("Agus Prasetyo", "U0001", "D0000001", "A09")
prw.post("/rawat/soap", {"no_rawat": n2, "tensi": "120/80", "nadi": "92", "suhu_tubuh": "38.4", "keluhan": "Demam, batuk pilek 2 hari", "penilaian": "ISPA", "rtl": "Simptomatik"})
dr.post("/rawat/diagnosa", {"no_rawat": n2, "kd_penyakit": "J06.9"})
dr.post("/rawat/tindakan", {"no_rawat": n2, "kd_jenis_prw": ["RJ001"], "kd_dokter": "D0000001"})
dr.post("/rawat/resep", {"no_rawat": n2, "kode_brng": ["B000001", "B000002"], "jml": ["10", "15"], "aturan": ["3x1", "3x1 habiskan"]})
r2 = one(f"SELECT no_resep FROM resep_obat WHERE no_rawat='{n2}'")
apt.post("/farmasi/validasi", {"no_resep": r2}); apt.post("/farmasi/serahkan", {"no_resep": r2})
print("Agus:", ksr.post("/kasir/bayar", {"no_rawat": n2}))

# 3) Joko — IGD, radiologi + gambar + expertise, operasi appendektomi, ranap (masih dirawat)
n3 = daftar("Joko Susilo", "IGDK", "D0000006", "A09")
prw.post("/rawat/soap", {"no_rawat": n3, "tensi": "130/85", "nadi": "104", "suhu_tubuh": "38.1", "keluhan": "Nyeri perut kanan bawah sejak semalam, mual", "pemeriksaan": "McBurney (+), rebound (+)", "penilaian": "Susp. appendisitis akut"})
dr.post("/rawat/diagnosa", {"no_rawat": n3, "kd_penyakit": "K35.8"})
dr.post("/rawat/radiologi", {"no_rawat": n3, "kd_jenis_prw": ["RAD003"], "diagnosa_klinis": "Susp. appendisitis"})
o3 = one(f"SELECT noorder FROM permintaan_radiologi WHERE no_rawat='{n3}'")
rad.post("/radiologi/periksa", {"noorder": o3, "nip": "P006", "kd_dokter": "D0000007", "kd": ["RAD003"], "kv_RAD003": "75", "mas_RAD003": "20", "proyeksi_RAD003": "AP", "ffd_RAD003": "100"})
rad.upload("/radiologi/gambar", {"noorder": o3}, "bno-ap.png", png_xray(), "image/png")
rad.post("/radiologi/expertise", {"noorder": o3, "hasil": "BNO AP:\nDistribusi udara usus normal, tidak tampak fecalith yang jelas.\nTidak tampak free air.\nKesan: tidak tampak kelainan radiologis yang spesifik; klinis appendisitis tidak dapat disingkirkan."})
daf.post("/rawat/kamar-masuk", {"no_rawat": n3, "kd_kamar": "MLT.01", "diagnosa_awal": "Appendisitis akut", "kd_dokter": "D0000006"})
dr.post("/operasi/booking", {"no_rawat": n3, "from": "rawat", "kode_paket": "OP001", "kd_ruang_ok": "OK1", "kd_dokter": "D0000006", "tanggal": TODAY, "jam_mulai": "08:00", "jam_selesai": "09:30"})
ok.post("/operasi/mulai", {"no_rawat": n3, "kode_paket": "OP001", "tanggal": TODAY, "jam_mulai": "08:00:00", "from": "jadwal"})
tglop = TODAY + "T08:05"
ok.post("/operasi/simpan", {"no_rawat": n3, "kode_paket": "OP001", "tgl_operasi": tglop, "jenis_anasthesi": "SAB", "kategori": "Sedang", "operator1": "D0000006", "dokter_anestesi": "D0000008", "asisten_operator1": "P001", "instrumen": "P002", "status": "Selesai"})
dr.post("/operasi/laporan", {"no_rawat": n3, "tanggal": tglop, "diagnosa_preop": "Appendisitis akut", "diagnosa_postop": "Appendisitis akut perforasi lokal", "jaringan_dieksekusi": "Appendiks", "selesaioperasi": TODAY + "T09:20", "permintaan_pa": "Ya", "laporan_operasi": "Insisi McBurney, appendiks hiperemis, ujung perforasi kecil. Appendektomi, cuci, jahit lapis demi lapis."})
ok.post("/operasi/bhp", {"no_rawat": n3, "kd_obat": "OK001", "jumlah": "2"})
dr.post("/rawat/resep", {"no_rawat": n3, "kode_brng": ["B000004"], "jml": ["6"], "aturan": ["2x1"]})
print("Joko: rad + OK + ranap")

# 4) Siti — IGD -> ranap (DM), lab, diet, alergi; resep menunggu farmasi
n4 = daftar("Siti Aminah", "IGDK", "D0000001", "A09")
prw.post("/rawat/soap", {"no_rawat": n4, "tensi": "150/90", "nadi": "96", "suhu_tubuh": "37.6", "keluhan": "Luka kaki kanan tidak sembuh 2 minggu, lemas", "penilaian": "DM tipe 2 dengan ulkus", "alergi": "Udang"})
dr.post("/rawat/diagnosa", {"no_rawat": n4, "kd_penyakit": "E11.9"})
daf.post("/rawat/kamar-masuk", {"no_rawat": n4, "kd_kamar": "ANG.01", "diagnosa_awal": "DM tipe 2 + ulkus pedis", "kd_dokter": "D0000002"})
dr.post("/rawat/lab", {"no_rawat": n4, "kd_jenis_prw": ["LAB001", "LAB002"], "diagnosa_klinis": "DM ulkus"})
dr.post("/rawat/resep", {"no_rawat": n4, "kode_brng": ["B000003", "B000001"], "jml": ["10", "10"], "aturan": ["3x1 sesudah makan", "3x1"]})

# 5) Bambang — ranap lansia (CKD), belum ada diet -> muncul peringatan di Gizi
n5 = daftar("Bambang Sutrisno", "IGDK", "D0000001", "A09")
prw.post("/rawat/soap", {"no_rawat": n5, "tensi": "170/100", "nadi": "88", "keluhan": "Sesak, bengkak kedua kaki", "penilaian": "CKD stage V"})
daf.post("/rawat/kamar-masuk", {"no_rawat": n5, "kd_kamar": "MWR.01", "diagnosa_awal": "CKD stage V", "kd_dokter": "D0000002"})

# 6) Rina — BPJS poli anak?/kandungan -> antrean menunggu dokter
n6 = daftar("Rina Wulandari", "INT", "D0000002", "BPJ")
# 7) Dewi — BPJS, sudah diperiksa, lab menunggu hasil
n7 = daftar("Dewi Lestari", "INT", "D0000002", "BPJ")
dr.post("/rawat/soap", {"no_rawat": n7, "tensi": "110/70", "nadi": "80", "keluhan": "Nyeri sendi lutut", "penilaian": "Susp. hiperurisemia"})
dr.post("/rawat/diagnosa", {"no_rawat": n7, "kd_penyakit": "M10.9"})
dr.post("/rawat/lab", {"no_rawat": n7, "kd_jenis_prw": ["LAB004"], "diagnosa_klinis": "Gout?"})
# 8) Ani — anak, poli umum, menunggu kasir
n8 = daftar("Ani Rahmawati", "U0001", "D0000001", "A09")
prw.post("/rawat/soap", {"no_rawat": n8, "suhu_tubuh": "37.9", "berat": "17", "keluhan": "Diare 1 hari", "penilaian": "GEA tanpa dehidrasi"})
dr.post("/rawat/diagnosa", {"no_rawat": n8, "kd_penyakit": "A09"})
dr.post("/rawat/tindakan", {"no_rawat": n8, "kd_jenis_prw": ["RJ001"], "kd_dokter": "D0000001"})
# 9) Hendra — booking operasi besok (jadwal OK)
n9 = daftar("Hendra Gunawan", "IGDK", "D0000006", "A09")
prw.post("/rawat/soap", {"no_rawat": n9, "keluhan": "Benjolan lipat paha kanan", "penilaian": "Hernia inguinalis lateralis dextra"})
dr.post("/rawat/diagnosa", {"no_rawat": n9, "kd_penyakit": "K40.9"})
besok = (datetime.date.today() + datetime.timedelta(days=1)).isoformat()
dr.post("/operasi/booking", {"no_rawat": n9, "from": "rawat", "kode_paket": "OP002", "kd_ruang_ok": "OK1", "kd_dokter": "D0000006", "tanggal": besok, "jam_mulai": "09:00", "jam_selesai": "10:30"})
# 10) Sri — BPJS, lunas (piutang BPJS kedua)
n10 = daftar("Sri Mulyani", "INT", "D0000002", "BPJ")
dr.post("/rawat/soap", {"no_rawat": n10, "tensi": "140/90", "nadi": "78", "keluhan": "Kontrol DM & HT", "penilaian": "DM tipe 2 terkontrol, HT"})
dr.post("/rawat/diagnosa", {"no_rawat": n10, "kd_penyakit": "E11.9"}); dr.post("/rawat/diagnosa", {"no_rawat": n10, "kd_penyakit": "I10"})
dr.post("/rawat/tindakan", {"no_rawat": n10, "kd_jenis_prw": ["RJ002"], "kd_dokter": "D0000002"})
dr.post("/rawat/resep", {"no_rawat": n10, "kode_brng": ["B000006"], "jml": ["30"], "aturan": ["1x1"]})
r10 = one(f"SELECT no_resep FROM resep_obat WHERE no_rawat='{n10}'")
apt.post("/farmasi/validasi", {"no_resep": r10}); apt.post("/farmasi/serahkan", {"no_resep": r10})
print("Sri:", ksr.post("/kasir/bayar", {"no_rawat": n10}))

# ---------------------------------------------------------------- gizi
for no, a, b in [(n3, "D06", "D05"), (n4, "D03", "D03")]:
    gz.post("/gizi/simpan", {"tab": "pasien", "tgl": TODAY, "no_rawat": no, "w_Pagi": a, "w_Siang": b, "w_Sore": b, "w_Malam": b})

# ---------------------------------------------------------------- keuangan: klaim BPJS sebagian, biaya operasional
sisa = one(f"SELECT sisapiutang FROM piutang_pasien WHERE no_rawat='{n1}'")
if sisa:
    print(keu.post("/keuangan/bayar-piutang", {"tab": "piutang", "no_rawat": n1, "cicilan": sisa, "via": "Bank", "catatan": "Klaim BPJS bulan lalu"}))
keu.post("/keuangan/jurnal", {"tab": "jurnal", "no_bukti": "PLN-1026", "keterangan": "Bayar listrik & air bulan berjalan", "kd_rek": ["520100", "111020"], "debet": ["8750000", ""], "kredit": ["", "8750000"]})

# ---------------------------------------------------------------- SATUSEHAT
print("SATUSEHAT:", daf.post("/satusehat/kirim-semua", {"tab": "kirim", "dari": TODAY, "sampai": TODAY}))

print("\nselesai,", len(fails), "gagal")
for f in fails:
    print(" -", f)
