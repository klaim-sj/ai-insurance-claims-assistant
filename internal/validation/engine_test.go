package validation

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// Seluruh data di test ini SINTETIS.

func fptr(v float64) *float64 { return &v }

func fld(key, value, status string) Field {
	return Field{Key: key, Value: value, Status: status, Confidence: fptr(95), Page: 1, Excerpt: "Label: " + value}
}

func found(key, value string) Field { return fld(key, value, "found") }

func baseClaim() ClaimRecord {
	return ClaimRecord{PolicyNumber: "POL-DEMO-0101", ClaimantName: "Rani Kusuma Dewi", ProductType: "Kesehatan (simulasi)"}
}

func textPage() []Page { return []Page{{Number: 1, Engine: "pdftotext", WordCount: 80}} }

// baseDocs: set lengkap dan konsisten (tanpa temuan).
func baseDocs() []Document {
	return []Document{
		{ID: 1, Type: "formulir_klaim", Status: "completed", PageCount: 1, Pages: textPage(), Fields: []Field{
			found("claimant_name", "Rani Kusuma Dewi"), found("policy_number", "POL-DEMO-0101"),
			found("nik", "0000********0001"), found("event_date", "2026-10-10"),
			found("document_date", "2026-10-15"), found("amount", "2750000"),
		}},
		{ID: 2, Type: "identitas", Status: "completed", PageCount: 1,
			Pages:  []Page{{Number: 1, Engine: "tesseract", WordCount: 40, MeanConfidence: fptr(88)}},
			Fields: []Field{found("claimant_name", "Rani Kusuma Dewi"), found("nik", "0000********0001")}},
		{ID: 3, Type: "surat_keterangan_medis", Status: "completed", PageCount: 1, Pages: textPage(), Fields: []Field{
			found("claimant_name", "Rani Kusuma Dewi"), found("event_date", "2026-10-10"),
		}},
		{ID: 4, Type: "kuitansi", Status: "completed", PageCount: 1, Pages: textPage(), Fields: []Field{
			found("claimant_name", "Rani Kusuma Dewi"), found("document_date", "2026-10-12"), found("amount", "2750000"),
		}},
	}
}

func setField(docs []Document, id int64, f Field) []Document {
	for i := range docs {
		if docs[i].ID != id {
			continue
		}
		for j := range docs[i].Fields {
			if docs[i].Fields[j].Key == f.Key {
				docs[i].Fields[j] = f
				return docs
			}
		}
		docs[i].Fields = append(docs[i].Fields, f)
	}
	return docs
}

func without(docs []Document, id int64) []Document {
	return slices.DeleteFunc(docs, func(d Document) bool { return d.ID == id })
}

func run(t *testing.T, in Input) []Finding {
	t.Helper()
	if in.Today == "" {
		in.Today = "2026-10-15"
	}
	fs, err := Evaluate(in, DefaultRules())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return fs
}

func codes(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Code+":"+string(f.Severity))
	}
	return out
}

func TestRules(t *testing.T) {
	cases := []struct {
		name string
		mod  func(in *Input)
		want []string // kode:severity, berurutan
	}{
		{"set lengkap dan konsisten", func(*Input) {}, nil},

		// MISSING_DOCUMENT
		{"kuitansi belum ada", func(in *Input) { in.Documents = without(in.Documents, 4) },
			[]string{"MISSING_DOCUMENT:high"}},
		{"semua dokumen belum ada", func(in *Input) { in.Documents = nil },
			[]string{"MISSING_DOCUMENT:high", "MISSING_DOCUMENT:high", "MISSING_DOCUMENT:high", "MISSING_DOCUMENT:high"}},
		{"dokumen gagal diproses tetap dianggap sudah diunggah", func(in *Input) {
			in.Documents[3] = Document{ID: 4, Type: "kuitansi", Status: "failed"}
		}, nil},
		{"produk non-kesehatan tidak memakai checklist kesehatan", func(in *Input) {
			in.Claim.ProductType = "Lainnya (simulasi)"
			in.Documents = without(in.Documents, 4)
		}, nil},

		// MISSING_FIELD
		{"nomor polis formulir tidak ditemukan", func(in *Input) {
			in.Documents = setField(in.Documents, 1, Field{Key: "policy_number", Status: "not_found"})
		}, []string{"MISSING_FIELD:medium"}},
		{"NIK identitas uncertain → verifikasi manual", func(in *Input) {
			in.Documents = setField(in.Documents, 2, Field{Key: "nik", Status: "uncertain", Confidence: fptr(41), Page: 1})
		}, []string{"NEEDS_MANUAL_VERIFICATION:low"}},
		{"baris field tidak ada sama sekali", func(in *Input) {
			in.Documents[3].Fields = in.Documents[3].Fields[:2] // amount hilang
		}, []string{"MISSING_FIELD:medium"}},
		{"field tidak wajib boleh kosong", func(in *Input) {
			in.Documents = setField(in.Documents, 1, Field{Key: "amount", Status: "not_found"})
		}, nil},

		// NAME_MISMATCH
		{"nama formulir berbeda dengan identitas", func(in *Input) {
			in.Documents = setField(in.Documents, 1, found("claimant_name", "Rani Kusumawati"))
		}, []string{"NAME_MISMATCH:medium"}},
		{"huruf besar-kecil, spasi, dan gelar dinormalisasi", func(in *Input) {
			in.Documents = setField(in.Documents, 1, found("claimant_name", "  RANI   kusuma DEWI, S.Kom "))
			in.Documents = setField(in.Documents, 3, found("claimant_name", "Ny. Rani Kusuma Dewi"))
			in.Documents = setField(in.Documents, 4, found("claimant_name", "Hj. Rani Kusuma-Dewi"))
		}, nil},
		{"inisial: kemungkinan singkatan", func(in *Input) {
			in.Documents = setField(in.Documents, 4, found("claimant_name", "R. Kusuma Dewi"))
		}, []string{"NAME_MISMATCH:low"}},
		{"nama tengah tidak ditulis: kemungkinan singkatan", func(in *Input) {
			in.Documents = setField(in.Documents, 3, found("claimant_name", "Rani Dewi"))
		}, []string{"NAME_MISMATCH:low"}},
		{"data klaim berbeda dengan identitas", func(in *Input) { in.Claim.ClaimantName = "Sari Lestari" },
			[]string{"NAME_MISMATCH:medium"}},
		{"identitas uncertain: dibandingkan dengan data klaim, tidak dianggap cocok/berbeda", func(in *Input) {
			in.Documents = setField(in.Documents, 2, Field{Key: "claimant_name", Status: "uncertain", Page: 1})
			in.Documents = setField(in.Documents, 1, found("claimant_name", "Rani Kusumawati"))
		}, []string{"NEEDS_MANUAL_VERIFICATION:low", "NAME_MISMATCH:medium"}},

		// POLICY_MISMATCH
		{"nomor polis berbeda", func(in *Input) {
			in.Documents = setField(in.Documents, 1, found("policy_number", "POL-DEMO-0110"))
		}, []string{"POLICY_MISMATCH:medium"}},
		{"nomor polis beda spasi/huruf kecil dianggap sama", func(in *Input) {
			in.Documents = setField(in.Documents, 1, found("policy_number", "pol-demo- 0101"))
		}, nil},

		// DATE_INCONSISTENCY
		{"kuitansi sebelum tanggal kejadian", func(in *Input) {
			in.Documents = setField(in.Documents, 4, found("document_date", "2026-10-05"))
		}, []string{"DATE_INCONSISTENCY:medium"}},
		{"kuitansi pada hari kejadian tidak bermasalah", func(in *Input) {
			in.Documents = setField(in.Documents, 4, found("document_date", "2026-10-10"))
		}, nil},
		{"tanggal kejadian di masa depan (kuitansi otomatis lebih awal)", func(in *Input) {
			in.Documents = setField(in.Documents, 3, found("event_date", "2026-12-01"))
		}, []string{"DATE_INCONSISTENCY:medium", "DATE_INCONSISTENCY:medium"}},
		{"kuitansi di masa depan", func(in *Input) {
			in.Documents = setField(in.Documents, 4, found("document_date", "2027-01-02"))
		}, []string{"DATE_INCONSISTENCY:medium"}},
		{"tanggal uncertain tidak dibandingkan", func(in *Input) {
			in.Documents = setField(in.Documents, 1, Field{Key: "document_date", Value: "2026-10-01", Status: "uncertain", Page: 1})
		}, nil},

		// LOW_QUALITY_DOCUMENT
		{"identitas buram", func(in *Input) {
			in.Documents[1].Pages = []Page{{Number: 1, Engine: "tesseract", WordCount: 3, MeanConfidence: fptr(27.1)}}
		}, []string{"LOW_QUALITY_DOCUMENT:high"}},
		{"satu dari dua halaman buram", func(in *Input) {
			in.Documents[1].Pages = []Page{
				{Number: 1, Engine: "tesseract", WordCount: 40, MeanConfidence: fptr(90)},
				{Number: 2, Engine: "tesseract", WordCount: 40, MeanConfidence: fptr(50)},
			}
		}, []string{"LOW_QUALITY_DOCUMENT:medium"}},
		{"lapisan teks PDF tidak dinilai confidence", func(in *Input) {
			in.Documents[0].Pages = []Page{{Number: 1, Engine: "pdftotext", WordCount: 80}}
		}, nil},

		// Dokumen pengganti
		{"unggahan ulang: hanya dokumen terbaru per jenis yang dinilai", func(in *Input) {
			old := in.Documents[0]
			old.ID = 9
			old.Fields = slices.Clone(old.Fields)
			in.Documents = append([]Document{old}, in.Documents...)
			in.Documents = setField(in.Documents, 9, found("claimant_name", "Nama Lama Salah"))
		}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := Input{Claim: baseClaim(), Documents: baseDocs()}
			tc.mod(&in)
			got := codes(run(t, in))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("temuan = %v, harap %v", got, tc.want)
			}
		})
	}
}

func TestFindingsHaveEvidenceAndNoPII(t *testing.T) {
	in := Input{Claim: baseClaim(), Documents: baseDocs(), Today: "2026-10-15"}
	in.Documents = setField(in.Documents, 1, found("claimant_name", "Rani Kusumawati"))
	in.Documents = setField(in.Documents, 1, found("policy_number", "POL-DEMO-0110"))
	in.Documents = setField(in.Documents, 4, found("document_date", "2026-10-05"))
	in.Documents = setField(in.Documents, 2, Field{Key: "nik", Status: "uncertain", Page: 1})
	in.Documents = setField(in.Documents, 4, Field{Key: "amount", Status: "not_found"})
	in.Documents = without(in.Documents, 3)
	fs := run(t, in)
	seen := map[string]bool{}
	for _, f := range fs {
		seen[f.Code] = true
		if len(f.Evidence) == 0 {
			t.Errorf("%s tanpa evidence", f.Code)
		}
		if f.RuleID == 0 || f.RuleVersion == 0 || f.Fingerprint == "" {
			t.Errorf("%s tanpa rule/fingerprint: %+v", f.Code, f)
		}
		for _, w := range []string{"ditolak", "tidak layak", "fraud", "palsu"} {
			if strings.Contains(strings.ToLower(f.Message), w) {
				t.Errorf("pesan %q memakai istilah terlarang %q", f.Message, w)
			}
		}
		if !strings.Contains(f.Message, "perlu ditinjau") {
			t.Errorf("pesan harus netral \"perlu ditinjau\": %q", f.Message)
		}
		if strings.Contains(f.Message, "Kusuma") || strings.Contains(f.Message, "POL-") {
			t.Errorf("pesan tidak boleh memuat nilai field: %q", f.Message)
		}
	}
	for _, c := range []string{CodeNameMismatch, CodePolicyMismatch, CodeDateInconsistency, CodeMissingDocument, CodeMissingField, CodeNeedsManualVerification} {
		if !seen[c] {
			t.Errorf("temuan %s tidak muncul", c)
		}
	}
	// Temuan perbandingan memuat bukti dari tiap sumber.
	for _, f := range fs {
		if f.Code == CodeNameMismatch || f.Code == CodePolicyMismatch || f.Code == CodeDateInconsistency {
			if len(f.Evidence) < 2 {
				t.Errorf("%s harus punya bukti dari kedua sumber: %+v", f.Code, f.Evidence)
			}
		}
	}
}

func TestEvaluateDeterministicAndUniqueFingerprints(t *testing.T) {
	in := Input{Claim: baseClaim(), Documents: baseDocs(), Today: "2026-10-15"}
	in.Documents = setField(in.Documents, 1, found("claimant_name", "Rani Kusumawati"))
	in.Documents = setField(in.Documents, 4, found("document_date", "2027-01-05"))
	a, b := run(t, in), run(t, in)
	if len(a) != len(b) {
		t.Fatal("hasil tidak deterministik")
	}
	fps := map[string]bool{}
	for i := range a {
		if a[i].Fingerprint != b[i].Fingerprint || a[i].Message != b[i].Message {
			t.Fatal("hasil tidak deterministik")
		}
		if fps[a[i].Fingerprint] {
			t.Fatalf("fingerprint ganda %s", a[i].Fingerprint)
		}
		fps[a[i].Fingerprint] = true
	}
}

func TestFindingValidate(t *testing.T) {
	ok := Finding{
		Code: CodePolicyMismatch, RuleID: 1, RuleVersion: 1, Severity: SeverityMedium,
		Message: "Nomor polis berbeda; perlu ditinjau.", Fingerprint: "x",
		Evidence: []Evidence{{Source: SourceDocument, DocumentID: 1, Page: 1, Field: "policy_number", Value: "POL-DEMO-0110"}},
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("temuan valid ditolak: %v", err)
	}
	mut := func(f func(*Finding)) Finding {
		c := ok
		c.Evidence = slices.Clone(ok.Evidence)
		f(&c)
		return c
	}
	cases := []struct {
		name string
		f    Finding
		want error
	}{
		{"tanpa evidence", mut(func(f *Finding) { f.Evidence = nil }), ErrNoEvidence},
		{"evidence dokumen tanpa halaman", mut(func(f *Finding) { f.Evidence[0].Page = 0 }), ErrInvalidEvidence},
		{"evidence dokumen tanpa document_id", mut(func(f *Finding) { f.Evidence[0].DocumentID = 0 }), ErrInvalidEvidence},
		{"evidence dokumen tanpa field/sinyal", mut(func(f *Finding) { f.Evidence[0].Field = "" }), ErrInvalidEvidence},
		{"sumber tak dikenal", mut(func(f *Finding) { f.Evidence[0].Source = "llm" }), ErrInvalidEvidence},
		{"NIK utuh di nilai", mut(func(f *Finding) {
			f.Evidence[0].Field, f.Evidence[0].Value = "nik", "0000010101900001"
		}), ErrInvalidEvidence},
		{"NIK utuh di kutipan", mut(func(f *Finding) { f.Evidence[0].Excerpt = "NIK: 0000 0101 0190 0001" }), ErrInvalidEvidence},
		{"kutipan terlalu panjang", mut(func(f *Finding) { f.Evidence[0].Excerpt = strings.Repeat("a", MaxExcerpt+1) }), ErrInvalidEvidence},
		{"severity tak dikenal", mut(func(f *Finding) { f.Severity = "critical" }), ErrInvalidFinding},
		{"tanpa versi aturan", mut(func(f *Finding) { f.RuleVersion = 0 }), ErrInvalidFinding},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.f.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, harap %v", err, tc.want)
			}
		})
	}
}

func TestCompareNames(t *testing.T) {
	ts := newTitleSet(defaultConfigs[CodeNameMismatch].(NameMismatchConfig).Titles)
	cases := []struct {
		a, b string
		want NameResult
	}{
		{"Rani Kusuma Dewi", "rani kusuma dewi", NameEqual},
		{"Rani  Kusuma Dewi ", "RANI KUSUMA DEWI", NameEqual},
		{"dr. Rani Kusuma Dewi, Sp.PD", "Rani Kusuma Dewi", NameEqual},
		{"Rani Kusuma Dewi S.Kom", "Rani Kusuma Dewi", NameEqual},
		{"Rani KusumaDewi", "Rani Kusuma Dewi", NameEqual},
		{"Rāni Kusuma Dewi", "Rani Kusuma Dewi", NameEqual},
		{"R. Kusuma Dewi", "Rani Kusuma Dewi", NameAbbreviation},
		{"Rani K. Dewi", "Rani Kusuma Dewi", NameAbbreviation},
		{"Rani Dewi", "Rani Kusuma Dewi", NameAbbreviation},
		{"Rani Kusumawati", "Rani Kusuma Dewi", NameDifferent},
		{"Sari Kusuma Dewi", "Rani Kusuma Dewi", NameDifferent},
		{"R. K. D.", "Rani Kusuma Dewi", NameDifferent}, // hanya inisial: tidak cukup
		{"Budi Santoso", "Rani Kusuma Dewi", NameDifferent},
	}
	for _, tc := range cases {
		got := compareNames(ts.normalize(tc.a), ts.normalize(tc.b))
		if got != tc.want {
			t.Errorf("compare(%q, %q) = %s, harap %s (%v vs %v)", tc.a, tc.b, got, tc.want, ts.normalize(tc.a), ts.normalize(tc.b))
		}
	}
}

// Gating confidence (Tahap 5.5): perbedaan/kekosongan hanya dilaporkan
// sebagai MISSING_FIELD / NAME_MISMATCH / POLICY_MISMATCH bila hasil baca
// yakin; selain itu NEEDS_MANUAL_VERIFICATION dengan bukti lengkap.
func TestConfidenceGating(t *testing.T) {
	lowPage := []Page{{Number: 1, Engine: "tesseract", WordCount: 4, MeanConfidence: fptr(35)}}
	okPage := []Page{{Number: 1, Engine: "tesseract", WordCount: 60, MeanConfidence: fptr(90)}}
	withConf := func(f Field, c float64) Field { f.Confidence = fptr(c); return f }
	cases := []struct {
		name     string
		mod      func(in *Input)
		want     []string
		evidence int // jumlah evidence minimal pada temuan pertama (0 = abaikan)
	}{
		{"not_found di dokumen berkualitas baik → MISSING_FIELD", func(in *Input) {
			in.Documents[1].Pages = okPage
			in.Documents = setField(in.Documents, 2, Field{Key: "nik", Status: "not_found"})
		}, []string{"MISSING_FIELD:medium"}, 1},
		{"not_found di dokumen berkualitas rendah → NMV (bukan MISSING_FIELD)", func(in *Input) {
			in.Documents[1].Pages = lowPage
			in.Documents = setField(in.Documents, 2, Field{Key: "nik", Status: "not_found"})
		}, []string{"NEEDS_MANUAL_VERIFICATION:low", "LOW_QUALITY_DOCUMENT:high"}, 1},
		{"uncertain di dokumen berkualitas baik → NMV", func(in *Input) {
			in.Documents = setField(in.Documents, 4, Field{Key: "amount", Value: "2750000", Status: "uncertain", Confidence: fptr(55), Page: 1})
		}, []string{"NEEDS_MANUAL_VERIFICATION:low"}, 1},
		{"nama berbeda, kedua nilai yakin → NAME_MISMATCH", func(in *Input) {
			in.Documents = setField(in.Documents, 1, withConf(found("claimant_name", "Rani Kusumawati"), 96))
		}, []string{"NAME_MISMATCH:medium"}, 2},
		{"nama berbeda, nilai dokumen confidence rendah (found 70) → NMV dengan kedua bukti", func(in *Input) {
			in.Documents = setField(in.Documents, 1, withConf(found("claimant_name", "Rani Kusumawati"), 70))
		}, []string{"NEEDS_MANUAL_VERIFICATION:medium"}, 2},
		{"nama berbeda, nilai dokumen uncertain berisi → satu NMV perbandingan (tanpa NMV field ganda)", func(in *Input) {
			in.Documents = setField(in.Documents, 1, Field{Key: "claimant_name", Value: "Rani Kusumawati", Status: "uncertain", Confidence: fptr(50), Page: 1})
		}, []string{"NEEDS_MANUAL_VERIFICATION:medium"}, 2},
		{"identitas confidence rendah tidak jadi acuan; dokumen lain dibandingkan dengan data klaim", func(in *Input) {
			in.Documents = setField(in.Documents, 2, withConf(found("claimant_name", "Rani Kusuma Dewl"), 72))
		}, []string{"NEEDS_MANUAL_VERIFICATION:medium"}, 2},
		{"kemungkinan singkatan dengan confidence rendah → NMV low", func(in *Input) {
			in.Documents = setField(in.Documents, 4, withConf(found("claimant_name", "R. Kusuma Dewi"), 75))
		}, []string{"NEEDS_MANUAL_VERIFICATION:low"}, 2},
		{"nilai sama walau confidence rendah → tidak ada temuan", func(in *Input) {
			in.Documents = setField(in.Documents, 1, withConf(found("claimant_name", "Rani Kusuma Dewi"), 62))
			in.Documents = setField(in.Documents, 1, withConf(found("policy_number", "POL-DEMO-0101"), 62))
		}, nil, 0},
		{"polis berbeda, yakin → POLICY_MISMATCH", func(in *Input) {
			in.Documents = setField(in.Documents, 1, withConf(found("policy_number", "POL-DEMO-0110"), 99))
		}, []string{"POLICY_MISMATCH:medium"}, 2},
		{"polis berbeda, confidence rendah → NMV dengan bukti dokumen + data klaim", func(in *Input) {
			in.Documents = setField(in.Documents, 1, withConf(found("policy_number", "POL-DEMO-0110"), 66))
		}, []string{"NEEDS_MANUAL_VERIFICATION:medium"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := Input{Claim: baseClaim(), Documents: baseDocs()}
			tc.mod(&in)
			fs := run(t, in)
			if got := codes(fs); !slices.Equal(got, tc.want) {
				t.Fatalf("temuan = %v, harap %v", got, tc.want)
			}
			if tc.evidence > 0 {
				for _, f := range fs {
					if f.Code == CodeLowQualityDocument {
						continue
					}
					if len(f.Evidence) < tc.evidence {
						t.Fatalf("%s: evidence %d, harap ≥ %d", f.Code, len(f.Evidence), tc.evidence)
					}
					if f.Code == CodeNeedsManualVerification {
						if f.RuleVersion != 1 || !strings.Contains(f.Message, "perlu ditinjau") {
							t.Fatalf("NMV tidak sesuai: %+v", f)
						}
					}
				}
			}
		})
	}
}

// Tanpa aturan NMV aktif, kondisi berconfidence rendah tidak dilaporkan
// sebagai mismatch (tidak pernah "naik kelas" menjadi temuan pasti).
func TestGatingWithoutNMVRule(t *testing.T) {
	var rules []Rule
	for _, r := range DefaultRules() {
		if r.Code != CodeNeedsManualVerification {
			rules = append(rules, r)
		}
	}
	in := Input{Claim: baseClaim(), Documents: baseDocs(), Today: "2026-10-15"}
	c := 60.0
	f := found("claimant_name", "Rani Kusumawati")
	f.Confidence = &c
	in.Documents = setField(in.Documents, 1, f)
	fs, err := Evaluate(in, rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("harap tanpa temuan, dapat %v", codes(fs))
	}
}
