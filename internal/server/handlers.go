package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/auth"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
)

// Pesan untuk pengguna. Sengaja generik.
const (
	msgLoginFailed  = "Email atau kata sandi salah."
	msgRateLimited  = "Terlalu banyak percobaan masuk. Silakan coba lagi beberapa saat lagi."
	retryAfterValue = "60"
)

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	if userFrom(r.Context()) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	if userFrom(r.Context()) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	s.render(w, r, http.StatusOK, "login", pageData{Title: "Masuk"})
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !s.limiter.Allow(s.clientIP(r)) {
		w.Header().Set("Retry-After", retryAfterValue)
		s.render(w, r, http.StatusTooManyRequests, "login", pageData{Title: "Masuk", Error: msgRateLimited})
		return
	}

	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")

	user, err := s.auth.Authenticate(ctx, email, password, s.meta(r))
	if errors.Is(err, auth.ErrInvalidCredentials) {
		s.render(w, r, http.StatusUnauthorized, "login", pageData{Title: "Masuk", Error: msgLoginFailed, Email: email})
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	// Cegah session fixation: token session dan token CSRF diganti.
	if err := s.sm.RenewToken(ctx); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.sm.Put(ctx, sessUserID, user.ID)
	s.sm.Put(ctx, sessCSRF, newToken())

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) postLogout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	if err := s.auth.Logout(ctx, u.ID, s.meta(r)); err != nil {
		s.log.Error("gagal menulis audit logout", "error", err)
	}
	if err := s.sm.Destroy(ctx); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	if err := authz.Require(u.Role, authz.PermDashboardView); err != nil {
		s.forbidden(w, r, authz.PermDashboardView)
		return
	}
	s.render(w, r, http.StatusOK, "dashboard", pageData{
		Title:  "Dashboard",
		Active: "/dashboard",
		KPIs:   []string{"Total Klaim", "Processing", "Perlu Ditinjau", "Perlu Dokumen"},
	})
}

// placeholder membuat handler halaman kosong untuk tahap berikutnya.
// authz.Require adalah lapis kedua setelah middleware requirePerm.
func (s *Server) placeholder(title, path string, perm authz.Permission) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r.Context())
		if u == nil || authz.Require(u.Role, perm) != nil {
			s.forbidden(w, r, perm)
			return
		}
		s.render(w, r, http.StatusOK, "placeholder", pageData{Title: title, Active: path})
	}
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusNotFound, "Halaman tidak ditemukan", "Halaman yang Anda cari tidak tersedia.")
}
