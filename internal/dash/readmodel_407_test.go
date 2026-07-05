package dash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jim80net/flotilla/internal/cos"
)

// TestHydrateLedgerBodies_Pure exercises the hydration logic behind an in-memory resolver:
// parsed entries get their Body filled from a hit; a miss and an unparsed line are left
// alone; a nil resolver is a no-op.
func TestHydrateLedgerBodies_Pure(t *testing.T) {
	entries := []LedgerEntry{
		{Parsed: true, Time: "T1", From: "operator", To: "d", Gist: "clamped one…"},
		{Parsed: true, Time: "T2", From: "operator", To: "d", Gist: "short complete"},
		{Parsed: false, Raw: "- malformed line"},
	}
	HydrateLedgerBodies(entries, func(t, from, to, gist string) (string, bool) {
		if t == "T1" {
			return "the FULL body for entry one, well past the clamp boundary", true
		}
		return "", false
	})
	if entries[0].Body != "the FULL body for entry one, well past the clamp boundary" {
		t.Errorf("entry 0 Body not hydrated: %q", entries[0].Body)
	}
	if entries[1].Body != "" {
		t.Errorf("entry 1 (resolver miss) must keep empty Body, got %q", entries[1].Body)
	}
	if entries[2].Body != "" {
		t.Errorf("entry 2 (unparsed) must not be hydrated, got %q", entries[2].Body)
	}

	// nil resolver: no panic, no change.
	entries[0].Body = ""
	HydrateLedgerBodies(entries, nil)
	if entries[0].Body != "" {
		t.Error("nil resolver must be a no-op")
	}
}

// TestLongMessageFullFidelityEndToEnd is the #407 regression: a 3,000-char operator message
// written through the REAL cos.Append path (audit line clamps the gist) must be rendered in
// full by the dash thread via the companion store — never as the clamped copy.
func TestLongMessageFullFidelityEndToEnd(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(dir, "ledger")

	var b strings.Builder
	b.WriteString("flotilla-dash: operator correction — ")
	for b.Len() < 3000 {
		b.WriteString("this instruction must survive the audit clamp intact. ")
	}
	full := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(full) < 3000 {
		t.Fatalf("test body only %d runes; want ≥3000", utf8.RuneCountInString(full))
	}

	if err := cos.Append(ledger, cos.Entry{
		Time: time.Unix(1000, 0).UTC(), Channel: "c", From: "operator", To: "flotilla-dash", Gist: full,
	}); err != nil {
		t.Fatalf("cos.Append: %v", err)
	}

	// Read the ledger file the way the dash server does, build history, hydrate via the REAL
	// companion lookup — the exact loadHistory path.
	raw, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	doc := BuildHistory(string(raw), "")
	HydrateLedgerBodies(doc.Ledger, func(tm, from, to, gist string) (string, bool) {
		return cos.LookupBody(ledger, tm, from, to, gist)
	})

	if len(doc.Ledger) != 1 {
		t.Fatalf("want 1 ledger entry, got %d", len(doc.Ledger))
	}
	e := doc.Ledger[0]
	// The audit gist is clamped (proof the bug's precondition holds)...
	if utf8.RuneCountInString(e.Gist) >= utf8.RuneCountInString(full) {
		t.Fatalf("gist not clamped (%d runes); the companion store would be untested", utf8.RuneCountInString(e.Gist))
	}
	if !strings.HasSuffix(e.Gist, "…") {
		t.Error("clamped gist must end with the clamp marker")
	}
	// ...but the hydrated Body is the operator's COMPLETE message.
	if e.Body != full {
		t.Errorf("thread Body is not the full message:\n got %d runes\nwant %d runes", utf8.RuneCountInString(e.Body), utf8.RuneCountInString(full))
	}
	// And the render contract holds: the body starts with what the audit line showed.
	if !strings.HasPrefix(e.Body, strings.TrimSuffix(e.Gist, "…")) {
		t.Error("hydrated Body must start with the clamped-gist prefix")
	}
}
