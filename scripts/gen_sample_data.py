#!/usr/bin/env python3
"""Membuat dokumen uji SINTETIS di sample-data/ untuk klaim kesehatan fiktif.

SELURUH ISI FIKTIF: nama, NIK (berawalan 0000), telepon (0800-0000-xxxx),
email (@example.com), rumah sakit, dokter, dan diagnosis generik. Setiap
halaman/gambar diberi watermark "CONTOH - DATA SINTETIS".

Hasil (PDF/PNG) di-commit sehingga generator ini hanya perlu dijalankan
ulang bila skenario berubah. Kebutuhan: Python 3, reportlab, Pillow.

    python3 -I scripts/gen_sample_data.py
    python3 -I scripts/gen_sample_data.py --expected-only   # hanya expected.json

Output deterministik untuk konten (PDF memakai invariant=1).
"""

import json
import os
import sys

from PIL import Image, ImageDraw, ImageFilter, ImageFont
from reportlab.lib.pagesizes import A4
from reportlab.pdfgen import canvas

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, "sample-data")
WATERMARK = "CONTOH - DATA SINTETIS"

# --- Data klaim fiktif (sumber kebenaran untuk expected.json) ---------------
CLAIM = {
    "policy_number": "POL-DEMO-0101",
    "claimant_name": "Rani Kusuma Dewi",
    "product_type": "Kesehatan (simulasi)",
    "received_at": "2026-10-15",
}
PERSON = {
    "full_name": "Rani Kusuma Dewi",
    "nik": "0000010101900001",
    "birth_place_date": "Kota Demo, 01-01-1990",
    "address": "Jl. Contoh Fiktif No. 1, Kota Demo",
    "phone": "0800-0000-1234",
    "email": "rani.kusuma@example.com",
}
INCIDENT_DATE = "2026-10-10"  # tanggal mulai rawat (kejadian)
DISCHARGE_DATE = "2026-10-12"
HOSPITAL = "RS Contoh Sehat (fiktif)"
DIAGNOSIS = "Demam tanpa komplikasi (diagnosis contoh generik)"
DOCTOR = "dr. Contoh Pemeriksa (fiktif)"
RECEIPT_NO = "KW-DEMO-0001"
RECEIPT_DATE = "2026-10-12"
RECEIPT_TOTAL = "Rp 2.750.000"
RECEIPT_ITEMS = [
    ("Kamar rawat inap 2 malam", "Rp 1.200.000"),
    ("Pemeriksaan dokter", "Rp 450.000"),
    ("Pemeriksaan laboratorium dasar", "Rp 600.000"),
    ("Obat-obatan", "Rp 500.000"),
]

# Nilai yang sengaja dibuat berbeda untuk skenario b, c, e.
NAME_VARIANT = "Rani Kusumawati"
POLICY_VARIANT = "POL-DEMO-0110"
RECEIPT_DATE_EARLY = "2026-10-05"


def idn(d):
    """YYYY-MM-DD -> DD-MM-YYYY (format tampilan dokumen)."""
    y, m, dd = d.split("-")
    return f"{dd}-{m}-{y}"


# --- PDF -------------------------------------------------------------------
def pdf_page(path, title, rows, footer_lines=()):
    c = canvas.Canvas(path, pagesize=A4, invariant=1)
    c.setTitle(f"{title} - {WATERMARK}")
    c.setAuthor("Generator data sintetis")
    w, h = A4

    # Watermark diagonal.
    c.saveState()
    c.setFillColorRGB(0.85, 0.85, 0.85)
    c.setFont("Helvetica-Bold", 44)
    c.translate(w / 2, h / 2)
    c.rotate(35)
    c.drawCentredString(0, 0, WATERMARK)
    c.restoreState()

    c.setFillColorRGB(0.75, 0.1, 0.1)
    c.setFont("Helvetica-Bold", 10)
    c.drawCentredString(w / 2, h - 30, WATERMARK + " - BUKAN DOKUMEN ASLI")
    c.setFillColorRGB(0, 0, 0)
    c.setFont("Helvetica-Bold", 16)
    c.drawString(50, h - 70, title)
    c.line(50, h - 78, w - 50, h - 78)

    y = h - 110
    c.setFont("Helvetica", 11)
    for label, value in rows:
        if label is None:  # baris pemisah / judul bagian
            y -= 6
            c.setFont("Helvetica-Bold", 11)
            c.drawString(50, y, value)
            c.setFont("Helvetica", 11)
            y -= 20
            continue
        c.drawString(50, y, label)
        c.drawString(230, y, ": " + value)
        y -= 20
    y -= 10
    for line in footer_lines:
        c.drawString(50, y, line)
        y -= 18

    c.setFont("Helvetica-Oblique", 8)
    c.setFillColorRGB(0.4, 0.4, 0.4)
    c.drawString(50, 30, "Dokumen ini dibuat otomatis untuk pengujian. Seluruh data fiktif.")
    c.showPage()
    c.save()


def formulir(path, name, policy):
    pdf_page(path, "FORMULIR PENGAJUAN KLAIM KESEHATAN", [
        (None, "A. Data Peserta"),
        ("Nama peserta", name),
        ("NIK", PERSON["nik"]),
        ("Nomor polis", policy),
        ("Telepon", PERSON["phone"]),
        ("Email", PERSON["email"]),
        (None, "B. Data Kejadian"),
        ("Jenis klaim", "Rawat inap"),
        ("Tanggal kejadian / masuk", idn(INCIDENT_DATE)),
        ("Tanggal keluar", idn(DISCHARGE_DATE)),
        ("Fasilitas kesehatan", HOSPITAL),
        ("Total biaya diajukan", RECEIPT_TOTAL),
    ], footer_lines=[
        "Saya menyatakan data di atas benar (contoh pernyataan).",
        f"Kota Demo, {idn(CLAIM['received_at'])}        Tanda tangan: [CONTOH]",
    ])


def surat_medis(path):
    pdf_page(path, "SURAT KETERANGAN MEDIS", [
        ("Fasilitas kesehatan", HOSPITAL),
        ("Nama pasien", PERSON["full_name"]),
        ("Tanggal masuk", idn(INCIDENT_DATE)),
        ("Tanggal keluar", idn(DISCHARGE_DATE)),
        ("Diagnosis", DIAGNOSIS),
        ("Dokter pemeriksa", DOCTOR),
    ], footer_lines=[
        "Pasien telah menjalani perawatan sesuai tanggal di atas (contoh).",
    ])


def kuitansi(path, date):
    rows = [
        ("Nomor kuitansi", RECEIPT_NO),
        ("Tanggal", idn(date)),
        ("Diterima dari", PERSON["full_name"]),
        ("Fasilitas kesehatan", HOSPITAL),
        (None, "Rincian biaya"),
    ]
    rows += [(k, v) for k, v in RECEIPT_ITEMS]
    rows += [("TOTAL", RECEIPT_TOTAL)]
    pdf_page(path, "KUITANSI PEMBAYARAN", rows)


# --- PNG -------------------------------------------------------------------
def font(size, bold=False):
    name = "DejaVuSans-Bold.ttf" if bold else "DejaVuSans.ttf"
    for d in ("/usr/share/fonts/truetype/dejavu", "/Library/Fonts", "C:/Windows/Fonts"):
        p = os.path.join(d, name)
        if os.path.exists(p):
            return ImageFont.truetype(p, size)
    return ImageFont.load_default(size)


def identitas(path, blur=0):
    # Tata letak generik "kartu identitas contoh", sengaja tidak meniru
    # desain dokumen resmi mana pun.
    W, H = 1000, 630
    img = Image.new("RGB", (W, H), (236, 242, 248))
    d = ImageDraw.Draw(img)
    d.rectangle([10, 10, W - 10, H - 10], outline=(60, 90, 140), width=4)
    d.text((40, 30), "KARTU IDENTITAS CONTOH", font=font(40, True), fill=(30, 50, 90))
    d.text((40, 85), WATERMARK + " - BUKAN DOKUMEN ASLI", font=font(22, True), fill=(180, 30, 30))
    d.rectangle([760, 150, 950, 400], outline=(120, 120, 120), width=2)
    d.text((790, 260), "[FOTO]", font=font(28), fill=(120, 120, 120))
    rows = [
        ("NIK", PERSON["nik"]),
        ("Nama", PERSON["full_name"]),
        ("Tempat/Tgl Lahir", PERSON["birth_place_date"]),
        ("Alamat", PERSON["address"]),
    ]
    y = 160
    for label, value in rows:
        d.text((40, y), label, font=font(26), fill=(0, 0, 0))
        d.text((300, y), ": " + value, font=font(26), fill=(0, 0, 0))
        y += 60

    # Watermark diagonal semi-transparan.
    wm = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    wd = ImageDraw.Draw(wm)
    wd.text((W / 2, H / 2), WATERMARK, font=font(70, True), fill=(200, 0, 0, 70), anchor="mm")
    wm = wm.rotate(25, center=(W / 2, H / 2))
    img = Image.alpha_composite(img.convert("RGBA"), wm).convert("RGB")

    if blur:
        img = img.filter(ImageFilter.GaussianBlur(blur))
    img.save(path, "PNG", optimize=True)


# --- expected.json ---------------------------------------------------------
def expected():
    claim_form_fields = {
        "claimant_name": PERSON["full_name"],
        "nik": PERSON["nik"],
        "policy_number": CLAIM["policy_number"],
        "phone": PERSON["phone"],
        "email": PERSON["email"],
        "claim_type": "rawat_inap",
        "incident_date": INCIDENT_DATE,
        "discharge_date": DISCHARGE_DATE,
        "provider_name": HOSPITAL,
        "total_amount": 2750000,
    }
    identity_fields = {
        "full_name": PERSON["full_name"],
        "nik": PERSON["nik"],
        "birth_place_date": PERSON["birth_place_date"],
        "address": PERSON["address"],
    }
    medical_fields = {
        "patient_name": PERSON["full_name"],
        "provider_name": HOSPITAL,
        "admission_date": INCIDENT_DATE,
        "discharge_date": DISCHARGE_DATE,
        "diagnosis": DIAGNOSIS,
        "doctor_name": DOCTOR,
    }
    receipt_fields = {
        "receipt_number": RECEIPT_NO,
        "receipt_date": RECEIPT_DATE,
        "payer_name": PERSON["full_name"],
        "provider_name": HOSPITAL,
        "total_amount": 2750000,
    }
    files = {
        "01_formulir_klaim.pdf": {"document_type": "formulir_klaim", "mime_type": "application/pdf", "pages": 1, "fields": claim_form_fields},
        "02_identitas.png": {"document_type": "identitas", "mime_type": "image/png", "pages": 1, "fields": identity_fields},
        "03_surat_keterangan_medis.pdf": {"document_type": "surat_keterangan_medis", "mime_type": "application/pdf", "pages": 1, "fields": medical_fields},
        "04_kuitansi.pdf": {"document_type": "kuitansi", "mime_type": "application/pdf", "pages": 1, "fields": receipt_fields},
        "b_formulir_klaim_nama_beda.pdf": {"document_type": "formulir_klaim", "mime_type": "application/pdf", "pages": 1,
                                           "fields": {**claim_form_fields, "claimant_name": NAME_VARIANT}},
        "c_formulir_klaim_polis_beda.pdf": {"document_type": "formulir_klaim", "mime_type": "application/pdf", "pages": 1,
                                            "fields": {**claim_form_fields, "policy_number": POLICY_VARIANT}},
        "d_identitas_buram.png": {"document_type": "identitas", "mime_type": "image/png", "pages": 1,
                                  "fields": identity_fields,
                                  "expected_unreadable": True,
                                  "note": "Nilai field adalah isi sebenarnya sebelum diburamkan. Sistem TIDAK boleh menebak; field diharapkan tidak terbaca atau confidence rendah."},
        "e_kuitansi_tanggal_sebelum_kejadian.pdf": {"document_type": "kuitansi", "mime_type": "application/pdf", "pages": 1,
                                                    "fields": {**receipt_fields, "receipt_date": RECEIPT_DATE_EARLY}},
    }
    base = ["01_formulir_klaim.pdf", "02_identitas.png", "03_surat_keterangan_medis.pdf", "04_kuitansi.pdf"]

    def swap(old, new):
        return [new if f == old else f for f in base]

    def ev(file, field, value, page=1):
        return {"file": file, "page": page, "field": field, "value": value}

    scenarios = [
        {
            "id": "a",
            "title": "Set lengkap dan konsisten",
            "upload": base,
            "expected_findings": [],
            "suggested_followup": ["Tidak ada temuan otomatis; petugas tetap meninjau dokumen sebelum melanjutkan."],
        },
        {
            "id": "b",
            "title": "Nama di formulir berbeda dengan identitas",
            "upload": swap("01_formulir_klaim.pdf", "b_formulir_klaim_nama_beda.pdf"),
            "expected_findings": [{
                "code": "NAME_MISMATCH",
                "category": "konsistensi_antardokumen",
                "status": "perlu_ditinjau",
                "description": "Nama pada formulir klaim berbeda dari nama pada dokumen identitas.",
                "evidence": [
                    ev("b_formulir_klaim_nama_beda.pdf", "claimant_name", NAME_VARIANT),
                    ev("02_identitas.png", "full_name", PERSON["full_name"]),
                ],
            }],
            "suggested_followup": ["Konfirmasi ejaan nama peserta; minta formulir yang sudah diperbaiki bila diperlukan."],
        },
        {
            "id": "c",
            "title": "Nomor polis di formulir berbeda dengan data klaim",
            "upload": swap("01_formulir_klaim.pdf", "c_formulir_klaim_polis_beda.pdf"),
            "expected_findings": [{
                "code": "POLICY_MISMATCH",
                "category": "konsistensi_data_klaim",
                "status": "perlu_ditinjau",
                "description": "Nomor polis pada formulir klaim berbeda dari nomor polis pada data klaim.",
                "evidence": [
                    ev("c_formulir_klaim_polis_beda.pdf", "policy_number", POLICY_VARIANT),
                    {"source": "claim_record", "field": "policy_number", "value": CLAIM["policy_number"]},
                ],
            }],
            "suggested_followup": ["Verifikasi nomor polis pada sistem polis; perbaiki data klaim atau minta formulir yang benar."],
        },
        {
            "id": "d",
            "title": "Gambar identitas sengaja buram",
            "upload": swap("02_identitas.png", "d_identitas_buram.png"),
            "expected_findings": [{
                "code": "LOW_QUALITY_DOCUMENT",
                "category": "kualitas_dokumen",
                "status": "perlu_ditinjau",
                "description": "Dokumen identitas buram sehingga field tidak dapat dibaca dengan andal.",
                "evidence": [
                    {"file": "d_identitas_buram.png", "page": 1, "field": None, "value": None,
                     "signal": "confidence OCR rendah / teks tidak terbaca"},
                ],
            }, {
                "code": "MISSING_FIELD",
                "category": "kelengkapan_field",
                "status": "perlu_ditinjau",
                "description": "Field wajib identitas (nama, NIK) tidak terbaca dengan pasti.",
                "evidence": [
                    {"file": "d_identitas_buram.png", "page": 1, "field": "full_name", "value": None,
                     "signal": "uncertain"},
                    {"file": "d_identitas_buram.png", "page": 1, "field": "nik", "value": None,
                     "signal": "uncertain"},
                ],
            }],
            "expected_behavior": "Jangan menebak nama/NIK dari gambar buram; perbandingan nama formulir vs identitas ditandai tidak dapat dilakukan, bukan dianggap cocok.",
            "suggested_followup": ["Minta unggah ulang dokumen identitas yang lebih jelas."],
        },
        {
            "id": "e",
            "title": "Tanggal kuitansi sebelum tanggal kejadian",
            "upload": swap("04_kuitansi.pdf", "e_kuitansi_tanggal_sebelum_kejadian.pdf"),
            "expected_findings": [{
                "code": "DATE_INCONSISTENCY",
                "category": "konsistensi_tanggal",
                "status": "perlu_ditinjau",
                "description": "Tanggal kuitansi lebih awal dari tanggal kejadian/masuk perawatan.",
                "evidence": [
                    ev("e_kuitansi_tanggal_sebelum_kejadian.pdf", "receipt_date", RECEIPT_DATE_EARLY),
                    ev("01_formulir_klaim.pdf", "incident_date", INCIDENT_DATE),
                    ev("03_surat_keterangan_medis.pdf", "admission_date", INCIDENT_DATE),
                ],
            }],
            "suggested_followup": ["Konfirmasi tanggal kuitansi ke fasilitas kesehatan; minta kuitansi yang sesuai bila salah ketik."],
        },
        {
            "id": "f",
            "title": "Kuitansi belum diunggah",
            "upload": [f for f in base if f != "04_kuitansi.pdf"],
            "expected_findings": [{
                "code": "MISSING_DOCUMENT",
                "category": "kelengkapan_dokumen",
                "status": "perlu_ditinjau",
                "description": "Dokumen wajib kuitansi belum ada pada klaim.",
                "evidence": [
                    {"source": "checklist", "field": "document_type", "value": "kuitansi"},
                ],
            }],
            "suggested_followup": ["Minta peserta mengunggah kuitansi (status 'Perlu dokumen' hanya bila petugas setuju)."],
        },
    ]
    return {
        "_disclaimer": "SELURUH DATA SINTETIS/FIKTIF. Tidak ada data pribadi nyata. Temuan bersifat 'perlu ditinjau', bukan keputusan klaim.",
        "_generator": "scripts/gen_sample_data.py",
        "claim": CLAIM,
        "date_format_in_documents": "DD-MM-YYYY (field di file ini dinormalisasi ke YYYY-MM-DD)",
        "files": files,
        "scenarios": scenarios,
    }


def main():
    os.makedirs(OUT, exist_ok=True)
    p = lambda n: os.path.join(OUT, n)
    if "--expected-only" not in sys.argv[1:]:
        build_files(p)
    with open(p("expected.json"), "w", encoding="utf-8") as f:
        json.dump(expected(), f, ensure_ascii=False, indent=2)
        f.write("\n")
    print("sample-data/ dibuat", file=sys.stderr)


def build_files(p):
    formulir(p("01_formulir_klaim.pdf"), PERSON["full_name"], CLAIM["policy_number"])
    identitas(p("02_identitas.png"))
    surat_medis(p("03_surat_keterangan_medis.pdf"))
    kuitansi(p("04_kuitansi.pdf"), RECEIPT_DATE)
    formulir(p("b_formulir_klaim_nama_beda.pdf"), NAME_VARIANT, CLAIM["policy_number"])
    formulir(p("c_formulir_klaim_polis_beda.pdf"), PERSON["full_name"], POLICY_VARIANT)
    identitas(p("d_identitas_buram.png"), blur=6)
    kuitansi(p("e_kuitansi_tanggal_sebelum_kejadian.pdf"), RECEIPT_DATE_EARLY)


if __name__ == "__main__":
    main()
