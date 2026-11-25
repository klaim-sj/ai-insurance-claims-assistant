// Command server menjalankan aplikasi web AI Insurance Claims Assistant.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/auth"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/config"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/server"
)

// Batas percobaan login per IP: 5 percobaan sekaligus, lalu 1 per 12 detik
// (≈5 per menit). Disimpan in-memory; ter-reset saat restart.
const (
	loginRateEvery = 12 * time.Second
	loginRateBurst = 5
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("server berhenti", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, warnings, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		logger.Warn(w)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return errors.New("tidak dapat terhubung ke database (periksa DATABASE_URL dan pg_isready)")
	}

	queries := db.New(pool)
	auditLog := audit.NewLogger(queries)
	authSvc, err := auth.NewService(queries, auditLog, auth.NewHasher(auth.DefaultParams))
	if err != nil {
		return err
	}

	srv, err := server.New(server.Options{
		Config:                 cfg,
		Pool:                   pool,
		Auth:                   authSvc,
		Audit:                  auditLog,
		Limiter:                auth.NewRateLimiter(loginRateEvery, loginRateBurst),
		Logger:                 logger,
		SessionCleanupInterval: 5 * time.Minute,
	})
	if err != nil {
		return err
	}
	defer srv.Close()

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		// Unggah dokumen hingga ±51 MB membutuhkan waktu baca lebih panjang.
		ReadTimeout:  2 * time.Minute,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server berjalan", "addr", cfg.Addr, "env", cfg.Env)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("menghentikan server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}
