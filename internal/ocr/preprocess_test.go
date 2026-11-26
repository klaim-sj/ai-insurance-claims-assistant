package ocr

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand"
	"testing"
)

// textLike membuat gambar sintetis berisi "baris teks" (balok hitam
// pendek berjajar) di latar putih.
func textLike(w, h int) *gray {
	g := &gray{w: w, h: h, p: make([]uint8, w*h)}
	for i := range g.p {
		g.p[i] = 255
	}
	for y := 60; y+14 < h-40; y += 40 {
		for x := 40; x+18 < w-40; x += 26 {
			for yy := y; yy < y+14; yy++ {
				for xx := x; xx < x+18; xx++ {
					g.p[yy*w+xx] = 0
				}
			}
		}
	}
	return g
}

func TestDeskewRecoversAngle(t *testing.T) {
	base := textLike(900, 700)
	if a := DetectSkew(base, 8); math.Abs(a) > 0.2 {
		t.Fatalf("gambar lurus terdeteksi miring %.1f°", a)
	}
	for _, deg := range []float64{-6, -3, 2, 4.5} {
		rot := rotate(base, deg)
		got := DetectSkew(rot, 8)
		if math.Abs(math.Abs(got)-math.Abs(deg)) > 0.5 {
			t.Fatalf("diputar %.1f°, terdeteksi %.1f°", deg, got)
		}
		// Koreksi seperti PreprocessImage harus meluruskan kembali.
		fixed := rotate(rot, -got)
		if a := DetectSkew(fixed, 8); math.Abs(a) > 0.5 {
			t.Fatalf("setelah koreksi %.1f° masih miring %.1f°", deg, a)
		}
	}
}

func TestOtsuSeparatesBimodal(t *testing.T) {
	g := &gray{w: 100, h: 100, p: make([]uint8, 10000)}
	for i := range g.p {
		if i%3 == 0 {
			g.p[i] = 40 + uint8(i%10)
		} else {
			g.p[i] = 200 + uint8(i%10)
		}
	}
	thr := otsu(g)
	if thr < 49 || thr >= 200 {
		t.Fatalf("ambang Otsu %d tidak memisahkan 40–49 dan 200–209", thr)
	}
	b := threshold(g, thr)
	for i, v := range b.p {
		if (i%3 == 0) != (v == 0) {
			t.Fatalf("piksel %d salah kelas", i)
		}
	}
}

func TestMedianRemovesSaltNoise(t *testing.T) {
	g := &gray{w: 50, h: 50, p: make([]uint8, 2500)}
	for i := range g.p {
		g.p[i] = 255
	}
	rnd := rand.New(rand.NewSource(1))
	for range 100 {
		g.p[rnd.Intn(len(g.p))] = 0 // titik noise terisolasi
	}
	out := median3(g)
	dark := 0
	for _, v := range out.p {
		if v < 128 {
			dark++
		}
	}
	if dark != 0 {
		t.Fatalf("median 3×3 masih menyisakan %d titik noise", dark)
	}
}

func TestRemoveLinesKeepsText(t *testing.T) {
	g := textLike(900, 400)
	// Garis tabel horizontal & vertikal.
	for x := 10; x < 890; x++ {
		g.p[30*g.w+x], g.p[31*g.w+x] = 0, 0
	}
	for y := 10; y < 390; y++ {
		g.p[y*g.w+20] = 0
	}
	out := removeLines(g)
	if out.p[30*g.w+400] != 255 || out.p[200*g.w+20] != 255 {
		t.Fatal("garis tabel harus dihapus")
	}
	if out.p[65*g.w+45] != 0 {
		t.Fatal("balok teks tidak boleh terhapus")
	}
}

func TestResolutionNormalization(t *testing.T) {
	small := image.NewGray(image.Rect(0, 0, 400, 250))
	for i := range small.Pix {
		small.Pix[i] = 255
	}
	g, info := PreprocessImage(small, PreprocessOptions{MaxSkewDeg: -1, Binarize: BinarizeNone})
	if info.Scale != 3 || g.w != 1200 || g.h != 750 {
		t.Fatalf("gambar kecil harus diperbesar (maks ×3): skala %.2f, %dx%d", info.Scale, g.w, g.h)
	}
	big := image.NewGray(image.Rect(0, 0, 5000, 3000))
	_, info = PreprocessImage(big, PreprocessOptions{MaxSkewDeg: -1, Binarize: BinarizeNone})
	if info.Width != 4000 || info.Height != 2400 {
		t.Fatalf("gambar besar harus diperkecil ke 4000 px: %dx%d", info.Width, info.Height)
	}
}

func TestPreprocessDeterministicAndBinary(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 600, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 600; x++ {
			v := uint8(230 - x/10) // latar bergradasi
			if y > 100 && y < 120 && x > 50 && x < 550 && (x/12)%2 == 0 {
				v = 30
			}
			img.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	a, _, err := Preprocess(buf.Bytes(), DefaultPreprocess())
	if err != nil {
		t.Fatal(err)
	}
	b, _, _ := Preprocess(buf.Bytes(), DefaultPreprocess())
	if !bytes.Equal(a, b) {
		t.Fatal("pra-pemrosesan harus deterministik")
	}
	out, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	gi := out.(*image.Gray)
	for _, v := range gi.Pix {
		if v != 0 && v != 255 {
			t.Fatalf("keluaran Otsu harus biner, dapat nilai %d", v)
		}
	}
}

func TestPreprocessRejectsHugeAndInvalid(t *testing.T) {
	if _, _, err := Preprocess([]byte("bukan gambar"), DefaultPreprocess()); err == nil {
		t.Fatal("data bukan gambar harus ditolak")
	}
	// Header PNG dengan dimensi raksasa (tanpa piksel nyata).
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1)))
	raw := buf.Bytes()
	// IHDR: lebar & tinggi di offset 16..23.
	copy(raw[16:24], []byte{0, 0, 0x27, 0x10, 0, 0, 0x27, 0x10}) // 10000×10000
	if _, _, err := Preprocess(raw, DefaultPreprocess()); err == nil {
		t.Fatal("dimensi di atas batas harus ditolak sebelum dekode penuh")
	}
}

func TestParsePSMs(t *testing.T) {
	cases := map[string][]string{
		"":            {"6"},
		"6,11":        {"6", "11"},
		" 4 , 4, 99 ": {"4"},
		"x,y":         {"6"},
	}
	for in, want := range cases {
		got := parsePSMs(in)
		if len(got) != len(want) {
			t.Fatalf("parsePSMs(%q) = %v, harap %v", in, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("parsePSMs(%q) = %v, harap %v", in, got, want)
			}
		}
	}
}
