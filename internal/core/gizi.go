package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ================================================================ GIZI
// Alur Khanza: ahli gizi menetapkan diet per pasien rawat inap per tanggal & waktu makan
// (detail_beri_diet, kamar = kamar pasien pada tanggal itu). Dapur memakai rekap per waktu & bangsal.

// kamarPada mengembalikan kamar pasien pada tanggal tertentu ("" bila tidak sedang dirawat).
func kamarPada(ctx context.Context, q Q, noRawat, tgl string) string {
	kd, _ := one[string](ctx, q, `SELECT kd_kamar FROM kamar_inap WHERE no_rawat=? AND tgl_masuk<=?
		AND (stts_pulang='-' OR tgl_keluar>=?) ORDER BY tgl_masuk DESC, jam_masuk DESC LIMIT 1`, noRawat, tgl, tgl)
	return kd
}

func cekWaktu(ctx context.Context, q Q, waktu string) error {
	if n, _ := one[int](ctx, q, "SELECT COUNT(*) FROM jam_diet_pasien WHERE waktu=?", waktu); n == 0 {
		return fmt.Errorf("waktu makan %q tidak terdaftar", waktu)
	}
	return nil
}

// SetDiet mengganti diet pasien untuk satu tanggal & waktu (satu diet per waktu).
func SetDiet(ctx context.Context, tx *sql.Tx, noRawat, tgl, waktu, kdDiet string) error {
	if _, err := time.Parse("2006-01-02", tgl); err != nil {
		return errors.New("tanggal tidak valid")
	}
	if err := cekWaktu(ctx, tx, waktu); err != nil {
		return err
	}
	if n, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM diet WHERE kd_diet=?", kdDiet); n == 0 {
		return errors.New("jenis diet tidak ditemukan")
	}
	kamar := kamarPada(ctx, tx, noRawat, tgl)
	if kamar == "" {
		return errors.New("pasien tidak sedang dirawat inap pada tanggal tersebut")
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM detail_beri_diet WHERE no_rawat=? AND tanggal=? AND waktu=?", noRawat, tgl, waktu); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO detail_beri_diet VALUES (?,?,?,?,?)", noRawat, kamar, tgl, waktu, kdDiet)
	return err
}

func HapusDiet(ctx context.Context, tx *sql.Tx, noRawat, tgl, waktu string) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM detail_beri_diet WHERE no_rawat=? AND tanggal=? AND waktu=?", noRawat, tgl, waktu)
	return err
}

// SalinDiet menyalin diet dari tanggal 'dari' ke 'ke' untuk pasien yang masih dirawat pada 'ke'
// dan belum punya diet di waktu tersebut. Mengembalikan jumlah baris yang dibuat.
func SalinDiet(ctx context.Context, tx *sql.Tx, dari, ke string) (int, error) {
	rows, err := tx.QueryContext(ctx, "SELECT no_rawat, waktu, kd_diet FROM detail_beri_diet WHERE tanggal=?", dari)
	if err != nil {
		return 0, err
	}
	type r struct{ no, w, d string }
	var list []r
	for rows.Next() {
		var x r
		rows.Scan(&x.no, &x.w, &x.d)
		list = append(list, x)
	}
	rows.Close()
	n := 0
	for _, x := range list {
		kamar := kamarPada(ctx, tx, x.no, ke)
		if kamar == "" {
			continue
		}
		if c, _ := one[int](ctx, tx, "SELECT COUNT(*) FROM detail_beri_diet WHERE no_rawat=? AND tanggal=? AND waktu=?", x.no, ke, x.w); c > 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO detail_beri_diet VALUES (?,?,?,?,?)", x.no, kamar, ke, x.w, x.d); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
