package bpjs

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

type vec struct {
	Cases                             []struct{ Plain, Comp string }
	Enc, Sig, Cons, Sec, Ts, EncPlain string
}

func load(t *testing.T) vec {
	b, err := os.ReadFile("testdata.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vec
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestLZStringMatchesJS(t *testing.T) {
	v := load(t)
	for i, c := range v.Cases {
		if got := CompressToEncodedURIComponent(c.Plain); got != c.Comp {
			t.Errorf("compress #%d mismatch:\n got %q\nwant %q", i, trunc(got), trunc(c.Comp))
		}
		got, err := DecompressFromEncodedURIComponent(c.Comp)
		if err != nil || got != c.Plain {
			t.Errorf("decompress #%d mismatch err=%v got %q", i, err, trunc(got))
		}
	}
}

func trunc(s string) string {
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

func TestSignatureAndDecryptMatchNode(t *testing.T) {
	v := load(t)
	if s := Signature(v.Cons, v.Sec, v.Ts); s != v.Sig {
		t.Fatalf("signature %s != %s", s, v.Sig)
	}
	plain, err := Decrypt(v.Cons, v.Sec, v.Ts, v.Enc)
	if err != nil || plain != v.EncPlain {
		t.Fatalf("decrypt: %v %q", err, plain)
	}
	if Encrypt(v.Cons, v.Sec, v.Ts, v.EncPlain) != v.Enc {
		t.Fatal("encrypt tidak sama dengan node")
	}
	if _, err := Decrypt(v.Cons, "salah", v.Ts, v.Enc); err == nil {
		t.Fatal("decrypt dengan secret salah seharusnya gagal")
	}
}

func TestClientAgainstMock(t *testing.T) {
	m := NewMock("cons1", "sec1")
	vc, an, err := m.Start()
	if err != nil {
		t.Fatal(err)
	}
	c := New(Config{ConsID: "cons1", Secret: "sec1", UserKeyVClaim: "uk", UserKeyAntrean: "uk2", BaseVClaim: vc, BaseAntrean: an, KodePPK: "0101R001", Mode: "mock"}, nil)
	ctx := context.Background()
	p, err := c.PesertaByKartu(ctx, "0001234567890", "2026-10-31")
	if err != nil || p["nama"] != "BUDI HARTONO" {
		t.Fatalf("peserta: %v %v", err, p)
	}
	if _, err := c.PesertaByKartu(ctx, "0000000000000", "2026-10-31"); err == nil {
		t.Fatal("peserta tak dikenal seharusnya error")
	}
	if p2, err := c.PesertaByNIK(ctx, "3372014505900002", "2026-10-31"); err != nil || p2["noKartu"] != "0001234567891" {
		t.Fatalf("nik: %v %v", err, p2)
	}
	ruj, err := c.Rujukan(ctx, "0101G0011025P000890", 1)
	if err != nil || ruj["noKunjungan"] == nil {
		t.Fatalf("rujukan: %v", err)
	}
	sep, err := c.InsertSEP(ctx, SEPRequest{NoKartu: "0001234567890", TglSep: "2026-10-31", JnsPelayanan: "2", KlsRawatHak: "3", NoMR: "000001", AsalRujukan: "1", TglRujukan: "2026-10-28", NoRujukan: "X", PPKRujukan: "0101G001", DiagAwal: "I10", PoliTujuan: "INT", KodeDPJP: "10002", DPJPLayan: "10002", User: "tes"})
	if err != nil || sep["noSep"] == "" {
		t.Fatalf("sep: %v", err)
	}
	if _, err := c.InsertSEP(ctx, SEPRequest{NoKartu: "0009999999999", DiagAwal: "I10"}); err == nil {
		t.Fatal("peserta nonaktif seharusnya ditolak")
	} else if e, ok := err.(*Error); !ok || e.Code != "201" {
		t.Fatalf("error type: %v", err)
	}
	if err := c.HapusSEP(ctx, sep["noSep"].(string), "tes"); err != nil {
		t.Fatal(err)
	}
	if err := c.TambahAntrean(ctx, AntreanAdd{KodeBooking: "2026/10/31/000001", JenisPasien: "JKN"}); err != nil {
		t.Fatal(err)
	}
	if err := c.TambahAntrean(ctx, AntreanAdd{KodeBooking: "2026/10/31/000001"}); err == nil {
		t.Fatal("duplikat kode booking seharusnya ditolak")
	}
	if err := c.UpdateWaktu(ctx, "2026/10/31/000001", 4, time.Now()); err != nil {
		t.Fatal(err)
	}
	if m.Antrean("2026/10/31/000001")["tasks"].(map[string]int64)["4"] == 0 {
		t.Fatal("task 4 tidak tercatat di server tiruan")
	}
	// signature salah
	bad := New(Config{ConsID: "cons1", Secret: "salah", UserKeyVClaim: "uk", BaseVClaim: vc, Mode: "mock"}, nil)
	if _, err := bad.PesertaByKartu(ctx, "0001234567890", "2026-10-31"); err == nil {
		t.Fatal("signature salah seharusnya ditolak")
	}
}
