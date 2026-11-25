package server

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/documents"
)

// documentRoutes dipasang di bawah /claims/{claimID}, sehingga sudah dijaga
// requirePerm(PermClaimsView) + requireClaimAccess. Service memeriksa ulang.
func (s *Server) documentRoutes(r chi.Router) {
	r.With(s.requirePerm(authz.PermDocumentsUpload)).Post("/documents", s.documentUpload)
	r.With(s.requirePerm(authz.PermDocumentsView)).Get("/documents/{docID:[0-9]+}", s.documentView)
}

// uploadState adalah keadaan formulir unggah setelah POST yang gagal.
type uploadState struct {
	Type       string
	FormError  string
	FileErrors []documents.FileError
	Duplicates []documents.DuplicateMatch
}

func (s *Server) documentUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	id := claimIDFrom(ctx)

	up := documents.Upload{
		ClaimID:          id,
		Type:             documents.Type(r.PostFormValue("document_type")),
		ConfirmDuplicate: r.PostFormValue("confirm_duplicate") == "1",
	}
	st := uploadState{Type: string(up.Type)}

	var headers []*multipart.FileHeader
	if r.MultipartForm != nil {
		headers = r.MultipartForm.File["files"]
	}
	if len(headers) > documents.MaxFiles {
		st.FormError = fmt.Sprintf("Maksimal %d file per unggahan.", documents.MaxFiles)
		s.renderUploadResult(w, r, http.StatusUnprocessableEntity, st)
		return
	}
	for i, fh := range headers {
		name := documents.SanitizeName(fh.Filename)
		// Tolak lebih awal tanpa membaca isi file yang terlalu besar.
		if fh.Size > documents.MaxFileSize {
			st.FileErrors = append(st.FileErrors, documents.FileError{Index: i, Name: name, Err: documents.ErrTooLarge})
			continue
		}
		data, err := readPart(fh)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		up.Files = append(up.Files, documents.File{Name: fh.Filename, Data: data})
	}
	if len(st.FileErrors) > 0 {
		st.FormError = "Tidak ada file yang disimpan. Perbaiki file yang ditandai lalu unggah ulang."
		s.renderUploadResult(w, r, http.StatusUnprocessableEntity, st)
		return
	}

	docs, err := s.docs.Upload(ctx, actorOf(u), up)
	var ve *documents.ValidationError
	var de *documents.DuplicateError
	switch {
	case errors.As(err, &ve):
		st.FormError = ve.Form
		st.FileErrors = ve.Files
		if st.FormError == "" {
			st.FormError = "Tidak ada file yang disimpan. Perbaiki file yang ditandai lalu unggah ulang."
		}
		s.renderUploadResult(w, r, http.StatusUnprocessableEntity, st)
		return
	case errors.As(err, &de):
		st.Duplicates = de.Matches
		s.renderUploadResult(w, r, http.StatusConflict, st)
		return
	case errors.Is(err, documents.ErrClaimClosed):
		// Form unggah tidak tampil untuk klaim ditutup; pesan di tingkat halaman.
		s.renderDetailWith(w, r, http.StatusUnprocessableEntity,
			detailOpts{Error: "Klaim sudah ditutup sehingga tidak menerima dokumen baru."})
		return
	case err != nil:
		s.claimServiceError(w, r, err)
		return
	}

	s.sm.Put(ctx, sessFlash, fmt.Sprintf("%d dokumen berhasil diunggah dan menunggu pemrosesan.", len(docs)))
	target := fmt.Sprintf("/claims/%d#documents", id)
	if r.Header.Get("HX-Request") == "true" {
		// Tanpa fragmen: redirect ke URL yang sama dengan "#..." hanya memicu
		// navigasi fragmen di browser sehingga daftar dokumen tidak dimuat ulang.
		w.Header().Set("HX-Redirect", fmt.Sprintf("/claims/%d", id))
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func readPart(fh *multipart.FileHeader) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// +1 agar file yang melebihi batas tetap terdeteksi oleh validator.
	return io.ReadAll(io.LimitReader(f, documents.MaxFileSize+1))
}

func (s *Server) renderUploadResult(w http.ResponseWriter, r *http.Request, status int, st uploadState) {
	s.renderDetailWith(w, r, status, detailOpts{Upload: &st})
}

// documentView melayani file lewat handler yang memeriksa hak akses klaim.
// Tidak ada URL publik; header mencegah MIME sniffing, cache bersama, dan
// eksekusi konten aktif.
func (s *Server) documentView(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	claimID := claimIDFrom(ctx)
	docID, err := strconv.ParseInt(chi.URLParam(r, "docID"), 10, 64)
	if err != nil || docID <= 0 {
		s.notFound(w, r)
		return
	}
	download := r.URL.Query().Get("download") == "1"

	d, f, err := s.docs.Open(ctx, actorOf(u), claimID, docID, download)
	switch {
	case errors.Is(err, documents.ErrNotFound), errors.Is(err, claims.ErrNotFound):
		s.notFound(w, r)
		return
	case err != nil:
		s.claimServiceError(w, r, err)
		return
	}
	defer f.Close()

	// Nama generik: nama file asli bisa memuat data pribadi.
	filename := fmt.Sprintf("dokumen-%d%s", d.ID, documents.Ext(d.MimeType))
	disposition := "inline"
	if download {
		disposition = "attachment"
	}
	h := w.Header()
	h.Set("Content-Type", d.MimeType)
	h.Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, filename))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-store")
	// Gambar dibuka dalam sandbox penuh. PDF tidak diberi "sandbox" karena
	// penampil PDF bawaan browser menolak merender dokumen sandbox; konten
	// tetap dibatasi default-src 'none'.
	csp := "default-src 'none'; frame-ancestors 'none'"
	if d.MimeType != documents.MIMEPDF {
		csp += "; img-src 'self'; sandbox"
	}
	h.Set("Content-Security-Policy", csp)
	h.Set("X-Frame-Options", "DENY")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	// ServeContent: dukungan Range untuk penampil PDF; modtime nol agar tidak
	// ada Last-Modified/caching kondisional.
	http.ServeContent(w, r, "", zeroTime, f)
}

var zeroTime time.Time
