// SIMRS Go — Sistem Informasi Manajemen Rumah Sakit dengan ERM,
// proses bisnis dan struktur data mengikuti SIMKES Khanza.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/yogaprasetia/simrs-go/internal/bpjs"
	"github.com/yogaprasetia/simrs-go/internal/db"
	"github.com/yogaprasetia/simrs-go/internal/web"
)

// loadEnv membaca file KEY=VALUE (tanpa menimpa env yang sudah ada).
func loadEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") || !strings.Contains(l, "=") {
			continue
		}
		kv := strings.SplitN(l, "=", 2)
		k, v := strings.TrimSpace(kv[0]), strings.Trim(strings.TrimSpace(kv[1]), `"'`)
		if _, ok := os.LookupEnv(k); !ok {
			os.Setenv(k, v)
		}
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	envFile := flag.String("env", "simrs.env", "file konfigurasi KEY=VALUE")
	migrate := flag.Bool("migrate", false, "buat tabel + data master contoh lalu keluar")
	seed := flag.Bool("seed", true, "isi data master contoh saat migrate")
	setPw := flag.String("set-password", "", "set password user (password dibaca dari stdin), contoh: -set-password admin")
	flag.Parse()
	loadEnv(*envFile)

	dsn := env("DB_DSN", "")
	if dsn == "" {
		dsn = fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8mb4&parseTime=false&loc=Local",
			env("DB_USER", "simrs"), env("DB_PASS", ""), env("DB_HOST", "127.0.0.1:3306"), env("DB_NAME", "sik"))
	}
	d, err := db.Open(dsn)
	if err != nil {
		log.Fatalf("koneksi database gagal: %v", err)
	}
	ctx := context.Background()
	if err := db.Migrate(ctx, d, *migrate && *seed); err != nil {
		log.Fatal(err)
	}
	if *migrate {
		log.Println("migrasi selesai")
	}
	if *setPw != "" {
		fmt.Fprint(os.Stderr, "Password baru: ")
		pw, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		pw = strings.TrimSpace(pw)
		if len(pw) < 8 {
			log.Fatal("password minimal 8 karakter")
		}
		var roles, nama string
		_ = d.QueryRow("SELECT roles, nama FROM app_user WHERE id_user=?", *setPw).Scan(&roles, &nama)
		if roles == "" {
			roles, nama = "admin", *setPw
		}
		if err := db.UpsertUser(ctx, d, *setPw, nama, pw, roles, ""); err != nil {
			log.Fatal(err)
		}
		log.Printf("password %s diperbarui", *setPw)
		return
	}
	if pw := os.Getenv("ADMIN_PASSWORD"); pw != "" {
		if created, err := db.EnsureAdmin(ctx, d, pw); err != nil {
			log.Fatal(err)
		} else if created {
			log.Println("user admin dibuat dari ADMIN_PASSWORD")
		}
	}
	if *migrate {
		return
	}

	cfg := bpjs.Config{ConsID: env("BPJS_CONS_ID", ""), Secret: env("BPJS_SECRET", ""), UserKeyVClaim: env("BPJS_USER_KEY_VCLAIM", ""),
		UserKeyAntrean: env("BPJS_USER_KEY_ANTREAN", ""), KodePPK: env("BPJS_KODE_PPK", ""), NamaPPK: env("BPJS_NAMA_PPK", ""), Mode: env("BPJS_MODE", "off")}
	switch cfg.Mode {
	case "dev":
		cfg.BaseVClaim, cfg.BaseAntrean = env("BPJS_VCLAIM_URL", bpjs.DevVClaim), env("BPJS_ANTREAN_URL", bpjs.DevAntrean)
	case "prod":
		cfg.BaseVClaim, cfg.BaseAntrean = env("BPJS_VCLAIM_URL", bpjs.ProdVClaim), env("BPJS_ANTREAN_URL", bpjs.ProdAntrean)
	case "mock":
		m := bpjs.NewMock("mock", "mock-secret")
		vc, an, err := m.Start()
		if err != nil {
			log.Fatal(err)
		}
		cfg.ConsID, cfg.Secret, cfg.UserKeyVClaim, cfg.UserKeyAntrean = "mock", "mock-secret", "mock", "mock"
		cfg.BaseVClaim, cfg.BaseAntrean = vc, an
		if cfg.KodePPK == "" {
			cfg.KodePPK, cfg.NamaPPK = "0101R001", "RS CONTOH"
		}
		log.Printf("BPJS mode mock: server tiruan di %s", vc)
	}
	if (cfg.Mode == "dev" || cfg.Mode == "prod") && (cfg.ConsID == "" || cfg.Secret == "") {
		log.Fatal("BPJS_MODE dev/prod membutuhkan BPJS_CONS_ID dan BPJS_SECRET")
	}

	secret := []byte(env("SESSION_SECRET", ""))
	if len(secret) == 0 {
		b := make([]byte, 32)
		rand.Read(b)
		secret = []byte(hex.EncodeToString(b))
	}
	srv, err := web.New(d, bpjs.New(cfg, d), secret)
	if err != nil {
		log.Fatal(err)
	}
	srv.Prefix = strings.TrimRight(env("BASE_PATH", ""), "/")
	addr := env("LISTEN", "127.0.0.1:8090")
	hs := &http.Server{Addr: addr, Handler: srv, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 90 * time.Second}
	go func() {
		log.Printf("SIMRS berjalan di http://%s%s/", addr, srv.Prefix)
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hs.Shutdown(c)
}
