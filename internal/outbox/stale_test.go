package outbox

import (
	"strings"
	"testing"
	"time"
)

func TestShouldStaleEscalate_DeferralThreshold(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	e := Entry{
		Sender: "alpha", Recipient: "cos", Deferrals: StaleDeferAt,
		EnqueuedAt: now.Add(-time.Minute),
	}
	if !ShouldStaleEscalate(e, now) {
		t.Fatal("deferrals at threshold should escalate")
	}
	e.LastStaleEscalation = now
	if ShouldStaleEscalate(e, now) {
		t.Fatal("already escalated must not fire again")
	}
}

func TestShouldStaleEscalate_MaxAge(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	e := Entry{
		Sender: "alpha", Recipient: "cos", Deferrals: 1,
		EnqueuedAt: now.Add(-StaleMaxAge - time.Second),
	}
	if !ShouldStaleEscalate(e, now) {
		t.Fatal("max-age exceeded should escalate")
	}
}

func TestStaleEscalationMessage_NamesPartiesAndAge(t *testing.T) {
	enq := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	now := enq.Add(2 * time.Hour)
	e := Entry{Sender: "backend", Recipient: "cos", Deferrals: 3, EnqueuedAt: enq}
	msg := StaleEscalationMessage(e, now)
	for _, want := range []string{`from "backend"`, `to "cos"`, "2h0m0s", "3 deferrals"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q missing %q", msg, want)
		}
	}
}
