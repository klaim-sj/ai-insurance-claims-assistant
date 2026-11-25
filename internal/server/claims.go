package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/auth"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
)

const ctxClaimID ctxKey = 100

// claimRoutes: setiap route dijaga requirePerm (RBAC), dan route per klaim
// juga dijaga requireClaimAccess (kebijakan per objek). Service melakukan
// pemeriksaan yang sama sebagai lapis kedua.
func (s *Server) claimRoutes(r chi.Router) {
	r.With(s.requirePerm(authz.PermClaimsView)).Get("/", s.claimsList)
	r.With(s.requirePerm(authz.PermClaimsCreate)).Get("/new", s.claimNew)
	r.With(s.requirePerm(authz.PermClaimsCreate)).Post("/", s.claimCreate)

	r.Route("/{claimID:[0-9]+}", func(r chi.Router) {
		r.Use(s.requirePerm(authz.PermClaimsView), s.requireClaimAccess)
		r.Get("/", s.claimDetail)
		r.With(s.requirePerm(authz.PermClaimsEdit)).Get("/edit", s.claimEdit)
		r.With(s.requirePerm(authz.PermClaimsEdit)).Post("/", s.claimUpdate)
		r.With(s.requirePerm(authz.PermClaimsTransition)).Post("/status", s.claimTransition)
	})
}

func actorOf(u *auth.User) claims.Actor { return claims.Actor{UserID: u.ID, Role: u.Role} }

func claimIDFrom(ctx context.Context) int64 {
	id, _ := ctx.Value(ctxClaimID).(int64)
	return id
}

// requireClaimAccess memuat klaim dari {claimID} dan memastikan pengguna
// berhak mengaksesnya. Penolakan dicatat sebagai access_denied.
func (s *Server) requireClaimAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "claimID"), 10, 64)
		if err != nil || id <= 0 {
			s.notFound(w, r)
			return
		}
		assigned, err := s.claims.AssignedTo(r.Context(), id)
		if errors.Is(err, claims.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		u := userFrom(r.Context())
		if u == nil || !authz.CanAccessClaim(u.Role, u.ID, assigned) {
			s.forbiddenClaim(w, r, id)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxClaimID, id)))
	})
}

func (s *Server) forbiddenClaim(w http.ResponseWriter, r *http.Request, id int64) {
	u := userFrom(r.Context())
	var uid *int64
	if u != nil {
		uid = &u.ID
	}
	if err := s.audit.Record(r.Context(), audit.Entry{
		UserID:     uid,
		EntityType: audit.EntityClaim,
		EntityID:   &id,
		Action:     audit.ActionAccessDenied,
		After: map[string]string{
			"layer":  "middleware",
			"method": r.Method,
			"path":   r.URL.Path,
			"reason": "claim_not_accessible",
		},
	}); err != nil {
		s.log.Error("gagal menulis audit access_denied", "error", err)
	}
	s.render(w, r, http.StatusForbidden, "forbidden", pageData{Title: "Akses tidak diizinkan"})
}

// claimServiceError memetakan error umum dari service ke respons HTTP.
func (s *Server) claimServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, claims.ErrNotFound):
		s.notFound(w, r)
	case errors.Is(err, authz.ErrForbidden):
		// access_denied sudah dicatat oleh service.
		s.render(w, r, http.StatusForbidden, "forbidden", pageData{Title: "Akses tidak diizinkan"})
	default:
		s.serverError(w, r, err)
	}
}

// --- Daftar klaim ---

type listFilterForm struct {
	Q, Status, Product, AssignedTo, From, To string
}

type pageLink struct {
	Label   string
	URL     string
	Current bool
}

type claimsListData struct {
	Result       claims.Page
	Filter       listFilterForm
	Statuses     []claims.Status
	Products     []string
	Officers     []claims.Officer
	CanFilterAll bool
	CanCreate    bool
	FirstItem    int
	LastItem     int
	PrevURL      string
	NextURL      string
	Pages        []pageLink
	HasFilter    bool
}

func (s *Server) claimsList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	a := actorOf(u)
	qv := r.URL.Query()

	ff := listFilterForm{
		Q:       strings.TrimSpace(qv.Get("q")),
		Status:  qv.Get("status"),
		Product: qv.Get("product"),
		From:    qv.Get("from"),
		To:      qv.Get("to"),
	}
	f := claims.Filter{Search: ff.Q}
	if st := claims.Status(ff.Status); st.Valid() {
		f.Status = st
	} else {
		ff.Status = ""
	}
	f.ProductType = ff.Product
	if !slices.Contains(claims.ProductTypes, ff.Product) {
		ff.Product = ""
	}
	if d, err := claims.ParseDate(ff.From); err == nil {
		f.ReceivedFrom = &d
	} else {
		ff.From = ""
	}
	if d, err := claims.ParseDate(ff.To); err == nil {
		f.ReceivedTo = &d
	} else {
		ff.To = ""
	}
	canAll := authz.Can(u.Role, authz.PermClaimsViewAll)
	if canAll {
		if id, err := strconv.ParseInt(qv.Get("assigned_to"), 10, 64); err == nil && id > 0 {
			f.AssignedTo = &id
			ff.AssignedTo = strconv.FormatInt(id, 10)
		}
	}
	f.Page, _ = strconv.Atoi(qv.Get("page"))

	res, err := s.claims.List(ctx, a, f)
	if err != nil {
		s.claimServiceError(w, r, err)
		return
	}
	data := claimsListData{
		Result:       res,
		Filter:       ff,
		Statuses:     claims.Statuses(),
		Products:     claims.ProductTypes,
		CanFilterAll: canAll,
		CanCreate:    authz.Can(u.Role, authz.PermClaimsCreate),
		HasFilter:    ff != (listFilterForm{}),
	}
	if canAll {
		if data.Officers, err = s.claims.Officers(ctx, a); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	if len(res.Items) > 0 {
		data.FirstItem = (res.Page-1)*res.PageSize + 1
		data.LastItem = data.FirstItem + len(res.Items) - 1
	}
	pageURL := func(p int) string {
		v := url.Values{}
		for k, val := range map[string]string{
			"q": ff.Q, "status": ff.Status, "product": ff.Product,
			"assigned_to": ff.AssignedTo, "from": ff.From, "to": ff.To,
		} {
			if val != "" {
				v.Set(k, val)
			}
		}
		if p > 1 {
			v.Set("page", strconv.Itoa(p))
		}
		if len(v) == 0 {
			return "/claims"
		}
		return "/claims?" + v.Encode()
	}
	if res.Page > 1 {
		data.PrevURL = pageURL(res.Page - 1)
	}
	if res.Page < res.TotalPages {
		data.NextURL = pageURL(res.Page + 1)
	}
	// Maksimal 7 nomor halaman di sekitar halaman aktif.
	lo, hi := max(1, res.Page-3), min(res.TotalPages, res.Page+3)
	for p := lo; p <= hi; p++ {
		data.Pages = append(data.Pages, pageLink{Label: strconv.Itoa(p), URL: pageURL(p), Current: p == res.Page})
	}

	s.render(w, r, http.StatusOK, "claims_list", pageData{Title: "Klaim", Active: "/claims", Data: data})
}

// --- Formulir buat/ubah ---

type claimFormData struct {
	Heading      string
	Action       string
	Submit       string
	Cancel       string
	ClaimNumber  string
	Input        claims.Input
	Errors       map[string]string
	Products     []string
	Officers     []claims.Officer
	CanReassign  bool
	AssigneeName string
	MaxDate      string
}

func (s *Server) newFormData(ctx context.Context, u *auth.User) (claimFormData, error) {
	d := claimFormData{
		Products:    claims.ProductTypes,
		CanReassign: authz.Can(u.Role, authz.PermClaimsReassign),
		MaxDate:     s.claims.Now().In(claims.Location).Format(claims.DateLayout),
	}
	if d.CanReassign {
		var err error
		if d.Officers, err = s.claims.Officers(ctx, actorOf(u)); err != nil {
			return d, err
		}
	}
	return d, nil
}

func formInput(r *http.Request) claims.Input {
	return claims.Input{
		ClaimantName: r.PostFormValue("claimant_name"),
		PolicyNumber: r.PostFormValue("policy_number"),
		ProductType:  r.PostFormValue("product_type"),
		ReceivedAt:   r.PostFormValue("received_at"),
		AssignedTo:   r.PostFormValue("assigned_to"),
	}
}

func (s *Server) claimNew(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	if err := authz.Require(u.Role, authz.PermClaimsCreate); err != nil {
		s.forbidden(w, r, authz.PermClaimsCreate)
		return
	}
	d, err := s.newFormData(r.Context(), u)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d.Heading, d.Action, d.Submit, d.Cancel = "Buat klaim", "/claims", "Simpan klaim", "/claims"
	d.Input = claims.Input{ProductType: claims.ProductHealthSim, ReceivedAt: d.MaxDate}
	if !d.CanReassign {
		d.AssigneeName = u.Name
	}
	s.render(w, r, http.StatusOK, "claim_form", pageData{Title: "Buat klaim", Active: "/claims", Data: d})
}

func (s *Server) claimCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	in := formInput(r)
	c, err := s.claims.Create(ctx, actorOf(u), in)
	var ve *claims.ValidationError
	if errors.As(err, &ve) {
		d, ferr := s.newFormData(ctx, u)
		if ferr != nil {
			s.serverError(w, r, ferr)
			return
		}
		d.Heading, d.Action, d.Submit, d.Cancel = "Buat klaim", "/claims", "Simpan klaim", "/claims"
		d.Input, d.Errors = in, ve.Fields
		if !d.CanReassign {
			d.AssigneeName = u.Name
		}
		s.render(w, r, http.StatusUnprocessableEntity, "claim_form",
			pageData{Title: "Buat klaim", Active: "/claims", Error: "Periksa kembali isian yang ditandai.", Data: d})
		return
	}
	if err != nil {
		s.claimServiceError(w, r, err)
		return
	}
	s.sm.Put(ctx, sessFlash, fmt.Sprintf("Klaim %s berhasil dibuat.", c.ClaimNumber))
	http.Redirect(w, r, fmt.Sprintf("/claims/%d", c.ID), http.StatusSeeOther)
}

func (s *Server) claimEdit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	id := claimIDFrom(ctx)
	det, err := s.claims.Get(ctx, actorOf(u), id)
	if err != nil {
		s.claimServiceError(w, r, err)
		return
	}
	if det.Status == claims.StatusClosed {
		s.sm.Put(ctx, sessFlash, "Klaim sudah ditutup sehingga datanya tidak dapat diubah.")
		http.Redirect(w, r, fmt.Sprintf("/claims/%d", id), http.StatusSeeOther)
		return
	}
	d, err := s.newFormData(ctx, u)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	c := det.Claim
	s.fillEditForm(&d, c.ID, c.ClaimNumber, det.AssigneeName)
	d.Input = claims.Input{
		ClaimantName: c.ClaimantName,
		PolicyNumber: c.PolicyNumber,
		ProductType:  c.ProductType,
		ReceivedAt:   c.ReceivedAt.In(claims.Location).Format(claims.DateLayout),
	}
	if c.AssignedTo != nil {
		d.Input.AssignedTo = strconv.FormatInt(*c.AssignedTo, 10)
	}
	s.render(w, r, http.StatusOK, "claim_form", pageData{Title: "Ubah " + c.ClaimNumber, Active: "/claims", Data: d})
}

func (s *Server) fillEditForm(d *claimFormData, id int64, number, assignee string) {
	d.Heading = "Ubah data klaim"
	d.Action = fmt.Sprintf("/claims/%d", id)
	d.Cancel = d.Action
	d.Submit = "Simpan perubahan"
	d.ClaimNumber = number
	if !d.CanReassign {
		d.AssigneeName = assignee
	}
}

func (s *Server) claimUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	id := claimIDFrom(ctx)
	in := formInput(r)
	c, err := s.claims.Update(ctx, actorOf(u), id, in)
	var ve *claims.ValidationError
	switch {
	case errors.As(err, &ve):
		det, gerr := s.claims.Get(ctx, actorOf(u), id)
		if gerr != nil {
			s.claimServiceError(w, r, gerr)
			return
		}
		d, ferr := s.newFormData(ctx, u)
		if ferr != nil {
			s.serverError(w, r, ferr)
			return
		}
		s.fillEditForm(&d, id, det.Claim.ClaimNumber, det.AssigneeName)
		d.Input, d.Errors = in, ve.Fields
		s.render(w, r, http.StatusUnprocessableEntity, "claim_form",
			pageData{Title: "Ubah " + det.Claim.ClaimNumber, Active: "/claims", Error: "Periksa kembali isian yang ditandai.", Data: d})
		return
	case errors.Is(err, claims.ErrClosed):
		s.sm.Put(ctx, sessFlash, "Klaim sudah ditutup sehingga datanya tidak dapat diubah.")
		http.Redirect(w, r, fmt.Sprintf("/claims/%d", id), http.StatusSeeOther)
		return
	case err != nil:
		s.claimServiceError(w, r, err)
		return
	}
	s.sm.Put(ctx, sessFlash, fmt.Sprintf("Data klaim %s tersimpan.", c.ClaimNumber))
	http.Redirect(w, r, fmt.Sprintf("/claims/%d", id), http.StatusSeeOther)
}

// --- Detail & transisi status ---

type claimDetailData struct {
	D              claims.Detail
	CanEdit        bool
	TransitionTo   string
	TransitionNote string
}

func (s *Server) renderDetail(w http.ResponseWriter, r *http.Request, status int, errMsg, to, note string) {
	ctx := r.Context()
	u := userFrom(ctx)
	det, err := s.claims.Get(ctx, actorOf(u), claimIDFrom(ctx))
	if err != nil {
		s.claimServiceError(w, r, err)
		return
	}
	data := claimDetailData{
		D:              det,
		CanEdit:        authz.Can(u.Role, authz.PermClaimsEdit) && det.Status != claims.StatusClosed,
		TransitionTo:   to,
		TransitionNote: note,
	}
	s.render(w, r, status, "claim_detail", pageData{
		Title: det.Claim.ClaimNumber, Active: "/claims", Error: errMsg, Data: data,
	})
}

func (s *Server) claimDetail(w http.ResponseWriter, r *http.Request) {
	s.renderDetail(w, r, http.StatusOK, "", "", "")
}

func (s *Server) claimTransition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	id := claimIDFrom(ctx)
	to := claims.Status(r.PostFormValue("to"))
	reason := r.PostFormValue("reason")

	c, err := s.claims.TransitionStatus(ctx, actorOf(u), id, to, reason)
	var te *claims.TransitionError
	switch {
	case errors.As(err, &te):
		s.renderDetail(w, r, http.StatusUnprocessableEntity, "Perubahan status tidak dapat dilakukan: "+te.Error()+".", string(to), reason)
		return
	case errors.Is(err, claims.ErrReasonRequired):
		s.renderDetail(w, r, http.StatusUnprocessableEntity,
			fmt.Sprintf("Alasan wajib diisi untuk status %q.", to.Label()), string(to), reason)
		return
	case errors.Is(err, claims.ErrReasonTooLong):
		s.renderDetail(w, r, http.StatusUnprocessableEntity,
			fmt.Sprintf("Alasan maksimal %d karakter.", claims.MaxReasonLength), string(to), reason)
		return
	case err != nil:
		s.claimServiceError(w, r, err)
		return
	}
	s.sm.Put(ctx, sessFlash, fmt.Sprintf("Status klaim %s diubah menjadi %q.", c.ClaimNumber, claims.Status(c.Status).Label()))
	http.Redirect(w, r, fmt.Sprintf("/claims/%d", id), http.StatusSeeOther)
}
