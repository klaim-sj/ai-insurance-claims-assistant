package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/auth"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/documents"
)

func isMultipart(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data")
}

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxCSRF
)

// maxFormBytes membatasi ukuran body form (bukan upload).
const maxFormBytes = 64 << 10

// maxUploadBytes membatasi body multipart unggah dokumen: MaxFiles × MaxFileSize
// ditambah ruang untuk field form dan header multipart.
const maxUploadBytes = documents.MaxFiles*documents.MaxFileSize + 1<<20

// uploadMemory: bagian multipart di atas ukuran ini ditulis ke file sementara
// (dihapus otomatis oleh net/http setelah request selesai).
const uploadMemory = 1 << 20

// uploadPathRe: satu-satunya route yang menerima body multipart besar.
var uploadPathRe = regexp.MustCompile(`^/claims/[0-9]+/documents$`)

// contentSecurityPolicy tanpa 'unsafe-inline' dan tanpa sumber eksternal.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; " +
	"base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

func userFrom(ctx context.Context) *auth.User {
	u, _ := ctx.Value(ctxUser).(*auth.User)
	return u
}

func csrfFrom(ctx context.Context) string {
	t, _ := ctx.Value(ctxCSRF).(string)
	return t
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if s.cfg.IsProduction() {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// requestLog mencatat request tanpa PII: hanya method, path (tanpa query),
// status, dan durasi. IP, cookie, dan body tidak dicatat.
func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.log.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"duration_ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				s.serverError(w, r, fmt.Errorf("panic: %v", rec))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// csrf memastikan setiap session punya token, lalu memvalidasi token pada
// request yang mengubah state (selain GET/HEAD/OPTIONS). Token dibaca dari
// field form "csrf_token" atau header "X-CSRF-Token" (HTMX).
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		token := s.sm.GetString(ctx, sessCSRF)
		if token == "" {
			token = newToken()
			s.sm.Put(ctx, sessCSRF, token)
		}

		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			limit := int64(maxFormBytes)
			// Batas besar hanya untuk route unggah dan session yang sudah login,
			// agar klien anonim tidak bisa mengirim body besar.
			if isMultipart(r) && uploadPathRe.MatchString(r.URL.Path) && s.sm.GetInt64(ctx, sessUserID) != 0 {
				limit = maxUploadBytes
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			if isMultipart(r) {
				if err := r.ParseMultipartForm(uploadMemory); err != nil {
					var mbe *http.MaxBytesError
					if errors.As(err, &mbe) {
						s.renderError(w, r, http.StatusRequestEntityTooLarge, "Unggahan terlalu besar",
							fmt.Sprintf("Total unggahan melebihi batas. Maksimal %d file, masing-masing %d MB.",
								documents.MaxFiles, documents.MaxFileSize>>20))
						return
					}
					s.renderError(w, r, http.StatusBadRequest, "Permintaan tidak valid", "Data formulir tidak dapat dibaca.")
					return
				}
			}
			sent := r.Header.Get("X-CSRF-Token")
			if sent == "" {
				sent = r.PostFormValue("csrf_token")
			}
			if sent == "" || subtle.ConstantTimeCompare([]byte(sent), []byte(token)) != 1 {
				s.renderError(w, r, http.StatusForbidden, "Permintaan tidak valid",
					"Sesi formulir sudah kedaluwarsa atau tidak valid. Muat ulang halaman lalu coba lagi.")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, ctxCSRF, token)))
	})
}

// loadUser memuat user dari session. User yang dihapus/nonaktif dikeluarkan.
func (s *Server) loadUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := s.sm.GetInt64(ctx, sessUserID)
		if id == 0 {
			next.ServeHTTP(w, r)
			return
		}
		u, err := s.auth.UserByID(ctx, id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			s.serverError(w, r, err)
			return
		}
		if err != nil || !u.IsActive || !u.Role.Valid() {
			s.sm.Remove(ctx, sessUserID)
			if err := s.sm.RenewToken(ctx); err != nil {
				s.serverError(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, ctxUser, &u)))
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r.Context()) == nil {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// requirePerm adalah lapis pertama RBAC (route). Handler tetap memanggil
// authz.Require sebagai lapis kedua.
func (s *Server) requirePerm(perm authz.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := userFrom(r.Context())
			if u == nil || !authz.Can(u.Role, perm) {
				s.forbidden(w, r, perm)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// forbidden mencatat access_denied lalu menampilkan halaman 403.
func (s *Server) forbidden(w http.ResponseWriter, r *http.Request, perm authz.Permission) {
	u := userFrom(r.Context())
	var uid *int64
	if u != nil {
		uid = &u.ID
	}
	if err := s.audit.Record(r.Context(), audit.Entry{
		UserID:     uid,
		EntityType: audit.EntityRoute,
		Action:     audit.ActionAccessDenied,
		After: map[string]string{
			"method":     r.Method,
			"path":       r.URL.Path,
			"permission": string(perm),
		},
	}); err != nil {
		s.log.Error("gagal menulis audit access_denied", "error", err)
	}
	if u == nil {
		s.renderError(w, r, http.StatusForbidden, "Akses tidak diizinkan", "Anda tidak memiliki akses ke halaman ini.")
		return
	}
	s.render(w, r, http.StatusForbidden, "forbidden", pageData{Title: "Akses tidak diizinkan"})
}

// clientIP mengambil IP klien. X-Forwarded-For hanya dipercaya bila
// TRUST_PROXY=true, dan yang dipakai adalah entri paling kanan (ditambahkan
// oleh proxy tepercaya), bukan entri kiri yang bisa dipalsukan klien.
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			ip := strings.TrimSpace(parts[len(parts)-1])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ipHash menghasilkan HMAC-SHA256 dari IP agar audit log dapat
// mengorelasikan percobaan tanpa menyimpan IP mentah.
func (s *Server) ipHash(ip string) string {
	m := hmac.New(sha256.New, s.cfg.Secret)
	m.Write([]byte(ip))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

func (s *Server) meta(r *http.Request) auth.Meta {
	return auth.Meta{IPHash: s.ipHash(s.clientIP(r))}
}

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("crypto/rand: %w", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
