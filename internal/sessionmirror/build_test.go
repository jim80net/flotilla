package sessionmirror

import (
	"testing"
	"time"
)

func TestBuildHistoryRespectsLimit(t *testing.T) {
	var lines []byte
	for i := 0; i < 5; i++ {
		rec := NewRecord(Input{
			Agent: "backend",
			At:    time.Unix(int64(i), 0).UTC(),
			Info:  string(rune('a' + i)),
		})
		lines = append(lines, MustLine(rec)...)
	}
	doc := BuildHistory("backend", lines, 2)
	if len(doc.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(doc.Entries))
	}
	if doc.Entries[0].Info != "d" || doc.Entries[1].Info != "e" {
		t.Errorf("last-two infos = %q, %q", doc.Entries[0].Info, doc.Entries[1].Info)
	}
	if doc.Limit != 2 {
		t.Errorf("limit = %d", doc.Limit)
	}
}

func TestParseLinesSkipsMalformed(t *testing.T) {
	data := []byte("not json\n" + string(MustLine(NewRecord(Input{Agent: "a", Info: "ok"}))))
	entries := ParseLines(data)
	if len(entries) != 1 || entries[0].Info != "ok" {
		t.Fatalf("entries = %+v", entries)
	}
}
