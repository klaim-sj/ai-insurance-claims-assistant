// Package ocr mengubah dokumen PDF/JPG/PNG menjadi teks per halaman.
//
// Alur: PDF dicoba dengan pdftotext (lapisan teks); halaman tanpa teks
// dirender dengan pdftoppm lalu dibaca Tesseract. Gambar langsung dibaca
// Tesseract. Confidence halaman adalah rata-rata confidence kata dari
// output TSV Tesseract (lapisan teks PDF dianggap 100).
//
// Hasil OCR berisi teks mentah dan TIDAK boleh di-log atau dimasukkan ke
// audit. Pemanggil wajib menyamarkan NIK (pii.MaskNIK) sebelum menyimpan.
package ocr

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Nama engine (sama dengan kolom ocr_results.engine).
const (
	EnginePDFText   = "pdftotext"
	EngineTesseract = "tesseract"
)

// Word adalah satu kata beserta confidence 0–100.
type Word struct {
	Text string
	Conf float64
}

// Line adalah satu baris teks.
type Line struct {
	Words []Word
}

// Text menggabungkan kata dengan spasi.
func (l Line) Text() string {
	parts := make([]string, len(l.Words))
	for i, w := range l.Words {
		parts[i] = w.Text
	}
	return strings.Join(parts, " ")
}

// Page adalah hasil OCR satu halaman.
type Page struct {
	Number        int
	Engine        string
	EngineVersion string
	Lines         []Line
	// MeanConfidence: rata-rata confidence kata; nil bila tidak ada kata.
	MeanConfidence *float64
	WordCount      int
}

// Text menggabungkan baris dengan newline.
func (p Page) Text() string {
	lines := make([]string, len(p.Lines))
	for i, l := range p.Lines {
		lines[i] = l.Text()
	}
	return strings.Join(lines, "\n")
}

// Result adalah hasil OCR satu dokumen.
type Result struct {
	Pages []Page
}

// Engine membaca teks dari dokumen.
type Engine interface {
	Process(ctx context.Context, data []byte, mime string) (Result, error)
}

// Code adalah kode error yang disimpan di claim_documents.processing_error_code.
type Code string

const (
	CodeTimeout           Code = "OCR_TIMEOUT"
	CodeEngineUnavailable Code = "OCR_ENGINE_UNAVAILABLE"
	CodeEngineError       Code = "OCR_ENGINE_ERROR"
	CodePDFTextFailed     Code = "PDF_TEXT_FAILED"
	CodePDFRenderFailed   Code = "PDF_RENDER_FAILED"
	CodeUnsupportedType   Code = "UNSUPPORTED_TYPE"
	CodeTooManyPages      Code = "TOO_MANY_PAGES"
)

// Error adalah kegagalan OCR. Pesan tidak pernah memuat isi dokumen,
// stderr mentah, atau path file.
type Error struct {
	Code      Code
	Retryable bool
	Err       error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("ocr %s: %v", e.Code, e.Err)
	}
	return "ocr " + string(e.Code)
}

func (e *Error) Unwrap() error { return e.Err }

// CodeOf mengembalikan kode error OCR, atau "" bila err bukan *Error.
func CodeOf(err error) Code {
	var oe *Error
	if errors.As(err, &oe) {
		return oe.Code
	}
	return ""
}

func meanConfidence(lines []Line) (*float64, int) {
	var sum float64
	n := 0
	for _, l := range lines {
		for _, w := range l.Words {
			sum += w.Conf
			n++
		}
	}
	if n == 0 {
		return nil, 0
	}
	m := sum / float64(n)
	return &m, n
}
