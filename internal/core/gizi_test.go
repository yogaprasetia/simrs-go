package core_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/yogaprasetia/simrs-go/internal/core"
)

func TestGizi(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	setNow("2026-11-23 07:00:00")
	defer func() { core.Now = func() time.Time { return time.Now().In(core.Loc) } }()
	p := &core.Pasien{Nama: "Rawat", JK: "L", TglLahir: "1970-01-01"}
	var r core.RegResult
	tx(t, d, func(x *sql.Tx) (err error) {
		core.SimpanPasien(ctx, x, p)
		r, err = core.Registrasi(ctx, x, core.RegInput{NoRM: p.NoRM, KdPoli: "IGDK", KdDokter: "D0000001", KdPJ: "A09"})
		return
	})
	if err := txErr(d, func(x *sql.Tx) error { return core.SetDiet(ctx, x, r.NoRawat, "2026-11-23", "Pagi", "D03") }); err == nil {
		t.Fatal("pasien belum ranap seharusnya ditolak")
	}
	tx(t, d, func(x *sql.Tx) error { return core.MasukKamar(ctx, x, r.NoRawat, "MLT.01", "DM", "D0000002") })
	if err := txErr(d, func(x *sql.Tx) error { return core.SetDiet(ctx, x, r.NoRawat, "2026-11-23", "Subuh", "D03") }); err == nil {
		t.Fatal("waktu tak dikenal")
	}
	tx(t, d, func(x *sql.Tx) error { return core.SetDiet(ctx, x, r.NoRawat, "2026-11-23", "Pagi", "D01") })
	tx(t, d, func(x *sql.Tx) error { return core.SetDiet(ctx, x, r.NoRawat, "2026-11-23", "Pagi", "D03") }) // ganti
	tx(t, d, func(x *sql.Tx) error { return core.SetDiet(ctx, x, r.NoRawat, "2026-11-23", "Siang", "D03") })
	if n := q1[int](t, d, "SELECT COUNT(*) FROM detail_beri_diet WHERE no_rawat=?", r.NoRawat); n != 2 {
		t.Fatalf("diet %d", n)
	}
	// pindah kamar keesokan hari -> salinan diet memakai kamar baru
	setNow("2026-11-24 08:00:00")
	tx(t, d, func(x *sql.Tx) error { return core.PindahKamar(ctx, x, r.NoRawat, "MWR.01") })
	var n int
	tx(t, d, func(x *sql.Tx) (err error) { n, err = core.SalinDiet(ctx, x, "2026-11-23", "2026-11-24"); return })
	if n != 2 || q1[string](t, d, "SELECT DISTINCT kd_kamar FROM detail_beri_diet WHERE tanggal='2026-11-24'") != "MWR.01" {
		t.Fatalf("salin %d", n)
	}
	tx(t, d, func(x *sql.Tx) (err error) { n, err = core.SalinDiet(ctx, x, "2026-11-23", "2026-11-24"); return })
	if n != 0 {
		t.Fatal("salin ulang tidak boleh menggandakan")
	}
	setNow("2026-11-25 10:00:00")
	tx(t, d, func(x *sql.Tx) error { return core.PulangRanap(ctx, x, r.NoRawat, "Membaik", "") })
	if err := txErr(d, func(x *sql.Tx) error { return core.SetDiet(ctx, x, r.NoRawat, "2026-11-26", "Pagi", "D03") }); err == nil {
		t.Fatal("setelah pulang seharusnya ditolak")
	}
	tx(t, d, func(x *sql.Tx) error { return core.SetDiet(ctx, x, r.NoRawat, "2026-11-25", "Pagi", "D03") }) // hari pulang masih dapat makan pagi
}
