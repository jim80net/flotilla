## Why

flotilla's default fleet behaviors are NOT in the product. The constitutional traits a fleet
needs to run well — beginning with the span-of-control discipline (the Rule of Three) — live only
as circumstantial `~/.claude` assets on the one host that dogfoods flotilla, or as README prose. A
newcomer who drops flotilla into their own project and Discord guild gets the message plumbing and
the clock, but NONE of the operating doctrine that makes the fleet legible: nothing tells their
Executive Officer (XO) agent how to divide its attention. That gap defeats flotilla's second goal —
being a genuine drop-in harness, not a tool that only works on the author's machine.

The per-agent workspace already exists as the natural home for this. `flotilla workspace init`
(`cmd/flotilla/workspace.go:69-124`) scaffolds `~/.flotilla/<agent>/` with the agent's identity
file in its native convention (`CLAUDE.md` for Claude Code, `AGENTS.md` for Grok/Cursor,
`CONVENTIONS.md` for aider — `internal/workspace/workspace.go:58-69`), and that identity loads
into the agent's system prompt at launch via `--append-system-prompt-file`
(`cmd/flotilla/workspace.go:94-100`). But today the identity file is written as a BARE
placeholder with ZERO constitutional doctrine (`cmd/flotilla/workspace.go:101`). The doctrine
prose exists (`docs/xo-doctrine.md`, and the drafted span-of-control content); it is simply never
distributed into a fleet.

This change ships the **installable constitutional skill-set distribution surface** — a versioned
in-repo doctrine tree, embedded in the binary, dropped into a fleet by one idempotent command and
seeded by default from `workspace init` — and populates it with its **first member**: the
**Rule of Three** span-of-control doctrine. The surface is deliberately member-count-agnostic so
the operator can populate the broader corpus incrementally through the same seam.

## Scope split (B1 of a ratified two-change split)

The design-gate trio (`/systems-review` + STORM, 2026-06-21) found a P1 in the originally-paired
SECOND member (a visibility-synthesis skill): it was specified to read "the Tier-1 mirror stream"
from Discord channel history, but flotilla is **SEND-ONLY to Discord** — the gateway is push-only
`MESSAGE_CREATE` (`internal/discord/gateway.go:41-50`), there is no history-fetch primitive anywhere
(grep-clean), and the relay drops inbound webhook posts (`internal/relay/relay.go:18-23`). That
substrate does NOT exist as running code. hydra-ops **ratified a SPLIT (2026-06-21)**:

- **B1 (THIS change):** ship the installable distribution surface + the Rule of Three NOW. This
  closes the circumstantial-asset gap and is independent of any synthesis substrate.
- **B2 (separate, forthcoming change):** the visibility-synthesis member returns to design as its
  own change, **ratified 2026-06-21 to read a LOCAL substrate** (the boats' local transcripts via
  `claudestore`, or a chief-of-staff-style mirror-event ledger — `internal/cos/ledger.go`), NOT
  Discord channel history. B2 is NOT specified here.

## What Changes

- **The installable distribution surface (net-new).** A versioned `assets/skills/` tree in the
  repository, embedded into the `flotilla` binary via `go:embed` (the same self-contained-binary
  pattern as `internal/dash/assets.go:14`), and a new `flotilla doctrine install <agent>`
  subcommand (a sibling of `workspace init`, dispatched from `cmd/flotilla/main.go`). The install
  is idempotent with the SAME kept/created write discipline `workspace init` already uses
  (`cmd/flotilla/workspace.go:111-118`) for whole-file members, and a CONTENT-LEVEL marker guard
  for members that append into an existing file (see below). `workspace init` is extended to SEED
  the constitutional set by default, so a freshly scaffolded workspace is born with the doctrine
  already in place.

- **Two delivery mechanisms WITHIN the set (both supported; v1 exercises the first).** A
  constitutional member declares HOW it loads. A **structural** rule (one that defines the agent's
  standing identity — the Rule of Three is structural: it governs how the agent is organized for
  the whole session) is appended into the agent's identity file and loaded ONCE at launch via
  `--append-system-prompt-file`. A **tick-time** discipline (one that must re-assert on a cadence)
  is delivered as a skill the agent invokes on its heartbeat / continuation wake. v1 ships exactly
  one member, an `identity-append` structural rule; the `heartbeat-skill` mechanism is part of the
  member-mechanism vocabulary the registry supports, so B2 plugs into the same seam without a
  schema change. Neither mechanism is "the" home.

- **Member 1 — the Rule of Three doctrine (content; structural).** The span-of-control invariant
  (no coordinating seat manages more than three active charges; the fourth charge forces a layer)
  plus the upward-aggregation and parallel-dispatch disciplines, authored to `docs/xo-doctrine.md`
  house style (drafted at `.claude/handoffs/DRAFT-rule-of-three.md`). It lands as a doctrine doc
  (`docs/span-of-control.md`, cross-linked from `docs/xo-doctrine.md`) AND as a distilled rule
  asset in the constitutional set, written into the agent identity so it loads once. It is the
  ONLY member v1 ships.

- **Marker-guarded append idempotency (net-new — fixes the trio's B1 P2).** Because
  `workspace init` ALWAYS writes the identity file (`cmd/flotilla/workspace.go:101,107`), the
  identity file ALWAYS exists by the time `doctrine install` runs, so the file-existence
  kept/created model cannot govern an APPEND into it (it would either never append or double-append
  on a second install). An `identity-append` member therefore wraps its distilled rule in a marked
  block — a sentinel fence (`<!-- flotilla:rule-of-three -->` … `<!-- /flotilla:rule-of-three -->`).
  Install appends the block ONCE if its marker is absent from the identity file, and
  DETECTS-and-SKIPS if the marker is already present (operator edits inside or around the block are
  preserved untouched). This is a CONTENT-LEVEL idempotency guard at a different granularity than
  the file-creation kept/created discipline: file-create governs whole-file members (a missing file
  is created, an existing one kept); marker-skip governs the append of a block into an
  already-existing identity file.

- **The extensibility seam (net-new; v1 holds exactly one member).** The constitutional set is a
  registry of members, each carrying its name, target file, delivery mechanism (`identity-append`
  vs `heartbeat-skill`), and embedded content. v1 registers EXACTLY one (the Rule of Three).
  Adding a member is adding a registry entry plus its embedded asset — the seam is clean and the
  install/seed loop is member-count-agnostic. WHICH further behaviors join the set
  (the visibility synthesis of B2, delegation-first, goalkeeper, fleet-rotation,
  narrow-answer-settle, and so on) is the operator's strategic lever, populated incrementally; this
  change does NOT pre-decide or enumerate the broader corpus.

## Rejected alternatives (the primary-home question — surface to hydra-ops at ratification)

- **Documentation-only (prose in `docs/`, no installer) — REJECTED as the primary home.** Prose a
  newcomer must find, read, and hand-copy into their agent's prompt is not a distribution surface;
  it is exactly the circumstantial-asset gap this change closes. The doctrine docs remain (they
  are the source of truth a member is distilled FROM), but the SHIPPED mechanism is the installer
  that puts the rule into the agent's standing identity automatically.

- **Pure heartbeat-injection (re-type every structural rule into every heartbeat) — REJECTED as
  the primary home for a STRUCTURAL rule.** A rule that defines the agent's standing organization
  (the Rule of Three) belongs in the identity loaded ONCE at launch, not re-typed on every
  heartbeat tick: re-injecting a structural invariant every tick burns context, invites drift
  between the injected copy and the canonical text, and conflates "who the agent IS" with "what
  the agent should do THIS tick". Heartbeat-injection remains the correct delivery for a tick-time
  discipline (the forthcoming B2 synthesis member will use it) — it is rejected only as the home
  for structural identity. The design-gate trio confirmed this rationale sound.

## Out of scope

- **Member 2 — visibility synthesis (Tiers 2 and 3).** This is a SEPARATE forthcoming change (B2),
  ratified 2026-06-21 to read a LOCAL substrate (the boats' local transcripts / a chief-of-staff-
  style mirror-event ledger), NOT Discord channel history. This change ships the surface + the
  first member only; B2 is NOT specified here. With B2 out of scope, this change introduces NO new
  roster accessor (no `ChannelsAwareOf`), NO membership-graph acyclicity / loop check, and NO
  synthesis routing or cadence — those belong to B2 and are re-designed around the ratified local
  substrate.
- **The broader constitutional corpus.** Only the one named member ships in v1. The remaining
  behaviors are the operator's lever to add later; this change leaves the seam, not a roadmap.
- **Tier 1 (the mechanical per-desk mirror).** Already shipped (`desk-mirror-tier1`, PR #135); this
  change neither re-specs it nor consumes it (consuming it is B2's concern).
- **Channel/webhook provisioning.** Provisioning channels from the roster is the separate
  `flotilla provision` line.
- **Per-surface load mechanisms beyond Claude Code.** The verify-first probe and the v1 install
  target the Claude Code launch recipe (`--append-system-prompt-file` composed with
  `--remote-control`). Grok/aider identity-file conventions already exist
  (`workspace.IdentityFileName`); wiring their load mechanisms into the installer is a fast-follow
  the seam supports, not v1 scope.

## Impact

- **New capability spec:** `constitutional-skillset`.
- **Affected code (implement phase, after the trio + hydra-ops ratify):** new `assets/skills/`
  embedded tree; new `flotilla doctrine install` subcommand (`cmd/flotilla/`); the seed extension
  to `cmd/flotilla/workspace.go`; the one member asset (Rule-of-Three rule); a new
  `docs/span-of-control.md` doctrine doc. NO change to `internal/roster` (the `ChannelsAwareOf`
  accessor and the acyclicity check were synthesis-routing, now deferred to B2).
- **Verify-first gate (binding, blocks the install build):** a live probe must confirm
  `claude --append-system-prompt-file <sentinel>` COMPOSED with the real launch recipe
  `claude --remote-control <name>` loads the sentinel file's contents into the session's system
  prompt. `claude --help` (2026-06-21) confirms both flags exist with no documented conflict; the
  NEW variable is the composition under `--remote-control`. This is an implement-phase task, not a
  design blocker.
- **Risk:** LOW. The surface is additive: whole-file members use idempotent kept/created writes,
  the identity-append member uses a marker-guarded append-once-detect-and-skip guard, and the set
  is default-seeded — no existing workspace file and no operator edit is overwritten. With the
  synthesis member deferred to B2, this change touches no daemon path and adds no roster routing.
