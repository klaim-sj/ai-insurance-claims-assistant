// Package pii berisi utilitas untuk menyamarkan data pribadi sebelum
// disimpan atau ditampilkan.
//
// NIK (16 digit) tidak pernah disimpan utuh: teks OCR, kutipan sumber, dan
// nilai field melewati MaskNIK lebih dulu.
package pii

import (
	"regexp"
	"strings"
)

// nikRe mencocokkan 16 digit berurutan, atau 4-4-4-4 dengan pemisah spasi,
// titik, atau tanda hubung yang konsisten. Batas digit di kedua sisi
// dicek manual agar deret lebih panjang (mis. 17 digit) tetap tertangkap
// oleh pemeriksaan ContainsUnmaskedNIK.
var nikRe = regexp.MustCompile(`[0-9]{16}|[0-9]{4} [0-9]{4} [0-9]{4} [0-9]{4}|[0-9]{4}\.[0-9]{4}\.[0-9]{4}\.[0-9]{4}|[0-9]{4}-[0-9]{4}-[0-9]{4}-[0-9]{4}`)

// longDigitsRe: deret digit 16 atau lebih tanpa pemisah.
var longDigitsRe = regexp.MustCompile(`[0-9]{16,}`)

// MaskedNIK mengembalikan bentuk tersamar dari 16 digit NIK:
// 4 digit pertama + 8 bintang + 4 digit terakhir.
func MaskedNIK(digits string) string {
	if len(digits) != 16 {
		return strings.Repeat("*", len(digits))
	}
	return digits[:4] + "********" + digits[12:]
}

// MaskNIK menyamarkan setiap kemunculan pola NIK di s. Deret digit yang
// lebih panjang dari 16 disamarkan seluruhnya kecuali 4 digit pertama dan
// terakhir, sehingga tidak ada deret 16 digit utuh yang tersisa.
func MaskNIK(s string) string {
	s = longDigitsRe.ReplaceAllStringFunc(s, func(m string) string {
		if len(m) == 16 {
			return MaskedNIK(m)
		}
		return m[:4] + strings.Repeat("*", len(m)-8) + m[len(m)-4:]
	})
	return nikRe.ReplaceAllStringFunc(s, func(m string) string {
		digits := DigitsOnly(m)
		if len(digits) != 16 {
			return m
		}
		return MaskedNIK(digits)
	})
}

// ContainsUnmaskedNIK melaporkan apakah s masih memuat pola NIK utuh.
func ContainsUnmaskedNIK(s string) bool {
	return longDigitsRe.MatchString(s) || nikRe.MatchString(s)
}

// DigitsOnly membuang semua karakter selain digit ASCII.
func DigitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
