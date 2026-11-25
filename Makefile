# Perintah goose tidak di-echo agar URL database (berisi password) tidak
# tercetak di terminal/log.

# Muat .env bila ada (format KEY=value tanpa tanda kutip).
-include .env
export

# goose & sqlc dipasang di $(go env GOPATH)/bin; dipanggil dengan path
# absolut agar tidak bergantung pada PATH.
GOBIN := $(shell go env GOPATH)/bin
GOOSE := $(GOBIN)/goose
SQLC  := $(GOBIN)/sqlc

MIGRATIONS_DIR := migrations

# Tailwind standalone CLI (tanpa Node.js), diunduh ke tmp/bin (diabaikan git).
TAILWIND_VERSION := v3.4.17
TAILWIND := tmp/bin/tailwindcss-$(TAILWIND_VERSION)
UNAME_S := $(shell uname -s | tr '[:upper:]' '[:lower:]')
UNAME_M := $(shell uname -m)
ifeq ($(UNAME_S),darwin)
  TW_OS := macos
else
  TW_OS := linux
endif
ifneq (,$(filter $(UNAME_M),arm64 aarch64))
  TW_ARCH := arm64
else
  TW_ARCH := x64
endif

.PHONY: help run test vet migrate-up migrate-down migrate-status migrate-test sqlc seed css db-check

help:
	@echo "Target: run test vet migrate-up migrate-down migrate-status migrate-test sqlc seed css db-check"

db-check:
	@pg_isready >/dev/null || (echo "PostgreSQL belum jalan. Jalankan: service postgresql start" && exit 1)

run: db-check
	go run ./cmd/server

test: db-check
	go test ./...

vet:
	go vet ./...

migrate-up: db-check
	@echo "goose up (DATABASE_URL)"
	@$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" up

migrate-down: db-check
	@echo "goose down (DATABASE_URL)"
	@$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" down

migrate-status: db-check
	@echo "goose status (DATABASE_URL)"
	@$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" status

# Test memigrasi DB test otomatis; target ini untuk migrasi manual.
migrate-test: db-check
	@echo "goose up (TEST_DATABASE_URL)"
	@$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(TEST_DATABASE_URL)" up

sqlc:
	$(SQLC) generate

seed: db-check
	go run ./cmd/seed

$(TAILWIND):
	mkdir -p tmp/bin
	curl -fsSL -o $@ https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-$(TW_OS)-$(TW_ARCH)
	chmod +x $@

# Hasil build (web/static/css/app.css) di-commit agar aplikasi jalan tanpa Node.js.
css: $(TAILWIND)
	$(TAILWIND) -c tailwind.config.js -i web/tailwind/input.css -o web/static/css/app.css --minify
