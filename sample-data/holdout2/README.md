# sample-data/holdout2 — set uji HOLDOUT2 SINTETIS

> **Seluruh isi folder ini FIKTIF** (NIK berawalan `0000`, telepon
> `0800-0000-xxxx`, email `@example.com`, nama/fasilitas/dokter fiktif) dan
> bertanda watermark **"CONTOH - DATA SINTETIS"**.

Dibuat dan di-commit **sebelum** perbaikan Tahap 5.5 (pra-pemrosesan OCR,
sinonim label, gating confidence) ditulis maupun dijalankan terhadapnya.
Baseline dengan kode Tahap 5 disimpan di `docs/eval/holdout2-baseline/`.
Perbaikan disetel hanya memakai `sample-data/` dan `sample-data/holdout/`;
set ini dijalankan ulang sekali di akhir dan dilaporkan apa adanya.

Variasi (berbeda dari holdout pertama): foto ala HP (perspektif + bayangan,
miring 5°), resolusi rendah (~100 dpi), PDF hasil scan dua halaman tanpa
lapisan teks (data pasien di halaman 2), latar kuning dengan watermark
tebal berwarna, font oblique/Carlito, formulir dua kolom (label di atas
nilai), tabel bergaris tebal, pencahayaan gelap + buram, label baru
("Nama Pemohon", "No Polis", "No. KTP / NIK", "Tgl Kejadian", "Sudah terima
dari", "Total Bayar", "Dirawat sejak") dan format tanggal baru (`14.08.2026`,
`14 Agu 2026`, `16/08/2026`).

| File | Jenis | Keterangan |
|---|---|---|
| `g01_formulir_foto_hp.jpg` | formulir_klaim | foto HP: perspektif, bayangan, miring 5° |
| `g02_identitas_resolusi_rendah.png` | identitas | 560×350 px (~100 dpi) |
| `g03_resume_medis_scan_2hal.pdf` | surat_keterangan_medis | scan 2 halaman tanpa lapisan teks, font oblique |
| `g04_bukti_bayar_latar_kuning.jpg` | kuitansi | latar kuning, watermark merah tebal, mono oblique, miring −6° |
| `g05_formulir_dua_kolom_nama_gelar.jpg` | formulir_klaim | dua kolom, nama "Hj. S. Aminah Rahayu, S.E." |
| `g06_bukti_bayar_tanggal_sebelum.png` | kuitansi | tanggal sebelum tanggal kejadian |
| `g07_resume_medis_nama_beda.pdf` | surat_keterangan_medis | PDF teks oblique, nama pasien berbeda |
| `g08_formulir_tabel_polis_beda.png` | formulir_klaim | tabel bergaris tebal, nomor polis berbeda |
| `g09_identitas_gelap_buram.jpg` | identitas | gelap, buram, noise (sengaja tidak terbaca) |

Ekspektasi (`expected.json`) memakai semantik Tahap 5.5: field yang tidak
terbaca pada dokumen kualitas rendah = `NEEDS_MANUAL_VERIFICATION`.
