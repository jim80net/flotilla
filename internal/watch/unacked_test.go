package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/transport"
)

type fakeRecent struct {
	byChannel map[string][]transport.Message
	err       error
}

func (f *fakeRecent) Recent(channelID string, limit int) ([]transport.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byChannel[channelID], nil
}

func TestUnackedStateStore_Prune(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	st := unackedState{Records: []alertedRecord{
		{MessageID: "old", AlertedAt: now.Add(-8 * 24 * time.Hour)},
		{MessageID: "new", AlertedAt: now.Add(-1 * 24 * time.Hour)},
	}}
	store := newUnackedStateStore("", 7*24*time.Hour)
	pruned := store.prune(st, now)
	if len(pruned.Records) != 1 || pruned.Records[0].MessageID != "new" {
		t.Fatalf("prune = %+v, want only new", pruned.Records)
	}
}

func TestUnackedBackstop_AlertsOnce_RetriesWakeOnBusy(t *testing.T) {
	now := time.Date(2026, 7, 2, 13, 0, 0, 0, time.UTC)
	cfg := &roster.Config{
		OperatorUserID: "op",
		XOAgent:        "cos",
		Channels: []roster.Channel{
			{ChannelID: "C1", XOAgent: "cos", Role: "alpha"},
		},
		Agents: []roster.Agent{{Name: "cos"}},
	}
	msg := transport.Message{
		ID: "100", AuthorID: "op", Content: "Can you ship the fix?",
		Timestamp: now.Add(-45 * time.Minute),
	}
	var alerts int
	var wakes int
	u := NewUnackedBackstop(cfg, &fakeRecent{byChannel: map[string][]transport.Message{"C1": {msg}}}, "",
		func(string) { alerts++ },
		func(agent, body string) error {
			wakes++
			if wakes == 1 {
				return surface.ErrBusy
			}
			return nil
		},
		nil,
	)
	u.now = func() time.Time { return now }
	st := unackedState{}

	u.sweepChannel(roster.Channel{ChannelID: "C1", XOAgent: "cos", Role: "alpha"}, &st, now)
	if alerts != 1 {
		t.Fatalf("alerts = %d, want 1", alerts)
	}
	if len(st.Records) != 1 || st.Records[0].WakeDone {
		t.Fatalf("wake busy must not mark WakeDone: %+v", st.Records)
	}

	u.sweepChannel(roster.Channel{ChannelID: "C1", XOAgent: "cos", Role: "alpha"}, &st, now)
	if alerts != 1 {
		t.Fatalf("second sweep must not re-alert; alerts = %d", alerts)
	}
	if !st.Records[0].WakeDone {
		t.Fatal("second sweep should confirm wake")
	}
}

func TestUnackedBackstop_NoOperatorIDInert(t *testing.T) {
	u := NewUnackedBackstop(&roster.Config{}, &fakeRecent{}, "", func(string) {}, nil, nil)
	if u.sweepChannel(roster.Channel{ChannelID: "C1"}, &unackedState{}, time.Now()) {
		t.Fatal("no operator id should be inert")
	}
}

func TestUnackedState_IndexKeysChannelAndMessage(t *testing.T) {
	st := unackedState{Records: []alertedRecord{
		{ChannelID: "C1", MessageID: "100"},
		{ChannelID: "C2", MessageID: "100"},
	}}
	if _, ok := st.index("C1", "100"); !ok {
		t.Fatal("want hit on matching channel+message")
	}
	if _, ok := st.index("C2", "999"); ok {
		t.Fatal("same MessageID on a different channel must not alias")
	}
}

func TestUnackedStateStore_PersistsPrunedLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-unacked-alerted.json")
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	// retention=0 on write so the expired record lands on disk; load uses real retention.
	writeStore := newUnackedStateStore(path, 0)
	st := unackedState{Records: []alertedRecord{
		{MessageID: "old", ChannelID: "C1", AlertedAt: now.Add(-8 * 24 * time.Hour)},
	}}
	if err := writeStore.save(st, now); err != nil {
		t.Fatal(err)
	}
	readStore := newUnackedStateStore(path, 7*24*time.Hour)
	loaded, pruned := readStore.load(now)
	if !pruned {
		t.Fatal("load must report pruned records")
	}
	if err := readStore.save(loaded, now); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"old"`) {
		t.Fatalf("pruned record must be persisted; still in %s", raw)
	}
}

func TestUnackedBackstop_SweepPersistsPrunedLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-unacked-alerted.json")
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	writeStore := newUnackedStateStore(path, 0)
	st := unackedState{Records: []alertedRecord{
		{MessageID: "old", ChannelID: "C1", AlertedAt: now.Add(-8 * 24 * time.Hour)},
	}}
	if err := writeStore.save(st, now); err != nil {
		t.Fatal(err)
	}
	store := newUnackedStateStore(path, 7*24*time.Hour)
	u := &UnackedBackstop{
		cfg:   &roster.Config{},
		store: store,
		now:   func() time.Time { return now },
	}
	u.sweep()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"old"`) {
		t.Fatalf("sweep must persist pruned load even with no new findings; still in %s", raw)
	}
}
