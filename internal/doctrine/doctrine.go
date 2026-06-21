// Package doctrine ships flotilla's default constitutional set: the operating
// doctrine a fleet needs to run well, embedded into the binary so it travels with
// the product rather than living as circumstantial host-local assets. A member
// registry drives an idempotent installer (cmd/flotilla doctrine install) and the
// default seed from `workspace init`, so a freshly scaffolded agent is born with
// the doctrine in place.
//
// v1 ships exactly one member — the Rule of Three (span of control) — delivered as
// an identity-append: its distilled text is appended into the agent's standing
// identity file so it loads once at launch via --append-system-prompt-file. The
// registry is member-count-agnostic; adding a member is adding a registry entry
// plus its embedded asset, with no change to the install or seed logic.
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
}

// The Rule-of-Three sentinel fence. These exact strings appear in the embedded
// asset (assets/skills/rule-of-three.md) and are what the install's idempotency
// guard detects — they are load-bearing, mirrored in the asset's in-fence note.
const (
	ruleOfThreeOpenMarker  = "<!-- flotilla:rule-of-three -->"
	ruleOfThreeCloseMarker = "<!-- /flotilla:rule-of-three -->"
)

// members is the v1 registry: exactly one entry. Adding a member is adding an entry
// here plus its embedded asset; the install/seed loop iterates this slice and
// dispatches by Mechanism, so it never needs to change as the set grows.
var members = []Member{
	{
		Name:        "rule-of-three",
		Mechanism:   MechanismIdentityAppend,
		Content:     mustRead("assets/skills/rule-of-three.md"),
		OpenMarker:  ruleOfThreeOpenMarker,
		CloseMarker: ruleOfThreeCloseMarker,
	},
}

// Members returns the constitutional set. The returned slice is a copy so a caller
// cannot mutate the registry.
func Members() []Member {
	out := make([]Member, len(members))
	copy(out, members)
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
