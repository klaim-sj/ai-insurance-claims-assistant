package ocr

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	_ "image/jpeg" // decoder JPEG
	"image/png"
	"math"
)

// Pra-pemrosesan gambar sebelum Tesseract (Go murni, tanpa dependensi
// eksternal): normalisasi resolusi, peregangan kontras, denoise (median
// 3×3), deskew (proyeksi baris), dan binarisasi (Otsu atau Sauvola).
// Deterministik: masukan sama → keluaran sama.

// Binarize adalah mode binarisasi.
type Binarize string

const (
	// BinarizeNone: kirim grayscale (Tesseract membinarisasi sendiri).
	BinarizeNone Binarize = "none"
	// BinarizeOtsu: ambang global Otsu.
	BinarizeOtsu Binarize = "otsu"
	// BinarizeSauvola: ambang adaptif lokal; tahan bayangan/latar tidak rata.
	BinarizeSauvola Binarize = "sauvola"
)

// PreprocessOptions mengatur pra-pemrosesan.
type PreprocessOptions struct {
	// Disabled: lewati pra-pemrosesan sepenuhnya (kirim file asli).
	Disabled bool
	// TargetLongSide: sisi panjang (px) setelah normalisasi resolusi;
	// gambar lebih kecil diperbesar (maks. MaxUpscale), lebih besar dari
	// MaxLongSide diperkecil. 0 = default 2400 (≈ A4 pada ~200–300 dpi).
	TargetLongSide int
	MaxUpscale     float64 // default 3
	MaxLongSide    int     // default 4000
	// MaxSkewDeg: rentang pencarian deskew ±derajat; 0 = default 8;
	// negatif = deskew mati.
	MaxSkewDeg float64
	Denoise    bool
	Binarize   Binarize
	// RemoveLines: hapus garis horizontal/vertikal panjang (garis sel
	// tabel, garis isian) setelah binarisasi. Garis tabel membuat
	// Tesseract menganggap area tabel sebagai gambar sehingga teks di
	// dalam sel tidak terbaca. Hanya berlaku bila Binarize != none.
	RemoveLines bool
}

// DefaultPreprocess adalah konfigurasi produksi. Otsu dipilih (bukan
// Sauvola) karena pada dataset pengembangan Sauvola menurunkan akurasi
// gambar identitas berlatar halus; lihat docs/eval.
func DefaultPreprocess() PreprocessOptions {
	return PreprocessOptions{Denoise: true, Binarize: BinarizeOtsu, RemoveLines: true}
}

func (o PreprocessOptions) withDefaults() PreprocessOptions {
	if o.TargetLongSide <= 0 {
		o.TargetLongSide = 2400
	}
	if o.MaxUpscale <= 0 {
		o.MaxUpscale = 3
	}
	if o.MaxLongSide <= 0 {
		o.MaxLongSide = 4000
	}
	if o.MaxSkewDeg == 0 {
		o.MaxSkewDeg = 8
	}
	if o.Binarize == "" {
		o.Binarize = BinarizeOtsu
	}
	return o
}

// PreprocessInfo meringkas langkah yang diterapkan (tanpa isi dokumen).
type PreprocessInfo struct {
	Scale    float64
	SkewDeg  float64 // sudut kemiringan terdeteksi (derajat, positif = berlawanan jarum jam)
	Width    int
	Height   int
	Binarize Binarize
}

// maxPixels: batas piksel gambar masukan (pertahanan decompression bomb).
const maxPixels = 60_000_000

// ErrImageTooLarge: dimensi gambar melebihi batas.
var ErrImageTooLarge = errors.New("dimensi gambar melebihi batas pra-pemrosesan")

// gray adalah gambar grayscale 8-bit sederhana.
type gray struct {
	w, h int
	p    []uint8
}

func (g *gray) at(x, y int) uint8 { return g.p[y*g.w+x] }

// Preprocess menerapkan pra-pemrosesan dan mengembalikan PNG grayscale.
func Preprocess(data []byte, opt PreprocessOptions) ([]byte, PreprocessInfo, error) {
	opt = opt.withDefaults()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, PreprocessInfo{}, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return nil, PreprocessInfo{}, ErrImageTooLarge
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, PreprocessInfo{}, err
	}
	g, info := PreprocessImage(img, opt)
	var buf bytes.Buffer
	if err := png.Encode(&buf, g.toImage()); err != nil {
		return nil, info, err
	}
	return buf.Bytes(), info, nil
}

// PreprocessImage menjalankan seluruh langkah pada image.Image.
func PreprocessImage(img image.Image, opt PreprocessOptions) (*gray, PreprocessInfo) {
	opt = opt.withDefaults()
	g := toGray(img)
	info := PreprocessInfo{Scale: 1, Binarize: opt.Binarize}

	long := max(g.w, g.h)
	switch {
	case long < opt.TargetLongSide:
		info.Scale = math.Min(float64(opt.TargetLongSide)/float64(long), opt.MaxUpscale)
	case long > opt.MaxLongSide:
		info.Scale = float64(opt.MaxLongSide) / float64(long)
	}
	if math.Abs(info.Scale-1) > 0.05 {
		g = resize(g, int(math.Round(float64(g.w)*info.Scale)), int(math.Round(float64(g.h)*info.Scale)))
	} else {
		info.Scale = 1
	}
	g = stretchContrast(g)
	if opt.Denoise {
		g = median3(g)
	}
	if opt.MaxSkewDeg > 0 {
		info.SkewDeg = DetectSkew(g, opt.MaxSkewDeg)
		if math.Abs(info.SkewDeg) >= 0.3 {
			g = rotate(g, -info.SkewDeg)
		}
	}
	switch opt.Binarize {
	case BinarizeOtsu:
		g = threshold(g, otsu(g))
	case BinarizeSauvola:
		g = sauvola(g, 0, 0.2)
	}
	if opt.RemoveLines && opt.Binarize != BinarizeNone {
		g = removeLines(g)
	}
	info.Width, info.Height = g.w, g.h
	return g, info
}

func toGray(img image.Image) *gray {
	b := img.Bounds()
	g := &gray{w: b.Dx(), h: b.Dy(), p: make([]uint8, b.Dx()*b.Dy())}
	switch m := img.(type) {
	case *image.Gray:
		for y := 0; y < g.h; y++ {
			o := m.PixOffset(b.Min.X, b.Min.Y+y)
			copy(g.p[y*g.w:(y+1)*g.w], m.Pix[o:o+g.w])
		}
		return g
	case *image.YCbCr: // JPEG: kanal Y adalah luminans
		for y := 0; y < g.h; y++ {
			o := m.YOffset(b.Min.X, b.Min.Y+y)
			copy(g.p[y*g.w:(y+1)*g.w], m.Y[o:o+g.w])
		}
		return g
	}
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			c := color.GrayModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.Gray)
			g.p[y*g.w+x] = c.Y
		}
	}
	return g
}

func (g *gray) toImage() *image.Gray {
	out := image.NewGray(image.Rect(0, 0, g.w, g.h))
	copy(out.Pix, g.p)
	return out
}

// resize: interpolasi bilinear.
func resize(g *gray, w, h int) *gray {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	out := &gray{w: w, h: h, p: make([]uint8, w*h)}
	sx := float64(g.w) / float64(w)
	sy := float64(g.h) / float64(h)
	for y := 0; y < h; y++ {
		fy := (float64(y)+0.5)*sy - 0.5
		for x := 0; x < w; x++ {
			fx := (float64(x)+0.5)*sx - 0.5
			out.p[y*w+x] = bilinear(g, fx, fy, 255)
		}
	}
	return out
}

func bilinear(g *gray, fx, fy float64, fill uint8) uint8 {
	x0, y0 := int(math.Floor(fx)), int(math.Floor(fy))
	dx, dy := fx-float64(x0), fy-float64(y0)
	px := func(x, y int) float64 {
		if x < 0 || y < 0 || x >= g.w || y >= g.h {
			// Tepi dijepit (clamp) agar tidak muncul bingkai; di luar jauh = fill.
			if x < -1 || y < -1 || x > g.w || y > g.h {
				return float64(fill)
			}
			x = min(max(x, 0), g.w-1)
			y = min(max(y, 0), g.h-1)
		}
		return float64(g.p[y*g.w+x])
	}
	v := px(x0, y0)*(1-dx)*(1-dy) + px(x0+1, y0)*dx*(1-dy) + px(x0, y0+1)*(1-dx)*dy + px(x0+1, y0+1)*dx*dy
	return uint8(math.Round(math.Min(255, math.Max(0, v))))
}

// stretchContrast meregangkan histogram persentil 1%–99% ke 0–255
// (membantu foto gelap/pudar).
func stretchContrast(g *gray) *gray {
	var hist [256]int
	for _, v := range g.p {
		hist[v]++
	}
	n := len(g.p)
	lo, hi := 0, 255
	for acc := 0; lo < 255; lo++ {
		acc += hist[lo]
		if acc > n/100 {
			break
		}
	}
	for acc := 0; hi > 0; hi-- {
		acc += hist[hi]
		if acc > n/100 {
			break
		}
	}
	if hi-lo < 16 || (lo <= 2 && hi >= 253) {
		return g
	}
	var lut [256]uint8
	for i := range lut {
		v := float64(i-lo) * 255 / float64(hi-lo)
		lut[i] = uint8(math.Min(255, math.Max(0, v)))
	}
	out := &gray{w: g.w, h: g.h, p: make([]uint8, len(g.p))}
	for i, v := range g.p {
		out.p[i] = lut[v]
	}
	return out
}

// median3: filter median 3×3 (menghapus noise titik/garam-merica).
func median3(g *gray) *gray {
	out := &gray{w: g.w, h: g.h, p: make([]uint8, len(g.p))}
	var win [9]uint8
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			k := 0
			for dy := -1; dy <= 1; dy++ {
				yy := min(max(y+dy, 0), g.h-1)
				for dx := -1; dx <= 1; dx++ {
					xx := min(max(x+dx, 0), g.w-1)
					win[k] = g.p[yy*g.w+xx]
					k++
				}
			}
			// Insertion sort 9 elemen (tanpa alokasi).
			for i := 1; i < 9; i++ {
				v := win[i]
				j := i - 1
				for j >= 0 && win[j] > v {
					win[j+1] = win[j]
					j--
				}
				win[j+1] = v
			}
			out.p[y*g.w+x] = win[4]
		}
	}
	return out
}

// otsu menghitung ambang global yang memaksimalkan varians antarkelas.
func otsu(g *gray) uint8 {
	var hist [256]float64
	for _, v := range g.p {
		hist[v]++
	}
	total := float64(len(g.p))
	var sum float64
	for i, h := range hist {
		sum += float64(i) * h
	}
	var sumB, wB, best float64
	thr := 127
	for t := 0; t < 256; t++ {
		wB += hist[t]
		if wB == 0 {
			continue
		}
		wF := total - wB
		if wF == 0 {
			break
		}
		sumB += float64(t) * hist[t]
		mB, mF := sumB/wB, (sum-sumB)/wF
		if v := wB * wF * (mB - mF) * (mB - mF); v > best {
			best, thr = v, t
		}
	}
	return uint8(thr)
}

func threshold(g *gray, t uint8) *gray {
	out := &gray{w: g.w, h: g.h, p: make([]uint8, len(g.p))}
	for i, v := range g.p {
		if v > t {
			out.p[i] = 255
		}
	}
	return out
}

// sauvola: ambang lokal T = m·(1 + k·(s/R − 1)) dengan jendela persegi
// (default ≈ 1/40 sisi panjang, min 15 px) memakai integral image.
func sauvola(g *gray, win int, k float64) *gray {
	if win <= 0 {
		win = max(15, max(g.w, g.h)/40) | 1
	}
	w, h := g.w, g.h
	sum := make([]float64, (w+1)*(h+1))
	sq := make([]float64, (w+1)*(h+1))
	for y := 0; y < h; y++ {
		var rs, rq float64
		for x := 0; x < w; x++ {
			v := float64(g.p[y*w+x])
			rs += v
			rq += v * v
			sum[(y+1)*(w+1)+x+1] = sum[y*(w+1)+x+1] + rs
			sq[(y+1)*(w+1)+x+1] = sq[y*(w+1)+x+1] + rq
		}
	}
	r := win / 2
	out := &gray{w: w, h: h, p: make([]uint8, len(g.p))}
	const R = 128.0
	for y := 0; y < h; y++ {
		y0, y1 := max(y-r, 0), min(y+r+1, h)
		for x := 0; x < w; x++ {
			x0, x1 := max(x-r, 0), min(x+r+1, w)
			n := float64((y1 - y0) * (x1 - x0))
			s := sum[y1*(w+1)+x1] - sum[y0*(w+1)+x1] - sum[y1*(w+1)+x0] + sum[y0*(w+1)+x0]
			q := sq[y1*(w+1)+x1] - sq[y0*(w+1)+x1] - sq[y1*(w+1)+x0] + sq[y0*(w+1)+x0]
			m := s / n
			sd := math.Sqrt(math.Max(0, q/n-m*m))
			t := m * (1 + k*(sd/R-1))
			if float64(g.p[y*w+x]) > t {
				out.p[y*w+x] = 255
			}
		}
	}
	return out
}

// DetectSkew memperkirakan kemiringan baris teks (derajat; positif =
// teks naik ke kanan / berlawanan jarum jam) dengan memaksimalkan
// "ketajaman" proyeksi horizontal piksel gelap pada gambar yang
// diperkecil. Pencarian kasar 0,5° lalu halus 0,1°.
func DetectSkew(g *gray, maxDeg float64) float64 {
	small := g
	if long := max(g.w, g.h); long > 1000 {
		f := 1000 / float64(long)
		small = resize(g, max(1, int(float64(g.w)*f)), max(1, int(float64(g.h)*f)))
	}
	t := otsu(small)
	type pt struct{ x, y float64 }
	var pts []pt
	for y := 0; y < small.h; y++ {
		for x := 0; x < small.w; x++ {
			if small.p[y*small.w+x] <= t {
				pts = append(pts, pt{float64(x), float64(y)})
			}
		}
	}
	// Terlalu sedikit (kosong) atau terlalu banyak (gambar gelap/latar
	// penuh) piksel gelap: tidak dapat diperkirakan dengan andal.
	if len(pts) < 50 || len(pts) > len(small.p)*6/10 {
		return 0
	}
	diag := int(math.Hypot(float64(small.w), float64(small.h))) + 2
	hist := make([]float64, 2*diag)
	score := func(deg float64) float64 {
		for i := range hist {
			hist[i] = 0
		}
		rad := deg * math.Pi / 180
		sin, cos := math.Sin(rad), math.Cos(rad)
		for _, p := range pts {
			// Koordinat y setelah memutar balik sebesar deg.
			yy := p.y*cos + p.x*sin
			hist[int(yy)+diag]++
		}
		var s float64
		for i := 1; i < len(hist); i++ {
			d := hist[i] - hist[i-1]
			s += d * d
		}
		return s
	}
	best, bestScore := 0.0, score(0)
	for d := -maxDeg; d <= maxDeg+1e-9; d += 0.5 {
		if s := score(d); s > bestScore*1.0001 {
			best, bestScore = d, s
		}
	}
	center := best
	for d := center - 0.5; d <= center+0.5+1e-9; d += 0.1 {
		if s := score(d); s > bestScore*1.0001 {
			best, bestScore = d, s
		}
	}
	return math.Round(best*10) / 10
}

// rotate memutar gambar sebesar deg (positif = berlawanan jarum jam pada
// koordinat layar) di sekitar pusat, ukuran tetap, latar putih.
func rotate(g *gray, deg float64) *gray {
	rad := deg * math.Pi / 180
	sin, cos := math.Sin(rad), math.Cos(rad)
	cx, cy := float64(g.w)/2, float64(g.h)/2
	out := &gray{w: g.w, h: g.h, p: make([]uint8, len(g.p))}
	for y := 0; y < g.h; y++ {
		dy := float64(y) - cy
		for x := 0; x < g.w; x++ {
			dx := float64(x) - cx
			// Pemetaan balik: titik keluaran → titik sumber.
			sx := cx + dx*cos - dy*sin
			sy := cy + dx*sin + dy*cos
			out.p[y*g.w+x] = bilinear(g, sx, sy, 255)
		}
	}
	return out
}

// removeLines menghapus garis horizontal yang lebih panjang dari 1/12
// lebar gambar dan garis vertikal lebih panjang dari 1/25 tinggi gambar
// (jauh lebih panjang dari goresan huruf). Celah putih kecil (≤ 3 px,
// mis. garis putus akibat noise/JPEG) tetap dihitung bagian garis, dan
// piksel hitam yang menempel tepat di atas/bawah (kiri/kanan) garis ikut
// dihapus agar garis tebal tidak menyisakan serpihan. Gambar harus biner.
func removeLines(g *gray) *gray {
	out := &gray{w: g.w, h: g.h, p: append([]uint8(nil), g.p...)}
	const maxGap = 3
	minH := max(40, g.w/12)
	minV := max(40, g.h/25)
	dark := func(x, y int) bool { return g.p[y*g.w+x] == 0 }
	clear := func(x, y int) {
		if x >= 0 && y >= 0 && x < g.w && y < g.h {
			out.p[y*g.w+x] = 255
		}
	}
	// scan menelusuri satu garis piksel (n piksel, at(i) = gelap?) dan
	// memanggil erase(a, b) untuk setiap segmen garis panjang [a, b).
	scan := func(n int, at func(int) bool, minLen int, erase func(a, b int)) {
		start, last, gap := -1, -1, 0
		for i := 0; i <= n; i++ {
			if i < n && at(i) {
				if start < 0 {
					start = i
				}
				last, gap = i, 0
				continue
			}
			if start < 0 {
				continue
			}
			gap++
			if i < n && gap <= maxGap {
				continue
			}
			if last-start+1 >= minLen {
				erase(start, last+1)
			}
			start, last, gap = -1, -1, 0
		}
	}
	for y := 0; y < g.h; y++ {
		yy := y
		scan(g.w, func(x int) bool { return dark(x, yy) }, minH, func(a, b int) {
			for x := a; x < b; x++ {
				for dy := -2; dy <= 2; dy++ {
					clear(x, yy+dy)
				}
			}
		})
	}
	for x := 0; x < g.w; x++ {
		xx := x
		scan(g.h, func(y int) bool { return dark(xx, y) }, minV, func(a, b int) {
			for y := a; y < b; y++ {
				for dx := -2; dx <= 2; dx++ {
					clear(xx+dx, y)
				}
			}
		})
	}
	return out
}
