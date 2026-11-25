// Package documents menangani unggah, penyimpanan privat, dan akses dokumen
// klaim. Tahap ini hanya menyimpan file dan metadata (processing_status =
// uploaded); OCR/ekstraksi dikerjakan di tahap berikutnya.
//
// Authorization dicek di sini (lapis service) selain di middleware HTTP.
// Audit tidak pernah memuat isi file, nama file asli, atau checksum lengkap.
package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/authz"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
)

// Type adalah jenis dokumen.
type Type string

const (
	TypeClaimForm   Type = "formulir_klaim"
	TypeIdentity    Type = "identitas"
	TypeMedicalNote Type = "surat_keterangan_medis"
	TypeReceipt     Type = "kuitansi"
	TypeOther       Type = "lainnya"
)

// Types mengembalikan jenis dokumen dalam urutan tampilan.
func Types() []Type {
	return []Type{TypeClaimForm, TypeIdentity, TypeMedicalNote, TypeReceipt, TypeOther}
}

// Valid melaporkan apakah t jenis yang dikenal.
func (t Type) Valid() bool {
	for _, v := range Types() {
		if v == t {
			return true
		}
	}
	return false
}

// Label untuk UI.
func (t Type) Label() string {
	switch t {
	case TypeClaimForm:
		return "Formulir klaim"
	case TypeIdentity:
		return "Identitas"
	case TypeMedicalNote:
		return "Surat keterangan medis"
	case TypeReceipt:
		return "Kuitansi"
	case TypeOther:
		return "Lainnya"
	}
	return "Tidak dikenal"
}

// ProcessingStatus dokumen.
const StatusUploaded = "uploaded"

// StatusLabel untuk UI.
func StatusLabel(s string) string {
	switch s {
	case StatusUploaded:
		return "Diunggah"
	case "queued":
		return "Dalam antrean"
	case "processing":
		return "Diproses"
	case "completed":
		return "Selesai diproses"
	case "failed":
		return "Gagal diproses"
	}
	return s
}

var (
	ErrNotFound = errors.New("dokumen tidak ditemukan")
	// ErrClaimClosed: klaim berstatus Ditutup tidak menerima dokumen baru.
	ErrClaimClosed = errors.New("klaim sudah ditutup")
)

// File adalah satu file kiriman pengguna.
type File struct {
	Name string // nama asli dari klien (tidak tepercaya)
	Data []byte
}

// FileError adalah penolakan satu file.
type FileError struct {
	Index int
	Name  string // nama yang sudah disanitasi, hanya untuk UI
	Err   error
}

// ValidationError dikembalikan bila input unggahan tidak valid. Bila ada
// satu file yang ditolak, tidak ada file yang disimpan (all-or-nothing).
type ValidationError struct {
	Form  string // kesalahan tingkat formulir (jenis dokumen, jumlah file)
	Files []FileError
}

func (e *ValidationError) Error() string { return "unggahan tidak valid" }

// DuplicateMatch adalah file kiriman yang isinya identik dengan dokumen yang
// sudah ada pada klaim yang sama (atau dengan file lain di unggahan yang sama).
type DuplicateMatch struct {
	Index        int
	Name         string // disanitasi, hanya untuk UI
	ExistingID   int64  // 0 bila duplikat sesama file dalam unggahan ini
	ExistingType Type
}

// DuplicateError meminta konfirmasi pengguna sebelum duplikat disimpan.
type DuplicateError struct{ Matches []DuplicateMatch }

func (e *DuplicateError) Error() string { return "dokumen duplikat terdeteksi" }

// Upload adalah permintaan unggah.
type Upload struct {
	ClaimID int64
	Type    Type
	Files   []File
	// ConfirmDuplicate: pengguna sudah melihat peringatan duplikat dan
	// tetap ingin menyimpan.
	ConfirmDuplicate bool
}

// Document adalah baris daftar dokumen.
type Document struct {
	ID           int64
	Type         Type
	OriginalName string
	MIME         string
	Size         int64
	Pages        int
	Status       string
	DuplicateOf  *int64
	UploaderName string
	CreatedAt    time.Time
}

// Service menangani dokumen klaim.
type Service struct {
	pool    *pgxpool.Pool
	q       *db.Queries
	audit   *audit.Logger
	storage *Storage
	log     *slog.Logger
}

// NewService membuat Service.
func NewService(pool *pgxpool.Pool, storage *Storage, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	q := db.New(pool)
	return &Service{pool: pool, q: q, audit: audit.NewLogger(q), storage: storage, log: logger}
}

// deny mencatat access_denied (tanpa PII) lalu mengembalikan authz.ErrForbidden.
func (s *Service) deny(ctx context.Context, a claims.Actor, claimID int64, op string) error {
	if err := s.audit.Record(ctx, audit.Entry{
		UserID: &a.UserID, EntityType: audit.EntityClaim, EntityID: &claimID,
		Action: audit.ActionAccessDenied,
		After:  map[string]string{"layer": "service", "operation": op},
	}); err != nil {
		s.log.Error("gagal menulis audit access_denied", "error", err)
	}
	return authz.ErrForbidden
}

// authorize memastikan actor punya izin perm dan boleh mengakses klaim.
func (s *Service) authorize(ctx context.Context, a claims.Actor, claimID int64, perm authz.Permission, op string) (db.Claim, error) {
	if !authz.Can(a.Role, perm) {
		return db.Claim{}, s.deny(ctx, a, claimID, op)
	}
	c, err := s.q.GetClaim(ctx, claimID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Claim{}, claims.ErrNotFound
	}
	if err != nil {
		return db.Claim{}, err
	}
	if !authz.CanAccessClaim(a.Role, a.UserID, c.AssignedTo) {
		return db.Claim{}, s.deny(ctx, a, claimID, op)
	}
	return c, nil
}

// List mengembalikan dokumen klaim.
func (s *Service) List(ctx context.Context, a claims.Actor, claimID int64) ([]Document, error) {
	if _, err := s.authorize(ctx, a, claimID, authz.PermDocumentsView, "document_list"); err != nil {
		return nil, err
	}
	rows, err := s.q.ListClaimDocuments(ctx, claimID)
	if err != nil {
		return nil, err
	}
	out := make([]Document, 0, len(rows))
	for _, r := range rows {
		d := Document{
			ID: r.ID, Type: Type(r.DocumentType), OriginalName: r.OriginalName, MIME: r.MimeType,
			Size: r.FileSize, Status: r.ProcessingStatus, DuplicateOf: r.DuplicateOf, CreatedAt: r.CreatedAt,
		}
		if r.PageCount != nil {
			d.Pages = int(*r.PageCount)
		}
		if r.UploaderName != nil {
			d.UploaderName = *r.UploaderName
		}
		out = append(out, d)
	}
	return out, nil
}

type validated struct {
	name     string
	info     Info
	data     []byte
	checksum string
}

// Upload memvalidasi lalu menyimpan file. Urutan: otorisasi → validasi
// seluruh file → cek duplikat → tulis ke storage → insert metadata + audit
// dalam satu transaksi (file dihapus lagi bila transaksi gagal).
func (s *Service) Upload(ctx context.Context, a claims.Actor, up Upload) ([]db.ClaimDocument, error) {
	c, err := s.authorize(ctx, a, up.ClaimID, authz.PermDocumentsUpload, "document_upload")
	if err != nil {
		return nil, err
	}
	if claims.Status(c.Status) == claims.StatusClosed {
		return nil, ErrClaimClosed
	}

	ve := &ValidationError{}
	switch {
	case !up.Type.Valid():
		ve.Form = "Pilih jenis dokumen."
	case len(up.Files) == 0:
		ve.Form = "Pilih minimal satu file."
	case len(up.Files) > MaxFiles:
		ve.Form = fmt.Sprintf("Maksimal %d file per unggahan.", MaxFiles)
	}
	if ve.Form != "" {
		return nil, ve
	}

	files := make([]validated, len(up.Files))
	for i, f := range up.Files {
		name := SanitizeName(f.Name)
		info, err := Validate(name, f.Data)
		if err != nil {
			ve.Files = append(ve.Files, FileError{Index: i, Name: name, Err: err})
			continue
		}
		sum := sha256.Sum256(f.Data)
		files[i] = validated{name: name, info: info, data: f.Data, checksum: hex.EncodeToString(sum[:])}
	}
	if len(ve.Files) > 0 {
		return nil, ve
	}

	// Deteksi duplikat: terhadap dokumen klaim ini dan sesama file kiriman.
	sums := make([]string, len(files))
	for i, f := range files {
		sums[i] = f.checksum
	}
	existing, err := s.q.ListClaimDocumentChecksums(ctx, db.ListClaimDocumentChecksumsParams{ClaimID: up.ClaimID, Checksums: sums})
	if err != nil {
		return nil, err
	}
	firstByChecksum := map[string]int64{}
	for _, e := range existing {
		if _, ok := firstByChecksum[e.Checksum]; !ok {
			firstByChecksum[e.Checksum] = e.ID
		}
	}
	dupOf := make([]*int64, len(files))
	var matches []DuplicateMatch
	seen := map[string]bool{}
	for i, f := range files {
		switch id, ok := firstByChecksum[f.checksum]; {
		case ok:
			dupOf[i] = &id
			m := DuplicateMatch{Index: i, Name: f.name, ExistingID: id}
			if doc, err := s.q.GetClaimDocument(ctx, db.GetClaimDocumentParams{ID: id, ClaimID: up.ClaimID}); err == nil {
				m.ExistingType = Type(doc.DocumentType)
			}
			matches = append(matches, m)
		case seen[f.checksum]:
			matches = append(matches, DuplicateMatch{Index: i, Name: f.name})
		}
		seen[f.checksum] = true
	}
	if len(matches) > 0 && !up.ConfirmDuplicate {
		for _, m := range matches {
			after := map[string]any{"document_type": string(up.Type), "file_index": m.Index, "within_upload": m.ExistingID == 0}
			var entityID *int64
			if m.ExistingID != 0 {
				after["existing_document_id"] = m.ExistingID
				entityID = &m.ExistingID
			}
			if err := s.audit.Record(ctx, audit.Entry{
				UserID: &a.UserID, EntityType: audit.EntityDocument, EntityID: entityID,
				Action: audit.ActionDocumentDuplicateDetected,
				After:  withClaim(after, up.ClaimID),
			}); err != nil {
				return nil, err
			}
		}
		return nil, &DuplicateError{Matches: matches}
	}

	// Tulis file dulu; metadata baru dibuat setelah semua file tersimpan.
	paths := make([]string, 0, len(files))
	cleanup := func() {
		for _, p := range paths {
			if err := s.storage.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
				s.log.Error("gagal menghapus file rollback", "error", err)
			}
		}
	}
	for _, f := range files {
		p, err := s.storage.Put(f.data)
		if err != nil {
			cleanup()
			return nil, err
		}
		paths = append(paths, p)
	}

	var docs []db.ClaimDocument
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		al := audit.NewLogger(q)
		for i, f := range files {
			pages := int32(f.info.Pages)
			d, err := q.InsertClaimDocument(ctx, db.InsertClaimDocumentParams{
				ClaimID: up.ClaimID, DocumentType: string(up.Type), OriginalName: f.name,
				StoragePath: paths[i], MimeType: f.info.MIME, FileSize: int64(len(f.data)),
				Checksum: f.checksum, UploadedBy: &a.UserID, PageCount: &pages, DuplicateOf: dupOf[i],
			})
			if err != nil {
				return err
			}
			after := map[string]any{
				"document_type": d.DocumentType, "mime_type": d.MimeType, "file_size": d.FileSize,
				"page_count": f.info.Pages, "processing_status": d.ProcessingStatus,
			}
			if d.DuplicateOf != nil {
				after["duplicate_of"] = *d.DuplicateOf
				after["duplicate_confirmed"] = true
			}
			if err := al.Record(ctx, audit.Entry{
				UserID: &a.UserID, EntityType: audit.EntityDocument, EntityID: &d.ID,
				Action: audit.ActionDocumentUploaded, After: withClaim(after, up.ClaimID),
			}); err != nil {
				return err
			}
			docs = append(docs, d)
		}
		return nil
	})
	if err != nil {
		cleanup()
		return nil, err
	}
	return docs, nil
}

func withClaim(m map[string]any, claimID int64) map[string]any {
	m["claim_id"] = claimID
	return m
}

// Open membuka dokumen untuk pratinjau/unduh setelah memeriksa hak akses
// klaim dan memastikan dokumen milik klaim tersebut. Akses dicatat sebagai
// document_viewed; bila audit gagal, file tidak dilayani (fail closed).
// Pemanggil wajib menutup file.
func (s *Service) Open(ctx context.Context, a claims.Actor, claimID, docID int64, download bool) (db.ClaimDocument, *os.File, error) {
	if _, err := s.authorize(ctx, a, claimID, authz.PermDocumentsView, "document_view"); err != nil {
		return db.ClaimDocument{}, nil, err
	}
	d, err := s.q.GetClaimDocument(ctx, db.GetClaimDocumentParams{ID: docID, ClaimID: claimID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ClaimDocument{}, nil, ErrNotFound
	}
	if err != nil {
		return db.ClaimDocument{}, nil, err
	}
	f, err := s.storage.Open(d.StoragePath)
	if err != nil {
		return db.ClaimDocument{}, nil, fmt.Errorf("membuka dokumen %d: %w", d.ID, err)
	}
	mode := "preview"
	if download {
		mode = "download"
	}
	if err := s.audit.Record(ctx, audit.Entry{
		UserID: &a.UserID, EntityType: audit.EntityDocument, EntityID: &d.ID,
		Action: audit.ActionDocumentViewed,
		After:  map[string]any{"claim_id": claimID, "mode": mode},
	}); err != nil {
		f.Close()
		return db.ClaimDocument{}, nil, err
	}
	return d, f, nil
}
