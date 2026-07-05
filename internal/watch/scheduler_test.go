package watch

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
)

func TestScheduleStateSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-schedule-state.json")
	want := ScheduleState{LastFired: map[string]string{"parade": "2026-07-05T12:07:00Z"}}
	if err := want.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := LoadScheduleState(path)
	if got.LastFired["parade"] != want.LastFired["parade"] {
		t.Errorf("LastFired = %#v, want %#v", got.LastFired, want.LastFired)
	}
}

func TestSchedulerNoDoubleFire(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	loc := time.UTC
	occ := time.Date(2026, 7, 5, 12, 7, 0, 0, loc)
	now := occ.Add(2 * time.Minute)

	var mu sync.Mutex
	var jobs []Job
	enqueue := func(j Job) {
		mu.Lock()
		jobs = append(jobs, j)
		mu.Unlock()
	}
	sc := NewScheduler([]roster.Schedule{{
		Name: "parade", At: "12:07Z", To: "xo", Prompt: "run parade",
	}}, statePath, dir, enqueue)
	sc.now = func() time.Time { return now }

	sc.Tick()
	sc.Tick()

	mu.Lock()
	n := len(jobs)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("enqueue count = %d, want 1 (no double-fire)", n)
	}
	st := LoadScheduleState(statePath)
	if st.LastFired["parade"] != occ.Format(time.RFC3339) {
		t.Errorf("last_fired = %q, want %s", st.LastFired["parade"], occ.Format(time.RFC3339))
	}
}

func TestSchedulerCatchUpLateMarker(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	loc := time.UTC
	occ := time.Date(2026, 7, 5, 12, 7, 0, 0, loc)
	now := occ.Add(10 * time.Minute) // well past scheduleLateGrace

	var body string
	sc := NewScheduler([]roster.Schedule{{
		Name: "walk", At: "12:07Z", To: "xo", Prompt: "dispatch walk",
	}}, statePath, dir, func(j Job) { body = j.Message })
	sc.now = func() time.Time { return now }

	sc.CatchUp()
	if !strings.HasPrefix(body, "[schedule late: walk due ") {
		t.Fatalf("body missing late prefix: %q", body)
	}
	if !strings.Contains(body, "dispatch walk") {
		t.Errorf("body = %q, want prompt appended after late prefix", body)
	}
}

func TestSchedulerOnTimeNoLateMarker(t *testing.T) {
	dir := t.TempDir()
	loc := time.UTC
	occ := time.Date(2026, 7, 5, 12, 7, 0, 0, loc)
	now := occ.Add(30 * time.Second)

	var body string
	sc := NewScheduler([]roster.Schedule{{
		Name: "parade", At: "12:07Z", To: "xo", Prompt: "go",
	}}, filepath.Join(dir, "state.json"), dir, func(j Job) { body = j.Message })
	sc.now = func() time.Time { return now }
	sc.Tick()
	if strings.HasPrefix(body, "[schedule late:") {
		t.Errorf("on-time fire must not have late prefix: %q", body)
	}
}

func TestSchedulerMissedTickAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	loc := time.UTC
	occ := time.Date(2026, 7, 5, 12, 7, 0, 0, loc)
	downUntil := occ.Add(15 * time.Minute)

	schedules := []roster.Schedule{{Name: "parade", At: "12:07Z", To: "xo", Prompt: "parade body"}}
	var first []Job
	sc1 := NewScheduler(schedules, statePath, dir, func(j Job) { first = append(first, j) })
	sc1.now = func() time.Time { return occ.Add(-2 * time.Hour) } // first boot, before today's slot
	sc1.Tick()
	if len(first) != 0 {
		t.Fatalf("first-boot pre-slot tick fired %d jobs, want 0", len(first))
	}

	var second []Job
	sc2 := NewScheduler(schedules, statePath, dir, func(j Job) { second = append(second, j) })
	sc2.now = func() time.Time { return downUntil }
	sc2.CatchUp()
	if len(second) != 1 {
		t.Fatalf("restart catch-up jobs = %d, want 1", len(second))
	}
	if !strings.HasPrefix(second[0].Message, "[schedule late:") {
		t.Errorf("missed slot must be late-marked: %q", second[0].Message)
	}

	var third []Job
	sc3 := NewScheduler(schedules, statePath, dir, func(j Job) { third = append(third, j) })
	sc3.now = func() time.Time { return downUntil.Add(time.Minute) }
	sc3.Tick()
	if len(third) != 0 {
		t.Fatalf("after catch-up, second tick fired %d jobs, want 0", len(third))
	}
}

func TestSchedulerPromptFilePreferred(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompts", "parade.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	want := "BUILD and DELIVER parade"
	if err := os.WriteFile(promptPath, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}

	var body string
	sc := NewScheduler([]roster.Schedule{{
		Name: "parade", At: "12:07Z", To: "xo", Prompt: "prompts/parade.md",
	}}, filepath.Join(dir, "state.json"), dir, func(j Job) { body = j.Message })
	sc.now = func() time.Time {
		return time.Date(2026, 7, 5, 12, 8, 0, 0, time.UTC)
	}
	sc.Tick()
	if body != want && !strings.HasSuffix(body, want) {
		// late prefix may be present depending on timing; file content must appear
		if !strings.Contains(body, want) {
			t.Errorf("body = %q, want file contents %q", body, want)
		}
	}
}

func TestDueOccurrenceTimezone(t *testing.T) {
	loc := time.FixedZone("UTC-5", -5*3600)
	// 12:07 in UTC-5 = 17:07 UTC
	now := time.Date(2026, 7, 5, 17, 10, 0, 0, time.UTC)
	occ, ok := dueOccurrence(now, 12, 7, loc, time.Time{})
	if !ok {
		t.Fatal("expected due occurrence")
	}
	if occ.Hour() != 12 || occ.Minute() != 7 {
		t.Errorf("occurrence in loc = %s, want 12:07 in UTC-5", occ.Format(time.RFC3339))
	}
	if !occ.Equal(time.Date(2026, 7, 5, 12, 7, 0, 0, loc)) {
		t.Errorf("occ = %v", occ)
	}
}

func TestParseDailyAtRoster(t *testing.T) {
	cases := []struct {
		at      string
		wantErr bool
	}{
		{"12:07Z", false},
		{"03:07+00:00", false},
		{"09:30-05:00", false},
		{"12:07", true},
		{"25:00Z", true},
		{"12:60Z", true},
		{"12:07EST", true},
	}
	for _, tc := range cases {
		_, _, _, err := roster.ParseDailyAt(tc.at)
		if tc.wantErr && err == nil {
			t.Errorf("ParseDailyAt(%q) = nil, want error", tc.at)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("ParseDailyAt(%q) = %v, want nil", tc.at, err)
		}
	}
}
