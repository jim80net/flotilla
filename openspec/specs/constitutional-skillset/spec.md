# constitutional-skillset Specification

## Purpose
TBD - created by archiving change constitutional-skillset. Update Purpose after archive.
## Requirements
### Requirement: An installable, binary-embedded constitutional doctrine set

The system SHALL ship its default constitutional doctrine as a versioned set of members embedded in
the `flotilla` binary, installable into a per-agent workspace by a single command WITHOUT the
operator writing a hook, a script, or hand-copying prose. Each member SHALL declare its name, its
target file within the workspace, its delivery `Mechanism`, and its content. v1's vocabulary SHALL
be `identity-append` (a STRUCTURAL rule loaded once into the agent's standing identity); the
`Mechanism` vocabulary is extensible per the extensibility requirement below. The set SHALL be
embedded so the binary remains self-contained (no external asset path to configure).

The install SHALL be idempotent, applying its idempotency at the granularity each member's mechanism
requires. v1 ships five `identity-append` members (marker-guarded append into the identity file)
and one `heartbeat-skill` member (whole-file kept/created semantics at its workspace-relative
`TargetFile`). The two granularities apply to disjoint member kinds: an APPEND member (one whose
content is appended into an existing file the workspace already owns, such as the agent's identity
file) SHALL use the content-level marker guard rather than file-existence, because the target file
always already exists (see the marker-guarded-append requirement below); a whole-file member SHALL
use kept/created semantics — a target file already present in the workspace SHALL be KEPT (never
overwritten — the operator may have edited it), a missing target SHALL be CREATED, and each decision
SHALL be reported.

#### Scenario: Installing the constitutional set into a fresh workspace

- **WHEN** the operator runs the doctrine-install command against an agent whose workspace has none of the constitutional members installed
- **THEN** every member is applied to its target (whole-file members written, append members appended) and each is reported as created/appended

#### Scenario: Re-installing never overwrites an operator's edits

- **WHEN** the doctrine-install command runs against a workspace where the members are already installed (possibly operator-edited)
- **THEN** every whole-file member is kept unchanged and reported as kept, every append member detects its marker and is skipped, and only genuinely-missing members are applied

### Requirement: doctrine install supports refresh of drifted identity-append blocks

The doctrine-install command SHALL accept a `--refresh` flag. When `--refresh` is set and an
identity-append member's opening marker is already present, the installer SHALL replace the fenced
region from the opening marker through the closing marker inclusive with the current embedded asset
content when the installed block differs from the asset (trailing-newline differences SHALL NOT
count as drift). When the installed block matches the asset, the install SHALL report a no-op. When
the opening marker is present but the closing marker is absent, the install SHALL fail closed with
an error and SHALL NOT partially rewrite the identity file. When the opening marker is absent,
`--refresh` SHALL append the block (same as a plain install). The command SHALL also accept `--all`
to run install/refresh against every agent in the roster.

#### Scenario: Refresh replaces a drifted fenced block

- **WHEN** `flotilla doctrine install --refresh <agent>` runs against an identity file whose fenced block content differs from the embedded asset
- **THEN** the fenced region is replaced with the current asset and reported as refreshed

#### Scenario: Refresh is a no-op when content is current

- **WHEN** `flotilla doctrine install --refresh <agent>` runs against an identity file whose fenced block already matches the embedded asset
- **THEN** the identity file is unchanged and the member is reported as already installed with reason `content current`

#### Scenario: Missing close marker fails closed

- **WHEN** `flotilla doctrine install --refresh <agent>` runs against an identity file with the opening marker present but the closing marker absent
- **THEN** the command errors without partially rewriting the file

### Requirement: workspace init seeds the constitutional set by default

The system SHALL seed the constitutional set into a workspace by default as part of scaffolding it,
so a freshly initialized workspace is born with the doctrine already in place rather than as a bare
identity placeholder. The seeding SHALL obey the same per-member idempotency as a direct install:
it SHALL NOT overwrite any whole-file member the base scaffold or a prior run created, and it SHALL
append an `identity-append` member's block exactly once (detect-and-skip on its marker thereafter),
so re-running initialization leaves every file unchanged.

#### Scenario: A scaffolded workspace is born with doctrine

- **WHEN** the operator initializes a new agent workspace
- **THEN** the base scaffold files AND the constitutional member are present (the structural rule appended into the identity file under its marker), and re-running the initialization keeps every file unchanged and does not re-append the block

### Requirement: The structural rule loads once into the agent's standing identity

The system SHALL deliver a STRUCTURAL member (one that defines the agent's standing organization,
such as the span-of-control rule) by writing its distilled instruction into the agent's native
identity file, so it loads once at launch into the agent's system prompt via
`--append-system-prompt-file` rather than being re-typed on every heartbeat. The installer SHALL
APPEND the distilled rule to the identity file rather than clobbering the agent's own identity
content. Documentation-only distribution and re-typing a structural rule into every heartbeat SHALL
NOT be the primary home for such a rule; the canonical doctrine document remains the source of truth
the distilled rule is derived from.

#### Scenario: The span-of-control rule reaches the standing identity

- **WHEN** a structural member is installed into an agent's workspace
- **THEN** its distilled instruction is appended to that agent's identity file (without clobbering the agent's own identity), so it loads once at launch

### Requirement: An identity-append member is idempotent via a content-level marker guard

An `identity-append` member SHALL wrap its distilled content in a uniquely-marked block (a sentinel
fence, e.g. `<!-- flotilla:rule-of-three -->` … `<!-- /flotilla:rule-of-three -->`), and the install
SHALL use the presence of that marker — NOT the existence of the target file — as its idempotency
guard. The install SHALL append the marked block exactly ONCE when its opening marker is ABSENT from
the identity file, and SHALL detect the marker and SKIP the append when it is already PRESENT —
preserving any operator edits made inside or around the block. This marker guard is required because
the agent's identity file is always written by `workspace init` (`cmd/flotilla/workspace.go:101`) and
therefore always exists by the time the doctrine is installed, so file-existence kept/created cannot
govern an append into it (it would either never append or double-append on a second install). This
append-once guard is a DISTINCT granularity from the whole-file kept/created discipline: file-create
governs a member that owns its own file (missing → created, present → kept); marker-skip governs the
append of a block into an already-existing shared file (the identity file). The two SHALL apply to
disjoint member kinds and never conflict.

#### Scenario: The structural rule is appended exactly once across repeated installs

- **WHEN** the doctrine is installed, then installed a second time against the same workspace
- **THEN** the first install appends the marked block to the identity file once, and the second install detects the marker and skips the append, so the block appears exactly once

#### Scenario: Operator edits around the marked block survive re-install

- **WHEN** the operator has edited content inside or adjacent to the marked block, and the doctrine is re-installed
- **THEN** the marker is detected, the append is skipped, and the operator's edits are left untouched

### Requirement: The Rule of Three span-of-control doctrine ships as a member

The system SHALL include, as a constitutional member, the Rule of Three: no coordinating seat
manages more than three active charges, and the arrival of a fourth charge forces the creation of
an intermediate lead and a re-clustering, recursively, until every seat manages at most three. The
member SHALL also carry the upward-aggregation discipline (each lead rolls its charges' reports
into one summary upward) and the parallel-not-serial discipline (independent workstreams are
dispatched concurrently, never one-at-a-time). The full doctrine SHALL exist as a documentation
page (the source of truth), and the distilled standing-instruction form SHALL be the installed
member, delivered as an `identity-append` structural rule.

#### Scenario: A coordinating agent receives the span-of-control discipline

- **WHEN** the constitutional set is installed for a coordinating agent
- **THEN** the agent's standing identity carries the ≤3-active-charges rule, the fourth-charge-forces-a-layer mechanic, upward aggregation, and parallel dispatch

### Requirement: executive-mini-brief constitutional member

The doctrine registry SHALL ship an `executive-mini-brief` `identity-append` member whose marked block
defines the operator-facing turn-final format: bottom line first in plain English; 2–5 bullets naming
work streams by what they do; identifiers compressed to an optional detail footer; and an explicit
action-status close (one concrete ask or a varied all-clear — not one fixed verbatim formula every
turn). `flotilla doctrine install` SHALL append the block idempotently (marker-detected skip).

#### Scenario: doctrine install appends mini-brief block

- **WHEN** `flotilla doctrine install <agent>` runs against an identity file lacking the
  `flotilla:executive-mini-brief` opening marker
- **THEN** the installer appends the member's fenced block into the agent's identity file

### Requirement: mirror turn-final audit

The XO Discord mirror hook SHALL log `MINI-BRIEF-AUDIT` when the turn-final's last line lacks an
explicit action-status close (concrete ask or varied all-clear phrasing) and SHALL still post the
text unchanged.

#### Scenario: mirror posts and audits needs-you line

- **WHEN** the hook extracts a non-empty assistant turn-final for the roster XO pane
- **THEN** it posts via `flotilla notify --chunk` and logs audit status for the action-status close

### Requirement: The constitutional set is extensible without enumerating its future contents

The constitutional set SHALL be a member registry such that adding a member is adding a registry
entry plus its embedded asset, with no change to the install or seed logic (the install/seed loop is
member-count-agnostic and dispatches each member by its declared `Mechanism`). v1 SHALL register
exactly six members as enumerated in `internal/doctrine` (`Members()`): `operating-principles`,
`rule-of-three`, `no-self-merge`, `act-dont-idle-hold`, and `executive-mini-brief` (each
`identity-append` into the agent's identity file), plus `visibility-synthesis` (`heartbeat-skill`
written to `skills/visibility-synthesis.md`). The registry shape SHALL be stable while the
`Mechanism` vocabulary remains EXTENSIBLE: v1 uses `identity-append` (structural rules loaded once
at launch) and `heartbeat-skill` (a tick-time whole-file skill); a future member of a new kind
SHALL extend the vocabulary with its own `Mechanism` value plus the write/load behavior that value
implies, designed when that member is. The set SHALL NOT pre-enumerate or hardcode a broader corpus
beyond the shipped registry, NOR pre-specify the install behavior of a mechanism no shipped member
uses; which further behaviors join the default set is an operator decision applied incrementally
through the same seam.

#### Scenario: Adding a member requires no install-logic change

- **WHEN** a new member is added to the registry with its embedded asset
- **THEN** the install and seed paths distribute it with no change to their logic (they iterate the registry and dispatch by mechanism, not a fixed list)

#### Scenario: v1 ships the six registry members

- **WHEN** the constitutional set is enumerated in v1 (`doctrine.Members()`)
- **THEN** it contains exactly the six members named above (five `identity-append`, one
  `heartbeat-skill`), with the seam left open for the operator to add more

