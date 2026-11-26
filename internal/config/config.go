// Package config memuat konfigurasi aplikasi dari environment variable.
package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MinSecretLength adalah panjang minimum APP_SECRET (dalam byte/karakter).
const MinSecretLength = 32

// Config berisi konfigurasi runtime aplikasi.
type Config struct {
	Env         string // development | production | test
	Addr        string // alamat listen HTTP, mis. ":8080"
	DatabaseURL string
	// Secret dipakai untuk HMAC (mis. hash IP di audit log). Tidak pernah dicetak.
	Secret []byte
	// TrustProxy: jika true, IP klien diambil dari X-Forwarded-For.
	TrustProxy bool
	// StorageDir: direktori private storage dokumen (di luar web/static).
	StorageDir string
	// WorkerEnabled: jalankan worker River (OCR/ekstraksi) di proses server.
	WorkerEnabled bool
	// WorkerConcurrency: jumlah dokumen yang diproses paralel.
	WorkerConcurrency int
	// OCRTimeout: batas waktu per pemanggilan binary OCR.
	OCRTimeout time.Duration
}

// Nilai default worker OCR.
const (
	DefaultWorkerConcurrency = 2
	DefaultOCRTimeout        = 60 * time.Second
)

// DefaultStorageDir dipakai bila STORAGE_DIR kosong.
const DefaultStorageDir = "storage/documents"

// IsProduction melaporkan apakah aplikasi berjalan di mode production.
func (c Config) IsProduction() bool { return c.Env == "production" }

// Load membaca konfigurasi memakai getenv (biasanya os.Getenv).
// warnings berisi peringatan non-fatal yang sebaiknya dicatat pemanggil.
func Load(getenv func(string) string) (cfg Config, warnings []string, err error) {
	cfg.Env = strings.ToLower(strings.TrimSpace(getenv("APP_ENV")))
	if cfg.Env == "" {
		cfg.Env = "development"
	}
	switch cfg.Env {
	case "development", "production", "test":
	default:
		return Config{}, nil, fmt.Errorf("APP_ENV tidak dikenal: harus development, production, atau test")
	}

	cfg.Addr = strings.TrimSpace(getenv("APP_ADDR"))
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}

	cfg.DatabaseURL = strings.TrimSpace(getenv("DATABASE_URL"))
	if cfg.DatabaseURL == "" {
		return Config{}, nil, errors.New("DATABASE_URL wajib diisi")
	}

	cfg.StorageDir = strings.TrimSpace(getenv("STORAGE_DIR"))
	if cfg.StorageDir == "" {
		cfg.StorageDir = DefaultStorageDir
	}

	cfg.WorkerEnabled = !strings.EqualFold(strings.TrimSpace(getenv("WORKER_ENABLED")), "false")
	cfg.WorkerConcurrency = DefaultWorkerConcurrency
	if v := strings.TrimSpace(getenv("WORKER_CONCURRENCY")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 16 {
			return Config{}, nil, errors.New("WORKER_CONCURRENCY harus bilangan 1–16")
		}
		cfg.WorkerConcurrency = n
	}
	cfg.OCRTimeout = DefaultOCRTimeout
	if v := strings.TrimSpace(getenv("OCR_TIMEOUT")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Second || d > 30*time.Minute {
			return Config{}, nil, errors.New("OCR_TIMEOUT harus durasi 1s–30m, mis. 60s")
		}
		cfg.OCRTimeout = d
	}

	cfg.TrustProxy = strings.EqualFold(strings.TrimSpace(getenv("TRUST_PROXY")), "true")

	secret := getenv("APP_SECRET")
	switch {
	case len(secret) >= MinSecretLength:
		cfg.Secret = []byte(secret)
	case cfg.IsProduction():
		return Config{}, nil, fmt.Errorf("APP_SECRET wajib diisi minimal %d karakter saat APP_ENV=production", MinSecretLength)
	default:
		buf := make([]byte, MinSecretLength)
		if _, err := rand.Read(buf); err != nil {
			return Config{}, nil, fmt.Errorf("membuat APP_SECRET sementara: %w", err)
		}
		cfg.Secret = buf
		warnings = append(warnings, fmt.Sprintf(
			"APP_SECRET kosong atau kurang dari %d karakter; memakai nilai acak sementara (hanya untuk development, hash IP di audit log tidak konsisten antar-restart)",
			MinSecretLength))
	}

	return cfg, warnings, nil
}
