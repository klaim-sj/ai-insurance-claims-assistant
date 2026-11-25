# Lingkungan pengembangan GitHub Codespaces: Go 1.24 + klien PostgreSQL
# (pg_isready dipakai oleh target `make db-check`).
FROM mcr.microsoft.com/devcontainers/go:1-1.24-bookworm

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends postgresql-client \
    && rm -rf /var/lib/apt/lists/*

# Jangan pernah mengunduh toolchain Go lain secara otomatis.
ENV GOTOOLCHAIN=local
