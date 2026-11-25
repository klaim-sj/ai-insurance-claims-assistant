# AI Insurance Claims Assistant

Proyek portofolio pribadi. Decision-support tool untuk petugas klaim.
AI TIDAK boleh menyetujui/menolak klaim, menghitung manfaat final, atau
menyimpulkan kelayakan/fraud. Keputusan akhir selalu pada manusia.

## Lingkungan
- Dikembangkan di Claude Code cloud session. PostgreSQL berjalan lokal
  di lingkungan (lihat DATABASE_URL & TEST_DATABASE_URL).
- Sebelum migration/test: cek `pg_isready`; jika belum jalan,
  `service postgresql start`.
- Jangan bergantung pada Docker atau CDN. docker-compose.yml hanya
  opsi untuk development lokal.
- Aset frontend (htmx.min.js, CSS hasil build Tailwind) disimpan di
  web/static dan di-commit, agar aplikasi jalan tanpa Node.js.

## Aturan wajib
- Semua data SINTETIS: NIK diawali 0000, telepon 0800-0000-xxxx,
  email @example.com, nama fiktif. Jangan pernah pakai data nyata.
- Jangan commit .env, API key, atau file upload. Sediakan .env.example.
- Dokumen disimpan di private storage (bukan public), nama file acak.
- Tidak ada PII di log, pesan error, atau analytics.
- Rule engine deterministik; LLM hanya merangkum hasil ekstraksi + aturan.
- Setiap temuan wajib punya evidence reference.
- Setiap perubahan status & aksi penting masuk audit_logs.
- Gunakan istilah "perlu ditinjau", bukan "ditolak"/"tidak layak".

## Stack
Go 1.24, chi, pgx/v5, sqlc, goose, scs (session store PostgreSQL),
argon2id, html/template + HTMX + Tailwind, PostgreSQL 16.
Queue (Tahap 4+): River.

## Konvensi
- Struktur: cmd/server, cmd/seed, internal/<domain>, migrations/,
  db/queries/, web/templates/, web/static/
- Authorization dicek di middleware DAN service layer.
- Test memakai TEST_DATABASE_URL, bukan database development.
- Satu tahap = satu branch = satu PR. Jangan melompat ke tahap berikutnya.
- Sebelum selesai: `go vet ./...`, `go test ./...`, semua harus hijau.

## Acuan
docs/blueprint.pdf
