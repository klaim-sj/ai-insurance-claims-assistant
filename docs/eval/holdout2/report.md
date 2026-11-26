# Evaluasi OCR, ekstraksi field, dan temuan — dataset holdout2

> Dataset: `sample-data/holdout2/` (SELURUHNYA SINTETIS). Hasil ekstraksi dan temuan adalah bahan
> verifikasi petugas ("perlu ditinjau"), bukan keputusan klaim. NIK hanya ditampilkan tersamar.

**Status set**: Holdout2 dibuat dan di-commit sebelum perbaikan Tahap 5.5 ditulis, tidak dipakai menyetel, dan dijalankan dengan kode final sekali; baseline kode Tahap 5 ada di docs/eval/holdout2-baseline/. Dilaporkan apa adanya.

- Dibuat: 2026-11-26T10:04:17Z
- Extractor: `rules-v2`; engine: pdftotext 24.02.0, tesseract 5.3.4
- Aturan validasi: MISSING_DOCUMENT v1, MISSING_FIELD v2, NAME_MISMATCH v2, POLICY_MISMATCH v2, DATE_INCONSISTENCY v1, LOW_QUALITY_DOCUMENT v1, NEEDS_MANUAL_VERIFICATION v1
- Ambang confidence: tinggi ≥ 85, perlu verifikasi ≥ 60, di bawahnya rendah (uncertain)
- Akurasi field = field `found` dengan nilai tepat ÷ field yang punya ground truth. `uncertain`/`not_found` dihitung tidak tepat (tidak menebak, tetapi perlu diperiksa manual).
- Dokumen yang sengaja dibuat tidak terbaca (`expected_unreadable`) tidak masuk akurasi; dihitung terpisah: benar bila sistem tidak menebak.
- Temuan: rule engine dijalankan per skenario. Satuan hitung = (skenario, finding_code): TP bila kode diharapkan dan muncul, FP bila muncul tanpa diharapkan, FN bila diharapkan tetapi tidak muncul. Evidence lengkap = TP yang evidence-nya merujuk semua file sumber yang disebut `expected.json`.

Mode **standar** = alur aplikasi (lapisan teks PDF, Tesseract untuk gambar/halaman tanpa teks). Mode **scan** = setiap halaman PDF dirender 300 dpi lalu dibaca Tesseract (simulasi dokumen hasil scan).

## Mode standar

### Temuan (rule engine)

- Semua kode: precision **35.3%** · recall **85.7%** · TP 6 · FP 11 · FN 1
- Substantif (tanpa `NEEDS_MANUAL_VERIFICATION`): precision **38.5%** · recall **83.3%** · TP 5 · FP 8 · FN 1
- `NEEDS_MANUAL_VERIFICATION`: 6 dari 42 temuan · evidence lengkap 6/6 TP · temuan ditolak validator evidence 0

| finding_code | TP | FP | FN | Precision | Recall |
|---|---:|---:|---:|---:|---:|
| MISSING_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| MISSING_FIELD | 0 | 7 | 0 | 0.0% | n/a |
| NAME_MISMATCH | 2 | 0 | 0 | 100.0% | 100.0% |
| POLICY_MISMATCH | 1 | 1 | 0 | 50.0% | 100.0% |
| DATE_INCONSISTENCY | 0 | 0 | 1 | n/a | 0.0% |
| LOW_QUALITY_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| NEEDS_MANUAL_VERIFICATION | 1 | 3 | 0 | 25.0% | 100.0% |

| Skenario | Diharapkan | Muncul (jumlah) | NMV | FP | FN |
|---|---|---|---:|---|---|
| ga · Set lengkap (foto HP, resolusi rendah, scan 2 halaman, latar warna) | — | MISSING_FIELD (5), NEEDS_MANUAL_VERIFICATION (1) | 1 | MISSING_FIELD, NEEDS_MANUAL_VERIFICATION | — |
| gb · Formulir dua kolom, nama bergelar dan disingkat | NAME_MISMATCH | MISSING_FIELD (4), NAME_MISMATCH (1), POLICY_MISMATCH (1) | 0 | MISSING_FIELD, POLICY_MISMATCH | — |
| gc · Tanggal bukti bayar sebelum tanggal kejadian | DATE_INCONSISTENCY | MISSING_FIELD (5), NEEDS_MANUAL_VERIFICATION (1) | 1 | MISSING_FIELD, NEEDS_MANUAL_VERIFICATION | DATE_INCONSISTENCY |
| gd · Nama pasien di resume medis berbeda | NAME_MISMATCH | MISSING_FIELD (5), NAME_MISMATCH (1), NEEDS_MANUAL_VERIFICATION (1) | 1 | MISSING_FIELD, NEEDS_MANUAL_VERIFICATION | — |
| ge · Nomor polis formulir tabel berbeda | POLICY_MISMATCH | MISSING_FIELD (3), POLICY_MISMATCH (1) | 0 | MISSING_FIELD | — |
| gf · Identitas gelap dan buram | LOW_QUALITY_DOCUMENT, NEEDS_MANUAL_VERIFICATION | LOW_QUALITY_DOCUMENT (1), MISSING_FIELD (5), NEEDS_MANUAL_VERIFICATION (3) | 3 | MISSING_FIELD | — |
| gg · Hanya identitas dan bukti bayar | MISSING_DOCUMENT | MISSING_DOCUMENT (2), MISSING_FIELD (2) | 0 | MISSING_FIELD | — |

<details><summary>Rincian temuan per skenario (kode, severity, sumber evidence; tanpa nilai)</summary>

- **ga**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
  - NEEDS_MANUAL_VERIFICATION [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number | claim_record:policy_number
- **gb**: 
  - MISSING_FIELD [medium] ← g05_formulir_dua_kolom_nama_gelar.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
  - NAME_MISMATCH [medium] ← g05_formulir_dua_kolom_nama_gelar.jpg hal.1 claimant_name | g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - POLICY_MISMATCH [medium] ← g05_formulir_dua_kolom_nama_gelar.jpg hal.1 policy_number | claim_record:policy_number
- **gc**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g06_bukti_bayar_tanggal_sebelum.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g06_bukti_bayar_tanggal_sebelum.png hal.1 amount
  - NEEDS_MANUAL_VERIFICATION [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number | claim_record:policy_number
- **gd**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g07_resume_medis_nama_beda.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
  - NAME_MISMATCH [medium] ← g07_resume_medis_nama_beda.pdf hal.1 claimant_name | g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - NEEDS_MANUAL_VERIFICATION [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number | claim_record:policy_number
- **ge**: 
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
  - POLICY_MISMATCH [medium] ← g08_formulir_tabel_polis_beda.png hal.1 policy_number | claim_record:policy_number
- **gf**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - NEEDS_MANUAL_VERIFICATION [low] ← g09_identitas_gelap_buram.jpg hal.1 claimant_name
  - NEEDS_MANUAL_VERIFICATION [low] ← g09_identitas_gelap_buram.jpg hal.1 nik
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
  - NEEDS_MANUAL_VERIFICATION [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number | claim_record:policy_number
  - LOW_QUALITY_DOCUMENT [high] ← g09_identitas_gelap_buram.jpg hal.1
- **gg**: 
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount

</details>

### Ekstraksi field

Akurasi keseluruhan **33.3%** (27 field) · uncertain 0 · not_found 15 · salah baca 3 · dokumen tidak terbaca: tidak menebak 2 / menebak 0 · total waktu 61671 ms (rata-rata 6852 ms/dokumen)

| Field | Total | Benar | Salah | Uncertain | Not found | Akurasi |
|---|---:|---:|---:|---:|---:|---:|
| claimant_name | 8 | 4 | 1 | 0 | 3 | 50.0% |
| policy_number | 3 | 1 | 2 | 0 | 0 | 33.3% |
| nik | 4 | 1 | 0 | 0 | 3 | 25.0% |
| event_date | 5 | 1 | 0 | 0 | 4 | 20.0% |
| document_date | 2 | 2 | 0 | 0 | 0 | 100.0% |
| amount | 5 | 0 | 0 | 0 | 5 | 0.0% |

| File | Engine | Conf. halaman | Waktu (ms) | Hasil |
|---|---|---|---:|---|
| `g01_formulir_foto_hp.jpg` | tesseract | 83.7 | 1504 | claimant_name=not_found; policy_number=salah (dapat "9000********0003", harap "POL-UJI-3315"); nik=not_found; event_date=not_found; amount=not_found |
| `g02_identitas_resolusi_rendah.png` | tesseract | 82.3 | 771 | claimant_name=benar; nik=benar |
| `g03_resume_medis_scan_2hal.pdf` | tesseract, tesseract | 85.7, 87.4 | 6687 | claimant_name=benar; event_date=not_found |
| `g04_bukti_bayar_latar_kuning.jpg` | tesseract | 84.9 | 1599 | claimant_name=not_found; document_date=benar; amount=not_found |
| `g05_formulir_dua_kolom_nama_gelar.jpg` | tesseract | 94.3 | 1531 | claimant_name=salah (dapat "Tgi Kejadian", harap "Hj. S. Aminah Rahayu, S.E."); policy_number=salah (dapat "RUMAHSAKIT", harap "POL-UJI-3315"); nik=not_found; event_date=not_found; amount=not_found |
| `g06_bukti_bayar_tanggal_sebelum.png` | tesseract | 94.5 | 1437 | claimant_name=not_found; document_date=benar; amount=not_found |
| `g07_resume_medis_nama_beda.pdf` | pdftotext | 100.0 | 9 | claimant_name=benar; event_date=not_found |
| `g08_formulir_tabel_polis_beda.png` | tesseract | 93.7 | 1672 | claimant_name=benar; policy_number=benar; nik=not_found; event_date=benar; amount=not_found |
| `g09_identitas_gelap_buram.jpg` | tesseract | 35.1 | 46461 | claimant_name=tidak_menebak; nik=tidak_menebak |

## Mode scan

### Temuan (rule engine)

- Semua kode: precision **35.3%** · recall **85.7%** · TP 6 · FP 11 · FN 1
- Substantif (tanpa `NEEDS_MANUAL_VERIFICATION`): precision **38.5%** · recall **83.3%** · TP 5 · FP 8 · FN 1
- `NEEDS_MANUAL_VERIFICATION`: 6 dari 42 temuan · evidence lengkap 6/6 TP · temuan ditolak validator evidence 0

| finding_code | TP | FP | FN | Precision | Recall |
|---|---:|---:|---:|---:|---:|
| MISSING_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| MISSING_FIELD | 0 | 7 | 0 | 0.0% | n/a |
| NAME_MISMATCH | 2 | 0 | 0 | 100.0% | 100.0% |
| POLICY_MISMATCH | 1 | 1 | 0 | 50.0% | 100.0% |
| DATE_INCONSISTENCY | 0 | 0 | 1 | n/a | 0.0% |
| LOW_QUALITY_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| NEEDS_MANUAL_VERIFICATION | 1 | 3 | 0 | 25.0% | 100.0% |

| Skenario | Diharapkan | Muncul (jumlah) | NMV | FP | FN |
|---|---|---|---:|---|---|
| ga · Set lengkap (foto HP, resolusi rendah, scan 2 halaman, latar warna) | — | MISSING_FIELD (5), NEEDS_MANUAL_VERIFICATION (1) | 1 | MISSING_FIELD, NEEDS_MANUAL_VERIFICATION | — |
| gb · Formulir dua kolom, nama bergelar dan disingkat | NAME_MISMATCH | MISSING_FIELD (4), NAME_MISMATCH (1), POLICY_MISMATCH (1) | 0 | MISSING_FIELD, POLICY_MISMATCH | — |
| gc · Tanggal bukti bayar sebelum tanggal kejadian | DATE_INCONSISTENCY | MISSING_FIELD (5), NEEDS_MANUAL_VERIFICATION (1) | 1 | MISSING_FIELD, NEEDS_MANUAL_VERIFICATION | DATE_INCONSISTENCY |
| gd · Nama pasien di resume medis berbeda | NAME_MISMATCH | MISSING_FIELD (5), NAME_MISMATCH (1), NEEDS_MANUAL_VERIFICATION (1) | 1 | MISSING_FIELD, NEEDS_MANUAL_VERIFICATION | — |
| ge · Nomor polis formulir tabel berbeda | POLICY_MISMATCH | MISSING_FIELD (3), POLICY_MISMATCH (1) | 0 | MISSING_FIELD | — |
| gf · Identitas gelap dan buram | LOW_QUALITY_DOCUMENT, NEEDS_MANUAL_VERIFICATION | LOW_QUALITY_DOCUMENT (1), MISSING_FIELD (5), NEEDS_MANUAL_VERIFICATION (3) | 3 | MISSING_FIELD | — |
| gg · Hanya identitas dan bukti bayar | MISSING_DOCUMENT | MISSING_DOCUMENT (2), MISSING_FIELD (2) | 0 | MISSING_FIELD | — |

<details><summary>Rincian temuan per skenario (kode, severity, sumber evidence; tanpa nilai)</summary>

- **ga**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
  - NEEDS_MANUAL_VERIFICATION [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number | claim_record:policy_number
- **gb**: 
  - MISSING_FIELD [medium] ← g05_formulir_dua_kolom_nama_gelar.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
  - NAME_MISMATCH [medium] ← g05_formulir_dua_kolom_nama_gelar.jpg hal.1 claimant_name | g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - POLICY_MISMATCH [medium] ← g05_formulir_dua_kolom_nama_gelar.jpg hal.1 policy_number | claim_record:policy_number
- **gc**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g06_bukti_bayar_tanggal_sebelum.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g06_bukti_bayar_tanggal_sebelum.png hal.1 amount
  - NEEDS_MANUAL_VERIFICATION [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number | claim_record:policy_number
- **gd**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g07_resume_medis_nama_beda.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
  - NAME_MISMATCH [medium] ← g07_resume_medis_nama_beda.pdf hal.1 claimant_name | g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - NEEDS_MANUAL_VERIFICATION [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number | claim_record:policy_number
- **ge**: 
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
  - POLICY_MISMATCH [medium] ← g08_formulir_tabel_polis_beda.png hal.1 policy_number | claim_record:policy_number
- **gf**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - NEEDS_MANUAL_VERIFICATION [low] ← g09_identitas_gelap_buram.jpg hal.1 claimant_name
  - NEEDS_MANUAL_VERIFICATION [low] ← g09_identitas_gelap_buram.jpg hal.1 nik
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
  - NEEDS_MANUAL_VERIFICATION [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number | claim_record:policy_number
  - LOW_QUALITY_DOCUMENT [high] ← g09_identitas_gelap_buram.jpg hal.1
- **gg**: 
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount

</details>

### Ekstraksi field

Akurasi keseluruhan **33.3%** (27 field) · uncertain 0 · not_found 15 · salah baca 3 · dokumen tidak terbaca: tidak menebak 2 / menebak 0 · total waktu 62368 ms (rata-rata 6929 ms/dokumen)

| Field | Total | Benar | Salah | Uncertain | Not found | Akurasi |
|---|---:|---:|---:|---:|---:|---:|
| claimant_name | 8 | 4 | 1 | 0 | 3 | 50.0% |
| policy_number | 3 | 1 | 2 | 0 | 0 | 33.3% |
| nik | 4 | 1 | 0 | 0 | 3 | 25.0% |
| event_date | 5 | 1 | 0 | 0 | 4 | 20.0% |
| document_date | 2 | 2 | 0 | 0 | 0 | 100.0% |
| amount | 5 | 0 | 0 | 0 | 5 | 0.0% |

| File | Engine | Conf. halaman | Waktu (ms) | Hasil |
|---|---|---|---:|---|
| `g01_formulir_foto_hp.jpg` | tesseract | 83.7 | 1463 | claimant_name=not_found; policy_number=salah (dapat "9000********0003", harap "POL-UJI-3315"); nik=not_found; event_date=not_found; amount=not_found |
| `g02_identitas_resolusi_rendah.png` | tesseract | 82.3 | 884 | claimant_name=benar; nik=benar |
| `g03_resume_medis_scan_2hal.pdf` | tesseract, tesseract | 85.7, 87.4 | 6930 | claimant_name=benar; event_date=not_found |
| `g04_bukti_bayar_latar_kuning.jpg` | tesseract | 84.9 | 1552 | claimant_name=not_found; document_date=benar; amount=not_found |
| `g05_formulir_dua_kolom_nama_gelar.jpg` | tesseract | 94.3 | 1442 | claimant_name=salah (dapat "Tgi Kejadian", harap "Hj. S. Aminah Rahayu, S.E."); policy_number=salah (dapat "RUMAHSAKIT", harap "POL-UJI-3315"); nik=not_found; event_date=not_found; amount=not_found |
| `g06_bukti_bayar_tanggal_sebelum.png` | tesseract | 94.5 | 1495 | claimant_name=not_found; document_date=benar; amount=not_found |
| `g07_resume_medis_nama_beda.pdf` | tesseract | 93.8 | 1690 | claimant_name=benar; event_date=not_found |
| `g08_formulir_tabel_polis_beda.png` | tesseract | 93.7 | 1425 | claimant_name=benar; policy_number=benar; nik=not_found; event_date=benar; amount=not_found |
| `g09_identitas_gelap_buram.jpg` | tesseract | 35.1 | 45487 | claimant_name=tidak_menebak; nik=tidak_menebak |

## Perbandingan konfigurasi OCR — formulir klaim berbentuk gambar (3 file)

Akurasi field formulir (nama, polis, NIK, tanggal kejadian, nominal) per konfigurasi; hanya untuk laporan, konfigurasi produksi tidak dipilih dari set holdout.

| Konfigurasi | Field benar | Akurasi | Waktu total (ms) |
|---|---:|---:|---:|
| tanpa pra-pemrosesan, PSM 6 (Tahap 5) | 0/15 | 0.0% | 1215 |
| pra-pemrosesan, PSM 3 | 3/15 | 20.0% | 3047 |
| pra-pemrosesan, PSM 4 | 3/15 | 20.0% | 3192 |
| pra-pemrosesan, PSM 6 | 3/15 | 20.0% | 3049 |
| pra-pemrosesan, PSM 11 | 3/15 | 20.0% | 2965 |
| pra-pemrosesan, PSM 6+11 (produksi) | 3/15 | 20.0% | 4310 |

## Catatan

- Set holdout kecil (puluhan field, beberapa skenario); satu dokumen dapat menggeser persentase secara besar. Gunakan sebagai sinyal arah, bukan ukuran kinerja produksi.
- Kegagalan di sini sengaja **tidak** diperbaiki dengan menyetel aturan pada set yang sama; perbaikan harus divalidasi pada set uji baru.
- Konfigurasi OCR: pra-pemrosesan Go (normalisasi resolusi, kontras, median 3×3, deskew, Otsu, hapus garis tabel) + Tesseract PSM 6,11 (hasil berskor tertinggi). Gating confidence: NAME/POLICY_MISMATCH hanya bila kedua nilai ≥ 85; selain itu `NEEDS_MANUAL_VERIFICATION`.
- Ambang confidence adalah nilai awal prototipe; perlu diuji ulang pada dataset yang lebih representatif.
- Laporan dibuat ulang dengan `make eval`.
