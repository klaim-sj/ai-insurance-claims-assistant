package validation_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/testutil"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/validation"
)

// Seluruh data SINTETIS (NIK 0000…, @example.com, nama fiktif).

type env struct {
	t       *testing.T
	pool    *pgxpool.Pool
	q       *db.Queries
	svc     *validation.Service
	claims  *claims.Service
	officer claims.Actor
	other   claims.Actor
	sup     claims.Actor
	claimID int64
	queue   *fakeQueue
}

type fakeQueue struct{ calls []int64 }

func (f *fakeQueue) EnqueueValidationTx(_ context.Context, _ pgx.Tx, claimID int64, _ string) error {
	f.calls = append(f.calls, claimID)
	return nil
}

func newEnv(t *testing.T, status string) *env {
	t.Helper()
	pool := testutil.DB(t)
	ctx := context.Background()
	q := db.New(pool)
	mk := func(name, email string, role authz.Role) claims.Actor {
		u, err := q.UpsertUser(ctx, db.UpsertUserParams{Name: name, Email: email, PasswordHash: "x", Role: string(role)})
		if err != nil {
			t.Fatal(err)
		}
		return claims.Actor{UserID: u.ID, Role: role}
	}
	e := &env{t: t, pool: pool, q: q, queue: &fakeQueue{}}
	e.officer = mk("Uji Petugas Satu", "petugas1.test@example.com", authz.RoleClaimsOfficer)
	e.other = mk("Uji Petugas Dua", "petugas2.test@example.com", authz.RoleClaimsOfficer)
	e.sup = mk("Uji Supervisor", "supervisor.test@example.com", authz.RoleSupervisor)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	e.claims = claims.NewService(pool, logger)
	e.svc = validation.NewService(pool, e.claims, logger)
	e.svc.Now = func() time.Time { return time.Date(2026, 10, 15, 9, 0, 0, 0, claims.Location) }
	e.svc.SetQueue(e.queue)
	if err := pool.QueryRow(ctx, `INSERT INTO claims (claim_number, policy_number, claimant_name, product_type, received_at, status, assigned_to)
		VALUES ('CLM-DEMO-0001', 'POL-DEMO-0101', 'Rani Kusuma Dewi', 'Kesehatan (simulasi)', now(), $1, $2) RETURNING id`,
		status, e.officer.UserID).Scan(&e.claimID); err != nil {
		t.Fatal(err)
	}
	return e
}

var docSeq int

// addDoc menyisipkan dokumen selesai diproses beserta field-nya.
func (e *env) addDoc(docType string, fields map[string]string, conf float64, engine string) int64 {
	e.t.Helper()
	ctx := context.Background()
	docSeq++
	var id int64
	if err := e.pool.QueryRow(ctx, `INSERT INTO claim_documents (claim_id, document_type, original_name, storage_path, mime_type,
		file_size, checksum, processing_status, uploaded_by, page_count)
		VALUES ($1, $2, 'contoh.pdf', $3, 'application/pdf', 100, $4, 'completed', $5, 1) RETURNING id`,
		e.claimID, docType, fmt.Sprintf("x/%d-%d", time.Now().UnixNano(), docSeq),
		fmt.Sprintf("%064x", time.Now().UnixNano()+int64(docSeq)), e.officer.UserID).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	words := 50
	if conf < 60 {
		words = 3
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO ocr_results (document_id, page_number, extracted_text, engine, engine_version, mean_confidence, word_count)
		VALUES ($1, 1, 'teks contoh', $2, 'uji', $3, $4)`, id, engine, conf, words); err != nil {
		e.t.Fatal(err)
	}
	for k, v := range fields {
		status, val := "found", &v
		if v == "" {
			status, val = "not_found", nil
		} else if v == "?" {
			status, val = "uncertain", nil
		}
		var page *int32
		if val != nil {
			p := int32(1)
			page = &p
		}
		if err := e.q.InsertExtractedField(ctx, db.InsertExtractedFieldParams{
			DocumentID: id, FieldKey: k, FieldValue: val, Status: status, Confidence: &conf,
			PageNumber: page, ExtractorVersion: "uji",
		}); err != nil {
			e.t.Fatal(err)
		}
	}
	return id
}

func (e *env) fullSet() (form, ident, med, receipt int64) {
	form = e.addDoc("formulir_klaim", map[string]string{
		"claimant_name": "Rani Kusuma Dewi", "policy_number": "POL-DEMO-0101", "event_date": "2026-10-10",
		"nik": "0000********0001",
	}, 100, "pdftotext")
	ident = e.addDoc("identitas", map[string]string{"claimant_name": "Rani Kusuma Dewi", "nik": "0000********0001"}, 88, "tesseract")
	med = e.addDoc("surat_keterangan_medis", map[string]string{"claimant_name": "Rani Kusuma Dewi", "event_date": "2026-10-10"}, 100, "pdftotext")
	receipt = e.addDoc("kuitansi", map[string]string{
		"claimant_name": "Rani Kusuma Dewi", "document_date": "2026-10-12", "amount": "2750000",
	}, 100, "pdftotext")
	return
}

func (e *env) run() validation.RunResult {
	e.t.Helper()
	r, err := e.svc.Run(context.Background(), e.claimID, validation.TriggerManual)
	if err != nil {
		e.t.Fatalf("Run: %v", err)
	}
	return r
}

func (e *env) findings() []db.ValidationFinding {
	e.t.Helper()
	rows, err := e.q.ListClaimFindings(context.Background(), e.claimID)
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func (e *env) status() string {
	e.t.Helper()
	c, err := e.q.GetClaim(context.Background(), e.claimID)
	if err != nil {
		e.t.Fatal(err)
	}
	return c.Status
}

func (e *env) auditCount(action string) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action = $1`, action).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestSeedMatchesDefaultRules(t *testing.T) {
	pool := testutil.DB(t)
	rows, err := db.New(pool).ListActiveValidationRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	def := validation.DefaultRules()
	if len(rows) != len(def) {
		t.Fatalf("aturan aktif %d, default %d", len(rows), len(def))
	}
	for i, r := range rows {
		var a, b any
		if err := json.Unmarshal(r.RuleConfig, &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(def[i].Config, &b); err != nil {
			t.Fatal(err)
		}
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if r.RuleCode != def[i].Code || int(r.Version) != def[i].Version || string(ja) != string(jb) {
			t.Errorf("seed %s v%d %s ≠ default %s v%d %s", r.RuleCode, r.Version, ja, def[i].Code, def[i].Version, jb)
		}
	}
	// Hanya satu versi aktif per aturan.
	if _, err := pool.Exec(context.Background(), `INSERT INTO validation_rules (rule_code, name, description, version)
		VALUES ('MISSING_FIELD', 'x', 'x', 2)`); err == nil {
		t.Fatal("dua versi aktif untuk aturan yang sama harus ditolak")
	}
}

func TestRunCreatesFindingsWithEvidenceAndNeedsReview(t *testing.T) {
	e := newEnv(t, "submitted")
	form, ident, _, _ := e.fullSet()
	if _, err := e.pool.Exec(context.Background(), `UPDATE extracted_fields SET field_value = 'Rani Kusumawati'
		WHERE document_id = $1 AND field_key = 'claimant_name'`, form); err != nil {
		t.Fatal(err)
	}
	r := e.run()
	if r.Open != 1 || r.Created != 1 || !r.StatusChanged {
		t.Fatalf("hasil %+v", r)
	}
	fs := e.findings()
	if len(fs) != 1 || fs[0].FindingCode != validation.CodeNameMismatch || fs[0].Status != "open" || fs[0].RuleVersion != 1 {
		t.Fatalf("temuan %+v", fs)
	}
	var ev []validation.Evidence
	if err := json.Unmarshal(fs[0].EvidenceJson, &ev); err != nil {
		t.Fatal(err)
	}
	if len(ev) != 2 || ev[0].DocumentID != form || ev[1].DocumentID != ident || ev[0].Page != 1 || ev[0].Field != "claimant_name" {
		t.Fatalf("evidence %+v", ev)
	}
	if e.status() != "needs_review" {
		t.Fatalf("status %s, harap needs_review", e.status())
	}
	// Riwayat status oleh sistem (changed_by NULL).
	var bySystem bool
	if err := e.pool.QueryRow(context.Background(), `SELECT changed_by IS NULL FROM claim_status_histories
		WHERE claim_id = $1 AND new_status = 'needs_review'`, e.claimID).Scan(&bySystem); err != nil || !bySystem {
		t.Fatalf("riwayat needs_review harus oleh sistem: %v", err)
	}
	if e.auditCount(audit.ActionValidationRunStarted) != 1 || e.auditCount(audit.ActionValidationRunCompleted) != 1 ||
		e.auditCount(audit.ActionFindingCreated) != 1 {
		t.Fatal("audit validasi tidak lengkap")
	}
}

func TestRunIsIdempotentAndResolves(t *testing.T) {
	e := newEnv(t, "submitted")
	form, _, _, _ := e.fullSet()
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `UPDATE extracted_fields SET field_value = 'POL-DEMO-0110'
		WHERE document_id = $1 AND field_key = 'policy_number'`, form); err != nil {
		t.Fatal(err)
	}
	first := e.run()
	for range 3 {
		r := e.run()
		if r.Created != 0 || r.Open != first.Open || r.Resolved != 0 {
			t.Fatalf("validasi ulang tidak idempoten: %+v (pertama %+v)", r, first)
		}
	}
	if n := len(e.findings()); n != 1 {
		t.Fatalf("temuan ganda: %d baris", n)
	}
	if e.auditCount(audit.ActionFindingCreated) != 1 {
		t.Fatal("finding_created tidak boleh tercatat ulang untuk kondisi yang sama")
	}

	// Kondisi diperbaiki: temuan menjadi resolved (tidak dihapus).
	if _, err := e.pool.Exec(ctx, `UPDATE extracted_fields SET field_value = 'POL-DEMO-0101'
		WHERE document_id = $1 AND field_key = 'policy_number'`, form); err != nil {
		t.Fatal(err)
	}
	r := e.run()
	fs := e.findings()
	if r.Resolved != 1 || r.Open != 0 || len(fs) != 1 || fs[0].Status != "resolved" || fs[0].ResolvedAt == nil {
		t.Fatalf("hasil %+v temuan %+v", r, fs)
	}
	if e.auditCount(audit.ActionFindingResolved) != 1 {
		t.Fatal("finding_resolved harus tercatat")
	}
	// Kondisi muncul lagi: baris yang sama dibuka kembali, bukan baris baru.
	if _, err := e.pool.Exec(ctx, `UPDATE extracted_fields SET field_value = 'POL-DEMO-0110'
		WHERE document_id = $1 AND field_key = 'policy_number'`, form); err != nil {
		t.Fatal(err)
	}
	r = e.run()
	fs = e.findings()
	if r.Reopened != 1 || len(fs) != 1 || fs[0].Status != "open" || fs[0].ResolvedAt != nil {
		t.Fatalf("hasil %+v temuan %+v", r, fs)
	}
	// Temuan dismissed (keputusan petugas) tidak dibuka kembali otomatis.
	if _, err := e.pool.Exec(ctx, `UPDATE validation_findings SET status = 'dismissed' WHERE id = $1`, fs[0].ID); err != nil {
		t.Fatal(err)
	}
	r = e.run()
	if fs = e.findings(); fs[0].Status != "dismissed" || r.Open != 0 || len(fs) != 1 {
		t.Fatalf("dismissed berubah: %+v", fs)
	}
}

func TestNeedDocumentsNeverSetAutomatically(t *testing.T) {
	for _, st := range []string{"submitted", "processing", "needs_review", "need_documents", "reviewed"} {
		t.Run(st, func(t *testing.T) {
			e := newEnv(t, st)
			// Hanya formulir: tiga dokumen wajib belum ada.
			e.addDoc("formulir_klaim", map[string]string{
				"claimant_name": "Rani Kusuma Dewi", "policy_number": "POL-DEMO-0101", "event_date": "2026-10-10",
			}, 100, "pdftotext")
			r := e.run()
			if r.Open != 3 {
				t.Fatalf("harap 3 temuan MISSING_DOCUMENT, dapat %+v", r)
			}
			want := st
			if st == "submitted" || st == "processing" {
				want = "needs_review"
			}
			if got := e.status(); got != want {
				t.Fatalf("status %s → %s, harap %s", st, got, want)
			}
			var n int
			if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM claim_status_histories
				WHERE claim_id = $1 AND new_status = 'need_documents' AND changed_by IS NULL`, e.claimID).Scan(&n); err != nil || n != 0 {
				t.Fatalf("need_documents tidak boleh di-set sistem: %d %v", n, err)
			}
			p, err := e.svc.Findings(context.Background(), e.officer, e.claimID)
			if err != nil {
				t.Fatal(err)
			}
			if !p.SuggestNeedDocuments || p.OpenCount != 3 {
				t.Fatalf("saran need_documents harus tampil sebagai saran: %+v", p)
			}
		})
	}
}

func TestNoFindingsKeepsStatus(t *testing.T) {
	e := newEnv(t, "submitted")
	e.fullSet()
	if r := e.run(); r.Open != 0 || r.StatusChanged {
		t.Fatalf("set lengkap tidak boleh menghasilkan temuan: %+v", r)
	}
	if e.status() != "submitted" {
		t.Fatal("status tidak boleh berubah tanpa temuan")
	}
}

func TestRunSkipsWhenDocumentsPending(t *testing.T) {
	e := newEnv(t, "submitted")
	id := e.addDoc("formulir_klaim", nil, 100, "pdftotext")
	if _, err := e.pool.Exec(context.Background(), `UPDATE claim_documents SET processing_status = 'processing' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if r := e.run(); r.Skipped != "documents_pending" || len(e.findings()) != 0 {
		t.Fatalf("harus dilewati saat dokumen masih diproses: %+v", r)
	}
	if err := e.svc.RequestRun(context.Background(), e.officer, e.claimID); !errors.Is(err, validation.ErrDocumentsPending) {
		t.Fatalf("RequestRun saat dokumen diproses: %v", err)
	}
}

func TestRunSkipsClosedClaim(t *testing.T) {
	e2 := newEnv(t, "closed")
	e2.addDoc("formulir_klaim", nil, 100, "pdftotext")
	if r := e2.run(); r.Skipped == "" || len(e2.findings()) != 0 {
		t.Fatalf("klaim closed tidak divalidasi: %+v", r)
	}
}

func TestFindingWithoutEvidenceRejected(t *testing.T) {
	e := newEnv(t, "submitted")
	ctx := context.Background()
	rules, err := e.q.ListActiveValidationRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f := validation.Finding{
		Code: validation.CodeMissingField, RuleID: rules[1].ID, RuleVersion: 1, Severity: validation.SeverityMedium,
		Message: "Field tidak ditemukan; perlu ditinjau.", Fingerprint: "uji|tanpa-evidence",
	}
	if _, err := validation.InsertFinding(ctx, e.q, e.claimID, f); !errors.Is(err, validation.ErrNoEvidence) {
		t.Fatalf("temuan tanpa evidence harus ditolak, err = %v", err)
	}
	f.Evidence = []validation.Evidence{{Source: validation.SourceDocument, DocumentID: 1}} // tanpa halaman/field
	if _, err := validation.InsertFinding(ctx, e.q, e.claimID, f); !errors.Is(err, validation.ErrInvalidEvidence) {
		t.Fatalf("evidence tidak lengkap harus ditolak, err = %v", err)
	}
	// Pertahanan berlapis di database.
	for _, ev := range []string{`[]`, `{}`, `[{"source":"document","field":"nik","value":"0000010101900001"}]`} {
		_, err := e.pool.Exec(ctx, `INSERT INTO validation_findings (claim_id, rule_id, rule_version, finding_code, severity, message, evidence_json, fingerprint)
			VALUES ($1, $2, 1, 'MISSING_FIELD', 'low', 'x', $3::jsonb, 'raw')`, e.claimID, rules[1].ID, ev)
		if err == nil {
			t.Errorf("database harus menolak evidence %s", ev)
		}
	}
	if n := len(e.findings()); n != 0 {
		t.Fatalf("tidak boleh ada temuan tersimpan, ada %d", n)
	}
}

func TestCrossOfficerAccessDenied(t *testing.T) {
	e := newEnv(t, "submitted")
	e.addDoc("formulir_klaim", map[string]string{"claimant_name": "Rani Kusuma Dewi"}, 100, "pdftotext")
	e.run()
	ctx := context.Background()
	if _, err := e.svc.Findings(ctx, e.other, e.claimID); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("petugas lain melihat temuan: %v", err)
	}
	if err := e.svc.RequestRun(ctx, e.other, e.claimID); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("petugas lain menjalankan validasi: %v", err)
	}
	if len(e.queue.calls) != 0 {
		t.Fatal("job tidak boleh dimasukkan untuk akses yang ditolak")
	}
	if e.auditCount(audit.ActionAccessDenied) < 2 {
		t.Fatal("penolakan akses harus tercatat di audit")
	}
	// Pemilik dan supervisor diizinkan.
	if _, err := e.svc.Findings(ctx, e.officer, e.claimID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RequestRun(ctx, e.sup, e.claimID); err != nil || len(e.queue.calls) != 1 {
		t.Fatalf("supervisor: %v", err)
	}
	// Admin tidak memiliki akses data klaim.
	if _, err := e.svc.Findings(ctx, claims.Actor{UserID: e.sup.UserID, Role: authz.RoleAdmin}, e.claimID); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("admin melihat temuan: %v", err)
	}
}

func TestAuditAndLogsHaveNoPII(t *testing.T) {
	e := newEnv(t, "submitted")
	form, _, _, _ := e.fullSet()
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `UPDATE extracted_fields SET field_value = 'Rani Kusumawati'
		WHERE document_id = $1 AND field_key = 'claimant_name'`, form); err != nil {
		t.Fatal(err)
	}
	e.run()
	rows, err := e.pool.Query(ctx, `SELECT coalesce(before_json::text, ''), coalesce(after_json::text, '') FROM audit_logs
		WHERE action IN ('validation_run_started', 'validation_run_completed', 'finding_created', 'finding_resolved', 'status_changed')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var b, a string
		if err := rows.Scan(&b, &a); err != nil {
			t.Fatal(err)
		}
		n++
		for _, bad := range []string{"Rani", "Kusuma", "POL-DEMO", "0000", "teks contoh", "evidence", "excerpt"} {
			if strings.Contains(b+a, bad) {
				t.Errorf("audit memuat %q: %s", bad, a)
			}
		}
	}
	if n < 4 {
		t.Fatalf("audit terlalu sedikit: %d", n)
	}
}
