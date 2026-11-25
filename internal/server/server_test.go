package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/auth"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/config"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/testutil"
)

// Data uji sintetis.
const testPassword = "kata-sandi-uji-0000"

var testUsers = map[authz.Role]struct{ name, email string }{
	authz.RoleClaimsOfficer: {"Uji Petugas Fiktif", "officer.test@example.com"},
	authz.RoleSupervisor:    {"Uji Supervisor Fiktif", "supervisor.test@example.com"},
	authz.RoleAdmin:         {"Uji Admin Fiktif", "admin.test@example.com"},
}

// Parameter argon2id ringan khusus test.
var lightParams = auth.Params{Memory: 1024, Iterations: 1, Parallelism: 1}

type testEnv struct {
	t     *testing.T
	srv   *Server
	ts    *httptest.Server
	pool  *pgxpool.Pool
	q     *db.Queries
	users map[authz.Role]db.User
}

type envOption func(*config.Config, *auth.RateLimiter)

func newEnv(t *testing.T, limiter *auth.RateLimiter, opts ...envOption) *testEnv {
	t.Helper()
	pool := testutil.DB(t)
	q := db.New(pool)
	hasher := auth.NewHasher(lightParams)

	users := make(map[authz.Role]db.User)
	for role, u := range testUsers {
		hash, err := hasher.Hash(testPassword)
		if err != nil {
			t.Fatal(err)
		}
		row, err := q.UpsertUser(context.Background(), db.UpsertUserParams{
			Name: u.name, Email: u.email, PasswordHash: hash, Role: string(role),
		})
		if err != nil {
			t.Fatalf("membuat user uji: %v", err)
		}
		users[role] = row
	}

	cfg := config.Config{Env: "test", Secret: []byte(strings.Repeat("k", config.MinSecretLength))}
	if limiter == nil {
		limiter = auth.NewRateLimiter(time.Millisecond, 1000)
	}
	for _, o := range opts {
		o(&cfg, limiter)
	}

	al := audit.NewLogger(q)
	svc, err := auth.NewService(q, al, hasher)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(Options{
		Config: cfg, Pool: pool, Auth: svc, Audit: al, Limiter: limiter,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		srv.Close()
	})
	return &testEnv{t: t, srv: srv, ts: ts, pool: pool, q: q, users: users}
}

func (e *testEnv) client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func (e *testEnv) get(c *http.Client, path string, hdr ...string) (*http.Response, string) {
	e.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, e.ts.URL+path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (e *testEnv) csrfToken(c *http.Client, path string) string {
	e.t.Helper()
	_, body := e.get(c, path)
	m := csrfRe.FindStringSubmatch(body)
	if m == nil {
		e.t.Fatalf("token CSRF tidak ditemukan di %s", path)
	}
	return m[1]
}

func (e *testEnv) post(c *http.Client, path string, form url.Values, hdr ...string) (*http.Response, string) {
	e.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.ts.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (e *testEnv) login(c *http.Client, email, password string, hdr ...string) (*http.Response, string) {
	e.t.Helper()
	token := e.csrfToken(c, "/login")
	return e.post(c, "/login", url.Values{"csrf_token": {token}, "email": {email}, "password": {password}}, hdr...)
}

func (e *testEnv) auditRows(action string) []db.AuditLog {
	e.t.Helper()
	rows, err := e.q.ListAuditLogsByAction(context.Background(), db.ListAuditLogsByActionParams{Action: action, Limit: 100})
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func (e *testEnv) allAuditJSON() string {
	e.t.Helper()
	rows, err := e.q.ListRecentAuditLogs(context.Background(), 1000)
	if err != nil {
		e.t.Fatal(err)
	}
	var sb strings.Builder
	for _, r := range rows {
		sb.Write(r.BeforeJson)
		sb.Write(r.AfterJson)
	}
	return sb.String()
}

func afterMap(t *testing.T, r db.AuditLog) map[string]string {
	t.Helper()
	m := map[string]string{}
	if err := json.Unmarshal(r.AfterJson, &m); err != nil {
		t.Fatalf("after_json tidak valid: %v", err)
	}
	return m
}

func sessionCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == "claims_session" {
			return c
		}
	}
	return nil
}

// --- Login ---

func TestLoginSuccessPerRole(t *testing.T) {
	e := newEnv(t, nil)
	wantMenu := map[authz.Role][]string{
		authz.RoleClaimsOfficer: {"/claims"},
		authz.RoleSupervisor:    {"/claims", "/reports"},
		authz.RoleAdmin:         {"/reports", "/admin/users"},
	}
	allMenu := []string{"/claims", "/reports", "/admin/users"}

	for role, u := range testUsers {
		t.Run(string(role), func(t *testing.T) {
			c := e.client()
			_, _ = e.get(c, "/login")
			before := c.Jar.Cookies(mustURL(e.ts.URL))

			resp, _ := e.login(c, strings.ToUpper(u.email), testPassword) // email case-insensitive
			if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/dashboard" {
				t.Fatalf("login harus 303 ke /dashboard, dapat %d %q", resp.StatusCode, resp.Header.Get("Location"))
			}
			ck := sessionCookie(resp)
			if ck == nil || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Secure {
				t.Fatalf("atribut cookie session tidak sesuai: %+v", ck)
			}
			if len(before) > 0 && before[0].Value == ck.Value {
				t.Fatal("token session harus diganti setelah login (anti session fixation)")
			}

			resp, body := e.get(c, "/dashboard")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("dashboard harus 200, dapat %d", resp.StatusCode)
			}
			if !strings.Contains(body, u.name) || !strings.Contains(body, role.Label()) {
				t.Fatal("dashboard harus menampilkan nama dan peran")
			}
			for _, p := range allMenu {
				want := contains(wantMenu[role], p)
				if got := strings.Contains(body, `href="`+p+`"`); got != want {
					t.Errorf("menu %s untuk %s: tampil=%v, harusnya %v", p, role, got, want)
				}
			}
		})
	}

	rows := e.auditRows(audit.ActionLoginSucceeded)
	if len(rows) != 3 {
		t.Fatalf("harus ada 3 audit login_succeeded, dapat %d", len(rows))
	}
	for _, r := range rows {
		if r.UserID == nil || r.EntityType != audit.EntityUser || afterMap(t, r)["ip_hash"] == "" {
			t.Fatalf("audit login_succeeded tidak lengkap: %+v", r)
		}
	}
	if strings.Contains(e.allAuditJSON(), testPassword) {
		t.Fatal("audit log tidak boleh memuat password")
	}
}

func TestLoginFailureWrongPassword(t *testing.T) {
	e := newEnv(t, nil)
	officer := e.users[authz.RoleClaimsOfficer]
	const wrong = "password-salah-0000"

	resp, body := e.login(e.client(), officer.Email, wrong)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("harus 401, dapat %d", resp.StatusCode)
	}
	if !strings.Contains(body, msgLoginFailed) {
		t.Fatal("harus menampilkan pesan generik")
	}
	if strings.Contains(body, wrong) {
		t.Fatal("password tidak boleh dikembalikan di respons")
	}

	rows := e.auditRows(audit.ActionLoginFailed)
	if len(rows) != 1 || rows[0].UserID == nil || *rows[0].UserID != officer.ID {
		t.Fatalf("audit login_failed harus mencatat user_id, dapat %+v", rows)
	}
	m := afterMap(t, rows[0])
	if m["reason"] != auth.ReasonInvalidCredentials || m["ip_hash"] == "" {
		t.Fatalf("after_json tidak sesuai: %v", m)
	}
	all := e.allAuditJSON()
	if strings.Contains(all, wrong) || strings.Contains(all, "127.0.0.1") {
		t.Fatal("audit log tidak boleh memuat password atau IP mentah")
	}
	if len(e.auditRows(audit.ActionLoginSucceeded)) != 0 {
		t.Fatal("tidak boleh ada login_succeeded")
	}
}

func TestLoginFailureUnknownEmailIsGeneric(t *testing.T) {
	e := newEnv(t, nil)
	const unknown = "tidak.terdaftar@example.com"

	resp, body := e.login(e.client(), unknown, testPassword)
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, msgLoginFailed) {
		t.Fatalf("email tak dikenal harus 401 dengan pesan generik yang sama, dapat %d", resp.StatusCode)
	}
	rows := e.auditRows(audit.ActionLoginFailed)
	if len(rows) != 1 || rows[0].UserID != nil {
		t.Fatalf("audit harus tercatat tanpa user_id, dapat %+v", rows)
	}
	if strings.Contains(e.allAuditJSON(), unknown) {
		t.Fatal("email yang dicoba tidak boleh disimpan di audit")
	}
}

func TestLoginInactiveUserIsGeneric(t *testing.T) {
	e := newEnv(t, nil)
	sup := e.users[authz.RoleSupervisor]
	if err := e.q.SetUserActive(context.Background(), db.SetUserActiveParams{ID: sup.ID, IsActive: false}); err != nil {
		t.Fatal(err)
	}
	resp, body := e.login(e.client(), sup.Email, testPassword)
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, msgLoginFailed) {
		t.Fatalf("akun nonaktif harus 401 generik, dapat %d", resp.StatusCode)
	}
	rows := e.auditRows(audit.ActionLoginFailed)
	if len(rows) != 1 || afterMap(t, rows[0])["reason"] != auth.ReasonInactiveAccount {
		t.Fatalf("alasan audit harus inactive_account, dapat %+v", rows)
	}
}

func TestDeactivatedUserLosesSession(t *testing.T) {
	e := newEnv(t, nil)
	c := e.client()
	officer := e.users[authz.RoleClaimsOfficer]
	e.login(c, officer.Email, testPassword)
	if err := e.q.SetUserActive(context.Background(), db.SetUserActiveParams{ID: officer.ID, IsActive: false}); err != nil {
		t.Fatal(err)
	}
	resp, _ := e.get(c, "/dashboard")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Fatalf("session user nonaktif harus dicabut, dapat %d", resp.StatusCode)
	}
}

// --- CSRF ---

func TestLoginRequiresCSRF(t *testing.T) {
	e := newEnv(t, nil)
	c := e.client()
	_, _ = e.get(c, "/login")
	officer := e.users[authz.RoleClaimsOfficer]

	for name, form := range map[string]url.Values{
		"tanpa token": {"email": {officer.Email}, "password": {testPassword}},
		"token salah": {"csrf_token": {"palsu"}, "email": {officer.Email}, "password": {testPassword}},
	} {
		resp, _ := e.post(c, "/login", form)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: harus 403, dapat %d", name, resp.StatusCode)
		}
	}
	if n := len(e.auditRows(audit.ActionLoginSucceeded)) + len(e.auditRows(audit.ActionLoginFailed)); n != 0 {
		t.Fatalf("request tanpa CSRF valid tidak boleh diproses, ada %d audit login", n)
	}
}

func TestCSRFHeaderAcceptedForHTMX(t *testing.T) {
	e := newEnv(t, nil)
	c := e.client()
	token := e.csrfToken(c, "/login")
	officer := e.users[authz.RoleClaimsOfficer]
	resp, _ := e.post(c, "/login", url.Values{"email": {officer.Email}, "password": {testPassword}}, "X-CSRF-Token", token)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("token via header harus diterima, dapat %d", resp.StatusCode)
	}
}

// --- Rate limit ---

func TestLoginRateLimitPerIP(t *testing.T) {
	limiter := auth.NewRateLimiter(time.Hour, 3)
	e := newEnv(t, limiter, func(c *config.Config, _ *auth.RateLimiter) { c.TrustProxy = true })
	c := e.client()
	officer := e.users[authz.RoleClaimsOfficer]
	ipA := []string{"X-Forwarded-For", "203.0.113.10"}

	for i := 0; i < 3; i++ {
		resp, _ := e.login(c, officer.Email, "salah-0000", ipA...)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("percobaan %d harus 401, dapat %d", i+1, resp.StatusCode)
		}
	}
	// Percobaan ke-4 diblokir, bahkan dengan password benar.
	resp, body := e.login(c, officer.Email, testPassword, ipA...)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("percobaan ke-4 harus 429, dapat %d", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" || !strings.Contains(body, msgRateLimited) {
		t.Fatal("respons 429 harus memuat Retry-After dan pesan")
	}
	if len(e.auditRows(audit.ActionLoginSucceeded)) != 0 {
		t.Fatal("login yang diblokir tidak boleh berhasil")
	}

	// IP lain tidak terpengaruh.
	resp, _ = e.login(e.client(), officer.Email, testPassword, "X-Forwarded-For", "203.0.113.20")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("IP lain harus tetap bisa login, dapat %d", resp.StatusCode)
	}
}

func TestClientIPIgnoresForwardedHeaderByDefault(t *testing.T) {
	limiter := auth.NewRateLimiter(time.Hour, 1)
	e := newEnv(t, limiter)
	officer := e.users[authz.RoleClaimsOfficer]
	e.login(e.client(), officer.Email, "salah-0000", "X-Forwarded-For", "198.51.100.1")
	resp, _ := e.login(e.client(), officer.Email, testPassword, "X-Forwarded-For", "198.51.100.2")
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("tanpa TRUST_PROXY, X-Forwarded-For tidak boleh dipakai untuk menghindari limit; dapat %d", resp.StatusCode)
	}
}

// --- RBAC ---

func TestCrossRoleAccess(t *testing.T) {
	e := newEnv(t, nil)
	cases := []struct {
		role authz.Role
		path string
		want int
	}{
		{authz.RoleClaimsOfficer, "/dashboard", http.StatusOK},
		{authz.RoleClaimsOfficer, "/claims", http.StatusOK},
		{authz.RoleClaimsOfficer, "/reports", http.StatusForbidden},
		{authz.RoleClaimsOfficer, "/admin/users", http.StatusForbidden},
		{authz.RoleSupervisor, "/claims", http.StatusOK},
		{authz.RoleSupervisor, "/reports", http.StatusOK},
		{authz.RoleSupervisor, "/admin/users", http.StatusForbidden},
		{authz.RoleAdmin, "/claims", http.StatusForbidden},
		{authz.RoleAdmin, "/reports", http.StatusOK},
		{authz.RoleAdmin, "/admin/users", http.StatusOK},
	}
	clients := map[authz.Role]*http.Client{}
	for role, u := range e.users {
		c := e.client()
		if resp, _ := e.login(c, u.Email, testPassword); resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("login %s gagal", role)
		}
		clients[role] = c
	}

	denied := 0
	for _, tc := range cases {
		resp, body := e.get(clients[tc.role], tc.path)
		if resp.StatusCode != tc.want {
			t.Errorf("%s GET %s = %d, want %d", tc.role, tc.path, resp.StatusCode, tc.want)
		}
		if tc.want == http.StatusForbidden {
			denied++
			if !strings.Contains(body, "Akses tidak diizinkan") {
				t.Errorf("%s GET %s: halaman 403 tidak sesuai", tc.role, tc.path)
			}
		}
	}

	rows := e.auditRows(audit.ActionAccessDenied)
	if len(rows) != denied {
		t.Fatalf("harus ada %d audit access_denied, dapat %d", denied, len(rows))
	}
	for _, r := range rows {
		m := afterMap(t, r)
		if r.UserID == nil || r.EntityType != audit.EntityRoute || m["path"] == "" || m["permission"] == "" {
			t.Fatalf("audit access_denied tidak lengkap: %+v %v", r, m)
		}
	}
}

// Lapis kedua: handler tetap menolak walaupun middleware route terlewat.
func TestHandlerEnforcesPermissionWithoutMiddleware(t *testing.T) {
	e := newEnv(t, nil)
	officer := auth.FromRow(e.users[authz.RoleClaimsOfficer])

	for name, h := range map[string]http.HandlerFunc{
		"/admin/users": e.srv.placeholder("Pengguna", "/admin/users", authz.PermUsersManage),
		"/reports":     e.srv.placeholder("Laporan", "/reports", authz.PermReportsView),
	} {
		req := httptest.NewRequest(http.MethodGet, name, nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxUser, &officer))
		rec := httptest.NewRecorder()
		h(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("handler %s tanpa middleware harus 403, dapat %d", name, rec.Code)
		}
	}
	if n := len(e.auditRows(audit.ActionAccessDenied)); n != 2 {
		t.Fatalf("harus ada 2 audit access_denied, dapat %d", n)
	}
}

func TestUnauthenticatedRedirectsToLogin(t *testing.T) {
	e := newEnv(t, nil)
	for _, p := range []string{"/dashboard", "/claims", "/reports", "/admin/users"} {
		resp, _ := e.get(e.client(), p)
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
			t.Errorf("GET %s tanpa login harus 303 ke /login, dapat %d", p, resp.StatusCode)
		}
	}
	resp, _ := e.get(e.client(), "/dashboard", "HX-Request", "true")
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("HX-Redirect") != "/login" {
		t.Fatalf("request HTMX tanpa login harus 401 + HX-Redirect, dapat %d", resp.StatusCode)
	}
}

// --- Logout ---

func TestLogout(t *testing.T) {
	e := newEnv(t, nil)
	c := e.client()
	sup := e.users[authz.RoleSupervisor]
	resp, _ := e.login(c, sup.Email, testPassword)
	oldCookie := sessionCookie(resp)

	token := e.csrfToken(c, "/dashboard")
	resp, _ = e.post(c, "/logout", url.Values{"csrf_token": {token}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Fatalf("logout harus 303 ke /login, dapat %d", resp.StatusCode)
	}

	rows := e.auditRows(audit.ActionLogout)
	if len(rows) != 1 || rows[0].UserID == nil || *rows[0].UserID != sup.ID {
		t.Fatalf("audit logout tidak tercatat dengan benar: %+v", rows)
	}

	if resp, _ := e.get(c, "/dashboard"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("setelah logout dashboard harus redirect, dapat %d", resp.StatusCode)
	}

	// Cookie lama tidak boleh bisa dipakai ulang (session dihapus di server).
	replay := e.client()
	replay.Jar.SetCookies(mustURL(e.ts.URL), []*http.Cookie{oldCookie})
	if resp, _ := e.get(replay, "/dashboard"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("cookie session lama harus ditolak, dapat %d", resp.StatusCode)
	}
}

func TestLogoutRequiresCSRF(t *testing.T) {
	e := newEnv(t, nil)
	c := e.client()
	e.login(c, e.users[authz.RoleAdmin].Email, testPassword)
	resp, _ := e.post(c, "/logout", url.Values{})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("logout tanpa CSRF harus 403, dapat %d", resp.StatusCode)
	}
	if len(e.auditRows(audit.ActionLogout)) != 0 {
		t.Fatal("logout tanpa CSRF tidak boleh tercatat")
	}
}

// --- Header & aset ---

func TestSecurityHeadersAndStatic(t *testing.T) {
	e := newEnv(t, nil)
	resp, body := e.get(e.client(), "/login")
	csp := resp.Header.Get("Content-Security-Policy")
	if csp == "" || strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
		t.Fatalf("CSP tidak sesuai: %q", csp)
	}
	if resp.Header.Get("X-Frame-Options") != "DENY" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("header keamanan tidak lengkap")
	}
	if strings.Contains(body, "<script>") || strings.Contains(body, " style=") || strings.Contains(body, "<style") {
		t.Fatal("halaman tidak boleh memuat inline script/style")
	}
	for _, p := range []string{"/static/css/app.css", "/static/js/htmx.min.js", "/static/js/app.js"} {
		if resp, _ := e.get(e.client(), p); resp.StatusCode != http.StatusOK {
			t.Errorf("%s harus 200, dapat %d", p, resp.StatusCode)
		}
	}
	if resp, _ := e.get(e.client(), "/static/js/"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("directory listing harus 404, dapat %d", resp.StatusCode)
	}
	if resp, _ := e.get(e.client(), "/tidak-ada"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("halaman tak dikenal harus 404, dapat %d", resp.StatusCode)
	}
}

func TestProductionCookieIsSecure(t *testing.T) {
	e := newEnv(t, nil, func(c *config.Config, _ *auth.RateLimiter) { c.Env = "production" })
	resp, _ := e.get(e.client(), "/login")
	ck := sessionCookie(resp)
	if ck == nil || !ck.Secure || !ck.HttpOnly {
		t.Fatalf("cookie production harus Secure dan HttpOnly: %+v", ck)
	}
	if resp.Header.Get("Strict-Transport-Security") == "" {
		t.Fatal("production harus mengirim HSTS")
	}
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
