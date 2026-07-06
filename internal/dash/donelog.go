package dash

// #418 goals done-history: the Goals "Realized" cell was a point-in-time snapshot with
// no achieved_at timestamps, so a look-back window ("realized in the last 7 days") had
// nothing to count against. This file is the data layer: an append-only JSONL log of
// OBSERVED roll-up transitions to/from "achieved", written by the dash server — the
// component that computes the roll-up is the only component that can see it transition.
//
// Log shape (one JSON object per line, `<roster-dir>/goals-done.jsonl` by default):
//
//	{"id":"g1","ts":"2026-07-06T12:00:00Z","achieved":true,"title":"..."}   achieve
//	{"id":"g1","ts":"2026-07-07T09:00:00Z","achieved":false}                regress
//	{"id":"g2","ts":"2026-07-06T12:00:00Z","achieved":true,"seed":true,...} see Seed
//
// The read side keeps the LATEST entry per id; a goal's achieved_at is that entry's ts
// while the entry says achieved. Timestamps are observation times: a transition that
// happens while no dash server is running is recorded at the next observation — later
// than the truth, never fabricated earlier.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// DoneEntry is one observed roll-up transition for a goal.
type DoneEntry struct {
	ID       string `json:"id"`
	TS       string `json:"ts"` // RFC3339 UTC observation time
	Achieved bool   `json:"achieved"`
	// Seed marks an achieve entry recorded on the log's FIRST observation of a fleet
	// (the log file did not exist): the goal was already achieved when history began,
	// so its true achieve time is unknown. Windowed counts exclude seeds — counting a
	// years-old achievement as "this week" would fabricate recency.
	Seed  bool   `json:"seed,omitempty"`
	Title string `json:"title,omitempty"` // convenience for log readers; achieve entries only
}

// ReadDoneLog returns the latest entry per goal id, in file (append) order. A missing
// file is an empty history; an unparsable line is skipped (per-row recovery — one bad
// line must not void the whole history).
func ReadDoneLog(path string) (map[string]DoneEntry, error) {
	out := make(map[string]DoneEntry)
	if path == "" {
		return out, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e DoneEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil || e.ID == "" || e.TS == "" {
			continue // per-row recovery: skip the bad line, keep the history
		}
		// A row whose ts is not RFC3339 is as corrupt as unparsable JSON — letting it
		// become a goal's LATEST entry would erase a prior valid stamp and hide the
		// goal from bounded windows downstream (cubic #449 P2).
		if _, err := time.Parse(time.RFC3339, e.TS); err != nil {
			continue
		}
		out[e.ID] = e
	}
	return out, sc.Err()
}

// doneRecorder observes rendered goals documents and appends achieve/regress
// transitions to the log. Safe for concurrent observers (HTTP handlers + the SSE
// poll hook) — the mutex serializes the diff-and-append, so a transition is
// recorded exactly once.
type doneRecorder struct {
	mu      sync.Mutex
	path    string
	loaded  bool
	seeding bool // true for the first observation when the log file did not yet exist
	latest  map[string]DoneEntry
}

func newDoneRecorder(path string) *doneRecorder {
	return &doneRecorder{path: path}
}

// load initializes the in-memory latest map from the log (once). Called under mu.
func (r *doneRecorder) load() {
	if r.loaded {
		return
	}
	r.loaded = true
	if _, err := os.Stat(r.path); os.IsNotExist(err) {
		r.seeding = true // first-ever observation: achieve entries are seeds
	}
	m, err := ReadDoneLog(r.path)
	if err != nil {
		// An unreadable log records against an empty view; appends may then duplicate
		// earlier entries, which the latest-wins read side absorbs harmlessly.
		m = make(map[string]DoneEntry)
	}
	r.latest = m
}

// observe diffs the doc's current roll-ups against the recorded history and appends
// every transition. Returns the appended entries (tests + logging); nil on no-op.
// A not-found doc records nothing — an absent goals file is not "everything regressed".
func (r *doneRecorder) observe(doc GoalsDoc, now time.Time) []DoneEntry {
	if r == nil || r.path == "" || !doc.Found {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	ts := now.UTC().Format(time.RFC3339)
	var appended []DoneEntry
	seen := make(map[string]bool, len(doc.Goals))
	for i := range doc.Goals {
		g := &doc.Goals[i]
		if g.Source == "roster" {
			continue // materialized desk cards are live presence, not authored goals
		}
		seen[g.ID] = true
		last, known := r.latest[g.ID]
		achieved := g.StatusDisplay == "achieved"
		switch {
		case achieved && (!known || !last.Achieved):
			appended = append(appended, DoneEntry{ID: g.ID, TS: ts, Achieved: true, Seed: r.seeding && !known, Title: g.Title})
		case !achieved && known && last.Achieved:
			appended = append(appended, DoneEntry{ID: g.ID, TS: ts, Achieved: false})
		}
	}
	// A goal REMOVED from the goals file keeps its last entry as-is: history is
	// append-only truth about what was observed, and `seen` exists so a future
	// retention policy can prune removed ids — deliberately not done here.
	_ = seen
	r.seeding = false // only the very first observation seeds
	if len(appended) == 0 {
		return nil
	}
	if err := appendDoneEntries(r.path, appended); err != nil {
		// Fail open on the WRITE but not on the MEMORY: keeping latest in sync means
		// we don't re-append forever on a read-only disk; the entry is simply lost,
		// which the next fresh process observes again.
		fmt.Fprintf(os.Stderr, "flotilla dash: goals done-log append failed: %v\n", err)
	}
	for _, e := range appended {
		r.latest[e.ID] = e
	}
	return appended
}

// history returns a copy of the latest-entry-per-goal view for read-model attachment.
func (r *doneRecorder) history() map[string]DoneEntry {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	out := make(map[string]DoneEntry, len(r.latest))
	for k, v := range r.latest {
		out[k] = v
	}
	return out
}

// appendDoneEntries appends one JSON line per entry (O_APPEND — each line well under
// PIPE_BUF, so concurrent same-file writers can't interleave a line).
func appendDoneEntries(path string, entries []DoneEntry) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, e := range entries {
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// AttachDoneHistory stamps achieved_at (+ the seed flag) onto currently-achieved goals
// from the recorded history. Pure — a post-BuildGoals step so the builder stays I/O-free.
// A currently-achieved goal with no history entry stays unstamped (history hasn't
// observed it yet); a regressed goal's stale achieve stamp is never shown.
func AttachDoneHistory(doc *GoalsDoc, hist map[string]DoneEntry) {
	if doc == nil || !doc.Found || len(hist) == 0 {
		return
	}
	for i := range doc.Goals {
		g := &doc.Goals[i]
		if g.StatusDisplay != "achieved" {
			continue
		}
		if e, ok := hist[g.ID]; ok && e.Achieved {
			g.AchievedAt = e.TS
			g.AchievedSeed = e.Seed
		}
	}
}
