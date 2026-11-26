package validation

import (
	"errors"
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/pii"
)

// Severity temuan: tingkat perhatian untuk petugas, bukan penilaian klaim.
type Severity string

const (
	SeverityLow    Severity = "low"
	SeverityMedium Severity = "medium"
	SeverityHigh   Severity = "high"
)

// Valid melaporkan apakah s dikenal.
func (s Severity) Valid() bool {
	return s == SeverityLow || s == SeverityMedium || s == SeverityHigh
}

// Sumber evidence.
const (
	// SourceDocument: halaman/field pada dokumen klaim.
	SourceDocument = "document"
	// SourceClaimRecord: data klaim yang diinput petugas.
	SourceClaimRecord = "claim_record"
	// SourceChecklist: daftar dokumen wajib pada konfigurasi aturan.
	SourceChecklist = "checklist"
)

// MaxExcerpt: panjang maksimum kutipan evidence (rune).
const MaxExcerpt = 120

// Evidence adalah satu referensi bukti. Nilai NIK selalu tersamar.
type Evidence struct {
	Source       string `json:"source"`
	DocumentID   int64  `json:"document_id,omitempty"`
	DocumentType string `json:"document_type,omitempty"`
	Page         int    `json:"page,omitempty"`
	Field        string `json:"field,omitempty"`
	Value        string `json:"value,omitempty"`
	Excerpt      string `json:"excerpt,omitempty"`
	// Note: sinyal non-PII, mis. "confidence OCR 27.1" atau "tidak ditemukan".
	Note string `json:"note,omitempty"`
}

// Finding adalah satu temuan hasil aturan.
type Finding struct {
	Code        string
	RuleID      int64
	RuleVersion int
	Severity    Severity
	Message     string
	Evidence    []Evidence
	// Fingerprint mengidentifikasi kondisi (aturan + sumber yang
	// dibandingkan). Kondisi sama → fingerprint sama → tidak ada temuan ganda.
	Fingerprint string
}

// Error validasi temuan.
var (
	ErrNoEvidence      = errors.New("temuan wajib memiliki evidence")
	ErrInvalidEvidence = errors.New("evidence tidak valid")
	ErrInvalidFinding  = errors.New("temuan tidak valid")
)

var (
	codeRe      = regexp.MustCompile(`^[A-Z_]{3,64}$`)
	maskedNIKRe = regexp.MustCompile(`^[0-9]{4}\*{8}[0-9]{4}$`)
)

// Validate memastikan temuan layak disimpan: kode & severity dikenal,
// minimal satu evidence, setiap evidence lengkap, dan tidak ada NIK utuh.
// Temuan yang gagal divalidasi TIDAK boleh disimpan.
func (f Finding) Validate() error {
	if !codeRe.MatchString(f.Code) || !f.Severity.Valid() || f.Fingerprint == "" ||
		f.Message == "" || utf8.RuneCountInString(f.Message) > 500 || f.RuleID <= 0 || f.RuleVersion <= 0 {
		return ErrInvalidFinding
	}
	if pii.ContainsUnmaskedNIK(f.Message) {
		return fmt.Errorf("%w: pesan memuat NIK utuh", ErrInvalidFinding)
	}
	if len(f.Evidence) == 0 {
		return ErrNoEvidence
	}
	for i, e := range f.Evidence {
		if err := e.validate(); err != nil {
			return fmt.Errorf("%w (evidence #%d: %v)", ErrInvalidEvidence, i+1, err)
		}
	}
	return nil
}

func (e Evidence) validate() error {
	switch e.Source {
	case SourceDocument:
		if e.DocumentID <= 0 || e.Page <= 0 {
			return errors.New("document_id dan halaman wajib")
		}
		if e.Field == "" && e.Note == "" {
			return errors.New("field atau sinyal wajib")
		}
	case SourceClaimRecord:
		if e.Field == "" {
			return errors.New("field wajib")
		}
	case SourceChecklist:
		if e.Field == "" || e.Value == "" {
			return errors.New("field dan nilai checklist wajib")
		}
	default:
		return errors.New("sumber tidak dikenal")
	}
	if utf8.RuneCountInString(e.Excerpt) > MaxExcerpt {
		return errors.New("kutipan terlalu panjang")
	}
	for _, s := range []string{e.Value, e.Excerpt, e.Note} {
		if pii.ContainsUnmaskedNIK(s) {
			return errors.New("memuat NIK utuh")
		}
	}
	if e.Field == "nik" && e.Value != "" && !maskedNIKRe.MatchString(e.Value) {
		return errors.New("NIK wajib tersamar")
	}
	return nil
}
