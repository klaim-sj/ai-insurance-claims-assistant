package documents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSampleDataValid memastikan dokumen sintetis di sample-data/ dapat
// diunggah (lolos validator) dan expected.json konsisten dengan file yang ada.
func TestSampleDataValid(t *testing.T) {
	checkSampleDir(t, filepath.Join("..", "..", "sample-data"), 6)
}

// TestHoldoutDataValid: set holdout juga harus lolos validator unggah dan
// mematuhi aturan data sintetis.
func TestHoldoutDataValid(t *testing.T) {
	checkSampleDir(t, filepath.Join("..", "..", "sample-data", "holdout"), 7)
}

// TestHoldout2DataValid: set holdout2 (Tahap 5.5).
func TestHoldout2DataValid(t *testing.T) {
	checkSampleDir(t, filepath.Join("..", "..", "sample-data", "holdout2"), 7)
}

func checkSampleDir(t *testing.T, dir string, scenarios int) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var exp struct {
		Files map[string]struct {
			DocumentType string         `json:"document_type"`
			MIME         string         `json:"mime_type"`
			Pages        int            `json:"pages"`
			Fields       map[string]any `json:"fields"`
		} `json:"files"`
		Scenarios []struct {
			ID     string   `json:"id"`
			Upload []string `json:"upload"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(raw, &exp); err != nil {
		t.Fatal(err)
	}
	if len(exp.Scenarios) != scenarios {
		t.Fatalf("harus ada %d skenario, dapat %d", scenarios, len(exp.Scenarios))
	}
	for name, f := range exp.Files {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		info, err := Validate(name, data)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if info.MIME != f.MIME || info.Pages != f.Pages || !Type(f.DocumentType).Valid() {
			t.Errorf("%s: info %+v tidak sesuai expected", name, info)
		}
		// Aturan data sintetis.
		if nik, ok := f.Fields["nik"].(string); ok && !strings.HasPrefix(nik, "0000") {
			t.Errorf("%s: NIK harus diawali 0000", name)
		}
		if ph, ok := f.Fields["phone"].(string); ok && !strings.HasPrefix(ph, "0800-0000-") {
			t.Errorf("%s: telepon harus 0800-0000-xxxx", name)
		}
		if em, ok := f.Fields["email"].(string); ok && !strings.HasSuffix(em, "@example.com") {
			t.Errorf("%s: email harus @example.com", name)
		}
	}
	for _, s := range exp.Scenarios {
		for _, f := range s.Upload {
			if _, ok := exp.Files[f]; !ok {
				t.Errorf("skenario %s merujuk file tak dikenal %s", s.ID, f)
			}
		}
	}
}
