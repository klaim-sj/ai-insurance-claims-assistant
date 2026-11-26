package validation

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// NameResult adalah hasil perbandingan dua nama ternormalisasi.
type NameResult string

const (
	NameEqual NameResult = "equal"
	// NameAbbreviation: kemungkinan singkatan (inisial atau bagian nama
	// tidak ditulis), bukan kepastian orang yang sama.
	NameAbbreviation NameResult = "abbreviation"
	NameDifferent    NameResult = "different"
)

type titleSet struct {
	prefix map[string]bool // sapaan di depan nama
	suffix map[string]bool // gelar akademik di belakang nama
}

func newTitleSet(titles []string) titleSet {
	ts := titleSet{prefix: map[string]bool{}, suffix: map[string]bool{}}
	for _, t := range titles {
		t = strings.ToLower(strings.TrimSpace(t))
		key := strings.ReplaceAll(t, ".", "")
		if key == "" {
			continue
		}
		if strings.Contains(t, ".") || key == "amd" {
			ts.suffix[key] = true
		} else {
			ts.prefix[key] = true
		}
	}
	return ts
}

// normalize: huruf kecil, tanpa diakritik, tanpa tanda baca, spasi
// dirapikan, sapaan di depan dan gelar di belakang (atau setelah koma)
// dibuang. Hasilnya daftar token nama.
func (ts titleSet) normalize(name string) []string {
	name = strings.ToLower(norm.NFKD.String(name))
	// Bagian setelah koma pertama adalah gelar ("Rani Dewi, S.Kom").
	if i := strings.IndexByte(name, ','); i >= 0 {
		name = name[:i]
	}
	var toks []string
	for _, raw := range strings.Fields(name) {
		// Token seperti "dr.rani" dipisah di titik.
		for _, part := range strings.Split(raw, ".") {
			var b strings.Builder
			for _, r := range part {
				if unicode.IsLetter(r) { // tanda diakritik (Mn) hasil NFKD terbuang
					b.WriteRune(r)
				}
			}
			if b.Len() > 0 {
				toks = append(toks, b.String())
			}
		}
	}
	// Gelar akademik bertitik ("s.kom") sudah terpecah menjadi "s","kom";
	// cocokkan ulang pasangan token di belakang.
	for len(toks) > 1 {
		n := len(toks)
		if ts.suffix[toks[n-1]] {
			toks = toks[:n-1]
			continue
		}
		if n > 2 && ts.suffix[toks[n-2]+toks[n-1]] {
			toks = toks[:n-2]
			continue
		}
		break
	}
	for len(toks) > 1 && ts.prefix[toks[0]] {
		toks = toks[1:]
	}
	return toks
}

func tokenMatch(a, b string) (match, full bool) {
	if a == b {
		return true, true
	}
	if len(a) == 1 && strings.HasPrefix(b, a) || len(b) == 1 && strings.HasPrefix(a, b) {
		return true, false
	}
	return false, false
}

// compareNames membandingkan dua nama ternormalisasi.
//
//   - equal: token identik, atau sama setelah spasi dihilangkan
//     ("kusumadewi" = "kusuma dewi").
//   - abbreviation: setiap token nama yang lebih pendek cocok berurutan
//     dengan token nama yang lebih panjang, berupa token sama atau inisial,
//     token pertama cocok, dan minimal satu token cocok penuh.
//     Contoh: "R. Kusuma Dewi" ~ "Rani Kusuma Dewi"; "Rani Dewi" ~ "Rani
//     Kusuma Dewi".
//   - different: selain itu ("Rani Kusumawati" vs "Rani Kusuma Dewi").
func compareNames(a, b []string) NameResult {
	if strings.Join(a, " ") == strings.Join(b, " ") || strings.Join(a, "") == strings.Join(b, "") {
		return NameEqual
	}
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	if len(short) == 0 {
		return NameDifferent
	}
	if m, _ := tokenMatch(short[0], long[0]); !m {
		return NameDifferent
	}
	j, fulls := 0, 0
	for _, s := range short {
		matched := false
		for j < len(long) {
			m, full := tokenMatch(s, long[j])
			j++
			if m {
				matched = true
				if full {
					fulls++
				}
				break
			}
		}
		if !matched {
			return NameDifferent
		}
	}
	if fulls == 0 {
		return NameDifferent
	}
	return NameAbbreviation
}
