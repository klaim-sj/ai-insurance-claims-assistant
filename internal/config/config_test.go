package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

var validSecret = strings.Repeat("s", MinSecretLength)

func TestLoadProductionRequiresSecret(t *testing.T) {
	for _, secret := range []string{"", "terlalu-pendek", strings.Repeat("x", MinSecretLength-1)} {
		_, _, err := Load(env(map[string]string{
			"APP_ENV": "production", "DATABASE_URL": "postgres://x", "APP_SECRET": secret,
		}))
		if err == nil {
			t.Fatalf("production dengan APP_SECRET sepanjang %d seharusnya gagal", len(secret))
		}
		if strings.Contains(err.Error(), secret) && secret != "" {
			t.Fatal("pesan error tidak boleh memuat nilai secret")
		}
	}
}

func TestLoadProductionWithValidSecret(t *testing.T) {
	cfg, warns, err := Load(env(map[string]string{
		"APP_ENV": "production", "DATABASE_URL": "postgres://x", "APP_SECRET": validSecret,
	}))
	if err != nil {
		t.Fatalf("tidak diharapkan error: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("tidak diharapkan warning: %v", warns)
	}
	if string(cfg.Secret) != validSecret || !cfg.IsProduction() {
		t.Fatal("konfigurasi production tidak sesuai")
	}
}

func TestLoadDevelopmentFallbackSecret(t *testing.T) {
	cfg, warns, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatalf("tidak diharapkan error: %v", err)
	}
	if cfg.Env != "development" || cfg.Addr != ":8080" {
		t.Fatalf("default tidak sesuai: %+v", cfg)
	}
	if len(cfg.Secret) != MinSecretLength {
		t.Fatalf("secret fallback harus %d byte, dapat %d", MinSecretLength, len(cfg.Secret))
	}
	if len(warns) != 1 {
		t.Fatalf("harus ada 1 warning, dapat %d", len(warns))
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	if _, _, err := Load(env(map[string]string{})); err == nil {
		t.Fatal("DATABASE_URL kosong seharusnya gagal")
	}
}

func TestLoadRejectsUnknownEnv(t *testing.T) {
	if _, _, err := Load(env(map[string]string{"APP_ENV": "staging", "DATABASE_URL": "x"})); err == nil {
		t.Fatal("APP_ENV tidak dikenal seharusnya gagal")
	}
}
