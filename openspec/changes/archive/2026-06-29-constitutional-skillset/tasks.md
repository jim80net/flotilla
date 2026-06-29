# Tasks — constitutional-skillset (B1: the installable default doctrine surface + the first member)

> Scope: B1 of the ratified split. Ships the installable distribution surface + the Rule of Three
> ONLY. The visibility-synthesis member (Tiers 2 and 3) is the separate forthcoming B2 change
> (ratified 2026-06-21 to read a LOCAL substrate, not Discord history) and is NOT in this change —
> hence NO `ChannelsAwareOf` accessor, NO membership-graph acyclicity check, NO synthesis
> routing/cadence here.

## 0. Verify-first live probe (BINDING — gates everything below)

- [x] 0.1 Confirm the ACTUAL B1 runtime path end-to-end — not merely that a standalone sentinel
      file loads. Construct an identity file shaped EXACTLY as the install produces it: the bare
      identity stub (as `workspace init` writes at `cmd/flotilla/workspace.go:101`) FOLLOWED BY an
      APPENDED marked block (the sentinel-fenced Rule-of-Three block from 2.2) carrying a unique
      recognizable token INSIDE the appended block (after the stub, not in it). Launch a throwaway
      Claude Code session with `claude --append-system-prompt-file <that identity>` COMPOSED with the
      real launch recipe `claude --remote-control <name>` (BOTH flags together — the standalone
      `--append-system-prompt-file` load is already noted at `cmd/flotilla/workspace.go:94-96`; the
      NEW variables are the `--remote-control` composition AND that an appended-AFTER-the-stub block
      survives into the prompt, since the install's real output is an append, not a whole-file
      write), and confirm the session can recite the token FROM THE APPENDED BLOCK. `claude --help`
      (2026-06-21) already confirms both flags exist with no documented conflict. If the appended
      block does NOT reach the live prompt, STOP and surface the blocker — the install mechanism's
      primary delivery path depends on the appended block (not a whole file) loading.
      (Per verify-api-response-shape-with-probe and the runtime-path-end-to-end discipline.)

## 1. `assets/skills/` — the embedded constitutional set (TDD)

- [x] 1.1 Create the versioned `assets/skills/` tree in-repo with the one member asset (section 3
      authors the CONTENT). Embed it via `//go:embed` (model on `internal/dash/assets.go:14`)
      behind a small `internal/doctrine` package exposing the member registry: each member carries
      `Name`, `TargetFile` (where it lands in the workspace), `Mechanism`, and `Content` (read from
      the embedded FS). Scope the `Mechanism` vocabulary to what B1 uses — `identity-append` — and
      do NOT pre-add or pre-test any other mechanism value; the type stays a plain string-backed
      enum so a future member kind extends the vocabulary when it is designed (no pre-baked second
      arm). TEST: the registry lists EXACTLY the one v1 member; the member's embedded content is
      non-empty and its `TargetFile`/`Mechanism` are set (the v1 member's `Mechanism` is
      `identity-append`); the FS round-trips (the embed directive guarantees the tree at build time).
- [x] 1.2 The registry is member-count-agnostic (adding a member = adding a registry entry + its
      embedded asset; no install/seed code change). TEST: a SECOND fake `identity-append`-shaped
      registry entry (its own marker, its own target) flows through the install loop unchanged —
      table-driven over the registry, not hardcoded to one — proving the loop's count-agnosticism
      without inventing a not-yet-designed mechanism.

## 2. `flotilla doctrine install` + `workspace init` seeding (TDD)

- [x] 2.1 `cmdDoctrineInstall(<agent> [--roster <path>])` — resolve the agent's workspace
      (`workspace.Dir`), iterate the member registry, and dispatch each member BY ITS `Mechanism`.
      B1's only mechanism is `identity-append`: it appends the member's distilled rule, wrapped in a
      sentinel-fenced marked block (see 2.2), to the agent's identity file (the file
      `workspace.IdentityFileName` resolves) rather than clobbering it. Do NOT implement or test a
      whole-file install arm for a mechanism no B1 member uses — the kept/created discipline
      `workspace init` already uses (`cmd/flotilla/workspace.go:109-119`) is the inherited model a
      future whole-file member would adopt, not a path B1 builds. Keep the dispatch a clean
      switch-on-`Mechanism` so a new arm is added with its member, not pre-stubbed now.
      TEST with a temp `$FLOTILLA_WORKSPACE_ROOT`: first install applies the member; a second
      install is idempotent (the identity-append member's marker detected and skipped); an
      operator-edited member is preserved.
- [x] 2.2 **Marker-guarded append idempotency (fixes the trio's B1 P2).** The identity file is
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
- [x] 2.3 Register `doctrine` in the `cmd/flotilla/main.go` subcommand switch (sibling of
      `workspace`/`result`/`register`).
- [x] 2.4 Extend `cmdWorkspaceInit` (`cmd/flotilla/workspace.go`) to SEED the constitutional set by
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

- [x] 3.1 Land the span-of-control doctrine as `docs/span-of-control.md`, authored to
      `docs/xo-doctrine.md` house style from the draft (`.claude/handoffs/DRAFT-rule-of-three.md`):
      the ≤3-active-charges invariant, the fourth-charge-forces-a-layer mechanic, upward
      aggregation, and parallel-not-serial dispatch. Cross-link it from `docs/xo-doctrine.md` and
      the federation section it references. FIX the draft's 4 BROKEN cross-links while porting: the
      draft (`.claude/handoffs/DRAFT-rule-of-three.md:65,168,232,246`) points at
      `./quickstart.md#federation--a-recursive-hub-and-spoke-fleet`, but the real heading is
      `### Federated fleets — per-project channels + ` + "`#fleet-command`" + ` (`docs/quickstart.md:412`),
      whose GitHub-rendered anchor is `#federated-fleets--per-project-channels--fleet-command`.
      Repoint all four to the correct anchor. COLD-TEST the landed doc (per
      cold-test-author-written-docs): enumerate every cross-link in `docs/span-of-control.md` and
      confirm each resolves to an existing heading/anchor in its target file — no link ships broken.
- [x] 3.2 Distil the structural rule into the embedded `identity-append` member asset (the concise
      standing-instruction form from the draft's "Wiring it in" block), WRAPPED in the sentinel
      fence (2.2) — the text that gets appended to a coordinating agent's identity file so it loads
      once at launch. Keep it a faithful compression of `docs/span-of-control.md` (single source of
      truth; the asset is the distilled view). VERIFY the doc and the asset do not contradict (per
      cold-test-author-written-docs: every claim in the short asset traces to the long doc).
- [x] 3.3 INSIDE the sentinel fence (so it travels with the appended block), carry a one-line
      load-bearing-marker note, e.g. `<!-- the flotilla:rule-of-three marker fence above/below is
      load-bearing — do NOT delete it; install detects it to avoid re-appending this block -->`.
      Rationale: the marker IS the idempotency guard (2.2); if an operator strips the marker but
      keeps the prose, the next install no longer detects the block and re-appends a duplicate. The
      in-fence note tells a human editing the identity file why the comment markers must stay. TEST:
      the appended block contains the load-bearing note between the opening and closing markers.
- [x] 3.4 Add ONE sentence to the Rule-of-Three CONTENT (both `docs/span-of-control.md` and the
      distilled asset) naming the recurring-fan-out edge: a sub-agent that is RE-DISPATCHED every
      heartbeat is functionally a STANDING charge (you must remember its state across rotations), so
      it COUNTS against the three; only TRANSIENT report-and-exit fan-out remains the unbounded
      floor. This sharpens the draft's "does this charge require me to remember its state across my
      next rotation?" discrimination test (draft lines 90-92) for the recurring case. FLAG in this
      task: this is the primary XO's doctrine knob — surface the exact sentence for the primary XO to eyeball,
      since where the standing/transient line falls for recurring fan-out is an operating-doctrine
      judgment, not a mechanical one.

## 4. `specs/constitutional-skillset/spec.md` (the capability deltas)

- [x] 4.1 The ADDED requirements are authored in this change's `specs/constitutional-skillset/spec.md`:
      the installable surface (embedded set + idempotent install + default seed), the structural-rule
      identity-append delivery, the content-level marker-guarded append idempotency, the Rule-of-Three
      member, and the extensibility seam. (Authored alongside this tasks file.) NO synthesis or
      routing requirements — those are B2.

## 5. Verify + gate

- [x] 5.1 `go build ./... && go test ./... -race` green; `go vet` clean; `gofmt` clean.
- [x] 5.2 `openspec validate constitutional-skillset --strict`.
- [ ] 5.3 Trio (systems-review + open-code-review + STORM) on the implementation — confirm the
      install is idempotent at BOTH granularities (whole-file kept/created never overwrites operator
      edits; the identity-append marker guard appends exactly once and detect-and-skips, preserving
      operator edits); the seed never clobbers base scaffold files nor double-appends the block; the
      registry is member-count-agnostic and dispatches by mechanism.
- [ ] 5.4 PR (reference this change); CI green. Report trio-clean + the ratification fork (install
      mechanism + rejected-alternatives) to the primary XO → the primary XO merges on clean gates.
