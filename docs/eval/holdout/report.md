# Evaluasi OCR, ekstraksi field, dan temuan — dataset holdout

> Dataset: `sample-data/holdout/` (SELURUHNYA SINTETIS). Hasil ekstraksi dan temuan adalah bahan
> verifikasi petugas ("perlu ditinjau"), bukan keputusan klaim. NIK hanya ditampilkan tersamar.

**Set holdout**: dokumen di set ini dibuat dengan tata letak, font, kemiringan, dan noise berbeda dan **tidak dipakai untuk menyetel** aturan ekstraksi maupun aturan validasi. Aturan dibekukan sebelum set ini dijalankan pertama kali; angka di bawah dilaporkan apa adanya.

- Dibuat: 2026-11-26T07:27:36Z
- Extractor: `rules-v1`; engine: pdftotext 24.02.0, tesseract 5.3.4
- Aturan validasi: MISSING_DOCUMENT v1, MISSING_FIELD v1, NAME_MISMATCH v1, POLICY_MISMATCH v1, DATE_INCONSISTENCY v1, LOW_QUALITY_DOCUMENT v1
- Ambang confidence: tinggi ≥ 85, perlu verifikasi ≥ 60, di bawahnya rendah (uncertain)
- Akurasi field = field `found` dengan nilai tepat ÷ field yang punya ground truth. `uncertain`/`not_found` dihitung tidak tepat (tidak menebak, tetapi perlu diperiksa manual).
- Dokumen yang sengaja dibuat tidak terbaca (`expected_unreadable`) tidak masuk akurasi; dihitung terpisah: benar bila sistem tidak menebak.
- Temuan: rule engine dijalankan per skenario. Satuan hitung = (skenario, finding_code): TP bila kode diharapkan dan muncul, FP bila muncul tanpa diharapkan, FN bila diharapkan tetapi tidak muncul. Evidence lengkap = TP yang evidence-nya merujuk semua file sumber yang disebut `expected.json`.

Mode **standar** = alur aplikasi (lapisan teks PDF, Tesseract untuk gambar/halaman tanpa teks). Mode **scan** = setiap halaman PDF dirender 300 dpi lalu dibaca Tesseract (simulasi dokumen hasil scan).

## Mode standar

### Temuan (rule engine)

Precision **37.5%** · recall **85.7%** · TP 6 · FP 10 · FN 1 · evidence lengkap 4/6 TP · temuan ditolak validator evidence 0

| finding_code | TP | FP | FN | Precision | Recall |
|---|---:|---:|---:|---:|---:|
| MISSING_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| MISSING_FIELD | 1 | 6 | 0 | 14.3% | 100.0% |
| NAME_MISMATCH | 2 | 3 | 0 | 40.0% | 100.0% |
| POLICY_MISMATCH | 1 | 0 | 0 | 100.0% | 100.0% |
| DATE_INCONSISTENCY | 0 | 0 | 1 | n/a | 0.0% |
| LOW_QUALITY_DOCUMENT | 1 | 1 | 0 | 50.0% | 100.0% |

| Skenario | Diharapkan | Muncul (jumlah) | FP | FN |
|---|---|---|---|---|
| ha · Set lengkap (nama dengan gelar, tata letak berbeda) | — | MISSING_FIELD (6), NAME_MISMATCH (2) | MISSING_FIELD, NAME_MISMATCH | — |
| hb · Nama kwitansi disingkat | NAME_MISMATCH | MISSING_FIELD (6), NAME_MISMATCH (2) | MISSING_FIELD | — |
| hc · Nomor polis formulir berbeda | POLICY_MISMATCH | MISSING_FIELD (3), NAME_MISMATCH (3), POLICY_MISMATCH (1) | MISSING_FIELD, NAME_MISMATCH | — |
| hd · Tanggal kwitansi di masa depan | DATE_INCONSISTENCY | LOW_QUALITY_DOCUMENT (1), MISSING_FIELD (7), NAME_MISMATCH (2) | LOW_QUALITY_DOCUMENT, MISSING_FIELD, NAME_MISMATCH | DATE_INCONSISTENCY |
| he · Identitas noise berat | LOW_QUALITY_DOCUMENT, MISSING_FIELD | LOW_QUALITY_DOCUMENT (1), MISSING_FIELD (7) | — | — |
| hf · Nama pasien di surat dokter berbeda | NAME_MISMATCH | MISSING_FIELD (8), NAME_MISMATCH (1) | MISSING_FIELD | — |
| hg · Hanya formulir dan surat dokter | MISSING_DOCUMENT | MISSING_DOCUMENT (2), MISSING_FIELD (3) | MISSING_FIELD | — |

<details><summary>Rincian temuan per skenario (kode, severity, sumber evidence; tanpa nilai)</summary>

- **ha**: 
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 policy_number
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← h02_identitas_kartu_mono.png hal.1 nik
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 document_date
  - NAME_MISMATCH [low] ← claim_record:claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - NAME_MISMATCH [low] ← h03_surat_keterangan_dokter.pdf hal.1 claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
- **hb**: 
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 policy_number
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← h02_identitas_kartu_mono.png hal.1 nik
  - MISSING_FIELD [medium] ← h05_kwitansi_nama_singkat.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h05_kwitansi_nama_singkat.jpg hal.1 amount
  - NAME_MISMATCH [low] ← claim_record:claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - NAME_MISMATCH [low] ← h03_surat_keterangan_dokter.pdf hal.1 claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - evidence tidak lengkap untuk: NAME_MISMATCH
- **hc**: 
  - MISSING_FIELD [medium] ← h02_identitas_kartu_mono.png hal.1 nik
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 document_date
  - NAME_MISMATCH [low] ← claim_record:claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - NAME_MISMATCH [low] ← h06_formulir_mono_polis_beda.pdf hal.1 claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - NAME_MISMATCH [low] ← h03_surat_keterangan_dokter.pdf hal.1 claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - POLICY_MISMATCH [medium] ← h06_formulir_mono_polis_beda.pdf hal.1 policy_number | claim_record:policy_number
- **hd**: 
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 policy_number
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← h02_identitas_kartu_mono.png hal.1 nik
  - MISSING_FIELD [low] ← h07_kwitansi_tanggal_depan.png hal.1 claimant_name
  - MISSING_FIELD [low] ← h07_kwitansi_tanggal_depan.png hal.1 document_date
  - MISSING_FIELD [low] ← h07_kwitansi_tanggal_depan.png hal.1 amount
  - NAME_MISMATCH [low] ← claim_record:claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - NAME_MISMATCH [low] ← h03_surat_keterangan_dokter.pdf hal.1 claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - LOW_QUALITY_DOCUMENT [high] ← h07_kwitansi_tanggal_depan.png hal.1
- **he**: 
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 policy_number
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 event_date
  - MISSING_FIELD [low] ← h08_identitas_noise_berat.jpg hal.1 claimant_name
  - MISSING_FIELD [low] ← h08_identitas_noise_berat.jpg hal.1 nik
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 document_date
  - LOW_QUALITY_DOCUMENT [high] ← h08_identitas_noise_berat.jpg hal.1
- **hf**: 
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 policy_number
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← h02_identitas_kartu_mono.png hal.1 nik
  - MISSING_FIELD [medium] ← h09_surat_dokter_nama_beda_scan.jpg hal.1 claimant_name
  - MISSING_FIELD [low] ← h09_surat_dokter_nama_beda_scan.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 document_date
  - NAME_MISMATCH [low] ← claim_record:claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - evidence tidak lengkap untuk: NAME_MISMATCH
- **hg**: 
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 policy_number
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 event_date

</details>

### Ekstraksi field

Akurasi keseluruhan **32.0%** (25 field) · uncertain 4 · not_found 12 · salah baca 1 · dokumen tidak terbaca: tidak menebak 2 / menebak 0 · total waktu 9880 ms

| Field | Total | Benar | Salah | Uncertain | Not found | Akurasi |
|---|---:|---:|---:|---:|---:|---:|
| claimant_name | 8 | 2 | 1 | 1 | 4 | 25.0% |
| policy_number | 2 | 1 | 0 | 0 | 1 | 50.0% |
| nik | 3 | 1 | 0 | 0 | 2 | 33.3% |
| event_date | 4 | 2 | 0 | 1 | 1 | 50.0% |
| document_date | 3 | 1 | 0 | 1 | 1 | 33.3% |
| amount | 5 | 1 | 0 | 1 | 3 | 20.0% |

| File | Engine | Conf. halaman | Waktu (ms) | Hasil |
|---|---|---|---:|---|
| `h01_formulir_klaim_tabel_scan.jpg` | tesseract | 65.3 | 394 | claimant_name=not_found; policy_number=not_found; nik=not_found; event_date=not_found; amount=not_found |
| `h02_identitas_kartu_mono.png` | tesseract | 65.2 | 375 | claimant_name=salah (dapat "Budi Santoso", harap "Budi Santoso Pratama"); nik=not_found |
| `h03_surat_keterangan_dokter.pdf` | pdftotext | 100.0 | 13 | claimant_name=benar; event_date=benar |
| `h04_kwitansi_struk.jpg` | tesseract | 62.5 | 597 | claimant_name=not_found; document_date=not_found; amount=benar |
| `h05_kwitansi_nama_singkat.jpg` | tesseract | 64.8 | 460 | claimant_name=not_found; document_date=benar; amount=not_found |
| `h06_formulir_mono_polis_beda.pdf` | pdftotext | 100.0 | 13 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=not_found |
| `h07_kwitansi_tanggal_depan.png` | tesseract | 46.8 | 563 | claimant_name=uncertain; document_date=uncertain; amount=uncertain |
| `h08_identitas_noise_berat.jpg` | tesseract | 15.1 | 6917 | claimant_name=tidak_menebak; nik=tidak_menebak |
| `h09_surat_dokter_nama_beda_scan.jpg` | tesseract | 70.1 | 548 | claimant_name=not_found; event_date=uncertain |

## Mode scan

### Temuan (rule engine)

Precision **37.5%** · recall **85.7%** · TP 6 · FP 10 · FN 1 · evidence lengkap 4/6 TP · temuan ditolak validator evidence 0

| finding_code | TP | FP | FN | Precision | Recall |
|---|---:|---:|---:|---:|---:|
| MISSING_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| MISSING_FIELD | 1 | 6 | 0 | 14.3% | 100.0% |
| NAME_MISMATCH | 2 | 3 | 0 | 40.0% | 100.0% |
| POLICY_MISMATCH | 1 | 0 | 0 | 100.0% | 100.0% |
| DATE_INCONSISTENCY | 0 | 0 | 1 | n/a | 0.0% |
| LOW_QUALITY_DOCUMENT | 1 | 1 | 0 | 50.0% | 100.0% |

| Skenario | Diharapkan | Muncul (jumlah) | FP | FN |
|---|---|---|---|---|
| ha · Set lengkap (nama dengan gelar, tata letak berbeda) | — | MISSING_FIELD (6), NAME_MISMATCH (2) | MISSING_FIELD, NAME_MISMATCH | — |
| hb · Nama kwitansi disingkat | NAME_MISMATCH | MISSING_FIELD (6), NAME_MISMATCH (2) | MISSING_FIELD | — |
| hc · Nomor polis formulir berbeda | POLICY_MISMATCH | MISSING_FIELD (3), NAME_MISMATCH (3), POLICY_MISMATCH (1) | MISSING_FIELD, NAME_MISMATCH | — |
| hd · Tanggal kwitansi di masa depan | DATE_INCONSISTENCY | LOW_QUALITY_DOCUMENT (1), MISSING_FIELD (7), NAME_MISMATCH (2) | LOW_QUALITY_DOCUMENT, MISSING_FIELD, NAME_MISMATCH | DATE_INCONSISTENCY |
| he · Identitas noise berat | LOW_QUALITY_DOCUMENT, MISSING_FIELD | LOW_QUALITY_DOCUMENT (1), MISSING_FIELD (7) | — | — |
| hf · Nama pasien di surat dokter berbeda | NAME_MISMATCH | MISSING_FIELD (8), NAME_MISMATCH (1) | MISSING_FIELD | — |
| hg · Hanya formulir dan surat dokter | MISSING_DOCUMENT | MISSING_DOCUMENT (2), MISSING_FIELD (3) | MISSING_FIELD | — |

<details><summary>Rincian temuan per skenario (kode, severity, sumber evidence; tanpa nilai)</summary>

- **ha**: 
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 policy_number
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← h02_identitas_kartu_mono.png hal.1 nik
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 document_date
  - NAME_MISMATCH [low] ← claim_record:claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - NAME_MISMATCH [low] ← h03_surat_keterangan_dokter.pdf hal.1 claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
- **hb**: 
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 policy_number
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← h02_identitas_kartu_mono.png hal.1 nik
  - MISSING_FIELD [medium] ← h05_kwitansi_nama_singkat.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h05_kwitansi_nama_singkat.jpg hal.1 amount
  - NAME_MISMATCH [low] ← claim_record:claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - NAME_MISMATCH [low] ← h03_surat_keterangan_dokter.pdf hal.1 claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - evidence tidak lengkap untuk: NAME_MISMATCH
- **hc**: 
  - MISSING_FIELD [medium] ← h02_identitas_kartu_mono.png hal.1 nik
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 document_date
  - NAME_MISMATCH [low] ← claim_record:claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - NAME_MISMATCH [low] ← h06_formulir_mono_polis_beda.pdf hal.1 claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - NAME_MISMATCH [low] ← h03_surat_keterangan_dokter.pdf hal.1 claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - POLICY_MISMATCH [medium] ← h06_formulir_mono_polis_beda.pdf hal.1 policy_number | claim_record:policy_number
- **hd**: 
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 policy_number
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← h02_identitas_kartu_mono.png hal.1 nik
  - MISSING_FIELD [low] ← h07_kwitansi_tanggal_depan.png hal.1 claimant_name
  - MISSING_FIELD [low] ← h07_kwitansi_tanggal_depan.png hal.1 document_date
  - MISSING_FIELD [low] ← h07_kwitansi_tanggal_depan.png hal.1 amount
  - NAME_MISMATCH [low] ← claim_record:claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - NAME_MISMATCH [low] ← h03_surat_keterangan_dokter.pdf hal.1 claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - LOW_QUALITY_DOCUMENT [high] ← h07_kwitansi_tanggal_depan.png hal.1
- **he**: 
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 policy_number
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 event_date
  - MISSING_FIELD [low] ← h08_identitas_noise_berat.jpg hal.1 claimant_name
  - MISSING_FIELD [low] ← h08_identitas_noise_berat.jpg hal.1 nik
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 document_date
  - LOW_QUALITY_DOCUMENT [high] ← h08_identitas_noise_berat.jpg hal.1
- **hf**: 
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 policy_number
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← h02_identitas_kartu_mono.png hal.1 nik
  - MISSING_FIELD [medium] ← h09_surat_dokter_nama_beda_scan.jpg hal.1 claimant_name
  - MISSING_FIELD [low] ← h09_surat_dokter_nama_beda_scan.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h04_kwitansi_struk.jpg hal.1 document_date
  - NAME_MISMATCH [low] ← claim_record:claimant_name | h02_identitas_kartu_mono.png hal.1 claimant_name
  - evidence tidak lengkap untuk: NAME_MISMATCH
- **hg**: 
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 policy_number
  - MISSING_FIELD [medium] ← h01_formulir_klaim_tabel_scan.jpg hal.1 event_date

</details>

### Ekstraksi field

Akurasi keseluruhan **32.0%** (25 field) · uncertain 4 · not_found 12 · salah baca 1 · dokumen tidak terbaca: tidak menebak 2 / menebak 0 · total waktu 12321 ms

| Field | Total | Benar | Salah | Uncertain | Not found | Akurasi |
|---|---:|---:|---:|---:|---:|---:|
| claimant_name | 8 | 2 | 1 | 1 | 4 | 25.0% |
| policy_number | 2 | 1 | 0 | 0 | 1 | 50.0% |
| nik | 3 | 1 | 0 | 0 | 2 | 33.3% |
| event_date | 4 | 2 | 0 | 1 | 1 | 50.0% |
| document_date | 3 | 1 | 0 | 1 | 1 | 33.3% |
| amount | 5 | 1 | 0 | 1 | 3 | 20.0% |

| File | Engine | Conf. halaman | Waktu (ms) | Hasil |
|---|---|---|---:|---|
| `h01_formulir_klaim_tabel_scan.jpg` | tesseract | 65.3 | 373 | claimant_name=not_found; policy_number=not_found; nik=not_found; event_date=not_found; amount=not_found |
| `h02_identitas_kartu_mono.png` | tesseract | 65.2 | 336 | claimant_name=salah (dapat "Budi Santoso", harap "Budi Santoso Pratama"); nik=not_found |
| `h03_surat_keterangan_dokter.pdf` | tesseract | 95.5 | 1105 | claimant_name=benar; event_date=benar |
| `h04_kwitansi_struk.jpg` | tesseract | 62.5 | 508 | claimant_name=not_found; document_date=not_found; amount=benar |
| `h05_kwitansi_nama_singkat.jpg` | tesseract | 64.8 | 525 | claimant_name=not_found; document_date=benar; amount=not_found |
| `h06_formulir_mono_polis_beda.pdf` | tesseract | 93.3 | 1228 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=not_found |
| `h07_kwitansi_tanggal_depan.png` | tesseract | 46.8 | 660 | claimant_name=uncertain; document_date=uncertain; amount=uncertain |
| `h08_identitas_noise_berat.jpg` | tesseract | 15.1 | 7001 | claimant_name=tidak_menebak; nik=tidak_menebak |
| `h09_surat_dokter_nama_beda_scan.jpg` | tesseract | 70.1 | 585 | claimant_name=not_found; event_date=uncertain |

## Catatan

- Set holdout kecil (puluhan field, beberapa skenario); satu dokumen dapat menggeser persentase secara besar. Gunakan sebagai sinyal arah, bukan ukuran kinerja produksi.
- Kegagalan di sini sengaja **tidak** diperbaiki dengan menyetel aturan pada set yang sama; perbaikan harus divalidasi pada set uji baru.
- Ambang confidence adalah nilai awal prototipe; perlu diuji ulang pada dataset yang lebih representatif.
- Laporan dibuat ulang dengan `make eval`.
