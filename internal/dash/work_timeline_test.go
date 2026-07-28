package dash

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/dash/tracker"
	"github.com/jim80net/flotilla/internal/dispatch"
	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/outbox"
)

func TestBuildWorkTimelineDocDeterministicCursorPagination(t *testing.T) {
	var events []WorkTimelineEvent
	for i := 0; i < 45; i++ {
		at := time.Date(2026, 7, 28, 10, i, 0, 0, time.UTC)
		events = append(events, WorkTimelineEvent{
			ID:       "dispatch/event-" + string(rune('a'+i)),
			Kind:     TimelineDispatch,
			State:    "delivered",
			At:       at.Format(time.RFC3339),
			Title:    "bounded fact",
			Source:   "inbound",
			SourceID: "event-" + string(rune('a'+i)),
		})
	}
	build := workTimelineBuild{
		subject: WorkTimelineSubject{Kind: "goal", ID: "alpha", Title: "Alpha", SourceID: "alpha"},
		events:  events,
		sources: []WorkTimelineSource{{ID: "dispatch", Label: "Dispatch", Status: "partial"}},
	}
	first := buildWorkTimelineDoc(build, time.Now(), 20, "")
	if len(first.Events) != 20 || first.NextCursor == "" || first.Total != 45 || !first.Partial {
		t.Fatalf("first page = %+v", first)
	}
	second := buildWorkTimelineDoc(build, time.Now(), 20, first.NextCursor)
	if len(second.Events) != 20 || second.NextCursor == "" || second.Total != 25 {
		t.Fatalf("second page = %+v", second)
	}
	third := buildWorkTimelineDoc(build, time.Now(), 20, second.NextCursor)
	if len(third.Events) != 5 || third.NextCursor != "" || third.Total != 5 {
		t.Fatalf("third page = %+v", third)
	}
	seen := map[string]bool{}
	for _, page := range []WorkTimelineDoc{first, second, third} {
		for _, event := range page.Events {
			if seen[event.ID] {
				t.Fatalf("event %q repeated across cursor pages", event.ID)
			}
			seen[event.ID] = true
		}
	}
	if len(seen) != 45 {
		t.Fatalf("seen = %d, want 45", len(seen))
	}
}

func TestBuildDispatchTimelineKeepsQueuedDeliveredAcknowledgedDistinct(t *testing.T) {
	dir := t.TempDir()
	const nonce = "flotilla-dispatch-1234abcd"
	message := "Advance goal alpha-goal via jim80net/flotilla#891" + inbound.FormatDispatchFooter(nonce)

	outboxPath, err := outbox.Path(dir, "cos")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := outbox.NewStore(outboxPath).Insert(outbox.Entry{
		ID: "outbox-1", Sender: "cos", Recipient: "alpha", Message: message,
		EnqueuedAt: time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "inbound-1", Sender: "cos", Recipient: "alpha", Message: message, Nonce: nonce,
		DeliveredAt: time.Date(2026, 7, 28, 10, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch.Consume(dir, dispatch.ConsumedEntry{
		Nonce: nonce, PayloadHash: dispatch.PayloadHash(message),
		ConsumedAt: time.Date(2026, 7, 28, 10, 2, 0, 0, time.UTC),
		Reason:     dispatch.ReasonDurableAck, Sender: "cos", Recipient: "alpha",
	}); err != nil {
		t.Fatal(err)
	}

	events, source := buildDispatchTimeline(dir, []string{"alpha-goal", "jim80net/flotilla#891"})
	if source.Status != "partial" || !strings.Contains(source.Detail, "compacted") {
		t.Fatalf("dispatch coverage = %+v", source)
	}
	got := map[string]bool{}
	for _, event := range events {
		got[event.State] = true
		if event.SourceID == "" {
			t.Fatalf("event lost native source id: %+v", event)
		}
		if strings.Contains(event.Detail, "Advance goal") {
			t.Fatalf("timeline leaked dispatch body: %+v", event)
		}
	}
	for _, state := range []string{"queued", "delivered", "acknowledged"} {
		if !got[state] {
			t.Errorf("missing distinct %s event: %+v", state, events)
		}
	}
}

func TestTimelineMessageMatchesExactRefsOnly(t *testing.T) {
	message := "Work jim80net/flotilla#891 and https://github.com/jim80net/flotilla/pull/892."
	if !timelineMessageMatches(message, []string{"jim80net/flotilla#891"}) {
		t.Fatal("repository-qualified issue ref did not match")
	}
	if !timelineMessageMatches(message, []string{"jim80net/flotilla#892"}) {
		t.Fatal("canonical pull URL did not match")
	}
	if timelineMessageMatches(message, []string{"jim80net/flotilla#89"}) {
		t.Fatal("partial issue number must not match")
	}
}

func TestTimelinePullEventsKeepOpenedAndMergedDistinct(t *testing.T) {
	events := timelinePullEvents("example/product", tracker.PullRequest{
		Number: 31, Title: "Bounded successor", State: "MERGED",
		CreatedAt: "2026-07-28T09:00:00Z", MergedAt: "2026-07-28T11:00:00Z",
	})
	if len(events) != 2 || events[0].State != "opened" || events[1].State != "merged" {
		t.Fatalf("pull lifecycle collapsed: %+v", events)
	}
}

func TestHandleWorkTimelineSharedGoalAndIssueContract(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	raw := []byte(`{"goals":[{"id":"alpha-goal","title":"Alpha goal","owner":"alpha","work_items":[{"kind":"issue","ref":"jim80net/flotilla#891"},{"kind":"backlog","match":"TIMELINE-GATE","label":"Independent gate"}]}]}`)
	if err := os.WriteFile(srv.cfg.GoalsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srv.cfg.DriveBacklogPath, []byte("## Backlog\n- [awaiting-auth] TIMELINE-GATE operator review\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeTracker{
		issues: []tracker.Issue{{Number: 891, Title: "Timeline issue", State: "OPEN"}},
		issue: tracker.Issue{
			Number: 891, Title: "Timeline issue", State: "OPEN",
			CreatedAt: "2026-07-28T10:00:00Z", UpdatedAt: "2026-07-28T11:00:00Z",
			URL: "https://github.com/jim80net/flotilla/issues/891",
		},
	}
	srv.cfg.Repo = "jim80net/flotilla"
	srv.tracker = fake
	srv.ledgerTrackers = map[string]tracker.Tracker{"jim80net/flotilla": fake}

	for _, path := range []string{
		"/api/work-timeline?goal=alpha-goal",
		"/api/work-timeline?repo=jim80net/flotilla&issue=891",
	} {
		rec := doGet(t, srv, path)
		if rec.Code != 200 {
			t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body.String())
		}
		var doc WorkTimelineDoc
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if doc.Subject.SourceID == "" || len(doc.Events) == 0 {
			t.Fatalf("GET %s lost typed subject/events: %+v", path, doc)
		}
		if strings.Contains(path, "goal=") {
			var gated bool
			for _, event := range doc.Events {
				gated = gated || (event.Kind == TimelineGate && event.State == "gated")
			}
			if !gated {
				t.Fatalf("GET %s collapsed the goal gate lifecycle: %+v", path, doc.Events)
			}
		}
		var github, dispatchCoverage bool
		for _, source := range doc.Sources {
			github = github || source.ID == "github"
			dispatchCoverage = dispatchCoverage || (source.ID == "dispatch" && source.Status == "partial")
		}
		if !github || !dispatchCoverage {
			t.Fatalf("GET %s coverage = %+v", path, doc.Sources)
		}
	}
}

func TestHandleWorkTimelineReturnsPartialWhenGitHubUnavailable(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	fake := &fakeTracker{err: errors.New("generic source failure")}
	srv.cfg.Repo = "jim80net/flotilla"
	srv.tracker = fake
	srv.ledgerTrackers = map[string]tracker.Tracker{"jim80net/flotilla": fake}
	rec := doGet(t, srv, "/api/work-timeline?repo=jim80net/flotilla&issue=891")
	if rec.Code != 200 {
		t.Fatalf("code = %d: %s", rec.Code, rec.Body.String())
	}
	var doc WorkTimelineDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Partial {
		t.Fatalf("partial = false: %+v", doc)
	}
	for _, source := range doc.Sources {
		if source.ID == "github" && source.Status == "unavailable" {
			return
		}
	}
	t.Fatalf("GitHub failure coverage missing: %+v", doc.Sources)
}
