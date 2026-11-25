// Command seed membuat akun demo dan klaim demo FIKTIF (idempoten).
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
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
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

	nc, err := SeedClaims(ctx, pool)
	switch {
	case errors.Is(err, ErrClaimsNotEmpty):
		fmt.Println("seed klaim dilewati:", err)
	case err != nil:
		return err
	case nc == 0:
		fmt.Println("seed klaim: klaim demo sudah ada")
	default:
		fmt.Printf("seed klaim selesai: %d klaim demo\n", nc)
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

// DemoClaim adalah klaim demo sintetis (blueprint bagian 5).
type DemoClaim struct {
	Number  string
	Name    string // nama fiktif
	Policy  string
	DaysAgo int // tanggal diterima relatif terhadap hari ini
	// Steps adalah urutan transisi menuju status contoh.
	Steps []DemoStep
}

// DemoStep adalah satu transisi status pada seed.
type DemoStep struct {
	To     claims.Status
	System bool
	Reason string
}

// DemoClaims sesuai contoh data dummy blueprint. Seluruh nama dan nomor
// polis fiktif.
var DemoClaims = []DemoClaim{
	{
		Number: "CLM-DEMO-0001", Name: "Budi Santoso", Policy: "POL-DEMO-0001", DaysAgo: 12,
		Steps: []DemoStep{
			{To: claims.StatusSubmitted},
			{To: claims.StatusProcessing, System: true},
			{To: claims.StatusNeedsReview, System: true, Reason: "Pemeriksaan otomatis (simulasi) menemukan hal yang perlu ditinjau petugas."},
		},
	},
	{
		Number: "CLM-DEMO-0002", Name: "Siti Rahma", Policy: "POL-DEMO-0002", DaysAgo: 7,
		Steps: []DemoStep{
			{To: claims.StatusSubmitted},
			{To: claims.StatusProcessing, System: true},
		},
	},
	{
		Number: "CLM-DEMO-0003", Name: "Andi Pratama", Policy: "POL-DEMO-0003", DaysAgo: 3,
		Steps: []DemoStep{
			{To: claims.StatusSubmitted},
			{To: claims.StatusNeedDocuments, Reason: "File pendukung buram (simulasi); mohon unggah ulang dokumen yang terbaca jelas."},
		},
	},
}

// ErrClaimsNotEmpty: seed klaim hanya berjalan pada tabel klaim kosong agar
// nomor CLM-DEMO-0001 s.d. 0003 tepat.
var ErrClaimsNotEmpty = errors.New("tabel klaim sudah berisi data selain klaim demo")

// demoOfficerEmail adalah petugas yang ditugaskan pada klaim demo.
const demoOfficerEmail = "officer@example.com"

// SeedClaims membuat klaim demo melalui claims.Service sehingga nomor
// otomatis, riwayat status, dan audit log terbentuk seperti penggunaan
// normal. Idempoten: mengembalikan 0 bila seluruh klaim demo sudah ada.
func SeedClaims(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	q := db.New(pool)
	existing := 0
	for _, dc := range DemoClaims {
		_, err := q.GetClaimByNumber(ctx, dc.Number)
		if err == nil {
			existing++
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
	}
	if existing == len(DemoClaims) {
		return 0, nil
	}
	var total int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM claims").Scan(&total); err != nil {
		return 0, err
	}
	var counter int64
	if err := pool.QueryRow(ctx,
		"SELECT coalesce(max(last_value), 0) FROM claim_number_counters WHERE prefix = $1",
		claims.NumberPrefix).Scan(&counter); err != nil {
		return 0, err
	}
	if total > 0 || counter > 0 {
		return 0, ErrClaimsNotEmpty
	}

	officer, err := q.GetUserByEmail(ctx, demoOfficerEmail)
	if err != nil {
		return 0, fmt.Errorf("akun demo officer belum ada: %w", err)
	}
	actor := claims.Actor{UserID: officer.ID, Role: authz.Role(officer.Role)}
	svc := claims.NewService(pool, nil)
	today := time.Now().In(claims.Location)

	n := 0
	for _, dc := range DemoClaims {
		c, err := svc.Create(ctx, actor, claims.Input{
			ClaimantName: dc.Name,
			PolicyNumber: dc.Policy,
			ProductType:  claims.ProductHealthSim,
			ReceivedAt:   today.AddDate(0, 0, -dc.DaysAgo).Format(claims.DateLayout),
		})
		if err != nil {
			return n, fmt.Errorf("membuat %s: %w", dc.Number, err)
		}
		if c.ClaimNumber != dc.Number {
			return n, fmt.Errorf("nomor klaim %s tidak sesuai harapan %s", c.ClaimNumber, dc.Number)
		}
		for _, st := range dc.Steps {
			if st.System {
				_, err = svc.SystemTransition(ctx, c.ID, st.To, st.Reason)
			} else {
				_, err = svc.TransitionStatus(ctx, actor, c.ID, st.To, st.Reason)
			}
			if err != nil {
				return n, fmt.Errorf("transisi %s ke %s: %w", dc.Number, st.To, err)
			}
		}
		n++
	}
	return n, nil
}
