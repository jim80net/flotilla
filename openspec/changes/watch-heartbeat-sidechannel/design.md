# Design: side-channel heartbeat — cheap-check-then-escalate (kill the self-trigger, light up the dormant gate)

> **Review status:** revised after `/systems-review` + `/open-code-review` on the
> first draft. The reviews falsified the first draft's headline ("activity-derived
> liveness collapses the ping") — see §"What the review changed" — so this design
> leads with the two *safe, high-value* wins and demotes activity-derived liveness
> to a deferred fork (F5). Fork rulings are the operator's at the checkpoint.

## Problem (verified against the running system)

The operator observes that "every ~20-min heartbeat fires a full Opus XO turn …
even when nothing is actionable." The forensic read of the live system explains
why, and reframes the work:

- **Production runs the LEGACY always-wake heartbeat, not the v2 detector.** The
  live roster `…/spark/state/flotilla.json` sets `heartbeat_interval: "20m"` and a
  heavy `heartbeat_message` (DUTY A: sweep every desk's pane; DUTY B: read the
  tracker + openspec + README and advance; DUTY C: operator-decision queue) — but
  it has **no `change_detector` field**. So `cmdWatch` takes the legacy branch
  (`cmd/flotilla/watch.go:256`): a `Heartbeat` that injects the full
  `heartbeat_message` into the XO pane every idle interval. The state dir confirms
  it — `flotilla-xo-alive` exists, but `flotilla-detector-state.json`,
  `flotilla-xo-settled`, and `flotilla-xo-awaiting` (the v2 artifacts) do **not**.
  *(This is a point-in-time observation; the prod-flip tasks re-verify it at flip
  time — the operator may enable the detector independently.)*
- **The v2 change-detector already implements the operator's design.** It is a
  deterministic, pure-Go, no-LLM tick (`internal/watch/detector.go`) that snapshots
  each desk's `surface.Assess` state + the tracker hash, diffs against a persisted
  snapshot, and wakes the XO ONLY on a curated material change — with three
  liveness layers (immediate shell-crash, wall-clock ack-age threshold, and a
  max-quiet safety ping). An idle fleet is `$0/tick`.

So **the bulk of the burn is removed by config, not code:** enabling the detector
in prod. The operator's ask to "extend the detector into a real
cheap-check-then-escalate gate" has exactly one *safe, high-value* code component —
killing a self-trigger that the detector has today — plus a naming/decomposition of
the escalation triggers. The headline "ack liveness cheaply without a full XO turn"
turns out to be *already true* once the detector is on, for the reason the liveness
decomposition below makes precise.

## The liveness decomposition (the durable insight — survived review)

"Ack liveness cheaply without a full XO turn" is in tension with the liveness
invariant *unless we are precise about what liveness means*. There are **three
independent liveness questions**, and only ONE irreducibly needs an XO turn:

| # | Question | Signal | Cost | Needs an XO turn? |
|---|----------|--------|------|-------------------|
| A | Is the `flotilla-watch` daemon (the clock) alive? | systemd `Restart=on-failure` | $0 | No |
| B | Is the XO pane present / not crashed-to-shell? | detector `Assess` → `StateShell` (debounced) every tick | $0 | No |
| C | Is the XO **responsive** (takes a turn, not wedged/context-exhausted)? | the XO touches the ack file | **a turn** | **Yes — irreducibly** |

A and B already cost nothing and need no XO turn — the cheap checker proves them
every tick. The entire token cost is in **C**, and C surfaces only through the
**max-quiet liveness ping** (`WakePing`): an ack-only XO turn on an otherwise-idle
fleet.

**The key conclusion (sharpened by the review):** C is *irreducible*. Proving the
XO is **responsive** requires it to actually take a turn; no side-channel process
can ack C on its behalf, because that would only prove the *side process* is alive,
not the XO. And — critically — you cannot even use a *proxy* for C (such as "the XO
recently wrote its tracker file") without weakening it: a tracker write proves the
XO ran *at some past instant*, not that its tmux composer will consume the next
input. An XO whose composer is wedged but whose tracker was written by its last
pre-wedge turn would look "alive" by the proxy while being dead to the watch
channel. **So C must stay a real ack elicited by a real wake.**

The good news: once the detector is on, C is **already cheap**. In the recommended
`liveness_ping_mode: "none"`, the ping fires at the wide `2K` cadence (K=3 ⇒ every
6 intervals = 120 min) and asks only for a one-line ack — not the full DUTY-A/B/C
sweep. A crash is still immediate (B). A busy XO is woken by *real* triggers and
acks as a side effect, so the ping essentially never fires. **The dedicated
liveness cost is therefore already ~one ack-only turn per ~2 hours on a fully-idle
fleet, and ~zero otherwise — there is no large remaining liveness burn to chase.**
The burn the operator sees is the *legacy full-sweep every 20 min*, removed by the
flip — not the ping.

## Mechanism 0 — light up the dormant detector (config; the immediate win)

Set `change_detector: true` + `liveness_ping_mode: "none"` in the live roster.
`cmdWatch` then runs the existing v2 detector instead of legacy always-wake. Idle
fleet → `$0/tick`; the XO is woken only on material change; liveness is the
three-layer detector path. This is an operator-gated production change to the
safety clock (tasks §1), verified end-to-end (force a stale ack → down-alert still
fires; shell-crash → immediate alert) before it is trusted.

**Window note (review M2):** the "no later than before" wedge guarantee is a
**v2-to-v2** statement (the detector's `K×interval` window is unchanged by this
design's code). The legacy→`none` flip *itself* changes the window character
(legacy's every-interval ack vs `none`'s `2K` ping); that is a separate,
operator-gated step whose live verification is tasks §1.3 — not part of the
additive code proof below.

## Mechanism 1 — resolve the tracker self-trigger (the real, safe code win)

Under v2, `externalMaterial` (`materiality.go:86`) treats a tracker-hash change as a
**wake** signal: "state tracker changed" → `WakeMaterial`. But `.flotilla-state.md`
is the XO's **own output** — the deployed heartbeat prompt instructs "keep
`.flotilla-state.md` current." So:

```
XO takes a turn → writes .flotilla-state.md → hash changes
  → next tick: externalMaterial sees the change → WakeMaterial → wakes the XO
  → XO takes a turn → writes .flotilla-state.md → … (until the XO settles)
```

self-perpetuating until the XO settles — it converts "the XO did work" into "wake
the XO again," partially defeating `$0-idle` after every burst of XO activity, and
conflating the XO's own writes with genuine external changes. (Verified: the
tracker-hash branch in `externalMaterial`, materiality.go:86-88, fires only from
`.flotilla-state.md`, which the roster prompt makes the XO write.)

**Fix:** a single-writer-by-XO tracker's delta is the XO's *own* action and SHALL
NOT be a wake signal. Remove the tracker-hash from the wake materiality set.
Genuine *external* state deltas (a desk or tool dropping a signal the XO must react
to) move to a **separate optional `--signal-file`** (`$FLOTILLA_SIGNAL_FILE`) whose
hash change IS a wake trigger — a file the XO does **not** write, so it carries no
self-trigger. This is purely a *reduction* of wakes (and a re-homing of the
external-signal wake); it touches no liveness threshold, so it cannot weaken the
watchdog. This is the "examines the state file … escalates only on a real trigger"
the operator asked for, done safely.

For a deployment whose tracker is genuinely multi-writer (written by non-XO
processes), the delta-as-wake behavior is still available (it is just the
external-signal trigger pointed at that file) — see F3.

## Mechanism 2 — name the escalation-trigger set (the "split")

Make the split explicit and extensible. The **cheap side-channel checker** (the
detector tick — pure-Go, no-LLM, every interval) escalates to a **full XO turn**
ONLY on one of:

1. **Operator message** — relay delivers immediately and clears settled. *Exists.*
2. **A desk needs attention** — a monitored desk's material transition
   (`→Shell/Errored/AwaitingApproval/AwaitingInput`, or `Working→Idle`). *Exists.*
3. **An external signal-file delta** — the optional `--signal-file` hash changed
   (Mechanism 1). *New, optional.*
4. **XO self-continuation** — the XO's own `Working→Idle`, bounded by the cap.
   *Exists.*
5. **Liveness wedge-probe** — the clock-driven max-quiet ping (independent of
   activity; see the review note). *Exists.*

Everything else — a desk resuming work, render flicker, the XO updating its own
tracker, a steady idle state — acks liveness cheaply (B + the existing ack path) and
spends **no XO turn**. The set stays code-extensible.

## Forks (operator's call — recorded for the checkpoint)

- **F1 — "PR awaiting merge" trigger: in-daemon poll vs XO duty.** A `gh pr list` /
  git poll is a **network + auth** call with its own failure modes; folding it into
  the safety-critical clock is the coupling the relay-non-fatal lesson
  (`cmd/flotilla/relay.go:336-340`) and the voice "separate process" decision warned
  against. **Rec: keep PR-state OUT of the safety clock.** Either (a) it stays an
  **XO duty** the XO performs when woken for triggers 1–4 (cheapest — recommended);
  or (b) a separate, optional, fail-safe emitter writes the `--signal-file` when a
  PR is mergeable. **Not** a poll inside `flotilla watch`.
- **F3 — tracker writer-model default.** Mechanism 1 assumes `.flotilla-state.md`
  is XO-single-writer (it *appears* so in this deployment, but has never been
  exercised under v2 — see review P2-3). **Rec: default "tracker delta is NOT a
  wake signal"** (it is the XO's own write); a deployment with a multi-writer
  tracker that needs delta-wakes points `--signal-file` at that file. This is
  strictly safer than today's "always wake on tracker delta."
- **F4 — make v2 the build-time default.** v2 is opt-in today; once this lands the
  legacy always-wake path is strictly worse. **Rec: default `change_detector` on**
  (keep legacy reachable via explicit `false`), and flip the live roster now
  regardless of this fork.
- **F5 — activity-derived liveness broadening (DEFERRED; the falsified first-draft
  idea, kept for the operator to rule on).** The first draft proposed feeding the
  tracker/marker mtimes into the liveness `Age()` so a busy XO never needs a ping.
  Review verdict: **low value, real risk — recommend NOT building it.** Reasons,
  all verified:
  - It does **not** achieve the stated goal: the ping fires from `quietTicks`
    (`detector.go:291-299`), a counter of detector-ticks-since-last-*detector-wake*,
    which is **independent of `Age()`**. Broadening `Age()` does not suppress the
    ping (systems-review P1-1).
  - The "fix" that *would* make it suppress the ping (reset `quietTicks` on activity)
    is the one genuine liveness regression in the whole effort: on an idle fleet in
    `none` mode the ping is the **sole** responsiveness-elicitor, and a tracker
    write is not proof of composer-responsiveness (the wedged-composer case above).
  - The tracker lives in a **git working tree**; `git pull` / `git checkout` /
    editor temp-rename advance its mtime with no XO involvement, spoofing "alive"
    (systems-review P1-2; cf. `working-tree-pollution-stash-restore`).
  - A future-dated mtime (clock skew / restored file) would pin `Age()` at 0 and
    **blind the watchdog indefinitely** unless every source is clamped to `≤ now`
    (systems-review P1-3; cf. `backfill-mtime-clamp-to-now`).
  - The settle marker is consumed each tick (`settled.go:37-53`), so its mtime is a
    one-tick, near-useless source (P2-1); the awaiting-marker *writer* side may not
    even be wired (OCR L2, PR-#18 lineage).
  If the operator nonetheless wants a defensive broadening, the **only safe form**
  is: down-alert sources ONLY (never the ping), **ack-file canonical**, tracker
  **opt-in** (not default) and **only** when proven single-writer, every source
  **clamped to `≤ now`**, settle/awaiting **excluded**. Given the ping is already
  cheap (Mechanism 0), the recommendation is to **not build F5** and let C stay the
  honest, irreducible, already-minimal ack.

## Safety invariants (must all hold — the review checklist)

1. **No liveness threshold is touched by the code in this change.** Mechanisms 1
   and 2 only *reduce/re-home wakes* and *name* the trigger set; the wedge window
   (`alertInterval×interval`) and the crash fast-path are byte-for-byte the v2
   path. (This is why the change is safe — it does not go near `Age()`/the ping.)
2. **Crash fast-path unchanged:** `StateShell` (debounced 2×) → immediate alert.
3. **Liveness independent of the snapshot:** stays in-memory + the ack file (H3,
   preserved).
4. **Fail-safe reads:** a `stat`/read error on the signal file → "unchanged" (no
   wake), the same direction as today's `trackerHasher` (`watch.go:377-386`).
5. **No new failure surface in the clock:** no network/auth added to
   `flotilla watch` (F1 keeps PR-state out).
6. **Backward-compatible:** legacy path untouched when `change_detector` is off; the
   signal-file trigger is inert when unconfigured.
7. **F5 not built** (default) — so none of P1-1/P1-2/P1-3/P2-1 can manifest.

## What the review changed (honesty log)

- **Dropped** the headline claim that activity-derived liveness "collapses the
  dedicated ping." It does not — `Age()` and `quietTicks` are independent
  (systems-review P1-1; OCR concurred). The real idle savings come from Mechanism 0
  (the flip) + Mechanism 1 (the self-trigger), not from feeding mtimes into `Age()`.
- **Demoted** activity-derived liveness to deferred fork **F5** with a "recommend
  not building" verdict and the four verified hazards (ping-independence,
  composer-wedge proxy, git-tree mtime spoof, future-mtime blind-spot).
- **Added** the OCR **H1** consistency fix: the base "Materiality-gated XO waking"
  requirement (which also names the tracker hash as a wake signal) is now MODIFIED
  alongside "Material change …" so the two cannot contradict post-archive.
- **Re-grounded** the single-writer claim as an *assumption pending §1
  verification* (review P2-3 / L3), not a standing fact.
- **Clarified** the window guarantee is v2-to-v2 (review M2).

## Test plan (TDD — for the build phase, after operator greenlight)

1. **Tracker self-trigger removed (Mechanism 1):** a tracker-hash change produces NO
   `WakeMaterial`; desk transitions and the signal-file still wake. (Pure predicate
   test, extends `materiality_test.go`.)
2. **External signal-file:** `--signal-file` hash change → exactly one
   `WakeMaterial` ("external signal"); unconfigured → inert; read error → unchanged
   (no wake-storm) — parity with `trackerHasher`'s fail-safe.
3. **Escalate-only-on-trigger contract (Mechanism 2):** a tick with none of the five
   triggers spends no XO turn; each trigger escalates with a trigger-naming prompt.
4. **Liveness untouched:** the existing detector liveness tests (wedge at
   `alertInterval×interval`, shell-immediate, snapshot fail-safe) stay green
   **unchanged** — proof the code did not go near the window.
5. `gofmt`/`go vet`/`go test -race ./...`/`openspec validate --strict`.

## Composition

Reuses the v2 detector loop, the `Injector`, the surface `Driver` (`Assess`), the
ack file, and the marker readers. Adds only: removal of the tracker-hash from the
*wake* set and an optional `--signal-file` wake source. No change to wedge/crash
thresholds, the ping, the snapshot fail-safe, or the relay path.
