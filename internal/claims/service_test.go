package claims_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/testutil"
)

// Waktu tetap agar test deterministik (data sintetis).
var fixedNow = time.Date(2026, 11, 20, 10, 0, 0, 0, claims.Location)

type env struct {
	t     *testing.T
	pool  *pgxpool.Pool
	q     *db.Queries
	svc   *claims.Service
	off1  claims.Actor
	off2  claims.Actor
	sup   claims.Actor
	admin claims.Actor
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testutil.DB(t)
	q := db.New(pool)
	mk := func(name, email string, role authz.Role) claims.Actor {
		u, err := q.UpsertUser(context.Background(), db.UpsertUserParams{
			Name: name, Email: email, PasswordHash: "x", Role: string(role),
		})
		if err != nil {
			t.Fatal(err)
		}
		return claims.Actor{UserID: u.ID, Role: role}
	}
	svc := claims.NewService(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Now = func() time.Time { return fixedNow }
	return &env{
		t: t, pool: pool, q: q, svc: svc,
		off1:  mk("Uji Petugas Satu", "petugas1.test@example.com", authz.RoleClaimsOfficer),
		off2:  mk("Uji Petugas Dua", "petugas2.test@example.com", authz.RoleClaimsOfficer),
		sup:   mk("Uji Supervisor", "supervisor.test@example.com", authz.RoleSupervisor),
		admin: mk("Uji Admin", "admin.test@example.com", authz.RoleAdmin),
	}
}

func input(policy, date string, assignee int64) claims.Input {
	return claims.Input{
		ClaimantName: "Nama Fiktif Uji",
		PolicyNumber: policy,
		ProductType:  claims.ProductHealthSim,
		ReceivedAt:   date,
		AssignedTo:   strconv.FormatInt(assignee, 10),
	}
}

func (e *env) create(a claims.Actor, assignee int64) db.Claim {
	e.t.Helper()
	c, err := e.svc.Create(context.Background(), a, input("POL-DEMO-0001", "2026-11-10", assignee))
	if err != nil {
		e.t.Fatalf("create: %v", err)
	}
	return c
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) auditRows(action string) []db.AuditLog {
	e.t.Helper()
	rows, err := e.q.ListAuditLogsByAction(context.Background(), db.ListAuditLogsByActionParams{Action: action, Limit: 100})
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func TestCreateAssignsNumberHistoryAndAudit(t *testing.T) {
	e := newEnv(t)
	c := e.create(e.off1, e.off2.UserID) // officer tidak bisa menugaskan orang lain

	if c.ClaimNumber != "CLM-DEMO-0001" || c.Status != "draft" {
		t.Fatalf("klaim = %s/%s", c.ClaimNumber, c.Status)
	}
	if c.AssignedTo == nil || *c.AssignedTo != e.off1.UserID {
		t.Fatal("klaim buatan officer harus ditugaskan ke dirinya sendiri")
	}
	if got := c.ReceivedAt.In(claims.Location).Format(claims.DateLayout); got != "2026-11-10" {
		t.Fatalf("received_at = %s", got)
	}
	if n := e.count("SELECT count(*) FROM claim_status_histories WHERE claim_id=$1 AND old_status IS NULL AND new_status='draft'", c.ID); n != 1 {
		t.Fatalf("riwayat pembuatan = %d", n)
	}
	rows := e.auditRows(audit.ActionClaimCreated)
	if len(rows) != 1 || rows[0].EntityType != audit.EntityClaim || *rows[0].EntityID != c.ID {
		t.Fatalf("audit claim_created tidak sesuai: %+v", rows)
	}
	js := string(rows[0].AfterJson)
	if strings.Contains(js, "Nama Fiktif Uji") || strings.Contains(js, "POL-DEMO") {
		t.Fatalf("audit tidak boleh memuat nama/polis: %s", js)
	}

	c2, err := e.svc.Create(context.Background(), e.sup, input("POL-DEMO-0002", "2026-11-11", e.off2.UserID))
	if err != nil || c2.ClaimNumber != "CLM-DEMO-0002" || *c2.AssignedTo != e.off2.UserID {
		t.Fatalf("supervisor create: %+v err=%v", c2, err)
	}
}

func TestCreateValidation(t *testing.T) {
	e := newEnv(t)
	good := input("POL-DEMO-0001", "2026-11-10", e.off1.UserID)
	cases := []struct {
		name  string
		mod   func(*claims.Input)
		field string
	}{
		{"nama kosong", func(i *claims.Input) { i.ClaimantName = "  " }, "claimant_name"},
		{"nama satu huruf", func(i *claims.Input) { i.ClaimantName = "A" }, "claimant_name"},
		{"nama berisi angka", func(i *claims.Input) { i.ClaimantName = "Nama 123" }, "claimant_name"},
		{"nama terlalu panjang", func(i *claims.Input) { i.ClaimantName = strings.Repeat("a", 101) }, "claimant_name"},
		{"polis kosong", func(i *claims.Input) { i.PolicyNumber = "" }, "policy_number"},
		{"polis format nyata", func(i *claims.Input) { i.PolicyNumber = "1234567890" }, "policy_number"},
		{"polis injeksi", func(i *claims.Input) { i.PolicyNumber = "POL-DEMO-0001' OR 1=1--" }, "policy_number"},
		{"produk di luar allowlist", func(i *claims.Input) { i.ProductType = "Jiwa" }, "product_type"},
		{"tanggal kosong", func(i *claims.Input) { i.ReceivedAt = "" }, "received_at"},
		{"tanggal salah format", func(i *claims.Input) { i.ReceivedAt = "10/11/2026" }, "received_at"},
		{"tanggal masa depan", func(i *claims.Input) { i.ReceivedAt = "2026-11-21" }, "received_at"},
		{"tanggal terlalu lama", func(i *claims.Input) { i.ReceivedAt = "1999-12-31" }, "received_at"},
		{"petugas kosong", func(i *claims.Input) { i.AssignedTo = "" }, "assigned_to"},
		{"petugas bukan angka", func(i *claims.Input) { i.AssignedTo = "abc" }, "assigned_to"},
		{"petugas adalah supervisor", func(i *claims.Input) { i.AssignedTo = strconv.FormatInt(e.sup.UserID, 10) }, "assigned_to"},
		{"petugas tidak ada", func(i *claims.Input) { i.AssignedTo = "999999" }, "assigned_to"},
	}
	for _, c := range cases {
		in := good
		c.mod(&in)
		_, err := e.svc.Create(context.Background(), e.sup, in)
		var ve *claims.ValidationError
		if !errors.As(err, &ve) || ve.Fields[c.field] == "" {
			t.Errorf("%s: harus ValidationError pada %s, dapat %v", c.name, c.field, err)
		}
	}
	if n := e.count("SELECT count(*) FROM claims"); n != 0 {
		t.Fatalf("tidak boleh ada klaim tersimpan, ada %d", n)
	}
	// Hari ini (WIB) diterima; nama & polis dinormalisasi.
	in := good
	in.ReceivedAt = "2026-11-20"
	in.ClaimantName = "  Nama   Fiktif  O'Uji-Coba "
	in.PolicyNumber = "pol-demo-123456"
	c, err := e.svc.Create(context.Background(), e.sup, in)
	if err != nil {
		t.Fatal(err)
	}
	if c.ClaimantName != "Nama Fiktif O'Uji-Coba" || c.PolicyNumber != "POL-DEMO-123456" {
		t.Fatalf("normalisasi: %q %q", c.ClaimantName, c.PolicyNumber)
	}
	// Nomor klaim tidak terlewat walaupun ada percobaan yang gagal validasi.
	if c.ClaimNumber != "CLM-DEMO-0001" {
		t.Fatalf("nomor = %s", c.ClaimNumber)
	}
}

func TestConcurrentCreateGeneratesUniqueNumbers(t *testing.T) {
	e := newEnv(t)
	const n = 20
	var wg sync.WaitGroup
	numbers := make([]string, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			c, err := e.svc.Create(context.Background(), e.off1,
				input(fmt.Sprintf("POL-DEMO-%04d", i), "2026-11-10", e.off1.UserID))
			numbers[i], errs[i] = c.ClaimNumber, err
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("create #%d: %v", i, err)
		}
	}
	sort.Strings(numbers)
	for i, num := range numbers {
		if want := fmt.Sprintf("CLM-DEMO-%04d", i+1); num != want {
			t.Fatalf("nomor ke-%d = %s, want %s (semua: %v)", i, num, want, numbers)
		}
	}
}

func TestValidTransitionsWriteHistoryAndAudit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.create(e.off1, e.off1.UserID)

	steps := []struct {
		system bool
		to     claims.Status
		reason string
	}{
		{false, claims.StatusSubmitted, ""},
		{true, claims.StatusProcessing, ""},
		{true, claims.StatusNeedsReview, "Hasil validasi otomatis (simulasi)"},
		{false, claims.StatusNeedDocuments, "Surat keterangan belum ada"},
		{false, claims.StatusSubmitted, ""},
		{false, claims.StatusReviewed, ""},
		{false, claims.StatusClosed, "Administrasi selesai"},
	}
	for _, s := range steps {
		var err error
		var got db.Claim
		if s.system {
			got, err = e.svc.SystemTransition(ctx, c.ID, s.to, s.reason)
		} else {
			got, err = e.svc.TransitionStatus(ctx, e.off1, c.ID, s.to, s.reason)
		}
		if err != nil || got.Status != string(s.to) {
			t.Fatalf("transisi ke %s: status=%s err=%v", s.to, got.Status, err)
		}
	}

	d, err := e.svc.Get(ctx, e.off1, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.History) != len(steps)+1 {
		t.Fatalf("timeline = %d entri", len(d.History))
	}
	if !d.History[2].BySystem || d.History[1].BySystem || d.History[1].ChangedByName != "Uji Petugas Satu" {
		t.Fatalf("pelaku timeline tidak sesuai: %+v", d.History[:3])
	}
	if d.History[4].Reason != "Surat keterangan belum ada" || d.History[4].OldStatus != claims.StatusNeedsReview {
		t.Fatalf("entri need_documents tidak sesuai: %+v", d.History[4])
	}
	if len(d.NextStatuses) != 0 {
		t.Fatal("klaim closed tidak boleh punya transisi berikutnya")
	}

	rows := e.auditRows(audit.ActionStatusChanged)
	if len(rows) != len(steps) {
		t.Fatalf("audit status_changed = %d", len(rows))
	}
	for _, r := range rows {
		if strings.Contains(string(r.AfterJson), "Surat keterangan") {
			t.Fatal("isi alasan tidak boleh disalin ke audit")
		}
	}
	var sys int
	for _, r := range rows {
		if r.UserID == nil {
			sys++
		}
	}
	if sys != 2 {
		t.Fatalf("audit transisi sistem harus tanpa user_id, dapat %d", sys)
	}
}

func TestInvalidTransitions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.create(e.off1, e.off1.UserID)

	// Lompatan yang tidak ada di peta.
	for _, to := range []claims.Status{claims.StatusReviewed, claims.StatusNeedDocuments, claims.StatusDraft, "approved", "rejected"} {
		_, err := e.svc.TransitionStatus(ctx, e.off1, c.ID, to, "alasan uji")
		if !errors.Is(err, claims.ErrInvalidTransition) {
			t.Errorf("draft -> %s harus ditolak, dapat %v", to, err)
		}
	}
	// Status khusus sistem tidak boleh di-set pengguna (termasuk supervisor).
	if _, err := e.svc.TransitionStatus(ctx, e.off1, c.ID, claims.StatusSubmitted, ""); err != nil {
		t.Fatal(err)
	}
	for _, a := range []claims.Actor{e.off1, e.sup} {
		_, err := e.svc.TransitionStatus(ctx, a, c.ID, claims.StatusProcessing, "")
		var te *claims.TransitionError
		if !errors.As(err, &te) || !te.SystemOnly {
			t.Errorf("%s tidak boleh set processing: %v", a.Role, err)
		}
	}
	if _, err := e.svc.SystemTransition(ctx, c.ID, claims.StatusProcessing, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.TransitionStatus(ctx, e.sup, c.ID, claims.StatusNeedsReview, ""); !errors.Is(err, claims.ErrInvalidTransition) {
		t.Fatalf("pengguna tidak boleh set needs_review: %v", err)
	}
	// Sistem tidak boleh melakukan transisi milik pengguna.
	if _, err := e.svc.SystemTransition(ctx, c.ID, claims.StatusReviewed, ""); !errors.Is(err, claims.ErrInvalidTransition) {
		t.Fatalf("sistem tidak boleh set reviewed dari processing: %v", err)
	}
	// Klaim closed final.
	c2 := e.create(e.off1, e.off1.UserID)
	if _, err := e.svc.TransitionStatus(ctx, e.off1, c2.ID, claims.StatusClosed, "Dibatalkan pemohon (simulasi)"); err != nil {
		t.Fatal(err)
	}
	for _, to := range claims.Statuses() {
		if _, err := e.svc.TransitionStatus(ctx, e.off1, c2.ID, to, "alasan"); !errors.Is(err, claims.ErrInvalidTransition) {
			t.Errorf("closed -> %s harus ditolak: %v", to, err)
		}
	}
	if _, err := e.svc.TransitionStatus(ctx, e.off1, 999999, claims.StatusSubmitted, ""); !errors.Is(err, claims.ErrNotFound) {
		t.Fatalf("klaim tidak ada: %v", err)
	}

	// Transisi yang ditolak tidak meninggalkan jejak.
	if n := e.count("SELECT count(*) FROM claim_status_histories WHERE claim_id=$1", c.ID); n != 3 {
		t.Fatalf("riwayat klaim 1 = %d, want 3", n)
	}
	if n := len(e.auditRows(audit.ActionStatusChanged)); n != 3 {
		t.Fatalf("audit status_changed = %d, want 3", n)
	}
}

func TestReasonRequired(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.create(e.off1, e.off1.UserID)
	if _, err := e.svc.TransitionStatus(ctx, e.off1, c.ID, claims.StatusClosed, "   "); !errors.Is(err, claims.ErrReasonRequired) {
		t.Fatalf("closed tanpa alasan: %v", err)
	}
	if _, err := e.svc.TransitionStatus(ctx, e.off1, c.ID, claims.StatusSubmitted, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.TransitionStatus(ctx, e.off1, c.ID, claims.StatusNeedDocuments, ""); !errors.Is(err, claims.ErrReasonRequired) {
		t.Fatalf("need_documents tanpa alasan: %v", err)
	}
	if _, err := e.svc.TransitionStatus(ctx, e.off1, c.ID, claims.StatusNeedDocuments, strings.Repeat("a", claims.MaxReasonLength+1)); !errors.Is(err, claims.ErrReasonTooLong) {
		t.Fatalf("alasan terlalu panjang: %v", err)
	}
	got, err := e.svc.TransitionStatus(ctx, e.off1, c.ID, claims.StatusNeedDocuments, "  Dokumen identitas buram  ")
	if err != nil || got.Status != string(claims.StatusNeedDocuments) {
		t.Fatalf("need_documents dengan alasan: %v", err)
	}
	var reason string
	if err := e.pool.QueryRow(ctx, "SELECT reason FROM claim_status_histories WHERE claim_id=$1 ORDER BY id DESC LIMIT 1", c.ID).Scan(&reason); err != nil || reason != "Dokumen identitas buram" {
		t.Fatalf("alasan tersimpan = %q err=%v", reason, err)
	}
}

func TestUpdateAuditsOnlyChangedFieldsMasked(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.create(e.off1, e.off1.UserID)

	// Tanpa perubahan: tidak ada audit.
	if _, err := e.svc.Update(ctx, e.off1, c.ID, input("POL-DEMO-0001", "2026-11-10", e.off1.UserID)); err != nil {
		t.Fatal(err)
	}
	if n := len(e.auditRows(audit.ActionClaimUpdated)); n != 0 {
		t.Fatalf("update tanpa perubahan tidak boleh diaudit, ada %d", n)
	}

	in := input("POL-DEMO-0009", "2026-11-12", e.off2.UserID) // officer tidak bisa reassign
	in.ClaimantName = "Nama Fiktif Baru"
	got, err := e.svc.Update(ctx, e.off1, c.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.PolicyNumber != "POL-DEMO-0009" || got.ClaimantName != "Nama Fiktif Baru" || *got.AssignedTo != e.off1.UserID {
		t.Fatalf("hasil update: %+v", got)
	}
	rows := e.auditRows(audit.ActionClaimUpdated)
	if len(rows) != 1 {
		t.Fatalf("audit claim_updated = %d", len(rows))
	}
	var before, after map[string]any
	_ = json.Unmarshal(rows[0].BeforeJson, &before)
	_ = json.Unmarshal(rows[0].AfterJson, &after)
	want := map[string][2]any{
		"claimant_name": {claims.Masked, claims.Masked},
		"policy_number": {claims.Masked, claims.Masked},
		"received_at":   {"2026-11-10", "2026-11-12"},
	}
	if len(before) != len(want) || len(after) != len(want) {
		t.Fatalf("audit harus hanya field berubah: before=%v after=%v", before, after)
	}
	for k, v := range want {
		if before[k] != v[0] || after[k] != v[1] {
			t.Errorf("%s: before=%v after=%v", k, before[k], after[k])
		}
	}
	all := string(rows[0].BeforeJson) + string(rows[0].AfterJson)
	if strings.Contains(all, "Fiktif") || strings.Contains(all, "POL-DEMO") {
		t.Fatalf("audit memuat PII: %s", all)
	}

	// Supervisor dapat reassign; tercatat sebagai id user.
	if _, err := e.svc.Update(ctx, e.sup, c.ID, func() claims.Input {
		i := input("POL-DEMO-0009", "2026-11-12", e.off2.UserID)
		i.ClaimantName = "Nama Fiktif Baru"
		return i
	}()); err != nil {
		t.Fatal(err)
	}
	rows = e.auditRows(audit.ActionClaimUpdated)
	var ra map[string]any
	_ = json.Unmarshal(rows[0].AfterJson, &ra)
	if len(ra) != 1 || ra["assigned_to"] != float64(e.off2.UserID) {
		t.Fatalf("audit reassign: %s", rows[0].AfterJson)
	}

	// Klaim closed tidak dapat diubah.
	if _, err := e.svc.TransitionStatus(ctx, e.sup, c.ID, claims.StatusClosed, "Selesai"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Update(ctx, e.sup, c.ID, input("POL-DEMO-0001", "2026-11-10", e.off2.UserID)); !errors.Is(err, claims.ErrClosed) {
		t.Fatalf("update klaim closed: %v", err)
	}
}

func TestAuthorization(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	other := e.create(e.off2, e.off2.UserID)
	in := input("POL-DEMO-0001", "2026-11-10", e.off1.UserID)

	// TC-07: officer tidak bisa membuka/mengubah klaim officer lain.
	if _, err := e.svc.Get(ctx, e.off1, other.ID); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("get klaim orang lain: %v", err)
	}
	if _, err := e.svc.Update(ctx, e.off1, other.ID, in); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("update klaim orang lain: %v", err)
	}
	if _, err := e.svc.TransitionStatus(ctx, e.off1, other.ID, claims.StatusSubmitted, ""); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("transisi klaim orang lain: %v", err)
	}
	denied := e.auditRows(audit.ActionAccessDenied)
	if len(denied) != 3 {
		t.Fatalf("access_denied = %d, want 3", len(denied))
	}
	for _, r := range denied {
		if r.EntityType != audit.EntityClaim || *r.EntityID != other.ID || *r.UserID != e.off1.UserID {
			t.Fatalf("access_denied tidak sesuai: %+v", r)
		}
	}
	got, _ := e.q.GetClaim(ctx, other.ID)
	if got.Status != "draft" || got.PolicyNumber != other.PolicyNumber {
		t.Fatal("klaim tidak boleh berubah setelah akses ditolak")
	}

	// Supervisor bisa melihat & mengubah klaim siapa pun.
	if _, err := e.svc.Get(ctx, e.sup, other.ID); err != nil {
		t.Fatalf("supervisor get: %v", err)
	}
	if _, err := e.svc.TransitionStatus(ctx, e.sup, other.ID, claims.StatusSubmitted, ""); err != nil {
		t.Fatalf("supervisor transisi: %v", err)
	}

	// Admin tidak mengakses data klaim sama sekali.
	if _, err := e.svc.Get(ctx, e.admin, other.ID); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("admin get harus ditolak")
	}
	if _, err := e.svc.List(ctx, e.admin, claims.Filter{}); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("admin list harus ditolak")
	}
	if _, err := e.svc.Create(ctx, e.admin, in); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("admin create harus ditolak")
	}
	if _, err := e.svc.Update(ctx, e.admin, other.ID, in); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("admin update harus ditolak")
	}
	if _, err := e.svc.TransitionStatus(ctx, e.admin, other.ID, claims.StatusReviewed, ""); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("admin transisi harus ditolak")
	}
	if n := len(e.auditRows(audit.ActionAccessDenied)); n != 8 {
		t.Fatalf("access_denied total = %d, want 8", n)
	}
}

func TestListFiltersAndPagination(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// 25 klaim untuk off1 (tanggal 1..25 Okt), 3 untuk off2.
	for i := 1; i <= 25; i++ {
		_, err := e.svc.Create(ctx, e.sup, input(fmt.Sprintf("POL-DEMO-%04d", 1000+i), fmt.Sprintf("2026-10-%02d", i), e.off1.UserID))
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 3; i++ {
		_, err := e.svc.Create(ctx, e.sup, input(fmt.Sprintf("POL-DEMO-%04d", 2000+i), "2026-11-01", e.off2.UserID))
		if err != nil {
			t.Fatal(err)
		}
	}
	// Dua klaim off1 di-submit.
	for _, id := range []int64{1, 2} {
		if _, err := e.svc.TransitionStatus(ctx, e.sup, id, claims.StatusSubmitted, ""); err != nil {
			t.Fatal(err)
		}
	}

	list := func(a claims.Actor, f claims.Filter) claims.Page {
		t.Helper()
		p, err := e.svc.List(ctx, a, f)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	date := func(s string) *time.Time {
		d, err := claims.ParseDate(s)
		if err != nil {
			t.Fatal(err)
		}
		return &d
	}

	// Pagination.
	p1 := list(e.sup, claims.Filter{})
	if p1.Total != 28 || len(p1.Items) != 20 || p1.TotalPages != 2 || p1.Page != 1 {
		t.Fatalf("halaman 1: total=%d items=%d pages=%d", p1.Total, len(p1.Items), p1.TotalPages)
	}
	p2 := list(e.sup, claims.Filter{Page: 2})
	if len(p2.Items) != 8 {
		t.Fatalf("halaman 2: %d item", len(p2.Items))
	}
	seen := map[int64]bool{}
	for _, it := range append(p1.Items, p2.Items...) {
		if seen[it.ID] {
			t.Fatalf("klaim %d muncul dua kali di pagination", it.ID)
		}
		seen[it.ID] = true
	}
	if !p1.Items[0].ReceivedAt.After(p1.Items[19].ReceivedAt) {
		t.Fatal("urutan harus received_at terbaru dulu")
	}
	if p := list(e.sup, claims.Filter{Page: 99}); len(p.Items) != 0 || p.Total != 28 {
		t.Fatal("halaman di luar jangkauan harus kosong")
	}

	// Cakupan officer: hanya klaim yang ditugaskan, filter petugas diabaikan.
	if p := list(e.off2, claims.Filter{AssignedTo: &e.off1.UserID}); p.Total != 3 {
		t.Fatalf("officer 2 harus melihat 3 klaim, dapat %d", p.Total)
	}
	if p := list(e.sup, claims.Filter{AssignedTo: &e.off2.UserID}); p.Total != 3 {
		t.Fatalf("filter petugas: %d", p.Total)
	}

	// Filter status, produk, tanggal (inklusif), pencarian.
	if p := list(e.sup, claims.Filter{Status: claims.StatusSubmitted}); p.Total != 2 {
		t.Fatalf("filter status: %d", p.Total)
	}
	if p := list(e.sup, claims.Filter{ProductType: claims.ProductHealthSim}); p.Total != 28 {
		t.Fatalf("filter produk: %d", p.Total)
	}
	if p := list(e.sup, claims.Filter{ReceivedFrom: date("2026-10-10"), ReceivedTo: date("2026-10-12")}); p.Total != 3 {
		t.Fatalf("filter tanggal: %d", p.Total)
	}
	if p := list(e.sup, claims.Filter{Search: "clm-demo-0002"}); p.Total != 1 || p.Items[0].ClaimNumber != "CLM-DEMO-0002" {
		t.Fatalf("cari nomor klaim: %+v", p)
	}
	if p := list(e.sup, claims.Filter{Search: "CLM-DEMO-001"}); p.Total != 10 {
		t.Fatalf("cari awalan nomor klaim (0010-0019): %d", p.Total)
	}
	if p := list(e.sup, claims.Filter{Search: "POL-DEMO-200"}); p.Total != 3 {
		t.Fatalf("cari nomor polis: %d", p.Total)
	}
	// Wildcard & injeksi diperlakukan sebagai teks literal.
	for _, q := range []string{"%", "_", "CLM-DEMO-00_1", "' OR '1'='1", `\`} {
		if p := list(e.sup, claims.Filter{Search: q}); p.Total != 0 {
			t.Errorf("pencarian %q harus literal, dapat %d", q, p.Total)
		}
	}
	// Kombinasi filter + cakupan officer.
	if p := list(e.off1, claims.Filter{Status: claims.StatusSubmitted, Search: "CLM-DEMO-0001"}); p.Total != 1 {
		t.Fatalf("kombinasi filter: %d", p.Total)
	}
	// Nilai filter tak dikenal diabaikan (tidak error).
	if p := list(e.sup, claims.Filter{Status: "approved", ProductType: "x"}); p.Total != 28 {
		t.Fatalf("filter tak dikenal: %d", p.Total)
	}
}

func TestUserTransitionsExcludeSystemStatuses(t *testing.T) {
	for _, from := range claims.Statuses() {
		for _, to := range claims.UserTransitions(from) {
			if to == claims.StatusProcessing || to == claims.StatusNeedsReview {
				t.Errorf("%s -> %s tidak boleh tersedia untuk pengguna", from, to)
			}
		}
	}
	for _, s := range claims.Statuses() {
		if strings.Contains(strings.ToLower(s.Label()), "tolak") || strings.Contains(strings.ToLower(s.Label()), "tidak layak") {
			t.Errorf("label %q tidak boleh memakai istilah ditolak/tidak layak", s.Label())
		}
	}
}
