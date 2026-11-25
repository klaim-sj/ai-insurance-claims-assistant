package documents

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/testutil"
)

func TestValidate(t *testing.T) {
	png := testutil.PNG(1)
	tests := []struct {
		name      string
		file      string
		data      []byte
		wantMIME  string
		wantPages int
		wantErr   error
	}{
		{"pdf valid", "formulir.pdf", testutil.PDF(2), MIMEPDF, 2, nil},
		{"pdf object stream", "surat.PDF", testutil.ObjStmPDF(3), MIMEPDF, 3, nil},
		{"png valid", "ktp.png", png, MIMEPNG, 1, nil},
		{"jpeg valid", "kuitansi.jpeg", testutil.JPEG(1), MIMEJPEG, 1, nil},
		{"jpg valid", "kuitansi.JPG", testutil.JPEG(2), MIMEJPEG, 1, nil},
		{"kosong", "a.pdf", nil, "", 0, ErrEmpty},
		{"terlalu besar", "a.pdf", append(testutil.PDF(1), make([]byte, MaxFileSize)...), "", 0, ErrTooLarge},
		// TC-02: ekstensi palsu.
		{"exe diberi ekstensi pdf", "formulir.pdf", append([]byte("MZ\x90\x00"), make([]byte, 64)...), "", 0, ErrUnsupported},
		{"html diberi ekstensi png", "foto.png", []byte("<html><script>alert(1)</script></html>"), "", 0, ErrUnsupported},
		{"png diberi ekstensi pdf", "formulir.pdf", png, "", 0, ErrExtMismatch},
		{"pdf diberi ekstensi jpg", "foto.jpg", testutil.PDF(1), "", 0, ErrExtMismatch},
		{"pdf tanpa ekstensi", "formulir", testutil.PDF(1), "", 0, ErrExtMismatch},
		{"pdf terenkripsi", "a.pdf", testutil.EncryptedPDF(), "", 0, ErrPDFEncrypted},
		{"pdf terpotong", "a.pdf", testutil.PDF(1)[:60], "", 0, ErrPDFCorrupt},
		{"pdf tanpa halaman", "a.pdf", bytes.ReplaceAll(testutil.PDF(1), []byte("/Type /Page "), []byte("/Type /Xxxx ")), "", 0, ErrPDFNoPages},
		{"pdf terlalu banyak halaman", "a.pdf", testutil.PDF(MaxPDFPages + 1), "", 0, ErrPDFTooMany},
		{"pdf object stream rusak", "a.pdf", bytes.Replace(testutil.ObjStmPDF(1), []byte("stream\n"), []byte("stream\nxx"), 1), "", 0, ErrPDFCorrupt},
		{"png terpotong", "a.png", png[:len(png)/2], "", 0, ErrImageCorrupt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := Validate(tt.file, tt.data)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, ingin %v", err, tt.wantErr)
			}
			if err == nil && (info.MIME != tt.wantMIME || info.Pages != tt.wantPages) {
				t.Fatalf("info = %+v, ingin %s/%d", info, tt.wantMIME, tt.wantPages)
			}
		})
	}
}

func TestSanitizeName(t *testing.T) {
	tests := map[string]string{
		"formulir.pdf":                    "formulir.pdf",
		"../../etc/passwd":                "passwd",
		`C:\Users\x\ktp.png`:              "ktp.png",
		"a\x00b\nc\".pdf":                 "abc.pdf",
		"   ":                             "dokumen",
		"..":                              "dokumen",
		strings.Repeat("a", 300) + ".pdf": strings.Repeat("a", 116) + ".pdf",
	}
	for in, want := range tests {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, ingin %q", in, got, want)
		}
	}
}

func TestStorageRejectsTraversal(t *testing.T) {
	st, err := NewStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.Put([]byte("isi sintetis"))
	if err != nil {
		t.Fatal(err)
	}
	if !storagePathRe.MatchString(p) {
		t.Fatalf("storage path %q tidak acak/terformat", p)
	}
	f, err := st.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	for _, bad := range []string{"../x", "/etc/passwd", "ab/../../x", ""} {
		if _, err := st.Open(bad); err == nil {
			t.Errorf("Open(%q) seharusnya ditolak", bad)
		}
	}
}
