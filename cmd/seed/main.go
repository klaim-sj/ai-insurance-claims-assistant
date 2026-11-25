// Command seed membuat akun demo FIKTIF (idempoten).
//
// Password diambil dari env DEMO_PASSWORD dan tidak pernah dicetak.
// Seed menolak berjalan saat APP_ENV=production.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/auth"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
)

// MinDemoPasswordLength adalah panjang minimum DEMO_PASSWORD.
const MinDemoPasswordLength = 12

// DemoUser adalah akun demo sintetis.
type DemoUser struct {
	Name  string
	Email string
	Role  authz.Role
}

// DemoUsers: nama fiktif dan domain @example.com (RFC 2606).
var DemoUsers = []DemoUser{
	{Name: "Demo Petugas Klaim", Email: "officer@example.com", Role: authz.RoleClaimsOfficer},
	{Name: "Demo Supervisor", Email: "supervisor@example.com", Role: authz.RoleSupervisor},
	{Name: "Demo Admin", Email: "admin@example.com", Role: authz.RoleAdmin},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed gagal:", err)
		os.Exit(1)
	}
}

func run() error {
	if os.Getenv("APP_ENV") == "production" {
		return errors.New("seed akun demo tidak boleh dijalankan saat APP_ENV=production")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL wajib diisi")
	}
	password := os.Getenv("DEMO_PASSWORD")
	if len(password) < MinDemoPasswordLength {
		return fmt.Errorf("DEMO_PASSWORD wajib diisi minimal %d karakter", MinDemoPasswordLength)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	n, err := Seed(ctx, pool, auth.NewHasher(auth.DefaultParams), password)
	if err != nil {
		return err
	}
	fmt.Printf("seed selesai: %d akun demo (password dari DEMO_PASSWORD)\n", n)
	for _, u := range DemoUsers {
		fmt.Printf("  - %-24s %s\n", u.Email, u.Role.Label())
	}
	return nil
}

// Seed membuat/memperbarui akun demo dalam satu transaksi dan mencatat audit.
func Seed(ctx context.Context, pool *pgxpool.Pool, hasher *auth.Hasher, password string) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		al := audit.NewLogger(q)
		for _, du := range DemoUsers {
			hash, err := hasher.Hash(password)
			if err != nil {
				return err
			}
			u, err := q.UpsertUser(ctx, db.UpsertUserParams{
				Name:         du.Name,
				Email:        du.Email,
				PasswordHash: hash,
				Role:         string(du.Role),
			})
			if err != nil {
				return fmt.Errorf("upsert %s: %w", du.Role, err)
			}
			if err := al.Record(ctx, audit.Entry{
				EntityType: audit.EntityUser,
				EntityID:   &u.ID,
				Action:     audit.ActionUserSeeded,
				After:      map[string]any{"role": u.Role, "is_active": u.IsActive},
			}); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}
