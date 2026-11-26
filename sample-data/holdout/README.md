# sample-data/holdout — set uji HOLDOUT SINTETIS

> **Seluruh isi folder ini FIKTIF** (NIK berawalan `0000`, telepon
> `0800-0000-xxxx`, email `@example.com`, nama/fasilitas/dokter fiktif) dan
> bertanda watermark **"CONTOH - DATA SINTETIS"**.

Set ini **tidak dipakai untuk menyetel** aturan ekstraksi maupun aturan
validasi. Aturan validasi v1 dibekukan (commit "feat: Tahap 5 …") sebelum
set ini dibuat, dan set ini di-commit sebelum dijalankan pertama kali.
Hasilnya dilaporkan apa adanya di `docs/eval/holdout/report.md`; kegagalan
tidak diperbaiki dengan menyetel ulang aturan pada set yang sama.

Perbedaan dari `sample-data/`: generator lain (`scripts/gen_holdout_data.py`),
klaim/peserta lain, tata letak tabel bergaris / kartu / struk sempit, font
serif & monospace, kemiringan −3°…+4°, noise dan bintik scan, kompresi JPEG,
variasi label ("Nama Tertanggung", "Telah terima dari", "Tanggal Pembayaran",
"Jumlah Dibayar") dan format tanggal (`03/09/2026`, `3 September 2026`,
`05-09-2026`).

| File | Jenis | Keterangan |
|---|---|---|
| `h01_formulir_klaim_tabel_scan.jpg` | formulir_klaim | tabel bergaris, serif, miring 2,2°, nama bergelar "Ir." |
| `h02_identitas_kartu_mono.png` | identitas | kartu monospace, latar bergaris, miring −3° |
| `h03_surat_keterangan_dokter.pdf` | surat_keterangan_medis | PDF Times, tanggal "3 September 2026" |
| `h04_kwitansi_struk.jpg` | kuitansi | struk monospace, miring 1,5° |
| `h05_kwitansi_nama_singkat.jpg` | kuitansi | nama "B. Santoso Pratama" (singkatan) |
| `h06_formulir_mono_polis_beda.pdf` | formulir_klaim | PDF Courier, nomor polis berbeda |
| `h07_kwitansi_tanggal_depan.png` | kuitansi | tanggal setelah tanggal evaluasi |
| `h08_identitas_noise_berat.jpg` | identitas | noise berat, kontras rendah, buram |
| `h09_surat_dokter_nama_beda_scan.jpg` | surat_keterangan_medis | scan, nama pasien berbeda |

Skenario dan temuan yang diharapkan ada di `expected.json`
(`evaluation_date` = tanggal "hari ini" untuk aturan tanggal masa depan).
