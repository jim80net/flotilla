package inbound

import (
	"strings"
	"testing"
)

func TestAcknowledged_SnippetFallback_RequiresVerbatimLine(t *testing.T) {
	needle := "TRACK C PR: https://github.com/jim80net/memex-hermes/pull/27"
	e := Entry{
		Nonce:   "flotilla-dispatch-abc12345",
		Message: needle,
	}
	// Reader-modeled paraphrase — must NOT ack via fallback (MF3 guard).
	paraphrase := "Surfaced the hermes portable-location PR for gate review."
	if Acknowledged(paraphrase, e) {
		t.Fatal("paraphrased turn-final must not ack without nonce echo")
	}
	// Verbatim distinctive line — fallback may ack when nonce stripped from mirror.
	if !Acknowledged("Done. "+needle, e) {
		t.Fatal("verbatim distinctive line should ack via fallback")
	}
}

func TestAcknowledged_NonceEcho_PrimaryPath(t *testing.T) {
	e := Entry{Nonce: "flotilla-dispatch-deadbeef", Message: "generic task"}
	if !Acknowledged("Shipped; footer flotilla-dispatch-deadbeef", e) {
		t.Fatal("nonce echo is primary ack path")
	}
}

func TestEvaluateFinish_ReinjectNotConfirmed_DoesNotEscalate(t *testing.T) {
	tr := NewTracker()
	e := Entry{
		ID: "d1", Sender: "memex", Recipient: "backend",
		Message: "Implement feature X per spec section 3",
		Nonce:   "flotilla-dispatch-zzzzzzzz",
	}
	tr.Track(e)

	actions := tr.OnFinish("backend", "Synthesis only.")
	if len(actions) != 1 || !actions[0].Reinject {
		t.Fatalf("first miss: %+v", actions)
	}
	if got := tr.Pending("backend"); len(got) != 1 || got[0].Deferrals != 0 {
		t.Fatalf("deferrals before confirmed reinject = %+v, want 0", got)
	}

	// Second miss without MarkReinjectDelivered — busy-dropped reinject must NOT escalate.
	actions = tr.OnFinish("backend", "Still no ack.")
	if len(actions) != 1 || actions[0].Escalate {
		t.Fatalf("unconfirmed reinject: got %+v, want reinject again not escalate", actions)
	}
}

func TestEvaluateFinish_EscalatesOnlyAfterConfirmedReinject(t *testing.T) {
	tr := NewTracker()
	e := Entry{
		ID: "d1", Sender: "memex", Recipient: "backend",
		Message:   "Implement feature X per spec section 3",
		Nonce:     "flotilla-dispatch-zzzzzzzz",
		Deferrals: 1,
	}
	tr.Track(e)

	actions := tr.OnFinish("backend", "Idle without nonce.")
	if len(actions) != 1 || !actions[0].Escalate {
		t.Fatalf("after confirmed reinject: %+v", actions)
	}
}

func TestFormatDispatchFooter_IncludesEchoInstruction(t *testing.T) {
	footer := FormatDispatchFooter("flotilla-dispatch-cafebabe")
	if !strings.Contains(footer, "#472") || !strings.Contains(footer, "flotilla-dispatch-cafebabe") {
		t.Fatalf("footer missing contract: %q", footer)
	}
}

func TestReinjectPreamble_IncludesEchoContract(t *testing.T) {
	p := ReinjectPreamble(Entry{Sender: "memex", Nonce: "flotilla-dispatch-11111111", Message: "task"})
	if !strings.Contains(p, ReinjectEchoReminder) || !strings.Contains(p, "flotilla-dispatch-11111111") {
		t.Fatalf("preamble missing echo contract: %q", p)
	}
}
