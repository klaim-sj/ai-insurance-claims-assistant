package extraction

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

// Labels adalah konfigurasi sinonim label per field.
type Labels struct {
	Version string           `json:"version"`
	Fields  map[Key][]string `json:"fields"`
}

//go:embed labels.json
var defaultLabelsJSON []byte

var active atomic.Pointer[Labels]

func init() {
	l, err := ParseLabels(defaultLabelsJSON)
	if err != nil {
		panic("labels.json bawaan tidak valid: " + err.Error())
	}
	active.Store(l)
}

// DefaultLabels mengembalikan konfigurasi label bawaan (labels.json).
func DefaultLabels() *Labels {
	l, _ := ParseLabels(defaultLabelsJSON)
	return l
}

// ParseLabels mengurai dan memvalidasi konfigurasi label: setiap field
// harus dikenal dan punya minimal satu label; label dinormalisasi dan
// tidak boleh kosong atau ganda di seluruh field (agar satu baris tidak
// dapat diklaim dua field).
func ParseLabels(raw []byte) (*Labels, error) {
	var l Labels
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("konfigurasi label: %w", err)
	}
	if strings.TrimSpace(l.Version) == "" {
		return nil, errors.New("konfigurasi label: version wajib")
	}
	known := map[Key]bool{}
	for _, k := range AllKeys {
		known[k] = true
	}
	owner := map[string]Key{}
	for k, ls := range l.Fields {
		if !known[k] {
			return nil, fmt.Errorf("konfigurasi label: field tak dikenal %q", k)
		}
		if len(ls) == 0 {
			return nil, fmt.Errorf("konfigurasi label: field %q tanpa label", k)
		}
		for i, s := range ls {
			n := normLabel(s)
			if n == "" {
				return nil, fmt.Errorf("konfigurasi label: label kosong pada %q", k)
			}
			if o, dup := owner[n]; dup {
				return nil, fmt.Errorf("konfigurasi label: label %q ganda (%s, %s)", n, o, k)
			}
			owner[n] = k
			ls[i] = n
		}
	}
	for _, k := range AllKeys {
		if len(l.Fields[k]) == 0 {
			return nil, fmt.Errorf("konfigurasi label: field %q belum punya label", k)
		}
	}
	return &l, nil
}

// LoadLabelsFile membaca konfigurasi label dari file.
func LoadLabelsFile(path string) (*Labels, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("membaca konfigurasi label: %w", err)
	}
	return ParseLabels(raw)
}

// SetLabels mengganti konfigurasi label yang dipakai Extract.
func SetLabels(l *Labels) {
	if l != nil {
		active.Store(l)
	}
}

// ActiveLabels mengembalikan konfigurasi label yang sedang dipakai.
func ActiveLabels() *Labels { return active.Load() }
