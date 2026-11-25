#!/usr/bin/env bash
# Memasang sqlc dari binary rilis resmi ke $(go env GOPATH)/bin.
#
# Mengapa binary, bukan `go install`? sqlc v1.31.1 (versi yang dipakai untuk
# generate internal/db) mendeklarasikan `go 1.26` di go.mod-nya sehingga tidak
# bisa di-build dengan Go 1.24 (GOTOOLCHAIN=local). Binary rilis berjalan
# tanpa bergantung pada versi Go, dan versinya sama persis dengan header kode
# hasil generate, sehingga `sqlc diff` konsisten.
#
# Rilis sqlc tidak menyertakan file checksum; nilai SHA-256 di bawah dihitung
# dari aset rilis v1.31.1 dan dipin di sini. Ubah keduanya bersamaan.
set -euo pipefail

SQLC_VERSION="1.31.1"
SHA256_linux_amd64="497ae4fcdfa64c5b0c311ffe4c2bd991e43991e82e5367792ed78bc2dca27354"
SHA256_linux_arm64="b7cae247740d0c51a1e657479e5b2d21e6fef428f596682a01bc55bf4ab8a23d"

case "$(uname -m)" in
  x86_64 | amd64) arch="amd64" ;;
  aarch64 | arm64) arch="arm64" ;;
  *) echo "Arsitektur tidak didukung: $(uname -m)" >&2; exit 1 ;;
esac
if [ "$(uname -s)" != "Linux" ]; then
  echo "Skrip ini hanya untuk Linux (Codespaces/CI)." >&2
  exit 1
fi

gobin="$(go env GOPATH)/bin"
if [ -x "$gobin/sqlc" ] && [ "$("$gobin/sqlc" version)" = "v${SQLC_VERSION}" ]; then
  echo "sqlc v${SQLC_VERSION} sudah terpasang"
  exit 0
fi

expected_var="SHA256_linux_${arch}"
expected="${!expected_var}"
asset="sqlc_${SQLC_VERSION}_linux_${arch}.tar.gz"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

curl -fsSL --retry 3 -o "$tmp/$asset" \
  "https://github.com/sqlc-dev/sqlc/releases/download/v${SQLC_VERSION}/${asset}"
echo "${expected}  $tmp/$asset" | sha256sum -c -
tar -xzf "$tmp/$asset" -C "$tmp" sqlc
mkdir -p "$gobin"
install -m 0755 "$tmp/sqlc" "$gobin/sqlc"
echo "sqlc $("$gobin/sqlc" version) terpasang di $gobin"
