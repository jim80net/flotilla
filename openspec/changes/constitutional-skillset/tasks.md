# Tasks — constitutional-skillset (B1: the installable default doctrine surface + the first member)

> Scope: B1 of the ratified split. Ships the installable distribution surface + the Rule of Three
> ONLY. The visibility-synthesis member (Tiers 2 and 3) is the separate forthcoming B2 change
> (ratified 2026-06-21 to read a LOCAL substrate, not Discord history) and is NOT in this change —
> hence NO `ChannelsAwareOf` accessor, NO membership-graph acyclicity check, NO synthesis
> routing/cadence here.

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

## 1. `assets/skills/` — the embedded constitutional set (TDD)

- [ ] 1.1 Create the versioned `assets/skills/` tree in-repo with the one member asset (section 3
      authors the CONTENT). Embed it via `//go:embed` (model on `internal/dash/assets.go:14`)
      behind a small `internal/doctrine` package exposing the member registry: each member carries
      `Name`, `TargetFile` (where it lands in the workspace), `Mechanism`
      (`identity-append` | `heartbeat-skill`), and `Content` (read from the embedded FS). TEST: the
      registry lists EXACTLY the one v1 member; the member's embedded content is non-empty and its
      `TargetFile`/`Mechanism` are set (the v1 member's `Mechanism` is `identity-append`); the FS
      round-trips (the embed directive guarantees the tree at build time). The `Mechanism` type
      SHALL admit both `identity-append` and `heartbeat-skill` values so a future member of either
      kind needs no schema change.
- [ ] 1.2 The registry is member-count-agnostic (adding a member = adding a registry entry + its
      embedded asset; no install/seed code change). TEST: a fake extra registry entry flows through
      the install loop unchanged (table-driven over the registry, not hardcoded to one).

## 2. `flotilla doctrine install` + `workspace init` seeding (TDD)

- [ ] 2.1 `cmdDoctrineInstall(<agent> [--roster <path>])` — resolve the agent's workspace
      (`workspace.Dir`), iterate the member registry, and dispatch each member BY ITS MECHANISM:
      - A `heartbeat-skill` (whole-file) member writes its content to its own `TargetFile` with the
        SAME idempotent kept/created discipline as `workspace init`
        (`cmd/flotilla/workspace.go:111-118`): an existing target is KEPT (never overwritten), a
        missing one is CREATED, each prints `kept`/`created`. (No such member ships in v1; the path
        exists for B2 and is covered by the member-count-agnostic test in 1.2.)
      - An `identity-append` member appends its distilled rule, wrapped in a sentinel-fenced marked
        block (see 2.2), to the agent's identity file (the file `workspace.IdentityFileName`
        resolves) rather than clobbering it.
      TEST with a temp `$FLOTILLA_WORKSPACE_ROOT`: first install applies every member; a second
      install is idempotent (whole-file members kept; the identity-append member's marker detected
      and skipped); an operator-edited member is preserved.
- [ ] 2.2 **Marker-guarded append idempotency (fixes the trio's B1 P2).** The identity file is
      ALWAYS written by `workspace init` (`cmd/flotilla/workspace.go:101,107`), so it always exists
      by install time — file-existence kept/created cannot govern the append. Implement a
      content-level marker guard: the `identity-append` member's content is wrapped in a sentinel
      fence (e.g. `<!-- flotilla:rule-of-three -->` … `<!-- /flotilla:rule-of-three -->`). Install
      reads the identity file, APPENDS the marked block once iff the OPENING marker is ABSENT, and
      DETECTS-and-SKIPS (printing a skip reason) if the marker is already PRESENT, leaving operator
      edits inside/around the block untouched. TEST: (a) first install appends the block once and
      the identity file then contains exactly one opening + one closing marker; (b) a second install
      detects the marker and does NOT re-append (still exactly one block); (c) operator text edited
      inside the block and adjacent to it survives a re-install verbatim. Keep this granularity
      DISTINCT from the whole-file kept/created path — they apply to disjoint member kinds and must
      not be conflated in the install loop.
- [ ] 2.3 Register `doctrine` in the `cmd/flotilla/main.go` subcommand switch (sibling of
      `workspace`/`result`/`register`).
- [ ] 2.4 Extend `cmdWorkspaceInit` (`cmd/flotilla/workspace.go`) to SEED the constitutional set by
      default after the base scaffold — calling the same install routine — so a freshly scaffolded
      workspace is born with the doctrine in place. The seed obeys the same per-member idempotency
      (whole-file members never overwrite a file the base scaffold or a prior run created; the
      identity-append member appends its marked block exactly once and detect-and-skips thereafter).
      Because `workspace init` writes the bare identity stub FIRST (`workspace.go:101,107`), the seed
      step appends the marked block INTO that just-written stub — verify the resulting identity file
      contains both the stub and the appended block on first init, and that re-running init does not
      re-append. TEST: `workspace init` on a clean root produces the base files AND the appended
      member block; re-running keeps everything (no duplicate block).

## 3. Member 1 — the Rule of Three doctrine asset (content)

- [ ] 3.1 Land the span-of-control doctrine as `docs/span-of-control.md`, authored to
      `docs/xo-doctrine.md` house style from the draft (`.claude/handoffs/DRAFT-rule-of-three.md`):
      the ≤3-active-charges invariant, the fourth-charge-forces-a-layer mechanic, upward
      aggregation, and parallel-not-serial dispatch. Cross-link it from `docs/xo-doctrine.md` and
      the federation section it references.
- [ ] 3.2 Distil the structural rule into the embedded `identity-append` member asset (the concise
      standing-instruction form from the draft's "Wiring it in" block), WRAPPED in the sentinel
      fence (2.2) — the text that gets appended to a coordinating agent's identity file so it loads
      once at launch. Keep it a faithful compression of `docs/span-of-control.md` (single source of
      truth; the asset is the distilled view). VERIFY the doc and the asset do not contradict (per
      cold-test-author-written-docs: every claim in the short asset traces to the long doc).

## 4. `specs/constitutional-skillset/spec.md` (the capability deltas)

- [ ] 4.1 The ADDED requirements are authored in this change's `specs/constitutional-skillset/spec.md`:
      the installable surface (embedded set + idempotent install + default seed), the structural-rule
      identity-append delivery, the content-level marker-guarded append idempotency, the Rule-of-Three
      member, and the extensibility seam. (Authored alongside this tasks file.) NO synthesis or
      routing requirements — those are B2.

## 5. Verify + gate

- [ ] 5.1 `go build ./... && go test ./... -race` green; `go vet` clean; `gofmt` clean.
- [ ] 5.2 `openspec validate constitutional-skillset --strict`.
- [ ] 5.3 Trio (systems-review + open-code-review + STORM) on the implementation — confirm the
      install is idempotent at BOTH granularities (whole-file kept/created never overwrites operator
      edits; the identity-append marker guard appends exactly once and detect-and-skips, preserving
      operator edits); the seed never clobbers base scaffold files nor double-appends the block; the
      registry is member-count-agnostic and dispatches by mechanism.
- [ ] 5.4 PR (reference this change); CI green. Report trio-clean + the ratification fork (install
      mechanism + rejected-alternatives) to hydra-ops → hydra-ops merges on clean gates.
