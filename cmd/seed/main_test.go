package main

import (
	"context"
	"strings"
	"testing"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/auth"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/testutil"
)

func TestDemoUsersAreSynthetic(t *testing.T) {
	if len(DemoUsers) != 3 {
		t.Fatalf("harus ada 3 akun demo, dapat %d", len(DemoUsers))
	}
	roles := map[string]bool{}
	for _, u := range DemoUsers {
		if !strings.HasSuffix(u.Email, "@example.com") {
			t.Errorf("email demo harus @example.com: %s", u.Email)
		}
		if !strings.HasPrefix(u.Name, "Demo ") {
			t.Errorf("nama demo harus jelas fiktif: %s", u.Name)
		}
		roles[string(u.Role)] = true
	}
	if len(roles) != 3 {
		t.Fatal("setiap akun demo harus punya peran berbeda")
	}
}

func TestSeedIsIdempotent(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	hasher := auth.NewHasher(auth.Params{Memory: 1024, Iterations: 1, Parallelism: 1})
	const pw = "demo-password-uji-0000"

	for i := 0; i < 2; i++ {
		n, err := Seed(ctx, pool, hasher, pw)
		if err != nil || n != 3 {
			t.Fatalf("seed ke-%d: n=%d err=%v", i+1, n, err)
		}
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("seed harus idempoten (3 user), dapat %d", count)
	}

	q := db.New(pool)
	for _, du := range DemoUsers {
		u, err := q.GetUserByEmail(ctx, du.Email)
		if err != nil {
			t.Fatalf("user %s tidak ada: %v", du.Role, err)
		}
		if u.Role != string(du.Role) || !u.IsActive {
			t.Fatalf("user %s tidak sesuai", du.Role)
		}
		if ok, _ := auth.Verify(pw, u.PasswordHash); !ok {
			t.Fatalf("password user %s tidak cocok", du.Role)
		}
	}

	rows, err := q.ListAuditLogsByAction(ctx, db.ListAuditLogsByActionParams{Action: audit.ActionUserSeeded, Limit: 10})
	if err != nil || len(rows) != 6 {
		t.Fatalf("harus ada 6 audit user_seeded, dapat %d (err=%v)", len(rows), err)
	}
	for _, r := range rows {
		if strings.Contains(string(r.AfterJson), "argon2") || strings.Contains(string(r.AfterJson), pw) {
			t.Fatal("audit seed tidak boleh memuat hash/password")
		}
	}
}
