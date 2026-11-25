package server

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"time"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/auth"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
)

// pageData adalah data untuk seluruh template.
type pageData struct {
	Title     string
	Active    string
	Error     string
	Email     string
	CSRFToken string
	User      *auth.User
	Menu      []authz.MenuItem
	KPIs      []string
	// Flash adalah pesan sukses sekali tampil (dari session).
	Flash string
	// Data adalah data khusus halaman.
	Data any
}

type templates struct {
	pages map[string]*template.Template
}

var pageFiles = map[string][]string{
	"login":        {"layout/base.html", "pages/login.html"},
	"error":        {"layout/base.html", "pages/error.html"},
	"dashboard":    {"layout/base.html", "layout/app.html", "pages/dashboard.html"},
	"placeholder":  {"layout/base.html", "layout/app.html", "pages/placeholder.html"},
	"forbidden":    {"layout/base.html", "layout/app.html", "pages/forbidden.html"},
	"claims_list":  {"layout/base.html", "layout/app.html", "pages/claims_list.html"},
	"claim_form":   {"layout/base.html", "layout/app.html", "pages/claim_form.html"},
	"claim_detail": {"layout/base.html", "layout/app.html", "pages/claim_detail.html"},
}

var bulan = [...]string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

// templateFuncs: tanggal ditampilkan dalam WIB dengan nama bulan Indonesia.
var templateFuncs = template.FuncMap{
	"date": func(t time.Time) string {
		t = t.In(claims.Location)
		return fmt.Sprintf("%02d %s %d", t.Day(), bulan[t.Month()-1], t.Year())
	},
	"datetime": func(t time.Time) string {
		t = t.In(claims.Location)
		return fmt.Sprintf("%02d %s %d %02d:%02d WIB", t.Day(), bulan[t.Month()-1], t.Year(), t.Hour(), t.Minute())
	},
	"isoDate":        func(t time.Time) string { return t.In(claims.Location).Format(claims.DateLayout) },
	"reasonRequired": claims.ReasonRequired,
}

func parseTemplates(fsys fs.FS) (*templates, error) {
	t := &templates{pages: make(map[string]*template.Template, len(pageFiles))}
	for name, files := range pageFiles {
		tpl, err := template.New(name).Funcs(templateFuncs).ParseFS(fsys, files...)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		t.pages[name] = tpl
	}
	return t, nil
}

// render menulis halaman ke buffer dulu agar error template tidak
// menghasilkan respons setengah jadi.
func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, page string, data pageData) {
	tpl, ok := s.tmpl.pages[page]
	if !ok {
		s.serverError(w, r, fmt.Errorf("template %q tidak ada", page))
		return
	}
	data.CSRFToken = csrfFrom(r.Context())
	if data.User == nil {
		data.User = userFrom(r.Context())
	}
	// Token CSRF hanya ada bila session sudah dimuat (LoadAndSave + csrf).
	if data.User != nil && data.Flash == "" && data.CSRFToken != "" {
		data.Flash = s.sm.PopString(r.Context(), sessFlash)
	}
	if data.User != nil && data.Menu == nil {
		data.Menu = authz.MenuFor(data.User.Role)
	}

	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "base", data); err != nil {
		s.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, title, msg string) {
	s.render(w, r, status, "error", pageData{Title: title, Error: msg})
}

// serverError mencatat error (tanpa data request/PII) dan menampilkan pesan generik.
func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("internal error", "method", r.Method, "path", r.URL.Path, "error", err)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte("Terjadi kesalahan pada server. Silakan coba lagi."))
}
