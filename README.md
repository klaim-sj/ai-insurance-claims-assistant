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
| 3. Dokumen | Upload privat, metadata, preview, validasi file, data uji sintetis | ✅ |
| 4. OCR & ekstraksi | Queue River, OCR (pdftotext/Tesseract), ekstraksi field deterministik, data terdeteksi vs data klaim, `make eval` | ✅ |
| 5. Validasi | Aturan deterministik, temuan lintas dokumen dengan evidence, job validasi otomatis, panel Temuan, precision/recall + set holdout | ✅ |
| 6+ | AI summary, review temuan, laporan | Berikutnya: 6. AI analysis |

## Stack

Go 1.24 · chi · pgx/v5 · sqlc · goose · scs (session di PostgreSQL) ·
argon2id · html/template + HTMX + Tailwind · PostgreSQL 16 ·
River v0.30.2 (queue di PostgreSQL) · Tesseract 5 (`ind+eng`) + poppler-utils.

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
internal/documents unggah dokumen: validasi file, private storage, duplikat, akses, hasil ekstraksi
internal/ocr      OCR via os/exec: pdftotext → pdftoppm + Tesseract (TSV → confidence)
internal/extraction ekstraksi field deterministik (tanpa LLM) + label confidence
internal/processing job River ProcessClaimDocument (worker, retry, audit)
internal/pii      penyamaran NIK
cmd/eval          evaluasi OCR + ekstraksi + temuan pada sample-data (make eval)
internal/config   konfigurasi dari environment
internal/db       kode hasil sqlc (jangan diedit manual)
internal/server   router, middleware (CSRF, RBAC, header keamanan), handler
internal/testutil database test terisolasi
migrations/       migration goose (juga disematkan untuk test)
db/queries/       query sqlc
sample-data/      dokumen uji SINTETIS + expected.json (lihat sample-data/README.md)
web/templates     template HTML
web/static        CSS, htmx, app.js
```

## Menjalankan secara lokal

Prasyarat: Go 1.24, PostgreSQL 16, alat OCR (`apt-get install
tesseract-ocr tesseract-ocr-ind poppler-utils`; cek dengan `make ocr-check`),
serta `goose` dan `sqlc` terpasang di `$(go env GOPATH)/bin`:

```bash
go install github.com/pressly/goose/v3/cmd/goose@v3.26.0
```

```bash
bash scripts/install-sqlc.sh
```

`sqlc` dipasang dari binary rilis v1.31.1 (khusus Linux) karena
`go install` sqlc v1.31.1 menuntut Go 1.26. Di macOS, pasang sqlc v1.31.1
dari rilis resminya atau package manager.

> **Catatan checksum:** upstream sqlc tidak menerbitkan file checksum untuk
> rilisnya. Hash SHA-256 di `scripts/install-sqlc.sh` **dihitung sendiri**
> dari aset rilis v1.31.1 saat skrip dibuat, lalu dipin. Hash ini hanya
> mendeteksi bila aset berubah setelah itu; ia bukan verifikasi dari
> upstream. Saat menaikkan versi, unduh aset baru, hitung ulang hash-nya
> (`sha256sum`), dan perbarui versi serta hash bersamaan.

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
16, alat OCR (`tesseract-ocr`, `tesseract-ocr-ind`, `poppler-utils`),
`gofmt` check, `go vet`, `sqlc diff`, `go test -race`, dan `make eval`. Kredensial
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

## Dokumen klaim (Tahap 3)

Unggah dan pratinjau dokumen langsung dari halaman detail klaim. Tahap ini
**hanya** menyimpan file dan metadata; OCR/ekstraksi/AI belum ada.

- **Unggah**: 1–5 file PDF/JPG/PNG per kiriman, satu jenis dokumen
  (`formulir_klaim`, `identitas`, `surat_keterangan_medis`, `kuitansi`,
  `lainnya`). Status awal `processing_status = uploaded`; sejak Tahap 4
  dokumen langsung diantrekan (`queued`) dalam transaksi yang sama.
- **Validasi server-side** (`internal/documents/validate.go`), semua atau
  tidak sama sekali (satu file ditolak → tidak ada yang disimpan):
  - jenis file dari **magic bytes**, harus cocok dengan ekstensi (TC-02);
  - maksimal 10 MB per file; body multipart dibatasi ±51 MB dengan
    `http.MaxBytesReader`, hanya di route unggah untuk sesi yang sudah login
    (request lain dibatasi 64 KB);
  - PNG/JPEG: batas resolusi (anti *decompression bomb*) lalu decode penuh
    untuk mendeteksi file rusak;
  - PDF: header, `startxref`/`%%EOF`, tolak `/Encrypt`, 1–50 halaman
    (termasuk objek halaman di *object stream* terkompresi, dengan batas
    dekompresi). Ini pemeriksaan struktural ringan berbasis stdlib, **bukan**
    parser PDF lengkap.
- **Timeout**: `http.Server` memakai timeout pendek untuk semua route
  (`ReadHeaderTimeout` 5 dtk, `ReadTimeout` 15 dtk, `WriteTimeout` 30 dtk;
  `server.NewHTTPServer`). Hanya route unggah (sesi login) yang
  memperpanjang read/write deadline **per request** lewat
  `http.ResponseController` (default 2 menit), di middleware `uploadBody`
  yang berjalan sebelum CSRF membaca body.
- **Private storage**: `STORAGE_DIR` (default `storage/documents`, di luar
  `web/static`, di-`.gitignore`). Nama file acak 128-bit (`ab/<32 hex>`),
  tanpa nama asli/ekstensi, izin `0600`/`0700`, akses lewat `os.Root`.
  Checksum SHA-256 disimpan. File dihapus lagi bila transaksi metadata gagal.
- **Duplikat** (checksum sama dalam klaim yang sama, atau dua file identik
  dalam satu kiriman): tidak disimpan, respons `409` dengan permintaan
  konfirmasi; setelah dikonfirmasi disimpan dengan `duplicate_of`. Dengan
  HTMX, file yang dipilih tetap ada saat konfirmasi.
- **Pratinjau/unduh** hanya lewat `GET /claims/{id}/documents/{docID}`
  (`?download=1` untuk lampiran): RBAC + akses per klaim di middleware
  **dan** service, dokumen harus milik klaim di URL (anti IDOR → `404`).
  Header: `Content-Disposition` dengan nama generik `dokumen-<id>.<ext>`,
  `nosniff`, `Cache-Control: private, no-store`, CSP `default-src 'none'`
  (gambar juga `sandbox`). Tidak ada URL publik.
- **Akses**: Claims Officer (klaim yang ditugaskan) dan Supervisor (seluruh
  klaim tim). Admin tidak memiliki izin dokumen. Klaim berstatus Ditutup
  tidak menerima dokumen baru.
- **Audit**: `document_uploaded` (jenis, mime, ukuran, halaman,
  `duplicate_of`), `document_viewed` (mode preview/download, gagal audit →
  file tidak dilayani), `document_duplicate_detected` (id dokumen yang
  sudah ada). **Tanpa** isi file, nama file asli, path storage, atau
  checksum. Nama asli (disanitasi) hanya disimpan di `claim_documents`
  untuk ditampilkan ke petugas yang berhak.
- **Data uji**: `sample-data/` berisi dokumen sintetis bertanda watermark
  "CONTOH - DATA SINTETIS" untuk 5 skenario (lengkap, nama beda, polis beda,
  gambar buram, kuitansi sebelum tanggal kejadian) plus `expected.json`.
- Belum ada: hapus dokumen, drag-and-drop, malware scanning (catatan untuk
  tahap *hardening*).

## OCR & ekstraksi field (Tahap 4)

Hasil OCR/ekstraksi adalah **bahan saran tindak lanjut petugas, bukan
keputusan**. Status klaim tidak diubah oleh pemrosesan dokumen.

- **Queue River** (`internal/processing`): job `ProcessClaimDocument`
  dimasukkan dengan `InsertTx` di transaksi unggah (bila transaksi batal, job
  ikut batal). Status dokumen: `uploaded → queued → processing → completed |
  failed`. Worker berjalan di proses server (`WORKER_ENABLED`, default
  `true`).
  - **Tanpa duplikasi job**: `UPDATE … WHERE processing_status IN
    ('uploaded','failed','completed')` (dokumen yang sedang antre/diproses
    tidak dapat diantrekan lagi) + unique job River per (dokumen,
    `queued_at`). Worker melewati job untuk dokumen yang sudah selesai.
  - **Retry terbatas**: maks. 3 percobaan, backoff 10 dtk → 40 dtk. Kode
    error (`OCR_TIMEOUT`, `OCR_ENGINE_ERROR`, `PDF_RENDER_FAILED`,
    `FILE_UNAVAILABLE`, …) disimpan di `claim_documents.processing_error_code`
    dan ditampilkan dengan pesan yang jelas. Error permanen langsung `failed`.
  - **Skema River** ada di `migrations/*_river_*.sql` (dijalankan goose),
    tetapi tidak dibaca sqlc: `sqlc.yaml` mendaftarkan migrasi aplikasi satu
    per satu sehingga `internal/db/models.go` hanya berisi tabel aplikasi.
    Migrasi aplikasi baru wajib ditambahkan ke `sqlc.yaml` (dijaga test).
  - **Proses ulang**: tombol untuk petugas (izin `documents.process`) pada
    dokumen `failed`/`completed`; hasil lama diganti.
- **OCR** (`internal/ocr`): PDF dicoba `pdftotext -raw` dulu; halaman tanpa
  teks dirender `pdftoppm -r 300 -gray` lalu dibaca `tesseract -l ind+eng
  --psm 6 tsv`. Gambar langsung Tesseract. Baris disusun ulang dari posisi
  kata TSV agar pasangan "label : nilai" tetap sebaris. Disimpan per halaman
  di `ocr_results` (teks, engine, engine_version, rata-rata confidence kata
  TSV; lapisan teks PDF = 100). Binary dipanggil lewat `os/exec` **tanpa
  shell**, dengan timeout per panggilan (`OCR_TIMEOUT`), environment minimal
  (tanpa secret), output dibatasi, stderr dibuang, di direktori sementara
  yang selalu dihapus.
- **Ekstraksi deterministik** (`internal/extraction`, tanpa LLM):
  `claimant_name`, `policy_number`, `nik`, `event_date`, `document_date`,
  `amount`, sesuai jenis dokumen. Setiap field menyimpan nilai ternormalisasi,
  status (`found`/`uncertain`/`not_found`), confidence, halaman, dan kutipan
  sumber pendek. Diagnosis/detail medis **tidak** diekstrak.
  - Label confidence: **tinggi** ≥ 85, **perlu verifikasi** 60–85,
    **rendah** < 60 (status `uncertain`). Nilai yang formatnya tidak valid
    juga `uncertain`. Label tidak ada → `not_found`; halaman tidak terbaca →
    semua field `uncertain` tanpa nilai (tidak menebak, TC-05).
- **NIK tidak pernah tersimpan utuh**: nilai field `0000********1234`; teks
  OCR dan kutipan juga disamarkan sebelum disimpan. Dijaga juga oleh CHECK
  constraint di database.
- **UI**: halaman *Hasil ekstraksi* per dokumen (pratinjau di kiri, field di
  kanan dengan halaman, kutipan, label confidence; status dipolling HTMX).
  Pratinjau tersemat memakai `?embed=1`, satu-satunya respons yang boleh
  di-frame, dan hanya oleh origin yang sama (`frame-ancestors 'self'`).
  Detail klaim menampilkan **Data terdeteksi vs data klaim** (nama & nomor
  polis dari formulir klaim terbaru). Tombol **Terapkan ke data klaim**
  hanya untuk nilai `found` yang berbeda dan lolos validasi; nilai diambil
  dari database (bukan input form), dengan konfirmasi, dan tercatat
  `extracted_value_applied`. Tidak ada perubahan otomatis.
- **Audit**: `document_processing_queued/started/completed/failed`,
  `extracted_value_applied`, `document_viewed` (mode `extraction`). Hanya
  ID, status, kode error, jumlah halaman/field, engine, durasi — **tanpa**
  teks OCR atau nilai field. Log aplikasi juga hanya memuat ID dan kode error.
- **Evaluasi**: `make eval` memproses `sample-data/` terhadap `expected.json`
  (mode standar dan mode *scan* yang memaksa OCR) dan menulis
  [`docs/eval/report.md`](docs/eval/report.md) + `report.json`: akurasi per
  field, jumlah uncertain/not_found, waktu proses.

## Validasi aturan & temuan (Tahap 5)

Temuan adalah **dasar saran tindak lanjut untuk petugas, bukan keputusan**.
Rule engine (`internal/validation`) deterministik, tanpa LLM, dan tidak
pernah menyetujui/menolak klaim, menghitung manfaat, atau menyimpulkan
kelayakan/fraud. Istilah UI: "perlu ditinjau".

| Aturan (v1) | Kondisi | Severity |
|---|---|---|
| `MISSING_DOCUMENT` | Dokumen wajib klaim kesehatan (formulir_klaim, identitas, surat_keterangan_medis, kuitansi) belum diunggah | high |
| `MISSING_FIELD` | Field wajib per jenis dokumen `not_found` (medium) atau `uncertain` (low) | low/medium |
| `NAME_MISMATCH` | Nama antar dokumen / vs data klaim berbeda setelah normalisasi (huruf, spasi, tanda baca, diakritik, gelar). Inisial/nama tengah hilang = "kemungkinan singkatan" (low); selain itu "berbeda" (medium). Acuan: identitas bila terbaca, selain itu data klaim. Nama `uncertain` tidak dibandingkan | low/medium |
| `POLICY_MISMATCH` | Nomor polis formulir ≠ data klaim (abaikan spasi/huruf besar-kecil) | medium |
| `DATE_INCONSISTENCY` | Tanggal kuitansi sebelum tanggal kejadian/masuk, atau tanggal setelah hari ini (WIB) | medium |
| `LOW_QUALITY_DOCUMENT` | Halaman OCR dengan confidence < 60 atau < 5 kata (lapisan teks PDF tidak dinilai) | medium; high bila semua halaman |

- **Konfigurasi**: `validation_rules` (rule_code, versi, `rule_config` jsonb,
  satu versi aktif per aturan). Seed v1 lewat migration `00012`; test menjaga
  seed = `validation.DefaultRules()`.
- **Evidence wajib**: setiap temuan memuat ≥1 evidence (`document`:
  document_id + halaman + field/sinyal + nilai + kutipan ≤120 karakter;
  `claim_record`; `checklist`). `Finding.Validate` menolak temuan tanpa
  evidence/NIK utuh sebelum insert, dan CHECK constraint di database
  menolaknya juga. NIK selalu `0000********1234`.
- **Dokumen yang dinilai**: per jenis hanya dokumen terbaru yang selesai
  diproses (unggahan ulang = pengganti); semua dokumen `lainnya`.
- **Kapan berjalan**: job River `validate_claim` dimasukkan dalam transaksi
  worker saat dokumen **terakhir** klaim selesai (berhasil/gagal final;
  baris klaim dikunci agar dua dokumen yang selesai bersamaan tetap memicu
  tepat satu validasi). Bila dokumen diunggah bertahap, validasi dapat
  berjalan lebih dari sekali; temuan sementara (mis. `MISSING_DOCUMENT`
  sebelum dokumen berikutnya diunggah) otomatis menjadi `resolved` pada
  validasi berikutnya, tetapi status `needs_review` yang sudah di-set
  tidak dikembalikan otomatis. Tombol **Jalankan ulang validasi** (izin
  `validation.run`, dicek di middleware dan service).
- **Idempoten**: temuan diidentifikasi `fingerprint` kondisi
  (`UNIQUE (claim_id, fingerprint)`). Kondisi sama → baris yang sama
  diperbarui; kondisi hilang → `resolved`; muncul lagi → dibuka kembali;
  `dismissed` tidak diubah. Proses per klaim berjalan berurutan.
- **Status klaim**: bila ada temuan open dan klaim `submitted`/`processing`,
  sistem memindahkan ke `needs_review` lewat `SystemTransition`. Status
  lain tidak disentuh. `need_documents` **tidak pernah** di-set otomatis:
  ditampilkan sebagai saran dengan tautan yang hanya mengisi pilihan form
  ubah status; petugas tetap mengisi alasan dan menyimpan.
- **UI**: panel **Temuan** di detail klaim — severity, pesan netral, bukti
  dari tiap sumber berdampingan, tautan ke halaman dokumen
  (`…/hasil?hal=N`, pratinjau PDF dibuka di halaman itu dan teks OCR
  halaman itu disorot), saran tindak lanjut.
- **Audit**: `validation_run_requested/started/completed`,
  `finding_created` (termasuk dibuka kembali), `finding_resolved` — hanya
  ID, kode, severity, versi aturan, jumlah. Tanpa nilai field, teks OCR,
  atau nama.

### Evaluasi temuan

`make eval` menjalankan rule engine per skenario `expected.json` dan
menghitung precision/recall per `finding_code` (satuan: skenario × kode)
serta *evidence coverage*. Dua laporan terpisah:

- [`docs/eval/report.md`](docs/eval/report.md) — dataset utama
  `sample-data/` (dipakai menyetel; angka tinggi di sini bukan ukuran
  kinerja nyata).
- [`docs/eval/holdout/report.md`](docs/eval/holdout/report.md) — set
  **holdout** `sample-data/holdout/` (generator, tata letak, font,
  kemiringan, dan noise berbeda; tidak dipakai menyetel; aturan dibekukan
  sebelum set dibuat). Hasil dilaporkan apa adanya.

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
| `make ocr-check` | Cek pdftotext, pdftoppm, tesseract + bahasa `ind` |
| `make eval` | Evaluasi OCR + ekstraksi + temuan: `sample-data/` → `docs/eval/`, holdout `sample-data/holdout/` → `docs/eval/holdout/` |

### Environment variable

| Variabel | Keterangan |
|---|---|
| `APP_ENV` | `development` (default), `production`, atau `test` |
| `APP_ADDR` | Alamat listen, default `:8080` |
| `APP_SECRET` | Kunci HMAC (mis. hash IP di audit log). **Wajib ≥ 32 karakter saat production**; aplikasi gagal start bila kosong/pendek. Di development, bila kosong dipakai nilai acak + warning. |
| `DATABASE_URL` | Database development |
| `TEST_DATABASE_URL` | Database test (harus berbeda dari `DATABASE_URL`) |
| `DEMO_PASSWORD` | Password akun demo untuk `make seed` (≥ 12 karakter) |
| `STORAGE_DIR` | Direktori private storage dokumen, default `storage/documents` (jangan di bawah `web/static`) |
| `WORKER_ENABLED` | Jalankan worker OCR di proses server (default `true`) |
| `WORKER_CONCURRENCY` | Dokumen yang diproses paralel, 1–16 (default `2`) |
| `OCR_TIMEOUT` | Batas waktu per pemanggilan binary OCR, mis. `60s` (default) |
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
  `claim_updated`, `status_changed`, `document_uploaded`, `document_viewed`,
  `document_duplicate_detected`, `document_processing_queued/started/
  completed/failed`, `extracted_value_applied`. Tidak menyimpan
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

Cakupan Tahap 5: setiap aturan table-driven (termasuk normalisasi nama,
singkatan vs berbeda, nama/tanggal uncertain tidak dibandingkan, dokumen
pengganti), validator evidence (tanpa evidence, halaman kosong, NIK utuh,
kutipan panjang) di service dan CHECK database, idempotensi (jalan ulang,
resolved, dibuka kembali, dismissed tetap), `need_documents` tidak pernah
di-set otomatis dari status mana pun, job validasi tepat sekali setelah
dokumen terakhir, end-to-end River (unggah → OCR → temuan → `needs_review`),
akses lintas petugas ditolak (service & HTTP), audit tanpa PII, panel UI
tanpa istilah "ditolak"/"tidak layak".
