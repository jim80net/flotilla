package outbox

import (
	"strings"
	"testing"
	"time"
)

func TestShouldStaleEscalate_WorkingSuppressesDeferralArm500(t *testing.T) {
	// Live #500 shape: ~6 deferrals / ~1m while recipient is mid-turn Working.
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	e := Entry{
		Sender: "alpha", Recipient: "backend", Deferrals: 6,
		EnqueuedAt: now.Add(-time.Minute),
	}
	if ShouldStaleEscalate(e, now, RecipientWorking) {
		t.Fatal("Working + 6 deferrals + 1m must NOT escalate (deferral arm suppressed)")
	}
	// Even at the old StaleDeferAt=6 recalibrated floor, Working stays quiet until max age.
	e.Deferrals = StaleDeferAt
	if ShouldStaleEscalate(e, now, RecipientWorking) {
		t.Fatal("Working must not escalate on deferral count alone even at StaleDeferAt")
	}
	// Age arm still fires for long-stuck Working (honest busy alert, not cry-wolf at 1m).
	e.EnqueuedAt = now.Add(-StaleMaxAge - time.Second)
	e.Deferrals = 6
	if !ShouldStaleEscalate(e, now, RecipientWorking) {
		t.Fatal("Working past StaleMaxAge should escalate once (age arm)")
	}
	e.LastStaleEscalation = now
	if ShouldStaleEscalate(e, now, RecipientWorking) {
		t.Fatal("already escalated must not fire again")
	}
}

func TestShouldStaleEscalate_WedgeEscalatesFast500(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	e := Entry{
		Sender: "alpha", Recipient: "backend", Deferrals: StaleDeferAtWedge,
		EnqueuedAt: now.Add(-10 * time.Second),
	}
	if !ShouldStaleEscalate(e, now, RecipientWedge) {
		t.Fatal("wedge at StaleDeferAtWedge should escalate fast")
	}
	e2 := Entry{
		Sender: "alpha", Recipient: "backend", Deferrals: 1,
		EnqueuedAt: now.Add(-StaleMaxAgeWedge - time.Second),
	}
	if !ShouldStaleEscalate(e2, now, RecipientWedge) {
		t.Fatal("wedge past StaleMaxAgeWedge should escalate")
	}
	// Below both arms — quiet.
	e3 := Entry{
		Sender: "alpha", Recipient: "backend", Deferrals: 1,
		EnqueuedAt: now.Add(-10 * time.Second),
	}
	if ShouldStaleEscalate(e3, now, RecipientWedge) {
		t.Fatal("wedge below thresholds must not escalate")
	}
}

func TestShouldStaleEscalate_TransientDeferralThreshold500(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	e := Entry{
		Sender: "alpha", Recipient: "cos", Deferrals: StaleDeferAt,
		EnqueuedAt: now.Add(-time.Minute),
	}
	if !ShouldStaleEscalate(e, now, RecipientTransient) {
		t.Fatal("transient at StaleDeferAt should escalate")
	}
	// Old cry-wolf count (6) must NOT fire under recalibrated threshold.
	e.Deferrals = 6
	if ShouldStaleEscalate(e, now, RecipientUnknown) {
		t.Fatal("6 deferrals must not escalate after #500 recalibration (StaleDeferAt=90)")
	}
	e.LastStaleEscalation = now
	e.Deferrals = StaleDeferAt
	if ShouldStaleEscalate(e, now, RecipientTransient) {
		t.Fatal("already escalated must not fire again")
	}
}

func TestShouldStaleEscalate_MaxAge(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	e := Entry{
		Sender: "alpha", Recipient: "cos", Deferrals: 1,
		EnqueuedAt: now.Add(-StaleMaxAge - time.Second),
	}
	if !ShouldStaleEscalate(e, now, RecipientUnknown) {
		t.Fatal("max-age exceeded should escalate")
	}
}

func TestStaleEscalationMessage_WorkingHonest500(t *testing.T) {
	enq := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	now := enq.Add(2 * time.Hour)
	e := Entry{Sender: "backend", Recipient: "build-desk", Deferrals: 10, EnqueuedAt: enq}
	msg := StaleEscalationMessage(e, now, RecipientWorking)
	for _, want := range []string{`from "backend"`, `to "build-desk"`, "2h0m0s", "10 deferrals", "busy", "mid-turn"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("Working message %q missing %q", msg, want)
		}
	}
	for _, bad := range []string{"wedged", "input-blocked"} {
		if strings.Contains(msg, bad) {
			t.Fatalf("Working message must not claim %q: %q", bad, msg)
		}
	}
}

func TestStaleEscalationMessage_WedgeNamesParties(t *testing.T) {
	enq := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	now := enq.Add(2 * time.Hour)
	e := Entry{Sender: "backend", Recipient: "cos", Deferrals: 3, EnqueuedAt: enq}
	msg := StaleEscalationMessage(e, now, RecipientWedge)
	for _, want := range []string{`from "backend"`, `to "cos"`, "2h0m0s", "3 deferrals", "wedged"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("wedge message %q missing %q", msg, want)
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
	if _, _, err := NewStore(path).Insert(Entry{
		ID: "e1", Sender: "backend", Recipient: "cos", Message: "hi",
		EnqueuedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := MarkStaleEscalated(dir, "backend", "e1"); err != nil {
		t.Fatal(err)
	}
	got := NewStore(path).Load()
	if len(got) != 1 || got[0].LastStaleEscalation.IsZero() {
		t.Fatalf("MarkStaleEscalated must stamp, got %+v", got)
	}
}
