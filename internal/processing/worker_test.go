package processing

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/documents"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/ocr"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/pii"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/testutil"
)

const samples = "../../sample-data/"

// NIK sintetis pada sample-data (berawalan 0000).
const sampleNIK = "0000010101900001"

func requireTools(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"pdftotext", "pdftoppm", "tesseract"} {
		if _, err := exec.LookPath(bin); err != nil {
			if os.Getenv("CI") != "" {
				t.Fatalf("%s wajib terpasang di CI", bin)
			}
			t.Skipf("%s tidak terpasang", bin)
		}
	}
}

// fakeEngine mengembalikan hasil dari fungsi uji.
type fakeEngine struct {
	mu    sync.Mutex
	calls int
	fn    func(call int) (ocr.Result, error)
}

func (f *fakeEngine) Process(context.Context, []byte, string) (ocr.Result, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	return f.fn(n)
}

func okResult() ocr.Result {
	conf := 100.0
	return ocr.Result{Pages: []ocr.Page{{
		Number: 1, Engine: ocr.EnginePDFText, EngineVersion: "test", MeanConfidence: &conf, WordCount: 4,
		Lines: []ocr.Line{{Words: []ocr.Word{{Text: "Nama", Conf: 100}, {Text: "peserta", Conf: 100}, {Text: ":", Conf: 100}, {Text: "Uji", Conf: 100}, {Text: "Fiktif", Conf: 100}}}},
	}}}
}

type env struct {
	t       *testing.T
	pool    *pgxpool.Pool
	q       *db.Queries
	docs    *documents.Service
	queue   *Client
	storage *documents.Storage
	officer claims.Actor
	other   claims.Actor
	claim   db.Claim
	logs    *bytes.Buffer
	log     *slog.Logger
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testutil.DB(t)
	q := db.New(pool)
	ctx := context.Background()
	mk := func(name, email string) db.User {
		u, err := q.UpsertUser(ctx, db.UpsertUserParams{Name: name, Email: email, PasswordHash: "x", Role: string(authz.RoleClaimsOfficer)})
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	o1 := mk("Uji Petugas Satu", "petugas1.test@example.com")
	o2 := mk("Uji Petugas Dua", "petugas2.test@example.com")
	var c db.Claim
	err := pool.QueryRow(ctx, `INSERT INTO claims (claim_number, policy_number, claimant_name, product_type, received_at, status, assigned_to)
		VALUES ('CLM-DEMO-0001', 'POL-DEMO-0101', 'Rani Kusuma Dewi', 'Kesehatan (simulasi)', now(), 'submitted', $1)
		RETURNING id, claim_number, policy_number, claimant_name, status, assigned_to`, o1.ID).
		Scan(&c.ID, &c.ClaimNumber, &c.PolicyNumber, &c.ClaimantName, &c.Status, &c.AssignedTo)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	st, err := documents.NewStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := documents.NewService(pool, st, logger)
	qc, err := NewClient(pool, ClientConfig{Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetQueue(qc.Queue)
	return &env{
		t: t, pool: pool, q: q, docs: svc, queue: qc, storage: st,
		officer: claims.Actor{UserID: o1.ID, Role: authz.RoleClaimsOfficer},
		other:   claims.Actor{UserID: o2.ID, Role: authz.RoleClaimsOfficer},
		claim:   c, logs: logs, log: logger,
	}
}

func (e *env) upload(docType documents.Type, name string, data []byte) db.ClaimDocument {
	e.t.Helper()
	docs, err := e.docs.Upload(context.Background(), e.officer, documents.Upload{
		ClaimID: e.claim.ID, Type: docType, Files: []documents.File{{Name: name, Data: data}}, ConfirmDuplicate: true,
	})
	if err != nil {
		e.t.Fatalf("upload: %v", err)
	}
	return docs[0]
}

func (e *env) sample(name string) []byte {
	e.t.Helper()
	b, err := os.ReadFile(samples + name)
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

func (e *env) doc(id int64) db.ClaimDocument {
	e.t.Helper()
	d, err := e.q.GetDocumentByID(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

func job(d db.ClaimDocument, attempt int) *river.Job[ProcessClaimDocumentArgs] {
	return &river.Job[ProcessClaimDocumentArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: attempt, MaxAttempts: MaxAttempts},
		Args:   ProcessClaimDocumentArgs{DocumentID: d.ID, QueuedAt: QueuedToken(*d.QueuedAt)},
	}
}

func (e *env) worker(engine ocr.Engine) *Worker {
	w := NewWorker(e.pool, e.storage, engine, e.log)
	w.RetryBase = time.Millisecond
	return w
}

func (e *env) jobCount(docID int64) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM river_job WHERE kind = 'process_claim_document' AND (args->>'document_id')::bigint = $1`, docID).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) fields(docID int64) map[string]db.ExtractedField {
	e.t.Helper()
	rows, err := e.q.ListExtractedFields(context.Background(), docID)
	if err != nil {
		e.t.Fatal(err)
	}
	m := map[string]db.ExtractedField{}
	for _, r := range rows {
		m[r.FieldKey] = r
	}
	return m
}

func (e *env) audits(action string) []db.AuditLog {
	e.t.Helper()
	rows, err := e.q.ListAuditLogsByAction(context.Background(), db.ListAuditLogsByActionParams{Action: action, Limit: 100})
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func val(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func TestUploadQueuesExactlyOneJob(t *testing.T) {
	e := newEnv(t)
	d := e.upload(documents.TypeClaimForm, "f.pdf", e.sample("01_formulir_klaim.pdf"))
	if d.ProcessingStatus != documents.StatusQueued || e.jobCount(d.ID) != 1 {
		t.Fatalf("unggah harus queued dengan 1 job, dapat %q / %d job", d.ProcessingStatus, e.jobCount(d.ID))
	}
	if len(e.audits(audit.ActionDocumentProcessingQueued)) != 1 {
		t.Fatal("audit document_processing_queued harus tercatat")
	}

	// Proses ulang saat masih queued ditolak: tidak ada job ganda.
	if err := e.docs.Reprocess(context.Background(), e.officer, e.claim.ID, d.ID); !errors.Is(err, documents.ErrAlreadyQueued) {
		t.Fatalf("proses ulang dokumen queued harus ErrAlreadyQueued, dapat %v", err)
	}
	// Insert ulang job untuk permintaan yang sama dilewati River (unique).
	err := pgx.BeginFunc(context.Background(), e.pool, func(tx pgx.Tx) error {
		return e.queue.Queue.EnqueueTx(context.Background(), tx, d.ID, *d.QueuedAt)
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := e.jobCount(d.ID); n != 1 {
		t.Fatalf("job duplikat tidak boleh dibuat, dapat %d job", n)
	}

	// Setelah selesai, proses ulang membuat tepat satu job baru.
	if err := e.worker(&fakeEngine{fn: func(int) (ocr.Result, error) { return okResult(), nil }}).Work(context.Background(), job(d, 1)); err != nil {
		t.Fatal(err)
	}
	if err := e.docs.Reprocess(context.Background(), e.officer, e.claim.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.docs.Reprocess(context.Background(), e.officer, e.claim.ID, d.ID); !errors.Is(err, documents.ErrAlreadyQueued) {
		t.Fatalf("proses ulang kedua harus ditolak, dapat %v", err)
	}
	if n := e.jobCount(d.ID); n != 2 || e.doc(d.ID).ProcessingStatus != documents.StatusQueued {
		t.Fatalf("proses ulang harus menambah tepat 1 job, dapat %d", n)
	}
}

// Worker yang menerima job ganda untuk dokumen yang sudah selesai tidak
// memproses ulang.
func TestWorkerSkipsCompletedAndStaleJobs(t *testing.T) {
	e := newEnv(t)
	d := e.upload(documents.TypeOther, "a.png", testutil.PNG(1))
	eng := &fakeEngine{fn: func(int) (ocr.Result, error) { return okResult(), nil }}
	w := e.worker(eng)
	if err := w.Work(context.Background(), job(d, 1)); err != nil {
		t.Fatal(err)
	}
	if err := w.Work(context.Background(), job(d, 1)); err != nil || eng.calls != 1 {
		t.Fatalf("job ganda tidak boleh memproses ulang (calls=%d, err=%v)", eng.calls, err)
	}
	stale := job(d, 1)
	stale.Args.QueuedAt = QueuedToken(d.QueuedAt.Add(-time.Second))
	if err := w.Work(context.Background(), stale); err != nil || eng.calls != 1 {
		t.Fatal("job dari permintaan lama harus dilewati")
	}
	if len(e.audits(audit.ActionDocumentProcessingCompleted)) != 1 {
		t.Fatal("harus tepat satu audit completed")
	}
}

func TestWorkerTextPDF(t *testing.T) {
	requireTools(t)
	e := newEnv(t)
	d := e.upload(documents.TypeClaimForm, "f.pdf", e.sample("01_formulir_klaim.pdf"))
	if err := e.worker(ocr.NewExec(ocr.ExecConfig{TempDir: t.TempDir()})).Work(context.Background(), job(d, 1)); err != nil {
		t.Fatal(err)
	}
	got := e.doc(d.ID)
	if got.ProcessingStatus != documents.StatusCompleted || got.ProcessingErrorCode != nil || got.ProcessedAt == nil {
		t.Fatalf("status = %q, ingin completed", got.ProcessingStatus)
	}
	pages, _ := e.q.ListOCRResults(context.Background(), d.ID)
	if len(pages) != 1 || pages[0].Engine != ocr.EnginePDFText || pages[0].EngineVersion == "" {
		t.Fatalf("ocr_results tidak sesuai: %+v", pages)
	}
	f := e.fields(d.ID)
	want := map[string]string{"claimant_name": "Rani Kusuma Dewi", "policy_number": "POL-DEMO-0101",
		"nik": "0000********0001", "event_date": "2026-10-10", "amount": "2750000"}
	for k, v := range want {
		if val(f[k].FieldValue) != v || f[k].Status != "found" || f[k].PageNumber == nil || f[k].SourceExcerpt == nil {
			t.Errorf("%s = %q (%s), ingin %q found dengan halaman & kutipan", k, val(f[k].FieldValue), f[k].Status, v)
		}
	}
	if len(e.audits(audit.ActionDocumentProcessingStarted)) != 1 || len(e.audits(audit.ActionDocumentProcessingCompleted)) != 1 {
		t.Fatal("audit started/completed harus tercatat")
	}
	// Status klaim tidak diubah oleh pemrosesan dokumen.
	c, _ := e.q.GetClaim(context.Background(), e.claim.ID)
	if c.Status != e.claim.Status {
		t.Fatalf("status klaim berubah menjadi %q", c.Status)
	}
}

func TestWorkerScannedPDF(t *testing.T) {
	requireTools(t)
	e := newEnv(t)
	jpg, w, h, err := testutil.PNGToJPEG(e.sample("02_identitas.png"))
	if err != nil {
		t.Fatal(err)
	}
	d := e.upload(documents.TypeIdentity, "scan.pdf", testutil.ScannedPDF(jpg, w, h))
	if err := e.worker(ocr.NewExec(ocr.ExecConfig{TempDir: t.TempDir()})).Work(context.Background(), job(d, 1)); err != nil {
		t.Fatal(err)
	}
	pages, _ := e.q.ListOCRResults(context.Background(), d.ID)
	if len(pages) != 1 || pages[0].Engine != ocr.EngineTesseract || pages[0].MeanConfidence == nil {
		t.Fatalf("PDF hasil scan harus diproses Tesseract: %+v", pages)
	}
	f := e.fields(d.ID)
	if val(f["claimant_name"].FieldValue) != "Rani Kusuma Dewi" || val(f["nik"].FieldValue) != "0000********0001" {
		t.Fatalf("field dari PDF hasil scan salah: nama=%q nik=%q", val(f["claimant_name"].FieldValue), val(f["nik"].FieldValue))
	}
}

// TC-05: gambar buram → uncertain, tidak menebak nilai.
func TestWorkerBlurryImageUncertain(t *testing.T) {
	requireTools(t)
	e := newEnv(t)
	d := e.upload(documents.TypeIdentity, "buram.png", e.sample("d_identitas_buram.png"))
	if err := e.worker(ocr.NewExec(ocr.ExecConfig{TempDir: t.TempDir()})).Work(context.Background(), job(d, 1)); err != nil {
		t.Fatal(err)
	}
	if e.doc(d.ID).ProcessingStatus != documents.StatusCompleted {
		t.Fatal("dokumen buram tetap selesai diproses (dengan hasil tidak pasti)")
	}
	f := e.fields(d.ID)
	if len(f) != 2 {
		t.Fatalf("identitas harus punya 2 field, dapat %d", len(f))
	}
	for k, r := range f {
		if r.Status != "uncertain" || r.FieldValue != nil {
			t.Errorf("%s harus uncertain tanpa nilai, dapat %s %q", k, r.Status, val(r.FieldValue))
		}
	}
}

// OCR gagal pada percobaan pertama, berhasil pada percobaan kedua.
func TestWorkerRetryThenSuccess(t *testing.T) {
	e := newEnv(t)
	d := e.upload(documents.TypeOther, "a.png", testutil.PNG(1))
	eng := &fakeEngine{fn: func(call int) (ocr.Result, error) {
		if call == 1 {
			return ocr.Result{}, &ocr.Error{Code: ocr.CodeEngineError, Retryable: true, Err: errors.New("uji")}
		}
		return okResult(), nil
	}}
	w := e.worker(eng)
	if err := w.Work(context.Background(), job(d, 1)); err == nil {
		t.Fatal("percobaan pertama harus mengembalikan error agar River mencoba ulang")
	}
	got := e.doc(d.ID)
	if got.ProcessingStatus != documents.StatusQueued || val(got.ProcessingErrorCode) != "OCR_ENGINE_ERROR" || got.ProcessingAttempts != 1 {
		t.Fatalf("setelah gagal sementara: status=%q code=%q attempts=%d", got.ProcessingStatus, val(got.ProcessingErrorCode), got.ProcessingAttempts)
	}
	if next := w.NextRetry(job(d, 2)); time.Until(next) > time.Second {
		t.Fatal("backoff uji harus pendek")
	}
	if err := w.Work(context.Background(), job(d, 2)); err != nil {
		t.Fatal(err)
	}
	got = e.doc(d.ID)
	if got.ProcessingStatus != documents.StatusCompleted || got.ProcessingErrorCode != nil || got.ProcessingAttempts != 2 {
		t.Fatalf("setelah retry: status=%q code=%q attempts=%d", got.ProcessingStatus, val(got.ProcessingErrorCode), got.ProcessingAttempts)
	}
	fails := e.audits(audit.ActionDocumentProcessingFailed)
	if len(fails) != 1 || !strings.Contains(string(fails[0].AfterJson), `"final": false`) && !strings.Contains(string(fails[0].AfterJson), `"final":false`) {
		t.Fatalf("audit failed (final=false) harus tercatat: %d", len(fails))
	}
}

func TestWorkerFinalFailureStoresErrorCode(t *testing.T) {
	e := newEnv(t)
	d := e.upload(documents.TypeOther, "a.png", testutil.PNG(1))
	eng := &fakeEngine{fn: func(int) (ocr.Result, error) {
		return ocr.Result{}, &ocr.Error{Code: ocr.CodeTimeout, Retryable: true, Err: errors.New("uji")}
	}}
	w := e.worker(eng)
	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		if err := w.Work(context.Background(), job(d, attempt)); err == nil {
			t.Fatal("harus gagal")
		}
	}
	got := e.doc(d.ID)
	if got.ProcessingStatus != documents.StatusFailed || val(got.ProcessingErrorCode) != "OCR_TIMEOUT" {
		t.Fatalf("setelah percobaan habis: status=%q code=%q", got.ProcessingStatus, val(got.ProcessingErrorCode))
	}

	// Error yang tidak dapat dicoba ulang langsung gagal final (JobCancel).
	d2 := e.upload(documents.TypeOther, "b.png", testutil.PNG(2))
	w2 := e.worker(&fakeEngine{fn: func(int) (ocr.Result, error) { return ocr.Result{}, &ocr.Error{Code: ocr.CodeUnsupportedType} }})
	err := w2.Work(context.Background(), job(d2, 1))
	var ce *river.JobCancelError
	if !errors.As(err, &ce) || e.doc(d2.ID).ProcessingStatus != documents.StatusFailed {
		t.Fatalf("error permanen harus JobCancel dan status failed, dapat %v", err)
	}

	// Petugas dapat memproses ulang dokumen gagal.
	if err := e.docs.Reprocess(context.Background(), e.officer, e.claim.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	got = e.doc(d.ID)
	if got.ProcessingStatus != documents.StatusQueued || got.ProcessingErrorCode != nil || got.ProcessingAttempts != 0 {
		t.Fatalf("proses ulang harus mereset status: %q %q %d", got.ProcessingStatus, val(got.ProcessingErrorCode), got.ProcessingAttempts)
	}
}

// Akses lintas petugas ditolak untuk hasil OCR, status, dan proses ulang.
func TestCrossOfficerAccessDenied(t *testing.T) {
	e := newEnv(t)
	d := e.upload(documents.TypeOther, "a.png", testutil.PNG(1))
	ctx := context.Background()
	if _, err := e.docs.Result(ctx, e.other, e.claim.ID, d.ID); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("Result harus ditolak, dapat %v", err)
	}
	if _, err := e.docs.Status(ctx, e.other, e.claim.ID, d.ID); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("Status harus ditolak, dapat %v", err)
	}
	if err := e.docs.Reprocess(ctx, e.other, e.claim.ID, d.ID); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("Reprocess harus ditolak, dapat %v", err)
	}
	if _, err := e.docs.Compare(ctx, e.other, e.claim); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("Compare harus ditolak, dapat %v", err)
	}
	if n := len(e.audits(audit.ActionAccessDenied)); n != 4 {
		t.Fatalf("setiap penolakan harus dicatat, dapat %d", n)
	}
	// Pemilik tetap bisa; dokumen klaim lain tidak dapat diakses lewat ID.
	if _, err := e.docs.Result(ctx, e.officer, e.claim.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.docs.Result(ctx, e.officer, e.claim.ID, d.ID+999); !errors.Is(err, documents.ErrNotFound) {
		t.Fatalf("dokumen tidak ada harus ErrNotFound, dapat %v", err)
	}
}

// Nilai NIK tidak pernah tersimpan utuh di tabel mana pun, audit, job,
// maupun log.
func TestNIKNeverStoredWhole(t *testing.T) {
	requireTools(t)
	e := newEnv(t)
	w := e.worker(ocr.NewExec(ocr.ExecConfig{TempDir: t.TempDir()}))
	for _, s := range []struct {
		typ  documents.Type
		file string
	}{{documents.TypeClaimForm, "01_formulir_klaim.pdf"}, {documents.TypeIdentity, "02_identitas.png"}} {
		d := e.upload(s.typ, s.file, e.sample(s.file))
		if err := w.Work(context.Background(), job(d, 1)); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	var dump string
	err := e.pool.QueryRow(ctx, `SELECT
		coalesce((SELECT string_agg(extracted_text, '|') FROM ocr_results), '') ||
		coalesce((SELECT string_agg(coalesce(field_value,'') || coalesce(source_excerpt,''), '|') FROM extracted_fields), '') ||
		coalesce((SELECT string_agg(coalesce(before_json::text,'') || coalesce(after_json::text,''), '|') FROM audit_logs), '') ||
		coalesce((SELECT string_agg(args::text || coalesce(errors::text,''), '|') FROM river_job), '')`).Scan(&dump)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dump, "0000********0001") {
		t.Fatal("bentuk tersamar NIK seharusnya ada (sanity check)")
	}
	if strings.Contains(dump, sampleNIK) || pii.ContainsUnmaskedNIK(dump) {
		t.Fatal("NIK utuh tersimpan di database")
	}
	logs := e.logs.String()
	if strings.Contains(logs, sampleNIK) || strings.Contains(logs, "Rani") || strings.Contains(logs, "POL-DEMO") {
		t.Fatal("log memuat PII atau teks OCR")
	}
	// Audit tidak memuat teks OCR / nilai field.
	var auditText string
	_ = e.pool.QueryRow(ctx, `SELECT coalesce(string_agg(coalesce(after_json::text,''), '|'), '') FROM audit_logs`).Scan(&auditText)
	if strings.Contains(auditText, "Rani") || strings.Contains(auditText, "POL-DEMO") || strings.Contains(auditText, "0000*") {
		t.Fatal("audit memuat isi dokumen")
	}

	// Pertahanan berlapis di database.
	docID := e.doc(1).ID
	if _, err := e.pool.Exec(ctx, `INSERT INTO extracted_fields (document_id, field_key, field_value, status, extractor_version)
		VALUES ($1, 'nik', $2, 'found', 'x')`, docID, sampleNIK); err == nil {
		t.Fatal("database harus menolak NIK utuh di extracted_fields")
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO ocr_results (document_id, page_number, extracted_text, engine, engine_version)
		VALUES ($1, 99, $2, 'tesseract', 'x')`, docID, "NIK "+sampleNIK); err == nil {
		t.Fatal("database harus menolak NIK utuh di ocr_results")
	}
}

// End-to-end dengan client River sungguhan: unggah → job diproses worker.
func TestRiverEndToEnd(t *testing.T) {
	e := newEnv(t)
	eng := &fakeEngine{fn: func(int) (ocr.Result, error) { return okResult(), nil }}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := NewClient(e.pool, ClientConfig{Worker: e.worker(eng), Logger: quiet, FetchPollInterval: 50 * time.Millisecond})
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
	d := e.upload(documents.TypeOther, "a.png", testutil.PNG(3))

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if e.doc(d.ID).ProcessingStatus == documents.StatusCompleted {
			if f := e.fields(d.ID); val(f["claimant_name"].FieldValue) != "Uji Fiktif" {
				t.Fatalf("field tidak tersimpan: %+v", f)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("dokumen tidak selesai diproses; status %q", e.doc(d.ID).ProcessingStatus)
}
