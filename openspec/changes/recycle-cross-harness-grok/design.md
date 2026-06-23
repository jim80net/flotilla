# Design — Cross-harness recycle: make `grok` recycle-capable (#158)

**Status:** DRAFT for the design-trio gate (systems-review + open-code-review) and hydra-ops review.
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
(`grok.go:158-164`) keys Working on the `⇣` arrow OR a braille spinner, so it reads this
**AwaitingApproval** state as **Working**. This is the documented `#58` "blocking gates not yet
live-captured" gap — and it is now a **#158 prerequisite**: a recycle's Phase-0/Phase-1
idle∧ComposerCleared gate must NEVER classify an approval modal as a cleared composer (that would
fire `/exit` keystrokes into a live modal, mis-route, and at worst close a desk mid-decision). See §5.

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
3. **Approval modal** — the `Allow …?` / `1/4:select` render (captured in §2a). (Must classify as
   NOT-cleared, and ideally drive `Assess → AwaitingApproval`.)
4. **Cursor behavior** — where grok's terminal cursor sits relative to the composer (claude's probe
   is cursor-indexed; grok pane `%21` reported cursor y=64 of height=77 with the modal at the
   bottom — grok's cursor may NOT track the composer the way claude's does, so a cursor-indexed
   classifier may need a different anchor — the box-char `❯`/`◆`/`U+2500` chrome).

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
5. Send grok the `TakeoverTurn` at the committed handoff path.
6. Verify: full XO function, flotilla reachability, real-money order path intact — end-to-end.

## 7. Scope / phasing / what's NOT in

**In #158 (flotilla code):** grok `ComposerStateProbe`, grok `RecycleBridge`, grok
`AwaitingApproval` (gate safety) — all live-characterized; unit tests with fakes per the surface
test pattern; spec deltas (surface + recycle).
**In #158 (operational):** the family-office migration runbook + live exercise.
**NOT in #158 (follow-ups, filed/flagged):**
- grok graceful `Close` live-characterization (optional polish; respawn-kill suffices).
- Moving the claude bridge to `.flotilla/handoffs/` for path uniformity (uniformity question).
- A general `migrate` verb (only if migrations become routine).
- The fuller grok blocking-gate set (auth/payment) — #58; #158 covers the tool-approval gate it
  needs for recycle safety, the rest stays #58.

## 8. Open items for hydra-ops

1. **Confirm fork-3 / option (C)** (orchestrated migration, no new verb) — believed ratified; proceed
   unless redirected.
2. **Throwaway-grok characterization session** — affirmed-envelope spend; heads-up that I'll spin one
   to live-capture grok's composer/approval/idle renders (the impl-gating empirical step).
3. **family-office cutover timing** — operator/hydra-ops-owned (real-money order path); #158 code +
   runbook land first, the live cutover is scheduled separately.
4. **SSH-agent push blocker** — `git fetch`/push currently fails ("1password ssh key … agent
   communication failed"); the PR push will need this resolved (operator-owned).
