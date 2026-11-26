# Ringkasan evaluasi lintas set

> SELURUH DATA SINTETIS. Temuan adalah bahan peninjauan petugas, bukan keputusan klaim.

Precision/recall dihitung per (skenario × finding_code). *Substantif* = tanpa `NEEDS_MANUAL_VERIFICATION`. Waktu = total OCR + ekstraksi seluruh file set pada mode tersebut.

| Set | Mode | Akurasi field | Precision | Recall | Precision substantif | Recall substantif | NMV / temuan | Waktu total (ms) | Rata-rata/dokumen (ms) |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|
| utama | standar | 100.0% | 100.0% | 100.0% | 100.0% | 100.0% | 2 / 7 | 2257 | 282 |
| utama | scan | 100.0% | 100.0% | 100.0% | 100.0% | 100.0% | 2 / 7 | 14969 | 1871 |
| holdout | standar | 84.0% | 54.5% | 85.7% | 100.0% | 83.3% | 10 / 16 | 22499 | 2499 |
| holdout | scan | 84.0% | 54.5% | 85.7% | 100.0% | 83.3% | 10 / 16 | 26014 | 2890 |
| holdout2 | standar | 33.3% | 35.3% | 85.7% | 38.5% | 83.3% | 6 / 42 | 61671 | 6852 |
| holdout2 | scan | 33.3% | 35.3% | 85.7% | 38.5% | 83.3% | 6 / 42 | 62368 | 6929 |
| holdout2 (baseline kode Tahap 5) | standar | 11.1% | 30.0% | 42.9% | n/a | n/a | — | 10468 | 1163 |
| holdout2 (baseline kode Tahap 5) | scan | 11.1% | 30.0% | 42.9% | n/a | n/a | — | 10433 | 1159 |

Laporan rinci: `docs/eval/report.md` (utama), `docs/eval/holdout/report.md`, `docs/eval/holdout2/report.md`, baseline `docs/eval/holdout2-baseline/report.md` (kode Tahap 5, dijalankan sebelum perbaikan). Baseline dibuat sebelum metrik substantif/NMV ada, sehingga kolom itu kosong (n/a) untuknya.
