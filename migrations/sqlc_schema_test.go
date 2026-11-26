package migrations_test

import (
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/klaim-sj/ai-insurance-claims-assistant/migrations"
)

// sqlc.yaml hanya membaca migrasi aplikasi (bukan skema River). Test ini
// memastikan setiap migrasi aplikasi baru ikut didaftarkan dan migrasi
// River tidak, sehingga internal/db/models.go tetap hanya berisi tabel
// aplikasi dan query sqlc tidak kehilangan skema.
func TestSqlcSchemaListsAppMigrations(t *testing.T) {
	raw, err := os.ReadFile("../sqlc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s*-\s*"migrations/([^"]+\.sql)"\s*$`).FindAllStringSubmatch(string(raw), -1) {
		listed[m[1]] = true
	}

	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	for _, f := range files {
		isRiver := strings.Contains(f, "_river_")
		switch {
		case isRiver && listed[f]:
			t.Errorf("%s adalah migrasi River dan tidak boleh ada di schema sqlc.yaml", f)
		case !isRiver && !listed[f]:
			t.Errorf("migrasi aplikasi %s belum didaftarkan di schema sqlc.yaml", f)
		}
		delete(listed, f)
	}
	for f := range listed {
		t.Errorf("sqlc.yaml mendaftarkan %s yang tidak ada", f)
	}
}
