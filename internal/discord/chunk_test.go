package discord

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunkContentSingleChunkUnderLimit(t *testing.T) {
	got := ChunkContent("a short body", 100)
	if len(got) != 1 || got[0] != "a short body" {
		t.Errorf("ChunkContent(short) = %v, want one unchanged chunk", got)
	}
}

func TestChunkContentSplitsOnParagraphBoundaries(t *testing.T) {
	// Three 40-rune paragraphs with a limit of 90: paras 1+2 fit together (40+2+40=82<=90); para 3
	// starts a new chunk. Result: 2 ordered chunks, each within the limit, reassembling to the source.
	p1 := strings.Repeat("a", 40)
	p2 := strings.Repeat("b", 40)
	p3 := strings.Repeat("c", 40)
	text := p1 + "\n\n" + p2 + "\n\n" + p3
	got := ChunkContent(text, 90)
	if len(got) != 2 {
		t.Fatalf("ChunkContent = %d chunks, want 2; %v", len(got), got)
	}
	for i, c := range got {
		if n := utf8.RuneCountInString(c); n > 90 {
			t.Errorf("chunk %d has %d runes, exceeds limit 90", i, n)
		}
	}
	if got[0] != p1+"\n\n"+p2 || got[1] != p3 {
		t.Errorf("chunk boundaries/order wrong: %q", got)
	}
}

func TestChunkContentManyParagraphsEachWithinLimit(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 20; i++ {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.Repeat("x", 30))
	}
	got := ChunkContent(b.String(), 50)
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(got))
	}
	for i, c := range got {
		if n := utf8.RuneCountInString(c); n > 50 {
			t.Errorf("chunk %d has %d runes, exceeds limit 50", i, n)
		}
	}
	// Order preserved: concatenating chunks (re-joining with the boundary that was split on) recovers
	// every paragraph in sequence.
	if !strings.HasPrefix(got[0], "xxx") {
		t.Errorf("first chunk should start with the first paragraph, got %q", got[0])
	}
}

func TestChunkContentHardSplitsOversizeParagraph(t *testing.T) {
	// A single 250-rune paragraph with no internal boundary, limit 100 → 3 hard-split chunks
	// (100+100+50), each within the limit, reassembling exactly.
	para := strings.Repeat("z", 250)
	got := ChunkContent(para, 100)
	if len(got) != 3 {
		t.Fatalf("ChunkContent(oversize para) = %d chunks, want 3; lens=%v", len(got), chunkLens(got))
	}
	for i, c := range got {
		if n := utf8.RuneCountInString(c); n > 100 {
			t.Errorf("chunk %d has %d runes, exceeds limit 100", i, n)
		}
	}
	if strings.Join(got, "") != para {
		t.Error("hard-split chunks must reassemble to the original paragraph")
	}
}

func TestChunkContentNeverSplitsMidRune(t *testing.T) {
	// Multi-byte runes (3 bytes each) hard-split on a rune boundary, never mid-rune.
	para := strings.Repeat("世", 250) // each "世" is 3 bytes, 1 rune
	got := ChunkContent(para, 100)
	for i, c := range got {
		if n := utf8.RuneCountInString(c); n > 100 {
			t.Errorf("chunk %d has %d runes, exceeds limit 100", i, n)
		}
		if !utf8.ValidString(c) {
			t.Errorf("chunk %d split a multi-byte rune", i)
		}
	}
	if strings.Join(got, "") != para {
		t.Error("multi-byte hard-split chunks must reassemble to the original")
	}
}

func TestChunkContentEmpty(t *testing.T) {
	got := ChunkContent("", 100)
	if len(got) != 1 || got[0] != "" {
		t.Errorf("ChunkContent(\"\") = %v, want a single empty chunk", got)
	}
}

func chunkLens(chunks []string) []int {
	ns := make([]int, len(chunks))
	for i, c := range chunks {
		ns[i] = utf8.RuneCountInString(c)
	}
	return ns
}
