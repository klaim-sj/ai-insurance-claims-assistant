package documents

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Batas validasi file.
const (
	MaxFileSize  = 10 << 20 // 10 MB per file
	MaxFiles     = 5        // jumlah file per unggahan
	MaxPDFPages  = 50
	maxImageSide = 12000
	// maxImagePixels membatasi memori decode (≈25 MP ≈ 100 MB RGBA).
	maxImagePixels = 25_000_000
	// maxInflate membatasi total hasil dekompresi object stream PDF
	// (perlindungan decompression bomb).
	maxInflate = 64 << 20
)

// MIME yang diizinkan.
const (
	MIMEPDF  = "application/pdf"
	MIMEJPEG = "image/jpeg"
	MIMEPNG  = "image/png"
)

// Pesan penolakan file. Pesan ini ditampilkan ke pengguna dan tidak memuat
// isi file.
var (
	ErrEmpty         = errors.New("file kosong")
	ErrTooLarge      = fmt.Errorf("ukuran file melebihi batas %d MB", MaxFileSize>>20)
	ErrUnsupported   = errors.New("jenis file tidak didukung; hanya PDF, JPG, atau PNG")
	ErrExtMismatch   = errors.New("ekstensi file tidak sesuai dengan isi file")
	ErrPDFEncrypted  = errors.New("PDF terenkripsi atau dilindungi kata sandi; unggah versi tanpa kata sandi")
	ErrPDFCorrupt    = errors.New("PDF rusak atau tidak lengkap")
	ErrPDFNoPages    = errors.New("PDF tidak memiliki halaman")
	ErrPDFTooMany    = fmt.Errorf("PDF memiliki lebih dari %d halaman; pisahkan menjadi beberapa file", MaxPDFPages)
	ErrImageCorrupt  = errors.New("gambar rusak atau tidak dapat dibaca")
	ErrImageTooLarge = errors.New("resolusi gambar terlalu besar")
)

var extByMIME = map[string][]string{
	MIMEPDF:  {".pdf"},
	MIMEJPEG: {".jpg", ".jpeg"},
	MIMEPNG:  {".png"},
}

// Info adalah hasil validasi satu file.
type Info struct {
	MIME  string
	Pages int // jumlah halaman PDF; 1 untuk gambar
}

// Ext mengembalikan ekstensi kanonis untuk MIME.
func Ext(mime string) string {
	if e := extByMIME[mime]; len(e) > 0 {
		return e[0]
	}
	return ""
}

// DetectMIME menentukan jenis file dari magic bytes (bukan dari ekstensi
// atau Content-Type kiriman klien).
func DetectMIME(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("%PDF-")):
		return MIMEPDF
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return MIMEPNG
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return MIMEJPEG
	}
	return ""
}

// Validate memeriksa isi file dan kecocokannya dengan nama file.
func Validate(name string, data []byte) (Info, error) {
	if len(data) == 0 {
		return Info{}, ErrEmpty
	}
	if len(data) > MaxFileSize {
		return Info{}, ErrTooLarge
	}
	mime := DetectMIME(data)
	if mime == "" {
		return Info{}, ErrUnsupported
	}
	ext := strings.ToLower(filepath.Ext(name))
	okExt := false
	for _, e := range extByMIME[mime] {
		if e == ext {
			okExt = true
		}
	}
	if !okExt {
		return Info{}, ErrExtMismatch
	}

	switch mime {
	case MIMEPDF:
		pages, err := checkPDF(data)
		if err != nil {
			return Info{}, err
		}
		return Info{MIME: mime, Pages: pages}, nil
	default:
		if err := checkImage(mime, data); err != nil {
			return Info{}, err
		}
		return Info{MIME: mime, Pages: 1}, nil
	}
}

func checkImage(mime string, data []byte) error {
	decodeConfig, decode := png.DecodeConfig, png.Decode
	if mime == MIMEJPEG {
		decodeConfig, decode = jpeg.DecodeConfig, jpeg.Decode
	}
	cfg, err := decodeConfig(bytes.NewReader(data))
	if err != nil {
		return ErrImageCorrupt
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return ErrImageCorrupt
	}
	if cfg.Width > maxImageSide || cfg.Height > maxImageSide || cfg.Width*cfg.Height > maxImagePixels {
		return ErrImageTooLarge
	}
	// Decode penuh untuk mendeteksi file terpotong/rusak.
	var img image.Image
	if img, err = decode(bytes.NewReader(data)); err != nil || img == nil {
		return ErrImageCorrupt
	}
	return nil
}

var (
	// /Type /Page (bukan /Pages). Delimiter PDF: spasi, /, <, >, [, ], (, ).
	reTypePage = regexp.MustCompile(`/Type\s*/Page(?:[\s/<>\[\]()]|$)`)
	reObjStm   = regexp.MustCompile(`/Type\s*/ObjStm(?:[\s/<>\[\]()]|$)`)
	reEncrypt  = regexp.MustCompile(`/Encrypt(?:[\s/<>\[\]()]|$)`)
	reStream   = regexp.MustCompile(`stream\r?\n`)
)

// checkPDF adalah pemeriksaan struktural ringan berbasis stdlib (bukan parser
// PDF lengkap): header, penanda akhir file, enkripsi, dan jumlah halaman,
// termasuk objek halaman di dalam object stream terkompresi.
func checkPDF(data []byte) (int, error) {
	if !bytes.HasPrefix(data, []byte("%PDF-1.")) && !bytes.HasPrefix(data, []byte("%PDF-2.")) {
		return 0, ErrPDFCorrupt
	}
	tail := data[max(0, len(data)-2048):]
	if !bytes.Contains(tail, []byte("%%EOF")) {
		return 0, ErrPDFCorrupt
	}
	if !bytes.Contains(data, []byte("startxref")) {
		return 0, ErrPDFCorrupt
	}
	if reEncrypt.Match(data) {
		return 0, ErrPDFEncrypted
	}

	pages := len(reTypePage.FindAllIndex(data, -1))
	budget := int64(maxInflate)
	for _, loc := range reObjStm.FindAllIndex(data, -1) {
		n, err := countPagesInObjStm(data[loc[1]:], &budget)
		if err != nil {
			return 0, err
		}
		pages += n
		if pages > MaxPDFPages {
			return 0, ErrPDFTooMany
		}
	}
	switch {
	case pages == 0:
		return 0, ErrPDFNoPages
	case pages > MaxPDFPages:
		return 0, ErrPDFTooMany
	}
	return pages, nil
}

// countPagesInObjStm men-dekompresi stream setelah kamus /ObjStm lalu
// menghitung objek /Type /Page di dalamnya.
func countPagesInObjStm(rest []byte, budget *int64) (int, error) {
	loc := reStream.FindIndex(rest)
	if loc == nil {
		return 0, ErrPDFCorrupt
	}
	body := rest[loc[1]:]
	end := bytes.Index(body, []byte("endstream"))
	if end < 0 {
		return 0, ErrPDFCorrupt
	}
	zr, err := zlib.NewReader(bytes.NewReader(body[:end]))
	if err != nil {
		return 0, ErrPDFCorrupt
	}
	defer zr.Close()
	out, err := io.ReadAll(io.LimitReader(zr, *budget+1))
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return 0, ErrPDFCorrupt
	}
	*budget -= int64(len(out))
	if *budget < 0 {
		return 0, ErrPDFCorrupt
	}
	return len(reTypePage.FindAllIndex(out, -1)), nil
}

// SanitizeName membersihkan nama file asli untuk disimpan/ditampilkan:
// hanya basename, tanpa karakter kontrol, maksimal 120 karakter.
// Nama asli tidak pernah dicatat di log atau audit.
func SanitizeName(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if !utf8.ValidString(name) {
		name = strings.ToValidUTF8(name, "")
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if r := []rune(name); len(r) > 120 {
		ext := filepath.Ext(name)
		if len([]rune(ext)) > 10 {
			ext = ""
		}
		name = string(r[:120-len([]rune(ext))]) + ext
	}
	if name == "" || name == "." || name == ".." {
		return "dokumen"
	}
	return name
}
