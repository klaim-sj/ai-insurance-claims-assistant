package migrations_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/testutil"
	"github.com/klaim-sj/ai-insurance-claims-assistant/migrations"
)

func TestDownAndUpRoundTrip(t *testing.T) {
	pool := testutil.DB(t)
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(sqlDB, ".", 0); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_name IN ('users','claims','claim_documents','audit_logs','sessions')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("setelah down semua tabel harus terhapus, sisa %d", n)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
}

func TestClaimStatusConstraint(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	insert := `INSERT INTO claims (claim_number, policy_number, claimant_name, product_type, received_at, status)
	           VALUES ($1, 'POL-0000-0001', 'Nama Fiktif Uji', 'demo', now(), $2)`

	valid := []string{"draft", "submitted", "processing", "needs_review", "need_documents", "reviewed", "closed"}
	for i, st := range valid {
		if _, err := pool.Exec(ctx, insert, "CLM-TEST-000"+string(rune('1'+i)), st); err != nil {
			t.Errorf("status %q harus diterima: %v", st, err)
		}
	}
	if _, err := pool.Exec(ctx, insert, "CLM-TEST-0099", "approved"); err == nil {
		t.Fatal("status di luar daftar harus ditolak")
	}
	if _, err := pool.Exec(ctx, insert, "CLM-TEST-0001", "draft"); err == nil {
		t.Fatal("claim_number duplikat harus ditolak")
	}
}
