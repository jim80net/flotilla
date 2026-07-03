package watch

import (
	"encoding/json"
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
	msgs := f.byChannel[channelID]
	if limit <= 0 || limit >= len(msgs) {
		return msgs, nil
	}
	return msgs[len(msgs)-limit:], nil
}

func (f *fakeRecent) RecentSince(channelID string, since time.Time) ([]transport.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []transport.Message
	for _, m := range f.byChannel[channelID] {
		if m.Timestamp.IsZero() || !m.Timestamp.Before(since) {
			out = append(out, m)
		}
	}
	return out, nil
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

func TestUnackedStateStore_PersistsPrunedLoad(t *testing.T) {
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
		t.Fatalf("pruned record must be persisted to disk; still in %s", raw)
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
	u := &UnackedBackstop{
		cfg:   &roster.Config{},
		store: newUnackedStateStore(path, 7*24*time.Hour),
		now:   func() time.Time { return now },
	}
	u.sweep()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"old"`) {
		t.Fatalf("sweep must persist pruned load with no new findings; still in %s", raw)
	}
}

func TestUnackedState_IndexByChannelAndMessage(t *testing.T) {
	now := time.Now()
	st := unackedState{Records: []alertedRecord{
		{MessageID: "100", ChannelID: "C1", AlertedAt: now},
	}}
	if _, ok := st.index("C2", "100"); ok {
		t.Fatal("same message id on a different channel must not dedup")
	}
	if _, ok := st.index("C1", "100"); !ok {
		t.Fatal("matching channel+message should dedup")
	}
}

func TestUnackedBackstop_BusyChannelStillAlertsEligibleMessage(t *testing.T) {
	now := time.Date(2026, 7, 2, 14, 0, 0, 0, time.UTC)
	var msgs []transport.Message
	for i := 0; i < 120; i++ {
		msgs = append(msgs, transport.Message{
			ID:        "noise",
			AuthorID:  "other",
			Content:   "channel chatter",
			Timestamp: now.Add(-time.Duration(120-i) * time.Minute),
		})
	}
	msgs[70] = transport.Message{
		ID: "target", AuthorID: "op", Content: "Can you ship the fix?",
		Timestamp: now.Add(-50 * time.Minute),
	}
	var alerts int
	u := NewUnackedBackstop(
		&roster.Config{OperatorUserID: "op", Channels: []roster.Channel{{ChannelID: "C1"}}},
		&fakeRecent{byChannel: map[string][]transport.Message{"C1": msgs}},
		"",
		func(string) { alerts++ },
		nil,
		nil,
	)
	u.now = func() time.Time { return now }
	st := unackedState{}
	if !u.sweepChannel(roster.Channel{ChannelID: "C1"}, &st, now) {
		t.Fatal("eligible operator message must alert even when >DefaultLookback newer messages exist")
	}
	if alerts != 1 {
		t.Fatalf("alerts = %d, want 1", alerts)
	}
}
