// Package extraction mengubah teks OCR menjadi field terstruktur dengan
// aturan deterministik (tanpa LLM).
//
// Prinsip:
//   - Setiap field yang ditemukan menyertakan halaman dan kutipan sumber.
//   - Tidak menebak: field tanpa label dikembalikan not_found; nilai yang
//     tidak terbaca atau confidence rendah dikembalikan uncertain.
//   - NIK selalu disamarkan (0000********1234); kutipan juga disamarkan.
//   - Diagnosis dan detail medis TIDAK pernah diekstrak ke field.
package extraction

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/ocr"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/pii"
)

// Version dicatat di extracted_fields.extractor_version.
const Version = "rules-v2"

// Ambang confidence (0–100). Nilai awal untuk prototipe; diukur ulang
// dengan `make eval` pada dataset sintetis, bukan probabilitas klaim benar.
const (
	// HighConfidence ke atas: label "tinggi".
	HighConfidence = 85.0
	// MinConfidence: di bawah ini field berstatus uncertain (label "rendah").
	MinConfidence = 60.0
	// MinReadableWords: halaman OCR dengan kata lebih sedikit dianggap
	// tidak terbaca.
	MinReadableWords = 5
)

// MaxExcerpt adalah panjang maksimal kutipan sumber (rune).
const MaxExcerpt = 120

// Key adalah nama field (sama dengan extracted_fields.field_key).
type Key string

const (
	KeyClaimantName Key = "claimant_name"
	KeyPolicyNumber Key = "policy_number"
	KeyNIK          Key = "nik"
	KeyEventDate    Key = "event_date"
	KeyDocumentDate Key = "document_date"
	KeyAmount       Key = "amount"
)

// AllKeys dalam urutan tampilan.
var AllKeys = []Key{KeyClaimantName, KeyPolicyNumber, KeyNIK, KeyEventDate, KeyDocumentDate, KeyAmount}

// Label untuk UI.
func (k Key) Label() string {
	switch k {
	case KeyClaimantName:
		return "Nama"
	case KeyPolicyNumber:
		return "Nomor polis"
	case KeyNIK:
		return "NIK (disamarkan)"
	case KeyEventDate:
		return "Tanggal kejadian / masuk"
	case KeyDocumentDate:
		return "Tanggal dokumen"
	case KeyAmount:
		return "Nominal"
	}
	return string(k)
}

// Status field.
type Status string

const (
	StatusFound     Status = "found"
	StatusUncertain Status = "uncertain"
	StatusNotFound  Status = "not_found"
)

// Field adalah hasil ekstraksi satu field.
type Field struct {
	Key        Key
	Value      string // kosong bila not_found atau tidak terbaca
	Status     Status
	Confidence *float64
	Page       int // 0 bila tidak ada
	Excerpt    string
}

// ConfidenceLabel memetakan confidence ke label UI.
func ConfidenceLabel(c *float64) string {
	switch {
	case c == nil:
		return ""
	case *c >= HighConfidence:
		return "tinggi"
	case *c >= MinConfidence:
		return "perlu verifikasi"
	}
	return "rendah"
}

// KeysFor mengembalikan field yang relevan untuk jenis dokumen.
func KeysFor(docType string) []Key {
	switch docType {
	case "formulir_klaim":
		return AllKeys
	case "identitas":
		return []Key{KeyClaimantName, KeyNIK}
	case "surat_keterangan_medis":
		return []Key{KeyClaimantName, KeyEventDate, KeyDocumentDate}
	case "kuitansi":
		return []Key{KeyClaimantName, KeyDocumentDate, KeyAmount}
	}
	return AllKeys
}

// candidate adalah pasangan label–nilai pada satu baris.
type candidate struct {
	page     int
	label    string // dinormalisasi, untuk pencocokan
	rawLabel string // apa adanya, untuk kutipan
	value    string
	conf     float64 // rata-rata confidence kata nilai
	pageConf float64
}

// Extract menjalankan seluruh aturan untuk dokumen bertipe docType.
func Extract(docType string, pages []ocr.Page) []Field {
	cands := candidates(pages)
	readable := false
	var worstConf *float64
	for _, p := range pages {
		if pageReadable(p) {
			readable = true
		} else if p.MeanConfidence != nil && (worstConf == nil || *p.MeanConfidence < *worstConf) {
			c := *p.MeanConfidence
			worstConf = &c
		}
	}

	keys := KeysFor(docType)
	out := make([]Field, 0, len(keys))
	for _, k := range keys {
		f, ok := extractKey(k, cands)
		if !ok && k == KeyDocumentDate && (docType == "formulir_klaim" || docType == "surat_keterangan_medis") {
			f, ok = signatureDate(pages)
		}
		if !ok {
			f = Field{Key: k, Status: StatusNotFound}
			if !readable {
				// Tidak dapat dipastikan tidak ada: halaman tidak terbaca.
				f.Status = StatusUncertain
				f.Confidence = worstConf
				if len(pages) > 0 {
					f.Page = pages[0].Number
				}
			}
		}
		out = append(out, f)
	}
	return out
}

// LowQuality melaporkan apakah ada halaman OCR yang tidak terbaca dengan
// andal (sedikit kata atau confidence rata-rata rendah).
func LowQuality(engine string, words int, meanConf *float64) bool {
	return !pageReadable(ocr.Page{Engine: engine, WordCount: words, MeanConfidence: meanConf})
}

// pageReadable: lapisan teks PDF selalu terbaca; halaman OCR harus punya
// cukup kata dengan confidence rata-rata memadai.
func pageReadable(p ocr.Page) bool {
	if p.Engine == ocr.EnginePDFText {
		return p.WordCount > 0
	}
	return p.WordCount >= MinReadableWords && p.MeanConfidence != nil && *p.MeanConfidence >= MinConfidence
}

var labelCleanRe = regexp.MustCompile(`[^\p{L}\p{N}/ ]+`)

func normLabel(s string) string {
	s = strings.ToLower(s)
	s = labelCleanRe.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

func candidates(pages []ocr.Page) []candidate {
	multi := multiWordLabels()
	var out []candidate
	for _, p := range pages {
		pc := 0.0
		if p.MeanConfidence != nil {
			pc = *p.MeanConfidence
		}
		add := func(label, rawLabel string, ws []ocr.Word) {
			ws = cleanValueWords(ws)
			if len(ws) == 0 {
				return
			}
			out = append(out, candidate{
				page: p.Number, label: label, rawLabel: rawLabel, value: joinWords(ws),
				conf: meanConf(ws), pageConf: pc,
			})
		}
		for i, l := range p.Lines {
			label, valueWords, ok := splitLine(l)
			if ok {
				// Nilai di baris berikut bila baris label berakhir dengan pemisah.
				if len(cleanValueWords(valueWords)) == 0 && i+1 < len(p.Lines) {
					next := p.Lines[i+1]
					if !strings.ContainsAny(next.Text(), ":;|") {
						valueWords = next.Words
					}
				}
				add(normLabel(label), label, valueWords)
				if multi[normLabel(label)] > 0 {
					continue
				}
			}
			// Tanpa pemisah (mis. tabel yang garisnya dihapus, atau PSM
			// yang tidak membaca ":"): label multikata di awal baris. Label
			// satu kata ("tanggal", "total") hanya bila kata berikutnya
			// diawali angka/"Rp", agar "Tanggal Keluar ..." tidak terbaca
			// sebagai tanggal dokumen.
			if lbl, raw, rest, ok := prefixLabel(l, multi); ok {
				add(lbl, raw, rest)
			}
		}
	}
	return out
}

// prefixLabels: label ternormalisasi → jumlah kata.
func multiWordLabels() map[string]int {
	m := map[string]int{}
	for _, ls := range active.Load().Fields {
		for _, l := range ls {
			m[l] = len(strings.Fields(l))
		}
	}
	return m
}

// numericStart: kata diawali angka atau "Rp" (nilai tanggal/nominal).
func numericStart(s string) bool {
	s = strings.TrimLeft(s, valueTrim)
	if s == "" {
		return false
	}
	if r := []rune(s)[0]; unicode.IsDigit(r) {
		return true
	}
	return len(s) >= 2 && strings.EqualFold(s[:2], "rp")
}

// prefixLabel mencari label terpanjang di awal baris (lihat candidates).
func prefixLabel(l ocr.Line, multi map[string]int) (label, raw string, rest []ocr.Word, ok bool) {
	var toks []string
	var idx []int // indeks kata asal untuk setiap token
	for i, w := range l.Words {
		for _, t := range strings.Fields(normLabel(w.Text)) {
			toks = append(toks, t)
			idx = append(idx, i)
		}
	}
	best, bestLabel := -1, ""
	for n := 1; n <= len(toks) && n <= 6; n++ {
		cand := strings.Join(toks[:n], " ")
		lbl := ""
		if multi[cand] == n {
			lbl = cand
		} else if n >= 2 {
			for l, words := range multi {
				if words == n && labelMatch(cand, l) && (lbl == "" || l < lbl) {
					lbl = l // deterministik: label terkecil secara leksikal
				}
			}
		}
		if lbl == "" {
			continue
		}
		// Label harus berakhir di batas kata asal.
		if n < len(toks) && idx[n] == idx[n-1] {
			continue
		}
		if n == 1 && (idx[0]+1 >= len(l.Words) || !numericStart(l.Words[idx[0]+1].Text)) {
			continue
		}
		best, bestLabel = n, lbl
	}
	if best < 0 {
		return "", "", nil, false
	}
	last := idx[best-1]
	var rawParts []string
	for _, w := range l.Words[:last+1] {
		rawParts = append(rawParts, w.Text)
	}
	return bestLabel, strings.Join(rawParts, " "), l.Words[last+1:], true
}

// valueTrim: karakter tepi nilai yang merupakan sisa pemisah/garis tabel
// atau tanda kutip hasil OCR.
const valueTrim = "\"“”'‘’|:;—–-_~=,."

// cleanValueWords membuang kata tanpa huruf/angka di awal/akhir nilai
// dan memangkas sisa pemisah di tepi.
func cleanValueWords(ws []ocr.Word) []ocr.Word {
	alnum := func(s string) bool {
		for _, r := range s {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return true
			}
		}
		return false
	}
	for len(ws) > 0 && !alnum(ws[0].Text) {
		ws = ws[1:]
	}
	for len(ws) > 0 && !alnum(ws[len(ws)-1].Text) {
		ws = ws[:len(ws)-1]
	}
	if len(ws) == 0 {
		return nil
	}
	out := append([]ocr.Word(nil), ws...)
	out[0].Text = strings.TrimLeft(out[0].Text, valueTrim)
	// Titik akhir dipertahankan bila bagian singkatan di tengah nama.
	out[len(out)-1].Text = strings.TrimRight(out[len(out)-1].Text, valueTrim)
	if out[0].Text == "" || out[len(out)-1].Text == "" {
		return cleanValueWords(out)
	}
	return out
}

// splitLine memisahkan "Label : nilai" (juga "Label | nilai" pada tabel).
// OCR kadang membaca ":" sebagai ";".
func splitLine(l ocr.Line) (string, []ocr.Word, bool) {
	var label []string
	for i, w := range l.Words {
		idx := strings.IndexAny(w.Text, ":;|")
		if idx < 0 {
			label = append(label, w.Text)
			continue
		}
		if before := w.Text[:idx]; before != "" {
			label = append(label, before)
		}
		var value []ocr.Word
		if after := strings.TrimSpace(w.Text[idx+1:]); after != "" {
			value = append(value, ocr.Word{Text: after, Conf: w.Conf})
		}
		value = append(value, l.Words[i+1:]...)
		if normLabel(strings.Join(label, " ")) == "" {
			// Pemisah di awal baris (mis. garis kiri tabel): lewati.
			label = nil
			continue
		}
		if len(label) > 6 {
			return "", nil, false
		}
		return strings.Join(label, " "), value, true
	}
	return "", nil, false
}

func joinWords(ws []ocr.Word) string {
	parts := make([]string, len(ws))
	for i, w := range ws {
		parts[i] = w.Text
	}
	return strings.Join(parts, " ")
}

func meanConf(ws []ocr.Word) float64 {
	if len(ws) == 0 {
		return 0
	}
	var s float64
	for _, w := range ws {
		s += w.Conf
	}
	return s / float64(len(ws))
}

func extractKey(k Key, cands []candidate) (Field, bool) {
	labels := active.Load().Fields[k]
	// Cocok persis lebih dulu (urutan prioritas), baru toleran salah baca.
	for _, fuzzy := range []bool{false, true} {
		for _, lbl := range labels {
			for _, c := range cands {
				if c.label == lbl || fuzzy && labelMatch(c.label, lbl) {
					return build(k, c), true
				}
			}
		}
	}
	return Field{}, false
}

// labelMatch: label sama dengan toleransi salah baca OCR — jumlah kata
// sama, dan setiap kata identik atau (untuk kata ≥ 5 huruf) berjarak edit
// ≤ 1 ("jumtah dibayar" ~ "jumlah dibayar"). Label satu kata pendek
// ("nik", "nama") harus persis.
func labelMatch(got, want string) bool {
	if got == want {
		return true
	}
	a, b := strings.Fields(got), strings.Fields(want)
	if len(a) != len(b) {
		return false
	}
	fuzzy := 0
	for i := range a {
		if a[i] == b[i] {
			continue
		}
		if utf8.RuneCountInString(b[i]) < 5 || editDistance(a[i], b[i]) > 1 {
			return false
		}
		fuzzy++
	}
	// Minimal satu kata harus persis bila label multikata.
	return fuzzy < len(b) || len(b) == 1
}

// editDistance: jarak Levenshtein (rune).
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// build mengurai nilai kandidat menjadi Field dengan confidence
// min(confidence kata nilai, kecocokan format).
func build(k Key, c candidate) Field {
	value, format, display := parseValue(k, c.value)
	conf := math.Min(c.conf, format*100)
	conf = math.Round(conf*100) / 100
	f := Field{Key: k, Page: c.page, Confidence: &conf, Excerpt: excerpt(k, c.rawLabel, display)}
	switch {
	case value == "":
		f.Status = StatusUncertain
		if k != KeyNIK {
			// Nilai mentah ditampilkan untuk verifikasi manual (tersamar).
			f.Value = truncate(pii.MaskNIK(strings.TrimSpace(c.value)), 100)
		}
	case conf < MinConfidence:
		f.Status = StatusUncertain
		f.Value = value
	default:
		f.Status = StatusFound
		f.Value = value
	}
	return f
}

var (
	nameRe      = regexp.MustCompile(`^\p{L}[\p{L} .'\-]{1,99}$`)
	policyRe    = regexp.MustCompile(`^[A-Z0-9][A-Z0-9\-/.]{3,30}$`)
	numDateRe   = regexp.MustCompile(`\b([0-9]{1,2})[-/. ]([0-9]{1,2})[-/. ]([0-9]{4})\b`)
	wordDateRe  = regexp.MustCompile(`(?i)\b([0-9]{1,2})\s+([a-z]+)\s+([0-9]{4})\b`)
	amountRe    = regexp.MustCompile(`(?i)(rp\.?\s*)?([0-9]{1,3}(?:\.[0-9]{3})+|[0-9]{1,3}(?:,[0-9]{3})+|[0-9]+)(?:,[0-9]{2})?`)
	digitsRunRe = regexp.MustCompile(`[0-9]{6,}`)
)

var months = map[string]time.Month{
	"januari": 1, "februari": 2, "maret": 3, "april": 4, "mei": 5, "juni": 6, "juli": 7,
	"agustus": 8, "september": 9, "oktober": 10, "november": 11, "desember": 12,
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "jun": 6, "jul": 7, "agu": 8, "agt": 8,
	"sep": 9, "okt": 10, "nov": 11, "des": 12,
}

// parseValue mengembalikan nilai ternormalisasi ("" bila tidak valid),
// skor format 0–1, dan teks nilai untuk kutipan.
func parseValue(k Key, raw string) (value string, format float64, display string) {
	raw = strings.TrimSpace(raw)
	display = raw
	switch k {
	case KeyClaimantName:
		v := strings.Join(strings.Fields(strings.Trim(raw, " ,;")), " ")
		if nameRe.MatchString(v) {
			return v, 1, display
		}
		return "", 0, display
	case KeyPolicyNumber:
		v := strings.ToUpper(strings.Join(strings.Fields(raw), ""))
		if policyRe.MatchString(v) {
			return v, 1, display
		}
		return "", 0, display
	case KeyNIK:
		d := pii.DigitsOnly(raw)
		if len(d) == 16 && onlyNIKChars(raw) {
			return pii.MaskedNIK(d), 1, display
		}
		return "", 0, display
	case KeyEventDate, KeyDocumentDate:
		if d, ok := parseDate(raw); ok {
			return d, 1, display
		}
		return "", 0, display
	case KeyAmount:
		m := amountRe.FindStringSubmatch(raw)
		if m == nil {
			return "", 0, display
		}
		d := pii.DigitsOnly(m[2])
		n, err := strconv.ParseInt(d, 10, 64)
		if err != nil || n <= 0 {
			return "", 0, display
		}
		format = 1
		if m[1] == "" && !strings.ContainsAny(m[2], ".,") {
			format = 0.9 // tanpa "Rp" dan pemisah ribuan: kurang pasti
		}
		return strconv.FormatInt(n, 10), format, display
	}
	return "", 0, display
}

// onlyNIKChars: nilai NIK hanya boleh berisi digit dan pemisah; huruf
// (mis. "O" terbaca sebagai nol) membuat nilai tidak pasti.
func onlyNIKChars(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) && r != ' ' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

func parseDate(s string) (string, bool) {
	var d, m, y int
	if mm := numDateRe.FindStringSubmatch(s); mm != nil {
		d, _ = strconv.Atoi(mm[1])
		m, _ = strconv.Atoi(mm[2])
		y, _ = strconv.Atoi(mm[3])
	} else if mm := wordDateRe.FindStringSubmatch(s); mm != nil {
		mon, ok := months[strings.ToLower(mm[2])]
		if !ok {
			return "", false
		}
		d, _ = strconv.Atoi(mm[1])
		m = int(mon)
		y, _ = strconv.Atoi(mm[3])
	} else {
		return "", false
	}
	if y < 1900 || y > 2100 {
		return "", false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if t.Day() != d || int(t.Month()) != m {
		return "", false // tanggal tidak ada di kalender (mis. 31-02)
	}
	return t.Format("2006-01-02"), true
}

var signatureRe = regexp.MustCompile(`^([\p{L}][\p{L} .]{1,40}),\s*(.+)$`)

// signatureDate mencari pola "Kota, DD-MM-YYYY" (tempat dan tanggal tanda
// tangan) sebagai tanggal dokumen. Karena tanpa label, skor format 0,8
// sehingga paling tinggi berlabel "perlu verifikasi".
func signatureDate(pages []ocr.Page) (Field, bool) {
	for _, p := range pages {
		for _, l := range p.Lines {
			text := l.Text()
			m := signatureRe.FindStringSubmatch(text)
			if m == nil || strings.ContainsAny(m[1], ":;") {
				continue
			}
			dm := numDateRe.FindStringIndex(m[2])
			if dm == nil || dm[0] != 0 {
				continue
			}
			v, ok := parseDate(m[2])
			if !ok {
				continue
			}
			conf := math.Round(math.Min(meanConf(l.Words), 80)*100) / 100
			f := Field{
				Key: KeyDocumentDate, Value: v, Page: p.Number, Confidence: &conf,
				Excerpt: excerpt(KeyDocumentDate, "", m[1]+", "+m[2][dm[0]:dm[1]]),
			}
			f.Status = StatusFound
			if conf < MinConfidence {
				f.Status = StatusUncertain
			}
			return f, true
		}
	}
	return Field{}, false
}

// excerpt membentuk kutipan pendek "label: nilai" yang sudah disamarkan.
// Untuk NIK, deret digit panjang disamarkan seluruhnya kecuali 4 digit
// awal/akhir agar NIK yang terbaca sebagian pun tidak tersimpan.
func excerpt(k Key, label, value string) string {
	s := value
	if label != "" {
		s = label + ": " + value
	}
	s = pii.MaskNIK(s)
	if k == KeyNIK {
		s = digitsRunRe.ReplaceAllStringFunc(s, maskDigits)
	}
	return truncate(s, MaxExcerpt)
}

// maskDigits: deret >= 12 digit menyisakan 4 digit awal dan akhir;
// deret lebih pendek disamarkan seluruhnya.
func maskDigits(m string) string {
	if len(m) >= 12 {
		return m[:4] + strings.Repeat("*", len(m)-8) + m[len(m)-4:]
	}
	return strings.Repeat("*", len(m))
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
