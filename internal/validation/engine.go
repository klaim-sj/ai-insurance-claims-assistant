package validation

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/documents"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/extraction"
)

// ClaimRecord adalah data klaim yang diinput petugas.
type ClaimRecord struct {
	PolicyNumber string
	ClaimantName string
	ProductType  string
}

// Field adalah satu field hasil ekstraksi (nilai NIK sudah tersamar).
type Field struct {
	Key        string
	Value      string
	Status     string // found | uncertain | not_found
	Confidence *float64
	Page       int
	Excerpt    string
}

// Page adalah metadata kualitas satu halaman OCR (tanpa teks).
type Page struct {
	Number         int
	Engine         string
	MeanConfidence *float64
	WordCount      int
}

// Document adalah satu dokumen klaim beserta hasil pemrosesannya.
type Document struct {
	ID        int64
	Type      string
	Status    string // processing_status
	PageCount int
	Fields    []Field
	Pages     []Page
}

func (d Document) field(key string) (Field, bool) {
	for _, f := range d.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

func (d Document) found(key string) (Field, bool) {
	f, ok := d.field(key)
	return f, ok && f.Status == string(extraction.StatusFound) && f.Value != ""
}

// Input adalah seluruh data yang dibaca aturan.
type Input struct {
	Claim ClaimRecord
	// Documents berurutan dari yang paling lama diunggah.
	Documents []Document
	// Today: tanggal hari ini (WIB) dalam format YYYY-MM-DD.
	Today string
}

// ProductHealth: produk yang memakai checklist dokumen klaim kesehatan.
func isHealthProduct(p string) bool {
	return strings.Contains(strings.ToLower(p), "kesehatan")
}

// considered mengembalikan dokumen yang dinilai aturan: untuk setiap jenis
// dokumen wajib/terstruktur hanya dokumen TERBARU yang selesai diproses
// (unggahan ulang dianggap pengganti), ditambah seluruh dokumen "lainnya".
func considered(docs []Document) []Document {
	latest := map[string]int{}
	for i, d := range docs {
		if d.Status != "completed" {
			continue
		}
		latest[d.Type] = i
	}
	var out []Document
	for i, d := range docs {
		if d.Status != "completed" {
			continue
		}
		if d.Type == string(documents.TypeOther) || latest[d.Type] == i {
			out = append(out, d)
		}
	}
	return out
}

func latestOfType(docs []Document, types ...string) []Document {
	var out []Document
	for _, d := range docs {
		if slices.Contains(types, d.Type) {
			out = append(out, d)
		}
	}
	return out
}

func typeLabel(t string) string { return documents.Type(t).Label() }

func docEvidence(d Document, f Field, note string) Evidence {
	page := f.Page
	if page <= 0 {
		page = 1
	}
	return Evidence{
		Source: SourceDocument, DocumentID: d.ID, DocumentType: d.Type, Page: page,
		Field: f.Key, Value: f.Value, Excerpt: truncate(f.Excerpt, MaxExcerpt), Note: note,
	}
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// Evaluate menjalankan seluruh aturan aktif secara deterministik. Urutan
// keluaran stabil (urutan aturan, lalu urutan dokumen). Setiap temuan
// sudah lolos Finding.Validate; aturan yang menghasilkan temuan tidak valid
// mengembalikan error (tidak pernah disimpan diam-diam tanpa evidence).
func Evaluate(in Input, rules []Rule) ([]Finding, error) {
	docs := considered(in.Documents)
	var out []Finding
	for _, r := range rules {
		var (
			fs  []Finding
			err error
		)
		switch r.Code {
		case CodeMissingDocument:
			fs, err = missingDocument(in, r)
		case CodeMissingField:
			fs, err = missingField(docs, r)
		case CodeNameMismatch:
			fs, err = nameMismatch(in.Claim, docs, r)
		case CodePolicyMismatch:
			fs, err = policyMismatch(in.Claim, docs, r)
		case CodeDateInconsistency:
			fs, err = dateInconsistency(docs, in.Today, r)
		case CodeLowQualityDocument:
			fs, err = lowQuality(docs, r)
		default:
			continue // aturan tanpa implementasi diabaikan
		}
		if err != nil {
			return nil, err
		}
		for i := range fs {
			fs[i].Code = r.Code
			fs[i].RuleID = r.ID
			fs[i].RuleVersion = r.Version
			if err := fs[i].Validate(); err != nil {
				return nil, fmt.Errorf("aturan %s: %w", r.Code, err)
			}
		}
		out = append(out, fs...)
	}
	return out, nil
}

// --- MISSING_DOCUMENT ---

func missingDocument(in Input, r Rule) ([]Finding, error) {
	cfg, err := decode[MissingDocumentConfig](r)
	if err != nil || !isHealthProduct(in.Claim.ProductType) {
		return nil, err
	}
	present := map[string]bool{}
	for _, d := range in.Documents {
		present[d.Type] = true // status apa pun: dokumen sudah diunggah
	}
	var out []Finding
	for _, t := range cfg.RequiredDocumentTypes {
		if present[t] {
			continue
		}
		out = append(out, Finding{
			Severity: SeverityHigh,
			Message:  fmt.Sprintf("Dokumen wajib %q belum ada pada klaim ini; perlu ditinjau.", typeLabel(t)),
			Evidence: []Evidence{
				{Source: SourceChecklist, Field: "document_type", Value: t,
					Note: "wajib untuk klaim kesehatan; belum diunggah"},
				{Source: SourceClaimRecord, Field: "product_type", Value: in.Claim.ProductType},
			},
			Fingerprint: CodeMissingDocument + "|" + t,
		})
	}
	return out, nil
}

// --- MISSING_FIELD ---

func missingField(docs []Document, r Rule) ([]Finding, error) {
	cfg, err := decode[MissingFieldConfig](r)
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, d := range docs {
		for _, key := range cfg.RequiredFields[d.Type] {
			f, ok := d.field(key)
			if ok && f.Status == string(extraction.StatusFound) && f.Value != "" {
				continue
			}
			if !ok {
				f = Field{Key: key, Status: string(extraction.StatusNotFound)}
			}
			label := extraction.Key(key).Label()
			sev := SeverityMedium
			note := fmt.Sprintf("tidak ditemukan pada %d halaman dokumen", max(d.PageCount, 1))
			msg := fmt.Sprintf("Field %q tidak ditemukan pada %s; perlu ditinjau.", label, typeLabel(d.Type))
			if f.Status == string(extraction.StatusUncertain) {
				sev = SeverityLow
				note = "terbaca tetapi tidak pasti"
				if f.Confidence != nil {
					note = fmt.Sprintf("terbaca tetapi tidak pasti (confidence %.1f)", *f.Confidence)
				}
				msg = fmt.Sprintf("Field %q pada %s tidak terbaca dengan pasti; perlu ditinjau.", label, typeLabel(d.Type))
			}
			out = append(out, Finding{
				Severity:    sev,
				Message:     msg,
				Evidence:    []Evidence{docEvidence(d, f, note)},
				Fingerprint: fmt.Sprintf("%s|doc:%d|%s", CodeMissingField, d.ID, key),
			})
		}
	}
	return out, nil
}

// --- NAME_MISMATCH ---

type nameSource struct {
	key   string // untuk fingerprint
	label string
	ev    Evidence
	norm  []string
}

func nameMismatch(c ClaimRecord, docs []Document, r Rule) ([]Finding, error) {
	cfg, err := decode[NameMismatchConfig](r)
	if err != nil {
		return nil, err
	}
	titles := newTitleSet(cfg.Titles)

	var claimSrc *nameSource
	if n := titles.normalize(c.ClaimantName); len(n) > 0 {
		claimSrc = &nameSource{
			key: "claim", label: "data klaim", norm: n,
			ev: Evidence{Source: SourceClaimRecord, Field: "claimant_name", Value: c.ClaimantName},
		}
	}
	var docSrcs []nameSource
	var ref *nameSource
	for _, d := range docs {
		if d.Type == string(documents.TypeOther) {
			continue
		}
		// Nama yang tidak terbaca pasti (uncertain/not_found) tidak
		// dibandingkan: tidak pernah dianggap cocok maupun berbeda.
		f, ok := d.found(string(extraction.KeyClaimantName))
		if !ok {
			continue
		}
		n := titles.normalize(f.Value)
		if len(n) == 0 {
			continue
		}
		docSrcs = append(docSrcs, nameSource{
			key: fmt.Sprintf("doc:%d", d.ID), label: typeLabel(d.Type), norm: n,
			ev: docEvidence(d, f, ""),
		})
		if d.Type == string(documents.TypeIdentity) {
			s := docSrcs[len(docSrcs)-1]
			ref = &s
		}
	}
	// Acuan: dokumen identitas bila namanya terbaca; selain itu data klaim.
	if ref == nil {
		ref = claimSrc
	}
	if ref == nil {
		return nil, nil
	}
	others := docSrcs
	if ref != claimSrc && claimSrc != nil {
		others = append([]nameSource{*claimSrc}, docSrcs...)
	}
	var out []Finding
	for _, s := range others {
		if s.key == ref.key {
			continue
		}
		res := compareNames(s.norm, ref.norm)
		if res == NameEqual {
			continue
		}
		f := Finding{
			Evidence:    []Evidence{s.ev, ref.ev},
			Fingerprint: fmt.Sprintf("%s|%s|%s|%s", CodeNameMismatch, s.key, ref.key, res),
		}
		if res == NameAbbreviation {
			f.Severity = SeverityLow
			f.Message = fmt.Sprintf("Nama pada %s kemungkinan singkatan dari nama pada %s; perlu ditinjau.", s.label, ref.label)
		} else {
			f.Severity = SeverityMedium
			f.Message = fmt.Sprintf("Nama pada %s berbeda dengan nama pada %s; perlu ditinjau.", s.label, ref.label)
		}
		out = append(out, f)
	}
	return out, nil
}

// --- POLICY_MISMATCH ---

func normPolicy(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
}

func policyMismatch(c ClaimRecord, docs []Document, r Rule) ([]Finding, error) {
	cfg, err := decode[PolicyMismatchConfig](r)
	if err != nil || strings.TrimSpace(c.PolicyNumber) == "" {
		return nil, err
	}
	var out []Finding
	for _, d := range latestOfType(docs, cfg.DocumentTypes...) {
		f, ok := d.found(string(extraction.KeyPolicyNumber))
		if !ok || normPolicy(f.Value) == normPolicy(c.PolicyNumber) {
			continue
		}
		out = append(out, Finding{
			Severity: SeverityMedium,
			Message:  fmt.Sprintf("Nomor polis pada %s berbeda dengan nomor polis pada data klaim; perlu ditinjau.", typeLabel(d.Type)),
			Evidence: []Evidence{
				docEvidence(d, f, ""),
				{Source: SourceClaimRecord, Field: "policy_number", Value: c.PolicyNumber},
			},
			Fingerprint: fmt.Sprintf("%s|doc:%d", CodePolicyMismatch, d.ID),
		})
	}
	return out, nil
}

// --- DATE_INCONSISTENCY ---

func validISODate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, r := range s {
		if i != 4 && i != 7 && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func dateInconsistency(docs []Document, today string, r Rule) ([]Finding, error) {
	cfg, err := decode[DateInconsistencyConfig](r)
	if err != nil {
		return nil, err
	}
	type dated struct {
		d Document
		f Field
	}
	var events []dated
	for _, d := range latestOfType(docs, cfg.EventDocumentTypes...) {
		if f, ok := d.found(string(extraction.KeyEventDate)); ok && validISODate(f.Value) {
			events = append(events, dated{d, f})
		}
	}
	var out []Finding
	for _, d := range latestOfType(docs, cfg.ReceiptDocumentTypes...) {
		rf, ok := d.found(string(extraction.KeyDocumentDate))
		if !ok || !validISODate(rf.Value) {
			continue
		}
		ev := []Evidence{docEvidence(d, rf, "")}
		for _, e := range events {
			if rf.Value < e.f.Value { // format ISO: perbandingan string = perbandingan tanggal
				ev = append(ev, docEvidence(e.d, e.f, ""))
			}
		}
		if len(ev) == 1 {
			continue
		}
		out = append(out, Finding{
			Severity: SeverityMedium,
			Message: fmt.Sprintf("Tanggal pada %s lebih awal dari tanggal kejadian/masuk perawatan; perlu ditinjau.",
				typeLabel(d.Type)),
			Evidence:    ev,
			Fingerprint: fmt.Sprintf("%s|receipt_before_event|doc:%d", CodeDateInconsistency, d.ID),
		})
	}
	if cfg.CheckFutureDates && validISODate(today) {
		for _, d := range docs {
			for _, key := range []extraction.Key{extraction.KeyEventDate, extraction.KeyDocumentDate} {
				f, ok := d.found(string(key))
				if !ok || !validISODate(f.Value) || f.Value <= today {
					continue
				}
				out = append(out, Finding{
					Severity: SeverityMedium,
					Message: fmt.Sprintf("%s pada %s berada setelah tanggal hari ini; perlu ditinjau.",
						key.Label(), typeLabel(d.Type)),
					Evidence:    []Evidence{docEvidence(d, f, "tanggal di masa depan")},
					Fingerprint: fmt.Sprintf("%s|future|doc:%d|%s", CodeDateInconsistency, d.ID, key),
				})
			}
		}
	}
	return out, nil
}

// --- LOW_QUALITY_DOCUMENT ---

func lowQuality(docs []Document, r Rule) ([]Finding, error) {
	cfg, err := decode[LowQualityConfig](r)
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, d := range docs {
		var ev []Evidence
		for _, p := range d.Pages {
			// Lapisan teks PDF bukan hasil OCR; kualitasnya tidak diukur
			// dengan confidence.
			if p.Engine == "pdftotext" {
				continue
			}
			low := p.WordCount < cfg.MinWords || p.MeanConfidence == nil || *p.MeanConfidence < cfg.MinMeanConfidence
			if !low {
				continue
			}
			note := fmt.Sprintf("%d kata terbaca; confidence OCR tidak tersedia", p.WordCount)
			if p.MeanConfidence != nil {
				note = fmt.Sprintf("confidence OCR rata-rata %.1f (ambang %.0f); %d kata terbaca",
					*p.MeanConfidence, cfg.MinMeanConfidence, p.WordCount)
			}
			ev = append(ev, Evidence{
				Source: SourceDocument, DocumentID: d.ID, DocumentType: d.Type, Page: p.Number, Note: note,
			})
		}
		if len(ev) == 0 {
			continue
		}
		sev := SeverityMedium
		if len(ev) == len(d.Pages) {
			sev = SeverityHigh // tidak ada halaman yang terbaca andal
		}
		out = append(out, Finding{
			Severity: sev,
			Message: fmt.Sprintf("Kualitas hasil baca %s rendah sehingga isinya tidak dapat dibaca dengan andal; perlu ditinjau.",
				typeLabel(d.Type)),
			Evidence:    ev,
			Fingerprint: fmt.Sprintf("%s|doc:%d", CodeLowQualityDocument, d.ID),
		})
	}
	return out, nil
}
