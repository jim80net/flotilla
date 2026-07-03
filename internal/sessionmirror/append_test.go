package sessionmirror

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAppendCreatesLedgerAndBuildHistory(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecord(Input{
		Agent:   "backend",
		At:      time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC),
		Verbose: "verbose body",
		Info:    "info body",
	})
	if err := Append(dir, "backend", rec, AppendOptions{}); err != nil {
		t.Fatal(err)
	}
	path, err := LedgerPath(dir, "backend")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := BuildHistory("backend", raw, 0)
	if len(doc.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(doc.Entries))
	}
	if doc.Entries[0].Info != "info body" {
		t.Errorf("entry info = %q", doc.Entries[0].Info)
	}
}

func TestAppendRetentionCapDropsOldest(t *testing.T) {
	dir := t.TempDir()
	const max = 3
	for i := 0; i < 5; i++ {
		rec := NewRecord(Input{
			Agent:   "backend",
			At:      time.Unix(int64(i), 0).UTC(),
			Verbose: "v",
			Info:    strings.Repeat("x", i+1),
		})
		if err := Append(dir, "backend", rec, AppendOptions{MaxEntries: max}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	path, err := LedgerPath(dir, "backend")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := BuildHistory("backend", raw, 0)
	if len(doc.Entries) != max {
		t.Fatalf("entries = %d, want %d", len(doc.Entries), max)
	}
	for i, want := range []string{"xxx", "xxxx", "xxxxx"} {
		if doc.Entries[i].Info != want {
			t.Errorf("entry[%d].info = %q, want %q", i, doc.Entries[i].Info, want)
		}
	}
}

func TestAppendRequiresRosterDirAndAgent(t *testing.T) {
	if err := Append("", "backend", Record{}, AppendOptions{}); err == nil {
		t.Fatal("expected error for empty roster dir")
	}
	if err := Append(t.TempDir(), "", Record{}, AppendOptions{}); err == nil {
		t.Fatal("expected error for empty agent")
	}
}

func TestAppendReadsLargeLedgerLines(t *testing.T) {
	dir := t.TempDir()
	// Near DefaultVerboseCap — exercises scanner bound (verboseCap×4 + overhead).
	large := strings.Repeat("世", DefaultVerboseCap-1)
	rec := NewRecord(Input{
		Agent:   "backend",
		At:      time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC),
		Verbose: large,
		Info:    "info",
	})
	if err := Append(dir, "backend", rec, AppendOptions{}); err != nil {
		t.Fatal(err)
	}
	follow := NewRecord(Input{
		Agent:   "backend",
		At:      time.Date(2026, 7, 3, 12, 1, 0, 0, time.UTC),
		Verbose: "next",
		Info:    "next",
	})
	if err := Append(dir, "backend", follow, AppendOptions{}); err != nil {
		t.Fatalf("second append after large line: %v", err)
	}
	path, err := LedgerPath(dir, "backend")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := BuildHistory("backend", raw, 0)
	if len(doc.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(doc.Entries))
	}
	if doc.Entries[0].Verbose != large {
		t.Error("large verbose entry lost on read-back")
	}
	if len(ParseLines(raw)) != 2 {
		t.Fatal("ParseLines must not drop lines at/after the large record")
	}
}

func TestMarshalLedgerLineFitsMaxBytesWithANSIEscapes(t *testing.T) {
	// ANSI sequences are common in tmux turn-finals; JSON escaping expands them.
	const esc = "\x1b[31m"
	unit := esc + "x"
	n := DefaultVerboseCap/len([]rune(unit)) + 1
	verbose := truncateRunes(strings.Repeat(unit, n), DefaultVerboseCap)

	rec := NewRecord(Input{
		Agent:   "backend",
		At:      time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC),
		Verbose: verbose,
		Info:    "modeled info body",
	})
	line, err := marshalLedgerLine(rec)
	if err != nil {
		t.Fatal(err)
	}
	if len(line) > maxLineBytes+1 { // +1 for trailing newline
		t.Fatalf("marshaled line = %d bytes, want ≤ %d (+newline)", len(line), maxLineBytes)
	}
	if len(line) <= 1 {
		t.Fatal("expected non-empty marshaled line")
	}
}

func TestAppendANSIDenseVerboseAtCapRoundTrips(t *testing.T) {
	dir := t.TempDir()
	const esc = "\x1b[31m"
	unit := esc + "x"
	n := DefaultVerboseCap/len([]rune(unit)) + 1
	verbose := truncateRunes(strings.Repeat(unit, n), DefaultVerboseCap)

	rec := NewRecord(Input{
		Agent:   "backend",
		At:      time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC),
		Verbose: verbose,
		Info:    "info",
	})
	if err := Append(dir, "backend", rec, AppendOptions{}); err != nil {
		t.Fatal(err)
	}
	follow := NewRecord(Input{
		Agent:   "backend",
		At:      time.Date(2026, 7, 3, 12, 1, 0, 0, time.UTC),
		Verbose: "next",
		Info:    "next",
	})
	if err := Append(dir, "backend", follow, AppendOptions{}); err != nil {
		t.Fatalf("ledger wedged after ANSI-heavy line: %v", err)
	}
	path, err := LedgerPath(dir, "backend")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := BuildHistory("backend", raw, 0)
	if len(doc.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(doc.Entries))
	}
	if len(ParseLines(raw)) != 2 {
		t.Fatal("ParseLines dropped entries after ANSI-heavy line")
	}
}

func TestAppendConcurrentSameAgentRespectsCap(t *testing.T) {
	dir := t.TempDir()
	const max = 5
	const workers = 20
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			rec := NewRecord(Input{
				Agent:   "backend",
				At:      time.Unix(int64(i), 0).UTC(),
				Verbose: "v",
				Info:    fmt.Sprintf("entry-%02d", i),
			})
			if err := Append(dir, "backend", rec, AppendOptions{MaxEntries: max}); err != nil {
				t.Errorf("append %d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	path, err := LedgerPath(dir, "backend")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := BuildHistory("backend", raw, 0)
	if len(doc.Entries) != max {
		t.Fatalf("entries = %d, want ring cap %d", len(doc.Entries), max)
	}
}

func TestAppendRejectsUnsafeAgentName(t *testing.T) {
	rec := NewRecord(Input{Agent: "backend", Verbose: "v", Info: "i"})
	for _, agent := range []string{"../x", "a/b"} {
		if err := Append(t.TempDir(), agent, rec, AppendOptions{}); err == nil {
			t.Errorf("Append(%q) = nil, want validation error", agent)
		}
	}
}
