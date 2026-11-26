package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/validation"
)

// Evaluasi temuan: rule engine dijalankan per skenario atas hasil OCR +
// ekstraksi dokumen yang diunggah pada skenario itu, lalu kode temuan
// dibandingkan dengan expected_findings.
//
// Satuan hitung = (skenario, finding_code):
//   - TP: kode diharapkan dan muncul;
//   - FP: kode muncul tetapi tidak diharapkan;
//   - FN: kode diharapkan tetapi tidak muncul.
//
// Evidence coverage = TP yang evidence-nya merujuk SEMUA file sumber yang
// disebut expected.json untuk temuan itu.

type codeStats struct {
	Code      string   `json:"code"`
	TP        int      `json:"tp"`
	FP        int      `json:"fp"`
	FN        int      `json:"fn"`
	Precision *float64 `json:"precision"` // nil bila tidak ada prediksi
	Recall    *float64 `json:"recall"`    // nil bila tidak ada yang diharapkan
}

type scenarioResult struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Expected  []string `json:"expected"`
	Predicted []string `json:"predicted"`
	// Instances: jumlah temuan per kode (satu kode bisa muncul beberapa
	// kali, mis. MISSING_FIELD per field).
	Instances map[string]int `json:"instances"`
	FP        []string       `json:"false_positive"`
	FN        []string       `json:"false_negative"`
	// EvidenceMissing: TP yang evidence-nya tidak merujuk semua file sumber
	// yang diharapkan.
	EvidenceMissing []string `json:"evidence_incomplete,omitempty"`
	// Details: ringkasan temuan (kode, severity, sumber evidence) tanpa nilai.
	Details []string `json:"details"`
}

type findingsReport struct {
	Scenarios       []scenarioResult `json:"scenarios"`
	PerCode         []codeStats      `json:"per_code"`
	TP              int              `json:"tp"`
	FP              int              `json:"fp"`
	FN              int              `json:"fn"`
	Precision       *float64         `json:"precision"`
	Recall          *float64         `json:"recall"`
	TPWithEvidence  int              `json:"tp_evidence_complete"`
	InvalidFindings int              `json:"invalid_findings"`
	// Substantif: tanpa NEEDS_MANUAL_VERIFICATION (temuan yang menyatakan
	// kondisi, bukan permintaan verifikasi manual).
	SubstantivePrecision *float64 `json:"substantive_precision"`
	SubstantiveRecall    *float64 `json:"substantive_recall"`
	SubstantiveTP        int      `json:"substantive_tp"`
	SubstantiveFP        int      `json:"substantive_fp"`
	SubstantiveFN        int      `json:"substantive_fn"`
	// NMVInstances: jumlah temuan NEEDS_MANUAL_VERIFICATION (semua skenario).
	NMVInstances int `json:"nmv_instances"`
	// FindingInstances: jumlah seluruh temuan (semua skenario).
	FindingInstances int `json:"finding_instances"`
}

func ratio(a, b int) *float64 {
	if b == 0 {
		return nil
	}
	v := roundTo(float64(a)/float64(b), 4)
	return &v
}

func pctStr(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", *v*100)
}

func (r findingsReport) oneLine() string {
	return fmt.Sprintf("precision %s, recall %s (TP %d, FP %d, FN %d) · substantif (tanpa NMV) precision %s, recall %s · NMV %d dari %d temuan · evidence lengkap %d/%d",
		pctStr(r.Precision), pctStr(r.Recall), r.TP, r.FP, r.FN,
		pctStr(r.SubstantivePrecision), pctStr(r.SubstantiveRecall), r.NMVInstances, r.FindingInstances,
		r.TPWithEvidence, r.TP)
}

func evalFindings(exp expectedDoc, names []string, docs map[string]validation.Document, rules []validation.Rule) (findingsReport, error) {
	var rep findingsReport
	byID := map[int64]string{}
	for _, n := range names {
		byID[docs[n].ID] = n
	}
	stats := map[string]*codeStats{}
	stat := func(c string) *codeStats {
		if stats[c] == nil {
			stats[c] = &codeStats{Code: c}
		}
		return stats[c]
	}
	for _, c := range validation.AllCodes {
		stat(c)
	}

	for _, sc := range exp.Scenarios {
		claim := exp.Claim
		if sc.Claim != nil {
			claim = *sc.Claim
		}
		today := exp.EvaluationDate
		if today == "" {
			today = claim.ReceivedAt
		}
		in := validation.Input{
			Claim: validation.ClaimRecord{
				PolicyNumber: claim.PolicyNumber, ClaimantName: claim.ClaimantName, ProductType: claim.ProductType,
			},
			Today: today,
		}
		for _, f := range sc.Upload {
			d, ok := docs[f]
			if !ok {
				return rep, fmt.Errorf("skenario %s: file %s tidak ada di files", sc.ID, f)
			}
			in.Documents = append(in.Documents, d)
		}
		found, err := validation.Evaluate(in, rules)
		if err != nil {
			// Evaluate menolak temuan tanpa evidence yang valid.
			rep.InvalidFindings++
			return rep, fmt.Errorf("skenario %s: %w", sc.ID, err)
		}

		res := scenarioResult{ID: sc.ID, Title: sc.Title, Instances: map[string]int{}}
		predicted := map[string][]validation.Finding{}
		for _, f := range found {
			predicted[f.Code] = append(predicted[f.Code], f)
			res.Instances[f.Code]++
			rep.FindingInstances++
			if f.Code == validation.CodeNeedsManualVerification {
				rep.NMVInstances++
			}
			var srcs []string
			for _, e := range f.Evidence {
				switch e.Source {
				case validation.SourceDocument:
					s := fmt.Sprintf("%s hal.%d", byID[e.DocumentID], e.Page)
					if e.Field != "" {
						s += " " + e.Field
					}
					srcs = append(srcs, s)
				default:
					srcs = append(srcs, e.Source+":"+e.Field)
				}
			}
			res.Details = append(res.Details, fmt.Sprintf("%s [%s] ← %s", f.Code, f.Severity, strings.Join(srcs, " | ")))
		}
		expected := map[string]expectedFinding{}
		for _, ef := range sc.ExpectedFindings {
			expected[ef.Code] = ef
			res.Expected = append(res.Expected, ef.Code)
		}
		for c := range predicted {
			res.Predicted = append(res.Predicted, c)
		}
		sort.Strings(res.Expected)
		sort.Strings(res.Predicted)

		for _, c := range res.Predicted {
			ef, ok := expected[c]
			if !ok {
				stat(c).FP++
				res.FP = append(res.FP, c)
				continue
			}
			stat(c).TP++
			if evidenceComplete(ef, predicted[c], byID) {
				rep.TPWithEvidence++
			} else {
				res.EvidenceMissing = append(res.EvidenceMissing, c)
			}
		}
		for _, c := range res.Expected {
			if _, ok := predicted[c]; !ok {
				stat(c).FN++
				res.FN = append(res.FN, c)
			}
		}
		rep.Scenarios = append(rep.Scenarios, res)
	}

	codes := make([]string, 0, len(stats))
	for c := range stats {
		codes = append(codes, c)
	}
	sort.SliceStable(codes, func(i, j int) bool {
		ii, jj := slices.Index(validation.AllCodes, codes[i]), slices.Index(validation.AllCodes, codes[j])
		if ii < 0 {
			ii = len(validation.AllCodes)
		}
		if jj < 0 {
			jj = len(validation.AllCodes)
		}
		if ii != jj {
			return ii < jj
		}
		return codes[i] < codes[j]
	})
	for _, c := range codes {
		s := stats[c]
		s.Precision = ratio(s.TP, s.TP+s.FP)
		s.Recall = ratio(s.TP, s.TP+s.FN)
		rep.TP += s.TP
		rep.FP += s.FP
		rep.FN += s.FN
		if c != validation.CodeNeedsManualVerification {
			rep.SubstantiveTP += s.TP
			rep.SubstantiveFP += s.FP
			rep.SubstantiveFN += s.FN
		}
		rep.PerCode = append(rep.PerCode, *s)
	}
	rep.Precision = ratio(rep.TP, rep.TP+rep.FP)
	rep.Recall = ratio(rep.TP, rep.TP+rep.FN)
	rep.SubstantivePrecision = ratio(rep.SubstantiveTP, rep.SubstantiveTP+rep.SubstantiveFP)
	rep.SubstantiveRecall = ratio(rep.SubstantiveTP, rep.SubstantiveTP+rep.SubstantiveFN)
	return rep, nil
}

// evidenceComplete: setiap file sumber pada expected evidence dirujuk oleh
// minimal satu temuan berkode sama.
func evidenceComplete(ef expectedFinding, got []validation.Finding, byID map[int64]string) bool {
	refs := map[string]bool{}
	sources := map[string]bool{}
	for _, f := range got {
		for _, e := range f.Evidence {
			if e.Source == validation.SourceDocument {
				refs[byID[e.DocumentID]] = true
			} else {
				sources[e.Source] = true
			}
		}
	}
	for _, e := range ef.Evidence {
		switch {
		case e.File != "" && !refs[e.File]:
			return false
		case e.File == "" && e.Source != "" && !sources[e.Source]:
			return false
		}
	}
	return true
}

func findingsMarkdown(b *strings.Builder, fr findingsReport) {
	fmt.Fprintf(b, "### Temuan (rule engine)\n\n- Semua kode: precision **%s** · recall **%s** · TP %d · FP %d · FN %d\n",
		pctStr(fr.Precision), pctStr(fr.Recall), fr.TP, fr.FP, fr.FN)
	fmt.Fprintf(b, "- Substantif (tanpa `NEEDS_MANUAL_VERIFICATION`): precision **%s** · recall **%s** · TP %d · FP %d · FN %d\n",
		pctStr(fr.SubstantivePrecision), pctStr(fr.SubstantiveRecall), fr.SubstantiveTP, fr.SubstantiveFP, fr.SubstantiveFN)
	fmt.Fprintf(b, "- `NEEDS_MANUAL_VERIFICATION`: %d dari %d temuan · evidence lengkap %d/%d TP · temuan ditolak validator evidence %d\n\n",
		fr.NMVInstances, fr.FindingInstances, fr.TPWithEvidence, fr.TP, fr.InvalidFindings)
	b.WriteString("| finding_code | TP | FP | FN | Precision | Recall |\n|---|---:|---:|---:|---:|---:|\n")
	for _, s := range fr.PerCode {
		fmt.Fprintf(b, "| %s | %d | %d | %d | %s | %s |\n", s.Code, s.TP, s.FP, s.FN, pctStr(s.Precision), pctStr(s.Recall))
	}
	b.WriteString("\n| Skenario | Diharapkan | Muncul (jumlah) | NMV | FP | FN |\n|---|---|---|---:|---|---|\n")
	for _, s := range fr.Scenarios {
		var got []string
		for _, c := range s.Predicted {
			got = append(got, fmt.Sprintf("%s (%d)", c, s.Instances[c]))
		}
		dash := func(xs []string) string {
			if len(xs) == 0 {
				return "—"
			}
			return strings.Join(xs, ", ")
		}
		fmt.Fprintf(b, "| %s · %s | %s | %s | %d | %s | %s |\n", s.ID, s.Title, dash(s.Expected), dash(got),
			s.Instances[validation.CodeNeedsManualVerification], dash(s.FP), dash(s.FN))
	}
	b.WriteString("\n<details><summary>Rincian temuan per skenario (kode, severity, sumber evidence; tanpa nilai)</summary>\n\n")
	for _, s := range fr.Scenarios {
		fmt.Fprintf(b, "- **%s**: ", s.ID)
		if len(s.Details) == 0 {
			b.WriteString("tidak ada temuan\n")
			continue
		}
		b.WriteString("\n")
		for _, d := range s.Details {
			fmt.Fprintf(b, "  - %s\n", d)
		}
		if len(s.EvidenceMissing) > 0 {
			fmt.Fprintf(b, "  - evidence tidak lengkap untuk: %s\n", strings.Join(s.EvidenceMissing, ", "))
		}
	}
	b.WriteString("\n</details>\n\n")
}
