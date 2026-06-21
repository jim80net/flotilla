## Why

flotilla's default fleet behaviors are NOT in the product. The constitutional traits a fleet
needs to run well — the span-of-control discipline (the Rule of Three), the up-hierarchy
visibility synthesis — live only as circumstantial `~/.claude` assets on the one host that
dogfoods flotilla, or as README prose. A newcomer who drops flotilla into their own project and
Discord guild gets the message plumbing and the clock, but NONE of the operating doctrine that
makes the fleet legible: nothing tells their Executive Officer (XO) agent how to divide its
attention, and nothing rolls each desk's activity up the hierarchy into a readable picture. That
gap defeats flotilla's second goal — being a genuine drop-in harness, not a tool that only works
on the author's machine.

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
seeded by default from `workspace init` — and populates it with its **first two members**: the
**Rule of Three** doctrine and the **visibility-synthesis skill** (Tiers 2 and 3 of the
stratified visibility design). Tier 1 — the mechanical per-desk mirror — already shipped
(`desk-mirror-tier1`, PR #135, first live post verified); this change builds the synthesis tiers
ON its mirror stream as substrate.

## What Changes

- **The installable distribution surface (net-new).** A versioned `assets/skills/` tree in the
  repository, embedded into the `flotilla` binary via `go:embed` (the same self-contained-binary
  pattern as `internal/dash/assets.go:14`), and a new `flotilla doctrine install <agent>`
  subcommand (a sibling of `workspace init`, dispatched from `cmd/flotilla/main.go`). The install
  is idempotent with the SAME kept/created write discipline `workspace init` already uses
  (`cmd/flotilla/workspace.go:111-118`): a member that already exists in the target workspace is
  KEPT (never overwritten — the operator may have edited it), a missing member is CREATED, and
  each decision prints `kept`/`created`. `workspace init` is extended to SEED the constitutional
  set by default, so a freshly scaffolded workspace is born with the doctrine already in place.

- **Delivery mechanisms WITHIN the set (both, not competing).** A constitutional member declares
  HOW it loads. A **structural** rule (one that defines the agent's standing identity — the Rule
  of Three is structural: it governs how the agent is organized for the whole session) is written
  into the agent's identity file and loaded ONCE at launch via `--append-system-prompt-file`. A
  **tick-time** discipline (one that must re-assert on a cadence — the visibility synthesis is
  tick-time: it runs on the heartbeat) is delivered as a skill the agent invokes on its
  heartbeat / continuation wake. Both mechanisms ship inside the one set; neither is "the" home.

- **Member 1 — the Rule of Three doctrine (content; structural).** The span-of-control invariant
  (no coordinating seat manages more than three active charges; the fourth charge forces a layer)
  plus the upward-aggregation and parallel-dispatch disciplines, authored to `docs/xo-doctrine.md`
  house style (drafted at `.claude/handoffs/DRAFT-rule-of-three.md`). It lands as a doctrine doc
  (`docs/span-of-control.md`, cross-linked from `docs/xo-doctrine.md`) AND as a distilled rule
  asset in the constitutional set, written into the agent identity so it loads once.

- **Member 2 — the visibility-synthesis skill (Tiers 2 and 3; tick-time).** An XO behavior, run
  on its heartbeat, that synthesizes the tier below it UP to its own channel: a project-XO curates
  its boats' activity up to its project channel (Tier 2); the meta-XO curates the project channels
  up to the fleet-command channel (Tier 3). This is LLM curation, not daemon code (it mirrors the
  chief-of-staff substrate/integrator split — `internal/cos/ledger.go`: a deterministic substrate
  below, an integrating skill one level up). The skill reads the **Tier-1 mirror stream** (the
  boat-channel posts `desk-mirror-tier1` already produces) as its primary substrate, inheriting
  all of Tier 1's extraction fixes and keeping the tiers stratified, and posts one curated
  rollup. Cadence is heartbeat-driven debounce-up: events mark synthesis "owed", and the next
  quiet/continuation tick flushes one curated post (costs nothing on an idle fleet).

- **The routing accessor `ChannelsAwareOf` (ONE net-new roster method).** The synthesis routing is
  the TRANSPOSE of the command graph — command flows DOWN the `members[]` graph
  (`internal/roster/roster.go:55-57`), awareness flows UP it. `Config.ChannelsAwareOf(agent)`
  returns the channels where the agent is in `Members` OR is the channel's `XOAgent` — a pure
  derivation over the existing `Bindings()` (`internal/roster/roster.go:289`) that respects the
  read-only-slice contract and adds ZERO new roster schema. The synthesis READ set is those
  channels (the tier below); the POST target is `ChannelForXO(agent)`
  (`internal/roster/roster.go:343`) via `secrets.Webhook(agent)` (`internal/roster/secrets.go:62`).
  The inverse resolver does not exist today (grep-verified net-new).

- **The extensibility seam (net-new, deliberately empty beyond the two members).** The
  constitutional set is a registry of members, each carrying its name, target file, delivery
  mechanism (identity-append vs heartbeat-skill), and embedded content. v1 registers EXACTLY two.
  Adding a member is adding a registry entry plus its embedded asset — the seam is clean and the
  install/seed loop is member-count-agnostic. WHICH further behaviors join the set
  (delegation-first, goalkeeper, fleet-rotation, narrow-answer-settle, and so on) is the
  operator's strategic lever, populated incrementally; this change does NOT pre-decide or
  enumerate the broader corpus.

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
  discipline (Member 2 uses it) — it is rejected only as the home for structural identity.

## Out of scope

- **The broader constitutional corpus.** Only the two named members ship in v1. The remaining
  behaviors are the operator's lever to add later; this change leaves the seam, not a roadmap.
- **Tier 1 (the mechanical per-desk mirror).** Already shipped (`desk-mirror-tier1`); this change
  consumes its output, it does not re-spec it.
- **Channel/webhook provisioning.** The synthesis posts under existing per-desk webhooks
  (`secrets.Webhook`); provisioning channels from the roster is the separate `flotilla provision`
  line.
- **Per-surface load mechanisms beyond Claude Code.** The verify-first probe and the v1 install
  target the Claude Code launch recipe (`--append-system-prompt-file` composed with
  `--remote-control`). Grok/aider identity-file conventions already exist
  (`workspace.IdentityFileName`); wiring their load mechanisms into the installer is a fast-follow
  the seam supports, not v1 scope.

## Impact

- **New capability spec:** `constitutional-skillset`.
- **Affected code (implement phase, after the trio + hydra-ops ratify):** new `assets/skills/`
  embedded tree; new `flotilla doctrine install` subcommand (`cmd/flotilla/`); the seed extension
  to `cmd/flotilla/workspace.go`; the new `ChannelsAwareOf` accessor + tests in
  `internal/roster/roster.go`; the two member assets (Rule-of-Three rule, visibility-synthesis
  skill); a new `docs/span-of-control.md` doctrine doc.
- **Verify-first gate (binding, blocks the install build):** a live probe must confirm
  `claude --append-system-prompt-file <sentinel>` COMPOSED with the real launch recipe
  `claude --remote-control <name>` loads the sentinel file's contents into the session's system
  prompt. `claude --help` (2026-06-21) confirms both flags exist with no documented conflict; the
  NEW variable is the composition under `--remote-control`. This is an implement-phase task, not a
  design blocker.
- **Risk:** LOW for the surface (additive, idempotent kept/created writes, default-seeded — no
  existing workspace is overwritten). The synthesis skill is content + one pure-derivation
  accessor (no daemon-path change). The one thing the trio must confirm is the read/post DAG: with
  reads scoped to the tier below (`ChannelsAwareOf`) and posts to the agent's own channel
  (`ChannelForXO`), synthesis is acyclic iff the membership graph is a directed acyclic graph —
  asserted at roster load, fail-closed otherwise.
