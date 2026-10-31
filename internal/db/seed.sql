-- Data master contoh (INSERT IGNORE agar aman dijalankan ulang)
INSERT IGNORE INTO setting VALUES ('RS Sehat Sentosa','Jl. Merdeka No. 1','Kota Contoh','Jawa Tengah','(0271) 123456','0001R001','');

INSERT IGNORE INTO penjab (kd_pj,png_jawab) VALUES
('A09','UMUM'),('BPJ','BPJS KESEHATAN'),('A92','ASURANSI SWASTA'),('A65','PERUSAHAAN');

INSERT IGNORE INTO spesialis VALUES
('UMUM','Dokter Umum'),('S0001','Penyakit Dalam'),('S0002','Anak'),('S0003','Obstetri & Ginekologi'),('S0004','Gigi'),('S0005','Bedah');

INSERT IGNORE INTO dokter (kd_dokter,nm_dokter,jk,kd_sps,no_ijn_praktek,kd_dokter_bpjs) VALUES
('D0000001','dr. Andi Pratama','L','UMUM','SIP/001/2025','10001'),
('D0000002','dr. Sari Wulandari, Sp.PD','P','S0001','SIP/002/2025','10002'),
('D0000003','dr. Budi Santoso, Sp.A','L','S0002','SIP/003/2025','10003'),
('D0000004','dr. Rina Kartika, Sp.OG','P','S0003','SIP/004/2025','10004'),
('D0000005','drg. Dewi Lestari','P','S0004','SIP/005/2025','10005'),
('D0000006','dr. Hendra Gunawan, Sp.B','L','S0005','SIP/006/2025','10006');

INSERT IGNORE INTO poliklinik (kd_poli,nm_poli,registrasi,registrasilama,kd_poli_bpjs) VALUES
('U0001','Poli Umum',15000,10000,'UMU'),
('IGDK','IGD',25000,25000,'IGD'),
('INT','Poli Penyakit Dalam',30000,25000,'INT'),
('ANA','Poli Anak',30000,25000,'ANA'),
('OBG','Poli Kandungan',30000,25000,'OBG'),
('GIG','Poli Gigi',25000,20000,'GIG'),
('BED','Poli Bedah',30000,25000,'BED');

INSERT IGNORE INTO jadwal VALUES
('D0000001','SENIN','07:00','21:00','U0001',40),('D0000001','SELASA','07:00','21:00','U0001',40),('D0000001','RABU','07:00','21:00','U0001',40),
('D0000001','KAMIS','07:00','21:00','U0001',40),('D0000001','JUMAT','07:00','21:00','U0001',40),('D0000001','SABTU','07:00','14:00','U0001',30),
('D0000002','SENIN','08:00','14:00','INT',25),('D0000002','RABU','08:00','14:00','INT',25),('D0000002','JUMAT','08:00','14:00','INT',25),
('D0000003','SELASA','08:00','14:00','ANA',25),('D0000003','KAMIS','08:00','14:00','ANA',25),('D0000003','SABTU','08:00','12:00','ANA',20),
('D0000004','SENIN','13:00','18:00','OBG',20),('D0000004','KAMIS','13:00','18:00','OBG',20),
('D0000005','SELASA','08:00','13:00','GIG',15),('D0000005','JUMAT','08:00','11:00','GIG',15),
('D0000006','RABU','13:00','17:00','BED',15),('D0000006','SABTU','09:00','12:00','BED',15);

INSERT IGNORE INTO jns_perawatan (kd_jenis_prw,nm_perawatan,total_byrdr,kd_poli) VALUES
('RJ001','Konsultasi Dokter Umum',35000,'U0001'),
('RJ002','Konsultasi Dokter Spesialis',75000,'-'),
('RJ003','Nebulizer',60000,'-'),
('RJ004','Hecting / Jahit Luka (per jahitan)',25000,'-'),
('RJ005','Perawatan Luka',40000,'-'),
('RJ006','Pemasangan Infus',75000,'IGDK'),
('RJ007','EKG',85000,'-'),
('RJ008','Pemeriksaan Gula Darah Stik',25000,'-'),
('RJ009','Cabut Gigi Permanen',150000,'GIG'),
('RJ010','Tambal Gigi Komposit',200000,'GIG'),
('RJ011','USG Kandungan',150000,'OBG'),
('RJ012','Observasi IGD',50000,'IGDK');

INSERT IGNORE INTO jns_perawatan_inap (kd_jenis_prw,nm_perawatan,total_byrdr,kelas) VALUES
('RI001','Visite Dokter Umum',50000,'-'),
('RI002','Visite Dokter Spesialis',100000,'-'),
('RI003','Asuhan Keperawatan per Hari',75000,'-'),
('RI004','Pemasangan Kateter',100000,'-'),
('RI005','Ganti Balutan',50000,'-'),
('RI006','Transfusi Darah (per kantong)',150000,'-'),
('RI007','Oksigen per Jam',15000,'-');

INSERT IGNORE INTO bangsal (kd_bangsal,nm_bangsal) VALUES
('AP','Apotek'),('MLT','Melati'),('MWR','Mawar'),('ANG','Anggrek'),('ICU','ICU');

INSERT IGNORE INTO kamar (kd_kamar,kd_bangsal,trf_kamar,status,kelas) VALUES
('MLT.01','MLT',150000,'KOSONG','Kelas 3'),('MLT.02','MLT',150000,'KOSONG','Kelas 3'),('MLT.03','MLT',150000,'KOSONG','Kelas 3'),('MLT.04','MLT',150000,'KOSONG','Kelas 3'),
('MWR.01','MWR',250000,'KOSONG','Kelas 2'),('MWR.02','MWR',250000,'KOSONG','Kelas 2'),('MWR.03','MWR',250000,'KOSONG','Kelas 2'),
('ANG.01','ANG',400000,'KOSONG','Kelas 1'),('ANG.02','ANG',400000,'KOSONG','Kelas 1'),
('ANG.V1','ANG',750000,'KOSONG','Kelas VIP'),
('ICU.01','ICU',1200000,'KOSONG','Kelas Utama'),('ICU.02','ICU',1200000,'KOSONG','Kelas Utama');

INSERT IGNORE INTO kodesatuan VALUES ('TAB','Tablet'),('KAP','Kapsul'),('BTL','Botol'),('AMP','Ampul'),('VIAL','Vial'),('TUB','Tube'),('SAC','Sachet'),('FLS','Flash');

INSERT IGNORE INTO databarang (kode_brng,nama_brng,kode_sat,h_beli,ralan,ranap,stokminimal) VALUES
('B000001','Paracetamol 500 mg','TAB',150,500,600,200),
('B000002','Amoxicillin 500 mg','KAP',350,1000,1200,200),
('B000003','Ibuprofen 400 mg','TAB',250,800,900,100),
('B000004','Antasida Doen','TAB',100,400,500,100),
('B000005','Omeprazole 20 mg','KAP',400,1500,1800,100),
('B000006','Amlodipine 5 mg','TAB',300,1000,1200,100),
('B000007','Metformin 500 mg','TAB',200,700,800,100),
('B000008','Cetirizine 10 mg','TAB',200,700,800,100),
('B000009','OBH Sirup 100 ml','BTL',6000,15000,17000,20),
('B000010','Salbutamol 2 mg','TAB',150,500,600,100),
('B000011','Dexamethasone 0.5 mg','TAB',100,400,500,100),
('B000012','Ranitidine Inj 50 mg','AMP',3000,9000,10000,20),
('B000013','Ringer Laktat 500 ml','FLS',8000,20000,22000,30),
('B000014','Ceftriaxone Inj 1 g','VIAL',12000,35000,40000,20),
('B000015','Oralit','SAC',500,1500,1800,50),
('B000016','Captopril 25 mg','TAB',150,500,600,100),
('B000017','Asam Mefenamat 500 mg','TAB',250,800,900,100),
('B000018','Salep Gentamicin 0.1%','TUB',4000,12000,13000,10),
('B000019','Vitamin B Complex','TAB',100,300,400,100),
('B000020','Ondansetron Inj 4 mg','AMP',6000,18000,20000,20);

INSERT IGNORE INTO gudangbarang (kode_brng,kd_bangsal,stok) SELECT kode_brng,'AP',
  CASE WHEN kode_sat IN ('BTL','FLS','VIAL','AMP','TUB') THEN 80 ELSE 1000 END FROM databarang;

INSERT IGNORE INTO jns_perawatan_lab (kd_jenis_prw,nm_perawatan,total_byr) VALUES
('LAB001','Darah Lengkap',85000),('LAB002','Gula Darah Sewaktu',30000),('LAB003','Kolesterol Total',45000),
('LAB004','Asam Urat',35000),('LAB005','Urine Lengkap',50000),('LAB006','Fungsi Ginjal (Ureum, Kreatinin)',90000),
('LAB007','Widal',60000),('LAB008','HBsAg',75000);

INSERT IGNORE INTO template_laboratorium (id_template,kd_jenis_prw,Pemeriksaan,satuan,nilai_rujukan_ld,nilai_rujukan_pd,urut) VALUES
(1,'LAB001','Hemoglobin','g/dL','13.0-17.0','12.0-15.0',1),
(2,'LAB001','Leukosit','10^3/uL','4.0-10.0','4.0-10.0',2),
(3,'LAB001','Hematokrit','%','40-50','36-46',3),
(4,'LAB001','Trombosit','10^3/uL','150-400','150-400',4),
(5,'LAB001','Eritrosit','10^6/uL','4.5-5.5','4.0-5.0',5),
(6,'LAB002','Glukosa Sewaktu','mg/dL','70-200','70-200',1),
(7,'LAB003','Kolesterol Total','mg/dL','<200','<200',1),
(8,'LAB004','Asam Urat','mg/dL','3.4-7.0','2.4-5.7',1),
(9,'LAB005','Warna','','Kuning','Kuning',1),
(10,'LAB005','Protein','','Negatif','Negatif',2),
(11,'LAB005','Reduksi','','Negatif','Negatif',3),
(12,'LAB005','Leukosit Sedimen','/LPB','0-5','0-5',4),
(13,'LAB006','Ureum','mg/dL','10-50','10-50',1),
(14,'LAB006','Kreatinin','mg/dL','0.7-1.3','0.6-1.1',2),
(15,'LAB007','S. Typhi O','','<1/80','<1/80',1),
(16,'LAB007','S. Typhi H','','<1/80','<1/80',2),
(17,'LAB008','HBsAg','','Non Reaktif','Non Reaktif',1);

INSERT IGNORE INTO penyakit VALUES
('A01.0','Typhoid fever'),('A09','Diarrhoea and gastroenteritis of presumed infectious origin'),('A15.0','Tuberculosis of lung'),
('A90','Dengue fever [classical dengue]'),('A91','Dengue haemorrhagic fever'),('B01.9','Varicella without complication'),
('B35.4','Tinea corporis'),('E10.9','Type 1 diabetes mellitus without complications'),('E11.9','Type 2 diabetes mellitus without complications'),
('E78.5','Hyperlipidaemia, unspecified'),('E79.0','Hyperuricaemia without signs of inflammatory arthritis'),('G43.9','Migraine, unspecified'),
('G44.2','Tension-type headache'),('H10.9','Conjunctivitis, unspecified'),('H66.9','Otitis media, unspecified'),
('I10','Essential (primary) hypertension'),('I11.9','Hypertensive heart disease without heart failure'),('I20.9','Angina pectoris, unspecified'),
('I50.0','Congestive heart failure'),('I63.9','Cerebral infarction, unspecified'),('J00','Acute nasopharyngitis [common cold]'),
('J02.9','Acute pharyngitis, unspecified'),('J03.9','Acute tonsillitis, unspecified'),('J06.9','Acute upper respiratory infection, unspecified'),
('J18.9','Pneumonia, unspecified'),('J45.9','Asthma, unspecified'),('K02.9','Dental caries, unspecified'),('K04.0','Pulpitis'),
('K21.9','Gastro-oesophageal reflux disease without oesophagitis'),('K29.7','Gastritis, unspecified'),('K30','Dyspepsia'),
('K35.8','Acute appendicitis, other and unspecified'),('K59.0','Constipation'),('L02.9','Cutaneous abscess, unspecified'),
('L23.9','Allergic contact dermatitis, unspecified cause'),('L30.9','Dermatitis, unspecified'),('M10.9','Gout, unspecified'),
('M54.5','Low back pain'),('M79.1','Myalgia'),('N39.0','Urinary tract infection, site not specified'),
('O80.0','Spontaneous vertex delivery'),('O21.0','Mild hyperemesis gravidarum'),('R05','Cough'),('R10.4','Other and unspecified abdominal pain'),
('R50.9','Fever, unspecified'),('R51','Headache'),('S01.8','Open wound of other parts of head'),('S61.9','Open wound of wrist and hand part, unspecified'),
('S93.4','Sprain and strain of ankle'),('T14.1','Open wound of unspecified body region'),('Z00.0','General medical examination'),
('Z34.9','Supervision of normal pregnancy, unspecified');

INSERT IGNORE INTO icd9 VALUES
('89.03','Interview and evaluation, described as comprehensive'),('89.52','Electrocardiogram'),('93.94','Respiratory medication administered by nebulizer'),
('86.59','Closure of skin and subcutaneous tissue of other sites'),('86.28','Nonexcisional debridement of wound, infection or burn'),
('38.93','Venous catheterization, not elsewhere classified'),('99.04','Transfusion of packed cells'),('57.94','Insertion of indwelling urinary catheter'),
('23.09','Extraction of other tooth'),('23.2','Restoration of tooth by filling'),('88.78','Diagnostic ultrasound of gravid uterus'),
('47.09','Other appendectomy'),('73.59','Other manually assisted delivery'),('96.04','Insertion of endotracheal tube'),('93.96','Other oxygen enrichment');
