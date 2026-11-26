# Evaluasi OCR, ekstraksi field, dan temuan — dataset holdout2 (baseline kode Tahap 5)

> Dataset: `sample-data/holdout2/` (SELURUHNYA SINTETIS). Hasil ekstraksi dan temuan adalah bahan
> verifikasi petugas ("perlu ditinjau"), bukan keputusan klaim. NIK hanya ditampilkan tersamar.

**Set holdout**: dokumen di set ini dibuat dengan tata letak, font, kemiringan, dan noise berbeda dan **tidak dipakai untuk menyetel** aturan ekstraksi maupun aturan validasi. Aturan dibekukan sebelum set ini dijalankan pertama kali; angka di bawah dilaporkan apa adanya.

- Dibuat: 2026-11-26T09:22:24Z
- Extractor: `rules-v1`; engine: pdftotext 24.02.0, tesseract 5.3.4
- Aturan validasi: MISSING_DOCUMENT v1, MISSING_FIELD v1, NAME_MISMATCH v1, POLICY_MISMATCH v1, DATE_INCONSISTENCY v1, LOW_QUALITY_DOCUMENT v1
- Ambang confidence: tinggi ≥ 85, perlu verifikasi ≥ 60, di bawahnya rendah (uncertain)
- Akurasi field = field `found` dengan nilai tepat ÷ field yang punya ground truth. `uncertain`/`not_found` dihitung tidak tepat (tidak menebak, tetapi perlu diperiksa manual).
- Dokumen yang sengaja dibuat tidak terbaca (`expected_unreadable`) tidak masuk akurasi; dihitung terpisah: benar bila sistem tidak menebak.
- Temuan: rule engine dijalankan per skenario. Satuan hitung = (skenario, finding_code): TP bila kode diharapkan dan muncul, FP bila muncul tanpa diharapkan, FN bila diharapkan tetapi tidak muncul. Evidence lengkap = TP yang evidence-nya merujuk semua file sumber yang disebut `expected.json`.

Mode **standar** = alur aplikasi (lapisan teks PDF, Tesseract untuk gambar/halaman tanpa teks). Mode **scan** = setiap halaman PDF dirender 300 dpi lalu dibaca Tesseract (simulasi dokumen hasil scan).

## Mode standar

### Temuan (rule engine)

Precision **30.0%** · recall **42.9%** · TP 3 · FP 7 · FN 4 · evidence lengkap 2/3 TP · temuan ditolak validator evidence 0

| finding_code | TP | FP | FN | Precision | Recall |
|---|---:|---:|---:|---:|---:|
| MISSING_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| MISSING_FIELD | 0 | 7 | 0 | 0.0% | n/a |
| NAME_MISMATCH | 1 | 0 | 1 | 100.0% | 50.0% |
| POLICY_MISMATCH | 0 | 0 | 1 | n/a | 0.0% |
| DATE_INCONSISTENCY | 0 | 0 | 1 | n/a | 0.0% |
| LOW_QUALITY_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| NEEDS_MANUAL_VERIFICATION | 0 | 0 | 1 | n/a | 0.0% |

| Skenario | Diharapkan | Muncul (jumlah) | FP | FN |
|---|---|---|---|---|
| ga · Set lengkap (foto HP, resolusi rendah, scan 2 halaman, latar warna) | — | MISSING_FIELD (9) | MISSING_FIELD | — |
| gb · Formulir dua kolom, nama bergelar dan disingkat | NAME_MISMATCH | MISSING_FIELD (9) | MISSING_FIELD | NAME_MISMATCH |
| gc · Tanggal bukti bayar sebelum tanggal kejadian | DATE_INCONSISTENCY | MISSING_FIELD (8) | MISSING_FIELD | DATE_INCONSISTENCY |
| gd · Nama pasien di resume medis berbeda | NAME_MISMATCH | MISSING_FIELD (9), NAME_MISMATCH (1) | MISSING_FIELD | — |
| ge · Nomor polis formulir tabel berbeda | POLICY_MISMATCH | MISSING_FIELD (9) | MISSING_FIELD | POLICY_MISMATCH |
| gf · Identitas gelap dan buram | LOW_QUALITY_DOCUMENT, NEEDS_MANUAL_VERIFICATION | LOW_QUALITY_DOCUMENT (1), MISSING_FIELD (9) | MISSING_FIELD | NEEDS_MANUAL_VERIFICATION |
| gg · Hanya identitas dan bukti bayar | MISSING_DOCUMENT | MISSING_DOCUMENT (2), MISSING_FIELD (5) | MISSING_FIELD | — |

<details><summary>Rincian temuan per skenario (kode, severity, sumber evidence; tanpa nilai)</summary>

- **ga**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number
  - MISSING_FIELD [low] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 nik
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 document_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
- **gb**: 
  - MISSING_FIELD [medium] ← g05_formulir_dua_kolom_nama_gelar.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g05_formulir_dua_kolom_nama_gelar.jpg hal.1 policy_number
  - MISSING_FIELD [medium] ← g05_formulir_dua_kolom_nama_gelar.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 nik
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 document_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
- **gc**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number
  - MISSING_FIELD [low] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 nik
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g06_bukti_bayar_tanggal_sebelum.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g06_bukti_bayar_tanggal_sebelum.png hal.1 amount
- **gd**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number
  - MISSING_FIELD [low] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 nik
  - MISSING_FIELD [medium] ← g07_resume_medis_nama_beda.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 document_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
  - NAME_MISMATCH [medium] ← g07_resume_medis_nama_beda.pdf hal.1 claimant_name | claim_record:claimant_name
  - evidence tidak lengkap untuk: NAME_MISMATCH
- **ge**: 
  - MISSING_FIELD [medium] ← g08_formulir_tabel_polis_beda.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g08_formulir_tabel_polis_beda.png hal.1 policy_number
  - MISSING_FIELD [medium] ← g08_formulir_tabel_polis_beda.png hal.1 event_date
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 nik
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 document_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
- **gf**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number
  - MISSING_FIELD [low] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - MISSING_FIELD [low] ← g09_identitas_gelap_buram.jpg hal.1 claimant_name
  - MISSING_FIELD [low] ← g09_identitas_gelap_buram.jpg hal.1 nik
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 document_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
  - LOW_QUALITY_DOCUMENT [high] ← g09_identitas_gelap_buram.jpg hal.1
- **gg**: 
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 nik
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 document_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount

</details>

### Ekstraksi field

Akurasi keseluruhan **11.1%** (27 field) · uncertain 1 · not_found 23 · salah baca 0 · dokumen tidak terbaca: tidak menebak 2 / menebak 0 · total waktu 10468 ms

| Field | Total | Benar | Salah | Uncertain | Not found | Akurasi |
|---|---:|---:|---:|---:|---:|---:|
| claimant_name | 8 | 2 | 0 | 0 | 6 | 25.0% |
| policy_number | 3 | 0 | 0 | 0 | 3 | 0.0% |
| nik | 4 | 0 | 0 | 0 | 4 | 0.0% |
| event_date | 5 | 0 | 0 | 1 | 4 | 0.0% |
| document_date | 2 | 1 | 0 | 0 | 1 | 50.0% |
| amount | 5 | 0 | 0 | 0 | 5 | 0.0% |

| File | Engine | Conf. halaman | Waktu (ms) | Hasil |
|---|---|---|---:|---|
| `g01_formulir_foto_hp.jpg` | tesseract | 79.4 | 831 | claimant_name=not_found; policy_number=not_found; nik=not_found; event_date=uncertain; amount=not_found |
| `g02_identitas_resolusi_rendah.png` | tesseract | 92.5 | 239 | claimant_name=not_found; nik=not_found |
| `g03_resume_medis_scan_2hal.pdf` | tesseract, tesseract | 95.0, 95.1 | 4009 | claimant_name=benar; event_date=not_found |
| `g04_bukti_bayar_latar_kuning.jpg` | tesseract | 74.2 | 411 | claimant_name=not_found; document_date=not_found; amount=not_found |
| `g05_formulir_dua_kolom_nama_gelar.jpg` | tesseract | 94.3 | 313 | claimant_name=not_found; policy_number=not_found; nik=not_found; event_date=not_found; amount=not_found |
| `g06_bukti_bayar_tanggal_sebelum.png` | tesseract | 86.0 | 287 | claimant_name=not_found; document_date=benar; amount=not_found |
| `g07_resume_medis_nama_beda.pdf` | pdftotext | 100.0 | 8 | claimant_name=benar; event_date=not_found |
| `g08_formulir_tabel_polis_beda.png` | tesseract | 74.2 | 251 | claimant_name=not_found; policy_number=not_found; nik=not_found; event_date=not_found; amount=not_found |
| `g09_identitas_gelap_buram.jpg` | tesseract | 16.5 | 4119 | claimant_name=tidak_menebak; nik=tidak_menebak |

## Mode scan

### Temuan (rule engine)

Precision **30.0%** · recall **42.9%** · TP 3 · FP 7 · FN 4 · evidence lengkap 2/3 TP · temuan ditolak validator evidence 0

| finding_code | TP | FP | FN | Precision | Recall |
|---|---:|---:|---:|---:|---:|
| MISSING_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| MISSING_FIELD | 0 | 7 | 0 | 0.0% | n/a |
| NAME_MISMATCH | 1 | 0 | 1 | 100.0% | 50.0% |
| POLICY_MISMATCH | 0 | 0 | 1 | n/a | 0.0% |
| DATE_INCONSISTENCY | 0 | 0 | 1 | n/a | 0.0% |
| LOW_QUALITY_DOCUMENT | 1 | 0 | 0 | 100.0% | 100.0% |
| NEEDS_MANUAL_VERIFICATION | 0 | 0 | 1 | n/a | 0.0% |

| Skenario | Diharapkan | Muncul (jumlah) | FP | FN |
|---|---|---|---|---|
| ga · Set lengkap (foto HP, resolusi rendah, scan 2 halaman, latar warna) | — | MISSING_FIELD (9) | MISSING_FIELD | — |
| gb · Formulir dua kolom, nama bergelar dan disingkat | NAME_MISMATCH | MISSING_FIELD (9) | MISSING_FIELD | NAME_MISMATCH |
| gc · Tanggal bukti bayar sebelum tanggal kejadian | DATE_INCONSISTENCY | MISSING_FIELD (8) | MISSING_FIELD | DATE_INCONSISTENCY |
| gd · Nama pasien di resume medis berbeda | NAME_MISMATCH | MISSING_FIELD (9), NAME_MISMATCH (1) | MISSING_FIELD | — |
| ge · Nomor polis formulir tabel berbeda | POLICY_MISMATCH | MISSING_FIELD (9) | MISSING_FIELD | POLICY_MISMATCH |
| gf · Identitas gelap dan buram | LOW_QUALITY_DOCUMENT, NEEDS_MANUAL_VERIFICATION | LOW_QUALITY_DOCUMENT (1), MISSING_FIELD (9) | MISSING_FIELD | NEEDS_MANUAL_VERIFICATION |
| gg · Hanya identitas dan bukti bayar | MISSING_DOCUMENT | MISSING_DOCUMENT (2), MISSING_FIELD (5) | MISSING_FIELD | — |

<details><summary>Rincian temuan per skenario (kode, severity, sumber evidence; tanpa nilai)</summary>

- **ga**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number
  - MISSING_FIELD [low] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 nik
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 document_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
- **gb**: 
  - MISSING_FIELD [medium] ← g05_formulir_dua_kolom_nama_gelar.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g05_formulir_dua_kolom_nama_gelar.jpg hal.1 policy_number
  - MISSING_FIELD [medium] ← g05_formulir_dua_kolom_nama_gelar.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 nik
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 document_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
- **gc**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number
  - MISSING_FIELD [low] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 nik
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g06_bukti_bayar_tanggal_sebelum.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g06_bukti_bayar_tanggal_sebelum.png hal.1 amount
- **gd**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number
  - MISSING_FIELD [low] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 nik
  - MISSING_FIELD [medium] ← g07_resume_medis_nama_beda.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 document_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
  - NAME_MISMATCH [medium] ← g07_resume_medis_nama_beda.pdf hal.1 claimant_name | claim_record:claimant_name
  - evidence tidak lengkap untuk: NAME_MISMATCH
- **ge**: 
  - MISSING_FIELD [medium] ← g08_formulir_tabel_polis_beda.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g08_formulir_tabel_polis_beda.png hal.1 policy_number
  - MISSING_FIELD [medium] ← g08_formulir_tabel_polis_beda.png hal.1 event_date
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 nik
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 document_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
- **gf**: 
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g01_formulir_foto_hp.jpg hal.1 policy_number
  - MISSING_FIELD [low] ← g01_formulir_foto_hp.jpg hal.1 event_date
  - MISSING_FIELD [low] ← g09_identitas_gelap_buram.jpg hal.1 claimant_name
  - MISSING_FIELD [low] ← g09_identitas_gelap_buram.jpg hal.1 nik
  - MISSING_FIELD [medium] ← g03_resume_medis_scan_2hal.pdf hal.1 event_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 document_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount
  - LOW_QUALITY_DOCUMENT [high] ← g09_identitas_gelap_buram.jpg hal.1
- **gg**: 
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type
  - MISSING_DOCUMENT [high] ← checklist:document_type | claim_record:product_type
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 claimant_name
  - MISSING_FIELD [medium] ← g02_identitas_resolusi_rendah.png hal.1 nik
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 claimant_name
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 document_date
  - MISSING_FIELD [medium] ← g04_bukti_bayar_latar_kuning.jpg hal.1 amount

</details>

### Ekstraksi field

Akurasi keseluruhan **11.1%** (27 field) · uncertain 1 · not_found 23 · salah baca 0 · dokumen tidak terbaca: tidak menebak 2 / menebak 0 · total waktu 10433 ms

| Field | Total | Benar | Salah | Uncertain | Not found | Akurasi |
|---|---:|---:|---:|---:|---:|---:|
| claimant_name | 8 | 2 | 0 | 0 | 6 | 25.0% |
| policy_number | 3 | 0 | 0 | 0 | 3 | 0.0% |
| nik | 4 | 0 | 0 | 0 | 4 | 0.0% |
| event_date | 5 | 0 | 0 | 1 | 4 | 0.0% |
| document_date | 2 | 1 | 0 | 0 | 1 | 50.0% |
| amount | 5 | 0 | 0 | 0 | 5 | 0.0% |

| File | Engine | Conf. halaman | Waktu (ms) | Hasil |
|---|---|---|---:|---|
| `g01_formulir_foto_hp.jpg` | tesseract | 79.4 | 557 | claimant_name=not_found; policy_number=not_found; nik=not_found; event_date=uncertain; amount=not_found |
| `g02_identitas_resolusi_rendah.png` | tesseract | 92.5 | 220 | claimant_name=not_found; nik=not_found |
| `g03_resume_medis_scan_2hal.pdf` | tesseract, tesseract | 95.0, 95.1 | 3843 | claimant_name=benar; event_date=not_found |
| `g04_bukti_bayar_latar_kuning.jpg` | tesseract | 74.2 | 419 | claimant_name=not_found; document_date=not_found; amount=not_found |
| `g05_formulir_dua_kolom_nama_gelar.jpg` | tesseract | 94.3 | 302 | claimant_name=not_found; policy_number=not_found; nik=not_found; event_date=not_found; amount=not_found |
| `g06_bukti_bayar_tanggal_sebelum.png` | tesseract | 86.0 | 347 | claimant_name=not_found; document_date=benar; amount=not_found |
| `g07_resume_medis_nama_beda.pdf` | tesseract | 91.5 | 863 | claimant_name=benar; event_date=not_found |
| `g08_formulir_tabel_polis_beda.png` | tesseract | 74.2 | 313 | claimant_name=not_found; policy_number=not_found; nik=not_found; event_date=not_found; amount=not_found |
| `g09_identitas_gelap_buram.jpg` | tesseract | 16.5 | 3569 | claimant_name=tidak_menebak; nik=tidak_menebak |

## Catatan

- Set holdout kecil (puluhan field, beberapa skenario); satu dokumen dapat menggeser persentase secara besar. Gunakan sebagai sinyal arah, bukan ukuran kinerja produksi.
- Kegagalan di sini sengaja **tidak** diperbaiki dengan menyetel aturan pada set yang sama; perbaikan harus divalidasi pada set uji baru.
- Ambang confidence adalah nilai awal prototipe; perlu diuji ulang pada dataset yang lebih representatif.
- Laporan dibuat ulang dengan `make eval`.
