# sample-data — dokumen uji SINTETIS

> **Seluruh isi folder ini FIKTIF.** Nama, NIK (berawalan `0000`), telepon
> (`0800-0000-xxxx`), email (`@example.com`), fasilitas kesehatan, dokter, dan
> diagnosis generik dibuat untuk pengujian. Setiap dokumen bertanda watermark
> **"CONTOH - DATA SINTETIS"** dan tidak meniru desain dokumen resmi mana pun.

Klaim kesehatan fiktif: polis `POL-DEMO-0101`, peserta "Rani Kusuma Dewi",
rawat inap 10–12 Okt 2026.

| File | Jenis | Keterangan |
|---|---|---|
| `01_formulir_klaim.pdf` | formulir_klaim | set dasar |
| `02_identitas.png` | identitas | set dasar |
| `03_surat_keterangan_medis.pdf` | surat_keterangan_medis | set dasar |
| `04_kuitansi.pdf` | kuitansi | set dasar |
| `b_formulir_klaim_nama_beda.pdf` | formulir_klaim | nama ≠ identitas |
| `c_formulir_klaim_polis_beda.pdf` | formulir_klaim | nomor polis ≠ data klaim |
| `d_identitas_buram.png` | identitas | sengaja buram |
| `e_kuitansi_tanggal_sebelum_kejadian.pdf` | kuitansi | tanggal sebelum kejadian |

Skenario (detail di `expected.json`): set dasar diunggah apa adanya
(**a**), atau satu file diganti varian **b–e**. `expected.json` memuat nilai
field yang benar per file, temuan yang seharusnya muncul (status
`perlu_ditinjau`) beserta evidence reference, dan saran tindak lanjut untuk
petugas. Temuan adalah bahan peninjauan, **bukan** keputusan klaim.

Membuat ulang (butuh Python 3, reportlab, Pillow):

```bash
python3 -I scripts/gen_sample_data.py
```

`internal/documents/sampledata_test.go` memastikan semua file lolos validasi
unggah dan `expected.json` konsisten.
