package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/extraction"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/ocr"
)

// Perbandingan konfigurasi OCR untuk formulir klaim berbentuk gambar
// (tabel/kolom): beberapa PSM Tesseract, dengan dan tanpa pra-pemrosesan.
// Hanya untuk laporan; konfigurasi produksi tidak dipilih dari set holdout.

type psmRow struct {
	Config   string  `json:"config"`
	Fields   int     `json:"fields"`
	Correct  int     `json:"correct"`
	Accuracy float64 `json:"accuracy"`
	TotalMS  int64   `json:"total_ms"`
	Files    int     `json:"files"`
}

var psmConfigs = []struct {
	name, psm string
	pre       bool
}{
	{"tanpa pra-pemrosesan, PSM 6 (Tahap 5)", "6", false},
	{"pra-pemrosesan, PSM 3", "3", true},
	{"pra-pemrosesan, PSM 4", "4", true},
	{"pra-pemrosesan, PSM 6", "6", true},
	{"pra-pemrosesan, PSM 11", "11", true},
	{"pra-pemrosesan, PSM 6+11 (produksi)", ocr.DefaultPSM, true},
}

func comparePSM(ctx context.Context, samples string, exp expectedDoc, names []string, timeout time.Duration) ([]psmRow, error) {
	var files []string
	for _, n := range names {
		f := exp.Files[n]
		if f.DocumentType == "formulir_klaim" && f.MIMEType != "application/pdf" && !f.ExpectedUnreadable {
			files = append(files, n)
		}
	}
	if len(files) == 0 {
		return nil, nil
	}
	var rows []psmRow
	for _, c := range psmConfigs {
		p := ocr.DefaultPreprocess()
		p.Disabled = !c.pre
		eng := ocr.NewExec(ocr.ExecConfig{Timeout: timeout, PSM: c.psm, Preprocess: &p})
		row := psmRow{Config: c.name, Files: len(files)}
		for _, n := range files {
			data, err := os.ReadFile(filepath.Join(samples, n))
			if err != nil {
				return nil, err
			}
			start := time.Now()
			res, err := eng.Process(ctx, data, exp.Files[n].MIMEType)
			row.TotalMS += time.Since(start).Milliseconds()
			got := map[extraction.Key]extraction.Field{}
			if err == nil {
				for _, f := range extraction.Extract("formulir_klaim", res.Pages) {
					got[f.Key] = f
				}
			}
			for k, src := range groundTruthKey["formulir_klaim"] {
				raw, ok := exp.Files[n].Fields[src]
				if !ok {
					continue
				}
				row.Fields++
				if f := got[k]; f.Status == extraction.StatusFound && f.Value == normalizeExpected(k, raw) {
					row.Correct++
				}
			}
		}
		if row.Fields > 0 {
			row.Accuracy = roundTo(float64(row.Correct)/float64(row.Fields), 4)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func psmMarkdown(b *strings.Builder, rows []psmRow) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(b, "## Perbandingan konfigurasi OCR — formulir klaim berbentuk gambar (%d file)\n\n", rows[0].Files)
	b.WriteString("Akurasi field formulir (nama, polis, NIK, tanggal kejadian, nominal) per konfigurasi; hanya untuk laporan, konfigurasi produksi tidak dipilih dari set holdout.\n\n")
	b.WriteString("| Konfigurasi | Field benar | Akurasi | Waktu total (ms) |\n|---|---:|---:|---:|\n")
	for _, r := range rows {
		fmt.Fprintf(b, "| %s | %d/%d | %.1f%% | %d |\n", r.Config, r.Correct, r.Fields, r.Accuracy*100, r.TotalMS)
	}
	b.WriteString("\n")
}
