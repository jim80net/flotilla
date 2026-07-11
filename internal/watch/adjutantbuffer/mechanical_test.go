package adjutantbuffer

import (
	"strings"
	"testing"
	"time"
)

func TestIsMechanicalFinishEdge(t *testing.T) {
	cases := []struct {
		reason string
		want   bool
	}{
		{"backend: finished a turn (working→idle)", true},
		{"backend Working→Idle", true},
		{"frontend: finished a turn", true},
		{"backend PR gate blocked on review", false},
		{"operator:m1|hello", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsMechanicalFinishEdge(c.reason); got != c.want {
			t.Errorf("IsMechanicalFinishEdge(%q)=%v want %v", c.reason, got, c.want)
		}
	}
}

func TestPrepareInject_MechanicalOnlyNoBrief(t *testing.T) {
	at := time.Now().UTC()
	reason := "backend: finished a turn (working→idle)"
	f := File{Leader: "xo", Items: []Item{{At: at, Reason: reason}}}
	f.Items = normalizeItems(f.Items)
	brief, items, ok := PrepareInject("xo", f, DeliveredFile{}, false, false)
	if ok {
		t.Fatalf("mechanical-only must not inject, brief=%q", brief)
	}
	if len(items) != 1 {
		t.Fatalf("auto-consume items = %d", len(items))
	}
	if strings.Contains(brief, "Needs you") {
		t.Fatal("Needs you must not appear")
	}
}

func TestPrepareInject_JudgmentNeedsYouExcludesMechanical(t *testing.T) {
	at := time.Now().UTC()
	f := File{Leader: "xo", Items: []Item{
		{At: at, Reason: "backend: finished a turn (working→idle)"},
		{At: at, Reason: "backend PR gate needs decision"},
	}}
	brief, items, ok := PrepareInject("xo", f, DeliveredFile{}, false, false)
	if !ok {
		t.Fatal("judgment item must inject")
	}
	if !strings.Contains(brief, "Needs you") || !strings.Contains(brief, "PR gate") {
		t.Fatalf("brief missing judgment:\n%s", brief)
	}
	if strings.Contains(brief, "working→idle") || strings.Contains(brief, "finished a turn") {
		t.Fatalf("mechanical edge must not appear under Needs you:\n%s", brief)
	}
	// Both mechanical + judgment recorded on confirm.
	if len(items) != 2 {
		t.Fatalf("record items = %d want 2", len(items))
	}
}

func TestFormatBrief_NeverListsMechanical(t *testing.T) {
	f := File{Leader: "xo", Items: []Item{
		{Reason: "backend: finished a turn (working→idle)"},
		{Reason: "urgent judgment item here"},
	}}
	got := FormatBrief("xo", f, false, false)
	if strings.Contains(got, "working→idle") {
		t.Fatalf("mechanical leaked into brief:\n%s", got)
	}
	if !strings.Contains(got, "urgent judgment") {
		t.Fatalf("judgment missing:\n%s", got)
	}
}
