# Evaluasi OCR, ekstraksi field, dan temuan — dataset utama

> Dataset: `sample-data/` (SELURUHNYA SINTETIS). Hasil ekstraksi dan temuan adalah bahan
> verifikasi petugas ("perlu ditinjau"), bukan keputusan klaim. NIK hanya ditampilkan tersamar.

- Dibuat: 2026-11-26T07:27:26Z
- Extractor: `rules-v1`; engine: pdftotext 24.02.0, tesseract 5.3.4
- Aturan validasi: MISSING_DOCUMENT v1, MISSING_FIELD v1, NAME_MISMATCH v1, POLICY_MISMATCH v1, DATE_INCONSISTENCY v1, LOW_QUALITY_DOCUMENT v1
- Ambang confidence: tinggi ≥ 85, perlu verifikasi ≥ 60, di bawahnya rendah (uncertain)
- Akurasi field = field `found` dengan nilai tepat ÷ field yang punya ground truth. `uncertain`/`not_found` dihitung tidak tepat (tidak menebak, tetapi perlu diperiksa manual).
- Dokumen yang sengaja dibuat tidak terbaca (`expected_unreadable`) tidak masuk akurasi; dihitung terpisah: benar bila sistem tidak menebak.
- Temuan: rule engine dijalankan per skenario. Satuan hitung = (skenario, finding_code): TP bila kode diharapkan dan muncul, FP bila muncul tanpa diharapkan, FN bila diharapkan tetapi tidak muncul. Evidence lengkap = TP yang evidence-nya merujuk semua file sumber yang disebut `expected.json`.

Mode **standar** = alur aplikasi (lapisan teks PDF, Tesseract untuk gambar/halaman tanpa teks). Mode **scan** = setiap halaman PDF dirender 300 dpi lalu dibaca Tesseract (simulasi dokumen hasil scan).

## Mode standar

### Temuan (rule engine)

Precision **100.0%** · recall **100.0%** · TP 6 · FP 0 · FN 0 · evidence lengkap 6/6 TP · temuan ditolak validator evidence 0

| finding_code | TP | FP | FN | Precision | Recall |
|---|---:|---:|---:|---:|---:|
| MISSING_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| MISSING_FIELD | 1 | 0 | 0 | 100.0% | 100.0% |
| NAME_MISMATCH | 1 | 0 | 0 | 100.0% | 100.0% |
| POLICY_MISMATCH | 1 | 0 | 0 | 100.0% | 100.0% |
| DATE_INCONSISTENCY | 1 | 0 | 0 | 100.0% | 100.0% |
| LOW_QUALITY_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |

| Skenario | Diharapkan | Muncul (jumlah) | FP | FN |
|---|---|---|---|---|
| a · Set lengkap dan konsisten | — | — | — | — |
| b · Nama di formulir berbeda dengan identitas | NAME_MISMATCH | NAME_MISMATCH (1) | — | — |
| c · Nomor polis di formulir berbeda dengan data klaim | POLICY_MISMATCH | POLICY_MISMATCH (1) | — | — |
| d · Gambar identitas sengaja buram | LOW_QUALITY_DOCUMENT, MISSING_FIELD | LOW_QUALITY_DOCUMENT (1), MISSING_FIELD (2) | — | — |
| e · Tanggal kuitansi sebelum tanggal kejadian | DATE_INCONSISTENCY | DATE_INCONSISTENCY (1) | — | — |
| f · Kuitansi belum diunggah | MISSING_DOCUMENT | MISSING_DOCUMENT (1) | — | — |

<details><summary>Rincian temuan per skenario (kode, severity, sumber evidence; tanpa nilai)</summary>

- **a**: tidak ada temuan
- **b**: 
  - NAME_MISMATCH [medium] ← b_formulir_klaim_nama_beda.pdf hal.1 claimant_name | 02_identitas.png hal.1 claimant_name
- **c**: 
  - POLICY_MISMATCH [medium] ← c_formulir_klaim_polis_beda.pdf hal.1 policy_number | claim_record:policy_number
- **d**: 
  - MISSING_FIELD [low] ← d_identitas_buram.png hal.1 claimant_name
  - MISSING_FIELD [low] ← d_identitas_buram.png hal.1 nik
  - LOW_QUALITY_DOCUMENT [high] ← d_identitas_buram.png hal.1
- **e**: 
  - DATE_INCONSISTENCY [medium] ← e_kuitansi_tanggal_sebelum_kejadian.pdf hal.1 document_date | 01_formulir_klaim.pdf hal.1 event_date | 03_surat_keterangan_medis.pdf hal.1 event_date
- **f**: 
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type

</details>

### Ekstraksi field

Akurasi keseluruhan **100.0%** (25 field) · uncertain 0 · not_found 0 · salah baca 0 · dokumen tidak terbaca: tidak menebak 2 / menebak 0 · total waktu 894 ms

| Field | Total | Benar | Salah | Uncertain | Not found | Akurasi |
|---|---:|---:|---:|---:|---:|---:|
| claimant_name | 7 | 7 | 0 | 0 | 0 | 100.0% |
| policy_number | 3 | 3 | 0 | 0 | 0 | 100.0% |
| nik | 4 | 4 | 0 | 0 | 0 | 100.0% |
| event_date | 4 | 4 | 0 | 0 | 0 | 100.0% |
| document_date | 2 | 2 | 0 | 0 | 0 | 100.0% |
| amount | 5 | 5 | 0 | 0 | 0 | 100.0% |

| File | Engine | Conf. halaman | Waktu (ms) | Hasil |
|---|---|---|---:|---|
| `01_formulir_klaim.pdf` | pdftotext | 100.0 | 12 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=benar |
| `02_identitas.png` | tesseract | 86.8 | 359 | claimant_name=benar; nik=benar |
| `03_surat_keterangan_medis.pdf` | pdftotext | 100.0 | 13 | claimant_name=benar; event_date=benar |
| `04_kuitansi.pdf` | pdftotext | 100.0 | 10 | claimant_name=benar; document_date=benar; amount=benar |
| `b_formulir_klaim_nama_beda.pdf` | pdftotext | 100.0 | 9 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=benar |
| `c_formulir_klaim_polis_beda.pdf` | pdftotext | 100.0 | 11 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=benar |
| `d_identitas_buram.png` | tesseract | 27.1 | 466 | claimant_name=tidak_menebak; nik=tidak_menebak |
| `e_kuitansi_tanggal_sebelum_kejadian.pdf` | pdftotext | 100.0 | 14 | claimant_name=benar; document_date=benar; amount=benar |

## Mode scan

### Temuan (rule engine)

Precision **100.0%** · recall **100.0%** · TP 6 · FP 0 · FN 0 · evidence lengkap 6/6 TP · temuan ditolak validator evidence 0

| finding_code | TP | FP | FN | Precision | Recall |
|---|---:|---:|---:|---:|---:|
| MISSING_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| MISSING_FIELD | 1 | 0 | 0 | 100.0% | 100.0% |
| NAME_MISMATCH | 1 | 0 | 0 | 100.0% | 100.0% |
| POLICY_MISMATCH | 1 | 0 | 0 | 100.0% | 100.0% |
| DATE_INCONSISTENCY | 1 | 0 | 0 | 100.0% | 100.0% |
| LOW_QUALITY_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |

| Skenario | Diharapkan | Muncul (jumlah) | FP | FN |
|---|---|---|---|---|
| a · Set lengkap dan konsisten | — | — | — | — |
| b · Nama di formulir berbeda dengan identitas | NAME_MISMATCH | NAME_MISMATCH (1) | — | — |
| c · Nomor polis di formulir berbeda dengan data klaim | POLICY_MISMATCH | POLICY_MISMATCH (1) | — | — |
| d · Gambar identitas sengaja buram | LOW_QUALITY_DOCUMENT, MISSING_FIELD | LOW_QUALITY_DOCUMENT (1), MISSING_FIELD (2) | — | — |
| e · Tanggal kuitansi sebelum tanggal kejadian | DATE_INCONSISTENCY | DATE_INCONSISTENCY (1) | — | — |
| f · Kuitansi belum diunggah | MISSING_DOCUMENT | MISSING_DOCUMENT (1) | — | — |

<details><summary>Rincian temuan per skenario (kode, severity, sumber evidence; tanpa nilai)</summary>

- **a**: tidak ada temuan
- **b**: 
  - NAME_MISMATCH [medium] ← b_formulir_klaim_nama_beda.pdf hal.1 claimant_name | 02_identitas.png hal.1 claimant_name
- **c**: 
  - POLICY_MISMATCH [medium] ← c_formulir_klaim_polis_beda.pdf hal.1 policy_number | claim_record:policy_number
- **d**: 
  - MISSING_FIELD [low] ← d_identitas_buram.png hal.1 claimant_name
  - MISSING_FIELD [low] ← d_identitas_buram.png hal.1 nik
  - LOW_QUALITY_DOCUMENT [high] ← d_identitas_buram.png hal.1
- **e**: 
  - DATE_INCONSISTENCY [medium] ← e_kuitansi_tanggal_sebelum_kejadian.pdf hal.1 document_date | 01_formulir_klaim.pdf hal.1 event_date | 03_surat_keterangan_medis.pdf hal.1 event_date
- **f**: 
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type

</details>

### Ekstraksi field

Akurasi keseluruhan **100.0%** (25 field) · uncertain 0 · not_found 0 · salah baca 0 · dokumen tidak terbaca: tidak menebak 2 / menebak 0 · total waktu 8289 ms

| Field | Total | Benar | Salah | Uncertain | Not found | Akurasi |
|---|---:|---:|---:|---:|---:|---:|
| claimant_name | 7 | 7 | 0 | 0 | 0 | 100.0% |
| policy_number | 3 | 3 | 0 | 0 | 0 | 100.0% |
| nik | 4 | 4 | 0 | 0 | 0 | 100.0% |
| event_date | 4 | 4 | 0 | 0 | 0 | 100.0% |
| document_date | 2 | 2 | 0 | 0 | 0 | 100.0% |
| amount | 5 | 5 | 0 | 0 | 0 | 100.0% |

| File | Engine | Conf. halaman | Waktu (ms) | Hasil |
|---|---|---|---:|---|
| `01_formulir_klaim.pdf` | tesseract | 93.5 | 1394 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=benar |
| `02_identitas.png` | tesseract | 86.8 | 307 | claimant_name=benar; nik=benar |
| `03_surat_keterangan_medis.pdf` | tesseract | 94.3 | 1144 | claimant_name=benar; event_date=benar |
| `04_kuitansi.pdf` | tesseract | 93.3 | 1141 | claimant_name=benar; document_date=benar; amount=benar |
| `b_formulir_klaim_nama_beda.pdf` | tesseract | 93.3 | 1339 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=benar |
| `c_formulir_klaim_polis_beda.pdf` | tesseract | 93.5 | 1349 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=benar |
| `d_identitas_buram.png` | tesseract | 27.1 | 416 | claimant_name=tidak_menebak; nik=tidak_menebak |
| `e_kuitansi_tanggal_sebelum_kejadian.pdf` | tesseract | 92.7 | 1199 | claimant_name=benar; document_date=benar; amount=benar |

## Catatan

- **Batas validitas**: dataset utama berasal dari satu generator dengan tata letak rapi, dan aturan ekstraksi/validasi disetel memakai file yang sama. Angka tinggi di sini **tidak** mewakili dokumen nyata. Lihat laporan holdout (`docs/eval/holdout/report.md`) untuk set yang tidak dipakai menyetel.
- `document_date` formulir dan surat keterangan tidak punya ground truth di `expected.json`, sehingga tidak dihitung.
- Ambang confidence adalah nilai awal prototipe; perlu diuji ulang pada dataset yang lebih representatif.
- Laporan dibuat ulang dengan `make eval`.
