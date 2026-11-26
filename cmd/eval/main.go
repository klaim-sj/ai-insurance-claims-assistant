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
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/validation"
)

type expectedFile struct {
	DocumentType string                     `json:"document_type"`
	MIMEType     string                     `json:"mime_type"`
	Fields       map[string]json.RawMessage `json:"fields"`
	// ExpectedUnreadable: dokumen sengaja dibuat tidak terbaca; field-nya
	// tidak masuk akurasi (benar bila sistem tidak menebak).
	ExpectedUnreadable bool `json:"expected_unreadable"`
}

type expectedEvidence struct {
	File   string `json:"file"`
	Source string `json:"source"`
	Field  string `json:"field"`
}

type expectedFinding struct {
	Code     string             `json:"code"`
	Evidence []expectedEvidence `json:"evidence"`
}

type scenario struct {
	ID               string            `json:"id"`
	Title            string            `json:"title"`
	Upload           []string          `json:"upload"`
	ExpectedFindings []expectedFinding `json:"expected_findings"`
	// Claim: data klaim khusus skenario; kosong berarti claim global.
	Claim *claimRecord `json:"claim"`
}

type claimRecord struct {
	PolicyNumber string `json:"policy_number"`
	ClaimantName string `json:"claimant_name"`
	ProductType  string `json:"product_type"`
	ReceivedAt   string `json:"received_at"`
}

type expectedDoc struct {
	Claim claimRecord `json:"claim"`
	// EvaluationDate: tanggal "hari ini" untuk aturan tanggal masa depan
	// (deterministik); default claim.received_at.
	EvaluationDate string                  `json:"evaluation_date"`
	Files          map[string]expectedFile `json:"files"`
	Scenarios      []scenario              `json:"scenarios"`
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
	Findings        findingsReport `json:"findings"`
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
	Note             string       `json:"note,omitempty"`
	PSMComparison    []psmRow     `json:"psm_comparison,omitempty"`
	Dataset          string       `json:"dataset"`
	Samples          string       `json:"samples"`
	Holdout          bool         `json:"holdout"`
	RuleVersions     []string     `json:"rule_versions"`
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
	dataset := flag.String("dataset", "utama", "nama dataset di laporan")
	holdout := flag.Bool("holdout", false, "dataset holdout (tidak dipakai menyetel aturan)")
	flag.StringVar(&ocrPSM, "psm", "", "daftar PSM Tesseract dipisah koma (kosong = default produksi)")
	flag.StringVar(&ocrBinarize, "binarize", "", "none|otsu|sauvola (kosong = default produksi)")
	flag.BoolVar(&ocrNoPre, "no-preprocess", false, "matikan pra-pemrosesan gambar")
	flag.StringVar(&reportNote, "note", "", "catatan status set (ditampilkan di laporan)")
	flag.BoolVar(&psmCompare, "psm-compare", false, "sertakan perbandingan PSM untuk formulir berbentuk gambar")
	summary := flag.String("summarize", "", "daftar direktori laporan dipisah koma; tulis ringkasan ke -out (file .md)")
	flag.Parse()
	if *summary != "" {
		if err := summarizeReports(strings.Split(*summary, ","), *out); err != nil {
			fmt.Fprintln(os.Stderr, "ringkasan gagal:", err)
			os.Exit(1)
		}
		fmt.Println("ringkasan:", *out)
		return
	}
	if err := run(*samples, *out, *dataset, *holdout, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, "eval gagal:", err)
		os.Exit(1)
	}
}

// Flag eksperimen OCR (default = konfigurasi produksi).
var (
	ocrPSM, ocrBinarize, reportNote string
	ocrNoPre, psmCompare            bool
)

func execConfig(timeout time.Duration, force bool) ocr.ExecConfig {
	cfg := ocr.ExecConfig{Timeout: timeout, ForceOCR: force, PSM: ocrPSM}
	if cfg.PSM == "" {
		cfg.PSM = ocr.DefaultPSM
	}
	p := ocr.DefaultPreprocess()
	p.Disabled = ocrNoPre
	if ocrBinarize != "" {
		p.Binarize = ocr.Binarize(ocrBinarize)
	}
	cfg.Preprocess = &p
	return cfg
}

func run(samples, outDir, dataset string, holdout bool, timeout time.Duration) error {
	raw, err := os.ReadFile(filepath.Join(samples, "expected.json"))
	if err != nil {
		return err
	}
	var exp expectedDoc
	if err := json.Unmarshal(raw, &exp); err != nil {
		return fmt.Errorf("expected.json: %w", err)
	}
	unreadable := map[string]bool{}
	for n, f := range exp.Files {
		unreadable[n] = f.ExpectedUnreadable
	}

	ctx := context.Background()
	std := ocr.NewExec(execConfig(timeout, false))
	if err := std.Check(ctx); err != nil {
		return err
	}
	scan := ocr.NewExec(execConfig(timeout, true))

	names := make([]string, 0, len(exp.Files))
	for n := range exp.Files {
		names = append(names, n)
	}
	sort.Strings(names)

	rules := validation.DefaultRules()
	rep := report{
		Note:    reportNote,
		Dataset: dataset, Samples: filepath.ToSlash(samples), Holdout: holdout,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339),
		ExtractorVersion: extraction.Version,
		Engines: [2]string{
			"pdftotext " + std.Version(ctx, ocr.EnginePDFText),
			"tesseract " + std.Version(ctx, ocr.EngineTesseract),
		},
		Thresholds: [2]float64{extraction.HighConfidence, extraction.MinConfidence},
	}
	for _, r := range rules {
		rep.RuleVersions = append(rep.RuleVersions, fmt.Sprintf("%s v%d", r.Code, r.Version))
	}
	for _, m := range []struct {
		name string
		eng  *ocr.Exec
	}{{"standar", std}, {"scan", scan}} {
		mr := modeReport{Mode: m.name}
		docs := map[string]validation.Document{}
		for i, n := range names {
			fr, vd, err := evalFile(ctx, m.eng, filepath.Join(samples, n), n, exp.Files[n], unreadable[n])
			if err != nil {
				return err
			}
			vd.ID = int64(i + 1)
			docs[n] = vd
			mr.Files = append(mr.Files, fr)
			mr.TotalMS += fr.DurationMS
		}
		summarize(&mr)
		if mr.Findings, err = evalFindings(exp, names, docs, rules); err != nil {
			return err
		}
		rep.Modes = append(rep.Modes, mr)
	}

	if psmCompare {
		if rep.PSMComparison, err = comparePSM(ctx, samples, exp, names, timeout); err != nil {
			return err
		}
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
		fmt.Printf("[%s/%s] akurasi field %.1f%% (%d field), uncertain %d, not_found %d, salah %d, waktu %d ms\n",
			dataset, m.Mode, m.OverallAccuracy*100, m.TotalFields, m.UncertainCount, m.NotFoundCount, m.WrongCount, m.TotalMS)
		fmt.Printf("[%s/%s] temuan: %s\n", dataset, m.Mode, m.Findings.oneLine())
	}
	fmt.Printf("laporan: %s\n", filepath.Join(outDir, "report.md"))
	return nil
}

func evalFile(ctx context.Context, eng *ocr.Exec, path, name string, ef expectedFile, unreadable bool) (fileResult, validation.Document, error) {
	fr := fileResult{File: name, Type: ef.DocumentType, Unreadable: unreadable}
	vd := validation.Document{Type: ef.DocumentType, Status: "completed"}
	data, err := os.ReadFile(path)
	if err != nil {
		return fr, vd, err
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
		vd.Status = "failed"
	}
	vd.PageCount = len(res.Pages)
	for _, p := range res.Pages {
		vd.Pages = append(vd.Pages, validation.Page{Number: p.Number, Engine: p.Engine, MeanConfidence: p.MeanConfidence, WordCount: p.WordCount})
	}
	for _, f := range fields {
		// Penyamaran sama dengan worker sebelum field disimpan.
		vd.Fields = append(vd.Fields, validation.Field{
			Key: string(f.Key), Value: pii.MaskNIK(f.Value), Status: string(f.Status),
			Confidence: f.Confidence, Page: f.Page, Excerpt: pii.MaskNIK(f.Excerpt),
		})
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
		r := fieldResult{Field: string(k), Expected: want, Got: pii.MaskNIK(f.Value), Status: string(f.Status), Confidence: f.Confidence}
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
	return fr, vd, nil
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
	fmt.Fprintf(&b, "# Evaluasi OCR, ekstraksi field, dan temuan — dataset %s\n\n", rep.Dataset)
	fmt.Fprintf(&b, "> Dataset: `%s/` (SELURUHNYA SINTETIS). Hasil ekstraksi dan temuan adalah bahan\n", rep.Samples)
	b.WriteString("> verifikasi petugas (\"perlu ditinjau\"), bukan keputusan klaim. NIK hanya ditampilkan tersamar.\n\n")
	if rep.Note != "" {
		fmt.Fprintf(&b, "**Status set**: %s\n\n", rep.Note)
	} else if rep.Holdout {
		b.WriteString("**Set holdout**: dokumen di set ini dibuat dengan tata letak, font, kemiringan, dan noise berbeda dan **tidak dipakai untuk menyetel** aturan ekstraksi maupun aturan validasi. Aturan dibekukan sebelum set ini dijalankan pertama kali; angka di bawah dilaporkan apa adanya.\n\n")
	}
	fmt.Fprintf(&b, "- Dibuat: %s\n", rep.GeneratedAt)
	fmt.Fprintf(&b, "- Extractor: `%s`; engine: %s, %s\n", rep.ExtractorVersion, rep.Engines[0], rep.Engines[1])
	fmt.Fprintf(&b, "- Aturan validasi: %s\n", strings.Join(rep.RuleVersions, ", "))
	fmt.Fprintf(&b, "- Ambang confidence: tinggi ≥ %.0f, perlu verifikasi ≥ %.0f, di bawahnya rendah (uncertain)\n", rep.Thresholds[0], rep.Thresholds[1])
	b.WriteString("- Akurasi field = field `found` dengan nilai tepat ÷ field yang punya ground truth. `uncertain`/`not_found` dihitung tidak tepat (tidak menebak, tetapi perlu diperiksa manual).\n")
	b.WriteString("- Dokumen yang sengaja dibuat tidak terbaca (`expected_unreadable`) tidak masuk akurasi; dihitung terpisah: benar bila sistem tidak menebak.\n")
	b.WriteString("- Temuan: rule engine dijalankan per skenario. Satuan hitung = (skenario, finding_code): TP bila kode diharapkan dan muncul, FP bila muncul tanpa diharapkan, FN bila diharapkan tetapi tidak muncul. Evidence lengkap = TP yang evidence-nya merujuk semua file sumber yang disebut `expected.json`.\n\n")
	b.WriteString("Mode **standar** = alur aplikasi (lapisan teks PDF, Tesseract untuk gambar/halaman tanpa teks). Mode **scan** = setiap halaman PDF dirender 300 dpi lalu dibaca Tesseract (simulasi dokumen hasil scan).\n\n")

	for _, m := range rep.Modes {
		fmt.Fprintf(&b, "## Mode %s\n\n", m.Mode)
		findingsMarkdown(&b, m.Findings)
		b.WriteString("### Ekstraksi field\n\n")
		perDoc := int64(0)
		if len(m.Files) > 0 {
			perDoc = m.TotalMS / int64(len(m.Files))
		}
		fmt.Fprintf(&b, "Akurasi keseluruhan **%.1f%%** (%d field) · uncertain %d · not_found %d · salah baca %d · dokumen tidak terbaca: tidak menebak %d / menebak %d · total waktu %d ms (rata-rata %d ms/dokumen)\n\n",
			m.OverallAccuracy*100, m.TotalFields, m.UncertainCount, m.NotFoundCount, m.WrongCount, m.UnreadableOK, m.UnreadableBad, m.TotalMS, perDoc)
		b.WriteString("| Field | Total | Benar | Salah | Uncertain | Not found | Akurasi |\n|---|---:|---:|---:|---:|---:|---:|\n")
		for _, s := range m.Summary {
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %.1f%% |\n", s.Field, s.Total, s.Correct, s.Wrong, s.Uncertain, s.NotFound, s.Accuracy*100)
		}
		b.WriteString("\n| File | Engine | Conf. halaman | Waktu (ms) | Hasil |\n|---|---|---|---:|---|\n")
		for _, f := range m.Files {
			var outs []string
			for _, r := range f.Fields {
				o := r.Field + "=" + r.Outcome
				if r.Outcome == outWrong && r.Field != string(extraction.KeyNIK) {
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
	psmMarkdown(&b, rep.PSMComparison)
	b.WriteString("## Catatan\n\n")
	if rep.Holdout {
		b.WriteString("- Set holdout kecil (puluhan field, beberapa skenario); satu dokumen dapat menggeser persentase secara besar. Gunakan sebagai sinyal arah, bukan ukuran kinerja produksi.\n")
		b.WriteString("- Kegagalan di sini sengaja **tidak** diperbaiki dengan menyetel aturan pada set yang sama; perbaikan harus divalidasi pada set uji baru.\n")
	} else {
		b.WriteString("- **Batas validitas**: dataset utama berasal dari satu generator dengan tata letak rapi, dan aturan ekstraksi/validasi disetel memakai file yang sama. Angka tinggi di sini **tidak** mewakili dokumen nyata. Lihat laporan holdout (`docs/eval/holdout/report.md`) untuk set yang tidak dipakai menyetel.\n")
		b.WriteString("- `document_date` formulir dan surat keterangan tidak punya ground truth di `expected.json`, sehingga tidak dihitung.\n")
	}
	b.WriteString("- Konfigurasi OCR: pra-pemrosesan Go (normalisasi resolusi, kontras, median 3×3, deskew, Otsu, hapus garis tabel) + Tesseract PSM " + ocr.DefaultPSM + " (hasil berskor tertinggi). Gating confidence: NAME/POLICY_MISMATCH hanya bila kedua nilai ≥ 85; selain itu `NEEDS_MANUAL_VERIFICATION`.\n")
	b.WriteString("- Ambang confidence adalah nilai awal prototipe; perlu diuji ulang pada dataset yang lebih representatif.\n")
	b.WriteString("- Laporan dibuat ulang dengan `make eval`.\n")
	return b.String()
}
