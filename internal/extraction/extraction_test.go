package extraction

import (
	"strings"
	"testing"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/ocr"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/pii"
)

// page membuat halaman sintetis; setiap kata diberi confidence conf.
func page(engine string, conf float64, lines ...string) ocr.Page {
	p := ocr.Page{Number: 1, Engine: engine, EngineVersion: "test"}
	for _, l := range lines {
		var ln ocr.Line
		for _, w := range strings.Fields(l) {
			ln.Words = append(ln.Words, ocr.Word{Text: w, Conf: conf})
		}
		p.Lines = append(p.Lines, ln)
	}
	var sum float64
	for _, l := range p.Lines {
		for _, w := range l.Words {
			sum += w.Conf
			p.WordCount++
		}
	}
	if p.WordCount > 0 {
		m := sum / float64(p.WordCount)
		p.MeanConfidence = &m
	}
	return p
}

func byKey(fs []Field) map[Key]Field {
	m := map[Key]Field{}
	for _, f := range fs {
		m[f.Key] = f
	}
	return m
}

// Data sintetis (NIK berawalan 0000).
var formLines = []string{
	"CONTOH - DATA SINTETIS",
	"FORMULIR PENGAJUAN KLAIM KESEHATAN",
	"Nama peserta : Rani Kusuma Dewi",
	"NIK : 0000010101900001",
	"Nomor polis : POL-DEMO-0101",
	"Telepon : 0800-0000-1234",
	"Tanggal kejadian / masuk : 10-10-2026",
	"Total biaya diajukan : Rp 2.750.000",
	"Kota Demo, 15-10-2026 Tanda tangan: [CONTOH]",
}

func TestExtractClaimFormTextLayer(t *testing.T) {
	got := byKey(Extract("formulir_klaim", []ocr.Page{page(ocr.EnginePDFText, 100, formLines...)}))
	want := map[Key]string{
		KeyClaimantName: "Rani Kusuma Dewi", KeyPolicyNumber: "POL-DEMO-0101", KeyNIK: "0000********0001",
		KeyEventDate: "2026-10-10", KeyDocumentDate: "2026-10-15", KeyAmount: "2750000",
	}
	for k, v := range want {
		f := got[k]
		if f.Value != v || f.Status != StatusFound || f.Page != 1 || f.Excerpt == "" {
			t.Errorf("%s = %+v, ingin %q found dengan halaman & kutipan", k, f, v)
		}
	}
	// Tanggal tanda tangan tanpa label: maksimal "perlu verifikasi".
	if l := ConfidenceLabel(got[KeyDocumentDate].Confidence); l != "perlu verifikasi" {
		t.Errorf("document_date tanpa label harus perlu verifikasi, dapat %q", l)
	}
	if l := ConfidenceLabel(got[KeyClaimantName].Confidence); l != "tinggi" {
		t.Errorf("nama dari lapisan teks harus tinggi, dapat %q", l)
	}
	for _, f := range got {
		if pii.ContainsUnmaskedNIK(f.Value) || pii.ContainsUnmaskedNIK(f.Excerpt) {
			t.Fatalf("NIK utuh bocor di field %s", f.Key)
		}
	}
	if !strings.Contains(got[KeyNIK].Excerpt, "0000********0001") {
		t.Errorf("kutipan NIK harus tersamar, dapat %q", got[KeyNIK].Excerpt)
	}
}

// TC-05: confidence rendah → uncertain, nilai tetap ditampilkan untuk
// verifikasi tetapi tidak dianggap pasti.
func TestLowConfidenceIsUncertain(t *testing.T) {
	p := page(ocr.EngineTesseract, 90, formLines...)
	// Baris nama: kata nilai (setelah ":") terbaca dengan confidence rendah.
	for i := 3; i < len(p.Lines[2].Words); i++ {
		p.Lines[2].Words[i].Conf = 40
	}
	f := byKey(Extract("formulir_klaim", []ocr.Page{p}))[KeyClaimantName]
	if f.Status != StatusUncertain || ConfidenceLabel(f.Confidence) != "rendah" {
		t.Fatalf("nama dengan confidence rendah harus uncertain/rendah, dapat %+v", f)
	}
}

func TestMissingLabelIsNotFound(t *testing.T) {
	got := byKey(Extract("formulir_klaim", []ocr.Page{page(ocr.EnginePDFText, 100, "Nama peserta : Uji Fiktif")}))
	for _, k := range []Key{KeyPolicyNumber, KeyNIK, KeyEventDate, KeyAmount} {
		if got[k].Status != StatusNotFound || got[k].Value != "" {
			t.Errorf("%s harus not_found tanpa nilai, dapat %+v", k, got[k])
		}
	}
}

// Halaman tidak terbaca: semua field uncertain tanpa nilai (tidak menebak).
func TestUnreadablePageIsUncertainNotGuessed(t *testing.T) {
	got := Extract("identitas", []ocr.Page{page(ocr.EngineTesseract, 30, "~~")})
	if len(got) != 2 {
		t.Fatalf("identitas harus punya 2 field, dapat %d", len(got))
	}
	for _, f := range got {
		if f.Status != StatusUncertain || f.Value != "" {
			t.Errorf("%s harus uncertain tanpa nilai, dapat %+v", f.Key, f)
		}
	}
}

func TestInvalidValuesAreUncertain(t *testing.T) {
	got := byKey(Extract("formulir_klaim", []ocr.Page{page(ocr.EngineTesseract, 95,
		"Nama peserta : R4ni 123",
		"NIK : 00000101O1900001", // huruf O terbaca sebagai nol
		"Tanggal kejadian : 31-02-2026",
	)}))
	if f := got[KeyNIK]; f.Status != StatusUncertain || f.Value != "" || pii.ContainsUnmaskedNIK(f.Excerpt) {
		t.Errorf("NIK tidak valid harus uncertain tanpa nilai & kutipan tersamar, dapat %+v", f)
	}
	if strings.Contains(got[KeyNIK].Excerpt, "0101") && strings.Contains(got[KeyNIK].Excerpt, "1900") {
		t.Errorf("kutipan NIK sebagian tidak boleh memuat digit tengah: %q", got[KeyNIK].Excerpt)
	}
	if f := got[KeyEventDate]; f.Status != StatusUncertain {
		t.Errorf("tanggal tidak ada di kalender harus uncertain, dapat %+v", f)
	}
	if f := got[KeyClaimantName]; f.Status != StatusUncertain {
		t.Errorf("nama berisi angka harus uncertain, dapat %+v", f)
	}
}

// Diagnosis dan detail medis tidak pernah diekstrak ke field.
func TestMedicalLetterExcludesDiagnosis(t *testing.T) {
	got := Extract("surat_keterangan_medis", []ocr.Page{page(ocr.EnginePDFText, 100,
		"Nama pasien : Rani Kusuma Dewi",
		"Tanggal masuk : 10-10-2026",
		"Diagnosis : Demam tanpa komplikasi (diagnosis contoh generik)",
	)})
	keys := map[Key]bool{}
	for _, f := range got {
		keys[f.Key] = true
		if strings.Contains(strings.ToLower(f.Value+f.Excerpt), "demam") {
			t.Fatalf("detail medis bocor ke field %s", f.Key)
		}
	}
	if len(keys) != 3 || !keys[KeyClaimantName] || !keys[KeyEventDate] || !keys[KeyDocumentDate] {
		t.Fatalf("field surat medis tidak sesuai: %v", keys)
	}
}

func TestReceiptAndIdentity(t *testing.T) {
	kw := byKey(Extract("kuitansi", []ocr.Page{page(ocr.EnginePDFText, 100,
		"Tanggal : 12-10-2026", "Diterima dari : Rani Kusuma Dewi",
		"Obat-obatan : Rp 500.000", "TOTAL : Rp 2.750.000",
	)}))
	if kw[KeyAmount].Value != "2750000" || kw[KeyDocumentDate].Value != "2026-10-12" || kw[KeyClaimantName].Value != "Rani Kusuma Dewi" {
		t.Fatalf("kuitansi salah: %+v", kw)
	}
	id := byKey(Extract("identitas", []ocr.Page{page(ocr.EngineTesseract, 92,
		"KARTU IDENTITAS CONTOH", "NIK : 0000 0101 0190 0001", "Nama : Rani Kusuma Dewi",
		"Tempat/Tgl Lahir : Kota Demo, 01-01-1990",
	)}))
	if id[KeyNIK].Value != "0000********0001" || id[KeyClaimantName].Value != "Rani Kusuma Dewi" {
		t.Fatalf("identitas salah: %+v", id)
	}
	if _, ok := id[KeyDocumentDate]; ok {
		t.Fatal("tanggal lahir tidak boleh diekstrak sebagai tanggal dokumen")
	}
}

func TestConfidenceLabel(t *testing.T) {
	v := func(f float64) *float64 { return &f }
	cases := map[*float64]string{nil: "", v(100): "tinggi", v(85): "tinggi", v(84.9): "perlu verifikasi", v(60): "perlu verifikasi", v(59.9): "rendah"}
	for in, want := range cases {
		if got := ConfidenceLabel(in); got != want {
			t.Errorf("ConfidenceLabel(%v) = %q, ingin %q", in, got, want)
		}
	}
}

func TestParseDateWords(t *testing.T) {
	if d, ok := parseDate("10 Oktober 2026"); !ok || d != "2026-10-10" {
		t.Fatalf("tanggal dengan nama bulan gagal: %q %v", d, ok)
	}
}
