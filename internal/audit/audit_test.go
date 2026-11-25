package audit_test

import (
	"context"
	"testing"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/audit"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/db"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/testutil"
)

func TestRecordAndAppendOnly(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	q := db.New(pool)
	l := audit.NewLogger(q)

	if err := l.Record(ctx, audit.Entry{
		EntityType: audit.EntityRoute,
		Action:     audit.ActionAccessDenied,
		After:      map[string]string{"path": "/admin/users"},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	rows, err := q.ListRecentAuditLogs(ctx, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("harus ada 1 baris audit, dapat %d (err=%v)", len(rows), err)
	}
	r := rows[0]
	if r.UserID != nil || r.EntityID != nil || r.BeforeJson != nil || string(r.AfterJson) != `{"path": "/admin/users"}` {
		t.Fatalf("baris audit tidak sesuai: %+v after=%s", r, r.AfterJson)
	}

	if _, err := pool.Exec(ctx, "UPDATE audit_logs SET action = 'x'"); err == nil {
		t.Fatal("UPDATE audit_logs harus ditolak")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM audit_logs"); err == nil {
		t.Fatal("DELETE audit_logs harus ditolak")
	}
}
