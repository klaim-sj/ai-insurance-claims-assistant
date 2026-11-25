// Package auth berisi hashing password, rate limiting, dan logika login.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
)

// ErrInvalidCredentials sengaja generik: dipakai untuk email tak dikenal,
// password salah, maupun akun nonaktif, agar tidak membocorkan informasi.
var ErrInvalidCredentials = errors.New("email atau kata sandi salah")

// Kode alasan kegagalan login yang disimpan di audit log (bukan ke pengguna).
const (
	ReasonInvalidCredentials = "invalid_credentials"
	ReasonInactiveAccount    = "inactive_account"
)

// User adalah representasi pengguna yang aman untuk dibawa di request.
type User struct {
	ID       int64
	Name     string
	Email    string
	Role     authz.Role
	IsActive bool
}

// FromRow mengubah baris database menjadi User (tanpa password hash).
func FromRow(r db.User) User {
	return User{ID: r.ID, Name: r.Name, Email: r.Email, Role: authz.Role(r.Role), IsActive: r.IsActive}
}

// Meta adalah metadata request yang aman untuk audit (tanpa PII mentah).
type Meta struct {
	IPHash string
}

// Service menangani autentikasi.
type Service struct {
	q         *db.Queries
	audit     *audit.Logger
	dummyHash string
	log       *slog.Logger
}

// NewService membuat Service. hasher dipakai untuk membuat hash dummy agar
// waktu respons untuk email tak dikenal setara dengan password salah.
func NewService(q *db.Queries, a *audit.Logger, hasher *Hasher) (*Service, error) {
	dummy, err := hasher.Hash("dummy-password-untuk-penyamaan-waktu")
	if err != nil {
		return nil, fmt.Errorf("membuat hash dummy: %w", err)
	}
	return &Service{q: q, audit: a, dummyHash: dummy, log: slog.Default()}, nil
}

// Authenticate memverifikasi kredensial dan mencatat hasilnya di audit log.
// Password tidak pernah disimpan atau dicatat.
func (s *Service) Authenticate(ctx context.Context, email, password string, meta Meta) (User, error) {
	email = strings.TrimSpace(email)

	var row db.User
	var err error
	if email != "" {
		row, err = s.q.GetUserByEmail(ctx, email)
	} else {
		err = pgx.ErrNoRows
	}
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = Verify(password, s.dummyHash) // penyamaan waktu
		s.recordFailure(ctx, nil, ReasonInvalidCredentials, meta)
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, fmt.Errorf("mengambil user: %w", err)
	}

	ok, err := Verify(password, row.PasswordHash)
	if err != nil {
		return User{}, fmt.Errorf("verifikasi password: %w", err)
	}
	if !ok {
		s.recordFailure(ctx, &row.ID, ReasonInvalidCredentials, meta)
		return User{}, ErrInvalidCredentials
	}
	if !row.IsActive {
		s.recordFailure(ctx, &row.ID, ReasonInactiveAccount, meta)
		return User{}, ErrInvalidCredentials
	}

	// Fail closed: login sukses wajib tercatat.
	if err := s.audit.Record(ctx, audit.Entry{
		UserID:     &row.ID,
		EntityType: audit.EntityUser,
		EntityID:   &row.ID,
		Action:     audit.ActionLoginSucceeded,
		After:      map[string]string{"ip_hash": meta.IPHash},
	}); err != nil {
		return User{}, err
	}
	return FromRow(row), nil
}

func (s *Service) recordFailure(ctx context.Context, userID *int64, reason string, meta Meta) {
	// Kegagalan menulis audit tidak mengubah hasil (tetap gagal login),
	// tetapi dicatat di log aplikasi tanpa PII.
	err := s.audit.Record(ctx, audit.Entry{
		UserID:     userID,
		EntityType: audit.EntityUser,
		EntityID:   userID,
		Action:     audit.ActionLoginFailed,
		After:      map[string]string{"reason": reason, "ip_hash": meta.IPHash},
	})
	if err != nil {
		s.log.Error("gagal menulis audit login_failed", "error", err)
	}
}

// Logout mencatat logout pengguna.
func (s *Service) Logout(ctx context.Context, userID int64, meta Meta) error {
	return s.audit.Record(ctx, audit.Entry{
		UserID:     &userID,
		EntityType: audit.EntityUser,
		EntityID:   &userID,
		Action:     audit.ActionLogout,
		After:      map[string]string{"ip_hash": meta.IPHash},
	})
}

// UserByID mengambil user aktif berdasarkan ID (dipakai untuk memuat sesi).
func (s *Service) UserByID(ctx context.Context, id int64) (User, error) {
	row, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return User{}, err
	}
	return FromRow(row), nil
}
