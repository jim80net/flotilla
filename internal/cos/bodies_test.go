package cos

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// longBody returns a body of n runes with a recognizable, position-varying tail so two
// bodies that share a prefix still differ past maxGistRunes.
func longBody(n int, tail string) string {
	var b strings.Builder
	for b.Len() < n-len(tail) {
		b.WriteString("word ")
	}
	r := []rune(b.String())
	return string(r[:n-utf8.RuneCountInString(tail)]) + tail
}

func TestWillClampMatchesLine(t *testing.T) {
	short := "a short message"
	if WillClamp(short) {
		t.Errorf("WillClamp(short)=true; a %d-rune message must not clamp", utf8.RuneCountInString(short))
	}
	long := longBody(maxGistRunes+50, "END")
	if !WillClamp(long) {
		t.Errorf("WillClamp(long)=false; a %d-rune message must clamp", utf8.RuneCountInString(long))
	}
	// And Line must actually have clamped it (the marker present) — writer/clamp agree.
	line := Line(Entry{Time: time.Unix(0, 0).UTC(), From: "operator", To: "d", Gist: long})
	if !strings.Contains(line, clampMarker) {
		t.Error("Line did not clamp a body WillClamp says clamps — writer/clamp disagree")
	}
}

func TestWriteThenLookupRoundtrip(t *testing.T) {
	dir := t.TempDir()
	ledger := dir + "/ledger"
	ts := time.Unix(1000, 0).UTC()
	full := longBody(600, "the-actual-instruction-past-the-clamp")
	e := Entry{Time: ts, Channel: "c", From: "operator", To: "flotilla-dash", Gist: full}

	if err := WriteBody(ledger, e); err != nil {
		t.Fatalf("WriteBody: %v", err)
	}
	// The dash reconstructs the key from the PARSED ledger fields: the RFC3339 ts string,
	// from, to, and the clamped gist (what ParseLedgerLine yields).
	clamped := clampGist(full)
	got, ok := LookupBody(ledger, ts.Format(time.RFC3339), "operator", "flotilla-dash", clamped)
	if !ok {
		t.Fatal("LookupBody miss for a written clamped body")
	}
	if got != strings.TrimSpace(full) {
		t.Errorf("LookupBody returned a truncated/altered body:\n got len=%d\nwant len=%d", utf8.RuneCountInString(got), utf8.RuneCountInString(strings.TrimSpace(full)))
	}
	// The clamped gist must be a genuine prefix of the recovered full body (the render
	// contract: the thread shows the full body, which starts with what the audit line showed).
	if !strings.HasPrefix(got, strings.TrimSuffix(clamped, clampMarker)) {
		t.Error("recovered body does not start with the clamped-gist prefix")
	}
}

func TestLookupMissForUnclampedGist(t *testing.T) {
	dir := t.TempDir()
	ts := time.Unix(0, 0).UTC().Format(time.RFC3339)
	// A short gist that was never clamped (no marker) must not even attempt a lookup.
	if _, ok := LookupBody(dir+"/ledger", ts, "operator", "d", "short and complete"); ok {
		t.Error("LookupBody returned ok for an unclamped gist")
	}
	// A short gist that NATURALLY ends in the marker but is NOT the clamped LENGTH must not be
	// treated as clamped (else it could hydrate another entry's body on a key collision).
	if _, ok := LookupBody(dir+"/ledger", ts, "operator", "d", "a short thought that just trails off…"); ok {
		t.Error("LookupBody treated a short natural-ellipsis gist as clamped")
	}
	// A genuinely clamped-LENGTH gist with no companion file falls back cleanly (pre-#407 line).
	realClamp := clampGist(longBody(400, "past-the-clamp"))
	if _, ok := LookupBody(dir+"/ledger", ts, "operator", "d", realClamp); ok {
		t.Error("LookupBody returned ok with no companion file present")
	}
}

// TestLookupNoCrossEntrySubstitution is the cubic #422 P1 regression: a short message that
// naturally ends in "…" must NOT be hydrated with a DIFFERENT (clamped) same-key entry's body.
func TestLookupNoCrossEntrySubstitution(t *testing.T) {
	dir := t.TempDir()
	ledger := dir + "/ledger"
	ts := time.Unix(4242, 0).UTC()
	// A long clamped message A at (ts, operator, d) writes a companion body file under the key.
	longMsg := longBody(600, "the-long-clamped-message-tail")
	if err := WriteBody(ledger, Entry{Time: ts, From: "operator", To: "d", Gist: longMsg}); err != nil {
		t.Fatalf("WriteBody: %v", err)
	}
	// A SHORT message B at the SAME (ts, operator, d) that naturally ends in "…" — never
	// clamped, so no companion of its own. Its lookup shares A's key, but must NOT return A's body.
	shortNaturalEllipsis := "quick note, more soon…"
	got, ok := LookupBody(ledger, ts.Format(time.RFC3339), "operator", "d", shortNaturalEllipsis)
	if ok {
		t.Fatalf("cross-entry substitution: short natural-ellipsis gist hydrated a different entry's body: %q", got)
	}
}

func TestLookupDisambiguatesSameKeyBodies(t *testing.T) {
	dir := t.TempDir()
	ledger := dir + "/ledger"
	ts := time.Unix(2000, 0).UTC()
	// Two messages, SAME second + SAME parties, that differ WITHIN the clamp window (as two
	// real distinct corrections do) — so their clamped gists differ and the prefix
	// disambiguates them. (Two bodies with an identical first maxGistRunes are genuinely
	// indistinguishable from the audit line alone; that pathological case is out of scope.)
	a := "AAA distinct opening for the first message. " + longBody(500, "tail-a")
	b := "BBB distinct opening for the second message. " + longBody(500, "tail-b")
	for _, g := range []string{a, b} {
		if err := WriteBody(ledger, Entry{Time: ts, From: "operator", To: "d", Gist: g}); err != nil {
			t.Fatalf("WriteBody: %v", err)
		}
	}
	tsStr := ts.Format(time.RFC3339)
	if got, ok := LookupBody(ledger, tsStr, "operator", "d", clampGist(a)); !ok || got != strings.TrimSpace(a) {
		t.Error("disambiguation failed for body A (same-key collision not resolved by prefix)")
	}
	if got, ok := LookupBody(ledger, tsStr, "operator", "d", clampGist(b)); !ok || got != strings.TrimSpace(b) {
		t.Error("disambiguation failed for body B (same-key collision not resolved by prefix)")
	}
}

func TestAppendWritesCompanionForLongMessage(t *testing.T) {
	dir := t.TempDir()
	ledger := dir + "/ledger"
	ts := time.Unix(3000, 0).UTC()
	full := longBody(700, "instruction-that-lives-past-the-280-rune-clamp")
	if err := Append(ledger, Entry{Time: ts, Channel: "c", From: "operator", To: "flotilla-dash", Gist: full}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// Append must have written both the (clamped) audit line and the full companion body.
	got, ok := LookupBody(ledger, ts.Format(time.RFC3339), "operator", "flotilla-dash", clampGist(full))
	if !ok {
		t.Fatal("Append did not persist a companion body for a clamped message")
	}
	if got != strings.TrimSpace(full) {
		t.Error("companion body written by Append is not the full message")
	}
}
