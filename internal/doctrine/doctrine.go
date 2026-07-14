// Package doctrine ships flotilla's default constitutional set: the operating
// doctrine a fleet needs to run well, embedded into the binary so it travels with
// the product rather than living as circumstantial host-local assets. A member
// registry drives an idempotent installer (cmd/flotilla doctrine install) and the
// default seed from `workspace init`, so a freshly scaffolded agent is born with
// the doctrine in place.
//
// The set ships ten members:
//   - operating-principles — an IDENTITY-APPEND constitution: the twelve standing
//     Flotilla Operating Principles, distilled to one sentence each and appended into
//     the agent's identity file so the constitution loads once at launch. The full
//     prose lives in the repository's docs/OPERATING-PRINCIPLES.md.
//   - the Rule of Three (span of control) — an IDENTITY-APPEND guideline: its distilled
//     text is appended into the agent's standing identity file so it loads once at launch
//     via --append-system-prompt-file.
//   - no-self-merge — an IDENTITY-APPEND rule: a desk never merges its own work; the
//     agent one level above reviews and merges (the merge IS the independent review).
//   - act-dont-idle-hold — an IDENTITY-APPEND rule: execute authorized reversible work;
//     never stall on a non-decision by holding or waiting.
//   - executive-mini-brief — an IDENTITY-APPEND rule: curated operator messages use
//     the four-part mini-brief format (bottom line, plain-language streams, detail
//     footer, explicit needs-you line); routine turn-finals remain dash-only.
//   - xo-outbound — an IDENTITY-APPEND rule (coordinator-only): post operator-facing
//     replies via `flotilla notify`; do not notify on heartbeat ticks or routine plumbing.
//   - operator-direct-tasking — an IDENTITY-APPEND rule: operator-direct tasking is
//     first-class authorization; execute and report to coordinator; coordinators record
//     provenance and support (quality gates still apply to the work).
//   - decision-brief-on-blocked — an IDENTITY-APPEND rule: attach the six-element
//     decision brief when marking an item operator-blocked (#349).
//   - visibility-synthesis — a HEARTBEAT-SKILL: a whole-file curation skill written
//     into the agent's workspace, loaded when the daemon emits a synthesis wake.
//   - parade-formation — a HEARTBEAT-SKILL: a whole-file accomplishments-parade skill
//     written into the agent's workspace, loaded when the operator triggers a parade.
//
// The registry is member-count-agnostic; the install/seed loop dispatches by
// Mechanism. Adding a member is adding a registry entry plus its embedded asset (and,
// for a new mechanism, its dispatch arm in install.go — the mechanism-coupling
// contract).
package doctrine

import (
	"embed"
)

// assetsFS embeds the versioned constitutional asset tree so the `flotilla` binary
// is self-contained (no external asset path to configure) — the same pattern as
// internal/dash/assets.go.
//
//go:embed assets/skills
var assetsFS embed.FS

// Mechanism is HOW a member loads into an agent. The vocabulary is a plain
// string-backed enum, scoped in v1 to exactly what v1 uses — a future member kind
// extends the vocabulary with its own value plus the write/load behavior that value
// implies, designed when that member is. No second arm is pre-baked here.
type Mechanism string

// MechanismIdentityAppend appends a member's distilled, marker-fenced text into the
// agent's native identity file so a STRUCTURAL rule (one that defines the agent's
// standing organization, like the Rule of Three) loads once at launch via
// --append-system-prompt-file, rather than being re-typed on every heartbeat.
const MechanismIdentityAppend Mechanism = "identity-append"

// MechanismHeartbeatSkill writes a member's content as a WHOLE FILE into the agent's
// workspace at the member's TargetFile (a workspace-relative path), rather than
// appending into the identity file. It delivers a TICK-TIME discipline — a skill the
// agent loads when the daemon emits a synthesis wake — NOT a structural identity rule.
// Its install idempotency is STAT-based (a missing file is created via its own write;
// an existing file is kept, so operator edits survive), distinct from the
// marker-fenced identity-append guard. A whole-file member carries no marker fence.
const MechanismHeartbeatSkill Mechanism = "heartbeat-skill"

// Member is one constitutional asset and how it is delivered. An identity-append
// member also carries the sentinel-marker pair its content is fenced with — the
// install keys idempotency on the OPENING marker's presence in the identity file,
// not on file existence (the identity file always already exists by install time).
type Member struct {
	// Name is the member's stable identifier (and its source asset's base name).
	Name string
	// Mechanism is how the member loads into the agent (v1: identity-append).
	Mechanism Mechanism
	// Content is the embedded asset text, read from the binary at startup.
	Content string
	// OpenMarker / CloseMarker fence an identity-append member's block. The install
	// appends the block iff OpenMarker is absent from the identity file, else it
	// detects the marker and skips. Empty for non-append mechanisms.
	OpenMarker  string
	CloseMarker string
	// TargetFile is the workspace-RELATIVE path a whole-file member is written to
	// (e.g. "skills/visibility-synthesis.md"); the install resolves it against the
	// workspace dir. It is EMPTY for identity-append members (which write the identity
	// file, not a workspace-relative target) and REQUIRED for heartbeat-skill members.
	TargetFile string
	// CoordinatorOnly, when true, installs only for coordinator agents (any XO or CoS).
	CoordinatorOnly bool
}

// The operating-principles sentinel fence (same load-bearing role as the pairs below —
// these exact strings appear in assets/skills/operating-principles.md and the install
// keys idempotency on the opening marker's presence).
const (
	operatingPrinciplesOpenMarker  = "<!-- flotilla:operating-principles -->"
	operatingPrinciplesCloseMarker = "<!-- /flotilla:operating-principles -->"
)

// The Rule-of-Three sentinel fence. These exact strings appear in the embedded
// asset (assets/skills/rule-of-three.md) and are what the install's idempotency
// guard detects — they are load-bearing, mirrored in the asset's in-fence note.
const (
	ruleOfThreeOpenMarker  = "<!-- flotilla:rule-of-three -->"
	ruleOfThreeCloseMarker = "<!-- /flotilla:rule-of-three -->"
)

// The no-self-merge sentinel fence (same load-bearing role as the Rule-of-Three pair —
// these exact strings appear in assets/skills/no-self-merge.md and the install keys
// idempotency on the opening marker's presence).
const (
	noSelfMergeOpenMarker  = "<!-- flotilla:no-self-merge -->"
	noSelfMergeCloseMarker = "<!-- /flotilla:no-self-merge -->"
)

// The act-dont-idle-hold sentinel fence (same load-bearing role as the pairs above).
const (
	actDontIdleHoldOpenMarker  = "<!-- flotilla:act-dont-idle-hold -->"
	actDontIdleHoldCloseMarker = "<!-- /flotilla:act-dont-idle-hold -->"
)

// The executive-mini-brief sentinel fence (operator turn-final format).
const (
	executiveMiniBriefOpenMarker  = "<!-- flotilla:executive-mini-brief -->"
	executiveMiniBriefCloseMarker = "<!-- /flotilla:executive-mini-brief -->"
)

// The xo-outbound sentinel fence (coordinator notify doctrine).
const (
	xoOutboundOpenMarker  = "<!-- flotilla:xo-outbound -->"
	xoOutboundCloseMarker = "<!-- /flotilla:xo-outbound -->"
)

// The operator-direct-tasking sentinel fence (operator-direct authorization doctrine).
const (
	operatorDirectTaskingOpenMarker  = "<!-- flotilla:operator-direct-tasking -->"
	operatorDirectTaskingCloseMarker = "<!-- /flotilla:operator-direct-tasking -->"
)

// The decision-brief-on-blocked sentinel fence (#349 item D).
const (
	decisionBriefOnBlockedOpenMarker  = "<!-- flotilla:decision-brief-on-blocked -->"
	decisionBriefOnBlockedCloseMarker = "<!-- /flotilla:decision-brief-on-blocked -->"
)

// members is the registry. Adding a member is adding an entry here plus its embedded
// asset; the install/seed loop iterates this slice and dispatches by Mechanism, so it
// never needs to change as the set grows (a NEW mechanism additionally needs its
// dispatch arm in install.go — the mechanism-coupling contract).
var members = []Member{
	{
		// operating-principles: the twelve standing Flotilla Operating Principles — the
		// constitution every agent runs on — distilled to one sentence each. An
		// identity-append like the other structural rules, because it defines the agent's
		// standing posture ("how the agent operates"), loaded once into its identity at
		// launch. The full prose lives in the repository's docs/OPERATING-PRINCIPLES.md.
		Name:        "operating-principles",
		Mechanism:   MechanismIdentityAppend,
		Content:     mustRead("assets/skills/operating-principles.md"),
		OpenMarker:  operatingPrinciplesOpenMarker,
		CloseMarker: operatingPrinciplesCloseMarker,
	},
	{
		Name:        "rule-of-three",
		Mechanism:   MechanismIdentityAppend,
		Content:     mustRead("assets/skills/rule-of-three.md"),
		OpenMarker:  ruleOfThreeOpenMarker,
		CloseMarker: ruleOfThreeCloseMarker,
	},
	{
		// no-self-merge: a desk never merges its own work — the agent one level above
		// reviews and merges (the merge IS the independent review). An identity-append
		// like the Rule of Three, because it is a STRUCTURAL standing rule ("how the
		// agent operates"), loaded once into the agent's identity, not a tick-time skill.
		Name:        "no-self-merge",
		Mechanism:   MechanismIdentityAppend,
		Content:     mustRead("assets/skills/no-self-merge.md"),
		OpenMarker:  noSelfMergeOpenMarker,
		CloseMarker: noSelfMergeCloseMarker,
	},
	{
		// act-dont-idle-hold: execute authorized reversible work; never stall on a
		// non-decision by holding or waiting. An identity-append structural rule like
		// no-self-merge — loaded once into the agent's identity at launch.
		Name:        "act-dont-idle-hold",
		Mechanism:   MechanismIdentityAppend,
		Content:     mustRead("assets/skills/act-dont-idle-hold.md"),
		OpenMarker:  actDontIdleHoldOpenMarker,
		CloseMarker: actDontIdleHoldCloseMarker,
	},
	{
		// executive-mini-brief: curated operator-facing communications use the
		// four-part mini-brief shape; routine turn-finals remain dash-only.
		Name:        "executive-mini-brief",
		Mechanism:   MechanismIdentityAppend,
		Content:     mustRead("assets/skills/executive-mini-brief.md"),
		OpenMarker:  executiveMiniBriefOpenMarker,
		CloseMarker: executiveMiniBriefCloseMarker,
	},
	{
		// xo-outbound: coordinator-only notify doctrine (distilled from docs/xo-doctrine.md).
		Name:            "xo-outbound",
		Mechanism:       MechanismIdentityAppend,
		Content:         mustRead("assets/skills/xo-outbound.md"),
		OpenMarker:      xoOutboundOpenMarker,
		CloseMarker:     xoOutboundCloseMarker,
		CoordinatorOnly: true,
	},
	{
		// operator-direct-tasking: operator-direct tasking is first-class authorization;
		// execute and report to coordinator; coordinators record provenance and support.
		Name:        "operator-direct-tasking",
		Mechanism:   MechanismIdentityAppend,
		Content:     mustRead("assets/skills/operator-direct-tasking.md"),
		OpenMarker:  operatorDirectTaskingOpenMarker,
		CloseMarker: operatorDirectTaskingCloseMarker,
	},
	{
		// decision-brief-on-blocked: attach the six-element decision brief when marking
		// an item operator-blocked — the dash modal renders it; empty is a defect (#349).
		Name:        "decision-brief-on-blocked",
		Mechanism:   MechanismIdentityAppend,
		Content:     mustRead("assets/skills/decision-brief-on-blocked.md"),
		OpenMarker:  decisionBriefOnBlockedOpenMarker,
		CloseMarker: decisionBriefOnBlockedCloseMarker,
	},
	{
		// visibility-synthesis: a whole-file curation skill (Tiers 2/3 of the
		// stratified-visibility doctrine), invoked when the daemon emits a synthesis
		// wake. Delivered as a heartbeat-skill (workspace file), not an identity-append,
		// because it is a tick-time discipline, not a structural identity rule.
		Name:       "visibility-synthesis",
		Mechanism:  MechanismHeartbeatSkill,
		Content:    mustRead("assets/skills/visibility-synthesis.md"),
		TargetFile: "skills/visibility-synthesis.md",
	},
	{
		// parade-formation: a whole-file accomplishments-parade skill (the celebratory
		// sibling of visibility-synthesis), invoked when the operator runs `flotilla parade`.
		// Delivered as a heartbeat-skill (workspace file), not an identity-append.
		Name:       "parade-formation",
		Mechanism:  MechanismHeartbeatSkill,
		Content:    mustRead("assets/skills/parade-formation.md"),
		TargetFile: "skills/parade-formation.md",
	},
}

// Members returns the constitutional set. The returned slice is a copy so a caller
// cannot mutate the registry.
func Members() []Member {
	out := make([]Member, len(members))
	copy(out, members)
	return out
}

// MembersForAgent returns the constitutional set filtered for the agent's role.
// Coordinator-only members (xo-outbound) are omitted for execution desks.
func MembersForAgent(isCoordinator bool) []Member {
	all := Members()
	if isCoordinator {
		return all
	}
	out := make([]Member, 0, len(all))
	for _, m := range all {
		if m.CoordinatorOnly {
			continue
		}
		out = append(out, m)
	}
	return out
}

// mustRead reads an embedded asset at package-init time. A missing asset is a build
// error in spirit (the //go:embed directive guarantees the tree), so a read failure
// here is unrecoverable and panics rather than shipping an empty member.
func mustRead(path string) string {
	b, err := assetsFS.ReadFile(path)
	if err != nil {
		panic("doctrine: embedded asset missing: " + path + ": " + err.Error())
	}
	return string(b)
}
