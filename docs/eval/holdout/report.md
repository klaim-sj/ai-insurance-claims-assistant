# Evaluasi OCR, ekstraksi field, dan temuan — dataset holdout

> Dataset: `sample-data/holdout/` (SELURUHNYA SINTETIS). Hasil ekstraksi dan temuan adalah bahan
> verifikasi petugas ("perlu ditinjau"), bukan keputusan klaim. NIK hanya ditampilkan tersamar.

**Status set**: Holdout pertama (Tahap 5). Sejak Tahap 5.5 set ini DIPAKAI untuk menyetel pra-pemrosesan OCR dan ekstraksi, sehingga bukan lagi uji independen; dilaporkan sebagai pembanding.

- Dibuat: 2026-11-26T10:03:21Z
- Extractor: `rules-v2`; engine: pdftotext 24.02.0, tesseract 5.3.4
- Aturan validasi: MISSING_DOCUMENT v1, MISSING_FIELD v2, NAME_MISMATCH v2, POLICY_MISMATCH v2, DATE_INCONSISTENCY v1, LOW_QUALITY_DOCUMENT v1, NEEDS_MANUAL_VERIFICATION v1
- Ambang confidence: tinggi ≥ 85, perlu verifikasi ≥ 60, di bawahnya rendah (uncertain)
- Akurasi field = field `found` dengan nilai tepat ÷ field yang punya ground truth. `uncertain`/`not_found` dihitung tidak tepat (tidak menebak, tetapi perlu diperiksa manual).
- Dokumen yang sengaja dibuat tidak terbaca (`expected_unreadable`) tidak masuk akurasi; dihitung terpisah: benar bila sistem tidak menebak.
- Temuan: rule engine dijalankan per skenario. Satuan hitung = (skenario, finding_code): TP bila kode diharapkan dan muncul, FP bila muncul tanpa diharapkan, FN bila diharapkan tetapi tidak muncul. Evidence lengkap = TP yang evidence-nya merujuk semua file sumber yang disebut `expected.json`.

Mode **standar** = alur aplikasi (lapisan teks PDF, Tesseract untuk gambar/halaman tanpa teks). Mode **scan** = setiap halaman PDF dirender 300 dpi lalu dibaca Tesseract (simulasi dokumen hasil scan).

## Mode standar

### Temuan (rule engine)

- Semua kode: precision **54.5%** · recall **85.7%** · TP 6 · FP 5 · FN 1
- Substantif (tanpa `NEEDS_MANUAL_VERIFICATION`): precision **100.0%** · recall **83.3%** · TP 5 · FP 0 · FN 1
- `NEEDS_MANUAL_VERIFICATION`: 10 dari 16 temuan · evidence lengkap 6/6 TP · temuan ditolak validator evidence 0

| finding_code | TP | FP | FN | Precision | Recall |
|---|---:|---:|---:|---:|---:|
| MISSING_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| MISSING_FIELD | 0 | 0 | 0 | n/a | n/a |
| NAME_MISMATCH | 1 | 0 | 1 | 100.0% | 50.0% |
| POLICY_MISMATCH | 1 | 0 | 0 | 100.0% | 100.0% |
| DATE_INCONSISTENCY | 1 | 0 | 0 | 100.0% | 100.0% |
| LOW_QUALITY_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| NEEDS_MANUAL_VERIFICATION | 1 | 5 | 0 | 16.7% | 100.0% |

| Skenario | Diharapkan | Muncul (jumlah) | NMV | FP | FN |
|---|---|---|---:|---|---|
| ha · Set lengkap (nama dengan gelar, tata letak berbeda) | — | NEEDS_MANUAL_VERIFICATION (1) | 1 | NEEDS_MANUAL_VERIFICATION | — |
| hb · Nama kwitansi disingkat | NAME_MISMATCH | NEEDS_MANUAL_VERIFICATION (3) | 3 | NEEDS_MANUAL_VERIFICATION | NAME_MISMATCH |
| hc · Nomor polis formulir berbeda | POLICY_MISMATCH | NEEDS_MANUAL_VERIFICATION (1), POLICY_MISMATCH (1) | 1 | NEEDS_MANUAL_VERIFICATION | — |
| hd · Tanggal kwitansi di masa depan | DATE_INCONSISTENCY | DATE_INCONSISTENCY (1), NEEDS_MANUAL_VERIFICATION (2) | 2 | NEEDS_MANUAL_VERIFICATION | — |
| he · Identitas noise berat | LOW_QUALITY_DOCUMENT, NEEDS_MANUAL_VERIFICATION | LOW_QUALITY_DOCUMENT (1), NEEDS_MANUAL_VERIFICATION (2) | 2 | — | — |
| hf · Nama pasien di surat dokter berbeda | NAME_MISMATCH | NAME_MISMATCH (1), NEEDS_MANUAL_VERIFICATION (1) | 1 | NEEDS_MANUAL_VERIFICATION | — |
| hg · Hanya formulir dan surat dokter | MISSING_DOCUMENT | MISSING_DOCUMENT (2) | 0 | — | — |

<details><summary>Rincian temuan per skenario (kode, severity, sumber evidence; tanpa nilai)</summary>

- **ha**: 
  - NEEDS_MANUAL_VERIFICATION [low] ← h02_identitas_kartu_mono.png hal.1 nik
- **hb**: 
  - NEEDS_MANUAL_VERIFICATION [low] ← h02_identitas_kartu_mono.png hal.1 nik
  - NEEDS_MANUAL_VERIFICATION [low] ← h05_kwitansi_nama_singkat.jpg hal.1 amount
  - NEEDS_MANUAL_VERIFICATION [low] ← h05_kwitansi_nama_singkat.jpg hal.1 claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
- **hc**: 
  - NEEDS_MANUAL_VERIFICATION [low] ← h02_identitas_kartu_mono.png hal.1 nik
  - POLICY_MISMATCH [medium] ← h06_formulir_mono_polis_beda.pdf hal.1 policy_number | claim_record:policy_number
- **hd**: 
  - NEEDS_MANUAL_VERIFICATION [low] ← h02_identitas_kartu_mono.png hal.1 nik
  - NEEDS_MANUAL_VERIFICATION [low] ← h07_kwitansi_tanggal_depan.png hal.1 amount
  - DATE_INCONSISTENCY [medium] ← h07_kwitansi_tanggal_depan.png hal.1 document_date
- **he**: 
  - NEEDS_MANUAL_VERIFICATION [low] ← h08_identitas_noise_berat.jpg hal.1 claimant_name
  - NEEDS_MANUAL_VERIFICATION [low] ← h08_identitas_noise_berat.jpg hal.1 nik
  - LOW_QUALITY_DOCUMENT [high] ← h08_identitas_noise_berat.jpg hal.1
- **hf**: 
  - NEEDS_MANUAL_VERIFICATION [low] ← h02_identitas_kartu_mono.png hal.1 nik
  - NAME_MISMATCH [medium] ← h09_surat_dokter_nama_beda_scan.jpg hal.1 claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
- **hg**: 
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type

</details>

### Ekstraksi field

Akurasi keseluruhan **84.0%** (25 field) · uncertain 3 · not_found 0 · salah baca 1 · dokumen tidak terbaca: tidak menebak 2 / menebak 0 · total waktu 22499 ms (rata-rata 2499 ms/dokumen)

| Field | Total | Benar | Salah | Uncertain | Not found | Akurasi |
|---|---:|---:|---:|---:|---:|---:|
| claimant_name | 8 | 8 | 0 | 0 | 0 | 100.0% |
| policy_number | 2 | 2 | 0 | 0 | 0 | 100.0% |
| nik | 3 | 2 | 0 | 1 | 0 | 66.7% |
| event_date | 4 | 4 | 0 | 0 | 0 | 100.0% |
| document_date | 3 | 3 | 0 | 0 | 0 | 100.0% |
| amount | 5 | 2 | 1 | 2 | 0 | 40.0% |

| File | Engine | Conf. halaman | Waktu (ms) | Hasil |
|---|---|---|---:|---|
| `h01_formulir_klaim_tabel_scan.jpg` | tesseract | 91.5 | 1861 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=salah (dapat "1", harap "1850000") |
| `h02_identitas_kartu_mono.png` | tesseract | 79.1 | 1345 | claimant_name=benar; nik=uncertain |
| `h03_surat_keterangan_dokter.pdf` | pdftotext | 100.0 | 19 | claimant_name=benar; event_date=benar |
| `h04_kwitansi_struk.jpg` | tesseract | 84.0 | 1432 | claimant_name=benar; document_date=benar; amount=benar |
| `h05_kwitansi_nama_singkat.jpg` | tesseract | 82.3 | 1603 | claimant_name=benar; document_date=benar; amount=uncertain |
| `h06_formulir_mono_polis_beda.pdf` | pdftotext | 100.0 | 10 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=benar |
| `h07_kwitansi_tanggal_depan.png` | tesseract | 76.7 | 1459 | claimant_name=benar; document_date=benar; amount=uncertain |
| `h08_identitas_noise_berat.jpg` | tesseract | 40.7 | 13349 | claimant_name=tidak_menebak; nik=tidak_menebak |
| `h09_surat_dokter_nama_beda_scan.jpg` | tesseract | 90.6 | 1421 | claimant_name=benar; event_date=benar |

## Mode scan

### Temuan (rule engine)

- Semua kode: precision **54.5%** · recall **85.7%** · TP 6 · FP 5 · FN 1
- Substantif (tanpa `NEEDS_MANUAL_VERIFICATION`): precision **100.0%** · recall **83.3%** · TP 5 · FP 0 · FN 1
- `NEEDS_MANUAL_VERIFICATION`: 10 dari 16 temuan · evidence lengkap 6/6 TP · temuan ditolak validator evidence 0

| finding_code | TP | FP | FN | Precision | Recall |
|---|---:|---:|---:|---:|---:|
| MISSING_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| MISSING_FIELD | 0 | 0 | 0 | n/a | n/a |
| NAME_MISMATCH | 1 | 0 | 1 | 100.0% | 50.0% |
| POLICY_MISMATCH | 1 | 0 | 0 | 100.0% | 100.0% |
| DATE_INCONSISTENCY | 1 | 0 | 0 | 100.0% | 100.0% |
| LOW_QUALITY_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| NEEDS_MANUAL_VERIFICATION | 1 | 5 | 0 | 16.7% | 100.0% |

| Skenario | Diharapkan | Muncul (jumlah) | NMV | FP | FN |
|---|---|---|---:|---|---|
| ha · Set lengkap (nama dengan gelar, tata letak berbeda) | — | NEEDS_MANUAL_VERIFICATION (1) | 1 | NEEDS_MANUAL_VERIFICATION | — |
| hb · Nama kwitansi disingkat | NAME_MISMATCH | NEEDS_MANUAL_VERIFICATION (3) | 3 | NEEDS_MANUAL_VERIFICATION | NAME_MISMATCH |
| hc · Nomor polis formulir berbeda | POLICY_MISMATCH | NEEDS_MANUAL_VERIFICATION (1), POLICY_MISMATCH (1) | 1 | NEEDS_MANUAL_VERIFICATION | — |
| hd · Tanggal kwitansi di masa depan | DATE_INCONSISTENCY | DATE_INCONSISTENCY (1), NEEDS_MANUAL_VERIFICATION (2) | 2 | NEEDS_MANUAL_VERIFICATION | — |
| he · Identitas noise berat | LOW_QUALITY_DOCUMENT, NEEDS_MANUAL_VERIFICATION | LOW_QUALITY_DOCUMENT (1), NEEDS_MANUAL_VERIFICATION (2) | 2 | — | — |
| hf · Nama pasien di surat dokter berbeda | NAME_MISMATCH | NAME_MISMATCH (1), NEEDS_MANUAL_VERIFICATION (1) | 1 | NEEDS_MANUAL_VERIFICATION | — |
| hg · Hanya formulir dan surat dokter | MISSING_DOCUMENT | MISSING_DOCUMENT (2) | 0 | — | — |

<details><summary>Rincian temuan per skenario (kode, severity, sumber evidence; tanpa nilai)</summary>

- **ha**: 
  - NEEDS_MANUAL_VERIFICATION [low] ← h02_identitas_kartu_mono.png hal.1 nik
- **hb**: 
  - NEEDS_MANUAL_VERIFICATION [low] ← h02_identitas_kartu_mono.png hal.1 nik
  - NEEDS_MANUAL_VERIFICATION [low] ← h05_kwitansi_nama_singkat.jpg hal.1 amount
  - NEEDS_MANUAL_VERIFICATION [low] ← h05_kwitansi_nama_singkat.jpg hal.1 claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
- **hc**: 
  - NEEDS_MANUAL_VERIFICATION [low] ← h02_identitas_kartu_mono.png hal.1 nik
  - POLICY_MISMATCH [medium] ← h06_formulir_mono_polis_beda.pdf hal.1 policy_number | claim_record:policy_number
- **hd**: 
  - NEEDS_MANUAL_VERIFICATION [low] ← h02_identitas_kartu_mono.png hal.1 nik
  - NEEDS_MANUAL_VERIFICATION [low] ← h07_kwitansi_tanggal_depan.png hal.1 amount
  - DATE_INCONSISTENCY [medium] ← h07_kwitansi_tanggal_depan.png hal.1 document_date
- **he**: 
  - NEEDS_MANUAL_VERIFICATION [low] ← h08_identitas_noise_berat.jpg hal.1 claimant_name
  - NEEDS_MANUAL_VERIFICATION [low] ← h08_identitas_noise_berat.jpg hal.1 nik
  - LOW_QUALITY_DOCUMENT [high] ← h08_identitas_noise_berat.jpg hal.1
- **hf**: 
  - NEEDS_MANUAL_VERIFICATION [low] ← h02_identitas_kartu_mono.png hal.1 nik
  - NAME_MISMATCH [medium] ← h09_surat_dokter_nama_beda_scan.jpg hal.1 claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
- **hg**: 
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type

</details>

### Ekstraksi field

Akurasi keseluruhan **84.0%** (25 field) · uncertain 3 · not_found 0 · salah baca 1 · dokumen tidak terbaca: tidak menebak 2 / menebak 0 · total waktu 26014 ms (rata-rata 2890 ms/dokumen)

| Field | Total | Benar | Salah | Uncertain | Not found | Akurasi |
|---|---:|---:|---:|---:|---:|---:|
| claimant_name | 8 | 8 | 0 | 0 | 0 | 100.0% |
| policy_number | 2 | 2 | 0 | 0 | 0 | 100.0% |
| nik | 3 | 2 | 0 | 1 | 0 | 66.7% |
| event_date | 4 | 4 | 0 | 0 | 0 | 100.0% |
| document_date | 3 | 3 | 0 | 0 | 0 | 100.0% |
| amount | 5 | 2 | 1 | 2 | 0 | 40.0% |

| File | Engine | Conf. halaman | Waktu (ms) | Hasil |
|---|---|---|---:|---|
| `h01_formulir_klaim_tabel_scan.jpg` | tesseract | 91.5 | 1827 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=salah (dapat "1", harap "1850000") |
| `h02_identitas_kartu_mono.png` | tesseract | 79.1 | 1309 | claimant_name=benar; nik=uncertain |
| `h03_surat_keterangan_dokter.pdf` | tesseract | 95.7 | 1796 | claimant_name=benar; event_date=benar |
| `h04_kwitansi_struk.jpg` | tesseract | 84.0 | 1452 | claimant_name=benar; document_date=benar; amount=benar |
| `h05_kwitansi_nama_singkat.jpg` | tesseract | 82.3 | 1408 | claimant_name=benar; document_date=benar; amount=uncertain |
| `h06_formulir_mono_polis_beda.pdf` | tesseract | 94.4 | 1880 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=benar |
| `h07_kwitansi_tanggal_depan.png` | tesseract | 76.7 | 1446 | claimant_name=benar; document_date=benar; amount=uncertain |
| `h08_identitas_noise_berat.jpg` | tesseract | 40.7 | 13325 | claimant_name=tidak_menebak; nik=tidak_menebak |
| `h09_surat_dokter_nama_beda_scan.jpg` | tesseract | 90.6 | 1571 | claimant_name=benar; event_date=benar |

## Perbandingan konfigurasi OCR — formulir klaim berbentuk gambar (1 file)

Akurasi field formulir (nama, polis, NIK, tanggal kejadian, nominal) per konfigurasi; hanya untuk laporan, konfigurasi produksi tidak dipilih dari set holdout.

| Konfigurasi | Field benar | Akurasi | Waktu total (ms) |
|---|---:|---:|---:|
| tanpa pra-pemrosesan, PSM 6 (Tahap 5) | 0/5 | 0.0% | 457 |
| pra-pemrosesan, PSM 3 | 2/5 | 40.0% | 1255 |
| pra-pemrosesan, PSM 4 | 2/5 | 40.0% | 1181 |
| pra-pemrosesan, PSM 6 | 1/5 | 20.0% | 1357 |
| pra-pemrosesan, PSM 11 | 4/5 | 80.0% | 1192 |
| pra-pemrosesan, PSM 6+11 (produksi) | 4/5 | 80.0% | 1826 |

## Catatan

- Set holdout kecil (puluhan field, beberapa skenario); satu dokumen dapat menggeser persentase secara besar. Gunakan sebagai sinyal arah, bukan ukuran kinerja produksi.
- Kegagalan di sini sengaja **tidak** diperbaiki dengan menyetel aturan pada set yang sama; perbaikan harus divalidasi pada set uji baru.
- Konfigurasi OCR: pra-pemrosesan Go (normalisasi resolusi, kontras, median 3×3, deskew, Otsu, hapus garis tabel) + Tesseract PSM 6,11 (hasil berskor tertinggi). Gating confidence: NAME/POLICY_MISMATCH hanya bila kedua nilai ≥ 85; selain itu `NEEDS_MANUAL_VERIFICATION`.
- Ambang confidence adalah nilai awal prototipe; perlu diuji ulang pada dataset yang lebih representatif.
- Laporan dibuat ulang dengan `make eval`.
