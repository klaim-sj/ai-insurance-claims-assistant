package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/pii"
)

// summarizeReports membaca report.json beberapa set dan menulis tabel
// ringkasan lintas set (docs/eval/summary.md).
func summarizeReports(dirs []string, out string) error {
	var b strings.Builder
	b.WriteString("# Ringkasan evaluasi lintas set\n\n")
	b.WriteString("> SELURUH DATA SINTETIS. Temuan adalah bahan peninjauan petugas, bukan keputusan klaim.\n\n")
	b.WriteString("Precision/recall dihitung per (skenario × finding_code). *Substantif* = tanpa `NEEDS_MANUAL_VERIFICATION`. Waktu = total OCR + ekstraksi seluruh file set pada mode tersebut.\n\n")
	b.WriteString("| Set | Mode | Akurasi field | Precision | Recall | Precision substantif | Recall substantif | NMV / temuan | Waktu total (ms) | Rata-rata/dokumen (ms) |\n")
	b.WriteString("|---|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, d := range dirs {
		raw, err := os.ReadFile(filepath.Join(d, "report.json"))
		if err != nil {
			return err
		}
		var rep report
		if err := json.Unmarshal(raw, &rep); err != nil {
			return fmt.Errorf("%s: %w", d, err)
		}
		for _, m := range rep.Modes {
			per := int64(0)
			if len(m.Files) > 0 {
				per = m.TotalMS / int64(len(m.Files))
			}
			f := m.Findings
			nmv := "—"
			if f.FindingInstances > 0 || f.NMVInstances > 0 {
				nmv = fmt.Sprintf("%d / %d", f.NMVInstances, f.FindingInstances)
			}
			fmt.Fprintf(&b, "| %s | %s | %.1f%% | %s | %s | %s | %s | %s | %d | %d |\n",
				rep.Dataset, m.Mode, m.OverallAccuracy*100, pctStr(f.Precision), pctStr(f.Recall),
				pctStr(f.SubstantivePrecision), pctStr(f.SubstantiveRecall), nmv, m.TotalMS, per)
		}
	}
	b.WriteString("\nLaporan rinci: `docs/eval/report.md` (utama), `docs/eval/holdout/report.md`, `docs/eval/holdout2/report.md`, baseline `docs/eval/holdout2-baseline/report.md` (kode Tahap 5, dijalankan sebelum perbaikan). Baseline dibuat sebelum metrik substantif/NMV ada, sehingga kolom itu kosong (n/a) untuknya.\n")
	if pii.ContainsUnmaskedNIK(b.String()) {
		return errors.New("ringkasan memuat pola NIK utuh")
	}
	return os.WriteFile(out, []byte(b.String()), 0o644)
}
