# Lingkungan pengembangan GitHub Codespaces: Go 1.24 + klien PostgreSQL
# (pg_isready dipakai oleh target `make db-check`).
FROM mcr.microsoft.com/devcontainers/go:1-1.24-bookworm

# Base image membawa sumber apt pihak ketiga Yarn (dl.yarnpkg.com) yang kunci
# GPG-nya kedaluwarsa/berganti, sehingga `apt-get update` gagal (NO_PUBKEY,
# exit code 100). Proyek tidak memakai Yarn/Node, jadi sumber dan keyring-nya
# dihapus. Sumber lain di base image hanya debian.sources (repositori resmi).
# Bila base image kelak menambah sumber pihak ketiga lain yang tidak dipakai,
# hapus juga di sini.
RUN rm -f /etc/apt/sources.list.d/yarn*.list /usr/share/keyrings/yarn-archive-keyring.gpg \
    && apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends postgresql-client \
    && rm -rf /var/lib/apt/lists/*

# Jangan pernah mengunduh toolchain Go lain secara otomatis.
ENV GOTOOLCHAIN=local
