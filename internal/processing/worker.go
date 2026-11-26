package processing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/documents"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/extraction"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/ocr"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/pii"
)

// Kode error di luar OCR.
const (
	CodeFileUnavailable = "FILE_UNAVAILABLE"
	CodeStorageError    = "STORAGE_ERROR"
	CodeInterrupted     = "WORKER_INTERRUPTED"
	CodeInternal        = "INTERNAL_ERROR"
	CodeUnsafeOutput    = "UNSAFE_OUTPUT"
)

// Worker memproses job ProcessClaimDocument.
type Worker struct {
	river.WorkerDefaults[ProcessClaimDocumentArgs]

	pool    *pgxpool.Pool
	q       *db.Queries
	storage *documents.Storage
	engine  ocr.Engine
	log     *slog.Logger

	// RetryBase: jeda sebelum percobaan kedua; percobaan berikutnya ×4
	// (10 dtk, 40 dtk). Dapat diperkecil di test.
	RetryBase time.Duration
	// JobTimeout: batas waktu satu percobaan.
	JobTimeout time.Duration
}

// NewWorker membuat Worker.
func NewWorker(pool *pgxpool.Pool, storage *documents.Storage, engine ocr.Engine, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		pool: pool, q: db.New(pool), storage: storage, engine: engine, log: logger,
		RetryBase: 10 * time.Second, JobTimeout: 10 * time.Minute,
	}
}

// NextRetry: backoff eksponensial terbatas.
func (w *Worker) NextRetry(job *river.Job[ProcessClaimDocumentArgs]) time.Time {
	d := w.RetryBase
	for i := 1; i < job.Attempt; i++ {
		d *= 4
	}
	return time.Now().Add(d)
}

// Timeout membatasi durasi satu percobaan.
func (w *Worker) Timeout(*river.Job[ProcessClaimDocumentArgs]) time.Duration { return w.JobTimeout }

// jobError adalah kegagalan dengan kode yang disimpan ke dokumen.
type jobError struct {
	code      string
	retryable bool
	err       error
}

func (e *jobError) Error() string { return e.code + ": " + e.err.Error() }
func (e *jobError) Unwrap() error { return e.err }

func classify(err error) *jobError {
	var je *jobError
	if errors.As(err, &je) {
		return je
	}
	var oe *ocr.Error
	if errors.As(err, &oe) {
		return &jobError{code: string(oe.Code), retryable: oe.Retryable, err: err}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &jobError{code: CodeInterrupted, retryable: true, err: err}
	}
	return &jobError{code: CodeInternal, retryable: true, err: err}
}

// Work menjalankan satu percobaan.
func (w *Worker) Work(ctx context.Context, job *river.Job[ProcessClaimDocumentArgs]) error {
	docID := job.Args.DocumentID
	doc, err := w.q.GetDocumentByID(ctx, docID)
	if errors.Is(err, pgx.ErrNoRows) {
		return river.JobCancel(errors.New("dokumen tidak ada"))
	}
	if err != nil {
		return err
	}
	if doc.QueuedAt == nil || QueuedToken(*doc.QueuedAt) != job.Args.QueuedAt {
		// Job dari permintaan lama; permintaan terbaru punya job sendiri.
		return nil
	}
	doc, err = w.q.MarkDocumentProcessing(ctx, db.MarkDocumentProcessingParams{ID: docID, Attempt: int32(job.Attempt)})
	if errors.Is(err, pgx.ErrNoRows) {
		// Sudah selesai/gagal final: jangan memproses dua kali.
		return nil
	}
	if err != nil {
		return err
	}
	start := time.Now()
	err = w.audit(ctx, w.q, doc, audit.ActionDocumentProcessingStarted, map[string]any{
		"attempt": job.Attempt, "max_attempts": job.MaxAttempts,
	})
	var summary map[string]any
	if err == nil {
		summary, err = w.process(ctx, doc, job.Attempt, start)
	}
	if err == nil {
		w.log.Info("dokumen selesai diproses", "document_id", docID, "attempt", job.Attempt,
			"pages", summary["page_count"], "duration_ms", summary["duration_ms"])
		return nil
	}

	je := classify(err)
	final := !je.retryable || job.Attempt >= job.MaxAttempts
	// Catat kegagalan walau ctx job sudah dibatalkan/timeout.
	bg := context.WithoutCancel(ctx)
	if final {
		err = w.q.MarkDocumentFailed(bg, db.MarkDocumentFailedParams{ID: docID, ErrorCode: je.code})
	} else {
		err = w.q.MarkDocumentRetrying(bg, db.MarkDocumentRetryingParams{ID: docID, ErrorCode: je.code})
	}
	if err != nil {
		return err
	}
	if err := w.audit(bg, w.q, doc, audit.ActionDocumentProcessingFailed, map[string]any{
		"attempt": job.Attempt, "max_attempts": job.MaxAttempts, "error_code": je.code,
		"final": final, "duration_ms": time.Since(start).Milliseconds(),
	}); err != nil {
		return err
	}
	// Hanya kode error yang di-log; pesan error tidak memuat isi dokumen
	// tetapi tetap tidak dicetak agar log minimal.
	w.log.Warn("pemrosesan dokumen gagal", "document_id", docID, "attempt", job.Attempt,
		"error_code", je.code, "final", final)
	if final && !je.retryable {
		return river.JobCancel(je)
	}
	return je
}

func (w *Worker) process(ctx context.Context, doc db.ClaimDocument, attempt int, start time.Time) (map[string]any, error) {
	f, err := w.storage.Open(doc.StoragePath)
	if err != nil {
		return nil, &jobError{code: CodeFileUnavailable, err: errors.New("file dokumen tidak dapat dibuka")}
	}
	data, err := io.ReadAll(io.LimitReader(f, documents.MaxFileSize+1))
	f.Close()
	if err != nil {
		return nil, &jobError{code: CodeStorageError, retryable: true, err: errors.New("membaca file dokumen")}
	}
	if len(data) > documents.MaxFileSize {
		return nil, &jobError{code: CodeFileUnavailable, err: errors.New("ukuran file melebihi batas")}
	}

	res, err := w.engine.Process(ctx, data, doc.MimeType)
	if err != nil {
		return nil, err
	}
	fields := extraction.Extract(doc.DocumentType, res.Pages)

	counts := map[extraction.Status]int{}
	engines := map[string]bool{}
	var engineList []string
	err = pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		q := w.q.WithTx(tx)
		if err := q.DeleteOCRResults(ctx, doc.ID); err != nil {
			return err
		}
		if err := q.DeleteExtractedFields(ctx, doc.ID); err != nil {
			return err
		}
		for _, p := range res.Pages {
			text := pii.MaskNIK(p.Text())
			if pii.ContainsUnmaskedNIK(text) {
				return &jobError{code: CodeUnsafeOutput, err: errors.New("teks OCR masih memuat NIK utuh")}
			}
			if err := q.InsertOCRResult(ctx, db.InsertOCRResultParams{
				DocumentID: doc.ID, PageNumber: int32(p.Number), ExtractedText: text,
				Engine: p.Engine, EngineVersion: p.EngineVersion,
				MeanConfidence: round2(p.MeanConfidence), WordCount: int32(p.WordCount),
			}); err != nil {
				return fmt.Errorf("simpan hasil OCR: %w", err)
			}
			if !engines[p.Engine] {
				engines[p.Engine] = true
				engineList = append(engineList, p.Engine)
			}
		}
		for _, fl := range fields {
			counts[fl.Status]++
			p := db.InsertExtractedFieldParams{
				DocumentID: doc.ID, FieldKey: string(fl.Key), Status: string(fl.Status),
				Confidence: fl.Confidence, ExtractorVersion: extraction.Version,
			}
			if fl.Value != "" {
				v := fl.Value
				if fl.Key != extraction.KeyNIK {
					v = pii.MaskNIK(v)
				}
				p.FieldValue = &v
			}
			if fl.Page > 0 {
				n := int32(fl.Page)
				p.PageNumber = &n
			}
			if fl.Excerpt != "" {
				ex := pii.MaskNIK(fl.Excerpt)
				p.SourceExcerpt = &ex
			}
			if err := q.InsertExtractedField(ctx, p); err != nil {
				return fmt.Errorf("simpan field: %w", err)
			}
		}
		if err := q.MarkDocumentCompleted(ctx, doc.ID); err != nil {
			return err
		}
		return w.audit(ctx, q, doc, audit.ActionDocumentProcessingCompleted, map[string]any{
			"attempt": attempt, "page_count": len(res.Pages), "engines": engineList,
			"fields_found": counts[extraction.StatusFound], "fields_uncertain": counts[extraction.StatusUncertain],
			"fields_not_found": counts[extraction.StatusNotFound], "extractor_version": extraction.Version,
			"duration_ms": time.Since(start).Milliseconds(),
		})
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"page_count": len(res.Pages), "duration_ms": time.Since(start).Milliseconds()}, nil
}

// audit mencatat aksi sistem (user_id NULL) tanpa isi dokumen.
func (w *Worker) audit(ctx context.Context, q *db.Queries, doc db.ClaimDocument, action string, after map[string]any) error {
	after["claim_id"] = doc.ClaimID
	after["document_type"] = doc.DocumentType
	return audit.NewLogger(q).Record(ctx, audit.Entry{
		EntityType: audit.EntityDocument, EntityID: &doc.ID, Action: action, After: after,
	})
}

func round2(v *float64) *float64 {
	if v == nil {
		return nil
	}
	r := float64(int64(*v*100+0.5)) / 100
	return &r
}
