package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/auth"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
)

func (e *testEnv) loggedIn(role authz.Role) *http.Client {
	e.t.Helper()
	c := e.client()
	if resp, _ := e.login(c, e.users[role].Email, testPassword); resp.StatusCode != http.StatusSeeOther {
		e.t.Fatalf("login %s gagal", role)
	}
	return c
}

// secondOfficer membuat Claims Officer kedua (data sintetis).
func (e *testEnv) secondOfficer() db.User {
	e.t.Helper()
	u, err := e.q.UpsertUser(context.Background(), db.UpsertUserParams{
		Name: "Uji Petugas Lain Fiktif", Email: "officer2.test@example.com", PasswordHash: "x",
		Role: string(authz.RoleClaimsOfficer),
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return u
}

func (e *testEnv) seedClaim(assignee int64, policy string) db.Claim {
	e.t.Helper()
	sup := claims.Actor{UserID: e.users[authz.RoleSupervisor].ID, Role: authz.RoleSupervisor}
	c, err := e.srv.claims.Create(context.Background(), sup, claims.Input{
		ClaimantName: "Nama Fiktif Uji", PolicyNumber: policy, ProductType: claims.ProductHealthSim,
		ReceivedAt: "2026-11-01", AssignedTo: strconv.FormatInt(assignee, 10),
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return c
}

// rowLink adalah tautan baris klaim di daftar (lebih spesifik daripada
// nomor klaim, yang juga muncul di placeholder pencarian).
func rowLink(c db.Claim) string { return fmt.Sprintf(`href="/claims/%d"`, c.ID) }

func validClaimForm(token string) url.Values {
	return url.Values{
		"csrf_token":    {token},
		"claimant_name": {"Nama Fiktif Uji"},
		"policy_number": {"POL-DEMO-0001"},
		"product_type":  {claims.ProductHealthSim},
		"received_at":   {"2026-11-01"},
	}
}

func TestClaimCreateEditAndTransitionFlow(t *testing.T) {
	e := newEnv(t, nil)
	c := e.loggedIn(authz.RoleClaimsOfficer)

	resp, body := e.get(c, "/claims/new")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Buat klaim") {
		t.Fatalf("GET /claims/new = %d", resp.StatusCode)
	}
	token := csrfRe.FindStringSubmatch(body)[1]

	// Tanpa CSRF ditolak.
	f := validClaimForm("")
	if resp, _ := e.post(c, "/claims", f); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("create tanpa CSRF = %d", resp.StatusCode)
	}

	// Validasi server-side.
	bad := validClaimForm(token)
	bad.Set("policy_number", "1234567890")
	bad.Set("received_at", "2999-01-01")
	resp, body = e.post(c, "/claims", bad)
	if resp.StatusCode != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "POL-DEMO-0000") || !strings.Contains(body, "masa depan") {
		t.Fatalf("create tidak valid = %d", resp.StatusCode)
	}

	// Officer mencoba menugaskan ke supervisor: diabaikan, tetap ke dirinya.
	good := validClaimForm(token)
	good.Set("assigned_to", strconv.FormatInt(e.users[authz.RoleSupervisor].ID, 10))
	resp, _ = e.post(c, "/claims", good)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/claims/1" {
		t.Fatalf("create = %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, body = e.get(c, "/claims/1")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "CLM-DEMO-0001") ||
		!strings.Contains(body, "berhasil dibuat") || !strings.Contains(body, "Klaim dibuat (Draft)") ||
		!strings.Contains(body, e.users[authz.RoleClaimsOfficer].Name) {
		t.Fatalf("detail setelah create = %d", resp.StatusCode)
	}

	// Ubah data.
	if resp, _ := e.get(c, "/claims/1/edit"); resp.StatusCode != http.StatusOK {
		t.Fatalf("GET edit = %d", resp.StatusCode)
	}
	upd := validClaimForm(token)
	upd.Set("policy_number", "POL-DEMO-0002")
	if resp, _ := e.post(c, "/claims/1", upd); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update = %d", resp.StatusCode)
	}
	if n := len(e.auditRows(audit.ActionClaimUpdated)); n != 1 {
		t.Fatalf("audit claim_updated = %d", n)
	}

	status := func(to, reason string) (*http.Response, string) {
		return e.post(c, "/claims/1/status", url.Values{"csrf_token": {token}, "to": {to}, "reason": {reason}})
	}
	if resp, body := status("reviewed", ""); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "tidak diizinkan") {
		t.Fatalf("draft->reviewed = %d", resp.StatusCode)
	}
	if resp, _ := status("submitted", ""); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("draft->submitted = %d", resp.StatusCode)
	}
	if resp, body := status("processing", ""); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "hanya dapat diubah oleh sistem") {
		t.Fatalf("user set processing = %d", resp.StatusCode)
	}
	if resp, body := status("need_documents", " "); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Alasan wajib") {
		t.Fatalf("need_documents tanpa alasan = %d", resp.StatusCode)
	}
	if resp, _ := status("need_documents", "Surat keterangan simulasi belum ada"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("need_documents = %d", resp.StatusCode)
	}
	_, body = e.get(c, "/claims/1")
	for _, want := range []string{"Perlu dokumen", "Surat keterangan simulasi belum ada", "Diajukan → Perlu dokumen"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail tidak memuat %q", want)
		}
	}
	if strings.Contains(strings.ToLower(body), "ditolak") {
		t.Error("UI tidak boleh memakai istilah 'ditolak'")
	}
	if n := len(e.auditRows(audit.ActionStatusChanged)); n != 2 {
		t.Fatalf("audit status_changed = %d", n)
	}
}

// TC-07: petugas mencoba membuka klaim tanpa izin.
func TestOfficerCannotAccessOthersClaim(t *testing.T) {
	e := newEnv(t, nil)
	other := e.secondOfficer()
	own := e.seedClaim(e.users[authz.RoleClaimsOfficer].ID, "POL-DEMO-0001")
	foreign := e.seedClaim(other.ID, "POL-DEMO-0002")
	c := e.loggedIn(authz.RoleClaimsOfficer)
	token := e.csrfToken(c, "/claims")

	base := fmt.Sprintf("/claims/%d", foreign.ID)
	checks := []func() *http.Response{
		func() *http.Response { r, _ := e.get(c, base); return r },
		func() *http.Response { r, _ := e.get(c, base+"/edit"); return r },
		func() *http.Response { r, _ := e.post(c, base, validClaimForm(token)); return r },
		func() *http.Response {
			r, _ := e.post(c, base+"/status", url.Values{"csrf_token": {token}, "to": {"submitted"}})
			return r
		},
	}
	for i, chk := range checks {
		if resp := chk(); resp.StatusCode != http.StatusForbidden {
			t.Errorf("akses #%d ke klaim orang lain = %d, want 403", i, resp.StatusCode)
		}
	}
	rows := e.auditRows(audit.ActionAccessDenied)
	if len(rows) != len(checks) {
		t.Fatalf("access_denied = %d, want %d", len(rows), len(checks))
	}
	for _, r := range rows {
		m := afterMap(t, r)
		if r.EntityType != audit.EntityClaim || *r.EntityID != foreign.ID || *r.UserID != e.users[authz.RoleClaimsOfficer].ID || m["layer"] != "middleware" {
			t.Fatalf("access_denied tidak sesuai: %+v %v", r, m)
		}
	}
	got, _ := e.q.GetClaim(context.Background(), foreign.ID)
	if got.Status != "draft" || got.PolicyNumber != "POL-DEMO-0002" {
		t.Fatal("klaim orang lain tidak boleh berubah")
	}

	// Daftar hanya memuat klaim sendiri, walaupun filter petugas dipaksa.
	_, body := e.get(c, fmt.Sprintf("/claims?assigned_to=%d", other.ID))
	if !strings.Contains(body, rowLink(own)) || strings.Contains(body, rowLink(foreign)) {
		t.Fatal("officer hanya boleh melihat klaim yang ditugaskan kepadanya")
	}

	// Klaim tidak ada / ID tidak valid: 404.
	for _, p := range []string{"/claims/99999", "/claims/abc", "/claims/0"} {
		if resp, _ := e.get(c, p); resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, resp.StatusCode)
		}
	}
}

func TestSupervisorSeesAllAdminSeesNone(t *testing.T) {
	e := newEnv(t, nil)
	other := e.secondOfficer()
	c1 := e.seedClaim(e.users[authz.RoleClaimsOfficer].ID, "POL-DEMO-0001")
	c2 := e.seedClaim(other.ID, "POL-DEMO-0002")

	sup := e.loggedIn(authz.RoleSupervisor)
	_, body := e.get(sup, "/claims")
	if !strings.Contains(body, rowLink(c1)) || !strings.Contains(body, rowLink(c2)) || !strings.Contains(body, "Semua petugas") {
		t.Fatal("supervisor harus melihat seluruh klaim dan filter petugas")
	}
	_, body = e.get(sup, fmt.Sprintf("/claims?assigned_to=%d", other.ID))
	if strings.Contains(body, rowLink(c1)) || !strings.Contains(body, rowLink(c2)) {
		t.Fatal("filter petugas supervisor tidak bekerja")
	}
	for _, p := range []string{fmt.Sprintf("/claims/%d", c2.ID), fmt.Sprintf("/claims/%d/edit", c2.ID)} {
		if resp, _ := e.get(sup, p); resp.StatusCode != http.StatusOK {
			t.Errorf("supervisor GET %s = %d", p, resp.StatusCode)
		}
	}

	admin := e.loggedIn(authz.RoleAdmin)
	token := e.csrfToken(admin, "/dashboard")
	for _, p := range []string{"/claims", "/claims/new", fmt.Sprintf("/claims/%d", c1.ID)} {
		if resp, _ := e.get(admin, p); resp.StatusCode != http.StatusForbidden {
			t.Errorf("admin GET %s = %d, want 403", p, resp.StatusCode)
		}
	}
	if resp, _ := e.post(admin, "/claims", validClaimForm(token)); resp.StatusCode != http.StatusForbidden {
		t.Errorf("admin POST /claims = %d, want 403", resp.StatusCode)
	}
	if n := len(e.auditRows(audit.ActionAccessDenied)); n != 4 {
		t.Fatalf("access_denied admin = %d, want 4", n)
	}
	var count int
	_ = e.pool.QueryRow(context.Background(), "SELECT count(*) FROM claims").Scan(&count)
	if count != 2 {
		t.Fatal("admin tidak boleh membuat klaim")
	}
}

// Lapis kedua: handler/service tetap menolak walaupun middleware
// requireClaimAccess terlewat.
func TestClaimHandlerEnforcesAccessWithoutMiddleware(t *testing.T) {
	e := newEnv(t, nil)
	other := e.secondOfficer()
	foreign := e.seedClaim(other.ID, "POL-DEMO-0002")
	officer := auth.FromRow(e.users[authz.RoleClaimsOfficer])

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/claims/%d", foreign.ID), nil)
	ctx := context.WithValue(req.Context(), ctxUser, &officer)
	ctx = context.WithValue(ctx, ctxClaimID, foreign.ID)
	rctx := chi.NewRouteContext()
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	rec := httptest.NewRecorder()
	e.srv.claimDetail(rec, req.WithContext(ctx))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("handler tanpa middleware = %d, want 403", rec.Code)
	}
	rows := e.auditRows(audit.ActionAccessDenied)
	if len(rows) != 1 || afterMap(t, rows[0])["layer"] != "service" {
		t.Fatalf("access_denied lapis service tidak tercatat: %+v", rows)
	}
}

func TestClaimListFiltersOverHTTP(t *testing.T) {
	e := newEnv(t, nil)
	off := e.users[authz.RoleClaimsOfficer].ID
	for i := 1; i <= 23; i++ {
		e.seedClaim(off, fmt.Sprintf("POL-DEMO-%04d", i))
	}
	sup := e.loggedIn(authz.RoleSupervisor)

	_, body := e.get(sup, "/claims")
	if !strings.Contains(body, "Menampilkan 1–20 dari 23 klaim") || !strings.Contains(body, `href="/claims?page=2"`) {
		t.Fatal("pagination halaman 1 tidak sesuai")
	}
	_, body = e.get(sup, "/claims?page=2")
	if !strings.Contains(body, "Menampilkan 21–23 dari 23 klaim") {
		t.Fatal("pagination halaman 2 tidak sesuai")
	}
	_, body = e.get(sup, "/claims?q=pol-demo-0007")
	if !strings.Contains(body, "dari 1 klaim") || !strings.Contains(body, "POL-DEMO-0007") {
		t.Fatal("pencarian nomor polis tidak sesuai")
	}
	// Filter dipertahankan pada tautan halaman.
	_, body = e.get(sup, "/claims?status=draft")
	if !strings.Contains(body, `href="/claims?page=2&amp;status=draft"`) {
		t.Fatal("tautan pagination harus mempertahankan filter")
	}
	for _, p := range []string{
		"/claims?status=reviewed",
		"/claims?q=" + url.QueryEscape("' OR 1=1 --"),
		"/claims?from=2026-12-01",
	} {
		resp, body := e.get(sup, p)
		if resp.StatusCode != http.StatusOK || !strings.Contains(body, "dari 0 klaim") {
			t.Errorf("GET %s harus 200 tanpa hasil", p)
		}
	}
	// Tanggal tidak valid diabaikan, bukan error.
	if resp, body := e.get(sup, "/claims?from=bukan-tanggal&page=-5"); resp.StatusCode != http.StatusOK || !strings.Contains(body, "dari 23 klaim") {
		t.Fatal("filter tidak valid harus diabaikan")
	}
	// Request HTMX dilayani (hx-select mengambil #claims-results).
	if resp, body := e.get(sup, "/claims?status=draft", "HX-Request", "true"); resp.StatusCode != http.StatusOK || !strings.Contains(body, `id="claims-results"`) {
		t.Fatal("request HTMX gagal")
	}
}
