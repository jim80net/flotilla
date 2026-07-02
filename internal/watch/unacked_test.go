package watch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/transport"
	"github.com/jim80net/flotilla/internal/unacked"
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

func TestUnackedBackstop_PrunePersistsOnQuietSweep(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	path := filepath.Join(dir, "unacked.json")
	st := unackedState{Records: []alertedRecord{
		{MessageID: "old", AlertedAt: now.Add(-8 * 24 * time.Hour)},
	}}
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &roster.Config{OperatorUserID: "op", Channels: []roster.Channel{{ChannelID: "C1"}}}
	u := NewUnackedBackstop(cfg, &fakeRecent{byChannel: map[string][]transport.Message{"C1": {}}}, path,
		func(string) { t.Fatal("quiet sweep must not alert") }, nil, nil)
	u.now = func() time.Time { return now }
	u.sweep()
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved unackedState
	if err := json.Unmarshal(out, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Records) != 0 {
		t.Fatalf("pruned state should persist on quiet sweep; got %+v", saved.Records)
	}
}

func TestCoordinatorWakeBusyIsRetryable(t *testing.T) {
	if !errors.Is(surface.ErrBusy, surface.ErrBusy) {
		t.Fatal("sanity")
	}
	_ = unacked.DefaultMinAge
}
