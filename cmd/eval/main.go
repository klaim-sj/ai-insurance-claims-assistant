// Command eval mengukur OCR + ekstraksi deterministik pada dokumen
// sintetis di sample-data/ terhadap expected.json, lalu menulis laporan ke
// docs/eval/ (report.md dan report.json).
//
// Dua mode dijalankan:
//   - "standar": alur produksi (lapisan teks PDF bila ada, Tesseract bila tidak).
//   - "scan": setiap halaman PDF dirender dan dibaca Tesseract untuk
//     menyimulasikan dokumen hasil scan.
//
// NIK hanya dibandingkan dan ditampilkan dalam bentuk tersamar.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/extraction"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/ocr"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/pii"
)

type expectedFile struct {
	DocumentType string                     `json:"document_type"`
	MIMEType     string                     `json:"mime_type"`
	Fields       map[string]json.RawMessage `json:"fields"`
}

type expectedDoc struct {
	Files     map[string]expectedFile `json:"files"`
	Scenarios []struct {
		ExpectedFindings []struct {
			Code     string `json:"code"`
			Evidence []struct {
				File string `json:"file"`
			} `json:"evidence"`
		} `json:"expected_findings"`
	} `json:"scenarios"`
}

// groundTruthKey memetakan field expected.json ke field ekstraksi per
// jenis dokumen. Field tanpa pemetaan tidak punya ground truth.
var groundTruthKey = map[string]map[extraction.Key]string{
	"formulir_klaim": {
		extraction.KeyClaimantName: "claimant_name", extraction.KeyPolicyNumber: "policy_number",
		extraction.KeyNIK: "nik", extraction.KeyEventDate: "incident_date", extraction.KeyAmount: "total_amount",
	},
	"identitas": {extraction.KeyClaimantName: "full_name", extraction.KeyNIK: "nik"},
	"surat_keterangan_medis": {
		extraction.KeyClaimantName: "patient_name", extraction.KeyEventDate: "admission_date",
	},
	"kuitansi": {
		extraction.KeyClaimantName: "payer_name", extraction.KeyDocumentDate: "receipt_date",
		extraction.KeyAmount: "total_amount",
	},
}

// Hasil per field.
const (
	outCorrect   = "benar"
	outWrong     = "salah"
	outUncertain = "uncertain"
	outNotFound  = "not_found"
	outAbstained = "tidak_menebak" // dokumen tidak terbaca dan sistem tidak menebak
	outGuessed   = "menebak"       // dokumen tidak terbaca tetapi field berstatus found
)

type fieldResult struct {
	Field      string   `json:"field"`
	Expected   string   `json:"expected"`
	Got        string   `json:"got,omitempty"`
	Status     string   `json:"status"`
	Confidence *float64 `json:"confidence,omitempty"`
	Outcome    string   `json:"outcome"`
}

type fileResult struct {
	File       string        `json:"file"`
	Type       string        `json:"document_type"`
	Unreadable bool          `json:"expected_unreadable"`
	Engines    []string      `json:"engines"`
	MeanConf   []float64     `json:"page_mean_confidence"`
	DurationMS int64         `json:"duration_ms"`
	Error      string        `json:"error,omitempty"`
	Fields     []fieldResult `json:"fields"`
}

type fieldSummary struct {
	Field     string  `json:"field"`
	Total     int     `json:"total"`
	Correct   int     `json:"correct"`
	Wrong     int     `json:"wrong"`
	Uncertain int     `json:"uncertain"`
	NotFound  int     `json:"not_found"`
	Accuracy  float64 `json:"accuracy"`
}

type modeReport struct {
	Mode            string         `json:"mode"`
	Files           []fileResult   `json:"files"`
	Summary         []fieldSummary `json:"summary"`
	OverallAccuracy float64        `json:"overall_accuracy"`
	TotalFields     int            `json:"total_fields"`
	UncertainCount  int            `json:"uncertain_count"`
	NotFoundCount   int            `json:"not_found_count"`
	WrongCount      int            `json:"wrong_count"`
	UnreadableOK    int            `json:"unreadable_abstained"`
	UnreadableBad   int            `json:"unreadable_guessed"`
	TotalMS         int64          `json:"total_ms"`
}

type report struct {
	GeneratedAt      string       `json:"generated_at"`
	ExtractorVersion string       `json:"extractor_version"`
	Engines          [2]string    `json:"engine_versions"`
	Thresholds       [2]float64   `json:"thresholds_high_min"`
	Modes            []modeReport `json:"modes"`
}

func main() {
	samples := flag.String("samples", "sample-data", "direktori dokumen sintetis")
	out := flag.String("out", "docs/eval", "direktori laporan")
	timeout := flag.Duration("timeout", 60*time.Second, "batas waktu per pemanggilan binary OCR")
	flag.Parse()
	if err := run(*samples, *out, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, "eval gagal:", err)
		os.Exit(1)
	}
}

func run(samples, outDir string, timeout time.Duration) error {
	raw, err := os.ReadFile(filepath.Join(samples, "expected.json"))
	if err != nil {
		return err
	}
	var exp expectedDoc
	if err := json.Unmarshal(raw, &exp); err != nil {
		return fmt.Errorf("expected.json: %w", err)
	}
	unreadable := map[string]bool{}
	for _, sc := range exp.Scenarios {
		for _, f := range sc.ExpectedFindings {
			if f.Code == "DOCUMENT_UNREADABLE" {
				for _, ev := range f.Evidence {
					unreadable[ev.File] = true
				}
			}
		}
	}

	ctx := context.Background()
	std := ocr.NewExec(ocr.ExecConfig{Timeout: timeout})
	if err := std.Check(ctx); err != nil {
		return err
	}
	scan := ocr.NewExec(ocr.ExecConfig{Timeout: timeout, ForceOCR: true})

	names := make([]string, 0, len(exp.Files))
	for n := range exp.Files {
		names = append(names, n)
	}
	sort.Strings(names)

	rep := report{
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339),
		ExtractorVersion: extraction.Version,
		Engines: [2]string{
			"pdftotext " + std.Version(ctx, ocr.EnginePDFText),
			"tesseract " + std.Version(ctx, ocr.EngineTesseract),
		},
		Thresholds: [2]float64{extraction.HighConfidence, extraction.MinConfidence},
	}
	for _, m := range []struct {
		name string
		eng  *ocr.Exec
	}{{"standar", std}, {"scan", scan}} {
		mr := modeReport{Mode: m.name}
		for _, n := range names {
			fr, err := evalFile(ctx, m.eng, filepath.Join(samples, n), n, exp.Files[n], unreadable[n])
			if err != nil {
				return err
			}
			mr.Files = append(mr.Files, fr)
			mr.TotalMS += fr.DurationMS
		}
		summarize(&mr)
		rep.Modes = append(rep.Modes, mr)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	js, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	md := markdown(rep)
	// Pertahanan berlapis: laporan tidak boleh memuat NIK utuh.
	if pii.ContainsUnmaskedNIK(string(js)) || pii.ContainsUnmaskedNIK(md) {
		return errors.New("laporan memuat pola NIK utuh; dibatalkan")
	}
	if err := os.WriteFile(filepath.Join(outDir, "report.json"), append(js, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "report.md"), []byte(md), 0o644); err != nil {
		return err
	}
	for _, m := range rep.Modes {
		fmt.Printf("[%s] akurasi %.1f%% (%d field), uncertain %d, not_found %d, salah %d, waktu %d ms\n",
			m.Mode, m.OverallAccuracy*100, m.TotalFields, m.UncertainCount, m.NotFoundCount, m.WrongCount, m.TotalMS)
	}
	fmt.Printf("laporan: %s\n", filepath.Join(outDir, "report.md"))
	return nil
}

func evalFile(ctx context.Context, eng *ocr.Exec, path, name string, ef expectedFile, unreadable bool) (fileResult, error) {
	fr := fileResult{File: name, Type: ef.DocumentType, Unreadable: unreadable}
	data, err := os.ReadFile(path)
	if err != nil {
		return fr, err
	}
	start := time.Now()
	res, err := eng.Process(ctx, data, ef.MIMEType)
	var fields []extraction.Field
	if err == nil {
		fields = extraction.Extract(ef.DocumentType, res.Pages)
	}
	fr.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		// Kode error saja; pesan tidak memuat isi dokumen.
		fr.Error = string(ocr.CodeOf(err))
		if fr.Error == "" {
			fr.Error = "ERROR"
		}
	}
	for _, p := range res.Pages {
		fr.Engines = append(fr.Engines, p.Engine)
		c := 0.0
		if p.MeanConfidence != nil {
			c = *p.MeanConfidence
		}
		fr.MeanConf = append(fr.MeanConf, roundTo(c, 2))
	}
	byKey := map[extraction.Key]extraction.Field{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	mapping := groundTruthKey[ef.DocumentType]
	for _, k := range extraction.AllKeys {
		src, ok := mapping[k]
		if !ok {
			continue
		}
		rawVal, ok := ef.Fields[src]
		if !ok {
			continue
		}
		want := normalizeExpected(k, rawVal)
		f, ok := byKey[k]
		if !ok {
			f = extraction.Field{Key: k, Status: extraction.StatusNotFound}
		}
		r := fieldResult{Field: string(k), Expected: want, Got: f.Value, Status: string(f.Status), Confidence: f.Confidence}
		switch {
		case unreadable && f.Status == extraction.StatusFound:
			r.Outcome = outGuessed
		case unreadable:
			r.Outcome = outAbstained
		case f.Status == extraction.StatusFound && f.Value == want:
			r.Outcome = outCorrect
		case f.Status == extraction.StatusFound:
			r.Outcome = outWrong
		case f.Status == extraction.StatusUncertain:
			r.Outcome = outUncertain
		default:
			r.Outcome = outNotFound
		}
		fr.Fields = append(fr.Fields, r)
	}
	return fr, nil
}

func normalizeExpected(k extraction.Key, raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		var n json.Number
		if err := json.Unmarshal(raw, &n); err == nil {
			s = n.String()
		}
	}
	if k == extraction.KeyNIK {
		return pii.MaskedNIK(pii.DigitsOnly(s))
	}
	if k == extraction.KeyAmount {
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return strconv.FormatInt(int64(n), 10)
		}
	}
	return s
}

func summarize(mr *modeReport) {
	sums := map[string]*fieldSummary{}
	correct := 0
	for _, f := range mr.Files {
		for _, r := range f.Fields {
			switch r.Outcome {
			case outAbstained:
				mr.UnreadableOK++
				continue
			case outGuessed:
				mr.UnreadableBad++
				continue
			}
			s := sums[r.Field]
			if s == nil {
				s = &fieldSummary{Field: r.Field}
				sums[r.Field] = s
			}
			s.Total++
			mr.TotalFields++
			switch r.Outcome {
			case outCorrect:
				s.Correct++
				correct++
			case outWrong:
				s.Wrong++
				mr.WrongCount++
			case outUncertain:
				s.Uncertain++
				mr.UncertainCount++
			case outNotFound:
				s.NotFound++
				mr.NotFoundCount++
			}
		}
	}
	for _, k := range extraction.AllKeys {
		if s, ok := sums[string(k)]; ok {
			s.Accuracy = roundTo(float64(s.Correct)/float64(s.Total), 4)
			mr.Summary = append(mr.Summary, *s)
		}
	}
	if mr.TotalFields > 0 {
		mr.OverallAccuracy = roundTo(float64(correct)/float64(mr.TotalFields), 4)
	}
}

func roundTo(v float64, d int) float64 {
	p := 1.0
	for range d {
		p *= 10
	}
	return float64(int64(v*p+0.5)) / p
}

func markdown(rep report) string {
	var b strings.Builder
	b.WriteString("# Evaluasi OCR & ekstraksi field (Tahap 4)\n\n")
	b.WriteString("> Dataset: `sample-data/` (SELURUHNYA SINTETIS). Hasil ekstraksi adalah bahan\n")
	b.WriteString("> verifikasi petugas, bukan keputusan klaim. NIK hanya ditampilkan tersamar.\n\n")
	fmt.Fprintf(&b, "- Dibuat: %s\n", rep.GeneratedAt)
	fmt.Fprintf(&b, "- Extractor: `%s`; engine: %s, %s\n", rep.ExtractorVersion, rep.Engines[0], rep.Engines[1])
	fmt.Fprintf(&b, "- Ambang confidence: tinggi ≥ %.0f, perlu verifikasi ≥ %.0f, di bawahnya rendah (uncertain)\n", rep.Thresholds[0], rep.Thresholds[1])
	b.WriteString("- Akurasi = field `found` dengan nilai tepat ÷ field yang punya ground truth. `uncertain`/`not_found` dihitung tidak tepat (tidak menebak, tetapi perlu diperiksa manual).\n")
	b.WriteString("- Dokumen yang memang dibuat tidak terbaca (skenario d) tidak masuk akurasi; dihitung terpisah: benar bila sistem tidak menebak.\n\n")
	b.WriteString("Mode **standar** = alur aplikasi (lapisan teks PDF, Tesseract untuk gambar/halaman tanpa teks). Mode **scan** = setiap halaman PDF dirender 300 dpi lalu dibaca Tesseract (simulasi dokumen hasil scan).\n\n")

	for _, m := range rep.Modes {
		fmt.Fprintf(&b, "## Mode %s\n\n", m.Mode)
		fmt.Fprintf(&b, "Akurasi keseluruhan **%.1f%%** (%d field) · uncertain %d · not_found %d · salah baca %d · dokumen tidak terbaca: tidak menebak %d / menebak %d · total waktu %d ms\n\n",
			m.OverallAccuracy*100, m.TotalFields, m.UncertainCount, m.NotFoundCount, m.WrongCount, m.UnreadableOK, m.UnreadableBad, m.TotalMS)
		b.WriteString("| Field | Total | Benar | Salah | Uncertain | Not found | Akurasi |\n|---|---:|---:|---:|---:|---:|---:|\n")
		for _, s := range m.Summary {
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %.1f%% |\n", s.Field, s.Total, s.Correct, s.Wrong, s.Uncertain, s.NotFound, s.Accuracy*100)
		}
		b.WriteString("\n| File | Engine | Conf. halaman | Waktu (ms) | Hasil |\n|---|---|---|---:|---|\n")
		for _, f := range m.Files {
			var outs []string
			for _, r := range f.Fields {
				o := r.Field + "=" + r.Outcome
				if r.Outcome == outWrong {
					o += fmt.Sprintf(" (dapat %q, harap %q)", r.Got, r.Expected)
				}
				outs = append(outs, o)
			}
			if f.Error != "" {
				outs = append([]string{"ERROR " + f.Error}, outs...)
			}
			confs := make([]string, len(f.MeanConf))
			for i, c := range f.MeanConf {
				confs[i] = fmt.Sprintf("%.1f", c)
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %d | %s |\n", f.File, strings.Join(f.Engines, ", "),
				strings.Join(confs, ", "), f.DurationMS, strings.Join(outs, "; "))
		}
		b.WriteString("\n")
	}
	b.WriteString("## Catatan\n\n")
	b.WriteString("- **Batas validitas**: dataset hanya 8 file sintetis dari satu generator dengan tata letak rapi, dan aturan ekstraksi (label, PSM Tesseract, ambang) disetel memakai file yang sama. Akurasi tinggi di sini **tidak** mewakili dokumen nyata (tulisan tangan, foto miring, tata letak beragam). Perlu dataset uji terpisah yang lebih representatif.\n")
	b.WriteString("- `document_date` formulir dan surat keterangan tidak punya ground truth di `expected.json`, sehingga tidak dihitung.\n")
	b.WriteString("- Ambang confidence adalah nilai awal prototipe; perlu diuji ulang pada dataset yang lebih representatif.\n")
	b.WriteString("- Laporan dibuat ulang dengan `make eval`.\n")
	return b.String()
}
