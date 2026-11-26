package pii

import "testing"

func TestMaskNIK(t *testing.T) {
	cases := map[string]string{
		"NIK : 0000010101900001":                "NIK : 0000********0001",
		"NIK: 0000 0101 0190 0002":              "NIK: 0000********0002",
		"0000-0101-0190-0003 ok":                "0000********0003 ok",
		"0000.0101.0190.0004":                   "0000********0004",
		"x00000101019000051y":                   "x0000*********0051y",
		"Telepon 0800-0000-1234":                "Telepon 0800-0000-1234",
		"10-10-2026 12-10-2026":                 "10-10-2026 12-10-2026",
		"Rp 2.750.000":                          "Rp 2.750.000",
		"sudah 0000********0001 termask":        "sudah 0000********0001 termask",
		"dua 0000010101900001/0000010101900009": "dua 0000********0001/0000********0009",
	}
	for in, want := range cases {
		got := MaskNIK(in)
		if got != want {
			t.Errorf("MaskNIK(%q) = %q, want %q", in, got, want)
		}
		if ContainsUnmaskedNIK(got) {
			t.Errorf("hasil masih memuat NIK utuh: %q", got)
		}
	}
}

func TestContainsUnmaskedNIK(t *testing.T) {
	if !ContainsUnmaskedNIK("0000010101900001") {
		t.Fatal("NIK utuh harus terdeteksi")
	}
	if ContainsUnmaskedNIK("0000********0001") {
		t.Fatal("NIK termasking tidak boleh terdeteksi")
	}
}
