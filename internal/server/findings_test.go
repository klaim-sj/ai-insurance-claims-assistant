package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/validation"
)

func (e *testEnv) validationJobs(claimID int64) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM river_job WHERE kind = 'validate_claim' AND (args->>'claim_id')::bigint = $1`, claimID).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func section(body, id string) string {
	i := strings.Index(body, `id="`+id+`"`)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	if j := strings.Index(rest, "</section>"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func TestFindingsPanel(t *testing.T) {
	e := newEnv(t, nil)
	c, cl, token := e.officerClaim()
	ctx := context.Background()
	off := claims.Actor{UserID: e.users[authz.RoleClaimsOfficer].ID, Role: authz.RoleClaimsOfficer}
	if _, err := e.srv.claims.TransitionStatus(ctx, off, cl.ID, claims.StatusSubmitted, ""); err != nil {
		t.Fatal(err)
	}
	// Data sintetis: nama & polis pada formulir berbeda dengan data klaim.
	docID, _ := e.seedProcessed(cl.ID, map[string][3]string{
		"claimant_name": {"Nama Fiktif Lain", "found", "97"},
		"policy_number": {"POL-DEMO-0009", "found", "97"},
		"event_date":    {"2026-10-10", "found", "97"},
	})
	if _, err := e.srv.val.Run(ctx, cl.ID, validation.TriggerManual); err != nil {
		t.Fatal(err)
	}

	resp, body := e.get(c, fmt.Sprintf("/claims/%d", cl.ID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail = %d", resp.StatusCode)
	}
	panel := section(body, "findings")
	for _, want := range []string{
		"Temuan", "perlu ditinjau", "bukan keputusan klaim",
		"Perbedaan nama", "Perbedaan nomor polis", "Dokumen wajib belum ada",
		"Nama Fiktif Lain", "Nama Fiktif Uji", "POL-DEMO-0009", "POL-DEMO-0001", // bukti berdampingan
		fmt.Sprintf("/claims/%d/documents/%d/hasil?hal=1#hal-1", cl.ID, docID),
		"Saran tindak lanjut", "tidak</strong> diubah otomatis",
		fmt.Sprintf("/claims/%d?to=need_documents#transition-heading", cl.ID),
		"Jalankan ulang validasi", "Severity: Tinggi",
	} {
		if !strings.Contains(panel, want) {
			t.Errorf("panel temuan tidak memuat %q", want)
		}
	}
	low := strings.ToLower(panel)
	for _, bad := range []string{"ditolak", "tidak layak", "fraud"} {
		if strings.Contains(low, bad) {
			t.Errorf("panel temuan memakai istilah %q", bad)
		}
	}
	// Validasi otomatis memindahkan ke needs_review, bukan need_documents.
	got, _ := e.q.GetClaim(ctx, cl.ID)
	if got.Status != string(claims.StatusNeedsReview) {
		t.Fatalf("status %s, harap needs_review", got.Status)
	}

	// Saran need_documents hanya mengisi pilihan form; status tidak berubah.
	_, body = e.get(c, fmt.Sprintf("/claims/%d?to=need_documents", cl.ID))
	if !strings.Contains(body, `value="need_documents" required checked`) {
		t.Fatal("pilihan Perlu dokumen harus terisi dari saran")
	}
	got, _ = e.q.GetClaim(ctx, cl.ID)
	if got.Status != string(claims.StatusNeedsReview) {
		t.Fatal("membuka saran tidak boleh mengubah status")
	}

	// Jalankan ulang validasi: job dimasukkan dan tercatat di audit.
	resp, _ = e.post(c, fmt.Sprintf("/claims/%d/validation", cl.ID), url.Values{"csrf_token": {token}})
	if resp.StatusCode != http.StatusSeeOther || e.validationJobs(cl.ID) != 1 {
		t.Fatalf("jalankan ulang = %d, job %d", resp.StatusCode, e.validationJobs(cl.ID))
	}
	if len(e.auditRows(audit.ActionValidationRunRequested)) != 1 {
		t.Fatal("permintaan validasi harus tercatat di audit")
	}
	// Halaman dokumen dengan ?hal menyorot halaman.
	_, body = e.get(c, fmt.Sprintf("/claims/%d/documents/%d/hasil?hal=1", cl.ID, docID))
	if !strings.Contains(body, "?embed=1#page=1") {
		t.Fatal("pratinjau harus membuka halaman evidence")
	}
}

func TestFindingsCrossOfficerDenied(t *testing.T) {
	e := newEnv(t, nil)
	other := e.secondOfficer()
	foreign := e.seedClaim(other.ID, "POL-DEMO-0002")
	ctx := context.Background()
	if _, err := e.srv.claims.SystemTransition(ctx, foreign.ID, claims.StatusSubmitted, ""); err == nil {
		t.Fatal("draft → submitted bukan transisi sistem")
	}
	if _, err := e.pool.Exec(ctx, `UPDATE claims SET status = 'submitted' WHERE id = $1`, foreign.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.srv.val.Run(ctx, foreign.ID, validation.TriggerManual); err != nil {
		t.Fatal(err)
	}
	c := e.loggedIn(authz.RoleClaimsOfficer)
	token := e.csrfToken(c, "/claims")
	if resp, body := e.get(c, fmt.Sprintf("/claims/%d", foreign.ID)); resp.StatusCode != http.StatusForbidden || strings.Contains(body, `id="findings"`) {
		t.Fatalf("petugas lain melihat temuan: %d", resp.StatusCode)
	}
	if resp, _ := e.post(c, fmt.Sprintf("/claims/%d/validation", foreign.ID), url.Values{"csrf_token": {token}}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("petugas lain menjalankan validasi: %d", resp.StatusCode)
	}
	if e.validationJobs(foreign.ID) != 0 {
		t.Fatal("job tidak boleh dimasukkan")
	}
	// Admin tidak punya izin data klaim.
	a := e.loggedIn(authz.RoleAdmin)
	at := e.csrfToken(a, "/dashboard")
	if resp, _ := e.post(a, fmt.Sprintf("/claims/%d/validation", foreign.ID), url.Values{"csrf_token": {at}}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin menjalankan validasi: %d", resp.StatusCode)
	}
	// Supervisor boleh.
	s := e.loggedIn(authz.RoleSupervisor)
	st := e.csrfToken(s, "/claims")
	resp, body := e.get(s, fmt.Sprintf("/claims/%d", foreign.ID))
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `id="findings"`) {
		t.Fatalf("supervisor harus melihat temuan: %d", resp.StatusCode)
	}
	if resp, _ := e.post(s, fmt.Sprintf("/claims/%d/validation", foreign.ID), url.Values{"csrf_token": {st}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("supervisor menjalankan validasi: %d", resp.StatusCode)
	}
}
