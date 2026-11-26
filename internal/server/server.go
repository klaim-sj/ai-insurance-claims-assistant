// Package server merangkai router HTTP, session, middleware keamanan, dan handler.
package server

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/auth"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/config"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/documents"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/processing"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/validation"
	"github.com/klaim-sj/ai-insurance-claims-assistant/web"
)

// Kunci data session.
const (
	sessUserID = "user_id"
	sessCSRF   = "csrf_token"
	sessFlash  = "flash"
)

// Options adalah dependensi Server.
type Options struct {
	Config  config.Config
	Pool    *pgxpool.Pool
	Auth    *auth.Service
	Audit   *audit.Logger
	Limiter *auth.RateLimiter
	// Claims opsional; bila nil dibuat dari Pool.
	Claims *claims.Service
	// Documents opsional; bila nil dibuat dari Pool dan Config.StorageDir.
	Documents *documents.Service
	// Validation opsional; bila nil dibuat dari Pool dan Claims.
	Validation *validation.Service
	// Queue: antrean pemrosesan dokumen. Bila nil, dibuat client River
	// insert-only dari Pool (job diproses oleh worker terpisah).
	Queue  documents.Queue
	Logger *slog.Logger
	// UploadReadTimeout: batas baca body route unggah; 0 berarti
	// DefaultUploadReadTimeout.
	UploadReadTimeout time.Duration
	// SessionCleanupInterval: interval pembersihan session kedaluwarsa.
	// 0 menonaktifkan goroutine pembersih (berguna untuk test).
	SessionCleanupInterval time.Duration
}

// Server adalah aplikasi HTTP.
type Server struct {
	cfg     config.Config
	auth    *auth.Service
	audit   *audit.Logger
	limiter *auth.RateLimiter
	claims  *claims.Service
	docs    *documents.Service
	val     *validation.Service
	// uploadReadTimeout: perpanjangan read deadline khusus route unggah.
	uploadReadTimeout time.Duration
	log               *slog.Logger
	sm                *scs.SessionManager
	store             *pgxstore.PostgresStore
	tmpl              *templates
	handler           http.Handler
}

// New membuat Server.
func New(o Options) (*Server, error) {
	if o.Pool == nil || o.Auth == nil || o.Audit == nil || o.Limiter == nil {
		return nil, errors.New("server: Pool, Auth, Audit, dan Limiter wajib diisi")
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Claims == nil {
		o.Claims = claims.NewService(o.Pool, o.Logger)
	}
	if o.Documents == nil {
		dir := o.Config.StorageDir
		if dir == "" {
			dir = config.DefaultStorageDir
		}
		st, err := documents.NewStorage(dir)
		if err != nil {
			return nil, err
		}
		o.Documents = documents.NewService(o.Pool, st, o.Logger)
	}
	if o.Validation == nil {
		o.Validation = validation.NewService(o.Pool, o.Claims, o.Logger)
	}
	if o.Queue == nil {
		rc, err := processing.NewClient(o.Pool, processing.ClientConfig{Logger: o.Logger})
		if err != nil {
			return nil, err
		}
		o.Queue = rc.Queue
	}
	o.Documents.SetQueue(o.Queue)
	if vq, ok := o.Queue.(validation.Queue); ok {
		o.Validation.SetQueue(vq)
	}
	tmpl, err := parseTemplates(web.Templates())
	if err != nil {
		return nil, err
	}

	store := pgxstore.NewWithCleanupInterval(o.Pool, o.SessionCleanupInterval)
	sm := scs.New()
	sm.Store = store
	sm.Lifetime = 8 * time.Hour
	sm.IdleTimeout = 30 * time.Minute
	sm.Cookie.Name = "claims_session"
	sm.Cookie.HttpOnly = true
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Secure = o.Config.IsProduction()
	sm.Cookie.Path = "/"

	s := &Server{
		cfg:     o.Config,
		auth:    o.Auth,
		audit:   o.Audit,
		limiter: o.Limiter,
		claims:  o.Claims,
		docs:    o.Documents,
		val:     o.Validation,

		uploadReadTimeout: o.UploadReadTimeout,
		log:               o.Logger,
		sm:                sm,
		store:             store,
		tmpl:              tmpl,
	}
	if s.uploadReadTimeout <= 0 {
		s.uploadReadTimeout = DefaultUploadReadTimeout
	}
	sm.ErrorFunc = func(w http.ResponseWriter, r *http.Request, err error) {
		s.serverError(w, r, err)
	}
	s.handler = s.routes()
	return s, nil
}

// Handler mengembalikan http.Handler aplikasi.
func (s *Server) Handler() http.Handler { return s.handler }

// Close menghentikan goroutine pembersih session.
func (s *Server) Close() { s.store.StopCleanup() }

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(s.recoverer, s.requestLog, s.securityHeaders)

	r.Handle("/static/*", http.StripPrefix("/static/", staticHandler()))
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	r.Group(func(r chi.Router) {
		// uploadBody harus sebelum csrf karena csrf membaca body form.
		r.Use(s.sm.LoadAndSave, s.uploadBody, s.csrf, s.loadUser)

		r.Get("/", s.home)
		r.Get("/login", s.getLogin)
		r.Post("/login", s.postLogin)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/logout", s.postLogout)
			r.With(s.requirePerm(authz.PermDashboardView)).Get("/dashboard", s.dashboard)
			r.Route("/claims", s.claimRoutes)
			r.With(s.requirePerm(authz.PermReportsView)).Get("/reports", s.placeholder("Laporan", "/reports", authz.PermReportsView))
			r.With(s.requirePerm(authz.PermUsersManage)).Get("/admin/users", s.placeholder("Pengguna", "/admin/users", authz.PermUsersManage))
		})

		r.NotFound(s.notFound)
	})
	return r
}

// staticHandler melayani aset dari web/static tanpa directory listing.
func staticHandler() http.Handler {
	fsrv := http.FileServerFS(web.Static())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		fsrv.ServeHTTP(w, r)
	})
}
