package doctrine

import (
	"strings"
	"testing"
)

// v1 ships EXACTLY one member — the Rule of Three. This locks the count so a
// future member addition is a deliberate, reviewed change (and so the
// member-count-agnostic install loop is exercised against the real registry, not
// a fixture).
func TestMembersV1ShipsExactlyOne(t *testing.T) {
	members := Members()
	if len(members) != 1 {
		t.Fatalf("v1 registry should hold exactly one member, got %d", len(members))
	}
	m := members[0]
	if m.Name != "rule-of-three" {
		t.Errorf("member name = %q, want %q", m.Name, "rule-of-three")
	}
	if m.Mechanism != MechanismIdentityAppend {
		t.Errorf("member mechanism = %q, want %q", m.Mechanism, MechanismIdentityAppend)
	}
}

// The embedded content must round-trip from the binary (the //go:embed directive
// guarantees the tree at build time) and carry the marker fence the append-idempotency
// guard keys on, plus the load-bearing-marker note that travels with the block.
func TestRuleOfThreeContentIsEmbeddedAndMarked(t *testing.T) {
	m := Members()[0]
	if strings.TrimSpace(m.Content) == "" {
		t.Fatal("rule-of-three content is empty — the embed did not round-trip")
	}
	for _, want := range []string{
		ruleOfThreeOpenMarker,
		ruleOfThreeCloseMarker,
		"LOAD-BEARING", // the in-fence note explaining why the markers must stay
		"RE-DISPATCH",  // the recurring-fan-out doctrine sentence (task 3.4)
	} {
		if !strings.Contains(m.Content, want) {
			t.Errorf("rule-of-three content missing %q", want)
		}
	}
	// The load-bearing note and the recurring-fan-out sentence must sit BETWEEN the
	// markers (so they travel with the appended block), not outside the fence.
	open := strings.Index(m.Content, ruleOfThreeOpenMarker)
	close := strings.Index(m.Content, ruleOfThreeCloseMarker)
	if open < 0 || close < 0 || open >= close {
		t.Fatalf("markers out of order: open=%d close=%d", open, close)
	}
	inFence := m.Content[open:close]
	if !strings.Contains(inFence, "LOAD-BEARING") {
		t.Error("load-bearing note is not inside the marker fence")
	}
	if !strings.Contains(inFence, "RE-DISPATCH") {
		t.Error("recurring-fan-out sentence is not inside the marker fence")
	}
}

// Every identity-append member must declare a marker pair so the install can guard
// the append. A member that forgot its markers would double-append; catch that at
// the registry level.
func TestIdentityAppendMembersHaveMarkers(t *testing.T) {
	for _, m := range Members() {
		if m.Mechanism != MechanismIdentityAppend {
			continue
		}
		if m.OpenMarker == "" || m.CloseMarker == "" {
			t.Errorf("identity-append member %q has empty marker(s): open=%q close=%q", m.Name, m.OpenMarker, m.CloseMarker)
		}
		if !strings.Contains(m.Content, m.OpenMarker) || !strings.Contains(m.Content, m.CloseMarker) {
			t.Errorf("identity-append member %q content does not contain its declared markers", m.Name)
		}
	}
}
