package validation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
)

// Pemicu validasi (dicatat di audit).
const (
	TriggerDocumentsProcessed = "documents_processed"
	TriggerManual             = "manual"
)

// Error domain.
var (
	ErrNotFound         = errors.New("klaim tidak ditemukan")
	ErrDocumentsPending = errors.New("masih ada dokumen yang sedang diproses; validasi dijalankan otomatis setelah semuanya selesai")
	ErrClaimClosed      = errors.New("klaim sudah ditutup")
	ErrQueueUnavailable = errors.New("antrean validasi tidak tersedia")
)

// Queue memasukkan job validasi dalam transaksi pemanggil.
type Queue interface {
	EnqueueValidationTx(ctx context.Context, tx pgx.Tx, claimID int64, trigger string) error
}

// Service menjalankan validasi dan menyediakan temuan untuk UI.
type Service struct {
	pool   *pgxpool.Pool
	q      *db.Queries
	claims *claims.Service
	log    *slog.Logger
	queue  Queue
	// Now dapat diganti di test (tanggal "hari ini" untuk aturan tanggal).
	Now func() time.Time
}

// NewService membuat Service.
func NewService(pool *pgxpool.Pool, cs *claims.Service, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{pool: pool, q: db.New(pool), claims: cs, log: logger, Now: time.Now}
}

// SetQueue memasang antrean untuk RequestRun.
func (s *Service) SetQueue(q Queue) { s.queue = q }

// RunResult meringkas satu proses validasi.
type RunResult struct {
	Skipped       string // alasan dilewati; kosong bila validasi berjalan
	Open          int
	Created       int
	Reopened      int
	Resolved      int
	StatusChanged bool
}

// Run menjalankan seluruh aturan aktif untuk satu klaim (oleh sistem).
//
// Idempoten: temuan diidentifikasi fingerprint kondisi; kondisi yang sama
// memperbarui baris yang ada, kondisi yang hilang menjadi resolved, dan
// temuan dismissed tidak dibuka kembali. Proses validasi untuk klaim yang
// sama berjalan berurutan (baris klaim dikunci selama transaksi).
func (s *Service) Run(ctx context.Context, claimID int64, trigger string) (RunResult, error) {
	var res RunResult
	start := time.Now()
	var status claims.Status
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		c, err := q.GetClaimForUpdate(ctx, claimID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		status = claims.Status(c.Status)
		if status == claims.StatusClosed || status == claims.StatusDraft {
			res.Skipped = "claim_" + string(status)
			return nil
		}
		pending, err := q.CountUnfinishedDocuments(ctx, claimID)
		if err != nil {
			return err
		}
		if pending > 0 {
			// Job baru otomatis dimasukkan saat dokumen terakhir selesai.
			res.Skipped = "documents_pending"
			return nil
		}
		rules, err := s.activeRules(ctx, q)
		if err != nil {
			return err
		}
		versions := map[string]int{}
		for _, r := range rules {
			versions[r.Code] = r.Version
		}
		if err := s.audit(ctx, q, claimID, audit.ActionValidationRunStarted, map[string]any{
			"trigger": trigger, "rule_versions": versions,
		}); err != nil {
			return err
		}
		in, err := s.loadInput(ctx, q, c)
		if err != nil {
			return err
		}
		found, err := Evaluate(in, rules)
		if err != nil {
			return err
		}
		if err := s.persist(ctx, q, claimID, found, &res); err != nil {
			return err
		}
		byCode := map[string]int{}
		for _, f := range found {
			byCode[f.Code]++
		}
		return s.audit(ctx, q, claimID, audit.ActionValidationRunCompleted, map[string]any{
			"trigger": trigger, "rule_versions": versions, "findings_by_code": byCode,
			"open": res.Open, "created": res.Created, "reopened": res.Reopened, "resolved": res.Resolved,
			"duration_ms": time.Since(start).Milliseconds(),
		})
	})
	if err != nil || res.Skipped != "" {
		return res, err
	}
	// Status klaim diubah setelah commit (SystemTransition memakai
	// transaksinya sendiri). Hanya needs_review, hanya dari status antrean;
	// need_documents TIDAK PERNAH di-set otomatis.
	if res.Open > 0 && (status == claims.StatusSubmitted || status == claims.StatusProcessing) {
		_, err := s.claims.SystemTransition(ctx, claimID, claims.StatusNeedsReview,
			"Validasi otomatis: ada temuan yang perlu ditinjau petugas.")
		var te *claims.TransitionError
		switch {
		case err == nil:
			res.StatusChanged = true
		case errors.As(err, &te):
			// Status sudah diubah petugas di antara commit dan transisi.
		default:
			return res, err
		}
	}
	s.log.Info("validasi klaim selesai", "claim_id", claimID, "trigger", trigger, "open", res.Open,
		"created", res.Created, "resolved", res.Resolved, "status_changed", res.StatusChanged)
	return res, nil
}

func (s *Service) activeRules(ctx context.Context, q *db.Queries) ([]Rule, error) {
	rows, err := q.ListActiveValidationRules(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Rule, 0, len(rows))
	for _, r := range rows {
		out = append(out, Rule{ID: r.ID, Code: r.RuleCode, Version: int(r.Version), Config: r.RuleConfig})
	}
	return out, nil
}

func (s *Service) loadInput(ctx context.Context, q *db.Queries, c db.Claim) (Input, error) {
	in := Input{
		Claim: ClaimRecord{PolicyNumber: c.PolicyNumber, ClaimantName: c.ClaimantName, ProductType: c.ProductType},
		Today: s.Now().In(claims.Location).Format(claims.DateLayout),
	}
	docs, err := q.ListValidationDocuments(ctx, c.ID)
	if err != nil {
		return in, err
	}
	idx := map[int64]int{}
	for _, d := range docs {
		doc := Document{ID: d.ID, Type: d.DocumentType, Status: d.ProcessingStatus}
		if d.PageCount != nil {
			doc.PageCount = int(*d.PageCount)
		}
		idx[d.ID] = len(in.Documents)
		in.Documents = append(in.Documents, doc)
	}
	fields, err := q.ListValidationFields(ctx, c.ID)
	if err != nil {
		return in, err
	}
	for _, f := range fields {
		i, ok := idx[f.DocumentID]
		if !ok {
			continue
		}
		fl := Field{Key: f.FieldKey, Status: f.Status, Confidence: f.Confidence}
		if f.FieldValue != nil {
			fl.Value = *f.FieldValue
		}
		if f.PageNumber != nil {
			fl.Page = int(*f.PageNumber)
		}
		if f.SourceExcerpt != nil {
			fl.Excerpt = *f.SourceExcerpt
		}
		in.Documents[i].Fields = append(in.Documents[i].Fields, fl)
	}
	pages, err := q.ListValidationPages(ctx, c.ID)
	if err != nil {
		return in, err
	}
	for _, p := range pages {
		if i, ok := idx[p.DocumentID]; ok {
			in.Documents[i].Pages = append(in.Documents[i].Pages, Page{
				Number: int(p.PageNumber), Engine: p.Engine, MeanConfidence: p.MeanConfidence, WordCount: int(p.WordCount),
			})
		}
	}
	return in, nil
}

func (s *Service) persist(ctx context.Context, q *db.Queries, claimID int64, found []Finding, res *RunResult) error {
	existing, err := q.ListClaimFindingsForUpdate(ctx, claimID)
	if err != nil {
		return err
	}
	byFP := map[string]db.ValidationFinding{}
	for _, e := range existing {
		byFP[e.Fingerprint] = e
	}
	seen := map[string]bool{}
	for _, f := range found {
		if seen[f.Fingerprint] {
			continue // kondisi identik dalam satu proses: satu temuan
		}
		seen[f.Fingerprint] = true
		ev, err := json.Marshal(f.Evidence)
		if err != nil {
			return err
		}
		if e, ok := byFP[f.Fingerprint]; ok {
			if err := f.Validate(); err != nil {
				return err
			}
			row, err := q.RefreshFinding(ctx, db.RefreshFindingParams{
				ID: e.ID, RuleID: f.RuleID, RuleVersion: int32(f.RuleVersion),
				Severity: string(f.Severity), Message: f.Message, EvidenceJson: ev,
			})
			if err != nil {
				return fmt.Errorf("perbarui temuan: %w", err)
			}
			if e.Status == "resolved" {
				res.Reopened++
				if err := s.auditFinding(ctx, q, row, audit.ActionFindingCreated, map[string]any{"reopened": true}); err != nil {
					return err
				}
			}
			if row.Status == "open" {
				res.Open++
			}
			continue
		}
		row, err := InsertFinding(ctx, q, claimID, f)
		if err != nil {
			return err
		}
		res.Created++
		res.Open++
		if err := s.auditFinding(ctx, q, row, audit.ActionFindingCreated, nil); err != nil {
			return err
		}
	}
	// Kondisi yang tidak lagi terdeteksi: temuan open menjadi resolved.
	// Temuan dismissed (keputusan petugas, Tahap 7) tidak diubah.
	for _, e := range existing {
		if seen[e.Fingerprint] || e.Status != "open" {
			continue
		}
		if err := q.ResolveFinding(ctx, e.ID); err != nil {
			return err
		}
		res.Resolved++
		if err := s.auditFinding(ctx, q, e, audit.ActionFindingResolved, map[string]any{
			"resolved_by": "system", "reason": "condition_no_longer_detected",
		}); err != nil {
			return err
		}
	}
	return nil
}

// InsertFinding menyimpan satu temuan setelah Finding.Validate. Temuan
// tanpa evidence (atau evidence tidak lengkap) ditolak dan tidak disimpan.
func InsertFinding(ctx context.Context, q *db.Queries, claimID int64, f Finding) (db.ValidationFinding, error) {
	if err := f.Validate(); err != nil {
		return db.ValidationFinding{}, err
	}
	ev, err := json.Marshal(f.Evidence)
	if err != nil {
		return db.ValidationFinding{}, err
	}
	row, err := q.InsertFinding(ctx, db.InsertFindingParams{
		ClaimID: claimID, RuleID: f.RuleID, RuleVersion: int32(f.RuleVersion), FindingCode: f.Code,
		Severity: string(f.Severity), Message: f.Message, EvidenceJson: ev, Fingerprint: f.Fingerprint,
	})
	if err != nil {
		return db.ValidationFinding{}, fmt.Errorf("simpan temuan: %w", err)
	}
	return row, nil
}

func (s *Service) audit(ctx context.Context, q *db.Queries, claimID int64, action string, after map[string]any) error {
	return audit.NewLogger(q).Record(ctx, audit.Entry{
		EntityType: audit.EntityClaim, EntityID: &claimID, Action: action, After: after,
	})
}

// auditFinding: hanya ID, kode, severity, versi aturan. Tidak ada nilai
// evidence maupun pesan.
func (s *Service) auditFinding(ctx context.Context, q *db.Queries, f db.ValidationFinding, action string, extra map[string]any) error {
	after := map[string]any{
		"claim_id": f.ClaimID, "finding_code": f.FindingCode, "severity": f.Severity,
		"rule_id": f.RuleID, "rule_version": f.RuleVersion,
	}
	for k, v := range extra {
		after[k] = v
	}
	return audit.NewLogger(q).Record(ctx, audit.Entry{
		EntityType: audit.EntityFinding, EntityID: &f.ID, Action: action, After: after,
	})
}

// --- Akses pengguna ---

func (s *Service) authorize(ctx context.Context, a claims.Actor, claimID int64, perm authz.Permission, op string) (db.Claim, error) {
	deny := func() error {
		if err := audit.NewLogger(s.q).Record(ctx, audit.Entry{
			UserID: &a.UserID, EntityType: audit.EntityClaim, EntityID: &claimID,
			Action: audit.ActionAccessDenied,
			After:  map[string]string{"layer": "service", "operation": op},
		}); err != nil {
			s.log.Error("gagal menulis audit access_denied", "error", err)
		}
		return authz.ErrForbidden
	}
	if !authz.Can(a.Role, perm) {
		return db.Claim{}, deny()
	}
	c, err := s.q.GetClaim(ctx, claimID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Claim{}, ErrNotFound
	}
	if err != nil {
		return db.Claim{}, err
	}
	if !authz.CanAccessClaim(a.Role, a.UserID, c.AssignedTo) {
		return db.Claim{}, deny()
	}
	return c, nil
}

// RequestRun memasukkan job validasi ulang atas permintaan petugas.
func (s *Service) RequestRun(ctx context.Context, a claims.Actor, claimID int64) error {
	c, err := s.authorize(ctx, a, claimID, authz.PermValidationRun, "validation_run")
	if err != nil {
		return err
	}
	if claims.Status(c.Status) == claims.StatusClosed {
		return ErrClaimClosed
	}
	if s.queue == nil {
		return ErrQueueUnavailable
	}
	pending, err := s.q.CountUnfinishedDocuments(ctx, claimID)
	if err != nil {
		return err
	}
	if pending > 0 {
		return ErrDocumentsPending
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.queue.EnqueueValidationTx(ctx, tx, claimID, TriggerManual); err != nil {
			return err
		}
		return audit.NewLogger(s.q.WithTx(tx)).Record(ctx, audit.Entry{
			UserID: &a.UserID, EntityType: audit.EntityClaim, EntityID: &claimID,
			Action: audit.ActionValidationRunRequested, After: map[string]any{"trigger": TriggerManual},
		})
	})
}

// FindingView adalah temuan untuk UI.
type FindingView struct {
	ID          int64
	Code        string
	Severity    Severity
	Status      string
	Message     string
	RuleVersion int32
	Evidence    []Evidence
	CreatedAt   time.Time
	ResolvedAt  *time.Time
}

// SeverityLabel untuk UI.
func (f FindingView) SeverityLabel() string { return f.Severity.Label() }

// Label untuk UI.
func (s Severity) Label() string {
	switch s {
	case SeverityHigh:
		return "Tinggi"
	case SeverityMedium:
		return "Sedang"
	case SeverityLow:
		return "Rendah"
	}
	return string(s)
}

// StatusLabel untuk UI. Tidak memakai istilah "ditolak"/"tidak layak".
func (f FindingView) StatusLabel() string {
	switch f.Status {
	case "open":
		return "Perlu ditinjau"
	case "resolved":
		return "Kondisi tidak terdeteksi lagi"
	case "dismissed":
		return "Diabaikan petugas"
	}
	return f.Status
}

// Suggestion adalah saran tindak lanjut untuk petugas, bukan keputusan.
type Suggestion struct {
	Text string
	// NeedDocuments: saran meminta dokumen; status need_documents hanya
	// diubah bila petugas mengonfirmasi lewat form ubah status.
	NeedDocuments bool
}

// SuggestionFor mengembalikan saran tindak lanjut per kode temuan.
func SuggestionFor(code string) Suggestion {
	switch code {
	case CodeMissingDocument:
		return Suggestion{Text: "Pertimbangkan meminta dokumen yang belum ada kepada peserta.", NeedDocuments: true}
	case CodeLowQualityDocument:
		return Suggestion{Text: "Periksa dokumen secara manual; bila tidak terbaca, pertimbangkan meminta unggah ulang yang lebih jelas.", NeedDocuments: true}
	case CodeMissingField:
		return Suggestion{Text: "Periksa halaman dokumen secara manual untuk field yang belum terbaca."}
	case CodeNameMismatch:
		return Suggestion{Text: "Bandingkan ejaan nama pada sumber-sumber; konfirmasi ke peserta bila diperlukan."}
	case CodePolicyMismatch:
		return Suggestion{Text: "Verifikasi nomor polis pada sistem polis; perbaiki data klaim atau minta formulir yang benar."}
	case CodeDateInconsistency:
		return Suggestion{Text: "Konfirmasi tanggal ke fasilitas kesehatan; kemungkinan salah ketik perlu diperiksa."}
	case CodeNeedsManualVerification:
		return Suggestion{Text: "Cocokkan nilai langsung dengan pratinjau dokumen; sistem tidak cukup yakin membaca atau membandingkannya secara otomatis."}
	}
	return Suggestion{}
}

// Panel adalah data panel "Temuan" pada detail klaim.
type Panel struct {
	Findings    []FindingView
	OpenCount   int
	Suggestions []Suggestion
	// SuggestNeedDocuments: ada temuan open yang menyarankan permintaan
	// dokumen. Hanya saran; tidak ada perubahan status otomatis.
	SuggestNeedDocuments bool
}

// Findings mengembalikan temuan klaim untuk pengguna yang berhak.
func (s *Service) Findings(ctx context.Context, a claims.Actor, claimID int64) (Panel, error) {
	if _, err := s.authorize(ctx, a, claimID, authz.PermClaimsView, "findings_view"); err != nil {
		return Panel{}, err
	}
	rows, err := s.q.ListClaimFindings(ctx, claimID)
	if err != nil {
		return Panel{}, err
	}
	var p Panel
	sugg := map[string]bool{}
	for _, r := range rows {
		v := FindingView{
			ID: r.ID, Code: r.FindingCode, Severity: Severity(r.Severity), Status: r.Status,
			Message: r.Message, RuleVersion: r.RuleVersion, CreatedAt: r.CreatedAt, ResolvedAt: r.ResolvedAt,
		}
		if err := json.Unmarshal(r.EvidenceJson, &v.Evidence); err != nil {
			return Panel{}, fmt.Errorf("evidence temuan %d: %w", r.ID, err)
		}
		p.Findings = append(p.Findings, v)
		if r.Status != "open" {
			continue
		}
		p.OpenCount++
		if !sugg[r.FindingCode] {
			sugg[r.FindingCode] = true
			sg := SuggestionFor(r.FindingCode)
			if sg.Text != "" {
				p.Suggestions = append(p.Suggestions, sg)
			}
			if sg.NeedDocuments {
				p.SuggestNeedDocuments = true
			}
		}
	}
	sort.SliceStable(p.Suggestions, func(i, j int) bool {
		return p.Suggestions[i].NeedDocuments && !p.Suggestions[j].NeedDocuments
	})
	return p, nil
}
