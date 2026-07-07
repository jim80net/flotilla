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

func TestStaleClaimKeyRoundTrip(t *testing.T) {
	key := StaleClaimKey("backend", "abc")
	sender, id, ok := ParseStaleClaimKey(key)
	if !ok || sender != "backend" || id != "abc" {
		t.Fatalf("ParseStaleClaimKey(%q) = %q %q %v", key, sender, id, ok)
	}
}

func TestMarkStaleEscalated_OnlyOnConfirm(t *testing.T) {
	dir := t.TempDir()
	path, _ := Path(dir, "backend")
	NewStore(path).Upsert(Entry{
		ID: "e1", Sender: "backend", Recipient: "cos", Message: "hi",
		EnqueuedAt: time.Now().UTC(),
	})
	if err := MarkStaleEscalated(dir, "backend", "e1"); err != nil {
		t.Fatal(err)
	}
	got := NewStore(path).Load()
	if len(got) != 1 || got[0].LastStaleEscalation.IsZero() {
		t.Fatalf("MarkStaleEscalated must stamp, got %+v", got)
	}
}
