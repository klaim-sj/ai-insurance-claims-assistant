# Evaluasi OCR & ekstraksi field (Tahap 4)

> Dataset: `sample-data/` (SELURUHNYA SINTETIS). Hasil ekstraksi adalah bahan
> verifikasi petugas, bukan keputusan klaim. NIK hanya ditampilkan tersamar.

- Dibuat: 2026-11-26T05:10:38Z
- Extractor: `rules-v1`; engine: pdftotext 24.02.0, tesseract 5.3.4
- Ambang confidence: tinggi ≥ 85, perlu verifikasi ≥ 60, di bawahnya rendah (uncertain)
- Akurasi = field `found` dengan nilai tepat ÷ field yang punya ground truth. `uncertain`/`not_found` dihitung tidak tepat (tidak menebak, tetapi perlu diperiksa manual).
- Dokumen yang memang dibuat tidak terbaca (skenario d) tidak masuk akurasi; dihitung terpisah: benar bila sistem tidak menebak.

Mode **standar** = alur aplikasi (lapisan teks PDF, Tesseract untuk gambar/halaman tanpa teks). Mode **scan** = setiap halaman PDF dirender 300 dpi lalu dibaca Tesseract (simulasi dokumen hasil scan).

## Mode standar

Akurasi keseluruhan **100.0%** (25 field) · uncertain 0 · not_found 0 · salah baca 0 · dokumen tidak terbaca: tidak menebak 2 / menebak 0 · total waktu 716 ms

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
| `01_formulir_klaim.pdf` | pdftotext | 100.0 | 7 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=benar |
| `02_identitas.png` | tesseract | 86.8 | 271 | claimant_name=benar; nik=benar |
| `03_surat_keterangan_medis.pdf` | pdftotext | 100.0 | 14 | claimant_name=benar; event_date=benar |
| `04_kuitansi.pdf` | pdftotext | 100.0 | 7 | claimant_name=benar; document_date=benar; amount=benar |
| `b_formulir_klaim_nama_beda.pdf` | pdftotext | 100.0 | 7 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=benar |
| `c_formulir_klaim_polis_beda.pdf` | pdftotext | 100.0 | 10 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=benar |
| `d_identitas_buram.png` | tesseract | 27.1 | 391 | claimant_name=tidak_menebak; nik=tidak_menebak |
| `e_kuitansi_tanggal_sebelum_kejadian.pdf` | pdftotext | 100.0 | 9 | claimant_name=benar; document_date=benar; amount=benar |

## Mode scan

Akurasi keseluruhan **100.0%** (25 field) · uncertain 0 · not_found 0 · salah baca 0 · dokumen tidak terbaca: tidak menebak 2 / menebak 0 · total waktu 7483 ms

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
| `01_formulir_klaim.pdf` | tesseract | 93.5 | 1272 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=benar |
| `02_identitas.png` | tesseract | 86.8 | 310 | claimant_name=benar; nik=benar |
| `03_surat_keterangan_medis.pdf` | tesseract | 94.3 | 1178 | claimant_name=benar; event_date=benar |
| `04_kuitansi.pdf` | tesseract | 93.3 | 920 | claimant_name=benar; document_date=benar; amount=benar |
| `b_formulir_klaim_nama_beda.pdf` | tesseract | 93.3 | 1128 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=benar |
| `c_formulir_klaim_polis_beda.pdf` | tesseract | 93.5 | 1239 | claimant_name=benar; policy_number=benar; nik=benar; event_date=benar; amount=benar |
| `d_identitas_buram.png` | tesseract | 27.1 | 362 | claimant_name=tidak_menebak; nik=tidak_menebak |
| `e_kuitansi_tanggal_sebelum_kejadian.pdf` | tesseract | 92.7 | 1074 | claimant_name=benar; document_date=benar; amount=benar |

## Catatan

- **Batas validitas**: dataset hanya 8 file sintetis dari satu generator dengan tata letak rapi, dan aturan ekstraksi (label, PSM Tesseract, ambang) disetel memakai file yang sama. Akurasi tinggi di sini **tidak** mewakili dokumen nyata (tulisan tangan, foto miring, tata letak beragam). Perlu dataset uji terpisah yang lebih representatif.
- `document_date` formulir dan surat keterangan tidak punya ground truth di `expected.json`, sehingga tidak dihitung.
- Ambang confidence adalah nilai awal prototipe; perlu diuji ulang pada dataset yang lebih representatif.
- Laporan dibuat ulang dengan `make eval`.
