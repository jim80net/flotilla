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
		{Parsed: true, Nonce: "aa11", Gist: "clamped one…"},
		{Parsed: true, Nonce: "", Gist: "short complete"}, // no nonce → not hydrated
		{Parsed: false, Nonce: "bb22", Raw: "- malformed line"},
	}
	HydrateLedgerBodies(entries, func(nonce string) (string, bool) {
		if nonce == "aa11" {
			return "the FULL body for entry one, well past the clamp boundary", true
		}
		return "", false
	})
	if entries[0].Body != "the FULL body for entry one, well past the clamp boundary" {
		t.Errorf("entry 0 Body not hydrated: %q", entries[0].Body)
	}
	if entries[1].Body != "" {
		t.Errorf("entry 1 (no nonce) must keep empty Body, got %q", entries[1].Body)
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
	HydrateLedgerBodies(doc.Ledger, func(nonce string) (string, bool) {
		return cos.LookupBody(ledger, nonce)
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
	if e.Nonce == "" {
		t.Error("a clamped entry must carry a companion nonce parsed from the line")
	}
	// ...but the hydrated Body is the operator's COMPLETE message.
	if e.Body != full {
		t.Errorf("thread Body is not the full message:\n got %d runes\nwant %d runes", utf8.RuneCountInString(e.Body), utf8.RuneCountInString(full))
	}
}

// TestSameSecondSamePrefixResolveOwnBodies is the cubic #422 CLASS regression: two messages
// in the same second, same parties, sharing an IDENTICAL clamped prefix (so identical audit
// gists) must each render their OWN full body — the nonce identity disambiguates where a
// content/prefix scan cannot.
func TestSameSecondSamePrefixResolveOwnBodies(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(dir, "ledger")
	ts := time.Unix(5000, 0).UTC()
	shared := strings.Repeat("shared prefix word ", 40) // > 280 runes of identical text
	a := shared + " AAA-distinct-tail-for-message-one"
	b := shared + " BBB-distinct-tail-for-message-two"
	for _, g := range []string{a, b} {
		if err := cos.Append(ledger, cos.Entry{Time: ts, Channel: "c", From: "operator", To: "flotilla-dash", Gist: g}); err != nil {
			t.Fatalf("cos.Append: %v", err)
		}
	}
	raw, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	doc := BuildHistory(string(raw), "")
	HydrateLedgerBodies(doc.Ledger, func(nonce string) (string, bool) {
		return cos.LookupBody(ledger, nonce)
	})
	if len(doc.Ledger) != 2 {
		t.Fatalf("want 2 entries, got %d", len(doc.Ledger))
	}
	// Both audit gists are identical (same clamped prefix) — the precondition for the class.
	if doc.Ledger[0].Gist != doc.Ledger[1].Gist {
		t.Fatal("test precondition failed: the two clamped gists must be identical")
	}
	got := map[string]bool{}
	for _, e := range doc.Ledger {
		if e.Body == "" {
			t.Fatalf("entry not hydrated (nonce=%q)", e.Nonce)
		}
		got[e.Body] = true
	}
	if !got[strings.TrimSpace(a)] || !got[strings.TrimSpace(b)] || len(got) != 2 {
		t.Errorf("each message must resolve to its OWN body; got %d distinct bodies (cross-entry substitution)", len(got))
	}
}
