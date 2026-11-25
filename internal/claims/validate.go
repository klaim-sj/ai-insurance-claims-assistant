package claims

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Location adalah zona waktu bisnis (WIB). Memakai zona tetap agar tidak
// bergantung pada tzdata di lingkungan.
var Location = time.FixedZone("WIB", 7*60*60)

// DateLayout adalah format tanggal input/filter (sesuai <input type="date">).
const DateLayout = "2006-01-02"

// NumberPrefix adalah prefix nomor klaim demo.
const NumberPrefix = "CLM-DEMO"

// FormatNumber membentuk nomor klaim, mis. CLM-DEMO-0001.
func FormatNumber(n int64) string { return fmt.Sprintf("%s-%04d", NumberPrefix, n) }

// ProductHealthSim adalah satu-satunya produk pada skenario demo.
const ProductHealthSim = "Kesehatan (simulasi)"

// ProductTypes adalah allowlist jenis produk.
var ProductTypes = []string{ProductHealthSim}

func validProduct(p string) bool {
	for _, x := range ProductTypes {
		if p == x {
			return true
		}
	}
	return false
}

var (
	// Hanya format polis demo yang diterima agar nomor polis nyata tidak
	// dapat dimasukkan ke aplikasi portofolio ini.
	policyRe = regexp.MustCompile(`^POL-DEMO-[0-9]{4,6}$`)
	nameRe   = regexp.MustCompile(`^\p{L}[\p{L} .'\-]*$`)
	spaceRe  = regexp.MustCompile(`\s+`)
)

var minReceivedAt = time.Date(2000, 1, 1, 0, 0, 0, 0, Location)

// Input adalah data klaim mentah dari formulir.
type Input struct {
	ClaimantName string
	PolicyNumber string
	ProductType  string
	ReceivedAt   string // YYYY-MM-DD
	AssignedTo   string // ID user; diabaikan untuk peran tanpa izin reassign
}

// ValidationError berisi pesan per field. Pesan tidak memuat nilai input.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "data klaim tidak valid" }

type fields struct {
	name       string
	policy     string
	product    string
	receivedAt time.Time
	assignedTo *int64
}

// ParseDate mengurai tanggal YYYY-MM-DD sebagai awal hari WIB.
func ParseDate(s string) (time.Time, error) {
	return time.ParseInLocation(DateLayout, strings.TrimSpace(s), Location)
}

func startOfDay(t time.Time) time.Time {
	t = t.In(Location)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, Location)
}

func validate(in Input, now time.Time) (fields, *ValidationError) {
	errs := map[string]string{}
	var f fields

	f.name = strings.TrimSpace(spaceRe.ReplaceAllString(in.ClaimantName, " "))
	switch n := utf8.RuneCountInString(f.name); {
	case n == 0:
		errs["claimant_name"] = "Nama pemohon wajib diisi."
	case n < 2 || n > 100:
		errs["claimant_name"] = "Nama pemohon harus 2–100 karakter."
	case !nameRe.MatchString(f.name):
		errs["claimant_name"] = "Nama pemohon hanya boleh berisi huruf, spasi, titik, apostrof, atau tanda hubung."
	}

	f.policy = strings.ToUpper(strings.TrimSpace(in.PolicyNumber))
	switch {
	case f.policy == "":
		errs["policy_number"] = "Nomor polis wajib diisi."
	case !policyRe.MatchString(f.policy):
		errs["policy_number"] = "Nomor polis harus berformat POL-DEMO-0000 (data simulasi)."
	}

	f.product = strings.TrimSpace(in.ProductType)
	if !validProduct(f.product) {
		errs["product_type"] = "Jenis produk tidak valid."
	}

	if strings.TrimSpace(in.ReceivedAt) == "" {
		errs["received_at"] = "Tanggal diterima wajib diisi."
	} else if d, err := ParseDate(in.ReceivedAt); err != nil {
		errs["received_at"] = "Tanggal diterima tidak valid."
	} else if d.After(startOfDay(now)) {
		errs["received_at"] = "Tanggal diterima tidak boleh di masa depan."
	} else if d.Before(minReceivedAt) {
		errs["received_at"] = "Tanggal diterima terlalu lama."
	} else {
		f.receivedAt = d
	}

	if s := strings.TrimSpace(in.AssignedTo); s == "" {
		errs["assigned_to"] = "Petugas wajib dipilih."
	} else if id, err := strconv.ParseInt(s, 10, 64); err != nil || id <= 0 {
		errs["assigned_to"] = "Petugas tidak valid."
	} else {
		f.assignedTo = &id
	}

	if len(errs) > 0 {
		return f, &ValidationError{Fields: errs}
	}
	return f, nil
}

// normalizeReason merapikan alasan dan memeriksa panjangnya.
func normalizeReason(to Status, reason string) (*string, error) {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > MaxReasonLength {
		return nil, ErrReasonTooLong
	}
	if reason == "" {
		if ReasonRequired(to) {
			return nil, ErrReasonRequired
		}
		return nil, nil
	}
	return &reason, nil
}

// searchPattern mengubah kata kunci menjadi pola LIKE awalan dengan
// wildcard (%, _) dan escape (\) di-escape, sehingga input pengguna
// diperlakukan sebagai teks literal.
func searchPattern(q string) *string {
	q = strings.ToUpper(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	if utf8.RuneCountInString(q) > 50 {
		q = string([]rune(q)[:50])
	}
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	p := r.Replace(q) + "%"
	return &p
}
