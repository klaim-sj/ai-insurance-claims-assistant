#!/usr/bin/env bash
# Dijalankan sekali setelah codespace dibuat (postCreateCommand).
set -euo pipefail

export GOTOOLCHAIN=local

echo "==> Memasang goose v3.26.0 (kompatibel Go 1.24)"
go install github.com/pressly/goose/v3/cmd/goose@v3.26.0

echo "==> Memasang sqlc v1.31.1 (binary rilis, lihat scripts/install-sqlc.sh)"
bash scripts/install-sqlc.sh

echo "==> Mengunduh dependensi Go"
go mod download

echo "==> Menunggu PostgreSQL"
for _ in $(seq 1 30); do
  pg_isready -q && break
  sleep 2
done
pg_isready

echo "==> Migration database development"
make migrate-up

cat <<'MSG'

Selesai. Langkah berikutnya:
  make seed   # butuh Codespaces secret DEMO_PASSWORD
  make run    # lalu buka port 8080 dari tab Ports
MSG
