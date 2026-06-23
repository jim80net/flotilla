# Design — Cross-harness recycle: make `grok` recycle-capable (#158)

**Status:** Design-trio gate PASSED (systems-review + open-code-review, both code-grounded); findings
folded below (see §9). Ready for the formal openspec change + the live grok characterization.
**Issue:** #158 — family-office migrate Claude Code → Grok surface (operator-directed).
**Depends on:** #157 desk-recycle (MERGED, 6.3 live-validated). Unblocked.
**Ratified fork (operator, via prior chapter handoff):** fork-3 — a **simple portable-markdown
bridge**; add a formal handoff schema ONLY if a flotilla-specific hard edge surfaces during the
Claude-authored-handoff → Grok-takeover exercise (then surface it as a finding).

## 1. Objective

Ship the **cross-harness recycle capability** the recycle spec already declares the design is
*ready* for but does not yet *ship*:

> `openspec/specs/recycle/spec.md:179` — "the only harness meeting the recycle-capable bar today is
> Claude Code — the design is cross-harness-READY, not a shipped cross-harness capability".

Concretely: make the **`grok`** surface driver meet the recycle-capable bar, so a Grok desk can be
recycled context-preserved exactly as a Claude desk is, and so **family-office** can migrate Claude
Code → Grok re-activating from a Claude-Code-authored handoff, full XO function intact, on the Grok
subscription (operator-directed spend; grok-research already runs on Grok).

This is the flotilla **cross-harness drop-in** pillar (agentize an existing harness without replacing
it; inter-harness fleets). The grok recycle-capability is the *generalizable* capability; the
family-office-on-Grok config is the *circumstantial* instance.

## 2. Code-grounded gap analysis (what blocks a grok recycle today)

`flotilla recycle` REFUSES cleanly on any surface that does not implement BOTH
`surface.RecycleBridge` AND `surface.ComposerStateProbe` (`cmd/flotilla/recycle.go:392-399` — the
no-silent-degrade invariant). The `grok` driver (`internal/surface/grok.go`) implements neither:

| Capability | claude (`claude.go`) | grok (`grok.go`) today | #158 needs |
|---|---|---|---|
| `Driver` core (Name/Submit/Assess/Rotate/Close) | ✅ | ✅ | — |
| `ResultReader.LatestResult` | ✅ | ✅ (~/.grok store) | — |
| `ComposerStateProbe.ComposerState` | ✅ (`claude.go:199`) | ❌ **missing** | **ADD** (gates need it) |
| `RecycleBridge` (HandoffPath/HandoffTurn/TakeoverTurn) | ✅ (`claude.go:123-160`) | ❌ **missing** | **ADD** |
| `Close` graceful | ✅ `/exit` | ❌ `ErrNoGracefulClose` (`grok.go:109`) | tolerated (see §4) |
| `AwaitingApproval` assessed-state | n/a | ❌ never emitted (`grok.go:33`) | **ADD** — gate safety (§5) |

### 2a. Live finding (empirical, this session, read-only capture of grok-research pane `%21`)

grok-research was observed **blocked on a tool-approval modal** —

```
  ┃  Allow Edit `…/research/options_vol_edge/findings.md`?
  ┃  1 (●) Yes, and don't ask again for anything (always-approve mode)
  ┃  2 (○) Yes, allow all edits during this session
  ┃  3 (○) Yes
  ┃  4 (○) No, reject (type to add feedback)
  1/4:select  │  Ctrl+o:yolo  │  Ctrl+c:cancel
```

— while its status line still showed `⇣89.0k`. The current `parseGrokState`
(`grok.go:158-164`) keys Working on the `⇣` arrow OR a braille spinner over the last 12 lines; the
`⇣89.0k` in the tail forces **Working** here (a modal with no co-present arrow/spinner would instead
fall through to the `return StateIdle` default — `grok.go:163`). Either way it MIS-classifies a
blocking modal. This is the documented `#58` "blocking gates not yet live-captured" gap.

**Precise gate-safety framing (per the design-trio P2 finding — the earlier draft mis-stated the
danger).** The recycle gate is `idleCleared = Assess()==StateIdle && ComposerState()==ComposerCleared`
(`recycle.go:251-252`), an AND. So a modal mis-read as **Working** is *fail-CLOSED* — the `Assess`
arm fails, `pollIdleCleared`/`pollHandoffGate` never pass, recycle ABORTS on timeout, desk untouched
(`recycle.go:126-128, 146-148`); `/exit` (Phase 2) is unreachable. The DANGEROUS path is the inverse:
a modal mis-read as **Idle** (arrow scrolled off, no spinner frame in the tail — plausible, the modal
is a static render) *conjoined with* `ComposerState` wrongly returning `ComposerCleared`. THAT fires
`/exit` into a live modal. Therefore the **load-bearing safety property #158 must guarantee is:
`ComposerState` returns NON-`Cleared` (and/or `Assess` returns non-`Idle`) on the approval modal.**
`AwaitingApproval` as an assessed state is a liveness *nice-to-have* (XO escalation), NOT the gate-
safety mechanism — the gate-safety mechanism is the composer-probe modal classification. See §5.

## 3. Migration mechanism — orchestrated, per ratified fork-3 (NO new heavy verb)

`recycle` resolves ONE driver from the desk's current roster surface and uses it for ALL phases
(handoff gate → close → relaunch → takeover). A claude→grok migration is two surfaces, so a single
`recycle` call cannot span it (the Phase-4 takeover would run grok's ComposerState against a claude
pane, or vice-versa). Three options were considered:

- **(A) `recycle --to-surface grok`** — one atomic command using the FROM driver for phases 0–2 and
  the TO driver for phase 4. Powerful but breaks recycle's single-driver invariant and adds a
  rarely-used flag to the safety-critical core. Rejected for #158 (over-build vs the ratified fork).
- **(B) A new `flotilla migrate` verb.** Cleaner separation but a whole new lifecycle command for a
  one-desk exercise. Rejected for #158 (premature; revisit only if migrations become routine).
- **(C) Orchestrate with existing primitives + the portable-markdown handoff** — RATIFIED (fork-3):
  1. Drive family-office (still Claude) to write the handoff to its designated path and commit it —
     a `RecycleBridge.HandoffTurn` send (the claude bridge; the handoff body is already
     harness-agnostic markdown).
  2. Flip the roster `surface` (`claude-code` → `grok`) AND the launch recipe (claude command → grok
     command) in `state/flotilla.json` + `state/flotilla-launch.json` (host-local, circumstantial).
  3. `flotilla resume family-office` — relaunches via the new (grok) recipe; resume does NOT restore
     context (`resume.go:232`), so it comes up fresh.
  4. Send the grok session the `RecycleBridge.TakeoverTurn` pointing at the same committed handoff
     path; it reads the Claude-authored markdown and takes over.

  (C) needs **no new lifecycle verb** — only that `grok` becomes recycle-capable (so a grok desk can
  ALSO be recycled same-harness afterward) and that the handoff bridged is portable markdown (it
  already is). The orchestration is an XO-run runbook, not flotilla code.

**Decision:** (C). The flotilla *code* deliverable is grok recycle-capability (§4–§5); the migration
itself is an operational runbook (§6) that the live exercise validates.

## 4. grok `RecycleBridge` (the portable-markdown bridge)

- **HandoffPath** — `<cwd>/.flotilla/handoffs/recycle-<token>.md`. A **harness-agnostic** location
  (NOT `.claude/handoffs/`, which is claude-branded). Rationale: a recycle-capable grok desk should
  not write into a claude-namespaced dir; `.flotilla/handoffs/` is the product-owned, portable home.
  The token (timestamp + crypto/rand nonce) keeps it unique + absent-at-HEAD by construction, exactly
  as the claude bridge requires. (The claude bridge keeps `.claude/handoffs/` — unchanged — so this
  is additive, not a migration of existing behavior. Whether to ALSO move the claude bridge to
  `.flotilla/handoffs/` for uniformity is a follow-up question, NOT bundled here.)
- **HandoffTurn** — the same non-interactive, self-committing instruction as claude's, retargeted:
  grok runs git/tools (it is editing files live — see §2a), so `git add -f <path> && git commit` is
  available. Wording must NOT reference claude/memex skills; it references the handoff document
  FORMAT only. grok has no `/handoff` skill, so the "do NOT run the interactive /handoff skill" clause
  is dropped; the imperative "write the document, commit, stop" remains.
- **TakeoverTurn** — read the designated path, BEGIN IMMEDIATELY, parlay via a flotilla message never
  an in-pane prompt. grok has no `/takeover` skill (drop that clause); the remote-driven discipline
  is identical.

`Close` stays `ErrNoGracefulClose` for now. recycle TOLERATES this via the handoff-gated
respawn-kill fallback (`recycle.go:194-200`) — **safe because the handoff is durable by Phase 2**.
Live-characterizing grok's `/exit` (the `grok.go:109` "#158 live-characterizes grok's graceful
close" note) is **optional polish**, NOT a recycle blocker; defer unless the live exercise needs it.

## 5. grok `ComposerStateProbe` + `AwaitingApproval` — REQUIRES live characterization

The idle∧ComposerCleared gate is the safety core of recycle. grok's composer render is NOT the same
as claude's (different glyphs, different cursor behavior, an approval modal claude does not have).
Per the grok driver's own hard-won history (it was once written against the WRONG product —
`grok.go:18-26`) and `~/.claude/rules/never-fabricate-empirical-values.md`, the ComposerState and
AwaitingApproval markers **MUST be live-captured, never written from memory**. Required captures
(states to observe on a real grok TUI):

1. **Idle / cleared** — grok after "Turn completed in Xs." with an empty composer box. (Needed for
   `ComposerCleared` + the idle gate.)
2. **Pending** — a body typed but not submitted. (Needed for `ComposerPending`.)
3. **Approval modal** — the `Allow …?` / `1/4:select` render (captured in §2a). LOAD-BEARING: must
   classify as NOT-`Cleared` (the gate-safety property, §2a); driving `Assess → AwaitingApproval` is
   the liveness bonus (§G2).
4. **Cursor behavior** — where grok's terminal cursor sits relative to the composer (claude's probe
   is cursor-indexed; grok pane `%21` reported cursor y=64 of height=77 with the modal at the
   bottom — grok's cursor may NOT track the composer the way claude's does, so a cursor-indexed
   classifier may need a different anchor — the box-char `❯`/`◆`/`U+2500` chrome).
5. **Multi-line paste delivery (design-trio P1 — load-bearing).** Both bridge turns are MULTI-LINE
   (numbered steps with literal `\n`, as claude's are — `claude.go:132-160`). grok's `Submit` routes
   through `deliver.Send` (bracketed paste), and grok's bracketed-paste MULTI-line behavior is
   explicitly UNCONFIRMED (`grok.go:73-77`: "if a multi-line capture shows early submits, wire `send`
   to `deliver.SendCtrlJ`"). If grok lacks bracketed-paste mode, each `\n` submits early and the
   handoff/takeover instruction FRAGMENTS — Phase 1 breaks. So the session MUST deliver a multi-line
   body and verify it lands as ONE turn; if it fragments, the grok driver's `send` seam is wired to
   `deliver.SendCtrlJ` (the same fix the driver doc names). This is a recycle-correctness gate, not
   cosmetic.
6. **Identity file** — confirm grok's instruction file is `AGENTS.md` (flagged `ASSUMED` at
   `workspace.go:54-57`); free to verify in the same session, closes an open code `ASSUMED`.

**Empirical prerequisite:** grok-research cannot be commandeered (active desk; currently mid-decision
at an approval modal). The clean characterization harness is a **throwaway grok session** (exactly as
6.3 used a throwaway claude for the recycle live-validation). This is within the affirmed Grok
subscription envelope (#158 is operator-directed grok spend; the subscription is flat, ~zero marginal
cost). **This characterization gates the driver implementation** — without it, the ComposerState
classifier would be a fabrication.

Also re-verify (flagged ASSUMED in `workspace.go:55`): grok's identity file is `AGENTS.md` — confirm
against the official grok CLI during the same session.

## 6. Migration runbook (the operational exercise — §3 option C)

(Authored here for completeness; EXECUTED by hydra-ops/operator, not auto-fired — family-office owns
tactical-head's real-money order path, so the cutover TIMING is operator-owned.)

1. **Capability-parity check FIRST** (per #158 acceptance): confirm Grok's harness supports
   family-office's XO role — multi-agent reviews (silent-failure/OCR subagents), git/PR ops, MCP/tool
   access. If a genuine gap exists, surface it as a finding (do NOT silently degrade the desk).
2. family-office (Claude) writes + commits its handoff (claude `HandoffTurn`).
3. Flip roster surface + launch recipe to grok (host-local config).
4. `flotilla resume family-office` (relaunch on grok, fresh).
5. Send grok the `TakeoverTurn` at the committed handoff path. **The path is the FROM-harness
   (claude) path** (`<cwd>/.claude/handoffs/recycle-<token>.md`), sourced from the claude recycle's
   status record `~/.flotilla/family-office/last-recycle.json` `handoff_path` field
   (`recycle.go:489-493`) — NOT guessed, and NOT grok's own `.flotilla/handoffs/` path (grok's
   `TakeoverTurn` is path-parametric, so handing it a `.claude/handoffs/` path is correct).
6. Verify: full XO function, flotilla reachability, real-money order path intact — end-to-end.

## 7. Scope / phasing / what's NOT in

**In #158 (flotilla code):** grok `ComposerStateProbe`, grok `RecycleBridge`, grok
`AwaitingApproval` (liveness) + the gate-safe modal classification (the load-bearing property, §2a),
multi-line `Submit` confirmation (→ `SendCtrlJ` if needed, §5.5) — all live-characterized; unit tests
with fakes per the established surface test pattern (`grok_test.go` table classifier +
`recycle_test.go` bridge-substring + `TestRecycleSupport`/`stubNoBridge` refuse coverage — KEEP the
`stubNoBridge` case, since grok becoming recycle-capable removes the last in-tree refuse fixture).

**Exact spec deltas this change MUST author (design-trio G1, High — the deltas, named):**
- `openspec/specs/surface/spec.md:337` grok-driver requirement ("reduced state set"): it currently
  mandates grok emit ONLY `Shell/Working/Idle` and **SHALL NOT** emit `AwaitingApproval` until the
  gates are live-captured (`:354-357`). #158 live-captures the tool-approval gate, so this delta
  **retracts that clause** and adds `AwaitingApproval` + the `ComposerStateProbe`/modal classification.
- `openspec/specs/surface/spec.md:440`/`:458-460` RecycleBridge requirement ("Only Claude Code
  implements the bridge today… this spec does NOT assert one exists"): amend to assert grok now
  implements it, with the `.flotilla/handoffs/` convention as a second worked example (`:443`).
- The `ComposerStateProbe` capability requirement is NOT yet in the active surface spec — it is parked
  in the **unarchived** `confirm-cursor-disposition` change (`confirm-cursor-disposition/specs/
  surface/spec.md`). **Spec-ordering dependency:** either that change archives first, or #158's
  surface delta carries the requirement. Flag to hydra-ops; do NOT silently duplicate it.
- `AwaitingApproval` wiring: mirror the **aider** precedent (`surface/spec.md:128` — "Emitting
  AwaitingApproval and Errored activates XO escalation") and note that grok desks are today invisible
  to the XO-only wedge timer (`grok.go:33-35`); making `AwaitingApproval` meaningful closes that.
- `openspec/specs/recycle/spec.md:175-199` cross-harness-ready requirement: change "the only harness
  meeting the recycle-capable bar today is Claude Code" → grok now meets it, and add an
  orchestrated-migration scenario encoding the FROM/TO path-sourcing invariant (§6 step 5).
**In #158 (operational):** the family-office migration runbook + live exercise.
**NOT in #158 (follow-ups, filed/flagged):**
- grok graceful `Close` live-characterization (optional polish; respawn-kill suffices).
- Moving the claude bridge to `.flotilla/handoffs/` for path uniformity (uniformity question).
- A general `migrate` verb (only if migrations become routine).
- The fuller grok blocking-gate set (auth/payment) — #58; #158 covers the tool-approval gate it
  needs for recycle safety, the rest stays #58.

## 8. Open items for hydra-ops

1. **fork-3 / option (C)** (orchestrated migration, no new verb) — CONFIRMED by hydra-ops (2026-06-23).
2. **Throwaway-grok characterization session** — APPROVED by hydra-ops (affirmed Grok-subscription
   envelope; the correct cold-test-the-live-artifact step). Proceeding.
3. **family-office cutover timing** — CONFIRMED operator-timed by hydra-ops (real-money order path);
   #158 code + runbook land first; hydra-ops gates/merges the PR like the others.
4. **SSH-agent push blocker** — RESOLVED by hydra-ops: use the gh-token HTTPS bypass (per
   `.claude/rules/git-push-bypass-1password.md`): `git -c credential.helper= -c
   "url.https://x-access-token:$(gh auth token)@github.com/.insteadOf=git@github.com:" push origin
   <branch>`. Not a blocker; the 1Password agent itself is flagged to the operator separately.

## 9. Design-trio findings folded (systems-review + open-code-review, both code-grounded)

Both reviews verified every factual/code claim in this design against source (all TRUE; the
fabrication audit PASSED — no invented grok markers, live-capture explicitly gated). Findings folded:

- **P1 (systems, load-bearing) — multi-line paste UNCONFIRMED for grok.** Folded into §5.5: the live
  session MUST verify a multi-line body lands as one turn; wire `deliver.SendCtrlJ` if it fragments.
- **P2 (systems) — §2a danger was mis-stated.** Folded into §2a: a Working mis-read is fail-CLOSED
  (stalls); the load-bearing property is `ComposerState` non-`Cleared` on the modal.
- **P2 (systems) — FROM/TO path sourcing.** Folded into §6 step 5: source the takeover path from the
  claude recycle's `last-recycle.json` `handoff_path`.
- **P2 (systems) — handoff-path git-root invariant.** The designated path MUST resolve under the git
  work-tree containing the desk's cwd (`HandoffDurable`/`HandoffAbsentAtHead` compute
  `filepath.Rel(gitTopLevel(cwd), path)` — `deliver/recycle.go:64-129`). `<cwd>/.flotilla/handoffs/`
  satisfies this when cwd is inside the worktree; the spec delta states it explicitly (don't assume a
  grok desk's cwd is the git root, as the claude bridge does in practice).
- **P3 (systems) — `remainOnExit(true)` is a no-op on the grok kill-fallback path** (grok never
  `/exit`s); harmless, worth a one-line code comment, not a behavior change.
- **G1 (OCR, High) — spec deltas under-specified.** Folded into §7: the three requirements + the
  `confirm-cursor-disposition` spec-ordering dependency are now named.
- **G2 (OCR) — AwaitingApproval needs the aider-escalation pattern + wedge-timer note.** Folded into §7.
- **G3/G4 (OCR, Low) — test pattern + path-divergence** confirmed consistent; folded into §7 scope.

## 10. Live characterization results (throwaway grok session, 2026-06-23 — EMPIRICAL, not memory)

Captured against a throwaway `grok -m grok-composer-2.5-fast` session in an isolated tmux session
(`grokchar`), driven through each state, read-only `tmux capture-pane`. These are the ground-truth
renders the grok `ComposerStateProbe` / `AwaitingApproval` classifiers are built from (per
never-fabricate-empirical-values — NO marker here is recalled).

### 10.1 Composer is a BOX; the cursor tracks it

grok's composer is a box drawn with `╭─╮ │ ╰─╯` (light box chars), labelled `… Composer 2.5 Fast ─╯`
on the bottom border. The input line is `  │ ❯ <body>                    │` — the `❯` (U+276F) prompt
is preceded by a `│` (U+2502) LEFT border and the body is followed by spaces + a `│` RIGHT border.
The terminal cursor SITS on this line (idle: x=6,y=72 right after `│ ❯ `; pending: x moved right with
the body) — so a cursor-indexed probe (like claude's) is viable, but it MUST strip the `│` border
before the `❯` (claude's `CutPrefix("❯")` alone would fail on grok's `│ ❯`).

| State | cursor line (at cursorY) | bottom status line |
|---|---|---|
| **Cleared** (idle) | `│ ❯` + only spaces + `│` | `[stable]` OR `Shift+Tab:mode │ Ctrl+.:shortcuts` |
| **Pending** | `│ ❯ <body>` + `│` | `Enter:send │ Shift+Tab:mode │ Ctrl+.:shortcuts` |
| **Working** | `│ ❯` empty + `│` (box PERSISTS, cursor stays on it) | spinner `⠙ Waiting…` / `⇣<n>k` |
| **Approval modal** | cursor is OFF the composer, on the `◆ Run … ⇣<n>k` line (NO `❯`) | `1/4:select │ Ctrl+o:yolo │ Ctrl+c:cancel` |

### 10.2 grok `ComposerState` classifier (derived, verified across all 5 captures)

Read the line at cursorY; strip leading whitespace; strip a leading `│` border + whitespace; then:
`CutPrefix("❯")` — if absent → **Undetermined** (covers the approval modal, where the cursor is on
the `◆ Run` line, and multi-line-pending continuation lines, which have no `❯`); else strip the
trailing `│` + whitespace and the leading whitespace of the remainder → empty ⇒ **Cleared**,
non-empty ⇒ **Pending**. grok has no docked-agents sub-composer / queued / list-nav states, so
`ComposerQueued/SubAgent/ListNav` do not apply.

**Gate-safety property holds (the load-bearing §2a requirement, now empirically confirmed):** on the
approval modal the cursor is on `◆ Run …` (no `❯`) ⇒ `Undetermined` ⇒ NON-`Cleared` ⇒ the recycle
`idleCleared` AND-gate fails closed; `/exit` is never fired into a modal. During **Working** the box
persists so `ComposerState` reads `Cleared`, but `Assess==Working` (spinner) fails the AND — safe,
exactly as claude behaves.

### 10.3 grok `AwaitingApproval` (liveness; fixes the live #58/#158 mis-read)

The approval modal renders a `┃` (U+2503 HEAVY bar) block — `┃ Allow <Verb> \`<path>\`?` + numbered
options `1 (●)…4 (○)` — and the status line `N/M:select │ Ctrl+o:yolo │ Ctrl+c:cancel`. The `⇣<n>k`
arrow is CO-PRESENT on the `◆ Run` line, which is why `parseGrokState` currently returns Working.
Fix: in `parseGrokState`, check for the approval modal FIRST (anchor: the `N/M:select` status token
AND/OR the `┃`+`Allow …?` block) → return `StateAwaitingApproval`, BEFORE the `⇣`/spinner Working
check. (Conservative anchors only — the `select` status token is grok chrome, not prose.)

### 10.4 P1 multi-line paste — RESOLVED (grok supports bracketed-paste multi-line)

A 3-line body bracketed-pasted (`tmux load-buffer` + `paste-buffer -p`, the same bracketed-paste
`deliver.Send` uses) landed as ONE composer body across 3 box lines with NO early submit. So grok's
`Submit` delivers a multi-line handoff/takeover turn intact — **`SendCtrlJ` is NOT needed**; the
existing `g.send = deliver.Send` is correct for the bridge turns. (The driver's `grok.go:73-77`
"multi-line unconfirmed" caveat is now CONFIRMED safe for bracketed-paste; update that comment.)

### 10.5 Identity file — `AGENTS.md` ASSUMPTION is WRONG (finding, not bundled)

grok did NOT create or auto-load an `AGENTS.md` (nor a `MEMORY.md`) in the workspace at launch.
grok's instruction model (from `grok --help` + binary strings) is `MEMORY.md` (workspace + global
`~/.grok/memory/MEMORY.md`, opt-in via the `memory` subcommand / `--experimental-memory`), plus
`--rules <RULES>` and `--system-prompt-override`. So `workspace.go:54-65`'s ASSUMED `AGENTS.md`-for-
grok mapping is almost certainly INCORRECT — grok uses `MEMORY.md`/`--rules`, not `AGENTS.md`. This
does NOT affect the recycle MECHANISM (the handoff carries chapter context, not the identity file),
so it is OUT OF #158's recycle-driver scope — recorded as a follow-up (correct `workspace.go`'s grok
identity mapping; relevant to where the family-office migration writes its persistent XO doctrine on
grok). File as a separate issue; flag in the migration runbook (§6 step 1 capability-parity check).
