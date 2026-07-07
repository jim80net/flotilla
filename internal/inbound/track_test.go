package inbound

import (
	"strings"
	"testing"
	"time"
)

func TestInboundAcknowledged_NonceEcho(t *testing.T) {
	e := Entry{Nonce: "flotilla-dispatch-deadbeef", Message: "fix the login bug"}
	if !Acknowledged("Shipped fix; nonce flotilla-dispatch-deadbeef in footer.", e) {
		t.Fatal("want ack when nonce echoed")
	}
	if Acknowledged("Done. No marker.", e) {
		t.Fatal("want no ack without nonce or snippet")
	}
}

func TestInboundOnFinish_ReinjectsOnFirstMiss(t *testing.T) {
	tr := NewTracker()
	e := Entry{
		ID: "d1", Sender: "memex", Recipient: "codex-harness-dev",
		Message:     "TRACK C PR: https://github.com/jim80net/memex-hermes/pull/27",
		Nonce:       "flotilla-dispatch-abc12345",
		DeliveredAt: time.Now().UTC(),
	}
	tr.Track(e)

	actions := tr.OnFinish("codex-harness-dev", "Finished visibility synthesis for parent XO.")
	if len(actions) != 1 || !actions[0].Reinject || actions[0].Escalate {
		t.Fatalf("first miss: got %+v, want reinject only", actions)
	}
	pending := tr.Pending("codex-harness-dev")
	if len(pending) != 1 || pending[0].Deferrals != 0 {
		t.Fatalf("entry pending with deferrals=0 before confirmed reinject: %+v", pending)
	}
}

func TestInboundOnFinish_EscalatesOnSecondMiss(t *testing.T) {
	tr := NewTracker()
	e := Entry{
		ID: "d1", Sender: "memex", Recipient: "backend",
		Message:   "Implement feature X per spec section 3",
		Nonce:     "flotilla-dispatch-zzzzzzzz",
		Deferrals: 1,
	}
	tr.Track(e)

	actions := tr.OnFinish("backend", "Idle — synthesis duty complete.")
	if len(actions) != 1 || !actions[0].Escalate {
		t.Fatalf("second miss: got %+v, want escalate", actions)
	}
	if len(tr.Pending("backend")) != 0 {
		t.Fatal("entry must drop after escalate threshold")
	}
}

func TestInboundOnFinish_ClearsOnAck(t *testing.T) {
	tr := NewTracker()
	e := Entry{
		ID: "d1", Sender: "xo", Recipient: "backend",
		Message: "Implement feature X per spec section 3",
		Nonce:   "flotilla-dispatch-cafebabe",
	}
	tr.Track(e)

	turn := "Implemented feature X; flotilla-dispatch-cafebabe"
	if actions := tr.OnFinish("backend", turn); len(actions) != 0 {
		t.Fatalf("want no action on ack, got %+v", actions)
	}
	if len(tr.Pending("backend")) != 0 {
		t.Fatal("pending must clear on ack")
	}
}

func TestReinjectPreamble_IncludesOriginalBody(t *testing.T) {
	e := Entry{Sender: "memex", Nonce: "flotilla-dispatch-11111111", Message: "do the thing"}
	p := ReinjectPreamble(e)
	if !strings.Contains(p, "dropped-dispatch resume") || !strings.Contains(p, "do the thing") || !strings.Contains(p, ReinjectEchoReminder) {
		t.Fatalf("preamble missing markers: %q", p)
	}
}
