package cos

import (
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// longBody returns a body of n runes with a recognizable tail so a clamped copy is a strict
// prefix of the full text and two bodies can differ past the clamp boundary.
func longBody(n int, tail string) string {
	var b strings.Builder
	for b.Len() < n-len(tail) {
		b.WriteString("word ")
	}
	r := []rune(b.String())
	return string(r[:n-utf8.RuneCountInString(tail)]) + tail
}

// nonceFromLine extracts the trailing ` #<nonce>` a rendered clamped line carries (mirrors
// the dash's ParseLedgerLine so the test asserts the real round-trip).
func nonceFromLine(line string) string {
	line = strings.TrimRight(line, "\n")
	if q := strings.LastIndex(line, `"`); q >= 0 && q+1 < len(line) {
		if tail := strings.TrimSpace(line[q+1:]); strings.HasPrefix(tail, "#") {
			return tail[1:]
		}
	}
	return ""
}

func TestWillClampMatchesLine(t *testing.T) {
	if WillClamp("a short message") {
		t.Error("WillClamp(short)=true; a short message must not clamp")
	}
	long := longBody(maxGistRunes+50, "END")
	if !WillClamp(long) {
		t.Error("WillClamp(long)=false; a long message must clamp")
	}
	if line := Line(Entry{Time: time.Unix(0, 0).UTC(), From: "operator", To: "d", Gist: long}); !strings.Contains(line, clampMarker) {
		t.Error("Line did not clamp a body WillClamp says clamps")
	}
}

func TestNonceUniqueValidAndSafe(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		n, err := newNonce()
		if err != nil {
			t.Fatal(err)
		}
		if !IsNonce(n) {
			t.Fatalf("newNonce produced a non-nonce: %q", n)
		}
		if seen[n] {
			t.Fatalf("nonce collision at %d", i)
		}
		seen[n] = true
	}
	for _, bad := range []string{"", "ABC", "g0f1", "../evil", "de/ad", "dead beef", "dead.txt"} {
		if IsNonce(bad) {
			t.Errorf("IsNonce accepted a malformed/traversal token: %q", bad)
		}
	}
}

func TestWriteThenLookupByNonce(t *testing.T) {
	dir := t.TempDir()
	ledger := dir + "/ledger"
	full := longBody(600, "the-instruction-past-the-clamp")
	n, err := newNonce()
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteBody(ledger, n, full); err != nil {
		t.Fatalf("WriteBody: %v", err)
	}
	got, ok := LookupBody(ledger, n)
	if !ok || got != strings.TrimSpace(full) {
		t.Fatalf("roundtrip: ok=%v len(got)=%d want=%d", ok, utf8.RuneCountInString(got), utf8.RuneCountInString(strings.TrimSpace(full)))
	}
	// Misses: unknown nonce, malformed/traversal nonce, empty.
	for _, bad := range []string{"deadbeefdeadbeef", "../../etc/passwd", ""} {
		if _, ok := LookupBody(ledger, bad); ok {
			t.Errorf("LookupBody hit for a bad/absent nonce: %q", bad)
		}
	}
}

func TestWriteBodyRejectsTraversalNonce(t *testing.T) {
	if err := WriteBody(t.TempDir()+"/ledger", "../evil", "x"); err == nil {
		t.Error("WriteBody accepted a traversal nonce")
	}
}

func TestAppendMintsNonceAndWritesBody(t *testing.T) {
	dir := t.TempDir()
	ledger := dir + "/ledger"
	full := longBody(700, "instruction-that-lives-past-the-clamp")
	if err := Append(ledger, Entry{Time: time.Unix(3000, 0).UTC(), Channel: "c", From: "operator", To: "flotilla-dash", Gist: full}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	raw, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	nonce := nonceFromLine(string(raw))
	if !IsNonce(nonce) {
		t.Fatalf("Append did not write a nonce; line=%q", raw)
	}
	got, ok := LookupBody(ledger, nonce)
	if !ok || got != strings.TrimSpace(full) {
		t.Error("Append's companion body is not retrievable by its nonce")
	}
}

func TestUnclampedAppendCarriesNoNonce(t *testing.T) {
	dir := t.TempDir()
	ledger := dir + "/ledger"
	if err := Append(ledger, Entry{Time: time.Unix(1, 0).UTC(), From: "operator", To: "d", Gist: "short complete message"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(ledger)
	if n := nonceFromLine(string(raw)); n != "" {
		t.Errorf("an unclamped message must carry no nonce, got %q", n)
	}
}

// TestTwoSameSecondSamePrefixDistinctNonces is the cubic #422 CLASS fix: two messages in the
// same second, same parties, with an IDENTICAL clamped prefix get DISTINCT nonces and DISTINCT
// companion files — so lookup by identity never substitutes one for the other.
func TestTwoSameSecondSamePrefixDistinctNonces(t *testing.T) {
	dir := t.TempDir()
	ledger := dir + "/ledger"
	ts := time.Unix(4242, 0).UTC()
	shared := strings.Repeat("shared prefix word ", 40) // > maxGistRunes of identical text
	a := shared + " AAA-distinct-tail"
	b := shared + " BBB-distinct-tail"
	for _, g := range []string{a, b} {
		if err := Append(ledger, Entry{Time: ts, From: "operator", To: "d", Gist: g}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	lines := strings.Split(strings.TrimRight(readFile(t, ledger), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 ledger lines, got %d", len(lines))
	}
	nA, nB := nonceFromLine(lines[0]), nonceFromLine(lines[1])
	if !IsNonce(nA) || !IsNonce(nB) || nA == nB {
		t.Fatalf("expected two DISTINCT nonces, got %q and %q", nA, nB)
	}
	if got, _ := LookupBody(ledger, nA); got != strings.TrimSpace(a) {
		t.Error("nonce A resolved to the wrong body (cross-entry substitution)")
	}
	if got, _ := LookupBody(ledger, nB); got != strings.TrimSpace(b) {
		t.Error("nonce B resolved to the wrong body (cross-entry substitution)")
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// #423: the companion store is bounded — bodies older than BodyRetention are pruned on
// the same (clamped-append) path that grows the store; their ledger entries fall back to
// the audit gist, the documented miss path.
func TestPruneBodies_RetentionAndSafety(t *testing.T) {
	dir := t.TempDir()
	ledger := dir + "/cos-ledger.md"
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)

	// Three bodies: fresh, just-inside-retention, and stale; plus two ancient files the
	// pruner must never touch (a non-nonce name and a nonce-named non-.txt).
	mk := func(nonce, body string, age time.Duration) {
		t.Helper()
		if err := WriteBody(ledger, nonce, body); err != nil {
			t.Fatal(err)
		}
		p := BodiesDir(ledger) + "/" + nonce + ".txt"
		if err := os.Chtimes(p, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	fresh := strings.Repeat("a", NonceHexLen)
	edge := strings.Repeat("b", NonceHexLen)
	stale := strings.Repeat("c", NonceHexLen)
	mk(fresh, "fresh body", time.Hour)
	mk(edge, "edge body", BodyRetention-time.Minute)
	mk(stale, "stale body", BodyRetention+time.Hour)
	old := now.Add(-2 * BodyRetention)
	notOurs := []string{
		BodiesDir(ledger) + "/README.txt",                                   // non-nonce name
		BodiesDir(ledger) + "/" + strings.Repeat("e", NonceHexLen) + ".bak", // nonce-named, wrong extension
	}
	for _, p := range notOurs {
		if err := os.WriteFile(p, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}

	PruneBodies(ledger, now)

	if _, ok := LookupBody(ledger, fresh); !ok {
		t.Error("a fresh body must survive pruning")
	}
	if _, ok := LookupBody(ledger, edge); !ok {
		t.Error("a body just inside retention must survive pruning")
	}
	if _, ok := LookupBody(ledger, stale); ok {
		t.Error("a body past retention must be pruned (its entry falls back to the gist)")
	}
	for _, p := range notOurs {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s is not ours to delete (non-nonce name / wrong extension)", p)
		}
	}
	// Pruning a store that doesn't exist is a silent no-op (best-effort discipline).
	// The ledger sits in a NON-existent subdirectory so its BodiesDir truly doesn't
	// exist — a sibling ledger in `dir` would resolve to the SAME bodies/ dir the
	// writes above created and never exercise this path (cubic #452 P3).
	PruneBodies(dir+"/nowhere/no-such-ledger.md", now)
}

// The Append path prunes as it writes: a clamped append with entry time T removes bodies
// older than T-BodyRetention, so long-running operation stays bounded with no sweeper.
func TestAppendPrunesStaleBodies(t *testing.T) {
	dir := t.TempDir()
	ledger := dir + "/cos-ledger.md"
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	stale := strings.Repeat("d", NonceHexLen)
	if err := WriteBody(ledger, stale, "old clamped body"); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-BodyRetention - 24*time.Hour)
	if err := os.Chtimes(BodiesDir(ledger)+"/"+stale+".txt", old, old); err != nil {
		t.Fatal(err)
	}
	if err := Append(ledger, Entry{Time: now, From: "operator", To: "d", Gist: longBody(400, " END")}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, ok := LookupBody(ledger, stale); ok {
		t.Error("a clamped Append must prune stale bodies")
	}
	// The new entry's own body survived (it was written just now).
	line := strings.Split(strings.TrimRight(readFile(t, ledger), "\n"), "\n")[0]
	if n := nonceFromLine(line); n == "" {
		t.Fatal("clamped append must carry a nonce")
	} else if _, ok := LookupBody(ledger, n); !ok {
		t.Error("the fresh append's own body must survive its prune pass")
	}
}
