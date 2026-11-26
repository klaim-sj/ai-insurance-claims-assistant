package validation

import (
	"context"
	"errors"
	"time"

	"github.com/riverqueue/river"
)

// ValidateClaimArgs adalah argumen job validasi. RequestedAt membedakan
// setiap permintaan sehingga job yang sama tidak dimasukkan dua kali,
// sedangkan permintaan baru (mis. dokumen baru selesai saat validasi lain
// sedang berjalan) tetap menghasilkan job sendiri. Hasilnya tetap
// idempoten karena proses validasi per klaim berjalan berurutan.
type ValidateClaimArgs struct {
	ClaimID     int64  `json:"claim_id" river:"unique"`
	Trigger     string `json:"trigger"`
	RequestedAt string `json:"requested_at" river:"unique"`
}

// Kind adalah nama jenis job di River.
func (ValidateClaimArgs) Kind() string { return "validate_claim" }

// MaxAttempts adalah batas percobaan job validasi.
const MaxAttempts = 3

// InsertOpts: unik per (klaim, permintaan).
func (ValidateClaimArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: MaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// NewArgs membuat argumen job untuk permintaan saat ini.
func NewArgs(claimID int64, trigger string) ValidateClaimArgs {
	return ValidateClaimArgs{ClaimID: claimID, Trigger: trigger, RequestedAt: time.Now().UTC().Format(time.RFC3339Nano)}
}

// Worker memproses job ValidateClaim.
type Worker struct {
	river.WorkerDefaults[ValidateClaimArgs]
	svc *Service
}

// NewWorker membuat Worker.
func NewWorker(svc *Service) *Worker { return &Worker{svc: svc} }

// Timeout: validasi hanya membaca database; tidak butuh waktu lama.
func (w *Worker) Timeout(*river.Job[ValidateClaimArgs]) time.Duration { return time.Minute }

// Work menjalankan validasi.
func (w *Worker) Work(ctx context.Context, job *river.Job[ValidateClaimArgs]) error {
	_, err := w.svc.Run(ctx, job.Args.ClaimID, job.Args.Trigger)
	if errors.Is(err, ErrNotFound) {
		return river.JobCancel(err)
	}
	if err != nil {
		// Hanya ID dan jenis error; tidak ada isi dokumen.
		w.svc.log.Warn("validasi klaim gagal", "claim_id", job.Args.ClaimID, "attempt", job.Attempt)
	}
	return err
}
