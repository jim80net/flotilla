package transport

import "testing"

// The self-mirror guard is the load-bearing security property of the Discord
// extraction: the transport's OWN outbound (audit-mirror) posts must NEVER re-enter
// the relay, or the bus feeds back on itself. The guard moved OUT of relay.Accept and
// INTO the discord Subscribe adapter (selfMirrorGuardAdapter); these tests pin it
// there, author-agnostically.

func TestSelfMirrorGuard_DropsWebhookPost(t *testing.T) {
	var delivered int
	adapter := selfMirrorGuardAdapter(func(string, string, string, string) { delivered++ })
	// A webhook-flagged inbound (the transport's own mirror) must be dropped before
	// the handler — it never reaches the relay.
	adapter("C1", "100", "webhook-123", "someone", "echoed mirror content")
	if delivered != 0 {
		t.Fatalf("a webhook post reached the handler %d time(s); must be dropped (feedback guard)", delivered)
	}
}

// TestSelfMirrorGuard_DropsSelfPostEvenWhenSenderIsOperator is the NEW adversarial
// case the old single relay self-mirror test did NOT cover: a self-post whose author
// id EQUALS the operator id. A sender-equality check would MISS this (it looks like
// an operator message); only the author-AGNOSTIC webhook-id drop catches it. Without
// this property, a mirror post that happened to carry the operator's id would feed
// back into the relay.
func TestSelfMirrorGuard_DropsSelfPostEvenWhenSenderIsOperator(t *testing.T) {
	const operatorID = "111111111111111111"
	var delivered int
	adapter := selfMirrorGuardAdapter(func(string, string, string, string) { delivered++ })
	// webhookID non-empty AND authorID == the operator id: the drop must STILL fire,
	// because it keys on the webhook id alone, not the author.
	adapter("C1", "100", "webhook-123", operatorID, "mirror content carrying the operator id")
	if delivered != 0 {
		t.Fatalf("a self-post with sender==operator reached the handler %d time(s); the author-agnostic guard must drop it", delivered)
	}
}

func TestSelfMirrorGuard_PassesGenuineOperatorMessage(t *testing.T) {
	const operatorID = "111111111111111111"
	var gotOrigin, gotID, gotSender, gotContent string
	var delivered int
	adapter := selfMirrorGuardAdapter(func(origin, id, sender, content string) {
		delivered++
		gotOrigin, gotID, gotSender, gotContent = origin, id, sender, content
	})
	// A genuine (non-webhook) operator message passes through, projected to the
	// 4-field medium-agnostic shape (webhookID folded out).
	adapter("C1", "200", "", operatorID, "status please")
	if delivered != 1 {
		t.Fatalf("a genuine operator message delivered %d time(s), want 1", delivered)
	}
	if gotOrigin != "C1" || gotID != "200" || gotSender != operatorID || gotContent != "status please" {
		t.Errorf("projection = (%q,%q,%q,%q), want (C1,200,%s,status please)", gotOrigin, gotID, gotSender, gotContent, operatorID)
	}
}
