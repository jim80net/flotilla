package doctrine

import (
	"fmt"
	"strings"
	"testing"
)

// memberByName resolves a registry member by name so tests do not depend on slice
// order (the registry now holds more than one member).
func memberByName(t *testing.T, name string) Member {
	t.Helper()
	for _, m := range Members() {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("registry missing member %q", name)
	return Member{}
}

// The registry ships EXACTLY nine members: operating-principles (identity-append),
// the Rule of Three (identity-append), no-self-merge (identity-append),
// act-dont-idle-hold (identity-append), executive-mini-brief (identity-append),
// xo-outbound (identity-append, coordinator-only), operator-direct-tasking
// (identity-append), visibility-synthesis (heartbeat-skill), and parade-formation
// (heartbeat-skill).
// This locks the count so a future member addition is a deliberate, reviewed change
// (and so the member-count-agnostic install loop is exercised against the real
// registry, not a fixture).
func TestMembersRegistryContents(t *testing.T) {
	members := Members()
	if len(members) != 9 {
		t.Fatalf("registry should hold exactly nine members, got %d", len(members))
	}
	byName := map[string]Member{}
	for _, m := range members {
		byName[m.Name] = m
	}

	op, ok := byName["operating-principles"]
	if !ok {
		t.Fatal("registry missing operating-principles member")
	}
	if op.Mechanism != MechanismIdentityAppend {
		t.Errorf("operating-principles mechanism = %q, want %q", op.Mechanism, MechanismIdentityAppend)
	}
	if op.OpenMarker != operatingPrinciplesOpenMarker || op.CloseMarker != operatingPrinciplesCloseMarker {
		t.Errorf("operating-principles markers = open=%q close=%q, want the operating-principles fence", op.OpenMarker, op.CloseMarker)
	}
	if strings.TrimSpace(op.Content) == "" {
		t.Error("operating-principles content is empty — the embed did not round-trip")
	}

	rot, ok := byName["rule-of-three"]
	if !ok {
		t.Fatal("registry missing rule-of-three member")
	}
	if rot.Mechanism != MechanismIdentityAppend {
		t.Errorf("rule-of-three mechanism = %q, want %q", rot.Mechanism, MechanismIdentityAppend)
	}

	nsm, ok := byName["no-self-merge"]
	if !ok {
		t.Fatal("registry missing no-self-merge member")
	}
	if nsm.Mechanism != MechanismIdentityAppend {
		t.Errorf("no-self-merge mechanism = %q, want %q", nsm.Mechanism, MechanismIdentityAppend)
	}
	if nsm.OpenMarker != noSelfMergeOpenMarker || nsm.CloseMarker != noSelfMergeCloseMarker {
		t.Errorf("no-self-merge markers = open=%q close=%q, want the no-self-merge fence", nsm.OpenMarker, nsm.CloseMarker)
	}
	if strings.TrimSpace(nsm.Content) == "" {
		t.Error("no-self-merge content is empty — the embed did not round-trip")
	}

	ad, ok := byName["act-dont-idle-hold"]
	if !ok {
		t.Fatal("registry missing act-dont-idle-hold member")
	}
	if ad.Mechanism != MechanismIdentityAppend {
		t.Errorf("act-dont-idle-hold mechanism = %q, want %q", ad.Mechanism, MechanismIdentityAppend)
	}
	if ad.OpenMarker != actDontIdleHoldOpenMarker || ad.CloseMarker != actDontIdleHoldCloseMarker {
		t.Errorf("act-dont-idle-hold markers = open=%q close=%q, want the act-dont-idle-hold fence", ad.OpenMarker, ad.CloseMarker)
	}
	if strings.TrimSpace(ad.Content) == "" {
		t.Error("act-dont-idle-hold content is empty — the embed did not round-trip")
	}

	emb, ok := byName["executive-mini-brief"]
	if !ok {
		t.Fatal("registry missing executive-mini-brief member")
	}
	if emb.Mechanism != MechanismIdentityAppend {
		t.Errorf("executive-mini-brief mechanism = %q, want %q", emb.Mechanism, MechanismIdentityAppend)
	}
	if emb.OpenMarker != executiveMiniBriefOpenMarker || emb.CloseMarker != executiveMiniBriefCloseMarker {
		t.Errorf("executive-mini-brief markers = open=%q close=%q, want the executive-mini-brief fence", emb.OpenMarker, emb.CloseMarker)
	}
	if strings.TrimSpace(emb.Content) == "" {
		t.Error("executive-mini-brief content is empty — the embed did not round-trip")
	}

	xo, ok := byName["xo-outbound"]
	if !ok {
		t.Fatal("registry missing xo-outbound member")
	}
	if !xo.CoordinatorOnly {
		t.Error("xo-outbound must be coordinator-only")
	}
	if xo.Mechanism != MechanismIdentityAppend {
		t.Errorf("xo-outbound mechanism = %q, want %q", xo.Mechanism, MechanismIdentityAppend)
	}
	if xo.OpenMarker != xoOutboundOpenMarker || xo.CloseMarker != xoOutboundCloseMarker {
		t.Errorf("xo-outbound markers = open=%q close=%q, want the xo-outbound fence", xo.OpenMarker, xo.CloseMarker)
	}
	if !strings.Contains(xo.Content, "flotilla notify") {
		t.Error("xo-outbound content must mention flotilla notify")
	}

	odt, ok := byName["operator-direct-tasking"]
	if !ok {
		t.Fatal("registry missing operator-direct-tasking member")
	}
	if odt.Mechanism != MechanismIdentityAppend {
		t.Errorf("operator-direct-tasking mechanism = %q, want %q", odt.Mechanism, MechanismIdentityAppend)
	}
	if odt.OpenMarker != operatorDirectTaskingOpenMarker || odt.CloseMarker != operatorDirectTaskingCloseMarker {
		t.Errorf("operator-direct-tasking markers = open=%q close=%q, want the operator-direct-tasking fence", odt.OpenMarker, odt.CloseMarker)
	}
	if strings.TrimSpace(odt.Content) == "" {
		t.Error("operator-direct-tasking content is empty — the embed did not round-trip")
	}
	if !strings.Contains(odt.Content, "first-class authorization") {
		t.Error("operator-direct-tasking content must state first-class authorization")
	}

	vs, ok := byName["visibility-synthesis"]
	if !ok {
		t.Fatal("registry missing visibility-synthesis member")
	}
	if vs.Mechanism != MechanismHeartbeatSkill {
		t.Errorf("visibility-synthesis mechanism = %q, want %q", vs.Mechanism, MechanismHeartbeatSkill)
	}
	if vs.TargetFile != "skills/visibility-synthesis.md" {
		t.Errorf("visibility-synthesis TargetFile = %q, want %q", vs.TargetFile, "skills/visibility-synthesis.md")
	}
	if strings.TrimSpace(vs.Content) == "" {
		t.Error("visibility-synthesis content is empty — the embed did not round-trip")
	}
	// A heartbeat-skill member carries no marker fence (whole-file, stat-based).
	if vs.OpenMarker != "" || vs.CloseMarker != "" {
		t.Errorf("heartbeat-skill member should carry no marker fence, got open=%q close=%q", vs.OpenMarker, vs.CloseMarker)
	}

	pf, ok := byName["parade-formation"]
	if !ok {
		t.Fatal("registry missing parade-formation member")
	}
	if pf.Mechanism != MechanismHeartbeatSkill {
		t.Errorf("parade-formation mechanism = %q, want %q", pf.Mechanism, MechanismHeartbeatSkill)
	}
	if pf.TargetFile != "skills/parade-formation.md" {
		t.Errorf("parade-formation TargetFile = %q, want %q", pf.TargetFile, "skills/parade-formation.md")
	}
	if strings.TrimSpace(pf.Content) == "" {
		t.Error("parade-formation content is empty — the embed did not round-trip")
	}
	if !strings.Contains(pf.Content, "## Learnings") {
		t.Error("parade-formation content must document the Learnings block")
	}
	if pf.OpenMarker != "" || pf.CloseMarker != "" {
		t.Errorf("heartbeat-skill member should carry no marker fence, got open=%q close=%q", pf.OpenMarker, pf.CloseMarker)
	}
}

// The embedded content must round-trip from the binary (the //go:embed directive
// guarantees the tree at build time) and carry the marker fence the append-idempotency
// guard keys on, plus the load-bearing-marker note that travels with the block.
func TestRuleOfThreeContentIsEmbeddedAndMarked(t *testing.T) {
	m := memberByName(t, "rule-of-three")
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

// The Rule of Three is a GUIDELINE, not a hard rule (operator directive). Lock the
// reframe so a future edit cannot silently restore hard-rule wording.
func TestRuleOfThreeIsFramedAsGuideline(t *testing.T) {
	m := memberByName(t, "rule-of-three")
	for _, want := range []string{"guideline", "not a hard rule"} {
		if !strings.Contains(strings.ToLower(m.Content), strings.ToLower(want)) {
			t.Errorf("rule-of-three content should frame the rule as a guideline; missing %q", want)
		}
	}
}

// no-self-merge content must round-trip from the binary, carry its marker fence, and
// state the load-bearing message (a desk never merges its own work; the merge is the
// independent review), with the markers + note inside the fence so they travel with the
// appended block.
func TestNoSelfMergeContentIsEmbeddedAndMarked(t *testing.T) {
	m := memberByName(t, "no-self-merge")
	if strings.TrimSpace(m.Content) == "" {
		t.Fatal("no-self-merge content is empty — the embed did not round-trip")
	}
	for _, want := range []string{
		noSelfMergeOpenMarker,
		noSelfMergeCloseMarker,
		"LOAD-BEARING",              // the in-fence marker note
		"do **NOT** merge your own", // the rule itself
		"the merge IS the independent review",
	} {
		if !strings.Contains(m.Content, want) {
			t.Errorf("no-self-merge content missing %q", want)
		}
	}
	open := strings.Index(m.Content, noSelfMergeOpenMarker)
	close := strings.Index(m.Content, noSelfMergeCloseMarker)
	if open < 0 || close < 0 || open >= close {
		t.Fatalf("markers out of order: open=%d close=%d", open, close)
	}
	if !strings.Contains(m.Content[open:close], "LOAD-BEARING") {
		t.Error("load-bearing note is not inside the no-self-merge marker fence")
	}
}

// operating-principles content must round-trip from the binary, carry its marker fence
// and load-bearing note, state the constitution's through-line, and enumerate the eleven
// standing principles — with the markers + note inside the fence so they travel with the
// appended block.
func TestOperatingPrinciplesContentIsEmbeddedAndMarked(t *testing.T) {
	m := memberByName(t, "operating-principles")
	if strings.TrimSpace(m.Content) == "" {
		t.Fatal("operating-principles content is empty — the embed did not round-trip")
	}
	for _, want := range []string{
		operatingPrinciplesOpenMarker,
		operatingPrinciplesCloseMarker,
		"LOAD-BEARING",                  // the in-fence marker note
		"Flotilla Operating Principles", // the constitution title
		"docs/OPERATING-PRINCIPLES.md",  // the full-prose pointer
	} {
		if !strings.Contains(m.Content, want) {
			t.Errorf("operating-principles content missing %q", want)
		}
	}
	for _, want := range []string{
		"Coordinators delegate",
		"preserve bandwidth",
		"flotilla send",
	} {
		if !strings.Contains(m.Content, want) {
			t.Errorf("operating-principles content missing coordinator delegation phrase %q", want)
		}
	}
	for _, want := range []string{
		"Harness allocation",
		"judgment on Claude",
		"execution on grok",
	} {
		if !strings.Contains(m.Content, want) {
			t.Errorf("operating-principles content missing harness-allocation phrase %q", want)
		}
	}
	for _, want := range []string{
		"Desk homes are repo worktrees",
		"git worktree",
		"organic rotation",
	} {
		if !strings.Contains(m.Content, want) {
			t.Errorf("operating-principles content missing worktree-home phrase %q", want)
		}
	}
	// All twelve numbered principles must be present.
	for i := 1; i <= 12; i++ {
		want := fmt.Sprintf("%d. **", i)
		if !strings.Contains(m.Content, want) {
			t.Errorf("operating-principles content missing principle %q", want)
		}
	}
	open := strings.Index(m.Content, operatingPrinciplesOpenMarker)
	close := strings.Index(m.Content, operatingPrinciplesCloseMarker)
	if open < 0 || close < 0 || open >= close {
		t.Fatalf("markers out of order: open=%d close=%d", open, close)
	}
	if !strings.Contains(m.Content[open:close], "LOAD-BEARING") {
		t.Error("load-bearing note is not inside the operating-principles marker fence")
	}
}

func TestExecutiveMiniBriefContent(t *testing.T) {
	m := memberByName(t, "executive-mini-brief")
	if strings.TrimSpace(m.Content) == "" {
		t.Fatal("executive-mini-brief content is empty — the embed did not round-trip")
	}
	for _, want := range []string{
		"Bottom line first",
		"action status",
		"varied",
		"fixed formula",
		"Discord mirror",
		"what they do",
	} {
		if !strings.Contains(m.Content, want) {
			t.Errorf("executive-mini-brief content missing %q", want)
		}
	}
	// The retired fixed-verbatim closer must NOT be mandated as the only all-clear shape.
	if strings.Contains(m.Content, "Always end with exactly one of:") {
		t.Error("executive-mini-brief must not mandate a fixed verbatim closer pair")
	}
	open := strings.Index(m.Content, executiveMiniBriefOpenMarker)
	close := strings.Index(m.Content, executiveMiniBriefCloseMarker)
	if open < 0 || close < 0 || open >= close {
		t.Fatalf("markers out of order: open=%d close=%d", open, close)
	}
	if !strings.Contains(m.Content[open:close], "LOAD-BEARING") {
		t.Error("load-bearing note is not inside the executive-mini-brief marker fence")
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

func TestMembersForAgentOmitsCoordinatorOnlyForExecutionDesk(t *testing.T) {
	exec := MembersForAgent(false)
	for _, m := range exec {
		if m.CoordinatorOnly {
			t.Errorf("execution desk set includes coordinator-only member %q", m.Name)
		}
	}
	if len(exec) != 8 {
		t.Fatalf("execution desk MembersForAgent len = %d, want 8", len(exec))
	}
	coord := MembersForAgent(true)
	if len(coord) != 9 {
		t.Fatalf("coordinator MembersForAgent len = %d, want 9", len(coord))
	}
}

// workspace init stub plus coordinator identity-append members must fit Codex default
// project_doc_max_bytes (32 KiB) with headroom for operator prose.
func TestCoordinatorIdentityAppendBudgetUnder32KiB(t *testing.T) {
	const codexDefaultDocMax = 32 * 1024
	const workspaceStub = "# alpha-xo — desk identity\n\nYou are the alpha-xo desk. Describe this desk's standing role and task here.\n"
	var n int
	for _, m := range MembersForAgent(true) {
		if m.Mechanism != MechanismIdentityAppend {
			continue
		}
		n += len(m.Content)
	}
	total := len(workspaceStub) + n
	if total >= codexDefaultDocMax {
		t.Fatalf("coordinator identity-append total = %d bytes, want < %d", total, codexDefaultDocMax)
	}
}
