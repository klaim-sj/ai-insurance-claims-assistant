package ocr

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/testutil"
)

const samples = "../../sample-data/"

// requireTools: di CI alat OCR wajib terpasang; secara lokal test dilewati
// bila belum ada.
func requireTools(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"pdftotext", "pdftoppm", "tesseract"} {
		if _, err := exec.LookPath(bin); err != nil {
			if os.Getenv("CI") != "" {
				t.Fatalf("%s wajib terpasang di CI", bin)
			}
			t.Skipf("%s tidak terpasang", bin)
		}
	}
}

func readSample(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(samples + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// assertEmptyDir memastikan direktori sementara sudah dibersihkan.
func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("direktori sementara tidak dibersihkan: %d entri", len(entries))
	}
}

func TestTextPDFUsesTextLayer(t *testing.T) {
	requireTools(t)
	tmp := t.TempDir()
	e := NewExec(ExecConfig{TempDir: tmp})
	res, err := e.Process(context.Background(), readSample(t, "01_formulir_klaim.pdf"), "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 1 || res.Pages[0].Engine != EnginePDFText {
		t.Fatalf("harus 1 halaman via pdftotext, dapat %+v", res.Pages)
	}
	p := res.Pages[0]
	if p.MeanConfidence == nil || *p.MeanConfidence != 100 || p.EngineVersion == "unknown" {
		t.Fatalf("metadata halaman tidak sesuai: conf=%v version=%q", p.MeanConfidence, p.EngineVersion)
	}
	if !strings.Contains(p.Text(), "Nomor polis : POL-DEMO-0101") {
		t.Fatal("teks lapisan PDF tidak terbaca sesuai urutan baris")
	}
	assertEmptyDir(t, tmp)
}

func TestScannedPDFFallsBackToTesseract(t *testing.T) {
	requireTools(t)
	jpg, w, h, err := testutil.PNGToJPEG(readSample(t, "02_identitas.png"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	e := NewExec(ExecConfig{TempDir: tmp})
	res, err := e.Process(context.Background(), testutil.ScannedPDF(jpg, w, h), "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 1 || res.Pages[0].Engine != EngineTesseract {
		t.Fatalf("PDF hasil scan harus dibaca Tesseract, dapat %+v", res.Pages)
	}
	p := res.Pages[0]
	if p.MeanConfidence == nil || *p.MeanConfidence < 60 || p.WordCount < 5 {
		t.Fatalf("confidence/kata terlalu rendah: %v / %d", p.MeanConfidence, p.WordCount)
	}
	if !strings.Contains(p.Text(), "Rani Kusuma Dewi") {
		t.Fatal("nama sintetis tidak terbaca dari PDF hasil scan")
	}
	assertEmptyDir(t, tmp)
}

func TestBlurryImageHasLowQuality(t *testing.T) {
	requireTools(t)
	e := NewExec(ExecConfig{TempDir: t.TempDir()})
	res, err := e.Process(context.Background(), readSample(t, "d_identitas_buram.png"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	p := res.Pages[0]
	if p.Engine != EngineTesseract {
		t.Fatalf("gambar harus dibaca Tesseract, dapat %q", p.Engine)
	}
	if p.WordCount >= 5 && p.MeanConfidence != nil && *p.MeanConfidence >= 60 {
		t.Fatalf("gambar buram seharusnya berkualitas rendah: %d kata, conf %v", p.WordCount, *p.MeanConfidence)
	}
}

func TestTimeoutReturnsCode(t *testing.T) {
	requireTools(t)
	tmp := t.TempDir()
	e := NewExec(ExecConfig{TempDir: tmp, Timeout: time.Millisecond})
	_, err := e.Process(context.Background(), readSample(t, "02_identitas.png"), "image/png")
	if CodeOf(err) != CodeTimeout {
		t.Fatalf("harus OCR_TIMEOUT, dapat %v", err)
	}
	var oe *Error
	if !errorsAs(err, &oe) || !oe.Retryable {
		t.Fatal("timeout harus dapat dicoba ulang")
	}
	assertEmptyDir(t, tmp)
}

func TestMissingBinary(t *testing.T) {
	e := NewExec(ExecConfig{TempDir: t.TempDir(), Tesseract: filepath.Join(t.TempDir(), "tidak-ada")})
	_, err := e.Process(context.Background(), testutil.PNG(1), "image/png")
	if c := CodeOf(err); c != CodeEngineError && c != CodeEngineUnavailable {
		t.Fatalf("binary tidak ada harus gagal dengan kode engine, dapat %v", err)
	}
	if err := e.Check(context.Background()); CodeOf(err) != CodeEngineUnavailable {
		t.Fatalf("Check harus OCR_ENGINE_UNAVAILABLE, dapat %v", err)
	}
}

func TestUnsupportedType(t *testing.T) {
	_, err := NewExec(ExecConfig{}).Process(context.Background(), []byte("x"), "text/plain")
	if CodeOf(err) != CodeUnsupportedType {
		t.Fatalf("harus UNSUPPORTED_TYPE, dapat %v", err)
	}
}

func TestParseTSVRebuildsRows(t *testing.T) {
	tsv := "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
		"1\t1\t0\t0\t0\t0\t0\t0\t1000\t1000\t-1\t\n" +
		// Label dan nilai di blok berbeda tetapi sejajar vertikal.
		"5\t1\t1\t1\t1\t1\t10\t100\t80\t20\t95.5\tNama\n" +
		"5\t1\t2\t1\t1\t1\t400\t102\t10\t20\t60\t:\n" +
		"5\t1\t2\t1\t1\t2\t420\t101\t80\t20\t91\tUji\n" +
		"5\t1\t1\t1\t2\t1\t10\t200\t80\t20\t-1\t \n" +
		"5\t1\t1\t1\t2\t1\t10\t200\t80\t20\t88\tBaris2\n"
	lines, err := ParseTSV([]byte(tsv))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0].Text() != "Nama : Uji" || lines[1].Text() != "Baris2" {
		t.Fatalf("baris tidak tersusun benar: %+v", lines)
	}
	m, n := meanConfidence(lines)
	if n != 4 || m == nil || *m < 83 || *m > 84 {
		t.Fatalf("rata-rata confidence salah: %v (%d kata)", m, n)
	}
	if _, err := ParseTSV([]byte("bukan tsv\n")); err == nil {
		t.Fatal("header tidak dikenal harus error")
	}
}

func errorsAs(err error, target **Error) bool { return errors.As(err, target) }
