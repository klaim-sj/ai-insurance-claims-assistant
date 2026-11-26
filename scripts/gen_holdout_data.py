#!/usr/bin/env python3
"""Membuat set uji HOLDOUT SINTETIS di sample-data/holdout/.

Set ini sengaja dibuat berbeda dari sample-data/ (generator lain, tata
letak tabel/kartu lain, font serif/monospace, kemiringan, noise, JPEG,
variasi label dan format tanggal) dan TIDAK dipakai untuk menyetel aturan
ekstraksi maupun aturan validasi. Aturan dibekukan sebelum set ini dibuat.

SELURUH ISI FIKTIF: nama, NIK (berawalan 0000), telepon (0800-0000-xxxx),
email (@example.com), fasilitas kesehatan, dan dokter. Setiap dokumen
bertanda watermark "CONTOH - DATA SINTETIS".

    python3 -I scripts/gen_holdout_data.py
    python3 -I scripts/gen_holdout_data.py --expected-only   # hanya expected.json

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
OUT = os.path.join(ROOT, "sample-data", "holdout")
WATERMARK = "CONTOH - DATA SINTETIS"

CLAIM = {
    "policy_number": "POL-UJI-2207",
    "claimant_name": "Budi Santoso Pratama",
    "product_type": "Kesehatan (simulasi)",
    "received_at": "2026-09-10",
}
PERSON = {
    "full_name": "Budi Santoso Pratama",
    "nik": "0000020202850002",
    "phone": "0800-0000-5678",
    "email": "budi.pratama@example.com",
    "address": "Jl. Uji Fiktif No. 22, Kota Contoh",
}
EVENT_DATE = "2026-09-03"
DISCHARGE_DATE = "2026-09-05"
RECEIPT_DATE = "2026-09-05"
EVAL_DATE = "2026-09-10"
FUTURE_RECEIPT_DATE = "2026-11-21"
HOSPITAL = "Klinik Uji Sejahtera (fiktif)"
DOCTOR = "dr. Uji Pemeriksa (fiktif)"
TOTAL = 1850000

NAME_WITH_TITLE = "Ir. Budi Santoso Pratama"
NAME_ABBREV = "B. Santoso Pratama"
NAME_OTHER = "Budi Setiawan"
POLICY_OTHER = "POL-UJI-2270"

BULAN = ["Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli",
         "Agustus", "September", "Oktober", "November", "Desember"]


def slash(d):
    y, m, dd = d.split("-")
    return f"{dd}/{m}/{y}"


def dash(d):
    y, m, dd = d.split("-")
    return f"{dd}-{m}-{y}"


def words(d):
    y, m, dd = d.split("-")
    return f"{int(dd)} {BULAN[int(m) - 1]} {y}"


def rupiah(n):
    return "Rp" + f"{n:,}".replace(",", ".") + ",-"


FONT_DIRS = ["/usr/share/fonts/truetype/dejavu", "/usr/share/fonts/truetype/freefont",
             "/Library/Fonts", "C:/Windows/Fonts"]


def font(names, size):
    for n in names:
        for d in FONT_DIRS:
            p = os.path.join(d, n)
            if os.path.exists(p):
                return ImageFont.truetype(p, size)
    return ImageFont.load_default(size)


SERIF = ["DejaVuSerif.ttf", "FreeSerif.ttf"]
SERIF_B = ["DejaVuSerif-Bold.ttf", "FreeSerifBold.ttf"]
MONO = ["DejaVuSansMono.ttf", "FreeMono.ttf"]
MONO_B = ["DejaVuSansMono-Bold.ttf", "FreeMonoBold.ttf"]


# --- Degradasi gambar (deterministik) ---------------------------------------
def degrade(img, seed, angle=0.0, noise=0, blur=0.0, contrast=1.0):
    rnd = random.Random(seed)
    img = img.convert("L")
    if contrast != 1.0:
        img = img.point(lambda v: int(128 + (v - 128) * contrast))
    if angle:
        img = img.rotate(angle, resample=Image.BICUBIC, expand=True, fillcolor=245)
    if blur:
        img = img.filter(ImageFilter.GaussianBlur(blur))
    if noise:
        w, h = img.size
        n = Image.frombytes("L", (w, h), rnd.randbytes(w * h))
        # Noise acak di sekitar abu-abu; noise% menentukan kekuatan campuran.
        img = Image.blend(img, n, noise / 100.0)
        # Bintik hitam (debu scan).
        d = ImageDraw.Draw(img)
        for _ in range(int(w * h / 4000)):
            x, y = rnd.randrange(w), rnd.randrange(h)
            r = rnd.choice([1, 1, 2])
            d.ellipse([x, y, x + r, y + r], fill=rnd.randrange(0, 90))
    return img


def watermark(img, text_font):
    wm = Image.new("RGBA", img.size, (0, 0, 0, 0))
    d = ImageDraw.Draw(wm)
    d.text((img.size[0] / 2, img.size[1] / 2), WATERMARK, font=text_font, fill=(120, 120, 120, 60), anchor="mm")
    wm = wm.rotate(30, center=(img.size[0] / 2, img.size[1] / 2))
    return Image.alpha_composite(img.convert("RGBA"), wm).convert("RGB")


# --- Formulir klaim: tabel bergaris, font serif, hasil "scan" JPEG -----------
def formulir_scan(path, name, policy, seed, angle):
    W, H = 1240, 1754  # A4 150 dpi
    img = Image.new("RGB", (W, H), "white")
    d = ImageDraw.Draw(img)
    d.text((W / 2, 90), "KLAIM MANFAAT RAWAT INAP", font=font(SERIF_B, 40), fill="black", anchor="mm")
    d.text((W / 2, 140), "Formulir pengajuan (contoh) - " + WATERMARK, font=font(SERIF, 22), fill="black", anchor="mm")
    rows = [
        ("Nama Tertanggung", name),
        ("Nomor Polis", policy),
        ("NIK", PERSON["nik"]),
        ("No. Telepon", PERSON["phone"]),
        ("Email", PERSON["email"]),
        ("Tanggal Kejadian", slash(EVENT_DATE)),
        ("Tanggal Keluar", slash(DISCHARGE_DATE)),
        ("Fasilitas Kesehatan", HOSPITAL),
        ("Total Klaim", rupiah(TOTAL)),
    ]
    x0, x1, x2, y = 90, 470, 1150, 220
    f = font(SERIF, 28)
    for label, value in rows:
        d.rectangle([x0, y, x2, y + 70], outline="black", width=2)
        d.line([x1, y, x1, y + 70], fill="black", width=2)
        d.text((x0 + 15, y + 20), label, font=f, fill="black")
        d.text((x1 + 15, y + 20), value, font=f, fill="black")
        y += 70
    y += 60
    d.text((x0, y), "Pernyataan: data di atas diisi untuk pengujian (contoh).", font=font(SERIF, 24), fill="black")
    d.text((x0, y + 50), f"Kota Contoh, {slash(CLAIM['received_at'])}    Tanda tangan: [CONTOH]", font=font(SERIF, 24), fill="black")
    img = watermark(img, font(SERIF_B, 90))
    degrade(img, seed, angle=angle, noise=12).save(path, "JPEG", quality=70)


# --- Formulir klaim versi PDF monospace (polis berbeda) ----------------------
def formulir_pdf_mono(path, name, policy):
    c = canvas.Canvas(path, pagesize=A4, invariant=1)
    W, H = A4
    c.setFont("Courier-Bold", 15)
    c.drawString(60, H - 70, "PENGAJUAN KLAIM KESEHATAN")
    c.setFont("Courier", 9)
    c.drawString(60, H - 86, WATERMARK + " / dokumen uji holdout")
    rows = [
        ("Nama Tertanggung", name),
        ("Nomor Polis", policy),
        ("NIK", PERSON["nik"]),
        ("Tanggal Kejadian", slash(EVENT_DATE)),
        ("Tanggal Keluar", slash(DISCHARGE_DATE)),
        ("Fasilitas Kesehatan", HOSPITAL),
        ("Total Klaim", rupiah(TOTAL)),
    ]
    y = H - 130
    c.setFont("Courier", 11)
    for label, value in rows:
        c.drawString(60, y, f"{label:<22}: {value}")
        c.line(60, y - 6, 540, y - 6)
        y -= 26
    c.saveState()
    c.setFont("Courier-Bold", 50)
    c.setFillColorRGB(0.85, 0.85, 0.85)
    c.translate(W / 2, H / 2)
    c.rotate(35)
    c.drawCentredString(0, 0, "CONTOH")
    c.restoreState()
    c.showPage()
    c.save()


# --- Surat keterangan medis: PDF serif, label/format tanggal berbeda ---------
def surat_medis_pdf(path, name):
    c = canvas.Canvas(path, pagesize=A4, invariant=1)
    W, H = A4
    c.setFont("Times-Bold", 16)
    c.drawCentredString(W / 2, H - 70, "SURAT KETERANGAN DOKTER")
    c.setFont("Times-Italic", 10)
    c.drawCentredString(W / 2, H - 86, HOSPITAL + " - " + WATERMARK)
    c.setFont("Times-Roman", 12)
    y = H - 130
    c.drawString(70, y, "Yang bertanda tangan di bawah ini menerangkan bahwa:")
    y -= 30
    for label, value in [
        ("Nama Pasien", name),
        ("Tanggal Masuk Perawatan", words(EVENT_DATE)),
        ("Tanggal Keluar", words(DISCHARGE_DATE)),
        ("Keterangan", "Rawat inap (contoh generik)"),
    ]:
        c.drawString(90, y, label)
        c.drawString(260, y, ": " + value)
        y -= 22
    y -= 20
    c.drawString(70, y, f"Kota Contoh, {words(DISCHARGE_DATE)}")
    c.drawString(70, y - 18, DOCTOR)
    c.showPage()
    c.save()


def surat_medis_scan(path, name, seed, angle):
    W, H = 1240, 1754
    img = Image.new("RGB", (W, H), "white")
    d = ImageDraw.Draw(img)
    d.text((W / 2, 100), "SURAT KETERANGAN DOKTER", font=font(SERIF_B, 38), fill="black", anchor="mm")
    d.text((W / 2, 150), HOSPITAL + " - " + WATERMARK, font=font(SERIF, 22), fill="black", anchor="mm")
    f = font(SERIF, 28)
    y = 250
    d.text((100, y), "Yang bertanda tangan di bawah ini menerangkan bahwa:", font=f, fill="black")
    y += 70
    for label, value in [
        ("Nama Pasien", name),
        ("Tanggal Masuk Perawatan", words(EVENT_DATE)),
        ("Tanggal Keluar", words(DISCHARGE_DATE)),
        ("Keterangan", "Rawat inap (contoh generik)"),
    ]:
        d.text((130, y), label, font=f, fill="black")
        d.text((560, y), ": " + value, font=f, fill="black")
        y += 55
    d.text((100, y + 60), f"Kota Contoh, {words(DISCHARGE_DATE)}", font=f, fill="black")
    d.text((100, y + 110), DOCTOR, font=f, fill="black")
    img = watermark(img, font(SERIF_B, 90))
    degrade(img, seed, angle=angle, noise=10).save(path, "JPEG", quality=72)


# --- Kuitansi: struk monospace sempit, PNG/JPEG miring -----------------------
def kuitansi_scan(path, name, date, seed, angle, fmt="JPEG"):
    W, H = 900, 1300
    img = Image.new("RGB", (W, H), (250, 248, 240))
    d = ImageDraw.Draw(img)
    fb, f = font(MONO_B, 32), font(MONO, 24)
    d.text((W / 2, 70), "KWITANSI", font=fb, fill="black", anchor="mm")
    d.text((W / 2, 110), HOSPITAL, font=f, fill="black", anchor="mm")
    d.text((W / 2, 145), WATERMARK, font=f, fill="black", anchor="mm")
    d.line([40, 180, W - 40, 180], fill="black", width=2)
    y = 210
    for label, value in [
        ("No.", "KWT-UJI-0042"),
        ("Tanggal Pembayaran", dash(date)),
        ("Telah terima dari", name),
    ]:
        d.text((50, y), f"{label}", font=f, fill="black")
        d.text((370, y), f": {value}", font=f, fill="black")
        y += 50
    d.line([40, y + 10, W - 40, y + 10], fill="black", width=1)
    y += 40
    for item, amt in [("Kamar 2 malam", 900000), ("Jasa dokter", 350000), ("Obat", 600000)]:
        d.text((50, y), item, font=f, fill="black")
        d.text((W - 50, y), rupiah(amt), font=f, fill="black", anchor="ra")
        y += 45
    d.line([40, y + 10, W - 40, y + 10], fill="black", width=2)
    y += 40
    d.text((50, y), "Jumlah Dibayar", font=fb, fill="black")
    d.text((W - 50, y), rupiah(TOTAL), font=fb, fill="black", anchor="ra")
    img = watermark(img, font(MONO_B, 60))
    out = degrade(img, seed, angle=angle, noise=10)
    if fmt == "JPEG":
        out.save(path, "JPEG", quality=68)
    else:
        out.save(path, "PNG", optimize=True)


# --- Identitas: kartu monospace, latar bergradasi ----------------------------
def identitas_scan(path, seed, angle, noise, blur=0.0, contrast=1.0, fmt="PNG"):
    W, H = 1100, 700
    img = Image.new("RGB", (W, H), (225, 235, 225))
    d = ImageDraw.Draw(img)
    for i in range(0, H, 6):  # pola garis latar
        d.line([0, i, W, i], fill=(215, 228, 215), width=2)
    d.rectangle([12, 12, W - 12, H - 12], outline=(40, 90, 60), width=5)
    d.text((40, 30), "KARTU PESERTA UJI", font=font(MONO_B, 42), fill=(20, 60, 30))
    d.text((40, 90), WATERMARK + " / BUKAN DOKUMEN ASLI", font=font(MONO, 22), fill=(150, 20, 20))
    f = font(MONO, 28)
    y = 170
    for label, value in [
        ("NIK", PERSON["nik"]),
        ("Nama Lengkap", PERSON["full_name"]),
        ("Alamat", PERSON["address"]),
    ]:
        d.text((40, y), label, font=f, fill="black")
        d.text((300, y), ": " + value, font=f, fill="black")
        y += 70
    d.rectangle([850, 170, 1060, 430], outline=(90, 90, 90), width=2)
    d.text((955, 300), "[FOTO]", font=font(MONO, 26), fill=(90, 90, 90), anchor="mm")
    img = watermark(img, font(MONO_B, 70))
    out = degrade(img, seed, angle=angle, noise=noise, blur=blur, contrast=contrast)
    if fmt == "JPEG":
        out.save(path, "JPEG", quality=55)
    else:
        out.save(path, "PNG", optimize=True)


# --- expected.json -----------------------------------------------------------
def expected():
    form = {
        "claimant_name": PERSON["full_name"], "nik": PERSON["nik"], "policy_number": CLAIM["policy_number"],
        "incident_date": EVENT_DATE, "total_amount": TOTAL,
    }
    ident = {"full_name": PERSON["full_name"], "nik": PERSON["nik"]}
    med = {"patient_name": PERSON["full_name"], "admission_date": EVENT_DATE}
    rec = {"payer_name": PERSON["full_name"], "receipt_date": RECEIPT_DATE, "total_amount": TOTAL}
    files = {
        "h01_formulir_klaim_tabel_scan.jpg": {"document_type": "formulir_klaim", "mime_type": "image/jpeg", "pages": 1,
                                              "fields": {**form, "claimant_name": NAME_WITH_TITLE},
                                              "note": "Nama memakai gelar 'Ir.'; setelah normalisasi sama dengan data klaim."},
        "h02_identitas_kartu_mono.png": {"document_type": "identitas", "mime_type": "image/png", "pages": 1, "fields": ident},
        "h03_surat_keterangan_dokter.pdf": {"document_type": "surat_keterangan_medis", "mime_type": "application/pdf", "pages": 1, "fields": med},
        "h04_kwitansi_struk.jpg": {"document_type": "kuitansi", "mime_type": "image/jpeg", "pages": 1, "fields": rec},
        "h05_kwitansi_nama_singkat.jpg": {"document_type": "kuitansi", "mime_type": "image/jpeg", "pages": 1,
                                          "fields": {**rec, "payer_name": NAME_ABBREV}},
        "h06_formulir_mono_polis_beda.pdf": {"document_type": "formulir_klaim", "mime_type": "application/pdf", "pages": 1,
                                             "fields": {**form, "policy_number": POLICY_OTHER}},
        "h07_kwitansi_tanggal_depan.png": {"document_type": "kuitansi", "mime_type": "image/png", "pages": 1,
                                           "fields": {**rec, "receipt_date": FUTURE_RECEIPT_DATE}},
        "h08_identitas_noise_berat.jpg": {"document_type": "identitas", "mime_type": "image/jpeg", "pages": 1, "fields": ident,
                                          "expected_unreadable": True,
                                          "note": "Noise berat + kontras rendah + buram; sistem tidak boleh menebak."},
        "h09_surat_dokter_nama_beda_scan.jpg": {"document_type": "surat_keterangan_medis", "mime_type": "image/jpeg", "pages": 1,
                                                "fields": {**med, "patient_name": NAME_OTHER}},
    }
    base = ["h01_formulir_klaim_tabel_scan.jpg", "h02_identitas_kartu_mono.png",
            "h03_surat_keterangan_dokter.pdf", "h04_kwitansi_struk.jpg"]

    def swap(old, new):
        return [new if f == old else f for f in base]

    def fnd(code, *evidence):
        return {"code": code, "status": "perlu_ditinjau", "evidence": list(evidence)}

    def ev(file, field):
        return {"file": file, "page": 1, "field": field}

    scenarios = [
        {"id": "ha", "title": "Set lengkap (nama dengan gelar, tata letak berbeda)", "upload": base, "expected_findings": []},
        {"id": "hb", "title": "Nama kwitansi disingkat", "upload": swap(base[3], "h05_kwitansi_nama_singkat.jpg"),
         "expected_findings": [fnd("NAME_MISMATCH", ev("h05_kwitansi_nama_singkat.jpg", "payer_name"),
                                   ev("h02_identitas_kartu_mono.png", "full_name"))],
         "note": "Diharapkan severity rendah (kemungkinan singkatan)."},
        {"id": "hc", "title": "Nomor polis formulir berbeda", "upload": swap(base[0], "h06_formulir_mono_polis_beda.pdf"),
         "expected_findings": [fnd("POLICY_MISMATCH", ev("h06_formulir_mono_polis_beda.pdf", "policy_number"),
                                   {"source": "claim_record", "field": "policy_number"})]},
        {"id": "hd", "title": "Tanggal kwitansi di masa depan", "upload": swap(base[3], "h07_kwitansi_tanggal_depan.png"),
         "expected_findings": [fnd("DATE_INCONSISTENCY", ev("h07_kwitansi_tanggal_depan.png", "receipt_date"))]},
        {"id": "he", "title": "Identitas noise berat", "upload": swap(base[1], "h08_identitas_noise_berat.jpg"),
         "expected_findings": [fnd("LOW_QUALITY_DOCUMENT", ev("h08_identitas_noise_berat.jpg", None)),
                               fnd("NEEDS_MANUAL_VERIFICATION", ev("h08_identitas_noise_berat.jpg", "full_name"))],
         "note": "Tahap 5.5: field tidak terbaca pada dokumen kualitas rendah = NEEDS_MANUAL_VERIFICATION (sebelumnya MISSING_FIELD)."},
        {"id": "hf", "title": "Nama pasien di surat dokter berbeda", "upload": swap(base[2], "h09_surat_dokter_nama_beda_scan.jpg"),
         "expected_findings": [fnd("NAME_MISMATCH", ev("h09_surat_dokter_nama_beda_scan.jpg", "patient_name"),
                                   ev("h02_identitas_kartu_mono.png", "full_name"))]},
        {"id": "hg", "title": "Hanya formulir dan surat dokter", "upload": [base[0], base[2]],
         "expected_findings": [fnd("MISSING_DOCUMENT", {"source": "checklist", "field": "document_type"})]},
    ]
    return {
        "_disclaimer": "SELURUH DATA SINTETIS/FIKTIF. Set HOLDOUT: tidak dipakai menyetel aturan. Temuan bersifat 'perlu ditinjau', bukan keputusan klaim.",
        "_generator": "scripts/gen_holdout_data.py",
        "claim": CLAIM,
        "evaluation_date": EVAL_DATE,
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
    print("sample-data/holdout/ dibuat", file=sys.stderr)


def build_files(p):
    formulir_scan(p("h01_formulir_klaim_tabel_scan.jpg"), NAME_WITH_TITLE, CLAIM["policy_number"], seed=101, angle=2.2)
    identitas_scan(p("h02_identitas_kartu_mono.png"), seed=102, angle=-3.0, noise=8)
    surat_medis_pdf(p("h03_surat_keterangan_dokter.pdf"), PERSON["full_name"])
    kuitansi_scan(p("h04_kwitansi_struk.jpg"), PERSON["full_name"], RECEIPT_DATE, seed=104, angle=1.5)
    kuitansi_scan(p("h05_kwitansi_nama_singkat.jpg"), NAME_ABBREV, RECEIPT_DATE, seed=105, angle=-2.0)
    formulir_pdf_mono(p("h06_formulir_mono_polis_beda.pdf"), PERSON["full_name"], POLICY_OTHER)
    kuitansi_scan(p("h07_kwitansi_tanggal_depan.png"), PERSON["full_name"], FUTURE_RECEIPT_DATE, seed=107, angle=3.5, fmt="PNG")
    identitas_scan(p("h08_identitas_noise_berat.jpg"), seed=108, angle=4.0, noise=55, blur=2.5, contrast=0.45, fmt="JPEG")
    surat_medis_scan(p("h09_surat_dokter_nama_beda_scan.jpg"), NAME_OTHER, seed=109, angle=-1.8)


if __name__ == "__main__":
    main()
