package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/documents"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/testutil"
)

// seedProcessed mengunggah formulir klaim lalu menyimulasikan hasil worker
// (status completed + field terekstrak sintetis). Mengembalikan ID dokumen
// dan ID field per field_key.
func (e *testEnv) seedProcessed(claimID int64, fields map[string][3]string) (int64, map[string]int64) {
	e.t.Helper()
	ctx := context.Background()
	sup := claims.Actor{UserID: e.users[authz.RoleSupervisor].ID, Role: authz.RoleSupervisor}
	docs, err := e.srv.docs.Upload(ctx, sup, documents.Upload{
		ClaimID: claimID, Type: documents.TypeClaimForm, ConfirmDuplicate: true,
		Files: []documents.File{{Name: "formulir.pdf", Data: testutil.PDF(1)}},
	})
	if err != nil {
		e.t.Fatal(err)
	}
	d := docs[0]
	if _, err := e.pool.Exec(ctx, `UPDATE claim_documents SET processing_status = 'processing' WHERE id = $1`, d.ID); err != nil {
		e.t.Fatal(err)
	}
	if err := e.q.MarkDocumentCompleted(ctx, d.ID); err != nil {
		e.t.Fatal(err)
	}
	ids := map[string]int64{}
	for key, f := range fields { // f = {value, status, confidence}
		var v *string
		if f[0] != "" {
			v = &f[0]
		}
		var conf float64
		fmt.Sscanf(f[2], "%g", &conf)
		page := int32(1)
		ex := "label: " + f[0]
		if err := e.q.InsertExtractedField(ctx, db.InsertExtractedFieldParams{
			DocumentID: d.ID, FieldKey: key, FieldValue: v, Status: f[1], Confidence: &conf,
			PageNumber: &page, SourceExcerpt: &ex, ExtractorVersion: "test",
		}); err != nil {
			e.t.Fatal(err)
		}
	}
	rows, _ := e.q.ListExtractedFields(ctx, d.ID)
	for _, r := range rows {
		ids[r.FieldKey] = r.ID
	}
	return d.ID, ids
}

func TestDocumentResultPage(t *testing.T) {
	e := newEnv(t, nil)
	c, cl, _ := e.officerClaim()
	docID, _ := e.seedProcessed(cl.ID, map[string][3]string{
		"claimant_name": {"Nama Fiktif Uji", "found", "97"},
		"policy_number": {"POL-DEMO-0001", "found", "72"},
		"nik":           {"", "uncertain", "40"},
	})
	resp, body := e.get(c, fmt.Sprintf("/claims/%d/documents/%d/hasil", cl.ID, docID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("halaman hasil harus 200, dapat %d", resp.StatusCode)
	}
	for _, want := range []string{
		fmt.Sprintf(`src="/claims/%d/documents/%d?embed=1"`, cl.ID, docID), // pratinjau di kiri
		"Field terekstrak", "Nama Fiktif Uji", "Halaman 1", "label: Nama Fiktif Uji",
		">tinggi<", ">perlu verifikasi<", ">rendah<", "Tidak pasti — verifikasi manual",
		"bukan keputusan klaim", "Proses ulang",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("halaman hasil harus memuat %q", want)
		}
	}
	if strings.Contains(body, `hx-trigger="every`) {
		t.Error("dokumen selesai tidak perlu polling")
	}
	rows := e.auditRows(audit.ActionDocumentViewed)
	if len(rows) != 1 || afterAny(t, rows[0])["mode"] != "extraction" {
		t.Fatal("akses hasil ekstraksi harus dicatat document_viewed mode extraction")
	}
}

func TestDocumentStatusPolling(t *testing.T) {
	e := newEnv(t, nil)
	c, cl, token := e.officerClaim()
	e.upload(c, cl.ID, token, "identitas", []upFile{{"id.png", testutil.PNG(1)}}, nil)
	doc := e.docs(cl.ID)[0]
	if doc.ProcessingStatus != documents.StatusQueued {
		t.Fatalf("status awal harus queued, dapat %q", doc.ProcessingStatus)
	}
	_, body := e.get(c, fmt.Sprintf("/claims/%d/documents/%d/hasil", cl.ID, doc.ID))
	if !strings.Contains(body, `hx-trigger="every 3s"`) || !strings.Contains(body, "Dalam antrean") {
		t.Fatal("dokumen queued harus menampilkan status dan polling")
	}
	statusURL := fmt.Sprintf("/claims/%d/documents/%d/status?shown=queued", cl.ID, doc.ID)
	resp, frag := e.get(c, statusURL)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("HX-Refresh") != "" || !strings.Contains(frag, `id="processing-status"`) {
		t.Fatalf("fragmen status queued tidak sesuai (%d)", resp.StatusCode)
	}
	_, _ = e.pool.Exec(context.Background(),
		`UPDATE claim_documents SET processing_status = 'failed', processing_error_code = 'OCR_TIMEOUT' WHERE id = $1`, doc.ID)
	resp, frag = e.get(c, statusURL)
	if resp.Header.Get("HX-Refresh") != "true" || !strings.Contains(frag, "OCR melebihi batas waktu") {
		t.Fatal("status final harus memicu muat ulang dan menampilkan pesan error")
	}
	// Polling tidak dicatat sebagai akses dokumen.
	if n := len(e.auditRows(audit.ActionDocumentViewed)); n != 1 {
		t.Fatalf("hanya halaman hasil yang dicatat, dapat %d", n)
	}
	// Detail klaim menampilkan kegagalan dan tombol proses ulang.
	_, body = e.get(c, fmt.Sprintf("/claims/%d", cl.ID))
	if !strings.Contains(body, "OCR melebihi batas waktu") || !strings.Contains(body, fmt.Sprintf(`/documents/%d/process"`, doc.ID)) {
		t.Fatal("detail klaim harus menampilkan error dan tombol Proses ulang")
	}
}

func TestReprocessHTTP(t *testing.T) {
	e := newEnv(t, nil)
	c, cl, token := e.officerClaim()
	e.upload(c, cl.ID, token, "identitas", []upFile{{"id.png", testutil.PNG(1)}}, nil)
	doc := e.docs(cl.ID)[0]
	path := fmt.Sprintf("/claims/%d/documents/%d/process", cl.ID, doc.ID)

	// Masih queued: tidak ada job ganda.
	resp, _ := e.post(c, path, url.Values{"csrf_token": {token}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("harus 303, dapat %d", resp.StatusCode)
	}
	_, body := e.get(c, resp.Header.Get("Location"))
	if !strings.Contains(body, "tidak dibuat antrean ganda") {
		t.Fatal("pesan antrean ganda harus tampil")
	}
	jobs := func() int {
		var n int
		_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM river_job WHERE (args->>'document_id')::bigint = $1`, doc.ID).Scan(&n)
		return n
	}
	if jobs() != 1 {
		t.Fatalf("harus tetap 1 job, dapat %d", jobs())
	}

	_, _ = e.pool.Exec(context.Background(), `UPDATE claim_documents SET processing_status = 'failed', processing_error_code = 'OCR_ENGINE_ERROR' WHERE id = $1`, doc.ID)
	resp, _ = e.post(c, path, url.Values{"csrf_token": {token}, "back": {"claim"}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != fmt.Sprintf("/claims/%d#documents", cl.ID) {
		t.Fatalf("proses ulang harus 303 ke detail klaim, dapat %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	d, _ := e.q.GetClaimDocument(context.Background(), db.GetClaimDocumentParams{ID: doc.ID, ClaimID: cl.ID})
	if d.ProcessingStatus != documents.StatusQueued || d.ProcessingErrorCode != nil || jobs() != 2 {
		t.Fatalf("setelah proses ulang: %q %v, %d job", d.ProcessingStatus, d.ProcessingErrorCode, jobs())
	}
	rows := e.auditRows(audit.ActionDocumentProcessingQueued)
	if len(rows) != 2 || afterAny(t, rows[0])["trigger"] != "reprocess" {
		t.Fatal("audit proses ulang harus tercatat")
	}
}

// Akses lintas petugas ditolak untuk hasil OCR, status, proses ulang, dan
// penerapan nilai terdeteksi (lapis middleware; service diuji di
// internal/processing).
func TestExtractionCrossOfficerDenied(t *testing.T) {
	e := newEnv(t, nil)
	other := e.secondOfficer()
	foreign := e.seedClaim(other.ID, "POL-DEMO-0009")
	docID, ids := e.seedProcessed(foreign.ID, map[string][3]string{"claimant_name": {"Nama Lain Fiktif", "found", "95"}})

	c := e.loggedIn(authz.RoleClaimsOfficer)
	token := e.csrfToken(c, "/claims")
	base := fmt.Sprintf("/claims/%d", foreign.ID)
	for _, p := range []string{fmt.Sprintf("%s/documents/%d/hasil", base, docID), fmt.Sprintf("%s/documents/%d/status", base, docID)} {
		if resp, body := e.get(c, p); resp.StatusCode != http.StatusForbidden || strings.Contains(body, "Nama Lain Fiktif") {
			t.Errorf("GET %s harus 403 tanpa isi, dapat %d", p, resp.StatusCode)
		}
	}
	for _, p := range []string{fmt.Sprintf("%s/documents/%d/process", base, docID), fmt.Sprintf("%s/detected/%d/apply", base, ids["claimant_name"])} {
		if resp, _ := e.post(c, p, url.Values{"csrf_token": {token}}); resp.StatusCode != http.StatusForbidden {
			t.Errorf("POST %s harus 403, dapat %d", p, resp.StatusCode)
		}
	}
	cl, _ := e.q.GetClaim(context.Background(), foreign.ID)
	if cl.ClaimantName != foreign.ClaimantName {
		t.Fatal("data klaim tidak boleh berubah")
	}
	if n := len(e.auditRows(audit.ActionAccessDenied)); n != 4 {
		t.Fatalf("setiap penolakan harus dicatat, dapat %d", n)
	}
	// Lapis service menolak penerapan nilai walau middleware dilewati.
	officer := claims.Actor{UserID: e.users[authz.RoleClaimsOfficer].ID, Role: authz.RoleClaimsOfficer}
	if _, _, err := e.srv.claims.ApplyDetectedValue(context.Background(), officer, foreign.ID, ids["claimant_name"]); err == nil {
		t.Fatal("service harus menolak")
	}
	// Admin tidak punya akses dokumen sama sekali.
	ac := e.loggedIn(authz.RoleAdmin)
	if resp, _ := e.get(ac, fmt.Sprintf("%s/documents/%d/hasil", base, docID)); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin harus 403, dapat %d", resp.StatusCode)
	}
}

func TestApplyDetectedValue(t *testing.T) {
	e := newEnv(t, nil)
	c, cl, token := e.officerClaim() // data klaim: "Nama Fiktif Uji", POL-DEMO-0001
	_, ids := e.seedProcessed(cl.ID, map[string][3]string{
		"claimant_name": {"Nama Terdeteksi Fiktif", "found", "96"},
		"policy_number": {"POL-DEMO-0002", "uncertain", "45"},
		"nik":           {"0000********0001", "found", "95"},
	})
	_, body := e.get(c, fmt.Sprintf("/claims/%d", cl.ID))
	for _, want := range []string{"Data terdeteksi vs data klaim", "Nama Terdeteksi Fiktif", "Berbeda — perlu ditinjau",
		"Tidak dapat dibandingkan — verifikasi manual", fmt.Sprintf("/detected/%d/apply", ids["claimant_name"])} {
		if !strings.Contains(body, want) {
			t.Errorf("detail klaim harus memuat %q", want)
		}
	}
	if strings.Contains(body, fmt.Sprintf("/detected/%d/apply", ids["policy_number"])) {
		t.Error("nilai uncertain tidak boleh punya tombol Terapkan")
	}
	// Tidak ada perubahan otomatis sebelum petugas menekan Terapkan.
	if got, _ := e.q.GetClaim(context.Background(), cl.ID); got.ClaimantName != "Nama Fiktif Uji" {
		t.Fatal("data klaim berubah tanpa konfirmasi")
	}

	resp, _ := e.post(c, fmt.Sprintf("/claims/%d/detected/%d/apply", cl.ID, ids["claimant_name"]), url.Values{"csrf_token": {token}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("terapkan harus 303, dapat %d", resp.StatusCode)
	}
	got, _ := e.q.GetClaim(context.Background(), cl.ID)
	if got.ClaimantName != "Nama Terdeteksi Fiktif" || got.PolicyNumber != "POL-DEMO-0001" || got.Status != cl.Status {
		t.Fatalf("hasil terapkan salah: %q %q %q", got.ClaimantName, got.PolicyNumber, got.Status)
	}
	rows := e.auditRows(audit.ActionExtractedValueApplied)
	if len(rows) != 1 || rows[0].UserID == nil || *rows[0].UserID != e.users[authz.RoleClaimsOfficer].ID {
		t.Fatal("extracted_value_applied harus tercatat dengan user")
	}
	if a := string(rows[0].AfterJson) + string(rows[0].BeforeJson); strings.Contains(a, "Terdeteksi") || strings.Contains(a, "Nama Fiktif") ||
		!strings.Contains(a, claims.Masked) {
		t.Fatalf("audit harus menyamarkan nilai: %s", a)
	}
	_, body = e.get(c, fmt.Sprintf("/claims/%d", cl.ID))
	if !strings.Contains(body, "diperbarui dari nilai terdeteksi") || !strings.Contains(body, ">Sama<") {
		t.Fatal("setelah diterapkan, perbandingan harus menunjukkan Sama")
	}

	// Nilai sama → tidak ada perubahan/audit baru.
	e.post(c, fmt.Sprintf("/claims/%d/detected/%d/apply", cl.ID, ids["claimant_name"]), url.Values{"csrf_token": {token}})
	if len(e.auditRows(audit.ActionExtractedValueApplied)) != 1 {
		t.Fatal("penerapan ulang nilai sama tidak boleh dicatat lagi")
	}
	// Nilai uncertain dan field selain nama/polis ditolak di server.
	for _, k := range []string{"policy_number", "nik"} {
		resp, _ := e.post(c, fmt.Sprintf("/claims/%d/detected/%d/apply", cl.ID, ids[k]), url.Values{"csrf_token": {token}})
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("terapkan %s harus 422, dapat %d", k, resp.StatusCode)
		}
	}
	if got, _ := e.q.GetClaim(context.Background(), cl.ID); got.PolicyNumber != "POL-DEMO-0001" {
		t.Fatal("nilai uncertain tidak boleh diterapkan")
	}
	// Field milik klaim lain tidak dapat dipakai lewat ID.
	other := e.seedClaim(e.users[authz.RoleClaimsOfficer].ID, "POL-DEMO-0003")
	if resp, _ := e.post(c, fmt.Sprintf("/claims/%d/detected/%d/apply", other.ID, ids["claimant_name"]), url.Values{"csrf_token": {token}}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("field klaim lain harus 404, dapat %d", resp.StatusCode)
	}
}

func TestEmbedPreviewHeaders(t *testing.T) {
	e := newEnv(t, nil)
	c, cl, token := e.officerClaim()
	e.upload(c, cl.ID, token, "identitas", []upFile{{"id.png", testutil.PNG(1)}}, nil)
	doc := e.docs(cl.ID)[0]
	resp, _ := e.get(c, fmt.Sprintf("/claims/%d/documents/%d?embed=1", cl.ID, doc.ID))
	if resp.Header.Get("X-Frame-Options") != "SAMEORIGIN" || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'self'") {
		t.Fatalf("embed harus mengizinkan frame same-origin: %q", resp.Header.Get("Content-Security-Policy"))
	}
	resp, _ = e.get(c, fmt.Sprintf("/claims/%d/documents/%d", cl.ID, doc.ID))
	if resp.Header.Get("X-Frame-Options") != "DENY" || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("tanpa embed harus menolak framing")
	}
}

func afterAny(t *testing.T, r db.AuditLog) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal(r.AfterJson, &m); err != nil {
		t.Fatalf("after_json tidak valid: %v", err)
	}
	return m
}
