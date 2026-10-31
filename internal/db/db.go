package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

//go:embed schema.sql
var schemaSQL string

//go:embed seed.sql
var seedSQL string

func Open(dsn string) (*sql.DB, error) {
	d, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(20)
	d.SetMaxIdleConns(10)
	d.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := d.PingContext(ctx); err != nil {
		return nil, err
	}
	return d, nil
}

func statements(s string) []string {
	var out []string
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "--") || t == "" {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
		if strings.HasSuffix(t, ";") {
			out = append(out, strings.TrimSuffix(strings.TrimSpace(b.String()), ";"))
			b.Reset()
		}
	}
	if strings.TrimSpace(b.String()) != "" {
		out = append(out, b.String())
	}
	return out
}

// Migrate membuat tabel (idempoten) lalu mengisi master data contoh.
func Migrate(ctx context.Context, d *sql.DB, seed bool) error {
	for _, st := range statements(schemaSQL) {
		if _, err := d.ExecContext(ctx, st); err != nil {
			return fmt.Errorf("schema: %w\n%s", err, st)
		}
	}
	if !seed {
		return nil
	}
	for _, st := range statements(seedSQL) {
		if _, err := d.ExecContext(ctx, st); err != nil {
			return fmt.Errorf("seed: %w\n%s", err, st)
		}
	}
	return nil
}

// Role yang tersedia
var Roles = []string{"admin", "pendaftaran", "dokter", "perawat", "farmasi", "lab", "kasir", "bpjs", "manajemen", "radiologi", "ok", "keuangan", "gizi"}

// EnsureAdmin membuat user admin bila belum ada user sama sekali.
func EnsureAdmin(ctx context.Context, d *sql.DB, password string) (bool, error) {
	var n int
	if err := d.QueryRowContext(ctx, "SELECT COUNT(*) FROM app_user").Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	return true, UpsertUser(ctx, d, "admin", "Administrator", password, "admin", "")
}

func UpsertUser(ctx context.Context, d *sql.DB, id, nama, password, roles, kdDokter string) error {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = d.ExecContext(ctx, `INSERT INTO app_user (id_user,nama,password_hash,roles,kd_dokter) VALUES (?,?,?,?,?)
		ON DUPLICATE KEY UPDATE nama=VALUES(nama), password_hash=VALUES(password_hash), roles=VALUES(roles), kd_dokter=VALUES(kd_dokter), aktif='1'`, id, nama, string(h), roles, kdDokter)
	return err
}
