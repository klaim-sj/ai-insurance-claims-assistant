# Analisis evaluasi Tahap 5.5 (dilaporkan apa adanya)

> Seluruh data SINTETIS. Temuan adalah bahan peninjauan petugas, bukan
> keputusan klaim. Angka lengkap: [`summary.md`](summary.md).

## Kronologi (untuk menilai kejujuran angka)

1. `f3fa2e6`: `sample-data/holdout2/` dibuat dan di-commit **sebelum** kode
   perbaikan ditulis.
2. `e8e0af0`: baseline holdout2 dengan kode Tahap 5
   ([`holdout2-baseline/report.md`](holdout2-baseline/report.md)).
3. Perbaikan disetel **hanya** dengan `sample-data/` dan
   `sample-data/holdout/` (holdout pertama kini set pengembangan, bukan
   uji independen).
4. `2e50260` / `dc480c3`: perbaikan dan pelaporan dibekukan.
5. Holdout2 dijalankan dengan kode final. Run pertama dibatalkan oleh
   pengaman PII eval sebelum laporan ditulis (nilai field non-NIK belum
   disamarkan di laporan eval; diperbaiki di `fix(eval)`, tanpa perubahan
   OCR/aturan); run berikutnya adalah hasil di bawah. Tidak ada penyetelan
   setelah melihat hasil holdout2.

Catatan bias: generator holdout2 dan perbaikan ditulis oleh pihak yang
sama. Sinonim label holdout2 ("Nama Pemohon" sudah ada sejak Tahap 4;
"Sudah terima dari", "Total Bayar", "Total Tagihan", "Dirawat sejak",
"Tgl Pulang", "No. KTP / NIK") **sengaja tidak** ditambahkan ke
`labels.json`.

## Hasil

| Set | Akurasi field | Precision (semua) | Recall | Precision substantif | Waktu rata-rata/dok |
|---|---:|---:|---:|---:|---:|
| holdout2 baseline (Tahap 5) | 11,1% | 30,0% | 42,9% | n/a | 1,2 s |
| **holdout2 (Tahap 5.5)** | **33,3%** | **35,3%** | **85,7%** | **38,5%** | **6,9 s** |
| holdout (set pengembangan) | 32,0% → 84,0% | 37,5% → 54,5% | 85,7% | 100% | 2,5 s |
| utama | 100% | 100% | 100% | 100% | 0,3 s |

**Target "precision holdout2 jauh di atas 37,5%" tidak tercapai.**
Recall holdout2 naik dari 42,9% ke 85,7% dan akurasi field naik 3×,
tetapi precision hanya naik dari 30,0% ke 35,3%.

## Mengapa precision holdout2 tetap rendah

1. **Kosakata label, bukan kualitas gambar (penyebab utama, 7 dari 11 FP).**
   Pra-pemrosesan membuat halaman terbaca dengan confidence tinggi
   (83–87), sehingga gating kualitas menilai dokumen "baik". Field yang
   labelnya tidak dikenal ("Total Tagihan", "Total Bayar", "Sudah terima
   dari", "Dirawat sejak") menjadi `not_found` → `MISSING_FIELD`
   (29 instance di 7 skenario). Gating confidence tidak dapat membedakan
   "field memang tidak ada" dari "label tidak dikenal" bila OCR-nya
   bersih.
2. **Ekstraksi salah tetapi yakin (1 FP, paling berbahaya).** Formulir dua
   kolom (label di atas nilai, g05) menghasilkan `POLICY_MISMATCH` dari
   nilai yang salah dipasangkan, dengan confidence tinggi. Pada g01,
   nomor polis tertukar dengan baris NIK; di sini gating bekerja (nilai
   berconfidence rendah → `NEEDS_MANUAL_VERIFICATION`, bukan mismatch),
   tetapi tetap dihitung FP karena ground truth tidak mengharapkan temuan.
3. **DATE_INCONSISTENCY terlewat (1 FN).** Tanggal kejadian tidak
   terbaca di formulir foto maupun resume medis ("Dirawat sejak"), jadi
   tidak ada pembanding; sesuai desain sistem tidak menebak.
4. **Waktu proses naik ~6×** (1,2 s → 6,9 s per dokumen di holdout2;
   PDF scan 2 halaman 6,7 s). Pra-pemrosesan Go murni pada resolusi
   ~2400–3500 px dan dua PSM per halaman.

## Yang berhasil

- **Garis tabel**: penghapusan garis membuat isi tabel terbaca (formulir
  tabel holdout: 0/5 → 4/5 field dengan PSM 11).
- **Gating confidence**: tidak ada `NAME_MISMATCH` palsu di holdout2;
  kedua `NAME_MISMATCH` benar. Perbedaan dengan nilai berconfidence rendah
  menjadi `NEEDS_MANUAL_VERIFICATION` dengan kedua bukti.
- **Dokumen gelap/buram** (g09) tidak ditebak; muncul sebagai
  `LOW_QUALITY_DOCUMENT` + `NEEDS_MANUAL_VERIFICATION`.
- Precision substantif holdout (set pengembangan) 100%.

## Usulan berikutnya (perlu set uji baru untuk diukur)

- `MISSING_FIELD` hanya bila **label field ditemukan tetapi nilainya
  kosong**; label tidak ditemukan pada dokumen hasil OCR →
  `NEEDS_MANUAL_VERIFICATION` ("label tidak dikenali"). Ini langsung
  menyasar penyebab 1, tetapi tidak diterapkan di sini karena dirumuskan
  setelah melihat holdout2.
- Ekstraksi tata letak "label di atas nilai" berbasis posisi kata
  (bounding box), bukan hanya baris.
- Proses kosakata label: tinjau label baru dari dokumen nyata yang sudah
  dianonimkan, bukan dari set uji.
- Optimasi waktu: pra-pemrosesan pada resolusi lebih rendah untuk
  deskew/Otsu, dan PSM kedua hanya bila PSM pertama berskor rendah.
