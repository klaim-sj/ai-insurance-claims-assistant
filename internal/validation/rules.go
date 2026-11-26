// Package validation menjalankan aturan validasi deterministik atas
// dokumen klaim dan hasil ekstraksinya, lalu menyimpan temuan beserta
// evidence reference.
//
// Temuan adalah bahan peninjauan petugas ("perlu ditinjau"), BUKAN
// keputusan klaim: rule engine tidak pernah menyetujui, menolak, menghitung
// manfaat, atau menyimpulkan kelayakan/fraud. Tidak ada LLM di paket ini.
package validation

import (
	"encoding/json"
	"fmt"
)

// Kode aturan sekaligus kode temuan.
const (
	CodeMissingDocument    = "MISSING_DOCUMENT"
	CodeMissingField       = "MISSING_FIELD"
	CodeNameMismatch       = "NAME_MISMATCH"
	CodePolicyMismatch     = "POLICY_MISMATCH"
	CodeDateInconsistency  = "DATE_INCONSISTENCY"
	CodeLowQualityDocument = "LOW_QUALITY_DOCUMENT"
	// CodeNeedsManualVerification: nilai tidak dapat dinilai otomatis
	// dengan andal (field uncertain, dokumen kualitas rendah, atau
	// perbandingan dengan nilai berconfidence rendah). Petugas mencocokkan
	// langsung dengan dokumen; bukan indikasi masalah klaim.
	CodeNeedsManualVerification = "NEEDS_MANUAL_VERIFICATION"
)

// AllCodes dalam urutan tampilan/evaluasi.
var AllCodes = []string{
	CodeMissingDocument, CodeMissingField, CodeNameMismatch,
	CodePolicyMismatch, CodeDateInconsistency, CodeLowQualityDocument,
	CodeNeedsManualVerification,
}

// ruleVersions: versi aturan aktif bawaan (HARUS sama dengan migration
// 00012 + 00013).
var ruleVersions = map[string]int{
	CodeMissingDocument: 1, CodeMissingField: 2, CodeNameMismatch: 2, CodePolicyMismatch: 2,
	CodeDateInconsistency: 1, CodeLowQualityDocument: 1, CodeNeedsManualVerification: 1,
}

// Rule adalah satu aturan aktif (baris validation_rules).
type Rule struct {
	ID      int64
	Code    string
	Version int
	Config  json.RawMessage
}

// Konfigurasi per aturan (validation_rules.rule_config).
type (
	MissingDocumentConfig struct {
		RequiredDocumentTypes []string `json:"required_document_types"`
	}
	// QualityConfig: ambang kualitas halaman OCR (sama dengan
	// LOW_QUALITY_DOCUMENT) untuk gating MISSING_FIELD v2.
	QualityConfig struct {
		MinMeanConfidence float64 `json:"min_mean_confidence"`
		MinWords          int     `json:"min_words"`
	}
	MissingFieldConfig struct {
		RequiredFields map[string][]string `json:"required_fields"`
		// v2: field not_found di dokumen berkualitas rendah menjadi
		// NEEDS_MANUAL_VERIFICATION, bukan MISSING_FIELD.
		DocumentQuality *QualityConfig `json:"document_quality,omitempty"`
	}
	NameMismatchConfig struct {
		// Titles: gelar/sapaan yang diabaikan. Tanpa titik = sapaan di
		// depan nama (dr, h, ny); dengan titik = gelar akademik di belakang
		// nama (s.kom, m.kes).
		Titles []string `json:"titles"`
		// v2: NAME_MISMATCH hanya bila kedua nilai ≥ MinConfidence; selain
		// itu NEEDS_MANUAL_VERIFICATION. 0 = tanpa gating (v1).
		MinConfidence float64 `json:"min_confidence,omitempty"`
	}
	PolicyMismatchConfig struct {
		DocumentTypes []string `json:"document_types"`
		MinConfidence float64  `json:"min_confidence,omitempty"`
	}
	NeedsManualVerificationConfig struct {
		FieldSeverity      Severity `json:"field_severity"`
		ComparisonSeverity Severity `json:"comparison_severity"`
	}
	DateInconsistencyConfig struct {
		ReceiptDocumentTypes []string `json:"receipt_document_types"`
		EventDocumentTypes   []string `json:"event_document_types"`
		CheckFutureDates     bool     `json:"check_future_dates"`
	}
	LowQualityConfig struct {
		MinMeanConfidence float64 `json:"min_mean_confidence"`
		MinWords          int     `json:"min_words"`
	}
)

// defaultConfigs: HARUS sama dengan aturan aktif setelah migrations
// 00012_validation.sql + 00013_validation_v2.sql (dijaga oleh
// TestSeedMatchesDefaultRules).
var defaultConfigs = map[string]any{
	CodeMissingDocument: MissingDocumentConfig{
		RequiredDocumentTypes: []string{"formulir_klaim", "identitas", "surat_keterangan_medis", "kuitansi"},
	},
	CodeMissingField: MissingFieldConfig{RequiredFields: map[string][]string{
		"formulir_klaim":         {"claimant_name", "policy_number", "event_date"},
		"identitas":              {"claimant_name", "nik"},
		"surat_keterangan_medis": {"claimant_name", "event_date"},
		"kuitansi":               {"claimant_name", "document_date", "amount"},
	}, DocumentQuality: &QualityConfig{MinMeanConfidence: 60, MinWords: 5}},
	CodeNameMismatch: NameMismatchConfig{Titles: []string{
		"dr", "drg", "prof", "ir", "h", "hj", "tn", "ny", "nn", "sdr", "sdri", "bpk", "bapak", "ibu",
		"an", "alm", "almh", "s.kom", "s.h", "s.e", "s.t", "s.pd", "s.ked", "s.kep", "s.farm", "s.sos",
		"m.m", "m.kes", "m.t", "m.si", "sp.pd", "sp.a", "amd",
	}, MinConfidence: 85},
	CodePolicyMismatch: PolicyMismatchConfig{DocumentTypes: []string{"formulir_klaim"}, MinConfidence: 85},
	CodeDateInconsistency: DateInconsistencyConfig{
		ReceiptDocumentTypes: []string{"kuitansi"},
		EventDocumentTypes:   []string{"formulir_klaim", "surat_keterangan_medis"},
		CheckFutureDates:     true,
	},
	CodeLowQualityDocument: LowQualityConfig{MinMeanConfidence: 60, MinWords: 5},
	CodeNeedsManualVerification: NeedsManualVerificationConfig{
		FieldSeverity: SeverityLow, ComparisonSeverity: SeverityMedium,
	},
}

// DefaultRules mengembalikan aturan aktif bawaan (dipakai eval dan test; di
// aplikasi aturan dibaca dari tabel validation_rules).
func DefaultRules() []Rule {
	out := make([]Rule, 0, len(AllCodes))
	for i, c := range AllCodes {
		b, err := json.Marshal(defaultConfigs[c])
		if err != nil {
			panic(err)
		}
		out = append(out, Rule{ID: int64(i + 1), Code: c, Version: ruleVersions[c], Config: b})
	}
	return out
}

func decode[T any](r Rule) (T, error) {
	var cfg T
	if err := json.Unmarshal(r.Config, &cfg); err != nil {
		return cfg, fmt.Errorf("rule_config %s v%d tidak valid: %w", r.Code, r.Version, err)
	}
	return cfg, nil
}
