// Package claims berisi logika bisnis manajemen klaim: pembuatan, perubahan
// data, daftar dengan filter, dan transisi status.
//
// Authorization dicek di sini (lapis service) selain di middleware HTTP.
// Seluruh perubahan ditulis bersama audit log dalam satu transaksi.
package claims

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
)

// PageSize adalah jumlah klaim per halaman daftar.
const PageSize = 20

// maxPage membatasi nomor halaman agar offset tidak overflow.
const maxPage = 100_000

// Masked menggantikan nilai field sensitif (nama pemohon, nomor polis) di
// audit log: audit mencatat bahwa field berubah tanpa menyimpan nilainya.
const Masked = "[DIUBAH]"

// Actor adalah pengguna yang melakukan aksi.
type Actor struct {
	UserID int64
	Role   authz.Role
}

// Service menangani klaim.
type Service struct {
	pool *pgxpool.Pool
	q    *db.Queries
	// audit di luar transaksi; dipakai untuk access_denied agar catatan
	// tidak ikut dibatalkan bersama transaksi.
	audit *audit.Logger
	log   *slog.Logger
	// Now dapat diganti di test.
	Now func() time.Time
}

// NewService membuat Service.
func NewService(pool *pgxpool.Pool, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	q := db.New(pool)
	return &Service{pool: pool, q: q, audit: audit.NewLogger(q), log: logger, Now: time.Now}
}

// deny mencatat access_denied pada lapis service lalu mengembalikan
// authz.ErrForbidden. Tidak ada PII yang dicatat.
func (s *Service) deny(ctx context.Context, a Actor, claimID *int64, op string) error {
	entity := audit.EntityClaim
	if claimID == nil {
		entity = audit.EntityRoute
	}
	if err := s.audit.Record(ctx, audit.Entry{
		UserID:     &a.UserID,
		EntityType: entity,
		EntityID:   claimID,
		Action:     audit.ActionAccessDenied,
		After:      map[string]string{"layer": "service", "operation": op},
	}); err != nil {
		s.log.Error("gagal menulis audit access_denied", "error", err)
	}
	return authz.ErrForbidden
}

// AssignedTo mengembalikan petugas klaim tanpa pemeriksaan izin. Hanya untuk
// middleware authorization (lapis pertama) yang memutuskan akses.
func (s *Service) AssignedTo(ctx context.Context, id int64) (*int64, error) {
	c, err := s.q.GetClaim(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return c.AssignedTo, nil
}

// Officer adalah pilihan petugas untuk penugasan.
type Officer struct {
	ID   int64
	Name string
}

// Officers mengembalikan petugas klaim aktif yang dapat ditugaskan.
func (s *Service) Officers(ctx context.Context, a Actor) ([]Officer, error) {
	if !authz.Can(a.Role, authz.PermClaimsView) {
		return nil, s.deny(ctx, a, nil, "list_officers")
	}
	rows, err := s.q.ListActiveUsersByRole(ctx, string(authz.RoleClaimsOfficer))
	if err != nil {
		return nil, err
	}
	out := make([]Officer, len(rows))
	for i, r := range rows {
		out[i] = Officer{ID: r.ID, Name: r.Name}
	}
	return out, nil
}

// checkAssignee memastikan petugas adalah Claims Officer aktif.
func checkAssignee(ctx context.Context, q *db.Queries, id int64) error {
	u, err := q.GetUserByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!u.IsActive || u.Role != string(authz.RoleClaimsOfficer))) {
		return &ValidationError{Fields: map[string]string{"assigned_to": "Petugas harus Claims Officer yang aktif."}}
	}
	return err
}

// Create membuat klaim baru berstatus draft dengan nomor otomatis.
// Claims Officer selalu ditugaskan ke dirinya sendiri.
func (s *Service) Create(ctx context.Context, a Actor, in Input) (db.Claim, error) {
	if !authz.Can(a.Role, authz.PermClaimsCreate) {
		return db.Claim{}, s.deny(ctx, a, nil, "create")
	}
	if !authz.Can(a.Role, authz.PermClaimsReassign) {
		in.AssignedTo = strconv.FormatInt(a.UserID, 10)
	}
	f, verr := validate(in, s.Now())
	if verr != nil {
		return db.Claim{}, verr
	}

	var out db.Claim
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		if err := checkAssignee(ctx, q, *f.assignedTo); err != nil {
			return err
		}
		n, err := q.NextClaimNumber(ctx, NumberPrefix)
		if err != nil {
			return fmt.Errorf("nomor klaim: %w", err)
		}
		c, err := q.InsertClaim(ctx, db.InsertClaimParams{
			ClaimNumber:  FormatNumber(n),
			PolicyNumber: f.policy,
			ClaimantName: f.name,
			ProductType:  f.product,
			ReceivedAt:   f.receivedAt,
			Status:       string(StatusDraft),
			AssignedTo:   f.assignedTo,
		})
		if err != nil {
			return fmt.Errorf("insert klaim: %w", err)
		}
		draft := string(StatusDraft)
		if _, err := q.InsertClaimStatusHistory(ctx, db.InsertClaimStatusHistoryParams{
			ClaimID: c.ID, NewStatus: draft, ChangedBy: &a.UserID,
		}); err != nil {
			return fmt.Errorf("riwayat status: %w", err)
		}
		// Nama pemohon & nomor polis tidak dicatat (PII).
		if err := audit.NewLogger(q).Record(ctx, audit.Entry{
			UserID: &a.UserID, EntityType: audit.EntityClaim, EntityID: &c.ID,
			Action: audit.ActionClaimCreated,
			After: map[string]any{
				"claim_number": c.ClaimNumber,
				"product_type": c.ProductType,
				"received_at":  c.ReceivedAt.In(Location).Format(DateLayout),
				"status":       c.Status,
				"assigned_to":  c.AssignedTo,
			},
		}); err != nil {
			return err
		}
		out = c
		return nil
	})
	return out, err
}

// Update mengubah data klaim (bukan status). Audit hanya memuat field yang
// berubah; nilai nama pemohon dan nomor polis disamarkan.
func (s *Service) Update(ctx context.Context, a Actor, id int64, in Input) (db.Claim, error) {
	if !authz.Can(a.Role, authz.PermClaimsEdit) {
		return db.Claim{}, s.deny(ctx, a, &id, "update")
	}
	var out db.Claim
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		cur, err := q.GetClaimForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !authz.CanAccessClaim(a.Role, a.UserID, cur.AssignedTo) {
			return authz.ErrForbidden
		}
		if Status(cur.Status) == StatusClosed {
			return ErrClosed
		}
		if !authz.Can(a.Role, authz.PermClaimsReassign) {
			in.AssignedTo = ""
			if cur.AssignedTo != nil {
				in.AssignedTo = strconv.FormatInt(*cur.AssignedTo, 10)
			}
		}
		f, verr := validate(in, s.Now())
		if verr != nil {
			return verr
		}

		before, after := map[string]any{}, map[string]any{}
		if f.name != cur.ClaimantName {
			before["claimant_name"], after["claimant_name"] = Masked, Masked
		}
		if f.policy != cur.PolicyNumber {
			before["policy_number"], after["policy_number"] = Masked, Masked
		}
		if f.product != cur.ProductType {
			before["product_type"], after["product_type"] = cur.ProductType, f.product
		}
		if !f.receivedAt.Equal(cur.ReceivedAt) {
			before["received_at"] = cur.ReceivedAt.In(Location).Format(DateLayout)
			after["received_at"] = f.receivedAt.Format(DateLayout)
		}
		if cur.AssignedTo == nil || *cur.AssignedTo != *f.assignedTo {
			if err := checkAssignee(ctx, q, *f.assignedTo); err != nil {
				return err
			}
			before["assigned_to"], after["assigned_to"] = cur.AssignedTo, *f.assignedTo
		}
		if len(after) == 0 {
			out = cur
			return nil
		}

		c, err := q.UpdateClaimFields(ctx, db.UpdateClaimFieldsParams{
			ID: id, PolicyNumber: f.policy, ClaimantName: f.name, ProductType: f.product,
			ReceivedAt: f.receivedAt, AssignedTo: f.assignedTo,
		})
		if err != nil {
			return fmt.Errorf("update klaim: %w", err)
		}
		if err := audit.NewLogger(q).Record(ctx, audit.Entry{
			UserID: &a.UserID, EntityType: audit.EntityClaim, EntityID: &id,
			Action: audit.ActionClaimUpdated, Before: before, After: after,
		}); err != nil {
			return err
		}
		out = c
		return nil
	})
	if errors.Is(err, authz.ErrForbidden) {
		return db.Claim{}, s.deny(ctx, a, &id, "update")
	}
	return out, err
}

// TransitionStatus mengubah status klaim atas permintaan pengguna.
// Status yang khusus sistem (processing, needs_review) ditolak.
func (s *Service) TransitionStatus(ctx context.Context, a Actor, id int64, to Status, reason string) (db.Claim, error) {
	if !authz.Can(a.Role, authz.PermClaimsTransition) {
		return db.Claim{}, s.deny(ctx, a, &id, "transition")
	}
	c, err := s.transition(ctx, &a, id, to, reason)
	if errors.Is(err, authz.ErrForbidden) {
		return db.Claim{}, s.deny(ctx, a, &id, "transition")
	}
	return c, err
}

// SystemTransition mengubah status atas nama sistem (changed_by NULL),
// mis. saat pemrosesan dokumen dimulai/selesai. Disiapkan untuk tahap
// berikutnya; hanya transisi bertanda sistem yang diizinkan.
func (s *Service) SystemTransition(ctx context.Context, id int64, to Status, reason string) (db.Claim, error) {
	return s.transition(ctx, nil, id, to, reason)
}

// transition adalah SATU-SATUNYA jalur perubahan status. Dalam satu
// transaksi: kunci baris klaim, validasi peta transisi, ubah status, tulis
// claim_status_histories, dan tulis audit_logs. a == nil berarti sistem.
func (s *Service) transition(ctx context.Context, a *Actor, id int64, to Status, reason string) (db.Claim, error) {
	if !to.Valid() {
		return db.Claim{}, &TransitionError{To: to}
	}
	r, err := normalizeReason(to, reason)
	if err != nil {
		return db.Claim{}, err
	}

	var out db.Claim
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		cur, err := q.GetClaimForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if a != nil && !authz.CanAccessClaim(a.Role, a.UserID, cur.AssignedTo) {
			return authz.ErrForbidden
		}

		from := Status(cur.Status)
		kind, ok := transitions[from][to]
		if !ok {
			return &TransitionError{From: from, To: to}
		}
		if kind == bySystem && a != nil {
			return &TransitionError{From: from, To: to, SystemOnly: true}
		}
		if kind == byUser && a == nil {
			return &TransitionError{From: from, To: to}
		}

		c, err := q.UpdateClaimStatus(ctx, db.UpdateClaimStatusParams{
			ID: id, OldStatus: string(from), NewStatus: string(to),
		})
		if err != nil {
			return fmt.Errorf("update status: %w", err)
		}
		var uid *int64
		actor := "system"
		if a != nil {
			uid = &a.UserID
			actor = "user"
		}
		fromStr := string(from)
		h, err := q.InsertClaimStatusHistory(ctx, db.InsertClaimStatusHistoryParams{
			ClaimID: id, OldStatus: &fromStr, NewStatus: string(to), ChangedBy: uid, Reason: r,
		})
		if err != nil {
			return fmt.Errorf("riwayat status: %w", err)
		}
		// Isi alasan tidak disalin ke audit (bisa berisi data pribadi);
		// audit merujuk ke baris riwayat lewat history_id.
		if err := audit.NewLogger(q).Record(ctx, audit.Entry{
			UserID: uid, EntityType: audit.EntityClaim, EntityID: &id,
			Action: audit.ActionStatusChanged,
			Before: map[string]any{"status": from},
			After: map[string]any{
				"status": to, "history_id": h.ID, "actor": actor, "reason_provided": r != nil,
			},
		}); err != nil {
			return err
		}
		out = c
		return nil
	})
	return out, err
}

// HistoryEntry adalah satu baris timeline status.
type HistoryEntry struct {
	OldStatus     Status // kosong untuk entri pembuatan
	NewStatus     Status
	ChangedByName string
	BySystem      bool
	Reason        string
	CreatedAt     time.Time
}

// Detail adalah data halaman detail klaim.
type Detail struct {
	Claim        db.Claim
	Status       Status
	AssigneeName string
	History      []HistoryEntry
	// NextStatuses: status tujuan yang dapat dipilih pengguna saat ini.
	NextStatuses []Status
}

// Get mengambil detail klaim beserta timeline status.
func (s *Service) Get(ctx context.Context, a Actor, id int64) (Detail, error) {
	if !authz.Can(a.Role, authz.PermClaimsView) {
		return Detail{}, s.deny(ctx, a, &id, "view")
	}
	c, err := s.q.GetClaim(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}
	if !authz.CanAccessClaim(a.Role, a.UserID, c.AssignedTo) {
		return Detail{}, s.deny(ctx, a, &id, "view")
	}

	d := Detail{Claim: c, Status: Status(c.Status)}
	if c.AssignedTo != nil {
		u, err := s.q.GetUserByID(ctx, *c.AssignedTo)
		if err != nil {
			return Detail{}, err
		}
		d.AssigneeName = u.Name
	}
	rows, err := s.q.ListClaimStatusHistories(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	for _, r := range rows {
		e := HistoryEntry{NewStatus: Status(r.NewStatus), BySystem: r.ChangedBy == nil, CreatedAt: r.CreatedAt}
		if r.OldStatus != nil {
			e.OldStatus = Status(*r.OldStatus)
		}
		if r.ChangedByName != nil {
			e.ChangedByName = *r.ChangedByName
		}
		if r.Reason != nil {
			e.Reason = *r.Reason
		}
		d.History = append(d.History, e)
	}
	if authz.Can(a.Role, authz.PermClaimsTransition) {
		d.NextStatuses = UserTransitions(d.Status)
	}
	return d, nil
}

// Filter adalah kriteria daftar klaim. Nilai kosong berarti tanpa filter.
type Filter struct {
	Search       string // awalan nomor klaim atau nomor polis
	Status       Status
	ProductType  string
	AssignedTo   *int64 // diabaikan untuk Claims Officer
	ReceivedFrom *time.Time
	ReceivedTo   *time.Time // inklusif (sampai akhir hari)
	Page         int
}

// ListItem adalah satu baris daftar klaim.
type ListItem struct {
	ID           int64
	ClaimNumber  string
	PolicyNumber string
	ClaimantName string
	ProductType  string
	ReceivedAt   time.Time
	Status       Status
	AssigneeName string
}

// Page adalah satu halaman hasil daftar klaim.
type Page struct {
	Items      []ListItem
	Total      int64
	Page       int
	PageSize   int
	TotalPages int
}

// List mengembalikan daftar klaim sesuai filter dan cakupan akses: Claims
// Officer hanya melihat klaim yang ditugaskan kepadanya.
func (s *Service) List(ctx context.Context, a Actor, f Filter) (Page, error) {
	if !authz.Can(a.Role, authz.PermClaimsView) {
		return Page{}, s.deny(ctx, a, nil, "list")
	}
	assigned := f.AssignedTo
	if !authz.Can(a.Role, authz.PermClaimsViewAll) {
		assigned = &a.UserID
	}
	var status, product *string
	if f.Status.Valid() {
		st := string(f.Status)
		status = &st
	}
	if validProduct(f.ProductType) {
		product = &f.ProductType
	}
	var from, before *time.Time
	if f.ReceivedFrom != nil {
		t := startOfDay(*f.ReceivedFrom)
		from = &t
	}
	if f.ReceivedTo != nil {
		t := startOfDay(*f.ReceivedTo).AddDate(0, 0, 1)
		before = &t
	}
	search := searchPattern(f.Search)

	total, err := s.q.CountClaims(ctx, db.CountClaimsParams{
		Search: search, Status: status, ProductType: product, AssignedTo: assigned,
		ReceivedFrom: from, ReceivedBefore: before,
	})
	if err != nil {
		return Page{}, err
	}
	page := f.Page
	if page < 1 {
		page = 1
	}
	if page > maxPage {
		page = maxPage
	}
	p := Page{Total: total, Page: page, PageSize: PageSize,
		TotalPages: int((total + PageSize - 1) / PageSize)}

	rows, err := s.q.ListClaims(ctx, db.ListClaimsParams{
		Search: search, Status: status, ProductType: product, AssignedTo: assigned,
		ReceivedFrom: from, ReceivedBefore: before,
		PageLimit: PageSize, PageOffset: int32((page - 1) * PageSize),
	})
	if err != nil {
		return Page{}, err
	}
	for _, r := range rows {
		it := ListItem{
			ID: r.ID, ClaimNumber: r.ClaimNumber, PolicyNumber: r.PolicyNumber,
			ClaimantName: r.ClaimantName, ProductType: r.ProductType,
			ReceivedAt: r.ReceivedAt, Status: Status(r.Status),
		}
		if r.AssigneeName != nil {
			it.AssigneeName = *r.AssigneeName
		}
		p.Items = append(p.Items, it)
	}
	return p, nil
}

// Field klaim yang boleh diisi dari nilai terdeteksi.
const (
	DetectedClaimantName = "claimant_name"
	DetectedPolicyNumber = "policy_number"
)

var (
	// ErrDetectedNotFound: field terekstrak tidak ada atau bukan milik klaim.
	ErrDetectedNotFound = errors.New("nilai terdeteksi tidak ditemukan")
	// ErrDetectedNotApplicable: field tidak boleh diterapkan (status bukan
	// found, bukan dari formulir klaim, atau bukan nama/nomor polis).
	ErrDetectedNotApplicable = errors.New("nilai terdeteksi tidak dapat diterapkan")
	// ErrDetectedNoChange: nilai sudah sama dengan data klaim.
	ErrDetectedNoChange = errors.New("nilai sudah sama dengan data klaim")
)

// ValidDetectedValue melaporkan apakah nilai terdeteksi lolos validasi
// data klaim (dipakai UI untuk menampilkan tombol "Terapkan").
func ValidDetectedValue(field, v string) bool {
	in := Input{ClaimantName: "Nama Uji", PolicyNumber: "POL-DEMO-0000", ProductType: ProductHealthSim,
		ReceivedAt: "2020-01-01", AssignedTo: "1"}
	switch field {
	case DetectedClaimantName:
		in.ClaimantName = v
	case DetectedPolicyNumber:
		in.PolicyNumber = v
	default:
		return false
	}
	_, verr := validate(in, time.Now())
	return verr == nil
}

// ApplyDetectedValue menerapkan nilai terdeteksi (nama atau nomor polis
// dari formulir klaim) ke data klaim atas konfirmasi petugas. Nilai diambil
// dari database, bukan dari input pengguna, dan divalidasi dengan aturan
// yang sama seperti formulir ubah data. Status klaim tidak berubah. Audit
// extracted_value_applied tidak memuat nilai (disamarkan).
func (s *Service) ApplyDetectedValue(ctx context.Context, a Actor, claimID, fieldID int64) (db.Claim, string, error) {
	if !authz.Can(a.Role, authz.PermClaimsEdit) {
		return db.Claim{}, "", s.deny(ctx, a, &claimID, "apply_detected_value")
	}
	var out db.Claim
	var key string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		cur, err := q.GetClaimForUpdate(ctx, claimID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !authz.CanAccessClaim(a.Role, a.UserID, cur.AssignedTo) {
			return authz.ErrForbidden
		}
		if Status(cur.Status) == StatusClosed {
			return ErrClosed
		}
		f, err := q.GetExtractedFieldForClaim(ctx, db.GetExtractedFieldForClaimParams{ID: fieldID, ClaimID: claimID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDetectedNotFound
		}
		if err != nil {
			return err
		}
		key = f.FieldKey
		if f.DocumentType != "formulir_klaim" || f.ProcessingStatus != "completed" ||
			f.Status != "found" || f.FieldValue == nil ||
			(f.FieldKey != DetectedClaimantName && f.FieldKey != DetectedPolicyNumber) {
			return ErrDetectedNotApplicable
		}

		in := Input{
			ClaimantName: cur.ClaimantName, PolicyNumber: cur.PolicyNumber, ProductType: cur.ProductType,
			ReceivedAt: cur.ReceivedAt.In(Location).Format(DateLayout),
		}
		if cur.AssignedTo != nil {
			in.AssignedTo = strconv.FormatInt(*cur.AssignedTo, 10)
		}
		if f.FieldKey == DetectedClaimantName {
			in.ClaimantName = *f.FieldValue
		} else {
			in.PolicyNumber = *f.FieldValue
		}
		v, verr := validate(in, s.Now())
		if verr != nil {
			return verr
		}
		if v.name == cur.ClaimantName && v.policy == cur.PolicyNumber {
			return ErrDetectedNoChange
		}
		c, err := q.UpdateClaimFields(ctx, db.UpdateClaimFieldsParams{
			ID: claimID, PolicyNumber: v.policy, ClaimantName: v.name, ProductType: cur.ProductType,
			ReceivedAt: cur.ReceivedAt, AssignedTo: cur.AssignedTo,
		})
		if err != nil {
			return fmt.Errorf("terapkan nilai terdeteksi: %w", err)
		}
		if err := audit.NewLogger(q).Record(ctx, audit.Entry{
			UserID: &a.UserID, EntityType: audit.EntityClaim, EntityID: &claimID,
			Action: audit.ActionExtractedValueApplied,
			Before: map[string]any{f.FieldKey: Masked},
			After: map[string]any{
				f.FieldKey: Masked, "field": f.FieldKey, "document_id": f.DocumentID,
				"extracted_field_id": f.ID, "confidence": f.Confidence,
			},
		}); err != nil {
			return err
		}
		out = c
		return nil
	})
	if errors.Is(err, authz.ErrForbidden) {
		return db.Claim{}, "", s.deny(ctx, a, &claimID, "apply_detected_value")
	}
	return out, key, err
}
