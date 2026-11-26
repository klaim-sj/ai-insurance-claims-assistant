#!/usr/bin/env python3
"""Membuat set uji HOLDOUT2 SINTETIS di sample-data/holdout2/.

Dibuat dan di-commit SEBELUM perbaikan Tahap 5.5 (pra-pemrosesan OCR,
sinonim label, gating confidence) dijalankan terhadapnya. Variasi sengaja
berbeda dari holdout pertama: foto ala HP (perspektif + bayangan),
resolusi rendah, PDF hasil scan dua halaman tanpa lapisan teks, latar
berwarna dengan watermark tebal, font oblique, kemiringan hingga ±6°,
pencahayaan gelap, label dan format tanggal baru.

SELURUH ISI FIKTIF: nama, NIK (berawalan 0000), telepon (0800-0000-xxxx),
email (@example.com), fasilitas kesehatan, dan dokter. Setiap dokumen
bertanda watermark "CONTOH - DATA SINTETIS".

    python3 -I scripts/gen_holdout2_data.py

Kebutuhan: Python 3, reportlab, Pillow. Noise memakai seed tetap.
"""

import json
import os
import random
import sys

from PIL import Image, ImageDraw, ImageFilter, ImageFont
from reportlab.lib.pagesizes import A4
from reportlab.pdfgen import canvas

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, "sample-data", "holdout2")
WATERMARK = "CONTOH - DATA SINTETIS"

CLAIM = {
    "policy_number": "POL-UJI-3315",
    "claimant_name": "Siti Aminah Rahayu",
    "product_type": "Kesehatan (simulasi)",
    "received_at": "2026-08-20",
}
PERSON = {
    "full_name": "Siti Aminah Rahayu",
    "nik": "0000030303920003",
    "phone": "0800-0000-9012",
    "email": "siti.rahayu@example.com",
    "address": "Gg. Contoh Fiktif 7, Kota Sintetis",
}
EVENT_DATE = "2026-08-14"
DISCHARGE_DATE = "2026-08-16"
RECEIPT_DATE = "2026-08-16"
EARLY_RECEIPT_DATE = "2026-08-10"
EVAL_DATE = "2026-08-20"
HOSPITAL = "RSIA Contoh Harapan (fiktif)"
DOCTOR = "dr. Contoh Penanggung (fiktif)"
TOTAL = 3125000

NAME_TITLED_ABBREV = "Hj. S. Aminah Rahayu, S.E."
NAME_OTHER = "Siti Nurhaliza"
POLICY_OTHER = "POL-UJI-3351"

BULAN3 = ["Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"]


def dmy_dot(d):
    y, m, dd = d.split("-")
    return f"{dd}.{m}.{y}"


def dmy_short(d):
    y, m, dd = d.split("-")
    return f"{dd} {BULAN3[int(m) - 1]} {y}"


def dmy_slash(d):
    y, m, dd = d.split("-")
    return f"{dd}/{m}/{y}"


def rupiah(n):
    return "IDR " + f"{n:,}".replace(",", ".")


FONT_DIRS = ["/usr/share/fonts/truetype/freefont", "/usr/share/fonts/truetype/dejavu",
             "/usr/share/fonts/truetype/crosextra", "/Library/Fonts", "C:/Windows/Fonts"]


def font(names, size):
    for n in names:
        for d in FONT_DIRS:
            p = os.path.join(d, n)
            if os.path.exists(p):
                return ImageFont.truetype(p, size)
    return ImageFont.load_default(size)


SANS = ["FreeSans.ttf", "DejaVuSans.ttf"]
SANS_B = ["FreeSansBold.ttf", "DejaVuSans-Bold.ttf"]
SANS_I = ["FreeSansOblique.ttf", "DejaVuSans.ttf"]
CARLITO = ["Carlito-Regular.ttf", "FreeSans.ttf"]
CARLITO_B = ["Carlito-Bold.ttf", "FreeSansBold.ttf"]
MONO_I = ["FreeMonoOblique.ttf", "DejaVuSansMono-Oblique.ttf"]
MONO_B = ["FreeMonoBold.ttf", "DejaVuSansMono-Bold.ttf"]


def watermark(img, f, alpha=60, color=(120, 120, 120)):
    wm = Image.new("RGBA", img.size, (0, 0, 0, 0))
    d = ImageDraw.Draw(wm)
    d.text((img.size[0] / 2, img.size[1] / 2), WATERMARK, font=f, fill=color + (alpha,), anchor="mm")
    wm = wm.rotate(28, center=(img.size[0] / 2, img.size[1] / 2))
    return Image.alpha_composite(img.convert("RGBA"), wm).convert("RGB")


def noise(img, seed, strength):
    rnd = random.Random(seed)
    w, h = img.size
    n = Image.frombytes("L", (w, h), rnd.randbytes(w * h)).convert(img.mode)
    return Image.blend(img, n, strength / 100.0)


def shadow(img, left=0.55, right=1.0):
    """Gradien pencahayaan horizontal (bayangan tangan/ponsel)."""
    w, h = img.size
    grad = Image.linear_gradient("L").rotate(90).resize((w, h))
    grad = grad.point(lambda v: int(255 * (left + (right - left) * (255 - v) / 255)))
    out = Image.composite(img, Image.new(img.mode, img.size, 0), grad)
    return Image.blend(img, out, 0.85)


def perspective(img, inset, fill):
    """Perspektif ringan: tepi atas lebih sempit dari tepi bawah."""
    w, h = img.size
    quad = (-inset, -inset // 2, 0, h, w, h, w + inset, -inset // 2)
    return img.transform((w, h), Image.QUAD, quad, resample=Image.BICUBIC, fillcolor=fill)


def rows_img(W, H, title, subtitle, rows, f, fb, lx, vx, y0, step, bg="white", line=False):
    img = Image.new("RGB", (W, H), bg)
    d = ImageDraw.Draw(img)
    d.text((W / 2, y0 - 120), title, font=fb, fill="black", anchor="mm")
    d.text((W / 2, y0 - 75), subtitle, font=f, fill="black", anchor="mm")
    y = y0
    for label, value in rows:
        d.text((lx, y), label, font=f, fill="black")
        d.text((vx, y), value, font=f, fill="black")
        if line:
            d.line([lx, y + step - 12, W - lx, y + step - 12], fill=(150, 150, 150), width=1)
        y += step
    return img, d, y


# g01: formulir difoto dengan ponsel (perspektif, bayangan, miring 5°).
def formulir_foto(path, name, policy, seed, angle=5.0):
    rows = [
        ("Nama Pemohon", ": " + name),
        ("No Polis", ": " + policy),
        ("No. KTP / NIK", ": " + PERSON["nik"]),
        ("HP", ": " + PERSON["phone"]),
        ("Tgl Kejadian", ": " + dmy_dot(EVENT_DATE)),
        ("Tgl Pulang", ": " + dmy_dot(DISCHARGE_DATE)),
        ("Rumah Sakit", ": " + HOSPITAL),
        ("Total Tagihan", ": " + rupiah(TOTAL)),
    ]
    img, d, y = rows_img(1300, 1700, "FORMULIR KLAIM RAWAT INAP", WATERMARK + " (uji holdout2)",
                         rows, font(SANS, 30), font(SANS_B, 42), 110, 470, 300, 62, line=True)
    d.text((110, y + 60), "Diisi oleh peserta (contoh).  TTD: [CONTOH]", font=font(SANS, 26), fill="black")
    img = watermark(img, font(SANS_B, 100))
    img = perspective(img, 60, (60, 60, 60))
    img = img.rotate(angle, resample=Image.BICUBIC, expand=True, fillcolor=(70, 70, 70))
    img = shadow(img, 0.5, 1.0)
    img = noise(img, seed, 8).filter(ImageFilter.GaussianBlur(0.8))
    img.save(path, "JPEG", quality=65)


# g05: formulir dua kolom, font Carlito, nama bergelar + singkatan.
def formulir_dua_kolom(path, name, seed, angle=-2.5):
    W, H = 1400, 1100
    img = Image.new("RGB", (W, H), (252, 252, 247))
    d = ImageDraw.Draw(img)
    f, fb = font(CARLITO, 30), font(CARLITO_B, 40)
    d.text((70, 60), "PERMOHONAN PENGGANTIAN BIAYA", font=fb, fill=(10, 30, 90))
    d.text((70, 110), WATERMARK + " / holdout2", font=f, fill=(10, 30, 90))
    left = [("Nama Pemohon", name), ("No Polis", CLAIM["policy_number"]), ("No. KTP / NIK", PERSON["nik"])]
    right = [("Tgl Kejadian", dmy_dot(EVENT_DATE)), ("Rumah Sakit", HOSPITAL), ("Total Tagihan", rupiah(TOTAL))]
    for col, items in ((70, left), (730, right)):
        y = 200
        for label, value in items:
            d.text((col, y), label, font=font(CARLITO_B, 26), fill="black")
            d.text((col, y + 36), value, font=f, fill="black")
            d.line([col, y + 80, col + 600, y + 80], fill=(120, 120, 120), width=1)
            y += 120
    img = watermark(img, font(CARLITO_B, 90))
    img = img.rotate(angle, resample=Image.BICUBIC, expand=True, fillcolor=(240, 240, 240))
    noise(img, seed, 6).save(path, "JPEG", quality=70)


# g08: formulir tabel dengan polis berbeda, PNG noise sedang.
def formulir_tabel(path, policy, seed, angle=1.0):
    W, H = 1240, 1300
    img = Image.new("RGB", (W, H), "white")
    d = ImageDraw.Draw(img)
    f = font(SANS, 28)
    d.text((W / 2, 80), "FORMULIR KLAIM RAWAT INAP", font=font(SANS_B, 40), fill="black", anchor="mm")
    d.text((W / 2, 125), WATERMARK, font=font(SANS, 22), fill="black", anchor="mm")
    rows = [("Nama Pemohon", PERSON["full_name"]), ("No Polis", policy), ("No. KTP / NIK", PERSON["nik"]),
            ("Tgl Kejadian", dmy_dot(EVENT_DATE)), ("Rumah Sakit", HOSPITAL), ("Total Tagihan", rupiah(TOTAL))]
    y = 200
    for label, value in rows:
        d.rectangle([80, y, 1160, y + 76], outline="black", width=3)
        d.line([440, y, 440, y + 76], fill="black", width=3)
        d.text((100, y + 22), label, font=f, fill="black")
        d.text((460, y + 22), value, font=f, fill="black")
        y += 76
    img = watermark(img, font(SANS_B, 90))
    img = img.rotate(angle, resample=Image.BICUBIC, expand=True, fillcolor="white")
    noise(img, seed, 14).convert("L").save(path, "PNG", optimize=True)


# g02: identitas resolusi rendah (~100 dpi).
def identitas_lowres(path, seed):
    W, H = 560, 350
    img = Image.new("RGB", (W, H), (230, 238, 250))
    d = ImageDraw.Draw(img)
    d.rectangle([4, 4, W - 4, H - 4], outline=(40, 60, 120), width=3)
    d.text((16, 12), "KARTU PESERTA CONTOH", font=font(SANS_B, 22), fill=(20, 30, 80))
    d.text((16, 42), WATERMARK, font=font(SANS, 13), fill=(160, 20, 20))
    f = font(SANS, 16)
    y = 85
    for label, value in [("NIK", PERSON["nik"]), ("Nama", PERSON["full_name"]), ("Alamat", PERSON["address"])]:
        d.text((16, y), label, font=f, fill="black")
        d.text((130, y), ": " + value, font=f, fill="black")
        y += 36
    d.rectangle([440, 85, 540, 215], outline=(100, 100, 100), width=1)
    img = watermark(img, font(SANS_B, 36))
    noise(img, seed, 5).save(path, "PNG", optimize=True)


# g09: identitas difoto gelap dan buram (sengaja tidak terbaca).
def identitas_gelap(path, seed):
    W, H = 1000, 640
    img = Image.new("RGB", (W, H), (200, 205, 210))
    d = ImageDraw.Draw(img)
    d.text((30, 30), "KARTU PESERTA CONTOH", font=font(SANS_B, 40), fill=(20, 30, 80))
    d.text((30, 90), WATERMARK, font=font(SANS, 22), fill=(160, 20, 20))
    f = font(SANS, 30)
    y = 170
    for label, value in [("NIK", PERSON["nik"]), ("Nama", PERSON["full_name"]), ("Alamat", PERSON["address"])]:
        d.text((30, y), label, font=f, fill="black")
        d.text((250, y), ": " + value, font=f, fill="black")
        y += 70
    img = img.point(lambda v: int(v * 0.28))  # under-exposed
    img = img.rotate(-4, resample=Image.BICUBIC, expand=True, fillcolor=(10, 10, 10))
    img = img.filter(ImageFilter.GaussianBlur(3.2))
    noise(img, seed, 30).save(path, "JPEG", quality=50)


# g03: surat keterangan medis, PDF hasil scan 2 halaman tanpa lapisan teks.
def surat_medis_scan_pdf(path, seed):
    W, H = 1240, 1754
    p1 = Image.new("RGB", (W, H), "white")
    d = ImageDraw.Draw(p1)
    d.text((W / 2, 110), "RESUME MEDIS", font=font(SANS_B, 44), fill="black", anchor="mm")
    d.text((W / 2, 165), HOSPITAL + " - " + WATERMARK, font=font(SANS, 24), fill="black", anchor="mm")
    d.text((100, 260), "Halaman 1 dari 2. Data pasien ada di halaman berikut.", font=font(SANS, 28), fill="black")
    d.text((100, 320), "Ringkasan perawatan: rawat inap (contoh generik).", font=font(SANS, 28), fill="black")
    p2 = Image.new("RGB", (W, H), "white")
    d = ImageDraw.Draw(p2)
    f = font(SANS_I, 30)
    y = 200
    for label, value in [("Nama Pasien", PERSON["full_name"]), ("Dirawat sejak", dmy_short(EVENT_DATE)),
                         ("Keluar", dmy_short(DISCHARGE_DATE)), ("Dokter", DOCTOR)]:
        d.text((100, y), label, font=f, fill="black")
        d.text((470, y), ": " + value, font=f, fill="black")
        y += 64
    d.text((100, y + 60), f"Kota Sintetis, {dmy_short(DISCHARGE_DATE)}", font=f, fill="black")
    pages = []
    for i, p in enumerate((p1, p2)):
        p = watermark(p, font(SANS_B, 100))
        p = p.rotate(-1.2 + i * 2.4, resample=Image.BICUBIC, expand=False, fillcolor="white")
        pages.append(noise(p, seed + i, 7).convert("L"))
    pages[0].save(path, "PDF", resolution=150, save_all=True, append_images=pages[1:])


# g07: surat keterangan medis PDF teks (Helvetica-Oblique), nama pasien berbeda.
def surat_medis_pdf_teks(path, name):
    c = canvas.Canvas(path, pagesize=A4, invariant=1)
    W, H = A4
    c.setFont("Helvetica-BoldOblique", 15)
    c.drawString(60, H - 70, "RESUME MEDIS")
    c.setFont("Helvetica-Oblique", 9)
    c.drawString(60, H - 86, HOSPITAL + " / " + WATERMARK)
    c.setFont("Helvetica-Oblique", 11)
    y = H - 130
    for label, value in [("Nama Pasien", name), ("Dirawat sejak", dmy_short(EVENT_DATE)),
                         ("Keluar", dmy_short(DISCHARGE_DATE)), ("Dokter", DOCTOR)]:
        c.drawString(60, y, label)
        c.drawString(200, y, ": " + value)
        y -= 20
    c.showPage()
    c.save()


# g04/g06: kuitansi, latar berwarna / font oblique, watermark tebal.
def kuitansi(path, date, seed, angle, bg, fmt, fonts):
    W, H = 900, 1150
    img = Image.new("RGB", (W, H), bg)
    d = ImageDraw.Draw(img)
    f, fb = font(fonts[0], 26), font(fonts[1], 34)
    d.text((W / 2, 70), "BUKTI PEMBAYARAN", font=fb, fill="black", anchor="mm")
    d.text((W / 2, 112), HOSPITAL, font=f, fill="black", anchor="mm")
    y = 200
    for label, value in [("No. Bukti", "BP-UJI-0907"), ("Tanggal", dmy_slash(date)),
                         ("Sudah terima dari", PERSON["full_name"])]:
        d.text((50, y), label, font=f, fill="black")
        d.text((380, y), ": " + value, font=f, fill="black")
        y += 55
    y += 30
    for item, amt in [("Rawat inap", 1800000), ("Visite dokter", 525000), ("Farmasi", 800000)]:
        d.text((50, y), item, font=f, fill="black")
        d.text((W - 50, y), rupiah(amt), font=f, fill="black", anchor="ra")
        y += 48
    d.line([40, y + 10, W - 40, y + 10], fill="black", width=2)
    d.text((50, y + 40), "Total Bayar", font=fb, fill="black")
    d.text((W - 50, y + 40), rupiah(TOTAL), font=fb, fill="black", anchor="ra")
    img = watermark(img, font(fonts[1], 80), alpha=110, color=(200, 40, 40))
    img = img.rotate(angle, resample=Image.BICUBIC, expand=True, fillcolor=bg)
    img = noise(img, seed, 8)
    if fmt == "JPEG":
        img.save(path, "JPEG", quality=70)
    else:
        img.save(path, "PNG", optimize=True)


def expected():
    form = {"claimant_name": PERSON["full_name"], "nik": PERSON["nik"], "policy_number": CLAIM["policy_number"],
            "incident_date": EVENT_DATE, "total_amount": TOTAL}
    ident = {"full_name": PERSON["full_name"], "nik": PERSON["nik"]}
    med = {"patient_name": PERSON["full_name"], "admission_date": EVENT_DATE}
    rec = {"payer_name": PERSON["full_name"], "receipt_date": RECEIPT_DATE, "total_amount": TOTAL}
    files = {
        "g01_formulir_foto_hp.jpg": {"document_type": "formulir_klaim", "mime_type": "image/jpeg", "pages": 1, "fields": form},
        "g02_identitas_resolusi_rendah.png": {"document_type": "identitas", "mime_type": "image/png", "pages": 1, "fields": ident},
        "g03_resume_medis_scan_2hal.pdf": {"document_type": "surat_keterangan_medis", "mime_type": "application/pdf", "pages": 2,
                                           "fields": med, "note": "PDF hasil scan tanpa lapisan teks; data pasien di halaman 2."},
        "g04_bukti_bayar_latar_kuning.jpg": {"document_type": "kuitansi", "mime_type": "image/jpeg", "pages": 1, "fields": rec},
        "g05_formulir_dua_kolom_nama_gelar.jpg": {"document_type": "formulir_klaim", "mime_type": "image/jpeg", "pages": 1,
                                                  "fields": {**form, "claimant_name": NAME_TITLED_ABBREV}},
        "g06_bukti_bayar_tanggal_sebelum.png": {"document_type": "kuitansi", "mime_type": "image/png", "pages": 1,
                                                "fields": {**rec, "receipt_date": EARLY_RECEIPT_DATE}},
        "g07_resume_medis_nama_beda.pdf": {"document_type": "surat_keterangan_medis", "mime_type": "application/pdf", "pages": 1,
                                           "fields": {**med, "patient_name": NAME_OTHER}},
        "g08_formulir_tabel_polis_beda.png": {"document_type": "formulir_klaim", "mime_type": "image/png", "pages": 1,
                                              "fields": {**form, "policy_number": POLICY_OTHER}},
        "g09_identitas_gelap_buram.jpg": {"document_type": "identitas", "mime_type": "image/jpeg", "pages": 1, "fields": ident,
                                          "expected_unreadable": True, "note": "Sengaja gelap dan buram; sistem tidak boleh menebak."},
    }
    base = ["g01_formulir_foto_hp.jpg", "g02_identitas_resolusi_rendah.png",
            "g03_resume_medis_scan_2hal.pdf", "g04_bukti_bayar_latar_kuning.jpg"]

    def swap(old, new):
        return [new if f == old else f for f in base]

    def fnd(code, *evidence):
        return {"code": code, "status": "perlu_ditinjau", "evidence": list(evidence)}

    def ev(file, field, page=1):
        return {"file": file, "page": page, "field": field}

    scenarios = [
        {"id": "ga", "title": "Set lengkap (foto HP, resolusi rendah, scan 2 halaman, latar warna)", "upload": base,
         "expected_findings": []},
        {"id": "gb", "title": "Formulir dua kolom, nama bergelar dan disingkat",
         "upload": swap(base[0], "g05_formulir_dua_kolom_nama_gelar.jpg"),
         "expected_findings": [fnd("NAME_MISMATCH", ev("g05_formulir_dua_kolom_nama_gelar.jpg", "claimant_name"),
                                   ev("g02_identitas_resolusi_rendah.png", "full_name"))],
         "note": "Diharapkan severity rendah (kemungkinan singkatan)."},
        {"id": "gc", "title": "Tanggal bukti bayar sebelum tanggal kejadian",
         "upload": swap(base[3], "g06_bukti_bayar_tanggal_sebelum.png"),
         "expected_findings": [fnd("DATE_INCONSISTENCY", ev("g06_bukti_bayar_tanggal_sebelum.png", "receipt_date"))]},
        {"id": "gd", "title": "Nama pasien di resume medis berbeda",
         "upload": swap(base[2], "g07_resume_medis_nama_beda.pdf"),
         "expected_findings": [fnd("NAME_MISMATCH", ev("g07_resume_medis_nama_beda.pdf", "patient_name"),
                                   ev("g02_identitas_resolusi_rendah.png", "full_name"))]},
        {"id": "ge", "title": "Nomor polis formulir tabel berbeda",
         "upload": swap(base[0], "g08_formulir_tabel_polis_beda.png"),
         "expected_findings": [fnd("POLICY_MISMATCH", ev("g08_formulir_tabel_polis_beda.png", "policy_number"),
                                   {"source": "claim_record", "field": "policy_number"})]},
        {"id": "gf", "title": "Identitas gelap dan buram", "upload": swap(base[1], "g09_identitas_gelap_buram.jpg"),
         "expected_findings": [fnd("LOW_QUALITY_DOCUMENT", ev("g09_identitas_gelap_buram.jpg", None)),
                               fnd("NEEDS_MANUAL_VERIFICATION", ev("g09_identitas_gelap_buram.jpg", "full_name"))]},
        {"id": "gg", "title": "Hanya identitas dan bukti bayar", "upload": [base[1], base[3]],
         "expected_findings": [fnd("MISSING_DOCUMENT", {"source": "checklist", "field": "document_type"})]},
    ]
    return {
        "_disclaimer": "SELURUH DATA SINTETIS/FIKTIF. Set HOLDOUT2: dibekukan sebelum perbaikan Tahap 5.5 dijalankan terhadapnya. Temuan bersifat 'perlu ditinjau', bukan keputusan klaim.",
        "_generator": "scripts/gen_holdout2_data.py",
        "_semantics": "Ekspektasi memakai semantik Tahap 5.5: field tidak terbaca pada dokumen kualitas rendah = NEEDS_MANUAL_VERIFICATION, bukan MISSING_FIELD.",
        "claim": CLAIM,
        "evaluation_date": EVAL_DATE,
        "files": files,
        "scenarios": scenarios,
    }


def main():
    os.makedirs(OUT, exist_ok=True)
    p = lambda n: os.path.join(OUT, n)
    formulir_foto(p("g01_formulir_foto_hp.jpg"), PERSON["full_name"], CLAIM["policy_number"], seed=201)
    identitas_lowres(p("g02_identitas_resolusi_rendah.png"), seed=202)
    surat_medis_scan_pdf(p("g03_resume_medis_scan_2hal.pdf"), seed=203)
    kuitansi(p("g04_bukti_bayar_latar_kuning.jpg"), RECEIPT_DATE, 204, -6.0, (250, 236, 160), "JPEG", (MONO_I, MONO_B))
    formulir_dua_kolom(p("g05_formulir_dua_kolom_nama_gelar.jpg"), NAME_TITLED_ABBREV, seed=205)
    kuitansi(p("g06_bukti_bayar_tanggal_sebelum.png"), EARLY_RECEIPT_DATE, 206, 2.0, (255, 255, 255), "PNG", (SANS_I, SANS_B))
    surat_medis_pdf_teks(p("g07_resume_medis_nama_beda.pdf"), NAME_OTHER)
    formulir_tabel(p("g08_formulir_tabel_polis_beda.png"), POLICY_OTHER, seed=208)
    identitas_gelap(p("g09_identitas_gelap_buram.jpg"), seed=209)
    with open(p("expected.json"), "w", encoding="utf-8") as f:
        json.dump(expected(), f, ensure_ascii=False, indent=2)
        f.write("\n")
    print("sample-data/holdout2/ dibuat", file=sys.stderr)


if __name__ == "__main__":
    main()
