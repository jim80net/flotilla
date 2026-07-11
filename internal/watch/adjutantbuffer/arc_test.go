package adjutantbuffer

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestAssignArcJoinsSameKeyWithinQuiet(t *testing.T) {
	base := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	quiet := 60 * time.Second
	items := []Item{{
		At:         base,
		Reason:     FormatOperatorReason("m1", "first"),
		ArcID:      "arc_test_1",
		OpenedAt:   base,
		MessageIDs: []string{"m1"},
		ChannelID:  "C_HOME",
		OperatorID: "U_OP",
	}}
	arcID, opened := AssignArc(items, "xo", "C_HOME", "U_OP", base.Add(30*time.Second), quiet)
	if arcID != "arc_test_1" {
		t.Fatalf("arc_id = %q, want arc_test_1", arcID)
	}
	if !opened.Equal(base) {
		t.Fatalf("opened_at = %v, want %v", opened, base)
	}
}

func TestAssignArcSplitsDifferentChannel(t *testing.T) {
	base := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	items := []Item{{
		At: base, Reason: FormatOperatorReason("m1", "a"),
		ArcID: "arc_a", OpenedAt: base, ChannelID: "C_A", OperatorID: "U_OP",
	}}
	arcID, _ := AssignArc(items, "xo", "C_B", "U_OP", base.Add(10*time.Second), 60*time.Second)
	if arcID == "arc_a" {
		t.Fatal("different channel must not reuse arc")
	}
}

func TestAssignArcSplitsDifferentOperator(t *testing.T) {
	base := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	items := []Item{{
		At: base, Reason: FormatOperatorReason("m1", "a"),
		ArcID: "arc_a", OpenedAt: base, ChannelID: "C_HOME", OperatorID: "U_ONE",
	}}
	arcID, _ := AssignArc(items, "xo", "C_HOME", "U_TWO", base.Add(10*time.Second), 60*time.Second)
	if arcID == "arc_a" {
		t.Fatal("different operator must not reuse arc")
	}
}

func TestAssignArcQuietZeroSingleton(t *testing.T) {
	base := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	items := []Item{{
		At: base, Reason: FormatOperatorReason("m1", "a"),
		ArcID: "arc_a", OpenedAt: base, ChannelID: "C_HOME", OperatorID: "U_OP",
	}}
	arcID, _ := AssignArc(items, "xo", "C_HOME", "U_OP", base.Add(1*time.Second), 0)
	if arcID == "arc_a" {
		t.Fatal("quiet=0 must always open a new arc")
	}
}

func TestAssignArcExpiresAfterQuiet(t *testing.T) {
	base := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	items := []Item{{
		At: base, Reason: FormatOperatorReason("m1", "a"),
		ArcID: "arc_a", OpenedAt: base, ChannelID: "C_HOME", OperatorID: "U_OP",
	}}
	arcID, _ := AssignArc(items, "xo", "C_HOME", "U_OP", base.Add(61*time.Second), 60*time.Second)
	if arcID == "arc_a" {
		t.Fatal("arc must close after quiet elapses")
	}
}

func TestAppendOperatorCoalescesAndDedups(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/buf.json"
	base := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	quiet := 60 * time.Second

	if err := AppendOperator(path, "xo", "m1", "hello", "C_HOME", "U_OP", base, quiet); err != nil {
		t.Fatal(err)
	}
	if err := AppendOperator(path, "xo", "m1", "hello", "C_HOME", "U_OP", base.Add(time.Second), quiet); err != nil {
		t.Fatal(err)
	}
	if err := AppendOperator(path, "xo", "m2", "world", "C_HOME", "U_OP", base.Add(5*time.Second), quiet); err != nil {
		t.Fatal(err)
	}

	f, _, _, err := Peek(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(f.Items))
	}
	if f.Items[0].ArcID == "" || f.Items[1].ArcID == "" {
		t.Fatal("expected arc_id on operator items")
	}
	if f.Items[0].ArcID != f.Items[1].ArcID {
		t.Fatalf("same arc_key within quiet: %q vs %q", f.Items[0].ArcID, f.Items[1].ArcID)
	}
	if len(f.Items[0].MessageIDs) != 1 || f.Items[0].MessageIDs[0] != "m1" {
		t.Fatalf("message_ids = %v", f.Items[0].MessageIDs)
	}
}

func TestLegacyItemWithoutArcFieldsLoads(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/buf.json"
	legacy := File{
		Leader: "xo",
		Items: []Item{{
			At:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Reason: FormatOperatorReason("legacy1", "body"),
			Key:    "operator:legacy1|body",
		}},
	}
	raw, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	f, ok, _, err := Peek(path)
	if err != nil || !ok {
		t.Fatalf("peek failed: ok=%v err=%v", ok, err)
	}
	if EffectiveArcID(f.Items[0]) != "legacy:legacy1" {
		t.Fatalf("synthetic arc = %q", EffectiveArcID(f.Items[0]))
	}
}

func TestClampArcQuiet(t *testing.T) {
	if ClampArcQuiet(0) != 0 {
		t.Fatal("zero disables coalesce")
	}
	if ClampArcQuiet(30*time.Second) != minArcQuiet {
		t.Fatalf("floor = %v", ClampArcQuiet(30*time.Second))
	}
	if ClampArcQuiet(120*time.Second) != maxArcQuiet {
		t.Fatalf("ceiling = %v", ClampArcQuiet(120*time.Second))
	}
}