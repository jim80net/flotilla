package cos

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixedTime() time.Time {
	// A fixed instant so the rendered line is deterministic. The ledger is NOT
	// filtered by a rolling window, so a hardcoded timestamp is safe here.
	return time.Date(2026, 6, 18, 14, 3, 5, 0, time.UTC)
}

func TestLineFormat(t *testing.T) {
	got := Line(Entry{Time: fixedTime(), Channel: "C_ALPHA", From: "operator", To: "alpha-xo", Gist: "ship the cache PR when green"})
	want := "- 2026-06-18T14:03:05Z · C_ALPHA · operator → alpha-xo · \"ship the cache PR when green\"\n"
	if got != want {
		t.Errorf("Line =\n%q\nwant\n%q", got, want)
	}
}

func TestLineEmptyChannelRendersDash(t *testing.T) {
	got := Line(Entry{Time: fixedTime(), Channel: "", From: "beta-xo", To: "operator", Gist: "merged"})
	if !strings.Contains(got, " · - · ") {
		t.Errorf("empty channel should render as '-': %q", got)
	}
}

func TestLineFlattensMultilineGist(t *testing.T) {
	// A multi-line body must collapse to ONE physical line (the atomic-append
	// precondition). %q escapes the newline as \n.
	got := Line(Entry{Time: fixedTime(), Channel: "C", From: "operator", To: "xo", Gist: "line one\nline two"})
	if strings.Count(got, "\n") != 1 { // only the trailing newline
		t.Errorf("multi-line gist must render on one line, got %q", got)
	}
	if !strings.Contains(got, `\n`) {
		t.Errorf("embedded newline should be escaped: %q", got)
	}
}

func TestClampGistBoundsLine(t *testing.T) {
	long := strings.Repeat("é", maxGistRunes+200) // multi-byte, over the cap
	got := Line(Entry{Time: fixedTime(), Channel: "C", From: "operator", To: "xo", Gist: long})
	// The whole line must stay well under PIPE_BUF (4096) so the append is atomic.
	if len(got) >= 4096 {
		t.Errorf("clamped line is %d bytes; must stay < PIPE_BUF (4096) for atomic append", len(got))
	}
	if !strings.Contains(got, "…") {
		t.Error("an over-length gist must be truncated with an ellipsis marker")
	}
}

func TestLineWorstCaseQuoteExpansionStaysAtomic(t *testing.T) {
	// \U0010000C is an unprintable supplementary code point: %q escapes it to the
	// 10-byte \U0010000c form — Go's WORST-CASE expansion (~10 bytes/rune). A full
	// maxGistRunes gist of these is the real stress on the PIPE_BUF byte bound that the
	// rune clamp must hold (the prior TestClampGistBounds case used 'é', which %q does
	// not escape, so it never exercised the expansion). A realistic prefix is included.
	gist := strings.Repeat("\U0010000C", maxGistRunes+50)
	got := Line(Entry{Time: fixedTime(), Channel: "123456789012345678", From: "operator", To: "alpha-xo", Gist: gist})
	if len(got) > maxLineBytes {
		t.Errorf("worst-case %%q line is %d bytes; must be ≤ PIPE_BUF (%d) for atomic append", len(got), maxLineBytes)
	}
}

func TestLineBackstopClipsPathologicalPrefix(t *testing.T) {
	// The gist is rune-clamped, but channel/from/to are unbounded by type. A pathological
	// agent name (far beyond any real roster) must still produce an atomically-appendable
	// line: Line's clip backstop guarantees ≤ PIPE_BUF unconditionally, ending in '\n'.
	huge := strings.Repeat("z", 5000)
	got := Line(Entry{Time: fixedTime(), Channel: "C", From: "operator", To: huge, Gist: "hello"})
	if len(got) > maxLineBytes {
		t.Errorf("backstop failed: line is %d bytes; must be ≤ %d", len(got), maxLineBytes)
	}
	if !strings.HasSuffix(got, "\n") || strings.Count(got, "\n") != 1 {
		t.Errorf("clipped line must end in exactly one newline: %q", got[max(0, len(got)-20):])
	}
}

func TestLineFlattensNewlineInPlainFields(t *testing.T) {
	// channel/from/to render with %s (not %q), so a CR/LF in any of them must be escaped
	// — otherwise it would inject a second physical line and forge a ledger entry,
	// breaking the one-line-per-entry + atomic-append invariant (cubic #109 P2).
	got := Line(Entry{
		Time:    fixedTime(),
		Channel: "C\nFORGED · evil → victim · \"injected\"",
		From:    "oper\rator",
		To:      "alpha\nxo",
		Gist:    "hi",
	})
	if strings.Count(got, "\n") != 1 { // only the trailing newline
		t.Errorf("a newline in a plain field must not inject a second line, got %q", got)
	}
	if !strings.Contains(got, `\n`) || !strings.Contains(got, `\r`) {
		t.Errorf("embedded CR/LF should be escaped: %q", got)
	}
}

func TestAppendCreatesAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context-ledger.md")
	if err := Append(path, Entry{Time: fixedTime(), Channel: "C_ALPHA", From: "operator", To: "alpha-xo", Gist: "first"}); err != nil {
		t.Fatalf("Append (create): %v", err)
	}
	if err := Append(path, Entry{Time: fixedTime(), Channel: "C_ALPHA", From: "alpha-xo", To: "operator", Gist: "second"}); err != nil {
		t.Fatalf("Append (existing): %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("ledger has %d lines, want 2:\n%s", len(lines), raw)
	}
	if !strings.Contains(lines[0], "operator → alpha-xo") || !strings.Contains(lines[0], `"first"`) {
		t.Errorf("line 0 wrong: %q", lines[0])
	}
	if !strings.Contains(lines[1], "alpha-xo → operator") || !strings.Contains(lines[1], `"second"`) {
		t.Errorf("line 1 wrong: %q", lines[1])
	}
}

func TestAppendConcurrentWritersNoInterleave(t *testing.T) {
	// Many goroutines append concurrently. Each line must land intact: exactly N lines,
	// each a complete, well-formed entry (no torn/interleaved bytes), since each line is a
	// single O_APPEND write of ≤ PIPE_BUF bytes.
	//
	// NOTE: this is an IN-PROCESS proxy for the documented CROSS-PROCESS invariant (the
	// `watch` daemon's mirror hook + a separate `flotilla notify` process). The kernel's
	// O_APPEND-under-PIPE_BUF atomicity is the same guarantee whether the racing writers
	// are goroutines (sharing one fd table) or separate processes (each its own fd, same
	// inode) — both issue independent write(2) calls on O_APPEND descriptors — so this
	// exercises the same code path; a true multi-process test would add a subprocess
	// harness without testing a different kernel guarantee.
	path := filepath.Join(t.TempDir(), "context-ledger.md")
	const n = 64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = Append(path, Entry{Time: fixedTime(), Channel: "C", From: "operator", To: "xo", Gist: strings.Repeat("x", 100)})
		}()
	}
	wg.Wait()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != n {
		t.Fatalf("got %d lines, want %d (interleaved/torn append)", len(lines), n)
	}
	for i, ln := range lines {
		if !strings.HasPrefix(ln, "- 2026-06-18T14:03:05Z · C · operator → xo · ") || !strings.HasSuffix(ln, `"`) {
			t.Fatalf("line %d torn/malformed: %q", i, ln)
		}
	}
}
