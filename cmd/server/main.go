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
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/config"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/documents"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/ocr"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/processing"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/server"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/validation"
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

	storage, err := documents.NewStorage(cfg.StorageDir)
	if err != nil {
		return err
	}
	docSvc := documents.NewService(pool, storage, logger)
	claimSvc := claims.NewService(pool, logger)
	valSvc := validation.NewService(pool, claimSvc, logger)

	// Queue River: job ProcessClaimDocument dimasukkan saat unggah/proses
	// ulang. Worker (OCR + ekstraksi) berjalan di proses ini bila diaktifkan.
	clientCfg := processing.ClientConfig{Logger: logger, MaxWorkers: cfg.WorkerConcurrency}
	if cfg.WorkerEnabled {
		engine := ocr.NewExec(ocr.ExecConfig{Timeout: cfg.OCRTimeout})
		if err := engine.Check(ctx); err != nil {
			// Server tetap berjalan; job akan gagal dengan kode error yang
			// jelas dan dapat diproses ulang setelah alat dipasang.
			logger.Warn("alat OCR belum lengkap; pemrosesan dokumen akan gagal", "error_code", string(ocr.CodeOf(err)))
		}
		clientCfg.Worker = processing.NewWorker(pool, storage, engine, logger)
		// Validasi aturan (Tahap 5) otomatis setelah semua dokumen klaim selesai.
		clientCfg.Validator = validation.NewWorker(valSvc)
	}
	queue, err := processing.NewClient(pool, clientCfg)
	if err != nil {
		return err
	}
	if cfg.WorkerEnabled {
		if err := queue.Start(ctx); err != nil {
			return err
		}
		logger.Info("worker dokumen berjalan", "concurrency", cfg.WorkerConcurrency)
		defer func() {
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := queue.Stop(stopCtx); err != nil {
				logger.Error("menghentikan worker", "error", err)
			}
		}()
	}

	srv, err := server.New(server.Options{
		Config:                 cfg,
		Pool:                   pool,
		Claims:                 claimSvc,
		Documents:              docSvc,
		Validation:             valSvc,
		Queue:                  queue.Queue,
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

	// Timeout pendek untuk semua route; route unggah memperpanjang batas
	// baca per request (lihat server.NewHTTPServer dan middleware uploadBody).
	httpSrv := server.NewHTTPServer(cfg.Addr, srv.Handler())

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
