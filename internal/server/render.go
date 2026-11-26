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
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/documents"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/extraction"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/validation"
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
	"login":           {"layout/base.html", "pages/login.html"},
	"error":           {"layout/base.html", "pages/error.html"},
	"dashboard":       {"layout/base.html", "layout/app.html", "pages/dashboard.html"},
	"placeholder":     {"layout/base.html", "layout/app.html", "pages/placeholder.html"},
	"forbidden":       {"layout/base.html", "layout/app.html", "pages/forbidden.html"},
	"claims_list":     {"layout/base.html", "layout/app.html", "pages/claims_list.html"},
	"claim_form":      {"layout/base.html", "layout/app.html", "pages/claim_form.html"},
	"claim_detail":    {"layout/base.html", "layout/app.html", "pages/claim_detail.html"},
	"document_result": {"layout/base.html", "layout/app.html", "pages/document_result.html"},
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
	"docTypeLabel":   func(t documents.Type) string { return t.Label() },
	"docStatusLabel": documents.StatusLabel,
	"filesize": func(n int64) string {
		switch {
		case n >= 1<<20:
			return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
		case n >= 1<<10:
			return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
		}
		return fmt.Sprintf("%d B", n)
	},
	"inc":           func(i int) int { return i + 1 },
	"docErrorLabel": documents.ErrorLabel,
	"fieldStatusLabel": func(st extraction.Status) string {
		switch st {
		case extraction.StatusFound:
			return "Terdeteksi"
		case extraction.StatusUncertain:
			return "Tidak pasti — verifikasi manual"
		case extraction.StatusNotFound:
			return "Tidak ditemukan"
		}
		return string(st)
	},
	// confClass: kelas badge label confidence.
	"confClass": func(label string) string {
		switch label {
		case "tinggi":
			return "bg-emerald-100 text-emerald-800"
		case "perlu verifikasi":
			return "bg-amber-100 text-amber-900"
		case "rendah":
			return "bg-red-100 text-red-800"
		}
		return "bg-slate-100 text-slate-700"
	},
	"docStatusClass": func(st string) string {
		switch st {
		case "completed":
			return "bg-emerald-100 text-emerald-800"
		case "failed":
			return "bg-red-100 text-red-800"
		case "queued", "processing":
			return "bg-sky-100 text-sky-800"
		}
		return "bg-slate-100 text-slate-700"
	},
	"pct": func(v *float64) string {
		if v == nil {
			return "—"
		}
		return fmt.Sprintf("%.0f", *v)
	},
	// --- Tahap 5: temuan validasi ---
	"severityClass": func(sv validation.Severity) string {
		switch sv {
		case validation.SeverityHigh:
			return "bg-rose-100 text-rose-900"
		case validation.SeverityMedium:
			return "bg-amber-100 text-amber-900"
		}
		return "bg-slate-100 text-slate-700"
	},
	"findingCodeLabel": findingCodeLabel,
	"evidenceSourceLabel": func(e validation.Evidence) string {
		switch e.Source {
		case validation.SourceDocument:
			return fmt.Sprintf("%s · dok. #%d · hal. %d", documents.Type(e.DocumentType).Label(), e.DocumentID, e.Page)
		case validation.SourceClaimRecord:
			return "Data klaim"
		case validation.SourceChecklist:
			return "Checklist dokumen wajib"
		}
		return e.Source
	},
	"evidenceFieldLabel": func(e validation.Evidence) string {
		switch e.Field {
		case "":
			return ""
		case "product_type":
			return "Jenis produk"
		case "document_type":
			return "Jenis dokumen"
		}
		if e.Source == validation.SourceClaimRecord && e.Field == "claimant_name" {
			return "Nama pemohon"
		}
		return extraction.Key(e.Field).Label()
	},
	"evidenceValue": func(e validation.Evidence) string {
		if e.Source == validation.SourceChecklist && e.Field == "document_type" {
			return documents.Type(e.Value).Label()
		}
		return e.Value
	},
	"deref": func(v *time.Time) time.Time {
		if v == nil {
			return time.Time{}
		}
		return *v
	},
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

// findingCodeLabel: nama jenis temuan untuk UI (netral, tanpa istilah
// "ditolak"/"tidak layak").
func findingCodeLabel(code string) string {
	switch code {
	case validation.CodeMissingDocument:
		return "Dokumen wajib belum ada"
	case validation.CodeMissingField:
		return "Field wajib belum terbaca"
	case validation.CodeNameMismatch:
		return "Perbedaan nama"
	case validation.CodePolicyMismatch:
		return "Perbedaan nomor polis"
	case validation.CodeDateInconsistency:
		return "Tanggal tidak konsisten"
	case validation.CodeLowQualityDocument:
		return "Kualitas dokumen rendah"
	case validation.CodeNeedsManualVerification:
		return "Perlu verifikasi manual"
	}
	return code
}
