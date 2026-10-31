# SIMRS Go

Sistem Informasi Manajemen Rumah Sakit (SIMRS) dengan Rekam Medis Elektronik (ERM), ditulis dalam Go. Proses bisnis dan struktur datanya meniru **SIMKES Khanza**: nama database `sik`, tabel `pasien`, `reg_periksa`, `pemeriksaan_ralan`, `resep_obat`, `kamar_inap`, `billing`, dan seterusnya. Penomoran juga sama dengan Khanza: `no_rawat` `yyyy/MM/dd/NNNNNN`, No. RM 6 digit, dan nomor antrean per dokter/poli.

Aplikasi ini berupa satu file biner tanpa dependensi. Antarmukanya web, bisa dibuka dari browser mana pun di jaringan RS. Database yang dipakai MySQL atau MariaDB.

## Modul & alur

1. **Registrasi**
   - Pasien baru: No. RM otomatis, validasi NIK, cek NIK ganda.
   - Kunjungan: `no_rawat`, nomor antrean, status Baru/Lama (per RS dan per poli), biaya registrasi baru/lama, dan umur saat daftar.
   - Dokter dipilih sesuai jadwal hari itu.
   - Registrasi bisa dibatalkan selama belum ada tindakan.
2. **Rawat jalan / IGD (ERM)**
   - SOAP/CPPT dengan tanda vital dan peringatan alergi.
   - Diagnosa ICD-10 (prioritas, kasus baru/lama) dan prosedur ICD-9-CM.
   - Tindakan dengan tarif dari master.
   - E-resep, permintaan lab, dan riwayat kunjungan.
3. **Farmasi**: antrean resep → validasi (memotong stok depo dan mencatat `riwayat_barang_medis`; ditolak bila stok kurang) → penyerahan. Ada etiket, penerimaan stok, dan opname.
4. **Laboratorium**: order → input hasil per parameter. Flag H/L dihitung otomatis dari nilai rujukan L/P. Hasil bisa dicetak.
5. **Rawat inap**
   - Masuk kamar dari ralan/IGD, pindah kamar, dan pulang dengan status pulang.
   - Lama inap dihitung ala Khanza (minimal 1 hari).
   - Tersedia peta kamar dan ketersediaan bed per bangsal/kelas.
6. **Kasir**
   - Billing gabungan: registrasi, tindakan, lab, obat, kamar, dan biaya tambahan.
   - Pembayaran ditolak bila resep/lab/radiologi belum selesai, operasi masih berlangsung, atau pasien masih dirawat.
   - Membuat nota RJ/RI lalu **mengunci** transaksi. Pembatalan hanya oleh admin.
7. **Radiologi**
   - Dokter mengirim permintaan dari ERM. No. order `PRyyyyMMddNNNN`, sama seperti Khanza.
   - Radiografer mencatat pelaksanaan dan parameter eksposi (proyeksi, kV, mAs, FFD, dosis). Komponen tarif (bagian RS, BHP, jasa dokter/petugas, KSO, manajemen) disalin ke `periksa_radiologi`.
   - Gambar diunggah (JPG/PNG/WEBP/PDF, isi file diverifikasi). Dokter radiologi menulis expertise lalu hasil bisa dicetak.
   - Antrean dibagi 3 tab: menunggu pemeriksaan → menunggu bacaan → selesai.
8. **Kamar operasi / VK**
   - Penjadwalan (`booking_operasi`) dengan **cek bentrok ruang OK** per rentang jam. Status Menunggu → Proses Operasi → Selesai. Ada garis waktu per ruang.
   - Input tindakan dari `paket_operasi` (operasi maupun kebidanan) beserta tim: operator 1–3, asisten, instrumen, dokter anestesi, asisten anestesi, dokter anak, perawat resusitasi, bidan, perawat luar, omloop.
   - Jasa tiap peran ditagihkan hanya bila petugasnya diisi. Sewa OK, alat, akomodasi, bagian RS, dan sarpras selalu ditagihkan.
   - Tersedia laporan operasi (diagnosa pra/pasca bedah, jaringan, kirim PA, no. implan, uraian) yang bisa dicetak, serta obat/BHP kamar operasi.
9. **Bridging BPJS**
   - VClaim: cek peserta (kartu/NIK), rujukan, insert/hapus SEP, cetak SEP.
   - Antrean RS / Mobile JKN: tambah antrean, task id 1–7, batal.
   - Teknis: signature HMAC-SHA256, dekripsi AES-256-CBC, dan LZString. Semua panggilan dicatat ke log.
10. **SATUSEHAT (Kemenkes, FHIR R4)**
    - Mengikuti alur "Kirim SATUSEHAT" Khanza, dengan tabel yang sama: `satu_sehat_encounter`, `satu_sehat_condition`, `satu_sehat_procedure`, `satu_sehat_observationttv*`, `satu_sehat_mapping_lokasi_ralan/ranap`, dan `pegawai.no_ktp` untuk NIK dokter.
    - Urutan kirim: **Encounter** (arrived; AMB/EMER/IMP) → **Condition** ICD-10 → **Procedure** ICD-9-CM → **Observation** tanda vital (nadi, napas, suhu, tensi sistolik/diastolik, TB, BB, lingkar perut; LOINC + UCUM) → **Encounter finished** dengan diagnosis dan riwayat status.
    - Encounter diselesaikan setelah kasir lunas (rawat jalan) atau pasien pulang (rawat inap).
    - IHS pasien/dokter dicari lewat NIK lalu disimpan. Token OAuth2 diperbarui otomatis.
    - Location poli/kamar bisa dibuat langsung dari halaman mapping.
    - Pengiriman bisa per kunjungan, "kirim semua", atau otomatis berkala (`SATUSEHAT_AUTO_MENIT`). Aman diulang karena data yang sudah terkirim tidak dikirim lagi.
    - Penyebab gagal (NIK kosong, lokasi belum di-mapping, pesan OperationOutcome) tampil per kunjungan, dan semua panggilan dicatat di log.
11. **Laporan**: kunjungan per poli (baru/lama, BPJS/non), 10 besar penyakit, pemeriksaan radiologi, tindakan operasi/VK, pendapatan, obat terbanyak, indikator BOR/ALOS/TOI/BTO/GDR, dan ekspor CSV.
12. **Pengguna & keamanan**
   - Login dengan peran: admin, pendaftaran, dokter, perawat, farmasi, lab, radiologi, ok (kamar operasi), kasir, bpjs, manajemen.
   - Password bcrypt, sesi di database, token CSRF di setiap form, dan audit log.

## Instalasi

```bash
# 1. database
mysql -u root -e "CREATE DATABASE sik CHARACTER SET utf8mb4; CREATE USER 'simrs'@'localhost' IDENTIFIED BY 'ganti-password'; GRANT ALL ON sik.* TO 'simrs'@'localhost';"
# 2. konfigurasi
cp simrs.env.example simrs.env   # isi DB_PASS, SESSION_SECRET, ADMIN_PASSWORD
# 3. buat tabel + data master contoh, lalu jalankan
./simrs -migrate
./simrs
```

Setelah itu buka `http://localhost:8090` dan login sebagai `admin`. Setelah login pertama, hapus `ADMIN_PASSWORD` dari `simrs.env`. Untuk mengganti password lewat terminal: `./simrs -set-password admin` (password diketik di terminal).

Gambar radiologi disimpan di folder `UPLOAD_DIR` (bawaan `uploads`). Ikutkan folder ini saat backup bersama database. Gambar DICOM dari modalitas/PACS belum didukung; unggah hasil ekspor JPG/PNG atau PDF.

Di Windows, gunakan `simrs.exe` dengan MariaDB/XAMPP. Biner untuk Linux dan Windows tersedia di halaman Releases.

### Membangun dari sumber
```bash
go build -o simrs .
GOOS=windows GOARCH=amd64 go build -o simrs.exe .
```

## Bridging BPJS

Kredensial (cons-id, secret, user key) diisi hanya di `simrs.env` di server, tidak pernah lewat antarmuka web.

- `BPJS_MODE=mock`: menjalankan **server tiruan lokal** yang memverifikasi signature dan mengenkripsi respons seperti server BPJS. Cocok untuk latihan dan demo. Kartu contoh: `0001234567890`, `0001234567891`, `0001234567892`. Kartu `0009999999999` berstatus nonaktif.
- `BPJS_MODE=dev`: memakai server development BPJS (butuh kredensial dev dari BPJS).
- `BPJS_MODE=prod`: memakai server produksi.

Mapping kode: `poliklinik.kd_poli_bpjs` dan `dokter.kd_dokter_bpjs` harus diisi dengan kode referensi BPJS.

## SATUSEHAT

1. Daftarkan RS di portal SATUSEHAT Kemenkes untuk mendapatkan Organization ID, Client ID, dan Client Secret (lingkungan dev/staging dulu).
2. Isi `SATUSEHAT_MODE`, `SATUSEHAT_CLIENT_ID`, `SATUSEHAT_CLIENT_SECRET`, dan `SATUSEHAT_ORG_ID` di `simrs.env`, lalu restart.
3. Login sebagai admin atau peran `bpjs`, buka **SATUSEHAT → Mapping**. Isi NIK setiap dokter, lalu isi atau **Buat** Location untuk tiap poli dan kamar.
4. Pastikan pasien punya NIK. Kirim dari tab **Kirim data**, atau aktifkan `SATUSEHAT_AUTO_MENIT`.

## Pengujian

```bash
go test ./...
```

Pengujian memakai database `sik_test`. Atur `SIMRS_TEST_DSN` atau siapkan user `simrs`. Cakupan:

- **Inti**: alur lengkap ralan dan ranap, termasuk tarif, stok, kunci kasir, dan 20 registrasi paralel.
- **Web**: semua peran, RBAC, CSRF, setiap halaman, unggah gambar (file palsu ditolak, path traversal 404), dan penelusuran semua tautan di halaman utama untuk menangkap URL rusak.
- **SATUSEHAT**: alur ralan, IGD→ranap, token kedaluwarsa, idempotensi, pasien/dokter tanpa NIK, dan server tiruan dipastikan menolak payload yang salah.
- **BPJS**: LZString, AES, dan signature dicocokkan dengan pustaka `lz-string` JavaScript dan Node `crypto`, plus tes klien terhadap server tiruan.

## Batasan

- Ini adalah subset dari Khanza (Khanza memiliki ratusan modul). Yang belum ada antara lain gizi, inventaris non-medis, kepegawaian, akuntansi/jurnal, INA-CBG grouper/E-Klaim, dan cetak berkas klaim.
- Bridging BPJS belum diuji ke server BPJS sungguhan. Yang sudah diuji hanya server tiruan dengan algoritme yang sama.
- SATUSEHAT juga baru diuji dengan server tiruan (`SATUSEHAT_MODE=mock`). Server tiruan ini memvalidasi aturan pokok: token, referensi, identifier unik, ICD/LOINC/UCUM, dan syarat finished. Server Kemenkes punya validasi lebih banyak, jadi uji dulu di `dev`/`stg` dengan kredensial RS sebelum `prod`. Resource lain (MedicationRequest/Dispense, ServiceRequest/Specimen/DiagnosticReport lab & radiologi, AllergyIntolerance, Composition) belum dikirim.
- Bukan produk bersertifikat. Lakukan uji coba dan validasi sebelum dipakai untuk pelayanan nyata.

## Tangkapan layar

![ERM](docs/erm.png)
![Registrasi](docs/registrasi.png)
![Lab](docs/lab.png)
![Billing](docs/billing.png)
![Radiologi](docs/radiologi.png)
![Kamar operasi](docs/operasi.png)
![Input operasi](docs/operasi-input.png)
![SATUSEHAT](docs/satusehat.png)
![Mapping SATUSEHAT](docs/satusehat-mapping.png)
