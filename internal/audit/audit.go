// Package audit mencatat aktivitas penting ke tabel audit_logs.
//
// Aturan: jangan pernah menyimpan password, token, atau PII mentah
// (email yang dicoba, IP mentah, dsb.) di Before/After.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
)

// Aksi audit yang dikenal.
const (
	ActionLoginSucceeded = "login_succeeded"
	ActionLoginFailed    = "login_failed"
	ActionLogout         = "logout"
	ActionAccessDenied   = "access_denied"
	ActionUserSeeded     = "user_seeded"
	ActionClaimCreated   = "claim_created"
	ActionClaimUpdated   = "claim_updated"
	ActionStatusChanged  = "status_changed"

	ActionDocumentUploaded          = "document_uploaded"
	ActionDocumentViewed            = "document_viewed"
	ActionDocumentDuplicateDetected = "document_duplicate_detected"

	// Tahap 4: pemrosesan OCR/ekstraksi. Tidak pernah memuat teks OCR atau
	// nilai field.
	ActionDocumentProcessingQueued    = "document_processing_queued"
	ActionDocumentProcessingStarted   = "document_processing_started"
	ActionDocumentProcessingCompleted = "document_processing_completed"
	ActionDocumentProcessingFailed    = "document_processing_failed"
	// ActionExtractedValueApplied: petugas menerapkan nilai terdeteksi ke
	// data klaim (nilai disamarkan di audit).
	ActionExtractedValueApplied = "extracted_value_applied"
)

// Jenis entitas.
const (
	EntityUser  = "user"
	EntityRoute = "route"
	EntityClaim = "claim"
	// EntityDocument: entity_id adalah claim_documents.id.
	EntityDocument = "claim_document"
)

// Entry adalah satu catatan audit.
type Entry struct {
	UserID     *int64
	EntityType string
	EntityID   *int64
	Action     string
	Before     any // diserialisasi ke JSON; nil berarti NULL
	After      any
}

// Logger menulis audit log ke database.
type Logger struct {
	q *db.Queries
}

// NewLogger membuat Logger.
func NewLogger(q *db.Queries) *Logger { return &Logger{q: q} }

// Record menyimpan entry. Error dikembalikan agar pemanggil bisa memutuskan
// apakah aksi harus dibatalkan (fail closed) atau cukup dicatat.
func (l *Logger) Record(ctx context.Context, e Entry) error {
	before, err := marshal(e.Before)
	if err != nil {
		return fmt.Errorf("audit: serialisasi before: %w", err)
	}
	after, err := marshal(e.After)
	if err != nil {
		return fmt.Errorf("audit: serialisasi after: %w", err)
	}
	_, err = l.q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		UserID:     e.UserID,
		EntityType: e.EntityType,
		EntityID:   e.EntityID,
		Action:     e.Action,
		BeforeJson: before,
		AfterJson:  after,
	})
	if err != nil {
		return fmt.Errorf("audit: insert: %w", err)
	}
	return nil
}

func marshal(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}
