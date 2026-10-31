-- Skema SIMRS (subset) dengan nama tabel & kolom mengikuti SIMKES Khanza (database "sik").
-- Tabel tambahan aplikasi web diberi awalan app_.

CREATE TABLE IF NOT EXISTS penjab (
  kd_pj char(3) NOT NULL PRIMARY KEY,
  png_jawab varchar(30) NOT NULL,
  status enum('0','1') NOT NULL DEFAULT '1'
);

CREATE TABLE IF NOT EXISTS spesialis (
  kd_sps char(5) NOT NULL PRIMARY KEY,
  nm_sps varchar(50) NOT NULL
);

CREATE TABLE IF NOT EXISTS dokter (
  kd_dokter varchar(20) NOT NULL PRIMARY KEY,
  nm_dokter varchar(50) NOT NULL,
  jk enum('L','P') NOT NULL DEFAULT 'L',
  kd_sps char(5) DEFAULT NULL,
  no_ijn_praktek varchar(120) DEFAULT '',
  kd_dokter_bpjs varchar(20) DEFAULT '',
  status enum('0','1') NOT NULL DEFAULT '1'
);

CREATE TABLE IF NOT EXISTS poliklinik (
  kd_poli char(5) NOT NULL PRIMARY KEY,
  nm_poli varchar(50) NOT NULL,
  registrasi double NOT NULL DEFAULT 0,
  registrasilama double NOT NULL DEFAULT 0,
  kd_poli_bpjs varchar(10) DEFAULT '',
  status enum('0','1') NOT NULL DEFAULT '1'
);

CREATE TABLE IF NOT EXISTS jadwal (
  kd_dokter varchar(20) NOT NULL,
  hari_kerja enum('SENIN','SELASA','RABU','KAMIS','JUMAT','SABTU','AKHAD') NOT NULL,
  jam_mulai time NOT NULL,
  jam_selesai time NOT NULL,
  kd_poli char(5) NOT NULL,
  kuota int NOT NULL DEFAULT 0,
  PRIMARY KEY (kd_dokter, hari_kerja, jam_mulai)
);

CREATE TABLE IF NOT EXISTS pasien (
  no_rkm_medis varchar(15) NOT NULL PRIMARY KEY,
  nm_pasien varchar(40) NOT NULL,
  no_ktp varchar(20) DEFAULT '',
  jk enum('L','P') NOT NULL,
  tmp_lahir varchar(15) DEFAULT '',
  tgl_lahir date NOT NULL,
  nm_ibu varchar(40) DEFAULT '',
  alamat varchar(200) DEFAULT '',
  gol_darah enum('A','B','O','AB','-') DEFAULT '-',
  pekerjaan varchar(60) DEFAULT '',
  stts_nikah enum('BELUM MENIKAH','MENIKAH','JANDA','DUDHA','JOMBLO') DEFAULT 'BELUM MENIKAH',
  agama varchar(12) DEFAULT '',
  tgl_daftar date NOT NULL,
  no_tlp varchar(40) DEFAULT '',
  umur varchar(30) DEFAULT '',
  pnd enum('TS','TK','SD','SMP','SMA','SLTA/SEDERAJAT','D1','D2','D3','D4','S1','S2','S3','-') DEFAULT '-',
  keluarga enum('AYAH','IBU','ISTRI','SUAMI','SAUDARA','ANAK','DIRI SENDIRI','LAIN-LAIN') DEFAULT 'DIRI SENDIRI',
  namakeluarga varchar(50) DEFAULT '',
  kd_pj char(3) NOT NULL DEFAULT 'A09',
  no_peserta varchar(25) DEFAULT '',
  KEY idx_nm (nm_pasien), KEY idx_ktp (no_ktp), KEY idx_peserta (no_peserta)
);

-- penomoran no_rkm_medis (Khanza: set_no_rkm_medis)
CREATE TABLE IF NOT EXISTS set_no_rkm_medis (
  no_rkm_medis varchar(15) NOT NULL PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS reg_periksa (
  no_reg varchar(8) NOT NULL,
  no_rawat varchar(17) NOT NULL PRIMARY KEY,
  tgl_registrasi date NOT NULL,
  jam_reg time NOT NULL,
  kd_dokter varchar(20) NOT NULL,
  no_rkm_medis varchar(15) NOT NULL,
  kd_poli char(5) NOT NULL,
  p_jawab varchar(100) DEFAULT '',
  almt_pj varchar(200) DEFAULT '',
  hubunganpj varchar(20) DEFAULT '',
  biaya_reg double NOT NULL DEFAULT 0,
  stts enum('Belum','Sudah','Batal','Berkas Diterima','Dirujuk','Meninggal','Dirawat','Pulang Paksa') NOT NULL DEFAULT 'Belum',
  stts_daftar enum('-','Lama','Baru') NOT NULL DEFAULT '-',
  status_lanjut enum('Ralan','Ranap') NOT NULL DEFAULT 'Ralan',
  kd_pj char(3) NOT NULL,
  umurdaftar int DEFAULT 0,
  sttsumur enum('Th','Bl','Hr') DEFAULT 'Th',
  status_bayar enum('Sudah Bayar','Belum Bayar') NOT NULL DEFAULT 'Belum Bayar',
  status_poli enum('Lama','Baru') NOT NULL DEFAULT 'Baru',
  KEY idx_tgl (tgl_registrasi), KEY idx_rm (no_rkm_medis), KEY idx_poli (kd_poli, tgl_registrasi)
);

CREATE TABLE IF NOT EXISTS pemeriksaan_ralan (
  no_rawat varchar(17) NOT NULL,
  tgl_perawatan date NOT NULL,
  jam_rawat time NOT NULL,
  suhu_tubuh varchar(5) DEFAULT '',
  tensi varchar(8) DEFAULT '',
  nadi varchar(3) DEFAULT '',
  respirasi varchar(3) DEFAULT '',
  tinggi varchar(5) DEFAULT '',
  berat varchar(5) DEFAULT '',
  spo2 varchar(3) DEFAULT '',
  gcs varchar(10) DEFAULT '',
  kesadaran varchar(30) DEFAULT 'Compos Mentis',
  keluhan varchar(2000) DEFAULT '',
  pemeriksaan varchar(2000) DEFAULT '',
  alergi varchar(80) DEFAULT '',
  lingkar_perut varchar(5) DEFAULT '',
  rtl varchar(2000) DEFAULT '',
  penilaian varchar(2000) DEFAULT '',
  instruksi varchar(2000) DEFAULT '',
  evaluasi varchar(2000) DEFAULT '',
  nip varchar(20) NOT NULL,
  PRIMARY KEY (no_rawat, tgl_perawatan, jam_rawat)
);

CREATE TABLE IF NOT EXISTS pemeriksaan_ranap LIKE pemeriksaan_ralan;

CREATE TABLE IF NOT EXISTS penyakit (
  kd_penyakit varchar(10) NOT NULL PRIMARY KEY,
  nm_penyakit varchar(100) NOT NULL,
  KEY idx_nm (nm_penyakit)
);

CREATE TABLE IF NOT EXISTS diagnosa_pasien (
  no_rawat varchar(17) NOT NULL,
  kd_penyakit varchar(10) NOT NULL,
  status enum('Ralan','Ranap') NOT NULL,
  prioritas tinyint NOT NULL DEFAULT 1,
  status_penyakit enum('Lama','Baru') DEFAULT 'Baru',
  PRIMARY KEY (no_rawat, kd_penyakit, status)
);

CREATE TABLE IF NOT EXISTS icd9 (
  kode varchar(8) NOT NULL PRIMARY KEY,
  deskripsi_panjang varchar(250) NOT NULL
);

CREATE TABLE IF NOT EXISTS prosedur_pasien (
  no_rawat varchar(17) NOT NULL,
  kode varchar(8) NOT NULL,
  status enum('Ralan','Ranap') NOT NULL,
  prioritas tinyint NOT NULL DEFAULT 1,
  PRIMARY KEY (no_rawat, kode, status)
);

-- tindakan rawat jalan
CREATE TABLE IF NOT EXISTS jns_perawatan (
  kd_jenis_prw varchar(15) NOT NULL PRIMARY KEY,
  nm_perawatan varchar(80) NOT NULL,
  material double DEFAULT 0,
  tarif_tindakandr double DEFAULT 0,
  total_byrdr double NOT NULL DEFAULT 0,
  kd_pj char(3) DEFAULT '-',
  kd_poli char(5) DEFAULT '-',
  status enum('0','1') NOT NULL DEFAULT '1'
);

CREATE TABLE IF NOT EXISTS rawat_jl_dr (
  no_rawat varchar(17) NOT NULL,
  kd_jenis_prw varchar(15) NOT NULL,
  kd_dokter varchar(20) NOT NULL,
  tgl_perawatan date NOT NULL,
  jam_rawat time NOT NULL,
  material double DEFAULT 0,
  tarif_tindakandr double DEFAULT 0,
  biaya_rawat double NOT NULL DEFAULT 0,
  stts_bayar enum('Sudah','Belum','Suspen') DEFAULT 'Belum',
  PRIMARY KEY (no_rawat, kd_jenis_prw, kd_dokter, tgl_perawatan, jam_rawat)
);

-- tindakan rawat inap
CREATE TABLE IF NOT EXISTS jns_perawatan_inap (
  kd_jenis_prw varchar(15) NOT NULL PRIMARY KEY,
  nm_perawatan varchar(80) NOT NULL,
  total_byrdr double NOT NULL DEFAULT 0,
  kelas enum('-','Rawat Umum','Kelas 1','Kelas 2','Kelas 3','Kelas Utama','Kelas VIP','Kelas VVIP') DEFAULT '-',
  status enum('0','1') NOT NULL DEFAULT '1'
);

CREATE TABLE IF NOT EXISTS rawat_inap_dr (
  no_rawat varchar(17) NOT NULL,
  kd_jenis_prw varchar(15) NOT NULL,
  kd_dokter varchar(20) NOT NULL,
  tgl_perawatan date NOT NULL,
  jam_rawat time NOT NULL,
  biaya_rawat double NOT NULL DEFAULT 0,
  PRIMARY KEY (no_rawat, kd_jenis_prw, kd_dokter, tgl_perawatan, jam_rawat)
);

-- rawat inap
CREATE TABLE IF NOT EXISTS bangsal (
  kd_bangsal char(5) NOT NULL PRIMARY KEY,
  nm_bangsal varchar(30) NOT NULL,
  status enum('0','1') NOT NULL DEFAULT '1'
);

CREATE TABLE IF NOT EXISTS kamar (
  kd_kamar varchar(15) NOT NULL PRIMARY KEY,
  kd_bangsal char(5) NOT NULL,
  trf_kamar double NOT NULL DEFAULT 0,
  status enum('ISI','KOSONG','DIBERSIHKAN','DIBOOKING') NOT NULL DEFAULT 'KOSONG',
  kelas enum('Kelas 1','Kelas 2','Kelas 3','Kelas Utama','Kelas VIP','Kelas VVIP') NOT NULL DEFAULT 'Kelas 3',
  statusdata enum('0','1') NOT NULL DEFAULT '1'
);

CREATE TABLE IF NOT EXISTS kamar_inap (
  no_rawat varchar(17) NOT NULL,
  kd_kamar varchar(15) NOT NULL,
  trf_kamar double NOT NULL DEFAULT 0,
  diagnosa_awal varchar(100) DEFAULT '',
  diagnosa_akhir varchar(100) DEFAULT '',
  tgl_masuk date NOT NULL,
  jam_masuk time NOT NULL,
  tgl_keluar date DEFAULT NULL,
  jam_keluar time DEFAULT NULL,
  lama double DEFAULT 0,
  ttl_biaya double DEFAULT 0,
  stts_pulang enum('Sehat','Rujuk','APS','+','Meninggal','Sembuh','Membaik','Pulang Paksa','-','Pindah Kamar','Status Belum Lengkap','Atas Persetujuan Dokter','Atas Permintaan Sendiri','Isoman','Lain-lain') NOT NULL DEFAULT '-',
  PRIMARY KEY (no_rawat, tgl_masuk, jam_masuk)
);

CREATE TABLE IF NOT EXISTS dpjp_ranap (
  no_rawat varchar(17) NOT NULL,
  kd_dokter varchar(20) NOT NULL,
  PRIMARY KEY (no_rawat, kd_dokter)
);

-- farmasi
CREATE TABLE IF NOT EXISTS kodesatuan (
  kode_sat char(4) NOT NULL PRIMARY KEY,
  satuan varchar(30) NOT NULL
);

CREATE TABLE IF NOT EXISTS databarang (
  kode_brng varchar(15) NOT NULL PRIMARY KEY,
  nama_brng varchar(80) NOT NULL,
  kode_sat char(4) NOT NULL,
  h_beli double NOT NULL DEFAULT 0,
  ralan double NOT NULL DEFAULT 0,
  ranap double NOT NULL DEFAULT 0,
  stokminimal double DEFAULT 0,
  status enum('0','1') NOT NULL DEFAULT '1',
  KEY idx_nm (nama_brng)
);

CREATE TABLE IF NOT EXISTS gudangbarang (
  kode_brng varchar(15) NOT NULL,
  kd_bangsal char(5) NOT NULL,
  stok double NOT NULL DEFAULT 0,
  no_batch varchar(20) NOT NULL DEFAULT '',
  no_faktur varchar(20) NOT NULL DEFAULT '',
  PRIMARY KEY (kode_brng, kd_bangsal, no_batch, no_faktur)
);

CREATE TABLE IF NOT EXISTS riwayat_barang_medis (
  kode_brng varchar(15) NOT NULL,
  stok_awal double NOT NULL,
  masuk double NOT NULL DEFAULT 0,
  keluar double NOT NULL DEFAULT 0,
  stok_akhir double NOT NULL,
  posisi enum('Pemberian Obat','Pengadaan','Penerimaan','Piutang','Retur Beli','Retur Jual','Retur Piutang','Mutasi','Opname','Resep Pulang','Retur Pasien','Stok Pasien Ranap','Pengambilan BHP','Penjualan','Stok Keluar','Hibah') NOT NULL,
  tanggal date NOT NULL,
  jam time NOT NULL,
  petugas varchar(20) NOT NULL,
  kd_bangsal char(5) NOT NULL,
  status enum('Simpan','Hapus') NOT NULL DEFAULT 'Simpan',
  no_batch varchar(20) DEFAULT '',
  no_faktur varchar(20) DEFAULT '',
  keterangan varchar(100) DEFAULT '',
  KEY idx_brg (kode_brng, tanggal)
);

CREATE TABLE IF NOT EXISTS resep_obat (
  no_resep varchar(14) NOT NULL PRIMARY KEY,
  tgl_perawatan date DEFAULT NULL,
  jam time DEFAULT NULL,
  no_rawat varchar(17) NOT NULL,
  kd_dokter varchar(20) NOT NULL,
  tgl_peresepan date NOT NULL,
  jam_peresepan time NOT NULL,
  status enum('ralan','ranap') NOT NULL,
  tgl_penyerahan date DEFAULT NULL,
  jam_penyerahan time DEFAULT NULL,
  KEY idx_rawat (no_rawat)
);

CREATE TABLE IF NOT EXISTS resep_dokter (
  no_resep varchar(14) NOT NULL,
  kode_brng varchar(15) NOT NULL,
  jml double NOT NULL,
  aturan_pakai varchar(150) DEFAULT '',
  PRIMARY KEY (no_resep, kode_brng)
);

CREATE TABLE IF NOT EXISTS detail_pemberian_obat (
  tgl_perawatan date NOT NULL,
  jam time NOT NULL,
  no_rawat varchar(17) NOT NULL,
  kode_brng varchar(15) NOT NULL,
  h_beli double DEFAULT 0,
  biaya_obat double NOT NULL DEFAULT 0,
  jml double NOT NULL,
  embalase double DEFAULT 0,
  tuslah double DEFAULT 0,
  total double NOT NULL DEFAULT 0,
  status enum('Ralan','Ranap') NOT NULL,
  kd_bangsal char(5) NOT NULL,
  no_batch varchar(20) NOT NULL DEFAULT '',
  no_faktur varchar(20) NOT NULL DEFAULT '',
  PRIMARY KEY (tgl_perawatan, jam, no_rawat, kode_brng, no_batch, no_faktur)
);

CREATE TABLE IF NOT EXISTS aturan_pakai (
  tgl_perawatan date NOT NULL,
  jam time NOT NULL,
  no_rawat varchar(17) NOT NULL,
  kode_brng varchar(15) NOT NULL,
  aturan varchar(150) DEFAULT '',
  PRIMARY KEY (tgl_perawatan, jam, no_rawat, kode_brng)
);

-- laboratorium
CREATE TABLE IF NOT EXISTS jns_perawatan_lab (
  kd_jenis_prw varchar(15) NOT NULL PRIMARY KEY,
  nm_perawatan varchar(80) NOT NULL,
  total_byr double NOT NULL DEFAULT 0,
  kategori enum('PK','PA','MB') NOT NULL DEFAULT 'PK',
  status enum('0','1') NOT NULL DEFAULT '1'
);

CREATE TABLE IF NOT EXISTS template_laboratorium (
  kd_jenis_prw varchar(15) NOT NULL,
  id_template int NOT NULL AUTO_INCREMENT PRIMARY KEY,
  Pemeriksaan varchar(200) NOT NULL,
  satuan varchar(20) DEFAULT '',
  nilai_rujukan_ld varchar(30) DEFAULT '',
  nilai_rujukan_pd varchar(30) DEFAULT '',
  urut int DEFAULT 0
);

CREATE TABLE IF NOT EXISTS permintaan_lab (
  noorder varchar(15) NOT NULL PRIMARY KEY,
  no_rawat varchar(17) NOT NULL,
  tgl_permintaan date NOT NULL,
  jam_permintaan time NOT NULL,
  tgl_sampel date DEFAULT NULL,
  jam_sampel time DEFAULT NULL,
  tgl_hasil date DEFAULT NULL,
  jam_hasil time DEFAULT NULL,
  dokter_perujuk varchar(20) NOT NULL,
  status enum('ralan','ranap') NOT NULL,
  informasi_tambahan varchar(60) DEFAULT '',
  diagnosa_klinis varchar(80) DEFAULT ''
);

CREATE TABLE IF NOT EXISTS permintaan_pemeriksaan_lab (
  noorder varchar(15) NOT NULL,
  kd_jenis_prw varchar(15) NOT NULL,
  stts_bayar enum('Sudah','Belum') DEFAULT 'Belum',
  PRIMARY KEY (noorder, kd_jenis_prw)
);

CREATE TABLE IF NOT EXISTS periksa_lab (
  no_rawat varchar(17) NOT NULL,
  nip varchar(20) NOT NULL,
  kd_jenis_prw varchar(15) NOT NULL,
  tgl_periksa date NOT NULL,
  jam time NOT NULL,
  dokter_perujuk varchar(20) NOT NULL,
  biaya double NOT NULL DEFAULT 0,
  kd_dokter varchar(20) NOT NULL,
  status enum('Ralan','Ranap') NOT NULL,
  kategori enum('PK','PA','MB') NOT NULL DEFAULT 'PK',
  PRIMARY KEY (no_rawat, kd_jenis_prw, tgl_periksa, jam)
);

CREATE TABLE IF NOT EXISTS detail_periksa_lab (
  no_rawat varchar(17) NOT NULL,
  kd_jenis_prw varchar(15) NOT NULL,
  tgl_periksa date NOT NULL,
  jam time NOT NULL,
  id_template int NOT NULL,
  nilai varchar(200) NOT NULL,
  nilai_rujukan varchar(30) DEFAULT '',
  keterangan varchar(60) DEFAULT '',
  PRIMARY KEY (no_rawat, kd_jenis_prw, tgl_periksa, jam, id_template)
);

-- kasir / billing (Khanza: billing, nota_jalan, nota_inap)
CREATE TABLE IF NOT EXISTS billing (
  noindex int NOT NULL AUTO_INCREMENT PRIMARY KEY,
  no_rawat varchar(17) NOT NULL,
  tgl_byr date NOT NULL,
  no varchar(50) DEFAULT '',
  nm_perawatan varchar(250) NOT NULL,
  pemisah char(1) DEFAULT '',
  biaya double NOT NULL DEFAULT 0,
  jumlah double NOT NULL DEFAULT 1,
  tambahan double NOT NULL DEFAULT 0,
  totalbiaya double NOT NULL DEFAULT 0,
  status enum('Laborat','Radiologi','Operasi','Obat','Ranap Dokter','Ranap Dokter Paramedis','Ranap Paramedis','Ralan Dokter','Ralan Dokter Paramedis','Ralan Paramedis','Tambahan','Potongan','Administrasi','Kamar','-','Registrasi','Harian','Service','TtlObat','TtlRanap Dokter','TtlRanap Paramedis','TtlRalan Dokter','TtlRalan Paramedis','TtlKamar','Dokter','Perawat','TtlTambahan','Retur Obat','TtlRetur Obat','Resep Pulang','TtlResep Pulang','TtlPotongan','TtlLaborat','TtlOperasi','TtlRadiologi','Tagihan') NOT NULL,
  KEY idx_rawat (no_rawat)
);

CREATE TABLE IF NOT EXISTS nota_jalan (
  no_rawat varchar(17) NOT NULL PRIMARY KEY,
  no_nota varchar(20) NOT NULL,
  tanggal date NOT NULL,
  jam time NOT NULL
);

CREATE TABLE IF NOT EXISTS nota_inap (
  no_rawat varchar(17) NOT NULL PRIMARY KEY,
  no_nota varchar(20) NOT NULL,
  tanggal date NOT NULL,
  jam time NOT NULL,
  Uang_Muka double DEFAULT 0
);

CREATE TABLE IF NOT EXISTS tambahan_biaya (
  no_rawat varchar(17) NOT NULL,
  nama_biaya varchar(60) NOT NULL,
  besar_biaya double NOT NULL,
  PRIMARY KEY (no_rawat, nama_biaya)
);

-- bridging BPJS
CREATE TABLE IF NOT EXISTS bridging_sep (
  no_sep varchar(40) NOT NULL PRIMARY KEY,
  no_rawat varchar(17) NOT NULL,
  tglsep date NOT NULL,
  tglrujukan date DEFAULT NULL,
  no_rujukan varchar(40) DEFAULT '',
  kdppkrujukan varchar(12) DEFAULT '',
  nmppkrujukan varchar(200) DEFAULT '',
  kdppkpelayanan varchar(12) DEFAULT '',
  nmppkpelayanan varchar(200) DEFAULT '',
  jnspelayanan enum('1','2') NOT NULL,
  catatan varchar(100) DEFAULT '',
  diagawal varchar(10) DEFAULT '',
  nmdiagnosaawal varchar(400) DEFAULT '',
  kdpolitujuan varchar(15) DEFAULT '',
  nmpolitujuan varchar(50) DEFAULT '',
  klsrawat enum('1','2','3') DEFAULT '3',
  lakalantas enum('0','1','2','3') DEFAULT '0',
  user varchar(25) DEFAULT '',
  nomr varchar(15) DEFAULT '',
  nama_pasien varchar(100) DEFAULT '',
  tanggal_lahir date DEFAULT NULL,
  peserta varchar(100) DEFAULT '',
  jkel char(1) DEFAULT '',
  no_kartu varchar(25) DEFAULT '',
  tglpulang datetime DEFAULT NULL,
  asal_rujukan enum('1. Faskes 1','2. Faskes 2(RS)') DEFAULT '1. Faskes 1',
  notelep varchar(40) DEFAULT '',
  kddpjp varchar(10) DEFAULT '',
  nmdpdjp varchar(100) DEFAULT '',
  KEY idx_rawat (no_rawat)
);

CREATE TABLE IF NOT EXISTS referensi_mobilejkn_bpjs_taskid (
  no_rawat varchar(17) NOT NULL,
  taskid enum('1','2','3','4','5','6','7','99') NOT NULL,
  waktu datetime NOT NULL,
  PRIMARY KEY (no_rawat, taskid)
);

CREATE TABLE IF NOT EXISTS app_bpjs_log (
  id bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
  waktu datetime NOT NULL,
  layanan varchar(20) NOT NULL,
  method varchar(8) NOT NULL,
  url varchar(300) NOT NULL,
  request text,
  code varchar(10) DEFAULT '',
  message varchar(300) DEFAULT '',
  durasi_ms int DEFAULT 0
);

-- pengguna aplikasi web
CREATE TABLE IF NOT EXISTS app_user (
  id_user varchar(30) NOT NULL PRIMARY KEY,
  nama varchar(60) NOT NULL,
  password_hash varchar(100) NOT NULL,
  roles varchar(200) NOT NULL,
  kd_dokter varchar(20) DEFAULT '',
  aktif enum('0','1') NOT NULL DEFAULT '1'
);

CREATE TABLE IF NOT EXISTS app_session (
  token char(64) NOT NULL PRIMARY KEY,
  id_user varchar(30) NOT NULL,
  expires datetime NOT NULL
);

CREATE TABLE IF NOT EXISTS app_audit (
  id bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
  waktu datetime NOT NULL,
  id_user varchar(30) NOT NULL,
  aksi varchar(60) NOT NULL,
  ref varchar(60) DEFAULT '',
  detail varchar(500) DEFAULT '',
  KEY idx_ref (ref)
);

CREATE TABLE IF NOT EXISTS setting (
  nama_instansi varchar(60) NOT NULL PRIMARY KEY,
  alamat_instansi varchar(150) DEFAULT '',
  kabupaten varchar(30) DEFAULT '',
  propinsi varchar(30) DEFAULT '',
  kontak varchar(50) DEFAULT '',
  kode_ppk varchar(15) DEFAULT '',
  kode_ppkinhealth varchar(15) DEFAULT ''
);


-- ===================================================== petugas (non-dokter)
CREATE TABLE IF NOT EXISTS petugas (
  nip varchar(20) NOT NULL PRIMARY KEY,
  nama varchar(50) NOT NULL,
  jk enum('L','P') DEFAULT 'L',
  jbtn varchar(30) DEFAULT '',
  status enum('0','1') NOT NULL DEFAULT '1'
);

-- ===================================================== radiologi
CREATE TABLE IF NOT EXISTS jns_perawatan_radiologi (
  kd_jenis_prw varchar(15) NOT NULL PRIMARY KEY,
  nm_perawatan varchar(80) NOT NULL,
  bagian_rs double NOT NULL DEFAULT 0,
  bhp double NOT NULL DEFAULT 0,
  tarif_perujuk double NOT NULL DEFAULT 0,
  tarif_tindakan_dokter double NOT NULL DEFAULT 0,
  tarif_tindakan_petugas double NOT NULL DEFAULT 0,
  kso double NOT NULL DEFAULT 0,
  menejemen double NOT NULL DEFAULT 0,
  total_byr double NOT NULL DEFAULT 0,
  kd_pj char(3) NOT NULL DEFAULT '-',
  status enum('0','1') NOT NULL DEFAULT '1',
  kelas varchar(15) NOT NULL DEFAULT '-'
);

CREATE TABLE IF NOT EXISTS permintaan_radiologi (
  noorder varchar(15) NOT NULL PRIMARY KEY,
  no_rawat varchar(17) NOT NULL,
  tgl_permintaan date NOT NULL,
  jam_permintaan time NOT NULL,
  tgl_sampel date DEFAULT NULL,
  jam_sampel time DEFAULT NULL,
  tgl_hasil date DEFAULT NULL,
  jam_hasil time DEFAULT NULL,
  dokter_perujuk varchar(20) NOT NULL,
  status enum('ralan','ranap') NOT NULL,
  informasi_tambahan varchar(60) DEFAULT '',
  diagnosa_klinis varchar(80) DEFAULT '',
  KEY idx_rawat (no_rawat)
);

CREATE TABLE IF NOT EXISTS permintaan_pemeriksaan_radiologi (
  noorder varchar(15) NOT NULL,
  kd_jenis_prw varchar(15) NOT NULL,
  stts_bayar enum('Sudah','Belum') DEFAULT 'Belum',
  PRIMARY KEY (noorder, kd_jenis_prw)
);

CREATE TABLE IF NOT EXISTS periksa_radiologi (
  no_rawat varchar(17) NOT NULL,
  nip varchar(20) NOT NULL,
  kd_jenis_prw varchar(15) NOT NULL,
  tgl_periksa date NOT NULL,
  jam time NOT NULL,
  dokter_perujuk varchar(20) NOT NULL,
  bagian_rs double NOT NULL DEFAULT 0,
  bhp double NOT NULL DEFAULT 0,
  tarif_perujuk double NOT NULL DEFAULT 0,
  tarif_tindakan_dokter double NOT NULL DEFAULT 0,
  tarif_tindakan_petugas double NOT NULL DEFAULT 0,
  kso double DEFAULT 0,
  menejemen double DEFAULT 0,
  biaya double NOT NULL DEFAULT 0,
  kd_dokter varchar(20) NOT NULL,
  status enum('Ranap','Ralan') DEFAULT NULL,
  proyeksi varchar(50) NOT NULL DEFAULT '',
  kV varchar(10) NOT NULL DEFAULT '',
  mAS varchar(10) NOT NULL DEFAULT '',
  FFD varchar(10) NOT NULL DEFAULT '',
  BSF varchar(10) NOT NULL DEFAULT '',
  inak varchar(10) NOT NULL DEFAULT '',
  jml_penyinaran varchar(10) NOT NULL DEFAULT '',
  dosis varchar(20) NOT NULL DEFAULT '',
  PRIMARY KEY (no_rawat, kd_jenis_prw, tgl_periksa, jam)
);

CREATE TABLE IF NOT EXISTS hasil_radiologi (
  no_rawat varchar(17) NOT NULL,
  tgl_periksa date NOT NULL,
  jam time NOT NULL,
  hasil text NOT NULL,
  PRIMARY KEY (no_rawat, tgl_periksa, jam)
);

CREATE TABLE IF NOT EXISTS gambar_radiologi (
  no_rawat varchar(17) NOT NULL,
  tgl_periksa date NOT NULL,
  jam time NOT NULL,
  lokasi_gambar varchar(500) NOT NULL,
  PRIMARY KEY (no_rawat, tgl_periksa, jam, lokasi_gambar)
);

-- penghubung order -> waktu pemeriksaan (Khanza menautkan lewat no_rawat+tgl+jam)
CREATE TABLE IF NOT EXISTS app_radiologi_order (
  noorder varchar(15) NOT NULL PRIMARY KEY,
  tgl_periksa date NOT NULL,
  jam time NOT NULL
);

-- ===================================================== kamar operasi / VK
CREATE TABLE IF NOT EXISTS ruang_ok (
  kd_ruang_ok varchar(3) NOT NULL PRIMARY KEY,
  nm_ruang_ok varchar(50) DEFAULT NULL
);

CREATE TABLE IF NOT EXISTS paket_operasi (
  kode_paket varchar(15) NOT NULL PRIMARY KEY,
  nm_perawatan varchar(80) NOT NULL,
  kategori enum('Kebidanan','Operasi') DEFAULT 'Operasi',
  operator1 double NOT NULL DEFAULT 0,
  operator2 double NOT NULL DEFAULT 0,
  operator3 double NOT NULL DEFAULT 0,
  asisten_operator1 double NOT NULL DEFAULT 0,
  asisten_operator2 double NOT NULL DEFAULT 0,
  asisten_operator3 double NOT NULL DEFAULT 0,
  instrumen double NOT NULL DEFAULT 0,
  dokter_anak double NOT NULL DEFAULT 0,
  perawaat_resusitas double NOT NULL DEFAULT 0,
  dokter_anestesi double NOT NULL DEFAULT 0,
  asisten_anestesi double NOT NULL DEFAULT 0,
  asisten_anestesi2 double NOT NULL DEFAULT 0,
  bidan double NOT NULL DEFAULT 0,
  bidan2 double NOT NULL DEFAULT 0,
  bidan3 double NOT NULL DEFAULT 0,
  perawat_luar double NOT NULL DEFAULT 0,
  sewa_ok double NOT NULL DEFAULT 0,
  alat double NOT NULL DEFAULT 0,
  akomodasi double NOT NULL DEFAULT 0,
  bagian_rs double NOT NULL DEFAULT 0,
  omloop double NOT NULL DEFAULT 0,
  omloop2 double NOT NULL DEFAULT 0,
  omloop3 double NOT NULL DEFAULT 0,
  omloop4 double NOT NULL DEFAULT 0,
  omloop5 double NOT NULL DEFAULT 0,
  sarpras double NOT NULL DEFAULT 0,
  dokter_pjanak double NOT NULL DEFAULT 0,
  dokter_umum double NOT NULL DEFAULT 0,
  kd_pj char(3) DEFAULT '-',
  status enum('0','1') DEFAULT '1',
  kelas varchar(15) DEFAULT '-'
);

CREATE TABLE IF NOT EXISTS booking_operasi (
  no_rawat varchar(17) NOT NULL,
  kode_paket varchar(15) NOT NULL,
  tanggal date NOT NULL,
  jam_mulai time NOT NULL,
  jam_selesai time DEFAULT NULL,
  status enum('Menunggu','Proses Operasi','Selesai') DEFAULT 'Menunggu',
  kd_dokter varchar(20) DEFAULT NULL,
  kd_ruang_ok varchar(3) NOT NULL,
  PRIMARY KEY (no_rawat, kode_paket, tanggal, jam_mulai),
  KEY idx_ruang (kd_ruang_ok, tanggal)
);

CREATE TABLE IF NOT EXISTS operasi (
  no_rawat varchar(17) NOT NULL,
  tgl_operasi datetime NOT NULL,
  jenis_anasthesi varchar(8) NOT NULL DEFAULT '-',
  kategori varchar(30) DEFAULT '-',
  operator1 varchar(20) NOT NULL,
  operator2 varchar(20) NOT NULL DEFAULT '-',
  operator3 varchar(20) NOT NULL DEFAULT '-',
  asisten_operator1 varchar(20) NOT NULL DEFAULT '-',
  asisten_operator2 varchar(20) NOT NULL DEFAULT '-',
  asisten_operator3 varchar(20) DEFAULT '-',
  instrumen varchar(20) DEFAULT '-',
  dokter_anak varchar(20) NOT NULL DEFAULT '-',
  perawaat_resusitas varchar(20) NOT NULL DEFAULT '-',
  dokter_anestesi varchar(20) NOT NULL DEFAULT '-',
  asisten_anestesi varchar(20) NOT NULL DEFAULT '-',
  asisten_anestesi2 varchar(20) DEFAULT '-',
  bidan varchar(20) NOT NULL DEFAULT '-',
  bidan2 varchar(20) DEFAULT '-',
  bidan3 varchar(20) DEFAULT '-',
  perawat_luar varchar(20) NOT NULL DEFAULT '-',
  omloop varchar(20) DEFAULT '-',
  omloop2 varchar(20) DEFAULT '-',
  omloop3 varchar(20) DEFAULT '-',
  omloop4 varchar(20) DEFAULT '-',
  omloop5 varchar(20) DEFAULT '-',
  dokter_pjanak varchar(20) DEFAULT '-',
  dokter_umum varchar(20) DEFAULT '-',
  kode_paket varchar(15) NOT NULL,
  biayaoperator1 double NOT NULL DEFAULT 0,
  biayaoperator2 double NOT NULL DEFAULT 0,
  biayaoperator3 double NOT NULL DEFAULT 0,
  biayaasisten_operator1 double NOT NULL DEFAULT 0,
  biayaasisten_operator2 double NOT NULL DEFAULT 0,
  biayaasisten_operator3 double DEFAULT 0,
  biayainstrumen double DEFAULT 0,
  biayadokter_anak double NOT NULL DEFAULT 0,
  biayaperawaat_resusitas double NOT NULL DEFAULT 0,
  biayadokter_anestesi double NOT NULL DEFAULT 0,
  biayaasisten_anestesi double NOT NULL DEFAULT 0,
  biayaasisten_anestesi2 double DEFAULT 0,
  biayabidan double NOT NULL DEFAULT 0,
  biayabidan2 double DEFAULT 0,
  biayabidan3 double DEFAULT 0,
  biayaperawat_luar double NOT NULL DEFAULT 0,
  biayaalat double NOT NULL DEFAULT 0,
  biayasewaok double NOT NULL DEFAULT 0,
  akomodasi double DEFAULT 0,
  bagian_rs double NOT NULL DEFAULT 0,
  biaya_omloop double DEFAULT 0,
  biaya_omloop2 double DEFAULT 0,
  biaya_omloop3 double DEFAULT 0,
  biaya_omloop4 double DEFAULT 0,
  biaya_omloop5 double DEFAULT 0,
  biayasarpras double DEFAULT 0,
  biaya_dokter_pjanak double DEFAULT 0,
  biaya_dokter_umum double DEFAULT 0,
  status enum('Ranap','Ralan') DEFAULT NULL,
  PRIMARY KEY (no_rawat, tgl_operasi, kode_paket)
);

CREATE TABLE IF NOT EXISTS laporan_operasi (
  no_rawat varchar(17) NOT NULL,
  tanggal datetime NOT NULL,
  diagnosa_preop varchar(100) NOT NULL,
  diagnosa_postop varchar(100) NOT NULL,
  jaringan_dieksekusi varchar(100) NOT NULL DEFAULT '',
  selesaioperasi datetime NOT NULL,
  permintaan_pa enum('Ya','Tidak') NOT NULL DEFAULT 'Tidak',
  nomor_implan varchar(50) NOT NULL DEFAULT '',
  laporan_operasi text NOT NULL,
  PRIMARY KEY (no_rawat, tanggal)
);

CREATE TABLE IF NOT EXISTS obatbhp_ok (
  kd_obat varchar(15) NOT NULL PRIMARY KEY,
  nm_obat varchar(50) NOT NULL,
  kode_sat char(4) NOT NULL,
  hargasatuan double NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS beri_obat_operasi (
  no_rawat varchar(17) NOT NULL,
  tanggal datetime NOT NULL,
  kd_obat varchar(15) NOT NULL,
  hargasatuan double NOT NULL,
  jumlah double NOT NULL,
  KEY idx_rawat (no_rawat)
);


-- ===================================================== SATUSEHAT (nama tabel mengikuti Khanza)
CREATE TABLE IF NOT EXISTS pegawai (
  id int NOT NULL AUTO_INCREMENT PRIMARY KEY,
  nik varchar(20) NOT NULL,
  nama varchar(50) NOT NULL,
  jk enum('Pria','Wanita') NOT NULL DEFAULT 'Pria',
  jbtn varchar(25) NOT NULL DEFAULT '',
  no_ktp varchar(20) NOT NULL DEFAULT '',
  UNIQUE KEY uk_nik (nik)
);

CREATE TABLE IF NOT EXISTS satu_sehat_mapping_lokasi_ralan (
  kd_poli char(5) NOT NULL PRIMARY KEY,
  id_organisasi_satusehat varchar(40) DEFAULT NULL,
  id_lokasi_satusehat varchar(40) DEFAULT NULL,
  longitude varchar(30) NOT NULL DEFAULT '',
  latitude varchar(30) NOT NULL DEFAULT '',
  altittude varchar(30) NOT NULL DEFAULT '',
  UNIQUE KEY uk_lokasi (id_lokasi_satusehat)
);

CREATE TABLE IF NOT EXISTS satu_sehat_mapping_lokasi_ranap (
  kd_kamar varchar(15) NOT NULL PRIMARY KEY,
  id_organisasi_satusehat varchar(40) DEFAULT NULL,
  id_lokasi_satusehat varchar(40) DEFAULT NULL,
  longitude varchar(30) NOT NULL DEFAULT '',
  latitude varchar(30) NOT NULL DEFAULT '',
  altittude varchar(30) NOT NULL DEFAULT '',
  UNIQUE KEY uk_lokasi (id_lokasi_satusehat)
);

CREATE TABLE IF NOT EXISTS satu_sehat_encounter (
  no_rawat varchar(17) NOT NULL PRIMARY KEY,
  id_encounter varchar(40) DEFAULT NULL
);

CREATE TABLE IF NOT EXISTS satu_sehat_condition (
  no_rawat varchar(17) NOT NULL,
  kd_penyakit varchar(15) NOT NULL,
  status enum('Ralan','Ranap') NOT NULL,
  id_condition varchar(40) DEFAULT NULL,
  PRIMARY KEY (no_rawat, kd_penyakit, status)
);

CREATE TABLE IF NOT EXISTS satu_sehat_procedure (
  no_rawat varchar(17) NOT NULL,
  kode varchar(10) NOT NULL,
  status enum('Ralan','Ranap') NOT NULL,
  id_procedure varchar(40) DEFAULT NULL,
  PRIMARY KEY (no_rawat, kode, status)
);

CREATE TABLE IF NOT EXISTS satu_sehat_observationttvnadi (
  no_rawat varchar(17) NOT NULL,
  tgl_perawatan date NOT NULL,
  jam_rawat time NOT NULL,
  status enum('Ralan','Ranap') NOT NULL,
  id_observation varchar(40) DEFAULT NULL,
  PRIMARY KEY (no_rawat, tgl_perawatan, jam_rawat, status)
);

CREATE TABLE IF NOT EXISTS satu_sehat_observationttvrespirasi (
  no_rawat varchar(17) NOT NULL,
  tgl_perawatan date NOT NULL,
  jam_rawat time NOT NULL,
  status enum('Ralan','Ranap') NOT NULL,
  id_observation varchar(40) DEFAULT NULL,
  PRIMARY KEY (no_rawat, tgl_perawatan, jam_rawat, status)
);

CREATE TABLE IF NOT EXISTS satu_sehat_observationttvsuhu (
  no_rawat varchar(17) NOT NULL,
  tgl_perawatan date NOT NULL,
  jam_rawat time NOT NULL,
  status enum('Ralan','Ranap') NOT NULL,
  id_observation varchar(40) DEFAULT NULL,
  PRIMARY KEY (no_rawat, tgl_perawatan, jam_rawat, status)
);

CREATE TABLE IF NOT EXISTS satu_sehat_observationttvtensi (
  no_rawat varchar(17) NOT NULL,
  tgl_perawatan date NOT NULL,
  jam_rawat time NOT NULL,
  status enum('Ralan','Ranap') NOT NULL,
  id_observation varchar(40) DEFAULT NULL,
  PRIMARY KEY (no_rawat, tgl_perawatan, jam_rawat, status)
);

CREATE TABLE IF NOT EXISTS satu_sehat_observationttvtb (
  no_rawat varchar(17) NOT NULL,
  tgl_perawatan date NOT NULL,
  jam_rawat time NOT NULL,
  status enum('Ralan','Ranap') NOT NULL,
  id_observation varchar(40) DEFAULT NULL,
  PRIMARY KEY (no_rawat, tgl_perawatan, jam_rawat, status)
);

CREATE TABLE IF NOT EXISTS satu_sehat_observationttvbb (
  no_rawat varchar(17) NOT NULL,
  tgl_perawatan date NOT NULL,
  jam_rawat time NOT NULL,
  status enum('Ralan','Ranap') NOT NULL,
  id_observation varchar(40) DEFAULT NULL,
  PRIMARY KEY (no_rawat, tgl_perawatan, jam_rawat, status)
);

CREATE TABLE IF NOT EXISTS satu_sehat_observationttvlp (
  no_rawat varchar(17) NOT NULL,
  tgl_perawatan date NOT NULL,
  jam_rawat time NOT NULL,
  status enum('Ralan','Ranap') NOT NULL,
  id_observation varchar(40) DEFAULT NULL,
  PRIMARY KEY (no_rawat, tgl_perawatan, jam_rawat, status)
);

CREATE TABLE IF NOT EXISTS app_satusehat_ihs (
  jenis enum('Patient','Practitioner') NOT NULL,
  nik varchar(20) NOT NULL,
  ihs varchar(40) NOT NULL,
  nama varchar(100) DEFAULT '',
  waktu datetime NOT NULL,
  PRIMARY KEY (jenis, nik)
);

CREATE TABLE IF NOT EXISTS app_satusehat_encounter_status (
  no_rawat varchar(17) NOT NULL PRIMARY KEY,
  status varchar(15) NOT NULL,
  waktu datetime NOT NULL
);

CREATE TABLE IF NOT EXISTS app_satusehat_log (
  id bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
  waktu datetime NOT NULL,
  method varchar(6) NOT NULL,
  url varchar(255) NOT NULL,
  request text,
  code int NOT NULL DEFAULT 0,
  message varchar(500) DEFAULT '',
  durasi_ms int DEFAULT 0,
  KEY idx_waktu (waktu)
);

CREATE TABLE IF NOT EXISTS app_satusehat_error (
  no_rawat varchar(17) NOT NULL PRIMARY KEY,
  pesan varchar(1000) NOT NULL,
  waktu datetime NOT NULL
);


-- ===================================================== logistik farmasi (pengadaan)
CREATE TABLE IF NOT EXISTS datasuplier (
  kode_suplier char(5) NOT NULL PRIMARY KEY,
  nama_suplier varchar(50) NOT NULL,
  alamat varchar(50) DEFAULT '',
  kota varchar(20) DEFAULT '',
  no_telp varchar(13) DEFAULT '',
  nama_bank varchar(30) DEFAULT '',
  rekening varchar(20) DEFAULT ''
);

CREATE TABLE IF NOT EXISTS surat_pemesanan_medis (
  no_pemesanan varchar(20) NOT NULL PRIMARY KEY,
  kode_suplier char(5) NOT NULL,
  nip varchar(20) NOT NULL,
  tanggal date NOT NULL,
  total1 double NOT NULL DEFAULT 0,
  potongan double NOT NULL DEFAULT 0,
  total2 double NOT NULL DEFAULT 0,
  ppn double NOT NULL DEFAULT 0,
  meterai double NOT NULL DEFAULT 0,
  tagihan double NOT NULL DEFAULT 0,
  status enum('Proses Pesan','Sudah Datang') NOT NULL DEFAULT 'Proses Pesan'
);

CREATE TABLE IF NOT EXISTS detail_surat_pemesanan_medis (
  no_pemesanan varchar(20) NOT NULL,
  kode_brng varchar(15) NOT NULL,
  kode_sat char(4) NOT NULL,
  jumlah double NOT NULL,
  h_pesan double NOT NULL,
  subtotal double NOT NULL,
  dis double NOT NULL DEFAULT 0,
  besardis double NOT NULL DEFAULT 0,
  total double NOT NULL,
  jumlah2 double NOT NULL DEFAULT 0,
  PRIMARY KEY (no_pemesanan, kode_brng)
);

CREATE TABLE IF NOT EXISTS pemesanan (
  no_faktur varchar(20) NOT NULL PRIMARY KEY,
  no_order varchar(20) DEFAULT '',
  kode_suplier char(5) NOT NULL,
  nip varchar(20) NOT NULL,
  tgl_pesan date NOT NULL,
  tgl_faktur date NOT NULL,
  tgl_tempo date NOT NULL,
  total1 double NOT NULL DEFAULT 0,
  potongan double NOT NULL DEFAULT 0,
  total2 double NOT NULL DEFAULT 0,
  ppn double NOT NULL DEFAULT 0,
  meterai double NOT NULL DEFAULT 0,
  tagihan double NOT NULL DEFAULT 0,
  kd_bangsal char(5) NOT NULL,
  status enum('Sudah Dibayar','Belum Dibayar','Belum Lunas') NOT NULL DEFAULT 'Belum Dibayar',
  KEY idx_tempo (tgl_tempo)
);

CREATE TABLE IF NOT EXISTS detailpesan (
  no_faktur varchar(20) NOT NULL,
  kode_brng varchar(15) NOT NULL,
  kode_sat char(4) NOT NULL,
  jumlah double NOT NULL,
  h_pesan double NOT NULL,
  subtotal double NOT NULL,
  dis double NOT NULL DEFAULT 0,
  besardis double NOT NULL DEFAULT 0,
  total double NOT NULL,
  no_batch varchar(20) NOT NULL DEFAULT '',
  jumlah2 double NOT NULL DEFAULT 0,
  kadaluarsa date DEFAULT NULL,
  PRIMARY KEY (no_faktur, kode_brng, no_batch)
);

CREATE TABLE IF NOT EXISTS bayar_pemesanan (
  tgl_bayar date NOT NULL,
  no_faktur varchar(20) NOT NULL,
  nip varchar(20) NOT NULL,
  besar_bayar double NOT NULL,
  keterangan varchar(100) DEFAULT '',
  nama_bayar varchar(50) NOT NULL,
  no_bukti varchar(30) NOT NULL DEFAULT '',
  KEY idx_faktur (no_faktur)
);

CREATE TABLE IF NOT EXISTS mutasibarang (
  kode_brng varchar(15) NOT NULL,
  jml double NOT NULL,
  harga double NOT NULL DEFAULT 0,
  kd_bangsaldari char(5) NOT NULL,
  kd_bangsalke char(5) NOT NULL,
  tanggal datetime NOT NULL,
  keterangan varchar(60) DEFAULT '',
  no_batch varchar(20) NOT NULL DEFAULT '',
  no_faktur varchar(20) NOT NULL DEFAULT '',
  KEY idx_tgl (tanggal)
);

CREATE TABLE IF NOT EXISTS opname (
  kode_brng varchar(15) NOT NULL,
  h_beli double NOT NULL DEFAULT 0,
  tanggal date NOT NULL,
  stok double NOT NULL,
  `real` double NOT NULL,
  selisih double NOT NULL,
  nomihilang double NOT NULL DEFAULT 0,
  lebih double NOT NULL DEFAULT 0,
  nomilebih double NOT NULL DEFAULT 0,
  keterangan varchar(60) DEFAULT '',
  kd_bangsal char(5) NOT NULL,
  no_batch varchar(20) NOT NULL DEFAULT '',
  no_faktur varchar(20) NOT NULL DEFAULT '',
  PRIMARY KEY (kode_brng, tanggal, kd_bangsal, no_batch, no_faktur)
);

-- ===================================================== keuangan & akuntansi
CREATE TABLE IF NOT EXISTS rekening (
  kd_rek varchar(15) NOT NULL PRIMARY KEY,
  nm_rek varchar(100) NOT NULL,
  tipe enum('N','M','R') NOT NULL,
  balance enum('D','K') NOT NULL,
  level enum('0','1') NOT NULL DEFAULT '0'
);

CREATE TABLE IF NOT EXISTS jurnal (
  no_jurnal varchar(20) NOT NULL PRIMARY KEY,
  no_bukti varchar(30) DEFAULT '',
  tgl_jurnal date NOT NULL,
  jam_jurnal time NOT NULL,
  jenis enum('U','P') NOT NULL DEFAULT 'U',
  keterangan varchar(350) DEFAULT '',
  KEY idx_tgl (tgl_jurnal),
  KEY idx_bukti (no_bukti)
);

CREATE TABLE IF NOT EXISTS detailjurnal (
  no_jurnal varchar(20) NOT NULL,
  kd_rek varchar(15) NOT NULL,
  debet double NOT NULL DEFAULT 0,
  kredit double NOT NULL DEFAULT 0,
  KEY idx_jurnal (no_jurnal),
  KEY idx_rek (kd_rek)
);

-- pemetaan transaksi -> akun (versi ringkas dari set_akun Khanza)
CREATE TABLE IF NOT EXISTS app_set_akun (
  nama varchar(40) NOT NULL PRIMARY KEY,
  kd_rek varchar(15) NOT NULL,
  keterangan varchar(100) DEFAULT ''
);

CREATE TABLE IF NOT EXISTS piutang_pasien (
  no_rawat varchar(17) NOT NULL PRIMARY KEY,
  tgl_piutang date NOT NULL,
  no_rkm_medis varchar(15) NOT NULL,
  status enum('Lunas','Belum Lunas') NOT NULL DEFAULT 'Belum Lunas',
  totalpiutang double NOT NULL,
  uangmuka double NOT NULL DEFAULT 0,
  sisapiutang double NOT NULL,
  tgltempo date NOT NULL
);

CREATE TABLE IF NOT EXISTS detail_piutang_pasien (
  no_rawat varchar(17) NOT NULL,
  nama_bayar varchar(50) NOT NULL,
  kd_pj char(3) NOT NULL,
  totalpiutang double NOT NULL,
  sisapiutang double NOT NULL,
  tgltempo date NOT NULL,
  PRIMARY KEY (no_rawat, nama_bayar)
);

CREATE TABLE IF NOT EXISTS bayar_piutang (
  tgl_bayar date NOT NULL,
  no_rkm_medis varchar(15) NOT NULL,
  besar_cicilan double NOT NULL,
  catatan varchar(100) DEFAULT '',
  no_rawat varchar(17) NOT NULL,
  kd_rek varchar(15) NOT NULL,
  kd_rek_kontra varchar(15) NOT NULL,
  diskon_piutang double NOT NULL DEFAULT 0,
  kd_rek_diskon_piutang varchar(15) DEFAULT '',
  tidak_terbayar double NOT NULL DEFAULT 0,
  kd_rek_tidak_terbayar varchar(15) DEFAULT '',
  KEY idx_rawat (no_rawat)
);


-- ===================================================== gizi
CREATE TABLE IF NOT EXISTS diet (
  kd_diet varchar(3) NOT NULL PRIMARY KEY,
  nama_diet varchar(50) NOT NULL
);

CREATE TABLE IF NOT EXISTS jam_diet_pasien (
  waktu enum('Pagi','Pagi2','Pagi3','Siang','Siang2','Siang3','Sore','Sore2','Sore3','Malam','Malam2','Malam3') NOT NULL PRIMARY KEY,
  jam char(5) NOT NULL
);

CREATE TABLE IF NOT EXISTS detail_beri_diet (
  no_rawat varchar(17) NOT NULL,
  kd_kamar varchar(15) NOT NULL,
  tanggal date NOT NULL,
  waktu enum('Pagi','Pagi2','Pagi3','Siang','Siang2','Siang3','Sore','Sore2','Sore3','Malam','Malam2','Malam3') NOT NULL,
  kd_diet varchar(3) NOT NULL,
  PRIMARY KEY (no_rawat, kd_kamar, tanggal, waktu, kd_diet),
  KEY idx_tgl (tanggal, waktu)
);
