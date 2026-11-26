// Package testutil menyediakan database test yang terisolasi.
//
// Isolasi: setiap test mengambil advisory lock PostgreSQL (sehingga test DB
// dari package berbeda yang dijalankan paralel oleh `go test ./...` tidak
// saling menimpa), lalu men-TRUNCATE seluruh tabel aplikasi sebelum mulai.
// Hasilnya deterministik: setiap test mulai dari database kosong dengan ID
// yang di-reset.
package testutil

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/klaim-sj/ai-insurance-claims-assistant/migrations"
)

// lockKey adalah kunci advisory lock bersama untuk seluruh test database.
const lockKey = 7_420_251_125

// Tabel aplikasi yang dikosongkan sebelum setiap test.
var tables = []string{
	"audit_logs", "claim_status_histories", "claim_number_counters",
	"validation_findings", "extracted_fields", "ocr_results", "claim_documents", "claims", "sessions", "users",
	"river_job", "river_leader", "river_queue",
}

var (
	poolOnce sync.Once
	pool     *pgxpool.Pool
	poolErr  error
	migrated bool
)

// DB mengembalikan pool ke TEST_DATABASE_URL yang sudah dimigrasi dan
// dikosongkan. Lock dilepas otomatis saat test selesai.
func DB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_DATABASE_URL wajib diisi untuk menjalankan test database")
	}
	if dev := os.Getenv("DATABASE_URL"); dev != "" && dev == url {
		t.Fatal("TEST_DATABASE_URL tidak boleh sama dengan DATABASE_URL")
	}

	poolOnce.Do(func() {
		pool, poolErr = pgxpool.New(context.Background(), url)
	})
	if poolErr != nil {
		t.Fatalf("koneksi test database: %v", poolErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire koneksi: %v", err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", int64(lockKey)); err != nil {
		conn.Release()
		t.Fatalf("advisory lock: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", int64(lockKey))
		conn.Release()
	})

	// Package lain (mis. test migrasi down/up) bisa membuat ulang tabel di
	// antara test; tutup koneksi lain agar cache prepared statement lama
	// tidak dipakai ("cached plan must not change result type"). Koneksi
	// lock tetap hidup sampai dilepas.
	pool.Reset()

	if !migrated {
		if err := migrate(pool); err != nil {
			t.Fatalf("migrasi test database: %v", err)
		}
		migrated = true
	}

	if _, err := conn.Exec(ctx, "TRUNCATE "+strings.Join(tables, ", ")+" RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}

func migrate(p *pgxpool.Pool) error {
	sqlDB := stdlib.OpenDBFromPool(p)
	defer sqlDB.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(sqlDB, ".")
}
