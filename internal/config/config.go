// Package config memuat konfigurasi aplikasi dari environment variable.
package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
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
}

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
