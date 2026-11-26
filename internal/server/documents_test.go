package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/documents"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/testutil"
)

type upFile struct {
	name string
	data []byte
}

// upload mengirim form multipart ke /claims/{id}/documents.
func (e *testEnv) upload(c *http.Client, claimID int64, token, docType string, files []upFile, extra map[string]string, hdr ...string) (*http.Response, string) {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("csrf_token", token)
	_ = mw.WriteField("document_type", docType)
	for k, v := range extra {
		_ = mw.WriteField(k, v)
	}
	for _, f := range files {
		fw, err := mw.CreateFormFile("files", f.name)
		if err != nil {
			e.t.Fatal(err)
		}
		_, _ = fw.Write(f.data)
	}
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/claims/%d/documents", e.ts.URL, claimID), &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
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

func (e *testEnv) docs(claimID int64) []db.ListClaimDocumentsRow {
	e.t.Helper()
	rows, err := e.q.ListClaimDocuments(context.Background(), claimID)
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func (e *testEnv) storedFiles() []string {
	e.t.Helper()
	var out []string
	_ = filepath.WalkDir(e.storageDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// officerClaim menyiapkan officer yang login beserta klaim yang ditugaskan kepadanya.
func (e *testEnv) officerClaim() (*http.Client, db.Claim, string) {
	e.t.Helper()
	c := e.loggedIn(authz.RoleClaimsOfficer)
	cl := e.seedClaim(e.users[authz.RoleClaimsOfficer].ID, "POL-DEMO-0001")
	token := e.csrfToken(c, fmt.Sprintf("/claims/%d", cl.ID))
	return c, cl, token
}

var storagePathRe = regexp.MustCompile(`^[0-9a-f]{2}/[0-9a-f]{32}$`)

// TC-01: unggah PDF dan PNG yang valid.
func TestDocumentUploadValid(t *testing.T) {
	e := newEnv(t, nil)
	c, cl, token := e.officerClaim()

	pdf, png := testutil.PDF(2), testutil.PNG(1)
	resp, _ := e.upload(c, cl.ID, token, "formulir_klaim", []upFile{
		{"Formulir Nama Fiktif.pdf", pdf}, {"lampiran.png", png},
	}, nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != fmt.Sprintf("/claims/%d#documents", cl.ID) {
		t.Fatalf("upload valid harus 303 ke detail, dapat %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	rows := e.docs(cl.ID)
	if len(rows) != 2 {
		t.Fatalf("harus ada 2 dokumen, dapat %d", len(rows))
	}
	for _, r := range rows {
		d, err := e.q.GetClaimDocument(context.Background(), db.GetClaimDocumentParams{ID: r.ID, ClaimID: cl.ID})
		if err != nil {
			t.Fatal(err)
		}
		// Tahap 4: dokumen langsung diantrekan untuk OCR dalam transaksi unggah.
		if d.ProcessingStatus != documents.StatusQueued || d.QueuedAt == nil {
			t.Errorf("processing_status = %q, ingin queued", d.ProcessingStatus)
		}
		if !storagePathRe.MatchString(d.StoragePath) || strings.Contains(d.StoragePath, "Formulir") {
			t.Errorf("storage_path harus acak, dapat %q", d.StoragePath)
		}
		if len(d.Checksum) != 64 || d.UploadedBy == nil || *d.UploadedBy != e.users[authz.RoleClaimsOfficer].ID {
			t.Errorf("metadata tidak lengkap: %+v", d)
		}
		want := png
		if d.MimeType == documents.MIMEPDF {
			want = pdf
			if d.PageCount == nil || *d.PageCount != 2 {
				t.Errorf("page_count PDF = %v, ingin 2", d.PageCount)
			}
		}
		full := filepath.Join(e.storageDir, filepath.FromSlash(d.StoragePath))
		got, err := os.ReadFile(full)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("isi file tersimpan tidak sama (err=%v)", err)
		}
		if st, _ := os.Stat(full); st.Mode().Perm() != 0o600 {
			t.Errorf("izin file = %v, ingin 0600", st.Mode().Perm())
		}
	}

	ups := e.auditRows(audit.ActionDocumentUploaded)
	if len(ups) != 2 {
		t.Fatalf("audit document_uploaded = %d, ingin 2", len(ups))
	}
	all := e.allAuditJSON()
	for _, leak := range []string{"Formulir Nama Fiktif", "lampiran.png", rows[0].OriginalName, "storage_path", "checksum"} {
		if strings.Contains(all, leak) {
			t.Errorf("audit tidak boleh memuat %q", leak)
		}
	}

	resp, body := e.get(c, fmt.Sprintf("/claims/%d", cl.ID))
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Formulir Nama Fiktif.pdf") ||
		!strings.Contains(body, fmt.Sprintf("/claims/%d/documents/%d", cl.ID, rows[0].ID)) {
		t.Fatal("daftar dokumen harus tampil di detail klaim")
	}
	// Tidak ada URL publik ke storage.
	if strings.Contains(body, "storage/") {
		t.Fatal("halaman tidak boleh memuat path storage")
	}
}

func TestDocumentUploadHTMXRedirect(t *testing.T) {
	e := newEnv(t, nil)
	c, cl, token := e.officerClaim()
	resp, _ := e.upload(c, cl.ID, token, "kuitansi", []upFile{{"k.jpg", testutil.JPEG(1)}}, nil, "HX-Request", "true")
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("HX-Redirect") != fmt.Sprintf("/claims/%d", cl.ID) {
		t.Fatalf("upload via HTMX harus 204 + HX-Redirect, dapat %d", resp.StatusCode)
	}
}

// TC-02 dan validasi lain: file ditolak, tidak ada yang disimpan.
func TestDocumentUploadRejected(t *testing.T) {
	e := newEnv(t, nil)
	c, cl, token := e.officerClaim()

	tests := []struct {
		name    string
		docType string
		files   []upFile
		want    int
		msg     string
	}{
		{"TC-02 exe berekstensi pdf", "formulir_klaim", []upFile{{"formulir.pdf", append([]byte("MZ\x90\x00"), make([]byte, 128)...)}}, http.StatusUnprocessableEntity, "jenis file tidak didukung"},
		{"TC-02 png berekstensi pdf", "formulir_klaim", []upFile{{"formulir.pdf", testutil.PNG(1)}}, http.StatusUnprocessableEntity, "ekstensi file tidak sesuai"},
		{"ukuran berlebih", "lainnya", []upFile{{"besar.pdf", append(testutil.PDF(1), make([]byte, documents.MaxFileSize)...)}}, http.StatusUnprocessableEntity, "melebihi batas 10 MB"},
		{"pdf terenkripsi", "lainnya", []upFile{{"a.pdf", testutil.EncryptedPDF()}}, http.StatusUnprocessableEntity, "terenkripsi"},
		{"pdf rusak", "lainnya", []upFile{{"a.pdf", testutil.PDF(1)[:40]}}, http.StatusUnprocessableEntity, "rusak"},
		{"satu file buruk membatalkan semua", "lainnya", []upFile{{"ok.png", testutil.PNG(2)}, {"bad.png", []byte("bukan gambar")}}, http.StatusUnprocessableEntity, "bad.png"},
		{"jenis dokumen tidak valid", "foto_selfie", []upFile{{"ok.png", testutil.PNG(3)}}, http.StatusUnprocessableEntity, "Pilih jenis dokumen"},
		{"tanpa file", "lainnya", nil, http.StatusUnprocessableEntity, "minimal satu file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := e.upload(c, cl.ID, token, tt.docType, tt.files, nil)
			if resp.StatusCode != tt.want {
				t.Fatalf("status = %d, ingin %d", resp.StatusCode, tt.want)
			}
			if !strings.Contains(body, tt.msg) {
				t.Fatalf("pesan %q tidak ditemukan", tt.msg)
			}
		})
	}
	if n := len(e.docs(cl.ID)); n != 0 {
		t.Fatalf("tidak boleh ada dokumen tersimpan, dapat %d", n)
	}
	if f := e.storedFiles(); len(f) != 0 {
		t.Fatalf("tidak boleh ada file di storage, dapat %d", len(f))
	}
	if n := len(e.auditRows(audit.ActionDocumentUploaded)); n != 0 {
		t.Fatalf("tidak boleh ada audit document_uploaded, dapat %d", n)
	}
}

func TestDocumentUploadTotalTooLarge(t *testing.T) {
	e := newEnv(t, nil)
	c, cl, token := e.officerClaim()
	big := make([]byte, documents.MaxFileSize)
	files := make([]upFile, documents.MaxFiles+1)
	for i := range files {
		files[i] = upFile{fmt.Sprintf("f%d.pdf", i), big}
	}
	resp, _ := e.upload(c, cl.ID, token, "lainnya", files, nil)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("total berlebih harus 413, dapat %d", resp.StatusCode)
	}
	if len(e.docs(cl.ID)) != 0 || len(e.storedFiles()) != 0 {
		t.Fatal("tidak boleh ada yang tersimpan")
	}
}

func TestDocumentUploadDuplicate(t *testing.T) {
	e := newEnv(t, nil)
	c, cl, token := e.officerClaim()
	pdf := testutil.PDF(1)

	if resp, _ := e.upload(c, cl.ID, token, "formulir_klaim", []upFile{{"a.pdf", pdf}}, nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("upload pertama gagal: %d", resp.StatusCode)
	}
	first := e.docs(cl.ID)[0]

	// Isi sama, nama berbeda: diminta konfirmasi, belum disimpan.
	resp, body := e.upload(c, cl.ID, token, "lainnya", []upFile{{"salinan.pdf", pdf}}, nil)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(body, "identik dengan dokumen yang sudah ada") ||
		!strings.Contains(body, `name="confirm_duplicate"`) {
		t.Fatalf("duplikat harus 409 dengan konfirmasi, dapat %d", resp.StatusCode)
	}
	if n := len(e.docs(cl.ID)); n != 1 {
		t.Fatalf("duplikat belum boleh disimpan, dokumen = %d", n)
	}
	dups := e.auditRows(audit.ActionDocumentDuplicateDetected)
	if len(dups) != 1 || dups[0].EntityID == nil || *dups[0].EntityID != first.ID {
		t.Fatalf("audit document_duplicate_detected tidak sesuai: %+v", dups)
	}

	// Duplikat sesama file dalam satu unggahan juga terdeteksi.
	other := testutil.PNG(9)
	resp, _ = e.upload(c, cl.ID, token, "lainnya", []upFile{{"x.png", other}, {"y.png", other}}, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplikat dalam satu unggahan harus 409, dapat %d", resp.StatusCode)
	}

	// Setelah konfirmasi, disimpan dengan penanda duplicate_of.
	resp, _ = e.upload(c, cl.ID, token, "lainnya", []upFile{{"salinan.pdf", pdf}}, map[string]string{"confirm_duplicate": "1"})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("duplikat terkonfirmasi harus 303, dapat %d", resp.StatusCode)
	}
	rows := e.docs(cl.ID)
	if len(rows) != 2 || rows[0].DuplicateOf == nil || *rows[0].DuplicateOf != first.ID {
		t.Fatalf("dokumen duplikat harus menunjuk ke #%d: %+v", first.ID, rows)
	}

	// Klaim lain dengan file yang sama bukan duplikat.
	cl2 := e.seedClaim(e.users[authz.RoleClaimsOfficer].ID, "POL-DEMO-0002")
	if resp, _ := e.upload(c, cl2.ID, token, "formulir_klaim", []upFile{{"a.pdf", pdf}}, nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("file sama di klaim lain harus diterima, dapat %d", resp.StatusCode)
	}
}

func TestDocumentPreviewAndDownload(t *testing.T) {
	e := newEnv(t, nil)
	c, cl, token := e.officerClaim()
	pdf := testutil.PDF(1)
	e.upload(c, cl.ID, token, "surat_keterangan_medis", []upFile{{"Surat Nama Fiktif.pdf", pdf}}, nil)
	doc := e.docs(cl.ID)[0]
	path := fmt.Sprintf("/claims/%d/documents/%d", cl.ID, doc.ID)

	resp, body := e.get(c, path)
	if resp.StatusCode != http.StatusOK || body != string(pdf) {
		t.Fatalf("pratinjau harus 200 dengan isi file, dapat %d", resp.StatusCode)
	}
	h := resp.Header
	checks := map[string]string{
		"Content-Type":           documents.MIMEPDF,
		"Content-Disposition":    fmt.Sprintf(`inline; filename="dokumen-%d.pdf"`, doc.ID),
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "private, no-store",
	}
	for k, v := range checks {
		if h.Get(k) != v {
			t.Errorf("header %s = %q, ingin %q", k, h.Get(k), v)
		}
	}
	if !strings.Contains(h.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Errorf("CSP dokumen harus default-src 'none', dapat %q", h.Get("Content-Security-Policy"))
	}
	if strings.Contains(h.Get("Content-Disposition"), "Fiktif") {
		t.Error("Content-Disposition tidak boleh memuat nama file asli")
	}

	resp, _ = e.get(c, path+"?download=1")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment;") {
		t.Fatalf("unduh harus attachment, dapat %d %q", resp.StatusCode, resp.Header.Get("Content-Disposition"))
	}

	views := e.auditRows(audit.ActionDocumentViewed)
	if len(views) != 2 || *views[0].EntityID != doc.ID {
		t.Fatalf("audit document_viewed = %d, ingin 2", len(views))
	}
	if m := string(views[0].AfterJson); !strings.Contains(m, `"mode": "download"`) && !strings.Contains(m, `"mode":"download"`) {
		t.Errorf("audit harus mencatat mode download: %s", m)
	}

	// Gambar dilayani dalam sandbox CSP.
	e.upload(c, cl.ID, token, "identitas", []upFile{{"id.png", testutil.PNG(4)}}, nil)
	img := e.docs(cl.ID)[0]
	resp, _ = e.get(c, fmt.Sprintf("/claims/%d/documents/%d", cl.ID, img.ID))
	if resp.Header.Get("Content-Type") != documents.MIMEPNG || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("header gambar tidak sesuai: %v", resp.Header)
	}

	if resp, _ := e.get(c, fmt.Sprintf("/claims/%d/documents/99999", cl.ID)); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("dokumen tidak ada harus 404, dapat %d", resp.StatusCode)
	}
}

// Officer tidak bisa melihat/mengunduh/mengunggah dokumen klaim orang lain.
func TestDocumentAccessOtherOfficerDenied(t *testing.T) {
	e := newEnv(t, nil)
	other := e.secondOfficer()
	foreign := e.seedClaim(other.ID, "POL-DEMO-0002")

	// Supervisor (seluruh klaim tim) mengunggah dokumen ke klaim officer lain.
	sc := e.loggedIn(authz.RoleSupervisor)
	stoken := e.csrfToken(sc, fmt.Sprintf("/claims/%d", foreign.ID))
	if resp, _ := e.upload(sc, foreign.ID, stoken, "identitas", []upFile{{"id.png", testutil.PNG(1)}}, nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("upload supervisor gagal: %d", resp.StatusCode)
	}
	doc := e.docs(foreign.ID)[0]
	path := fmt.Sprintf("/claims/%d/documents/%d", foreign.ID, doc.ID)
	if resp, _ := e.get(sc, path); resp.StatusCode != http.StatusOK {
		t.Fatalf("supervisor harus bisa melihat, dapat %d", resp.StatusCode)
	}
	views0 := len(e.auditRows(audit.ActionDocumentViewed))

	c, own, token := e.officerClaim()
	denied0 := len(e.auditRows(audit.ActionAccessDenied))
	for _, p := range []string{path, path + "?download=1"} {
		resp, body := e.get(c, p)
		if resp.StatusCode != http.StatusForbidden || strings.Contains(body, "PNG") {
			t.Fatalf("GET %s oleh officer lain harus 403, dapat %d", p, resp.StatusCode)
		}
	}
	if resp, _ := e.upload(c, foreign.ID, token, "lainnya", []upFile{{"x.png", testutil.PNG(2)}}, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("upload ke klaim orang lain harus 403, dapat %d", resp.StatusCode)
	}
	if len(e.auditRows(audit.ActionAccessDenied)) < denied0+3 {
		t.Fatal("penolakan akses harus dicatat")
	}

	// IDOR: dokumen klaim lain lewat klaim milik sendiri → 404.
	if resp, _ := e.get(c, fmt.Sprintf("/claims/%d/documents/%d", own.ID, doc.ID)); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("dokumen klaim lain via klaim sendiri harus 404, dapat %d", resp.StatusCode)
	}
	if len(e.docs(foreign.ID)) != 1 || len(e.auditRows(audit.ActionDocumentViewed)) != views0 {
		t.Fatal("tidak boleh ada dokumen baru atau akses dokumen tercatat")
	}

	// Lapis service juga menolak walau middleware dilewati.
	officer := claims.Actor{UserID: e.users[authz.RoleClaimsOfficer].ID, Role: authz.RoleClaimsOfficer}
	if _, _, err := e.srv.docs.Open(context.Background(), officer, foreign.ID, doc.ID, false); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("service harus menolak officer lain, dapat %v", err)
	}
}

// Admin tidak memiliki akses dokumen sama sekali.
func TestDocumentAdminDenied(t *testing.T) {
	e := newEnv(t, nil)
	c, cl, token := e.officerClaim()
	e.upload(c, cl.ID, token, "identitas", []upFile{{"id.png", testutil.PNG(1)}}, nil)
	doc := e.docs(cl.ID)[0]

	ac := e.loggedIn(authz.RoleAdmin)
	if resp, _ := e.get(ac, fmt.Sprintf("/claims/%d/documents/%d", cl.ID, doc.ID)); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin harus 403, dapat %d", resp.StatusCode)
	}
	atoken := e.csrfToken(ac, "/dashboard")
	if resp, _ := e.upload(ac, cl.ID, atoken, "lainnya", []upFile{{"x.png", testutil.PNG(2)}}, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("upload admin harus 403, dapat %d", resp.StatusCode)
	}
	if len(e.docs(cl.ID)) != 1 {
		t.Fatal("admin tidak boleh menambah dokumen")
	}

	// Lapis service juga menolak admin walau middleware dilewati.
	admin := claims.Actor{UserID: e.users[authz.RoleAdmin].ID, Role: authz.RoleAdmin}
	if _, _, err := e.srv.docs.Open(context.Background(), admin, cl.ID, doc.ID, false); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("service harus menolak admin, dapat %v", err)
	}
	if _, err := e.srv.docs.Upload(context.Background(), admin, documents.Upload{
		ClaimID: cl.ID, Type: documents.TypeOther, Files: []documents.File{{Name: "x.png", Data: testutil.PNG(5)}},
	}); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("service harus menolak upload admin, dapat %v", err)
	}
}

func TestDocumentUploadClosedClaim(t *testing.T) {
	e := newEnv(t, nil)
	c, cl, token := e.officerClaim()
	officer := claims.Actor{UserID: e.users[authz.RoleClaimsOfficer].ID, Role: authz.RoleClaimsOfficer}
	if _, err := e.srv.claims.TransitionStatus(context.Background(), officer, cl.ID, claims.StatusClosed, "Uji penutupan klaim fiktif"); err != nil {
		t.Fatal(err)
	}
	resp, body := e.upload(c, cl.ID, token, "lainnya", []upFile{{"a.png", testutil.PNG(1)}}, nil)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "sudah ditutup") {
		t.Fatalf("upload ke klaim ditutup harus 422, dapat %d", resp.StatusCode)
	}
	if strings.Contains(body, `id="upload-form"`) {
		t.Fatal("form unggah tidak boleh tampil untuk klaim ditutup")
	}
	if len(e.docs(cl.ID)) != 0 || len(e.storedFiles()) != 0 {
		t.Fatal("tidak boleh ada yang tersimpan")
	}
}
