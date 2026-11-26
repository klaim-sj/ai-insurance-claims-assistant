package claims

import (
	"errors"
	"fmt"
)

// Status adalah status administrasi klaim; nilainya sama dengan kolom
// claims.status. Status bukan keputusan manfaat/kelayakan klaim.
type Status string

const (
	StatusDraft         Status = "draft"
	StatusSubmitted     Status = "submitted"
	StatusProcessing    Status = "processing"
	StatusNeedsReview   Status = "needs_review"
	StatusNeedDocuments Status = "need_documents"
	StatusReviewed      Status = "reviewed"
	StatusClosed        Status = "closed"
)

var allStatuses = []Status{
	StatusDraft, StatusSubmitted, StatusProcessing, StatusNeedsReview,
	StatusNeedDocuments, StatusReviewed, StatusClosed,
}

// Statuses mengembalikan seluruh status dalam urutan alur.
func Statuses() []Status { return append([]Status(nil), allStatuses...) }

// Valid melaporkan apakah s adalah status yang dikenal.
func (s Status) Valid() bool {
	for _, x := range allStatuses {
		if s == x {
			return true
		}
	}
	return false
}

// Label untuk UI. Sengaja tidak memakai istilah "ditolak"/"tidak layak".
func (s Status) Label() string {
	switch s {
	case StatusDraft:
		return "Draft"
	case StatusSubmitted:
		return "Diajukan"
	case StatusProcessing:
		return "Sedang diproses"
	case StatusNeedsReview:
		return "Perlu ditinjau"
	case StatusNeedDocuments:
		return "Perlu dokumen"
	case StatusReviewed:
		return "Sudah ditinjau"
	case StatusClosed:
		return "Ditutup"
	default:
		return "Tidak dikenal"
	}
}

// BadgeClass mengembalikan class Tailwind untuk lencana status.
func (s Status) BadgeClass() string {
	switch s {
	case StatusDraft:
		return "bg-slate-100 text-slate-800"
	case StatusSubmitted:
		return "bg-blue-100 text-blue-800"
	case StatusProcessing:
		return "bg-violet-100 text-violet-800"
	case StatusNeedsReview:
		return "bg-amber-100 text-amber-900"
	case StatusNeedDocuments:
		return "bg-orange-100 text-orange-900"
	case StatusReviewed:
		return "bg-emerald-100 text-emerald-800"
	case StatusClosed:
		return "bg-slate-200 text-slate-700"
	default:
		return "bg-slate-100 text-slate-800"
	}
}

// actorKind menentukan siapa yang boleh melakukan suatu transisi.
type actorKind int

const (
	byUser actorKind = iota + 1
	bySystem
)

// transitions adalah peta transisi yang diizinkan. Satu-satunya sumber
// kebenaran; dipakai oleh service dan UI.
//
// processing dan needs_review hanya dapat di-set oleh sistem (pemrosesan
// dokumen/validasi otomatis). need_documents TIDAK PERNAH di-set sistem:
// temuan hanya menjadi saran yang dikonfirmasi petugas. closed bersifat final.
var transitions = map[Status]map[Status]actorKind{
	StatusDraft: {
		StatusSubmitted: byUser,
		StatusClosed:    byUser,
	},
	StatusSubmitted: {
		StatusProcessing:    bySystem,
		StatusNeedsReview:   bySystem, // validasi otomatis menemukan temuan open
		StatusNeedDocuments: byUser,
		StatusReviewed:      byUser,
	},
	StatusProcessing: {
		StatusNeedsReview: bySystem,
		StatusSubmitted:   bySystem, // pemrosesan gagal: kembali ke antrean
	},
	StatusNeedsReview: {
		StatusReviewed:      byUser,
		StatusNeedDocuments: byUser,
	},
	StatusNeedDocuments: {
		StatusSubmitted: byUser,
		StatusClosed:    byUser,
	},
	StatusReviewed: {
		StatusNeedDocuments: byUser,
		StatusClosed:        byUser,
	},
	StatusClosed: {},
}

// ReasonRequired melaporkan apakah transisi ke status to wajib beralasan.
func ReasonRequired(to Status) bool {
	return to == StatusNeedDocuments || to == StatusClosed
}

// UserTransitions mengembalikan status tujuan yang boleh dipilih pengguna
// dari status from, dalam urutan alur.
func UserTransitions(from Status) []Status {
	var out []Status
	for _, to := range allStatuses {
		if transitions[from][to] == byUser {
			out = append(out, to)
		}
	}
	return out
}

// MaxReasonLength adalah panjang maksimum alasan perubahan status.
const MaxReasonLength = 500

// Error domain.
var (
	ErrNotFound          = errors.New("klaim tidak ditemukan")
	ErrInvalidTransition = errors.New("perubahan status tidak diizinkan")
	ErrReasonRequired    = errors.New("alasan wajib diisi untuk status ini")
	ErrReasonTooLong     = fmt.Errorf("alasan maksimal %d karakter", MaxReasonLength)
	ErrClosed            = errors.New("klaim sudah ditutup dan tidak dapat diubah")
)

// TransitionError menjelaskan transisi yang ditolak. errors.Is(err,
// ErrInvalidTransition) bernilai true.
type TransitionError struct {
	From, To   Status
	SystemOnly bool
}

func (e *TransitionError) Error() string {
	if e.SystemOnly {
		return fmt.Sprintf("status %q hanya dapat diubah oleh sistem", e.To.Label())
	}
	return fmt.Sprintf("perubahan status dari %q ke %q tidak diizinkan", e.From.Label(), e.To.Label())
}

func (e *TransitionError) Unwrap() error { return ErrInvalidTransition }
