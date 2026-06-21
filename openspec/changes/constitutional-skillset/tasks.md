# Tasks — constitutional-skillset (the installable default doctrine surface + first two members)

## 0. Verify-first live probe (BINDING — gates everything below)

- [ ] 0.1 Confirm `claude --append-system-prompt-file <sentinel>` COMPOSED with the real launch
      recipe `claude --remote-control <name>` loads the sentinel file's contents into the live
      session's system prompt. Write a sentinel file with a unique recognizable token, launch a
      throwaway Claude Code session with BOTH flags together (not in isolation — the standalone
      `--append-system-prompt-file` load is already noted at `cmd/flotilla/workspace.go:94-96`; the
      NEW variable is the `--remote-control` composition), and confirm the session can recite the
      token. `claude --help` (2026-06-21) already confirms both flags exist with no documented
      conflict. If the composition does NOT load the file, STOP and surface the blocker — the
      install mechanism's primary delivery path depends on it. (Per verify-api-response-shape-with-probe.)

## 1. `internal/roster` — the `ChannelsAwareOf` accessor (TDD)

- [ ] 1.1 `Config.ChannelsAwareOf(agent string) []string` — return the channel ids where `agent`
      appears in the binding's `Members` OR equals the binding's `XOAgent`, derived purely over
      `Bindings()` (respecting the read-only-slice contract — read only, never append/reassign).
      TEST: an agent that is a member of two channels → both ids; an XO whose channel lists it as
      `XOAgent` → that id; an agent in no binding → empty (not nil-panic); de-duplication if an
      agent is both a member and the XO of the same channel → that id once. Mirror the existing
      `ChannelForXO`/`IsXO` test style.
- [ ] 1.2 `Config` exposes a membership-graph acyclicity check used at load: assert the directed
      graph (edge = an XO of channel C is a Member of channel C' it reports up into) is a directed
      acyclic graph; fail-closed with a clear error naming the cycle if not. TEST: a linear
      meta-XO → project-XO → boat chain validates; a deliberately cyclic roster fails load with the
      cycle named. (This is what guarantees synthesis reads-below / posts-own-level can never loop.)

## 2. `assets/skills/` — the embedded constitutional set (TDD)

- [ ] 2.1 Create the versioned `assets/skills/` tree in-repo with the two member assets (sections 4
      and 5 author the CONTENT). Embed it via `//go:embed` (model on `internal/dash/assets.go:14`)
      behind a small `internal/doctrine` package exposing the member registry: each member carries
      `Name`, `TargetFile` (where it lands in the workspace), `Mechanism`
      (`identity-append` | `heartbeat-skill`), and `Content` (read from the embedded FS). TEST: the
      registry lists EXACTLY the two v1 members; every member's embedded content is non-empty and
      its `TargetFile`/`Mechanism` are set; the FS round-trips (the embed directive guarantees the
      tree at build time).
- [ ] 2.2 The registry is member-count-agnostic (adding a member = adding a registry entry + its
      embedded asset; no install/seed code change). TEST: a fake extra registry entry flows through
      the install loop unchanged (table-driven over the registry, not hardcoded to two).

## 3. `flotilla doctrine install` + `workspace init` seeding (TDD)

- [ ] 3.1 `cmdDoctrineInstall(<agent> [--roster <path>])` — resolve the agent's workspace
      (`workspace.Dir`), iterate the member registry, and for each member write its content to its
      `TargetFile` in the workspace with the SAME idempotent kept/created discipline as
      `workspace init` (`cmd/flotilla/workspace.go:111-118`): an existing target is KEPT (never
      overwritten), a missing one is CREATED, each prints `kept`/`created`. An `identity-append`
      member appends its distilled rule to the agent's identity file (the file
      `workspace.IdentityFileName` resolves) rather than clobbering it; a `heartbeat-skill` member
      writes its own skill file. TEST with a temp `$FLOTILLA_WORKSPACE_ROOT`: first install creates
      every member; a second install KEEPS them all (idempotent); an operator-edited member is
      preserved.
- [ ] 3.2 Register `doctrine` in the `cmd/flotilla/main.go` subcommand switch (sibling of
      `workspace`/`result`/`register`).
- [ ] 3.3 Extend `cmdWorkspaceInit` (`cmd/flotilla/workspace.go`) to SEED the constitutional set by
      default after the base scaffold — calling the same install routine — so a freshly scaffolded
      workspace is born with the doctrine in place. The seed obeys the same kept/created discipline
      (it never overwrites a file the base scaffold or a prior run created). TEST: `workspace init`
      on a clean root produces the base files AND the two members; re-running keeps everything.

## 4. Member 1 — the Rule of Three doctrine asset (content)

- [ ] 4.1 Land the span-of-control doctrine as `docs/span-of-control.md`, authored to
      `docs/xo-doctrine.md` house style from the draft (`.claude/handoffs/DRAFT-rule-of-three.md`):
      the ≤3-active-charges invariant, the fourth-charge-forces-a-layer mechanic, upward
      aggregation, and parallel-not-serial dispatch. Cross-link it from `docs/xo-doctrine.md` and
      the federation section it references.
- [ ] 4.2 Distil the structural rule into the embedded `identity-append` member asset (the concise
      standing-instruction form from the draft's "Wiring it in" block) — the text that gets appended
      to a coordinating agent's identity file so it loads once at launch. Keep it a faithful
      compression of `docs/span-of-control.md` (single source of truth; the asset is the distilled
      view). VERIFY the doc and the asset do not contradict (per cold-test-author-written-docs:
      every claim in the short asset traces to the long doc).

## 5. Member 2 — the visibility-synthesis skill asset (content)

- [ ] 5.1 Author the `heartbeat-skill` member: the XO-facing skill that, on the heartbeat, reads the
      tier below via `flotilla` reads scoped by `ChannelsAwareOf(self)` (the Tier-1 mirror posts in
      those channels are the substrate), curates them into ONE rollup, and posts to
      `ChannelForXO(self)`. Spell out the per-tier output contract: Tier 2 (project-XO) = a curated
      domain view of its boats; Tier 3 (meta-XO) = a fleet headline + the operator-decision items +
      drill-down pointers DOWN the chain (#fleet-command → project channel → boat channel → pane —
      the inverse of the membership graph). State the cadence contract: heartbeat-driven
      debounce-up — a new mirror post marks synthesis "owed"; the next quiet/continuation tick
      flushes one curated post; an idle fleet costs nothing.
- [ ] 5.2 The skill MUST distinguish synthesis-read intent from command-read intent: the relay's
      feedback guard drops webhook posts to prevent command loops, but synthesis legitimately reads
      the (webhook-authored) mirror stream. Document in the skill that it reads the channel HISTORY
      as substrate (an explicit, read-only synthesis intent) and never treats a mirror post as an
      inbound command — and that loop-freedom is structurally guaranteed by reads-below /
      posts-own-level over the acyclic membership graph (task 1.2). No code change to the relay's
      guard (it stays correct for command delivery); this is a skill-contract statement.

## 6. `specs/constitutional-skillset/spec.md` (the capability deltas)

- [ ] 6.1 The ADDED requirements are authored in this change's `specs/constitutional-skillset/spec.md`:
      the installable surface (embedded set + idempotent install + default seed), the Rule-of-Three
      member, the visibility-synthesis member (membership-graph-derived routing), and the
      extensibility seam. (Authored alongside this tasks file.)

## 7. Verify + gate

- [ ] 7.1 `go build ./... && go test ./... -race` green; `go vet` clean; `gofmt` clean.
- [ ] 7.2 `openspec validate constitutional-skillset --strict`.
- [ ] 7.3 Trio (systems-review + open-code-review + STORM) on the implementation — confirm the
      install is idempotent and never overwrites operator edits; the seed never clobbers base
      scaffold files; `ChannelsAwareOf` respects the read-only-slice contract; the
      reads-below/posts-own-level DAG is asserted fail-closed; the registry is member-count-agnostic.
- [ ] 7.4 PR (reference this change); CI green. Report trio-clean + the ratification fork (install
      mechanism + rejected-alternatives) to hydra-ops → hydra-ops merges on clean gates.
