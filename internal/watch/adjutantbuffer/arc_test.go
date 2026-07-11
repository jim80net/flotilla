package adjutantbuffer

import (
	"strings"
	"testing"
	"time"
)

func TestParseArcQuiet(t *testing.T) {
	if ParseArcQuiet("") != DefaultArcQuiet {
		t.Fatal("empty default")
	}
	if ParseArcQuiet("0") != 0 || ParseArcQuiet("0s") != 0 {
		t.Fatal("zero disable")
	}
	if d := ParseArcQuiet("30s"); d != ArcQuietFloor {
		t.Fatalf("floor clamp got %v", d)
	}
	if d := ParseArcQuiet("120s"); d != ArcQuietCeil {
		t.Fatalf("ceil clamp got %v", d)
	}
	if d := ParseArcQuiet("60s"); d != 60*time.Second {
		t.Fatalf("got %v", d)
	}
}

func TestAssignArc_SameKeyJoins(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/buf.json"
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	quiet := 60 * time.Second
	if err := AppendOperator(path, "xo", "m1", "first", "C1", "op", now, quiet); err != nil {
		t.Fatal(err)
	}
	if err := AppendOperator(path, "xo", "m2", "second", "C1", "op", now.Add(10*time.Second), quiet); err != nil {
		t.Fatal(err)
	}
	f, _, err := load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Items) != 2 {
		t.Fatalf("items=%d", len(f.Items))
	}
	if f.Items[0].ArcID == "" || f.Items[0].ArcID != f.Items[1].ArcID {
		t.Fatalf("arc mismatch %q vs %q", f.Items[0].ArcID, f.Items[1].ArcID)
	}
	if f.Items[0].ChannelID != "C1" || f.Items[0].OperatorID != "op" {
		t.Fatalf("meta=%+v", f.Items[0])
	}
}

func TestAssignArc_DifferentChannelSplits(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/buf.json"
	now := time.Now().UTC()
	quiet := 60 * time.Second
	if err := AppendOperator(path, "xo", "m1", "a", "C1", "op", now, quiet); err != nil {
		t.Fatal(err)
	}
	if err := AppendOperator(path, "xo", "m2", "b", "C2", "op", now.Add(time.Second), quiet); err != nil {
		t.Fatal(err)
	}
	f, _, _ := load(path)
	if f.Items[0].ArcID == f.Items[1].ArcID {
		t.Fatal("expected different arcs for different channels")
	}
}

func TestAssignArc_QuietZeroSingleton(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/buf.json"
	now := time.Now().UTC()
	if err := AppendOperator(path, "xo", "m1", "a", "C1", "op", now, 0); err != nil {
		t.Fatal(err)
	}
	if err := AppendOperator(path, "xo", "m2", "b", "C1", "op", now.Add(time.Second), 0); err != nil {
		t.Fatal(err)
	}
	f, _, _ := load(path)
	if f.Items[0].ArcID == f.Items[1].ArcID {
		t.Fatal("quiet=0 must not join")
	}
}

func TestGroupByArcAndFormat(t *testing.T) {
	now := time.Now().UTC()
	items := []Item{
		{At: now, Reason: FormatOperatorReason("m1", "hello"), ArcID: "arcA"},
		{At: now.Add(time.Second), Reason: FormatOperatorReason("m2", "world"), ArcID: "arcA"},
	}
	groups := GroupByArc(items)
	if len(groups) != 1 || len(groups[0].Items) != 2 {
		t.Fatalf("groups=%+v", groups)
	}
	body := FormatArcBodies(groups[0].Items)
	if body != "hello"+BodyDelimiter+"world" {
		t.Fatalf("body=%q", body)
	}
}

func TestGroupByArc_LegacySingleton(t *testing.T) {
	items := []Item{
		{At: time.Now(), Reason: FormatOperatorReason("m9", "solo")},
	}
	groups := GroupByArc(items)
	if len(groups) != 1 || !strings.Contains(groups[0].ArcID, "singleton") {
		t.Fatalf("%+v", groups)
	}
}

func TestAppendOperator_Dedup(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/buf.json"
	now := time.Now().UTC()
	if err := AppendOperator(path, "xo", "m1", "hi", "C1", "op", now, 60*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := AppendOperator(path, "xo", "m1", "hi", "C1", "op", now.Add(time.Second), 60*time.Second); err != nil {
		t.Fatal(err)
	}
	if Len(path) != 1 {
		t.Fatalf("len=%d want 1", Len(path))
	}
}
