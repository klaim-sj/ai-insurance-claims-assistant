package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/documents"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/extraction"
)

// Tahap 4: halaman hasil ekstraksi, polling status, proses ulang, dan
// penerapan nilai terdeteksi. Semua route berada di bawah
// /claims/{claimID} (requirePerm + requireClaimAccess); service memeriksa
// ulang otorisasi.

type documentResultData struct {
	ClaimID     int64
	ClaimNumber string
	R           documents.Result
	CanProcess  bool
	ErrorLabel  string
	High, Min   float64
	// Page: halaman yang disorot (tautan evidence temuan); 0 bila tidak ada.
	Page int
}

func docIDParam(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "docID"), 10, 64)
	return id, err == nil && id > 0
}

func (s *Server) documentServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, documents.ErrNotFound), errors.Is(err, claims.ErrNotFound):
		s.notFound(w, r)
	default:
		s.claimServiceError(w, r, err)
	}
}

func (s *Server) documentResult(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	claimID := claimIDFrom(ctx)
	docID, ok := docIDParam(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	det, err := s.claims.Get(ctx, actorOf(u), claimID)
	if err != nil {
		s.claimServiceError(w, r, err)
		return
	}
	res, err := s.docs.Result(ctx, actorOf(u), claimID, docID)
	if err != nil {
		s.documentServiceError(w, r, err)
		return
	}
	data := documentResultData{
		ClaimID: claimID, ClaimNumber: det.Claim.ClaimNumber, R: res,
		CanProcess: authz.Can(u.Role, authz.PermDocumentsProcess) && det.Status != claims.StatusClosed && res.Final(),
		ErrorLabel: documents.ErrorLabel(res.ErrorCode),
		High:       extraction.HighConfidence, Min: extraction.MinConfidence,
	}
	// ?hal=N: tautan evidence temuan ke halaman tertentu.
	if n, err := strconv.Atoi(r.URL.Query().Get("hal")); err == nil && n > 0 && n <= 1000 {
		data.Page = n
	}
	s.render(w, r, http.StatusOK, "document_result", pageData{
		Title: fmt.Sprintf("Dokumen #%d · %s", res.ID, det.Claim.ClaimNumber), Active: "/claims", Data: data,
	})
}

// documentStatus melayani fragmen status untuk polling HTMX. Bila status
// sudah final dan berbeda dari yang sedang ditampilkan, halaman dimuat
// ulang agar hasil ekstraksi tampil.
func (s *Server) documentStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	docID, ok := docIDParam(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	res, err := s.docs.Status(ctx, actorOf(u), claimIDFrom(ctx), docID)
	if err != nil {
		s.documentServiceError(w, r, err)
		return
	}
	if res.Final() && r.URL.Query().Get("shown") != res.Status {
		w.Header().Set("HX-Refresh", "true")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := s.tmpl.pages["document_result"].ExecuteTemplate(w, "processing_status", documentResultData{
		ClaimID: claimIDFrom(ctx), R: res, ErrorLabel: documents.ErrorLabel(res.ErrorCode),
	}); err != nil {
		s.log.Error("render status dokumen", "error", err)
	}
}

func (s *Server) documentReprocess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	claimID := claimIDFrom(ctx)
	docID, ok := docIDParam(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	back := fmt.Sprintf("/claims/%d/documents/%d/hasil", claimID, docID)
	if r.PostFormValue("back") == "claim" {
		back = fmt.Sprintf("/claims/%d#documents", claimID)
	}
	err := s.docs.Reprocess(ctx, actorOf(u), claimID, docID)
	switch {
	case errors.Is(err, documents.ErrAlreadyQueued):
		s.sm.Put(ctx, sessFlash, "Dokumen sudah dalam antrean atau sedang diproses; tidak dibuat antrean ganda.")
	case errors.Is(err, documents.ErrClaimClosed):
		s.sm.Put(ctx, sessFlash, "Klaim sudah ditutup sehingga dokumen tidak dapat diproses ulang.")
	case errors.Is(err, documents.ErrQueueUnavailable):
		s.sm.Put(ctx, sessFlash, "Antrean pemrosesan tidak tersedia. Hubungi administrator.")
	case err != nil:
		s.documentServiceError(w, r, err)
		return
	default:
		s.sm.Put(ctx, sessFlash, fmt.Sprintf("Dokumen #%d dimasukkan ke antrean untuk diproses ulang.", docID))
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (s *Server) applyDetected(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	claimID := claimIDFrom(ctx)
	fieldID, err := strconv.ParseInt(chi.URLParam(r, "fieldID"), 10, 64)
	if err != nil || fieldID <= 0 {
		s.notFound(w, r)
		return
	}
	_, key, err := s.claims.ApplyDetectedValue(ctx, actorOf(u), claimID, fieldID)
	label := extraction.Key(key).Label()
	var ve *claims.ValidationError
	switch {
	case errors.As(err, &ve):
		msg := "Nilai terdeteksi tidak lolos validasi data klaim"
		for _, m := range ve.Fields {
			msg += ": " + m
			break
		}
		s.renderDetail(w, r, http.StatusUnprocessableEntity, msg+" Periksa dokumen dan ubah data secara manual bila perlu.", "", "")
		return
	case errors.Is(err, claims.ErrDetectedNoChange):
		s.sm.Put(ctx, sessFlash, label+" pada data klaim sudah sama dengan nilai terdeteksi.")
	case errors.Is(err, claims.ErrDetectedNotApplicable):
		s.renderDetail(w, r, http.StatusUnprocessableEntity,
			"Nilai ini tidak dapat diterapkan otomatis (confidence rendah atau bukan dari formulir klaim). Verifikasi manual terhadap dokumen.", "", "")
		return
	case errors.Is(err, claims.ErrDetectedNotFound):
		s.notFound(w, r)
		return
	case errors.Is(err, claims.ErrClosed):
		s.sm.Put(ctx, sessFlash, "Klaim sudah ditutup sehingga datanya tidak dapat diubah.")
	case err != nil:
		s.claimServiceError(w, r, err)
		return
	default:
		s.sm.Put(ctx, sessFlash, label+" pada data klaim diperbarui dari nilai terdeteksi (tercatat di audit).")
	}
	http.Redirect(w, r, fmt.Sprintf("/claims/%d#detected", claimID), http.StatusSeeOther)
}
