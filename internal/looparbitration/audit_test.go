package looparbitration

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAuditLogRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	log := NewAuditLog(path)
	at := time.Date(2026, 7, 9, 4, 0, 0, 0, time.UTC)
	e := AuditEntry{
		At: at, Coordinator: "xo", Target: "xo", Kind: KindRelay,
		Decision: AllowNow, Bypass: "urgent", Reason: "urgent-bypass",
	}
	if err := log.Record(e); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAudit(path)
	if err != nil || len(got) != 1 {
		t.Fatalf("LoadAudit: len=%d err=%v", len(got), err)
	}
	if got[0].Bypass != "urgent" || got[0].Kind != KindRelay {
		t.Fatalf("got %+v", got[0])
	}
}
