package processing

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/documents"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/ocr"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/testutil"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/validation"
)

func (e *env) validationJobs() int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM river_job WHERE kind = 'validate_claim' AND (args->>'claim_id')::bigint = $1`, e.claim.ID).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// Job validasi dimasukkan tepat sekali, saat dokumen TERAKHIR klaim selesai
// (berhasil maupun gagal final).
func TestValidationQueuedAfterLastDocument(t *testing.T) {
	e := newEnv(t)
	d1 := e.upload(documents.TypeClaimForm, "a.png", testutil.PNG(1))
	d2 := e.upload(documents.TypeReceipt, "b.png", testutil.PNG(2))
	ok := e.worker(&fakeEngine{fn: func(int) (ocr.Result, error) { return okResult(), nil }})
	ok.SetValidationQueue(e.queue.Queue)
	if err := ok.Work(context.Background(), job(d1, 1)); err != nil {
		t.Fatal(err)
	}
	if n := e.validationJobs(); n != 0 {
		t.Fatalf("validasi tidak boleh diantrekan saat dokumen lain belum selesai: %d", n)
	}
	fail := e.worker(&fakeEngine{fn: func(int) (ocr.Result, error) {
		return ocr.Result{}, &ocr.Error{Code: ocr.CodeUnsupportedType, Retryable: false}
	}})
	fail.SetValidationQueue(e.queue.Queue)
	_ = fail.Work(context.Background(), job(d2, 1))
	if e.doc(d2.ID).ProcessingStatus != documents.StatusFailed {
		t.Fatalf("dokumen kedua harus failed: %s", e.doc(d2.ID).ProcessingStatus)
	}
	if n := e.validationJobs(); n != 1 {
		t.Fatalf("harap tepat 1 job validasi setelah dokumen terakhir selesai, dapat %d", n)
	}
	// Tanpa antrean validasi (konfigurasi lama), tidak ada job.
	if err := e.docs.Reprocess(context.Background(), e.officer, e.claim.ID, d1.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.worker(&fakeEngine{fn: func(int) (ocr.Result, error) { return okResult(), nil }}).Work(context.Background(), job(e.doc(d1.ID), 1)); err != nil {
		t.Fatal(err)
	}
	if n := e.validationJobs(); n != 1 {
		t.Fatalf("worker tanpa antrean validasi tidak boleh memasukkan job: %d", n)
	}
}

// End-to-end dengan River: unggah set skenario b (nama berbeda) → OCR →
// validasi otomatis → temuan NAME_MISMATCH dan status needs_review.
func TestRiverValidationEndToEnd(t *testing.T) {
	requireTools(t)
	e := newEnv(t)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	cs := claims.NewService(e.pool, quiet)
	vs := validation.NewService(e.pool, cs, quiet)
	vs.Now = func() time.Time { return time.Date(2026, 10, 15, 9, 0, 0, 0, claims.Location) }
	w := e.worker(ocr.NewExec(ocr.ExecConfig{TempDir: t.TempDir()}))
	c, err := NewClient(e.pool, ClientConfig{Worker: w, Validator: validation.NewWorker(vs), Logger: quiet, FetchPollInterval: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = c.Stop(stopCtx)
	}()
	e.docs.SetQueue(c.Queue)
	e.upload(documents.TypeClaimForm, "f.pdf", e.sample("b_formulir_klaim_nama_beda.pdf"))
	e.upload(documents.TypeIdentity, "i.png", e.sample("02_identitas.png"))
	e.upload(documents.TypeMedicalNote, "m.pdf", e.sample("03_surat_keterangan_medis.pdf"))
	e.upload(documents.TypeReceipt, "k.pdf", e.sample("04_kuitansi.pdf"))

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		cl, err := e.q.GetClaim(context.Background(), e.claim.ID)
		if err != nil {
			t.Fatal(err)
		}
		if cl.Status == string(claims.StatusNeedsReview) {
			fs, err := e.q.ListClaimFindings(context.Background(), e.claim.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(fs) != 1 || fs[0].FindingCode != validation.CodeNameMismatch {
				var got []string
				for _, f := range fs {
					got = append(got, f.FindingCode)
				}
				t.Fatalf("harap tepat NAME_MISMATCH, dapat %v", got)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("validasi otomatis tidak memindahkan klaim ke needs_review")
}
