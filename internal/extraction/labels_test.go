package extraction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/ocr"
)

func TestDefaultLabelsValid(t *testing.T) {
	l := DefaultLabels()
	if l == nil || l.Version == "" {
		t.Fatal("labels.json bawaan harus valid")
	}
	for _, k := range AllKeys {
		if len(l.Fields[k]) == 0 {
			t.Errorf("field %s tanpa label", k)
		}
	}
}

func TestParseLabelsRejectsInvalid(t *testing.T) {
	full := `"claimant_name":["nama"],"policy_number":["polis"],"nik":["nik"],"event_date":["tanggal kejadian"],"document_date":["tanggal"],"amount":["total"]`
	cases := map[string]string{
		"json rusak":        `{`,
		"tanpa versi":       `{"fields":{` + full + `}}`,
		"field tak dikenal": `{"version":"x","fields":{` + full + `,"diagnosis":["diagnosis"]}}`,
		"label kosong":      `{"version":"x","fields":{` + strings.Replace(full, `["nama"]`, `["  "]`, 1) + `}}`,
		"label ganda":       `{"version":"x","fields":{` + strings.Replace(full, `["polis"]`, `["NAMA"]`, 1) + `}}`,
		"field tanpa label": `{"version":"x","fields":{` + strings.Replace(full, `"amount":["total"]`, `"amount":[]`, 1) + `}}`,
	}
	for name, raw := range cases {
		if _, err := ParseLabels([]byte(raw)); err == nil {
			t.Errorf("%s: harus ditolak", name)
		}
	}
	ok := `{"version":"uji","fields":{` + full + `}}`
	l, err := ParseLabels([]byte(ok))
	if err != nil {
		t.Fatal(err)
	}
	if l.Fields[KeyClaimantName][0] != "nama" {
		t.Fatal("label harus dinormalisasi")
	}
}

func line(words ...string) ocr.Line {
	l := ocr.Line{}
	for _, w := range words {
		l.Words = append(l.Words, ocr.Word{Text: w, Conf: 95})
	}
	return l
}

func lpage(lines ...ocr.Line) []ocr.Page {
	return []ocr.Page{{Number: 1, Engine: ocr.EngineTesseract, Lines: lines, WordCount: 40, MeanConfidence: fp(95)}}
}

func fp(v float64) *float64 { return &v }

func get(fs []Field, k Key) Field {
	for _, f := range fs {
		if f.Key == k {
			return f
		}
	}
	return Field{}
}

// Sinonim dari file konfigurasi (bukan hardcode) dipakai Extract, lalu
// dikembalikan ke bawaan.
func TestLabelsFromFileOverride(t *testing.T) {
	pg := lpage(line("Nama", "Penerima", "Manfaat", ":", "Nama", "Fiktif", "Uji"))
	if f := get(Extract("formulir_klaim", pg), KeyClaimantName); f.Status != StatusNotFound {
		t.Fatalf("label belum dikonfigurasi harus not_found, dapat %+v", f)
	}
	raw, _ := os.ReadFile("labels.json")
	custom := strings.Replace(string(raw), `"nama peserta",`, `"nama penerima manfaat", "nama peserta",`, 1)
	path := filepath.Join(t.TempDir(), "labels.json")
	if err := os.WriteFile(path, []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := LoadLabelsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	SetLabels(l)
	t.Cleanup(func() { SetLabels(DefaultLabels()) })
	if f := get(Extract("formulir_klaim", pg), KeyClaimantName); f.Status != StatusFound || f.Value != "Nama Fiktif Uji" {
		t.Fatalf("sinonim dari file tidak dipakai: %+v", f)
	}
}

func TestSeparatorsAndPrefixLabels(t *testing.T) {
	cases := []struct {
		name string
		line ocr.Line
		key  Key
		want string
	}{
		{"pemisah titik dua", line("Nama", "Peserta", ":", "Nama", "Fiktif"), KeyClaimantName, "Nama Fiktif"},
		{"pemisah garis tabel", line("|", "Nomor", "Polis", "|", "POL-UJI-0001", "|"), KeyPolicyNumber, "POL-UJI-0001"},
		{"label multikata tanpa pemisah", line("Nama", "Tertanggung", "“Nama", "Fiktif"), KeyClaimantName, "Nama Fiktif"},
		{"label satu kata + angka tanpa pemisah", line("Tanggal", "12-10-2026"), KeyDocumentDate, "2026-10-12"},
		{"label salah baca OCR (jarak edit 1)", line("Jumtah", "Dibayar.", "Rp1.850.000,-"), KeyAmount, "1850000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := get(Extract("formulir_klaim", lpage(tc.line)), tc.key)
			if tc.key == KeyDocumentDate {
				f = get(Extract("kuitansi", lpage(tc.line)), tc.key)
			}
			if tc.key == KeyAmount {
				f = get(Extract("kuitansi", lpage(tc.line)), tc.key)
			}
			if f.Status != StatusFound || f.Value != tc.want {
				t.Fatalf("dapat %+v, harap found %q", f, tc.want)
			}
		})
	}
	// Label satu kata diikuti kata huruf tidak dipakai tanpa pemisah:
	// "Tanggal Keluar 05/09/2026" bukan tanggal dokumen.
	f := get(Extract("kuitansi", lpage(line("Tanggal", "Keluar", "05/09/2026"))), KeyDocumentDate)
	if f.Status == StatusFound {
		t.Fatalf("Tanggal Keluar tidak boleh terbaca sebagai tanggal dokumen: %+v", f)
	}
	// Label pendek tidak dicocokkan secara toleran ("nlk" ≠ "nik").
	if labelMatch("nlk", "nik") || !labelMatch("jumtah dibayar", "jumlah dibayar") || labelMatch("jumtah dibayer", "jumlah dibayar") {
		t.Fatal("labelMatch tidak sesuai aturan toleransi")
	}
}
