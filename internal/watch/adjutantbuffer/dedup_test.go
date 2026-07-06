package adjutantbuffer

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadRejectsEmptyBufferEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-buffer.json")
	if err := Append(path, "xo", []string{"real item", "  ", ""}); err != nil {
		t.Fatal(err)
	}
	f, ok, _, err := Peek(path)
	if err != nil || !ok {
		t.Fatalf("Peek: ok=%v err=%v", ok, err)
	}
	if len(f.Items) != 1 || f.Items[0].Reason != "real item" {
		t.Fatalf("empty entries should be dropped at load, got %+v", f.Items)
	}
}

func TestAppendAssignsStableKeyAndStateHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-buffer.json")
	reason := "backend: finished a turn (working→idle)"
	if err := Append(path, "xo", []string{reason}); err != nil {
		t.Fatal(err)
	}
	f, _, _, err := Peek(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Items) != 1 {
		t.Fatalf("items = %+v", f.Items)
	}
	it := f.Items[0]
	if it.Key != itemKey(reason) {
		t.Fatalf("key = %q want %q", it.Key, itemKey(reason))
	}
	if it.StateHash == "" {
		t.Fatal("state hash must be set at append")
	}
}

func TestPrepareInjectSkipsConsumedItems(t *testing.T) {
	at := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	reason := "backend: finished a turn (working→idle)"
	it := Item{At: at, Reason: reason, Key: itemKey(reason), StateHash: itemStateHash(reason, at)}
	delivered := DeliveredFile{Entries: []DeliveredEntry{{Key: it.Key, StateHash: it.StateHash}}}
	brief, inject, ok := PrepareInject("xo", File{Items: []Item{it}}, delivered, false, false)
	if ok {
		t.Fatalf("all-consumed must not inject, got brief=%q inject=%+v", brief, inject)
	}
	if brief != "" || len(inject) != 0 {
		t.Fatalf("want empty inject, got brief=%q inject=%+v", brief, inject)
	}
}

func TestPrepareInjectDeltaRedeliversWhenStateHashChanges(t *testing.T) {
	reason := "backend: finished a turn (working→idle)"
	at1 := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	at2 := at1.Add(time.Minute)
	it1 := Item{At: at1, Reason: reason, Key: itemKey(reason), StateHash: itemStateHash(reason, at1)}
	it2 := Item{At: at2, Reason: reason, Key: itemKey(reason), StateHash: itemStateHash(reason, at2)}
	delivered := DeliveredFile{Entries: []DeliveredEntry{{Key: it1.Key, StateHash: it1.StateHash}}}
	brief, inject, ok := PrepareInject("xo", File{Items: []Item{it2}}, delivered, false, false)
	if !ok || len(inject) != 1 {
		t.Fatalf("fresh edge occurrence must inject, ok=%v inject=%+v", ok, inject)
	}
	if !strings.Contains(brief, reason) {
		t.Fatalf("brief missing reason:\n%s", brief)
	}
	if strings.Count(brief, "•") != 1 {
		t.Fatalf("count-from-rendered: want one bullet, got:\n%s", brief)
	}
}

func TestPrepareInjectCountFromRenderedList(t *testing.T) {
	at := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	fresh := Item{At: at, Reason: "backend: finished a turn (working→idle)"}
	fresh.Key = itemKey(fresh.Reason)
	fresh.StateHash = itemStateHash(fresh.Reason, at)
	consumedAt := at.Add(-time.Hour)
	consumed := Item{At: consumedAt, Reason: "frontend: entered shell"}
	consumed.Key = itemKey(consumed.Reason)
	consumed.StateHash = itemStateHash(consumed.Reason, consumedAt)
	delivered := DeliveredFile{Entries: []DeliveredEntry{{Key: consumed.Key, StateHash: consumed.StateHash}}}
	brief, inject, ok := PrepareInject("xo", File{Items: []Item{consumed, fresh}}, delivered, false, false)
	if !ok || len(inject) != 1 {
		t.Fatalf("want one inject item, ok=%v inject=%+v", ok, inject)
	}
	if !strings.Contains(brief, "1 buffered item(s)") {
		t.Fatalf("count must match post-dedup rendered list, got:\n%s", brief)
	}
	if strings.Contains(brief, consumed.Reason) {
		t.Fatalf("consumed item must not appear in bullets:\n%s", brief)
	}
}

func TestPrepareInjectNoInjectOnEmptyAfterDedup(t *testing.T) {
	at := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	it := Item{At: at, Reason: "backend PR gate"}
	it.Key = itemKey(it.Reason)
	it.StateHash = itemStateHash(it.Reason, at)
	delivered := DeliveredFile{Entries: []DeliveredEntry{{Key: it.Key, StateHash: it.StateHash}}}
	brief, _, ok := PrepareInject("xo", File{Items: []Item{it}}, delivered, false, false)
	if ok {
		t.Fatalf("specimen-3 null interrupt must not fire, brief=%q", brief)
	}
}

func TestRecordDeliveredRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-buffer-delivered.json")
	at := time.Now().UTC()
	it := Item{At: at, Reason: "backend: finished a turn (working→idle)", Key: itemKey("backend: finished a turn (working→idle)"), StateHash: itemStateHash("backend: finished a turn (working→idle)", at)}
	if err := RecordDelivered(path, "xo", []Item{it}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadDelivered(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Has(it.Key, it.StateHash) {
		t.Fatalf("delivered ledger missing %+v: %+v", it, got)
	}
}

func TestFilterUndeliveredDropsConsumedPair(t *testing.T) {
	at := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	it := Item{At: at, Reason: "backend: finished a turn (working→idle)"}
	it.Key = itemKey(it.Reason)
	it.StateHash = itemStateHash(it.Reason, at)
	delivered := DeliveredFile{Entries: []DeliveredEntry{{Key: it.Key, StateHash: it.StateHash}}}
	got := FilterUndelivered([]Item{it}, delivered)
	if len(got) != 0 {
		t.Fatalf("filter must drop consumed pair, got %+v", got)
	}
}
