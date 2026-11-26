package testutil

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
)

// PDF membuat PDF sintetis minimal dengan n halaman (tanpa isi teks).
func PDF(n int) []byte {
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	b.WriteString("1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n")
	kids := make([]string, n)
	for i := range n {
		kids[i] = fmt.Sprintf("%d 0 R", i+3)
	}
	fmt.Fprintf(&b, "2 0 obj << /Type /Pages /Kids [%s] /Count %d >> endobj\n", strings.Join(kids, " "), n)
	for i := range n {
		fmt.Fprintf(&b, "%d 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] >> endobj\n", i+3)
	}
	b.WriteString("xref\n0 1\n0000000000 65535 f \ntrailer << /Root 1 0 R >>\nstartxref\n0\n%%EOF\n")
	return []byte(b.String())
}

// EncryptedPDF membuat PDF sintetis yang menandai dirinya terenkripsi.
func EncryptedPDF() []byte {
	return bytes.Replace(PDF(1), []byte("trailer << /Root 1 0 R >>"),
		[]byte("trailer << /Root 1 0 R /Encrypt 9 0 R >>"), 1)
}

// ObjStmPDF membuat PDF sintetis dengan n halaman di dalam object stream
// terkompresi (format PDF 1.5+).
func ObjStmPDF(n int) []byte {
	var objs strings.Builder
	for i := range n {
		fmt.Fprintf(&objs, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] >>\n%d", i)
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, _ = zw.Write([]byte(objs.String()))
	_ = zw.Close()
	var b bytes.Buffer
	b.WriteString("%PDF-1.5\n")
	b.WriteString("1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n")
	fmt.Fprintf(&b, "2 0 obj << /Type /Pages /Count %d >> endobj\n", n)
	fmt.Fprintf(&b, "3 0 obj << /Type /ObjStm /N %d /First 0 /Filter /FlateDecode /Length %d >>\nstream\n", n, z.Len())
	b.Write(z.Bytes())
	b.WriteString("\nendstream\nendobj\n")
	b.WriteString("startxref\n0\n%%EOF\n")
	return b.Bytes()
}

func img(w, h int, seed uint8) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			m.Set(x, y, color.RGBA{uint8(x) + seed, uint8(y), seed, 255})
		}
	}
	return m
}

// PNG membuat gambar PNG sintetis; seed berbeda menghasilkan isi berbeda.
func PNG(seed uint8) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, img(32, 24, seed))
	return b.Bytes()
}

// JPEG membuat gambar JPEG sintetis.
func JPEG(seed uint8) []byte {
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img(32, 24, seed), nil)
	return b.Bytes()
}

// ScannedPDF membungkus gambar JPEG menjadi PDF satu halaman TANPA lapisan
// teks (menyimulasikan dokumen hasil scan). w dan h adalah ukuran piksel.
func ScannedPDF(jpg []byte, w, h int) []byte {
	var b bytes.Buffer
	offsets := make([]int, 0, 5)
	obj := func(s string) {
		offsets = append(offsets, b.Len())
		b.WriteString(s)
	}
	b.WriteString("%PDF-1.4\n")
	// Halaman A4 (595x842 pt); gambar diskalakan memenuhi lebar.
	pw, ph := 595.0, 595.0*float64(h)/float64(w)
	content := fmt.Sprintf("q %.2f 0 0 %.2f 0 0 cm /Im0 Do Q", pw, ph)
	obj("1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n")
	obj("2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n")
	obj(fmt.Sprintf("3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >> endobj\n", pw, ph))
	obj(fmt.Sprintf("4 0 obj << /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n", w, h, len(jpg)))
	b.Write(jpg)
	b.WriteString("\nendstream\nendobj\n")
	obj(fmt.Sprintf("5 0 obj << /Length %d >>\nstream\n%s\nendstream\nendobj\n", len(content), content))
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer << /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return b.Bytes()
}

// PNGToJPEG mengubah PNG menjadi JPEG berkualitas tinggi beserta ukurannya.
func PNGToJPEG(p []byte) ([]byte, int, int, error) {
	m, err := png.Decode(bytes.NewReader(p))
	if err != nil {
		return nil, 0, 0, err
	}
	rgba := image.NewRGBA(m.Bounds())
	for y := m.Bounds().Min.Y; y < m.Bounds().Max.Y; y++ {
		for x := m.Bounds().Min.X; x < m.Bounds().Max.X; x++ {
			rgba.Set(x, y, m.At(x, y))
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, rgba, &jpeg.Options{Quality: 95}); err != nil {
		return nil, 0, 0, err
	}
	return b.Bytes(), m.Bounds().Dx(), m.Bounds().Dy(), nil
}
