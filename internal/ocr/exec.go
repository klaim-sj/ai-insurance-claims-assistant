package ocr

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ExecConfig mengatur engine berbasis binary eksternal.
type ExecConfig struct {
	PDFToText string // default "pdftotext"
	PDFToPPM  string // default "pdftoppm"
	Tesseract string // default "tesseract"
	Lang      string // default "ind+eng"
	// Timeout per pemanggilan binary; default 60 detik.
	Timeout time.Duration
	// DPI render halaman PDF hasil scan; default 300.
	DPI int
	// MaxPages: batas halaman yang diproses; default 50.
	MaxPages int
	// TempDir: induk direktori sementara; "" berarti os.TempDir().
	TempDir string
	// ForceOCR: render dan OCR setiap halaman PDF walaupun punya lapisan
	// teks. Dipakai evaluasi untuk menyimulasikan dokumen hasil scan.
	ForceOCR bool
	// PSM: page segmentation mode Tesseract; default "6" (satu blok teks
	// seragam), paling stabil untuk dokumen formulir "label : nilai".
	// Dapat berisi beberapa mode dipisah koma (mis. "6,4,11"): setiap mode
	// dijalankan dan hasil dengan skor tertinggi (jumlah confidence kata
	// alfanumerik) dipakai. Lebih tahan tata letak tabel, tetapi waktu
	// OCR bertambah sebanding jumlah mode.
	PSM string
	// Preprocess: pra-pemrosesan gambar sebelum Tesseract (gambar unggahan
	// dan halaman PDF hasil render). nil = DefaultPreprocess(). Lapisan
	// teks PDF tidak disentuh.
	Preprocess *PreprocessOptions
}

// Exec memanggil pdftotext, pdftoppm, dan tesseract lewat os/exec tanpa
// shell, masing-masing dengan timeout, di direktori sementara yang selalu
// dihapus setelah selesai.
type Exec struct {
	cfg ExecConfig

	mu       sync.Mutex
	versions map[string]string
	psms     []string
}

// DefaultPSM adalah daftar PSM produksi: 6 (blok seragam) dan 11 (teks
// jarang; jauh lebih baik untuk tabel/formulir yang garisnya dihapus).
// Hasil dengan skor tertinggi dipakai.
const DefaultPSM = "6,11"

// parsePSMs mengurai daftar PSM; nilai tidak valid diabaikan (default "6").
func parsePSMs(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 13 || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		out = []string{"6"}
	}
	return out
}

// PSMs mengembalikan daftar PSM yang dipakai.
func (e *Exec) PSMs() []string { return append([]string(nil), e.psms...) }

// NewExec membuat engine dengan nilai default untuk field kosong.
func NewExec(cfg ExecConfig) *Exec {
	if cfg.PDFToText == "" {
		cfg.PDFToText = "pdftotext"
	}
	if cfg.PDFToPPM == "" {
		cfg.PDFToPPM = "pdftoppm"
	}
	if cfg.Tesseract == "" {
		cfg.Tesseract = "tesseract"
	}
	if cfg.Lang == "" {
		cfg.Lang = "ind+eng"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.DPI <= 0 {
		cfg.DPI = 300
	}
	if cfg.PSM == "" {
		cfg.PSM = DefaultPSM
	}
	if cfg.MaxPages <= 0 {
		cfg.MaxPages = 50
	}
	if cfg.Preprocess == nil {
		p := DefaultPreprocess()
		cfg.Preprocess = &p
	}
	return &Exec{cfg: cfg, versions: map[string]string{}, psms: parsePSMs(cfg.PSM)}
}

// maxOutput membatasi stdout yang dibaca dari binary (16 MB).
const maxOutput = 16 << 20

// minTextChars: halaman PDF dengan huruf/angka kurang dari ini dianggap
// tidak punya lapisan teks sehingga di-OCR.
const minTextChars = 3

// Check memastikan seluruh binary tersedia dan bahasa OCR terpasang.
func (e *Exec) Check(ctx context.Context) error {
	for _, bin := range []string{e.cfg.PDFToText, e.cfg.PDFToPPM, e.cfg.Tesseract} {
		if _, err := exec.LookPath(bin); err != nil {
			return &Error{Code: CodeEngineUnavailable, Err: fmt.Errorf("binary %s tidak ditemukan", filepath.Base(bin))}
		}
	}
	out, err := e.run(ctx, "", e.cfg.Tesseract, "--list-langs")
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		have[strings.TrimSpace(l)] = true
	}
	for _, lang := range strings.Split(e.cfg.Lang, "+") {
		if !have[lang] {
			return &Error{Code: CodeEngineUnavailable, Err: fmt.Errorf("bahasa tesseract %q belum terpasang", lang)}
		}
	}
	return nil
}

var versionRe = regexp.MustCompile(`[0-9]+\.[0-9]+(\.[0-9]+)?`)

// Version mengembalikan versi engine (di-cache).
func (e *Exec) Version(ctx context.Context, engine string) string {
	e.mu.Lock()
	v, ok := e.versions[engine]
	e.mu.Unlock()
	if ok {
		return v
	}
	var bin string
	var args []string
	switch engine {
	case EnginePDFText:
		bin, args = e.cfg.PDFToText, []string{"-v"}
	case EngineTesseract:
		bin, args = e.cfg.Tesseract, []string{"--version"}
	default:
		return "unknown"
	}
	// Versi dicetak ke stdout (tesseract) atau stderr (poppler).
	out, err := e.runCombined(ctx, bin, args...)
	if err != nil {
		return "unknown"
	}
	first, _, _ := strings.Cut(string(out), "\n")
	v = versionRe.FindString(first)
	if v == "" {
		v = "unknown"
	}
	e.mu.Lock()
	e.versions[engine] = v
	e.mu.Unlock()
	return v
}

// Process menjalankan OCR atas data dengan tipe mime.
func (e *Exec) Process(ctx context.Context, data []byte, mime string) (Result, error) {
	switch mime {
	case "application/pdf", "image/png", "image/jpeg":
	default:
		return Result{}, &Error{Code: CodeUnsupportedType}
	}
	dir, err := os.MkdirTemp(e.cfg.TempDir, "ocr-")
	if err != nil {
		return Result{}, &Error{Code: CodeEngineError, Retryable: true, Err: errors.New("membuat direktori sementara")}
	}
	defer os.RemoveAll(dir)

	input := filepath.Join(dir, "input")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		return Result{}, &Error{Code: CodeEngineError, Retryable: true, Err: errors.New("menulis file sementara")}
	}

	if mime != "application/pdf" {
		p, err := e.tesseract(ctx, dir, "input")
		if err != nil {
			return Result{}, err
		}
		p.Number = 1
		return Result{Pages: []Page{p}}, nil
	}
	return e.processPDF(ctx, dir)
}

func (e *Exec) processPDF(ctx context.Context, dir string) (Result, error) {
	out, err := e.run(ctx, dir, e.cfg.PDFToText,
		"-raw", "-enc", "UTF-8", "-f", "1", "-l", strconv.Itoa(e.cfg.MaxPages+1), "input", "-")
	if err != nil {
		return Result{}, withCode(err, CodePDFTextFailed)
	}
	// pdftotext memisahkan halaman dengan form feed; elemen terakhir kosong.
	texts := strings.Split(string(out), "\f")
	if len(texts) > 0 && strings.TrimSpace(texts[len(texts)-1]) == "" {
		texts = texts[:len(texts)-1]
	}
	if len(texts) == 0 {
		return Result{}, &Error{Code: CodePDFTextFailed, Err: errors.New("PDF tanpa halaman")}
	}
	if len(texts) > e.cfg.MaxPages {
		return Result{}, &Error{Code: CodeTooManyPages}
	}

	version := e.Version(ctx, EnginePDFText)
	res := Result{Pages: make([]Page, 0, len(texts))}
	for i, t := range texts {
		n := i + 1
		if !e.cfg.ForceOCR && countAlnum(t) >= minTextChars {
			res.Pages = append(res.Pages, textLayerPage(n, t, version))
			continue
		}
		p, err := e.ocrPDFPage(ctx, dir, n)
		if err != nil {
			return Result{}, err
		}
		res.Pages = append(res.Pages, p)
	}
	return res, nil
}

func textLayerPage(n int, text, version string) Page {
	var lines []Line
	for _, raw := range strings.Split(text, "\n") {
		fields := strings.Fields(raw)
		if len(fields) == 0 {
			continue
		}
		l := Line{Words: make([]Word, len(fields))}
		for i, f := range fields {
			l.Words[i] = Word{Text: f, Conf: 100}
		}
		lines = append(lines, l)
	}
	p := Page{Number: n, Engine: EnginePDFText, EngineVersion: version, Lines: lines}
	p.MeanConfidence, p.WordCount = meanConfidence(lines)
	return p
}

func (e *Exec) ocrPDFPage(ctx context.Context, dir string, n int) (Page, error) {
	prefix := fmt.Sprintf("page%d", n)
	_, err := e.run(ctx, dir, e.cfg.PDFToPPM,
		"-r", strconv.Itoa(e.cfg.DPI), "-gray", "-png",
		"-f", strconv.Itoa(n), "-l", strconv.Itoa(n), "input", prefix)
	if err != nil {
		return Page{}, withCode(err, CodePDFRenderFailed)
	}
	// pdftoppm menamai file prefix-N.png dengan zero padding bergantung
	// jumlah halaman; ambil satu-satunya file yang cocok.
	matches, _ := filepath.Glob(filepath.Join(dir, prefix+"-*.png"))
	sort.Strings(matches)
	if len(matches) != 1 {
		return Page{}, &Error{Code: CodePDFRenderFailed, Retryable: true, Err: errors.New("hasil render halaman tidak ditemukan")}
	}
	img := filepath.Base(matches[0])
	p, err := e.tesseract(ctx, dir, img)
	_ = os.Remove(matches[0])
	if err != nil {
		return Page{}, err
	}
	p.Number = n
	return p, nil
}

// tesseract menjalankan pra-pemrosesan (bila aktif) lalu Tesseract untuk
// setiap PSM yang dikonfigurasi, dan memilih hasil berskor tertinggi.
func (e *Exec) tesseract(ctx context.Context, dir, file string) (Page, error) {
	input, pre := file, false
	if !e.cfg.Preprocess.Disabled {
		if raw, err := os.ReadFile(filepath.Join(dir, file)); err == nil {
			if out, _, err := Preprocess(raw, *e.cfg.Preprocess); err == nil {
				name := "pre-" + file + ".png"
				if err := os.WriteFile(filepath.Join(dir, name), out, 0o600); err == nil {
					input, pre = name, true
				}
			}
			// Gagal dekode/terlalu besar: pakai file asli (Tesseract
			// melaporkan error sendiri bila memang tidak terbaca).
		}
	}
	var best Page
	bestScore := -1.0
	for _, psm := range e.psms {
		out, err := e.run(ctx, dir, e.cfg.Tesseract, input, "stdout", "-l", e.cfg.Lang, "--psm", psm, "tsv")
		if err != nil {
			return Page{}, err
		}
		lines, err := ParseTSV(out)
		if err != nil {
			return Page{}, &Error{Code: CodeEngineError, Retryable: true, Err: err}
		}
		p := Page{Engine: EngineTesseract, EngineVersion: e.Version(ctx, EngineTesseract), Lines: lines, PSM: psm, Preprocessed: pre}
		p.MeanConfidence, p.WordCount = meanConfidence(lines)
		if s := pageScore(lines); s > bestScore {
			best, bestScore = p, s
		}
	}
	if pre {
		_ = os.Remove(filepath.Join(dir, input))
	}
	return best, nil
}

// scoreMinConf: kata di bawah confidence ini tidak dihitung dalam skor
// pemilihan PSM (sisa garis/noise biasanya terbaca dengan confidence
// rendah dan tidak boleh "memenangkan" pemilihan).
const scoreMinConf = 70

// pageScore: jumlah confidence (0–1) kata alfanumerik yang terbaca yakin.
func pageScore(lines []Line) float64 {
	var s float64
	for _, l := range lines {
		for _, w := range l.Words {
			if countAlnum(w.Text) > 0 && w.Conf >= scoreMinConf {
				s += w.Conf / 100
			}
		}
	}
	return s
}

// ParseTSV mengurai output TSV Tesseract menjadi baris kata (level 5).
// Kata kosong dan confidence negatif diabaikan. Baris disusun ulang dari
// posisi kata (bukan blok Tesseract) agar pasangan "label : nilai" yang
// berjauhan secara horizontal tetap berada di baris yang sama.
func ParseTSV(out []byte) ([]Line, error) {
	type word struct {
		Word
		left, cy, h float64
	}
	var words []word
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), maxOutput)
	header := true
	for sc.Scan() {
		row := sc.Text()
		if header {
			header = false
			if !strings.HasPrefix(row, "level\t") {
				return nil, errors.New("format TSV tidak dikenal")
			}
			continue
		}
		cols := strings.Split(row, "\t")
		if len(cols) < 12 || cols[0] != "5" {
			continue
		}
		text := strings.TrimSpace(strings.Join(cols[11:], " "))
		conf, err := strconv.ParseFloat(cols[10], 64)
		if err != nil || conf < 0 || text == "" {
			continue
		}
		if conf > 100 {
			conf = 100
		}
		top, h := float64(atoi(cols[7])), float64(atoi(cols[9]))
		words = append(words, word{Word: Word{Text: text, Conf: conf}, left: float64(atoi(cols[6])), cy: top + h/2, h: h})
	}
	if err := sc.Err(); err != nil {
		return nil, errors.New("membaca TSV")
	}

	sort.SliceStable(words, func(i, j int) bool { return words[i].cy < words[j].cy })
	var rows [][]word
	var rowCY, rowH float64
	for _, w := range words {
		if len(rows) > 0 && math.Abs(w.cy-rowCY) <= math.Max(rowH, w.h)*0.5 {
			r := rows[len(rows)-1]
			rows[len(rows)-1] = append(r, w)
			n := float64(len(r) + 1)
			rowCY += (w.cy - rowCY) / n
			rowH += (w.h - rowH) / n
			continue
		}
		rows = append(rows, []word{w})
		rowCY, rowH = w.cy, w.h
	}
	lines := make([]Line, 0, len(rows))
	for _, r := range rows {
		sort.SliceStable(r, func(i, j int) bool { return r[i].left < r[j].left })
		l := Line{Words: make([]Word, len(r))}
		for i, w := range r {
			l.Words[i] = w.Word
		}
		lines = append(lines, l)
	}
	return lines, nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func countAlnum(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			n++
		}
	}
	return n
}

// withCode mengganti kode error generik engine dengan kode spesifik
// tahap (mis. render PDF) kecuali untuk timeout dan engine tidak tersedia.
func withCode(err error, code Code) error {
	var oe *Error
	if errors.As(err, &oe) && oe.Code == CodeEngineError {
		return &Error{Code: code, Retryable: oe.Retryable, Err: oe.Err}
	}
	return err
}

// limitedBuffer menyimpan maksimal max byte dan menandai bila terpotong.
type limitedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room < len(p) {
		if room > 0 {
			b.buf.Write(p[:room])
		}
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (e *Exec) command(ctx context.Context, dir, bin string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	// Environment minimal: tanpa variabel aplikasi (DATABASE_URL, secret).
	env := []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "OMP_THREAD_LIMIT=1"}
	if dir != "" {
		env = append(env, "HOME="+dir, "TMPDIR="+dir)
	}
	if v := os.Getenv("TESSDATA_PREFIX"); v != "" {
		env = append(env, "TESSDATA_PREFIX="+v)
	}
	cmd.Env = env
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

// run menjalankan binary dengan timeout dan mengembalikan stdout. Stderr
// dibuang (bisa memuat potongan isi dokumen) dan tidak pernah di-log.
func (e *Exec) run(ctx context.Context, dir, bin string, args ...string) ([]byte, error) {
	tctx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
	defer cancel()
	cmd := e.command(tctx, dir, bin, args...)
	stdout := &limitedBuffer{max: maxOutput}
	cmd.Stdout = stdout
	cmd.Stderr = nil
	err := cmd.Run()
	return stdout.buf.Bytes(), e.classify(ctx, tctx, bin, err, stdout.truncated)
}

func (e *Exec) runCombined(ctx context.Context, bin string, args ...string) ([]byte, error) {
	tctx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
	defer cancel()
	cmd := e.command(tctx, "", bin, args...)
	out := &limitedBuffer{max: 64 << 10}
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	if err != nil {
		// pdftotext -v keluar dengan status 0 atau 99 tergantung versi.
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return nil, e.classify(ctx, tctx, bin, err, false)
		}
	}
	return out.buf.Bytes(), nil
}

func (e *Exec) classify(parent, tctx context.Context, bin string, err error, truncated bool) error {
	name := filepath.Base(bin)
	switch {
	case parent.Err() != nil:
		// Job dibatalkan/shutdown: kembalikan error context apa adanya.
		return parent.Err()
	case errors.Is(tctx.Err(), context.DeadlineExceeded):
		return &Error{Code: CodeTimeout, Retryable: true, Err: fmt.Errorf("%s melebihi batas waktu %s", name, e.cfg.Timeout)}
	case errors.Is(err, exec.ErrNotFound):
		return &Error{Code: CodeEngineUnavailable, Err: fmt.Errorf("binary %s tidak ditemukan", name)}
	case err != nil:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return &Error{Code: CodeEngineError, Retryable: true, Err: fmt.Errorf("%s keluar dengan status %d", name, ee.ExitCode())}
		}
		return &Error{Code: CodeEngineError, Retryable: true, Err: fmt.Errorf("menjalankan %s gagal", name)}
	case truncated:
		return &Error{Code: CodeEngineError, Err: fmt.Errorf("output %s melebihi batas", name)}
	}
	return nil
}
