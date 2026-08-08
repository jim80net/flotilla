package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
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
	if len(third) != 1 {
		t.Fatalf("restart with no confirmed delivery fired %d jobs, want 1 durable retry", len(third))
	}
	if third[0].Kind != KindScheduled || third[0].ScheduleOccurrence != occ.Format(time.RFC3339) {
		t.Fatalf("restart retry = %+v, want same durable scheduled occurrence", third[0])
	}
}

func TestScheduledCoordinatorAliasBusySurvivesAndConfirmsOnce(t *testing.T) {
	coordinator := true
	cfg := &roster.Config{
		XOAgent: "cos",
		Agents: []roster.Agent{
			{Name: "cos", Coordinator: &coordinator},
			{Name: "cos-adj", AdjutantFor: "cos"},
		},
	}
	var mu sync.Mutex
	attempts := 0
	receipts := 0
	deferredReceipts := 0
	targets := []string{}
	confirmed := make(chan Job, 2)
	in := NewInjector(func(agent, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		targets = append(targets, agent)
		if attempts == 1 {
			return surface.ErrBusy
		}
		return nil
	}, 4)
	in.SetCoordinatorIngress(NewCoordinatorIngress(cfg))
	in.SetScheduledDeliveryHooks(func(job Job) { confirmed <- job }, nil)
	in.SetScheduledAttemptHooks(func(Job) error {
		mu.Lock()
		receipts++
		mu.Unlock()
		return nil
	}, func(Job, string) error {
		mu.Lock()
		deferredReceipts++
		mu.Unlock()
		return nil
	})
	in.reEnqueue = func(job Job, _ time.Duration) { in.Enqueue(job) }
	in.Start()
	defer in.Stop()
	in.Enqueue(Job{
		Agent: "cos", Message: "morning parade", Kind: KindScheduled,
		ScheduleName: "morning-parade", ScheduleOccurrence: "2026-08-08T12:07:00Z", SchedulePhase: "instruction",
	})
	select {
	case job := <-confirmed:
		if job.Agent != "cos-adj" || job.IntendedRecipient != "cos" {
			t.Fatalf("confirmed route = %+v, want cos→cos-adj alias", job)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduled job did not survive busy recipient")
	}
	select {
	case duplicate := <-confirmed:
		t.Fatalf("duplicate confirmation: %+v", duplicate)
	case <-time.After(20 * time.Millisecond):
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != 2 || receipts != 2 || deferredReceipts != 1 || fmt.Sprint(targets) != "[cos-adj cos-adj]" {
		t.Fatalf("attempts=%d receipts=%d deferred=%d targets=%v, want two receipt-bracketed attempts at aliased adjutant", attempts, receipts, deferredReceipts, targets)
	}
}

func TestCosCeremonyMatrixBusyRedirectArtifactAndMissingAlert(t *testing.T) {
	ceremonies := []struct {
		name     string
		at       string
		artifact string
		window   string
	}{
		{name: "morning-parade", at: "12:07Z", artifact: "state/parades/<date>/facts.md", window: "2h"},
		{name: "evening-walk", at: "03:07Z", artifact: "state/ceremonies/evening-walk/<date>.md", window: "6h"},
		{name: "cos-recursive-retro", at: "15:07Z", artifact: "state/retros/cos-<date>.md", window: "6h"},
		{name: "fleet-productivity-self-audit", at: "16:07Z", artifact: "state/ceremonies/productivity-self-audit/<date>.md", window: "1h"},
	}
	coordinator := true
	cfg := &roster.Config{
		XOAgent: "cos",
		Agents: []roster.Agent{
			{Name: "cos", Coordinator: &coordinator},
			{Name: "cos-adj", AdjutantFor: "cos"},
		},
	}
	ingress := NewCoordinatorIngress(cfg)

	for _, ceremony := range ceremonies {
		t.Run(ceremony.name, func(t *testing.T) {
			t.Run("busy redirect eventually confirms and observes artifact", func(t *testing.T) {
				dir := t.TempDir()
				now := time.Date(2026, 8, 8, 17, 0, 0, 123, time.UTC)
				var attemptsMu sync.Mutex
				var targets []string
				in := NewInjector(func(agent, _ string) error {
					attemptsMu.Lock()
					defer attemptsMu.Unlock()
					targets = append(targets, agent)
					if len(targets) == 1 {
						return surface.ErrBusy
					}
					return nil
				}, 4)
				in.SetCoordinatorIngress(ingress)
				schedule := roster.Schedule{
					Name: ceremony.name, At: ceremony.at, To: "cos", Prompt: "produce ceremony artifact",
					ExpectedArtifact: ceremony.artifact, ProductionWindow: ceremony.window,
				}
				sc := NewScheduler([]roster.Schedule{schedule}, filepath.Join(dir, "schedule-state.json"), dir, in.Enqueue)
				sc.SetOwningCoordinator(func(string) string { return "cos" })
				sc.now = func() time.Time { return now }
				confirmed := make(chan Job, 1)
				in.SetScheduledDeliveryHooks(func(job Job) {
					sc.DeliveryConfirmed(job)
					confirmed <- job
				}, sc.DeliveryFailed)
				in.SetScheduledAttemptHooks(sc.DeliveryAttemptStarted, sc.DeliveryDeferred)
				in.reEnqueue = func(job Job, _ time.Duration) { in.Enqueue(job) }
				in.Start()
				sc.Tick()
				select {
				case delivered := <-confirmed:
					if delivered.Agent != "cos-adj" || delivered.IntendedRecipient != "cos" {
						t.Fatalf("delivered route = %+v, want cos -> cos-adj", delivered)
					}
				case <-time.After(time.Second):
					t.Fatal("ceremony did not survive busy adjutant")
				}
				in.Stop()
				attemptsMu.Lock()
				if fmt.Sprint(targets) != "[cos-adj cos-adj]" {
					t.Fatalf("targets = %v, want busy then delivered to cos-adj", targets)
				}
				attemptsMu.Unlock()

				deliveredState := occurrenceByName(t, LoadScheduleState(sc.statePath), ceremony.name)
				deliveredAt, err := time.Parse(time.RFC3339, deliveredState.DeliveredAt)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(deliveredState.ExpectedArtifact), 0o700); err != nil {
					t.Fatal(err)
				}
				artifactBody := fmt.Sprintf("ceremony: %s\noccurrence: %s\noutcome: produced\n", ceremony.name, deliveredState.Occurrence)
				if err := os.WriteFile(deliveredState.ExpectedArtifact, []byte(artifactBody), 0o600); err != nil {
					t.Fatal(err)
				}
				producedAt := deliveredAt.Add(time.Second)
				if err := os.Chtimes(deliveredState.ExpectedArtifact, producedAt, producedAt); err != nil {
					t.Fatal(err)
				}
				now = deliveredAt.Add(time.Minute)
				sc.Tick()
				observed := occurrenceByName(t, LoadScheduleState(sc.statePath), ceremony.name)
				if observed.ArtifactConfirmedAt == "" || observed.FailureAt != "" {
					t.Fatalf("artifact lifecycle = %+v", observed)
				}
			})

			t.Run("missing artifact alerts owning coordinator", func(t *testing.T) {
				dir := t.TempDir()
				now := time.Date(2026, 8, 8, 17, 0, 0, 456, time.UTC)
				var jobs []Job
				schedule := roster.Schedule{
					Name: ceremony.name, At: ceremony.at, To: "cos", Prompt: "produce ceremony artifact",
					ExpectedArtifact: ceremony.artifact, ProductionWindow: ceremony.window,
				}
				sc := NewScheduler([]roster.Schedule{schedule}, filepath.Join(dir, "schedule-state.json"), dir, func(job Job) { jobs = append(jobs, job) })
				sc.SetOwningCoordinator(func(string) string { return "cos" })
				sc.now = func() time.Time { return now }
				sc.Tick()
				instruction := jobs[0]
				if err := sc.DeliveryAttemptStarted(instruction); err != nil {
					t.Fatal(err)
				}
				sc.DeliveryConfirmed(instruction)
				delivered := occurrenceByName(t, LoadScheduleState(sc.statePath), ceremony.name)
				deadline, err := time.Parse(time.RFC3339, delivered.ArtifactDeadline)
				if err != nil {
					t.Fatal(err)
				}
				var unrelatedPath string
				switch ceremony.name {
				case "evening-walk":
					unrelatedPath = filepath.Join(dir, "state", "parades", "2026-08-08", "facts.md")
				case "fleet-productivity-self-audit":
					unrelatedPath = filepath.Join(dir, "state", "fleet-backlog.md")
				}
				if unrelatedPath != "" {
					if err := os.MkdirAll(filepath.Dir(unrelatedPath), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(unrelatedPath, []byte("unrelated write\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					unrelatedAt := deadline.Add(-time.Second)
					if err := os.Chtimes(unrelatedPath, unrelatedAt, unrelatedAt); err != nil {
						t.Fatal(err)
					}
				}
				jobs = nil
				now = deadline.Add(time.Nanosecond)
				sc.Tick()
				if len(jobs) != 1 || jobs[0].SchedulePhase != "escalation" || !jobs[0].DirectToOwner {
					t.Fatalf("missing-artifact jobs = %+v, want direct-owner escalation", jobs)
				}
				routed := ingress.Apply(jobs[0])
				if len(routed) != 1 || routed[0].Agent != "cos" {
					t.Fatalf("direct-owner escalation was redirected: %+v", routed)
				}
				var alertTarget string
				alertInjector := NewInjector(func(agent, _ string) error {
					alertTarget = agent
					return nil
				}, 1)
				alertInjector.SetScheduledDeliveryHooks(sc.DeliveryConfirmed, sc.DeliveryFailed)
				alertInjector.SetScheduledAttemptHooks(sc.DeliveryAttemptStarted, sc.DeliveryDeferred)
				alertInjector.deliver(routed[0])
				if alertTarget != "cos" {
					t.Fatalf("missing-artifact alert delivered to %q, want owning coordinator cos", alertTarget)
				}
				failed := occurrenceByName(t, LoadScheduleState(sc.statePath), ceremony.name)
				if failed.FailureAt == "" || failed.EscalationEnqueuedAt == "" || failed.EscalatedAt == "" {
					t.Fatalf("durable missing-artifact alert = %+v", failed)
				}
			})
		})
	}
}

func TestSchedulerLifecycleArtifactConfirmation(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	occ := time.Date(2026, 8, 8, 12, 7, 0, 0, time.UTC)
	now := occ.Add(time.Minute)
	var jobs []Job
	enqueueSawPrematureState := false
	sc := NewScheduler([]roster.Schedule{{
		Name: "morning-parade", At: "12:07Z", To: "cos", Prompt: "produce facts",
		ExpectedArtifact: "state/parades/<date>/facts.md", ProductionWindow: "30m",
	}}, statePath, dir, func(job Job) {
		jobs = append(jobs, job)
		for _, state := range LoadScheduleState(statePath).Occurrences {
			enqueueSawPrematureState = enqueueSawPrematureState || state.EnqueuedAt != ""
		}
	})
	sc.now = func() time.Time { return now }
	sc.Tick()
	if len(jobs) != 1 || jobs[0].Kind != KindScheduled {
		t.Fatalf("trigger jobs = %+v", jobs)
	}
	if enqueueSawPrematureState {
		t.Fatal("enqueued_at was persisted before the enqueue callback accepted the job")
	}
	triggered := occurrenceByName(t, LoadScheduleState(statePath), "morning-parade")
	if triggered.TriggeredAt == "" || triggered.EnqueuedAt == "" || triggered.DeliveredAt != "" || triggered.ArtifactConfirmedAt != "" {
		t.Fatalf("trigger lifecycle = %+v", triggered)
	}
	if !strings.HasSuffix(triggered.ExpectedArtifact, "state/parades/2026-08-08/facts.md") {
		t.Fatalf("resolved artifact = %q", triggered.ExpectedArtifact)
	}
	sc.DeliveryConfirmed(jobs[0])
	delivered := occurrenceByName(t, LoadScheduleState(statePath), "morning-parade")
	if delivered.DeliveredAt == "" || delivered.ArtifactDeadline == "" || delivered.ArtifactConfirmedAt != "" {
		t.Fatalf("delivered lifecycle = %+v", delivered)
	}
	if err := os.MkdirAll(filepath.Dir(delivered.ExpectedArtifact), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(delivered.ExpectedArtifact, []byte("facts\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	producedAt := now.Add(30 * time.Second)
	if err := os.Chtimes(delivered.ExpectedArtifact, producedAt, producedAt); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	sc.Tick()
	confirmedState := occurrenceByName(t, LoadScheduleState(statePath), "morning-parade")
	if confirmedState.ArtifactConfirmedAt == "" || confirmedState.FailureAt != "" {
		t.Fatalf("artifact lifecycle = %+v", confirmedState)
	}
}

func TestSchedulerMissingArtifactDurablyEscalatesOwningCoordinator(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	occ := time.Date(2026, 8, 8, 12, 7, 0, 0, time.UTC)
	now := occ.Add(time.Minute)
	var jobs []Job
	sc := NewScheduler([]roster.Schedule{{
		Name: "morning-parade", At: "12:07Z", To: "cos", Prompt: "produce facts",
		ExpectedArtifact: "state/parades/<date>/facts.md", ProductionWindow: "10m",
	}}, statePath, dir, func(job Job) { jobs = append(jobs, job) })
	sc.SetOwningCoordinator(func(string) string { return "cos" })
	sc.now = func() time.Time { return now }
	sc.Tick()
	sc.DeliveryConfirmed(jobs[0])
	delivered := occurrenceByName(t, LoadScheduleState(statePath), "morning-parade")
	if err := os.MkdirAll(filepath.Dir(delivered.ExpectedArtifact), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(delivered.ExpectedArtifact, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Minute)
	sc.Tick()
	if len(jobs) != 2 {
		t.Fatalf("jobs = %+v, want instruction + escalation", jobs)
	}
	escalation := jobs[1]
	if escalation.Agent != "cos" || !escalation.DirectToOwner || escalation.SchedulePhase != "escalation" || !strings.Contains(escalation.Message, "facts.md") {
		t.Fatalf("escalation = %+v", escalation)
	}
	failed := occurrenceByName(t, LoadScheduleState(statePath), "morning-parade")
	if failed.FailureAt == "" || failed.FailureReason == "" || failed.EscalationEnqueuedAt == "" || failed.EscalatedAt != "" {
		t.Fatalf("durable failure = %+v", failed)
	}
	sc.DeliveryConfirmed(escalation)
	if got := occurrenceByName(t, LoadScheduleState(statePath), "morning-parade").EscalatedAt; got == "" {
		t.Fatal("confirmed escalation was not durably recorded")
	}
}

func TestSchedulerRestartDoesNotDuplicateConfirmedDelivery(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	now := time.Date(2026, 8, 8, 12, 8, 0, 0, time.UTC)
	schedules := []roster.Schedule{{Name: "parade", At: "12:07Z", To: "cos", Prompt: "go"}}
	var first []Job
	sc1 := NewScheduler(schedules, statePath, dir, func(job Job) { first = append(first, job) })
	sc1.now = func() time.Time { return now }
	sc1.Tick()
	sc1.DeliveryConfirmed(first[0])
	var restarted []Job
	sc2 := NewScheduler(schedules, statePath, dir, func(job Job) { restarted = append(restarted, job) })
	sc2.now = func() time.Time { return now.Add(time.Minute) }
	sc2.Tick()
	if len(restarted) != 0 {
		t.Fatalf("confirmed occurrence duplicated after restart: %+v", restarted)
	}
}

func TestArtifactConfirmationRejectsStaleAndEmptyFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "facts.md")
	delivered := time.Date(2026, 8, 8, 12, 8, 0, 0, time.UTC)
	if err := os.WriteFile(path, []byte("old facts\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := delivered.Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	deadline := delivered.Add(2 * time.Minute)
	if artifactProducedWithin(path, delivered, deadline) {
		t.Fatal("stale pre-delivery artifact was accepted")
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	newer := delivered.Add(time.Minute)
	if err := os.Chtimes(path, newer, newer); err != nil {
		t.Fatal(err)
	}
	if artifactProducedWithin(path, delivered, deadline) {
		t.Fatal("empty artifact was accepted")
	}
	if err := os.WriteFile(path, []byte("new facts\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, newer, newer); err != nil {
		t.Fatal(err)
	}
	if !artifactProducedWithin(path, delivered, deadline) {
		t.Fatal("non-empty post-delivery artifact was not accepted")
	}
}

func TestArtifactProducedWithinRejectsSameInstantAndAfterDeadline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "facts.md")
	if err := os.WriteFile(path, []byte("facts\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	delivered := time.Date(2026, 8, 8, 12, 8, 0, 123, time.UTC)
	deadline := delivered.Add(time.Hour)
	if err := os.Chtimes(path, delivered, delivered); err != nil {
		t.Fatal(err)
	}
	if artifactProducedWithin(path, delivered, deadline) {
		t.Fatal("same-instant artifact was accepted")
	}
	late := deadline.Add(time.Nanosecond)
	if err := os.Chtimes(path, late, late); err != nil {
		t.Fatal(err)
	}
	if artifactProducedWithin(path, delivered, deadline) {
		t.Fatal("artifact produced after deadline was accepted")
	}
}

func TestSchedulerCorruptStateFailsClosed(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(statePath, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	var jobs []Job
	sc := NewScheduler([]roster.Schedule{{Name: "parade", At: "12:07Z", To: "cos", Prompt: "go"}}, statePath, dir, func(job Job) { jobs = append(jobs, job) })
	sc.now = func() time.Time { return time.Date(2026, 8, 8, 12, 8, 0, 0, time.UTC) }
	sc.Tick()
	if len(jobs) != 0 || !sc.disabled {
		t.Fatalf("corrupt sidecar must disable scheduling: disabled=%v jobs=%+v", sc.disabled, jobs)
	}
}

func TestSchedulerInterruptedAttemptFailsClosedWithoutDuplicate(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	now := time.Date(2026, 8, 8, 12, 8, 0, 500, time.UTC)
	schedules := []roster.Schedule{{Name: "parade", At: "12:07Z", To: "cos", Prompt: "go"}}
	var first []Job
	sc1 := NewScheduler(schedules, statePath, dir, func(job Job) { first = append(first, job) })
	sc1.now = func() time.Time { return now }
	sc1.Tick()
	if err := sc1.DeliveryAttemptStarted(first[0]); err != nil {
		t.Fatal(err)
	}

	var restarted []Job
	sc2 := NewScheduler(schedules, statePath, dir, func(job Job) { restarted = append(restarted, job) })
	sc2.SetOwningCoordinator(func(string) string { return "cos" })
	sc2.now = func() time.Time { return now.Add(time.Minute) }
	sc2.Tick()
	if len(restarted) != 1 || restarted[0].SchedulePhase != "escalation" || !restarted[0].DirectToOwner {
		t.Fatalf("restart jobs = %+v, want one direct escalation and no instruction replay", restarted)
	}
	state := occurrenceByName(t, LoadScheduleState(statePath), "parade")
	if state.DeliveryUncertainAt == "" || !strings.Contains(state.FailureReason, "duplicate") {
		t.Fatalf("uncertain lifecycle = %+v", state)
	}
	if err := sc2.DeliveryAttemptStarted(restarted[0]); err != nil {
		t.Fatal(err)
	}
	var afterEscalationCrash []Job
	sc3 := NewScheduler(schedules, statePath, dir, func(job Job) { afterEscalationCrash = append(afterEscalationCrash, job) })
	sc3.now = func() time.Time { return now.Add(2 * time.Minute) }
	sc3.Tick()
	if len(afterEscalationCrash) != 1 || afterEscalationCrash[0].SchedulePhase != "escalation" || !afterEscalationCrash[0].DirectToOwner {
		t.Fatalf("ambiguous escalation was not durably retried to owner: %+v", afterEscalationCrash)
	}
	state = occurrenceByName(t, LoadScheduleState(statePath), "parade")
	if state.EscalationUncertainAt == "" {
		t.Fatalf("ambiguous escalation not recorded: %+v", state)
	}
}

func TestScheduledAmbiguousSurfaceErrorsNeverReplayInstructionAndRetryEscalation(t *testing.T) {
	for _, deliveryErr := range []error{surface.ErrUnconfirmed, surface.ErrPanelBlocked} {
		t.Run(deliveryErr.Error(), func(t *testing.T) {
			dir := t.TempDir()
			statePath := filepath.Join(dir, "state.json")
			now := time.Date(2026, 8, 8, 12, 8, 0, 700, time.UTC)
			var jobs []Job
			sc := NewScheduler([]roster.Schedule{{Name: "parade", At: "12:07Z", To: "cos", Prompt: "go"}}, statePath, dir, func(job Job) { jobs = append(jobs, job) })
			sc.SetOwningCoordinator(func(string) string { return "cos" })
			sc.now = func() time.Time { return now }
			sc.Tick()
			instruction := jobs[0]

			in := NewInjector(func(string, string) error { return deliveryErr }, 1)
			in.SetScheduledDeliveryHooks(sc.DeliveryConfirmed, sc.DeliveryFailed)
			in.SetScheduledAttemptHooks(sc.DeliveryAttemptStarted, sc.DeliveryDeferred)
			in.deliver(instruction)
			jobs = nil
			now = now.Add(time.Minute)
			sc.Tick()
			if len(jobs) != 1 || jobs[0].SchedulePhase != "escalation" {
				t.Fatalf("after ambiguous instruction error jobs=%+v, want escalation only", jobs)
			}

			escalation := jobs[0]
			in.deliver(escalation)
			jobs = nil
			now = now.Add(time.Minute)
			sc.Tick()
			if len(jobs) != 1 || jobs[0].SchedulePhase != "escalation" || jobs[0].Message != escalation.Message {
				t.Fatalf("after ambiguous escalation error jobs=%+v, want stable-ID escalation retry", jobs)
			}
		})
	}
}

func TestSchedulerUnfinishedPriorOccurrenceDoesNotBlockNextDay(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	now := time.Date(2026, 8, 8, 12, 8, 0, 0, time.UTC)
	schedules := []roster.Schedule{{Name: "parade", At: "12:07Z", To: "cos", Prompt: "go"}}
	var jobs []Job
	sc := NewScheduler(schedules, statePath, dir, func(job Job) { jobs = append(jobs, job) })
	sc.now = func() time.Time { return now }
	sc.Tick()
	now = now.Add(24 * time.Hour)
	jobs = nil
	sc = NewScheduler(schedules, statePath, dir, func(job Job) { jobs = append(jobs, job) })
	sc.now = func() time.Time { return now }
	sc.Tick()
	if len(jobs) != 2 {
		t.Fatalf("jobs = %+v, want prior retry plus independent next-day occurrence", jobs)
	}
	if got := len(LoadScheduleState(statePath).Occurrences); got != 2 {
		t.Fatalf("durable occurrences = %d, want 2", got)
	}
}

func occurrenceByName(t *testing.T, state ScheduleState, name string) ScheduleOccurrenceState {
	t.Helper()
	var matches []ScheduleOccurrenceState
	for _, occurrence := range state.Occurrences {
		if occurrence.ScheduleName == name {
			matches = append(matches, occurrence)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("occurrences for %q = %d, want 1: %+v", name, len(matches), state.Occurrences)
	}
	return matches[0]
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
