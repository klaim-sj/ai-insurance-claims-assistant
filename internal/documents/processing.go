package documents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/extraction"
)

var (
	// ErrAlreadyQueued: dokumen sedang diantrekan/diproses; tidak boleh ada
	// job ganda.
	ErrAlreadyQueued = errors.New("dokumen sedang dalam antrean atau diproses")
	// ErrQueueUnavailable: antrean pemrosesan tidak dikonfigurasi.
	ErrQueueUnavailable = errors.New("antrean pemrosesan tidak tersedia")
)

// ErrorLabel menerjemahkan kode error pemrosesan untuk petugas.
func ErrorLabel(code string) string {
	switch code {
	case "":
		return ""
	case "OCR_TIMEOUT":
		return "OCR melebihi batas waktu."
	case "OCR_ENGINE_UNAVAILABLE":
		return "Mesin OCR tidak tersedia di server."
	case "OCR_ENGINE_ERROR":
		return "Mesin OCR gagal membaca dokumen."
	case "PDF_TEXT_FAILED":
		return "Teks PDF tidak dapat dibaca."
	case "PDF_RENDER_FAILED":
		return "Halaman PDF tidak dapat dirender untuk OCR."
	case "UNSUPPORTED_TYPE":
		return "Jenis file tidak didukung untuk OCR."
	case "TOO_MANY_PAGES":
		return "Jumlah halaman melebihi batas pemrosesan."
	case "FILE_UNAVAILABLE":
		return "File dokumen tidak dapat dibuka dari penyimpanan."
	case "STORAGE_ERROR":
		return "Gangguan membaca penyimpanan dokumen."
	case "WORKER_INTERRUPTED":
		return "Pemrosesan terhenti sebelum selesai."
	case "UNSAFE_OUTPUT":
		return "Hasil OCR tidak dapat disimpan dengan aman."
	}
	return "Terjadi kesalahan saat memproses dokumen."
}

// enqueue mengubah status ke queued (dengan guard anti-duplikasi) lalu
// memasukkan job di transaksi yang sama, dan mencatat audit.
func (s *Service) enqueue(ctx context.Context, tx pgx.Tx, q *db.Queries, a claims.Actor, d db.ClaimDocument, trigger string) (db.ClaimDocument, error) {
	from := d.ProcessingStatus
	nd, err := q.MarkDocumentQueued(ctx, db.MarkDocumentQueuedParams{ID: d.ID, ClaimID: d.ClaimID})
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrAlreadyQueued
	}
	if err != nil {
		return d, err
	}
	if err := s.queue.EnqueueTx(ctx, tx, nd.ID, *nd.QueuedAt); err != nil {
		return d, fmt.Errorf("enqueue dokumen %d: %w", nd.ID, err)
	}
	if err := audit.NewLogger(q).Record(ctx, audit.Entry{
		UserID: &a.UserID, EntityType: audit.EntityDocument, EntityID: &nd.ID,
		Action: audit.ActionDocumentProcessingQueued,
		After: map[string]any{
			"claim_id": nd.ClaimID, "trigger": trigger,
			"from_status": from, "processing_status": nd.ProcessingStatus,
		},
	}); err != nil {
		return d, err
	}
	return nd, nil
}

// Reprocess mengantrekan ulang dokumen yang gagal atau sudah selesai.
// Dokumen yang sedang queued/processing ditolak (ErrAlreadyQueued).
func (s *Service) Reprocess(ctx context.Context, a claims.Actor, claimID, docID int64) error {
	c, err := s.authorize(ctx, a, claimID, authz.PermDocumentsProcess, "document_reprocess")
	if err != nil {
		return err
	}
	if claims.Status(c.Status) == claims.StatusClosed {
		return ErrClaimClosed
	}
	if s.queue == nil {
		return ErrQueueUnavailable
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		d, err := q.GetClaimDocument(ctx, db.GetClaimDocumentParams{ID: docID, ClaimID: claimID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		_, err = s.enqueue(ctx, tx, q, a, d, "reprocess")
		return err
	})
}

// FieldView adalah field terekstrak untuk UI.
type FieldView struct {
	ID              int64
	Key             extraction.Key
	Label           string
	Value           string
	Status          extraction.Status
	Confidence      *float64
	ConfidenceLabel string
	Page            int
	Excerpt         string
}

// OCRPage adalah ringkasan hasil OCR satu halaman (teks sudah disamarkan).
type OCRPage struct {
	Number         int
	Engine         string
	EngineVersion  string
	MeanConfidence *float64
	WordCount      int
	Text           string
}

// Result adalah data halaman detail dokumen.
type Result struct {
	ID           int64
	ClaimID      int64
	Type         Type
	OriginalName string
	MIME         string
	Pages        int
	Status       string
	ErrorCode    string
	Attempts     int
	QueuedAt     *time.Time
	ProcessedAt  *time.Time
	OCR          []OCRPage
	Fields       []FieldView
	// LowQualityPages: halaman yang tidak terbaca dengan andal.
	LowQualityPages []int
}

// Final melaporkan apakah pemrosesan sudah berakhir (bukan antre/proses).
func (r Result) Final() bool { return r.Status != StatusQueued && r.Status != StatusProcessing }

func toFieldView(id int64, key, status string, value *string, conf *float64, page *int32, excerpt *string) FieldView {
	k := extraction.Key(key)
	f := FieldView{ID: id, Key: k, Label: k.Label(), Status: extraction.Status(status), Confidence: conf}
	if value != nil {
		f.Value = *value
	}
	if conf != nil {
		f.ConfidenceLabel = extraction.ConfidenceLabel(conf)
	}
	if page != nil {
		f.Page = int(*page)
	}
	if excerpt != nil {
		f.Excerpt = *excerpt
	}
	return f
}

// getDoc memeriksa otorisasi dan memastikan dokumen milik klaim.
func (s *Service) getDoc(ctx context.Context, a claims.Actor, claimID, docID int64, op string) (db.ClaimDocument, error) {
	if _, err := s.authorize(ctx, a, claimID, authz.PermDocumentsView, op); err != nil {
		return db.ClaimDocument{}, err
	}
	d, err := s.q.GetClaimDocument(ctx, db.GetClaimDocumentParams{ID: docID, ClaimID: claimID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ClaimDocument{}, ErrNotFound
	}
	return d, err
}

func baseResult(d db.ClaimDocument) Result {
	r := Result{
		ID: d.ID, ClaimID: d.ClaimID, Type: Type(d.DocumentType), OriginalName: d.OriginalName,
		MIME: d.MimeType, Status: d.ProcessingStatus, Attempts: int(d.ProcessingAttempts),
		QueuedAt: d.QueuedAt, ProcessedAt: d.ProcessedAt,
	}
	if d.PageCount != nil {
		r.Pages = int(*d.PageCount)
	}
	if d.ProcessingErrorCode != nil {
		r.ErrorCode = *d.ProcessingErrorCode
	}
	return r
}

// Status mengembalikan status pemrosesan saja (tanpa hasil OCR) untuk
// polling; tidak dicatat sebagai akses dokumen.
func (s *Service) Status(ctx context.Context, a claims.Actor, claimID, docID int64) (Result, error) {
	d, err := s.getDoc(ctx, a, claimID, docID, "document_status")
	if err != nil {
		return Result{}, err
	}
	return baseResult(d), nil
}

// Result mengembalikan hasil OCR dan field terekstrak. Akses dicatat
// sebagai document_viewed (mode extraction); bila audit gagal, hasil tidak
// ditampilkan (fail closed).
func (s *Service) Result(ctx context.Context, a claims.Actor, claimID, docID int64) (Result, error) {
	d, err := s.getDoc(ctx, a, claimID, docID, "document_result")
	if err != nil {
		return Result{}, err
	}
	r := baseResult(d)
	pages, err := s.q.ListOCRResults(ctx, d.ID)
	if err != nil {
		return Result{}, err
	}
	for _, p := range pages {
		if extraction.LowQuality(p.Engine, int(p.WordCount), p.MeanConfidence) {
			r.LowQualityPages = append(r.LowQualityPages, int(p.PageNumber))
		}
		r.OCR = append(r.OCR, OCRPage{
			Number: int(p.PageNumber), Engine: p.Engine, EngineVersion: p.EngineVersion,
			MeanConfidence: p.MeanConfidence, WordCount: int(p.WordCount), Text: p.ExtractedText,
		})
	}
	fields, err := s.q.ListExtractedFields(ctx, d.ID)
	if err != nil {
		return Result{}, err
	}
	for _, f := range fields {
		r.Fields = append(r.Fields, toFieldView(f.ID, f.FieldKey, f.Status, f.FieldValue, f.Confidence, f.PageNumber, f.SourceExcerpt))
	}
	if err := s.audit.Record(ctx, audit.Entry{
		UserID: &a.UserID, EntityType: audit.EntityDocument, EntityID: &d.ID,
		Action: audit.ActionDocumentViewed,
		After:  map[string]any{"claim_id": claimID, "mode": "extraction"},
	}); err != nil {
		return Result{}, err
	}
	return r, nil
}

// Comparison membandingkan nilai terdeteksi dengan data klaim.
type Comparison struct {
	Key        extraction.Key
	Label      string
	ClaimValue string
	// Detected nil bila belum ada formulir klaim yang selesai diproses.
	Detected   *FieldView
	DocumentID int64
	Match      bool
}

// Compare mengembalikan perbandingan nama dan nomor polis antara formulir
// klaim terbaru yang selesai diproses dan data klaim. Hanya informasi;
// tidak ada perubahan otomatis.
func (s *Service) Compare(ctx context.Context, a claims.Actor, c db.Claim) ([]Comparison, error) {
	if _, err := s.authorize(ctx, a, c.ID, authz.PermDocumentsView, "document_compare"); err != nil {
		return nil, err
	}
	rows, err := s.q.ListClaimFormFields(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	out := []Comparison{
		{Key: extraction.KeyClaimantName, Label: "Nama pemohon", ClaimValue: c.ClaimantName},
		{Key: extraction.KeyPolicyNumber, Label: "Nomor polis", ClaimValue: c.PolicyNumber},
	}
	for i := range out {
		for _, r := range rows {
			if r.FieldKey != string(out[i].Key) {
				continue
			}
			f := toFieldView(r.ID, r.FieldKey, r.Status, r.FieldValue, r.Confidence, r.PageNumber, r.SourceExcerpt)
			out[i].Detected = &f
			out[i].DocumentID = r.DocumentID
			out[i].Match = f.Status == extraction.StatusFound && SameValue(out[i].Key, f.Value, out[i].ClaimValue)
			break // baris terurut dari formulir terbaru
		}
	}
	return out, nil
}

// SameValue membandingkan nilai tanpa membedakan huruf besar/kecil dan
// spasi berlebih.
func SameValue(k extraction.Key, a, b string) bool {
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	return norm(a) == norm(b)
}
