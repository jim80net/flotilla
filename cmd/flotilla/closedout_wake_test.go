package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/dispatch"
	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
)

func TestClosedOutSeatsQuarantinedRowsNeverWakeOrWedge(t *testing.T) {
	for _, recipient := range []string{"cos-tech-writer", "cos-ux-designer"} {
		t.Run(recipient, func(t *testing.T) {
			dir := t.TempDir()
			deskDir := filepath.Join(dir, "desks", recipient)
			if err := os.MkdirAll(deskDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(deskDir, "CLOSE-OUT-20260814.md"),
				[]byte("# Close-out\n\n**When:** 2026-08-14T00:00Z\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := &roster.Config{XOAgent: "xo", Agents: []roster.Agent{{Name: "xo"}, {Name: recipient}}}
			now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
			q := dispatch.NewQuarantineRegistry(dir)
			var backlogBody strings.Builder
			backlogBody.WriteString("## Backlog\n")
			for i := 0; i < 12; i++ {
				row := inbound.Entry{ID: fmt.Sprintf("synthetic-row-%02d", i), Sender: "xo", Recipient: recipient,
					Message: fmt.Sprintf("synthetic payload %02d", i), Nonce: fmt.Sprintf("flotilla-dispatch-%08x", i),
					DeliveredAt: now.Add(-time.Hour)}
				if err := inbound.Record(dir, row); err != nil {
					t.Fatal(err)
				}
				if inserted, err := q.Quarantine(dispatch.QuarantineEntry{Kind: "inbound-ack", RowID: row.ID,
					Nonce: row.Nonce, Sender: row.Sender, Recipient: recipient, QuarantinedAt: now.Add(-time.Hour)}); err != nil || !inserted {
					t.Fatalf("quarantine %d = (%v,%v)", i, inserted, err)
				}
				fmt.Fprintf(&backlogBody, "- [in-flight] held dispatch %s\n", dispatch.QuarantineBacklogToken(row.Nonce))
			}
			backlogPath := filepath.Join(dir, "flotilla-"+recipient+"-backlog.md")
			if err := os.WriteFile(backlogPath, []byte(backlogBody.String()), 0o600); err != nil {
				t.Fatal(err)
			}

			closedOut := func(agent string) bool { return recipientClosedOut(dir, cfg, agent) }
			delivered := 0
			injector := watch.NewInjector(func(string, string) error { delivered++; return nil }, 8)
			injector.SetRecipientClosedOut(closedOut)
			injector.Start()
			defer injector.Stop()
			warrant := deskWarrantedGateDynamic(func() *roster.Config { return cfg },
				func(agent string) ([]byte, bool, error) {
					raw, err := os.ReadFile(filepath.Join(dir, "flotilla-"+agent+"-backlog.md"))
					return raw, err == nil, err
				}, func(agent string) string { return filepath.Join(dir, "flotilla-"+agent+"-backlog.md") },
				func(string, string) {}, func() time.Time { return now },
				func(agent string, st backlog.Status) (backlog.Status, error) {
					return dispatch.ExcludeQuarantinedInboundWork(dir, agent, st)
				})
			if got := warrant(recipient); got != watch.DeskHeartbeatNotWarranted {
				t.Fatalf("quarantined queue remained actionable: warrant=%v", got)
			}
			wedged := 0
			det := watch.NewDetector(watch.DetectorConfig{
				XOAgent: "xo", Desks: []string{"xo", recipient}, Interval: time.Minute,
				Assess: func(string) surface.State { return surface.StateIdle }, AckAge: func() time.Duration { return 0 },
				Wake: func(watch.WakeKind, []string) {}, Persist: func(watch.Snapshot) error { return nil },
				HeartbeatEnabled: func(agent string) bool { return agent == recipient }, RecipientClosedOut: closedOut,
				HeartbeatWarranted: warrant, HeartbeatLiveState: func(string) surface.State { return surface.StateIdle },
				WakeDeskHeartbeat: func(agent string) {
					injector.Enqueue(watch.Job{Agent: agent, Kind: watch.KindDetector, Message: strings.Repeat("D", 1591)})
				},
				DeskEscalate: func(string) { wedged++ }, DeskHeartbeatEveryTicks: 1, DeskHeartbeatCap: 3,
				Now: func() time.Time { return now },
			}, filepath.Join(dir, "detector.json"))
			for i := 0; i < 6; i++ {
				det.Tick()
				now = now.Add(time.Minute)
			}
			// Exercise the independent detector-send guard too: even a caller that tries to enqueue
			// the exact live-shaped wake cannot reach the held pane.
			injector.Enqueue(watch.Job{Agent: recipient, Kind: watch.KindDetector, Message: strings.Repeat("D", 1591)})
			injector.Stop()
			if delivered != 0 || wedged != 0 {
				t.Fatalf("closed-out seat delivered=%d wedged=%d, want zero", delivered, wedged)
			}
			for _, e := range q.Load() {
				if !e.ReopenedAt.IsZero() {
					t.Fatalf("closed-out control reopened quarantine: %+v", e)
				}
			}
			path, _ := inbound.Path(dir, recipient)
			if rows := inbound.NewStore(path).Load(); len(rows) != 12 {
				t.Fatalf("source rows mutated: got %d", len(rows))
			}
			if consumed := dispatch.NewRegistry(dir).Load(); len(consumed) != 0 {
				t.Fatalf("quarantine fabricated ack: %+v", consumed)
			}

			// Explicit restoration alone is not the reopen edge: active quarantine keeps internal
			// wakes held until eligible real work is confirmed.
			restored := false
			cfg.Agents[1].ClosedOut = &restored
			preWork := watch.NewInjector(func(string, string) error { delivered++; return nil }, 1)
			preWork.SetRecipientClosedOut(func(agent string) bool { return recipientRoutingHeld(dir, cfg, agent) })
			preWork.Start()
			preWork.Enqueue(watch.Job{Agent: recipient, Kind: watch.KindDetector, Message: strings.Repeat("D", 1591)})
			preWork.Stop()
			if delivered != 0 {
				t.Fatalf("explicit restore resumed detector before eligible work edge: delivered=%d", delivered)
			}
			work := watch.NewInjector(func(string, string) error { delivered++; return nil }, 1)
			work.SetRecipientClosedOut(func(agent string) bool { return recipientRoutingHeld(dir, cfg, agent) })
			work.SetTurnConfirmed(func(agent string) { reopenRecipientQuarantine(dir, cfg, agent, now) })
			work.Start()
			work.Enqueue(watch.Job{Agent: recipient, Kind: watch.KindOperatorInterrupt, Message: "eligible restored work"})
			work.Stop()
			if active, err := q.HasActiveRecipient(recipient); err != nil || active {
				t.Fatalf("eligible restored work did not lift routing hold: active=%v err=%v", active, err)
			}
			resumed := watch.NewInjector(func(string, string) error { delivered++; return nil }, 1)
			resumed.SetRecipientClosedOut(func(agent string) bool { return recipientRoutingHeld(dir, cfg, agent) })
			resumed.Start()
			resumed.Enqueue(watch.Job{Agent: recipient, Kind: watch.KindDetector, Message: "normal resumed detector"})
			resumed.Stop()
			if delivered != 2 { // eligible work + post-edge detector
				t.Fatalf("post-restore delivery sequence=%d, want eligible work then detector", delivered)
			}
		})
	}
}
