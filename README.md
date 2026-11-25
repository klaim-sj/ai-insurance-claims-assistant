# AI Insurance Claims Assistant

[![CI](https://github.com/klaim-sj/ai-insurance-claims-assistant/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/klaim-sj/ai-insurance-claims-assistant/actions/workflows/ci.yml)

Proyek portofolio pribadi: aplikasi web *decision-support* untuk membantu
petugas klaim memeriksa dokumen, menemukan ketidaksesuaian, dan menyiapkan
ringkasan pemeriksaan.

> **Disclaimer**
> - **Seluruh data di repositori dan demo ini SINTETIS/FIKTIF.** Tidak ada
>   data nasabah, dokumen, atau identitas nyata. Akun demo memakai nama
>   fiktif dan domain `@example.com`; data uji memakai NIK berawalan `0000`
>   dan telepon `0800-0000-xxxx`.
> - Aplikasi **tidak** menyetujui/menolak klaim, menghitung manfaat final,
>   atau menyimpulkan kelayakan/fraud. **Keputusan akhir selalu pada petugas
>   berwenang.**
> - Bukan untuk penggunaan operasional tanpa penilaian keamanan, privasi,
>   legal, dan kepatuhan.

Acuan desain: [`docs/blueprint.pdf`](docs/blueprint.pdf).

## Status

| Tahap | Cakupan | Status |
|---|---|---|
| 1. Fondasi | Skema DB, login/logout, session, CSRF, rate limit, RBAC, audit log, layout | ✅ |
| 2. Claim CRUD | Buat/cari/lihat/ubah klaim, transisi status, timeline, otorisasi per klaim | ✅ |
| 3. Dokumen | Upload privat, metadata, preview, validasi file | Berikutnya |
| 4+ | OCR, validasi, AI summary, review, laporan | Direncanakan |

## Stack

Go 1.24 · chi · pgx/v5 · sqlc · goose · scs (session di PostgreSQL) ·
argon2id · html/template + HTMX + Tailwind · PostgreSQL 16.

Aset frontend (`web/static/css/app.css` hasil build Tailwind,
`web/static/js/htmx.min.js`) di-commit dan disematkan ke binary, sehingga
aplikasi berjalan **tanpa Node.js dan tanpa CDN**.

## Struktur

```
cmd/server        aplikasi web
cmd/seed          seed akun demo & klaim demo fiktif
internal/auth     argon2id, rate limiter, layanan login
internal/authz    matriks role → izin → menu
internal/audit    penulisan audit_logs
internal/claims   layanan klaim: validasi, nomor otomatis, transisi status, daftar
internal/config   konfigurasi dari environment
internal/db       kode hasil sqlc (jangan diedit manual)
internal/server   router, middleware (CSRF, RBAC, header keamanan), handler
internal/testutil database test terisolasi
migrations/       migration goose (juga disematkan untuk test)
db/queries/       query sqlc
web/templates     template HTML
web/static        CSS, htmx, app.js
```

## Menjalankan secara lokal

Prasyarat: Go 1.24, PostgreSQL 16, `goose` dan `sqlc` terpasang di
`$(go env GOPATH)/bin`:

```bash
go install github.com/pressly/goose/v3/cmd/goose@v3.26.0
```

```bash
bash scripts/install-sqlc.sh
```

`sqlc` dipasang dari binary rilis v1.31.1 (checksum SHA-256 dipin di
skrip, khusus Linux) karena `go install` sqlc v1.31.1 menuntut Go 1.26.
Di macOS, pasang sqlc v1.31.1 dari rilis resminya atau package manager.

Opsional, PostgreSQL via Docker (hanya untuk lokal; membuat `claims_dev`
dan `claims_test`):

```bash
docker compose up -d
```

1. Salin konfigurasi lalu isi nilainya (jangan commit `.env`):

   ```bash
   cp .env.example .env
   ```

2. Pastikan PostgreSQL berjalan:

   ```bash
   pg_isready || service postgresql start
   ```

3. Jalankan migration, seed akun demo, lalu server:

   ```bash
   make migrate-up
   ```

   ```bash
   make seed
   ```

   ```bash
   make run
   ```

4. Buka <http://localhost:8080>.

### Akun demo

`make seed` membuat tiga akun fiktif (idempoten). Password diambil dari
env `DEMO_PASSWORD` (minimal 12 karakter) dan tidak pernah dicetak. Seed
menolak berjalan saat `APP_ENV=production`.

| Email | Peran | Menu |
|---|---|---|
| `officer@example.com` | Claims Officer | Dashboard, Klaim |
| `supervisor@example.com` | Supervisor | Dashboard, Klaim, Laporan |
| `admin@example.com` | Admin | Dashboard, Laporan, Pengguna |

Seed juga membuat tiga klaim fiktif (hanya bila tabel klaim masih kosong),
seluruhnya ditugaskan ke `officer@example.com`, produk "Kesehatan (simulasi)":

| Nomor | Nama fiktif | Polis | Status |
|---|---|---|---|
| CLM-DEMO-0001 | Budi Santoso | POL-DEMO-0001 | Perlu ditinjau |
| CLM-DEMO-0002 | Siti Rahma | POL-DEMO-0002 | Sedang diproses |
| CLM-DEMO-0003 | Andi Pratama | POL-DEMO-0003 | Perlu dokumen |

Halaman Laporan dan Pengguna masih placeholder (tahap berikutnya).

## Mencoba di Codespaces

Repositori menyediakan `.devcontainer/` (Go 1.24 + PostgreSQL 16 dengan
database `claims_dev` dan `claims_test`). Saat codespace dibuat,
`postCreateCommand` memasang goose v3.26.0 dan sqlc v1.31.1 lalu
menjalankan `make migrate-up`. `DATABASE_URL`/`TEST_DATABASE_URL` sudah
diset otomatis; kredensial database hanya berlaku di dalam codespace.

1. **Isi secrets dulu** (tidak pernah disimpan di repo): GitHub →
   *Settings* → *Codespaces* → *Secrets* → *New secret*, lalu beri akses ke
   repositori ini:

   | Secret | Isi |
   |---|---|
   | `DEMO_PASSWORD` | Password akun demo, minimal 12 karakter. Buat baru, jangan pakai ulang password lain. |
   | `APP_SECRET` | Minimal 32 karakter, mis. hasil `openssl rand -base64 48`. Opsional di development (bila kosong dipakai nilai acak sementara). |

   Kedua secret juga dideklarasikan di `devcontainer.json`, sehingga
   halaman *Create codespace* (opsi *Advanced*) meminta nilainya bila belum
   ada. Secret yang ditambah/diubah setelah codespace berjalan baru terbaca
   setelah codespace di-*rebuild* atau di-*restart*.
2. **Buat codespace**: tombol *Code* → tab *Codespaces* → *Create
   codespace on main*. Tunggu sampai `postCreateCommand` selesai.
3. **Seed dan jalankan** di terminal codespace:

   ```bash
   make seed
   ```

   ```bash
   make run
   ```

4. **Buka port 8080** dari notifikasi yang muncul atau tab *Ports* →
   *Open in Browser*. Port di-forward dengan visibility **private**
   (hanya akun Anda yang bisa membuka); jangan ubah menjadi public.
   Login memakai akun demo (lihat *Akun demo* di atas) dengan password `DEMO_PASSWORD`.

Jangan membuat file `.env` di codespace kecuali perlu; bila ada, nilainya
menimpa environment variable dari codespace (Makefile memuat `.env`).

## Continuous integration

`.github/workflows/ci.yml` berjalan pada push ke `main` dan setiap pull
request dengan permission minimal (`contents: read`): service PostgreSQL
16, `gofmt` check, `go vet`, `sqlc diff`, dan `go test -race`. Kredensial
database CI adalah nilai sintetis yang ditulis di workflow, sedangkan
`APP_SECRET` dan `DEMO_PASSWORD` dibuat acak per run (`openssl rand`) dan
di-mask di log. Workflow tidak memakai secret repositori.

## Manajemen klaim (Tahap 2)

- **Nomor klaim** `CLM-DEMO-0001` dibuat otomatis lewat tabel
  `claim_number_counters` (`INSERT … ON CONFLICT DO UPDATE … RETURNING` di
  dalam transaksi pembuatan klaim). Baris penghitung terkunci sampai commit,
  jadi aman dari race condition dan tidak ada nomor terlewat saat rollback.
- **Validasi server-side**: nama pemohon 2–100 huruf; nomor polis wajib
  format simulasi `POL-DEMO-0000` (sengaja menolak format nomor polis
  nyata); produk dari allowlist; tanggal diterima tidak di masa depan
  (WIB); petugas harus Claims Officer aktif.
- **Transisi status** hanya lewat satu fungsi service
  (`claims.Service.transition`): kunci baris klaim, validasi peta transisi,
  tulis `claim_status_histories` dan `audit_logs` dalam satu transaksi.

  | Dari | Ke (pengguna) | Ke (hanya sistem) |
  |---|---|---|
  | Draft | Diajukan, Ditutup* | – |
  | Diajukan | Perlu dokumen*, Sudah ditinjau | Sedang diproses |
  | Sedang diproses | – | Perlu ditinjau, Diajukan (proses gagal) |
  | Perlu ditinjau | Sudah ditinjau, Perlu dokumen* | – |
  | Perlu dokumen | Diajukan, Ditutup* | – |
  | Sudah ditinjau | Perlu dokumen*, Ditutup* | – |
  | Ditutup | final | – |

  \* alasan wajib. Status administrasi **bukan** keputusan manfaat atau
  kelayakan klaim. `SystemTransition` disiapkan untuk pemrosesan dokumen
  pada tahap berikutnya.
- **Otorisasi per klaim** (middleware `requireClaimAccess` **dan** service):
  Claims Officer hanya klaim yang ditugaskan kepadanya; Supervisor seluruh
  klaim tim (demo: satu tim) dan dapat memindahkan penugasan; Admin tidak
  mengakses data klaim. Penolakan → `403` dan tercatat `access_denied`
  (entity `claim`).
- **Daftar klaim**: cari awalan nomor klaim/polis (wildcard di-escape),
  filter status, produk, rentang tanggal, petugas (Supervisor), 20 per
  halaman. Seluruh query lewat sqlc (parameterized).
- **Audit**: `claim_created`, `claim_updated` (before/after hanya field yang
  berubah; nilai nama pemohon & nomor polis disamarkan `[DIUBAH]`),
  `status_changed` (status lama/baru dan `history_id`; isi alasan tidak
  disalin ke audit).

### Target Makefile

| Target | Fungsi |
|---|---|
| `make run` | Menjalankan server |
| `make test` | `go test ./...` (memakai `TEST_DATABASE_URL`) |
| `make vet` | `go vet ./...` |
| `make migrate-up` / `migrate-down` / `migrate-status` | Migration DB development |
| `make migrate-test` | Migration manual DB test (test juga memigrasi otomatis) |
| `make sqlc` | Generate ulang `internal/db` |
| `make seed` | Seed akun demo & klaim demo |
| `make css` | Build ulang Tailwind (CLI standalone diunduh ke `tmp/bin`) |

### Environment variable

| Variabel | Keterangan |
|---|---|
| `APP_ENV` | `development` (default), `production`, atau `test` |
| `APP_ADDR` | Alamat listen, default `:8080` |
| `APP_SECRET` | Kunci HMAC (mis. hash IP di audit log). **Wajib ≥ 32 karakter saat production**; aplikasi gagal start bila kosong/pendek. Di development, bila kosong dipakai nilai acak + warning. |
| `DATABASE_URL` | Database development |
| `TEST_DATABASE_URL` | Database test (harus berbeda dari `DATABASE_URL`) |
| `DEMO_PASSWORD` | Password akun demo untuk `make seed` (≥ 12 karakter) |
| `TRUST_PROXY` | `true` hanya bila di belakang reverse proxy tepercaya (IP klien dari entri paling kanan `X-Forwarded-For`) |

## Catatan keamanan & privasi

- **Password**: argon2id (m=64 MiB, t=3, p=2), format PHC, perbandingan
  constant-time. Parameter bisa diatur lewat struct `auth.Params`; test
  memakai parameter ringan.
- **Session**: scs + PostgreSQL. Cookie `HttpOnly`, `SameSite=Lax`, `Secure`
  saat `APP_ENV=production`. Token session diganti saat login (anti
  *session fixation*) dan dihapus di server saat logout. Idle timeout 30
  menit, maksimal 8 jam. User yang dinonaktifkan langsung kehilangan sesi.
- **CSRF**: token per session, wajib di semua request yang mengubah state
  (field `csrf_token` atau header `X-CSRF-Token` untuk HTMX).
- **Login gagal**: pesan selalu generik ("Email atau kata sandi salah."),
  termasuk untuk email tak terdaftar dan akun nonaktif; waktu respons
  disamakan dengan verifikasi hash dummy.
- **Rate limit login**: per IP, 5 percobaan lalu 1 per 12 detik, respons
  `429` + `Retry-After`. **Disimpan in-memory: ter-reset saat aplikasi
  restart dan tidak dibagi antar-instance.** Cukup untuk demo satu
  instance, *bukan* untuk deployment multi-instance (gunakan penyimpanan
  bersama seperti PostgreSQL/Redis bila diperlukan).
- **RBAC**: diperiksa di middleware route **dan** di handler/service
  (`authz.Require`). Akses ditolak → `403` dan tercatat `access_denied`.
- **Audit log** (`audit_logs`, append-only via trigger): `login_succeeded`,
  `login_failed`, `logout`, `access_denied`, `user_seeded`, `claim_created`,
  `claim_updated`, `status_changed`. Tidak menyimpan
  password, email yang dicoba, maupun IP mentah; IP disimpan sebagai
  HMAC-SHA256 (`ip_hash`) untuk korelasi.
- **Log aplikasi**: hanya method, path (tanpa query), status, durasi. Tanpa
  IP, cookie, body, atau PII.
- **Header**: CSP ketat tanpa `'unsafe-inline'`/`'unsafe-eval'` dan tanpa
  sumber eksternal; HTMX dikonfigurasi via `<meta name="htmx-config">` dan
  `web/static/js/app.js`. Juga `X-Frame-Options: DENY`, `nosniff`,
  `Referrer-Policy`, HSTS saat production.
- **UI**: responsif, skip-link, label form, fokus terlihat, menu mobile
  dapat dipakai dengan keyboard (Enter/Escape).

## Pengujian

```bash
make vet
```

```bash
make test
```

Test database memakai `TEST_DATABASE_URL` (ditolak bila sama dengan
`DATABASE_URL`). Setiap test mengambil advisory lock PostgreSQL lalu
men-`TRUNCATE` seluruh tabel (`RESTART IDENTITY`), sehingga hasil
deterministik walau `go test ./...` menjalankan package secara paralel.

Cakupan Tahap 1: hash/verify password, konfigurasi `APP_SECRET`, login
sukses per peran, login gagal (password salah, email tak dikenal, akun
nonaktif), CSRF, rate limit per IP, akses lintas peran (route & handler),
redirect tanpa login, logout + replay cookie lama, audit log tercatat tanpa
PII, header keamanan, migration up/down, constraint status klaim, seed
idempoten.

Cakupan Tahap 2: transisi valid/tidak valid (termasuk status khusus sistem
dan klaim final), alasan wajib, TC-07 officer tidak bisa membuka/mengubah
klaim officer lain (route, edit, update, transisi; tercatat
`access_denied`), lapis service tetap menolak tanpa middleware, supervisor
bisa, admin tidak, 20 pembuatan klaim bersamaan menghasilkan nomor unik dan
berurutan, validasi form, audit hanya field berubah tanpa PII, filter
(status, produk, tanggal, petugas, pencarian, input wildcard/injeksi) dan
pagination, seed klaim idempoten.
