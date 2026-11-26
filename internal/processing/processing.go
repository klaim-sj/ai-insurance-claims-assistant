// Package processing menjalankan OCR + ekstraksi dokumen klaim di
// background memakai queue River (PostgreSQL).
//
// Alur status dokumen: uploaded → queued → processing → completed | failed.
// Status klaim TIDAK diubah di sini. Teks OCR dan nilai field tidak pernah
// masuk ke log atau audit; yang dicatat hanya ID, status, kode error,
// jumlah halaman/field, engine, dan durasi.
package processing

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/documents"
)

// MaxAttempts adalah batas percobaan per permintaan pemrosesan.
const MaxAttempts = 3

// ProcessClaimDocumentArgs adalah argumen job. QueuedAt (claim_documents.
// queued_at, RFC 3339 UTC) membedakan setiap permintaan pemrosesan,
// sehingga job yang sama tidak dapat dimasukkan dua kali, sedangkan proses
// ulang menghasilkan job baru.
type ProcessClaimDocumentArgs struct {
	DocumentID int64  `json:"document_id" river:"unique"`
	QueuedAt   string `json:"queued_at" river:"unique"`
}

// Kind adalah nama jenis job di River.
func (ProcessClaimDocumentArgs) Kind() string { return "process_claim_document" }

// InsertOpts: percobaan terbatas dan unik per (dokumen, permintaan).
func (ProcessClaimDocumentArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: MaxAttempts,
		UniqueOpts:  river.UniqueOpts{ByArgs: true},
	}
}

// QueuedToken memformat queued_at untuk argumen job.
func QueuedToken(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// Queue memasukkan job dalam transaksi pemanggil (transactional enqueue):
// bila transaksi batal, job ikut batal.
type Queue struct {
	client *river.Client[pgx.Tx]
}

// Ensure Queue memenuhi documents.Queue.
var _ documents.Queue = (*Queue)(nil)

// EnqueueTx memasukkan job ProcessClaimDocument. Job duplikat untuk
// permintaan yang sama dilewati oleh River (unique job).
func (q *Queue) EnqueueTx(ctx context.Context, tx pgx.Tx, documentID int64, queuedAt time.Time) error {
	_, err := q.client.InsertTx(ctx, tx, ProcessClaimDocumentArgs{DocumentID: documentID, QueuedAt: QueuedToken(queuedAt)}, nil)
	return err
}

// Client membungkus client River.
type Client struct {
	River *river.Client[pgx.Tx]
	Queue *Queue
}

// ClientConfig mengatur client River.
type ClientConfig struct {
	// Worker nil berarti client hanya untuk insert (tanpa memproses job).
	Worker *Worker
	// MaxWorkers: jumlah job paralel; default 2 (OCR berat CPU).
	MaxWorkers int
	Logger     *slog.Logger
	// FetchPollInterval: interval polling job baru (default River 1 detik).
	FetchPollInterval time.Duration
}

// NewClient membuat client River di atas pool.
func NewClient(pool *pgxpool.Pool, cfg ClientConfig) (*Client, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	rc := &river.Config{Logger: cfg.Logger}
	if cfg.Worker != nil {
		workers := river.NewWorkers()
		rc.Workers = workers
		if err := river.AddWorkerSafely(workers, cfg.Worker); err != nil {
			return nil, err
		}
		n := cfg.MaxWorkers
		if n <= 0 {
			n = 2
		}
		rc.Queues = map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: n}}
		if cfg.FetchPollInterval > 0 {
			rc.FetchPollInterval = cfg.FetchPollInterval
			rc.FetchCooldown = min(cfg.FetchPollInterval, 100*time.Millisecond)
		}
	}
	// Tanpa Worker: client insert-only (Workers nil), job diproses oleh
	// proses lain yang menjalankan worker.
	c, err := river.NewClient(riverpgxv5.New(pool), rc)
	if err != nil {
		return nil, err
	}
	return &Client{River: c, Queue: &Queue{client: c}}, nil
}

// Start mulai memproses job (hanya bila dibuat dengan Worker).
func (c *Client) Start(ctx context.Context) error { return c.River.Start(ctx) }

// Stop menghentikan client dengan menunggu job berjalan selesai sampai ctx
// habis, lalu membatalkannya.
func (c *Client) Stop(ctx context.Context) error {
	err := c.River.Stop(ctx)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		hard, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return c.River.StopAndCancel(hard)
	}
	return err
}
