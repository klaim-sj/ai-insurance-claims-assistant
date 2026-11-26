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

.PHONY: help run test vet migrate-up migrate-down migrate-status migrate-test sqlc seed css db-check eval ocr-check

help:
	@echo "Target: run test vet migrate-up migrate-down migrate-status migrate-test sqlc seed css db-check eval ocr-check"

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

# Alat OCR (tesseract-ocr, tesseract-ocr-ind, poppler-utils) wajib untuk worker & eval.
ocr-check:
	@command -v pdftotext >/dev/null && command -v pdftoppm >/dev/null && command -v tesseract >/dev/null \
		|| (echo "Pasang: apt-get install tesseract-ocr tesseract-ocr-ind poppler-utils" && exit 1)
	@tesseract --list-langs 2>/dev/null | grep -qx ind || (echo "Bahasa tesseract 'ind' belum terpasang (tesseract-ocr-ind)" && exit 1)

# Evaluasi OCR + ekstraksi + temuan rule engine (precision/recall per
# finding_code, jumlah NEEDS_MANUAL_VERIFICATION, waktu proses) terhadap
# expected.json; tidak butuh database. Tiga set dilaporkan terpisah:
#   - utama   : sample-data/          -> docs/eval/
#   - holdout : sample-data/holdout/  -> docs/eval/holdout/   (dipakai menyetel Tahap 5.5)
#   - holdout2: sample-data/holdout2/ -> docs/eval/holdout2/  (dibekukan sebelum perbaikan)
# lalu ringkasan lintas set -> docs/eval/summary.md.
HOLDOUT_NOTE := Holdout pertama (Tahap 5). Sejak Tahap 5.5 set ini DIPAKAI untuk menyetel pra-pemrosesan OCR dan ekstraksi, sehingga bukan lagi uji independen; dilaporkan sebagai pembanding.
HOLDOUT2_NOTE := Holdout2 dibuat dan di-commit sebelum perbaikan Tahap 5.5 ditulis, tidak dipakai menyetel, dan dijalankan dengan kode final sekali; baseline kode Tahap 5 ada di docs/eval/holdout2-baseline/. Dilaporkan apa adanya.

eval: ocr-check
	go run ./cmd/eval -samples sample-data -out docs/eval -dataset utama
	go run ./cmd/eval -samples sample-data/holdout -out docs/eval/holdout -dataset holdout -holdout -psm-compare -note "$(HOLDOUT_NOTE)"
	go run ./cmd/eval -samples sample-data/holdout2 -out docs/eval/holdout2 -dataset holdout2 -holdout -psm-compare -note "$(HOLDOUT2_NOTE)"
	go run ./cmd/eval -summarize docs/eval,docs/eval/holdout,docs/eval/holdout2,docs/eval/holdout2-baseline -out docs/eval/summary.md

seed: db-check
	go run ./cmd/seed

$(TAILWIND):
	mkdir -p tmp/bin
	curl -fsSL -o $@ https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-$(TW_OS)-$(TW_ARCH)
	chmod +x $@

# Hasil build (web/static/css/app.css) di-commit agar aplikasi jalan tanpa Node.js.
css: $(TAILWIND)
	$(TAILWIND) -c tailwind.config.js -i web/tailwind/input.css -o web/static/css/app.css --minify
