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
)

// AllCodes dalam urutan tampilan/evaluasi.
var AllCodes = []string{
	CodeMissingDocument, CodeMissingField, CodeNameMismatch,
	CodePolicyMismatch, CodeDateInconsistency, CodeLowQualityDocument,
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
	MissingFieldConfig struct {
		RequiredFields map[string][]string `json:"required_fields"`
	}
	NameMismatchConfig struct {
		// Titles: gelar/sapaan yang diabaikan. Tanpa titik = sapaan di
		// depan nama (dr, h, ny); dengan titik = gelar akademik di belakang
		// nama (s.kom, m.kes).
		Titles []string `json:"titles"`
	}
	PolicyMismatchConfig struct {
		DocumentTypes []string `json:"document_types"`
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

// defaultConfigs: HARUS sama dengan seed di migrations/00012_validation.sql
// (dijaga oleh TestSeedMatchesDefaultRules).
var defaultConfigs = map[string]any{
	CodeMissingDocument: MissingDocumentConfig{
		RequiredDocumentTypes: []string{"formulir_klaim", "identitas", "surat_keterangan_medis", "kuitansi"},
	},
	CodeMissingField: MissingFieldConfig{RequiredFields: map[string][]string{
		"formulir_klaim":         {"claimant_name", "policy_number", "event_date"},
		"identitas":              {"claimant_name", "nik"},
		"surat_keterangan_medis": {"claimant_name", "event_date"},
		"kuitansi":               {"claimant_name", "document_date", "amount"},
	}},
	CodeNameMismatch: NameMismatchConfig{Titles: []string{
		"dr", "drg", "prof", "ir", "h", "hj", "tn", "ny", "nn", "sdr", "sdri", "bpk", "bapak", "ibu",
		"an", "alm", "almh", "s.kom", "s.h", "s.e", "s.t", "s.pd", "s.ked", "s.kep", "s.farm", "s.sos",
		"m.m", "m.kes", "m.t", "m.si", "sp.pd", "sp.a", "amd",
	}},
	CodePolicyMismatch: PolicyMismatchConfig{DocumentTypes: []string{"formulir_klaim"}},
	CodeDateInconsistency: DateInconsistencyConfig{
		ReceiptDocumentTypes: []string{"kuitansi"},
		EventDocumentTypes:   []string{"formulir_klaim", "surat_keterangan_medis"},
		CheckFutureDates:     true,
	},
	CodeLowQualityDocument: LowQualityConfig{MinMeanConfidence: 60, MinWords: 5},
}

// DefaultRules mengembalikan aturan v1 bawaan (dipakai eval dan test; di
// aplikasi aturan dibaca dari tabel validation_rules).
func DefaultRules() []Rule {
	out := make([]Rule, 0, len(AllCodes))
	for i, c := range AllCodes {
		b, err := json.Marshal(defaultConfigs[c])
		if err != nil {
			panic(err)
		}
		out = append(out, Rule{ID: int64(i + 1), Code: c, Version: 1, Config: b})
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
