# Design — fleet bootstrap / standup

Public-safe design for standing up a federated flotilla fleet: COS, meta-XO, **ops-xo** (fleet
operations accountability), **product XOs**, adjutants, and execution desks across Claude /
Codex / Grok (and other registered surfaces). Examples use `flotilla.example.json` names only.

**Authority boundary:** A **product XO** (e.g. `alpha-xo` on the flotilla product lane) owns
product-specific implementation — not fleet operations. **`ops-xo`** is accountable for
operational topology, permissions rollout, bootstrap/standup, roster hygiene, and rename
execution. Do not model product XOs as fleet-ops owners.

## 1. Topology invariant — every desk has an XO

**Invariant:** Every execution agent (`fleet_role: desk` or `transient-task-desk`) MUST be
reachable under exactly one supervising project-XO via federation bindings. The meta-XO
(`xo_agent`) and optional chief-of-staff (`cos_agent`) sit above project XOs.

**Corollary:** A roster agent that owns a channel (`xo_agent` on its home binding) but lists
only itself (or only coordinators as observers) is a **desk-home** or **solo mirror** channel,
not a coordinator — see `TestIsCoordinator_SoloDeskChannelNotCoordinator`. Apparent “orphan
desks” in the dash rail or detector snapshot are **topology-discovery debt**: missing or
mis-tagged `channels[]` edges, not a product-endorsed layout.

```mermaid
flowchart TB
  OP[Operator]
  COS[cos / chief-of-staff]
  META[xo / meta-XO]
  OPS[ops-xo / fleet-operations XO]
  PXO[alpha-xo / product XO]
  ADJ[alpha-adj / adjutant]
  DESK[alpha-desk / execution desk]

  OP -->|Discord relay| META
  OP -->|Discord relay| OPS
  OP -->|Discord relay| PXO
  META --> COS
  META --> OPS
  META --> PXO
  OPS -->|bootstrap permissions rename roster| OPS
  PXO --> ADJ
  PXO --> DESK
```

**Bootstrap doctor action:** For each `fleet_role: desk`, assert ∃ binding where
`xo_agent` is a coordinator and `members` contains the desk (or desk owns home channel with
parent coordinators listed per visibility doc). Fail with `TOPOLOGY_MISSING_XO` and name the
desk + suggested binding shape — do not auto-mutate roster.

## 2. Explicit fleet role metadata

Today `IsCoordinator` is **derived** from bindings. Bootstrap and permissions need an
**explicit** role on each `agents[]` entry, validated at roster load against derived truth.

### 2.1 Proposed field

```jsonc
{
  "name": "alpha-xo",
  "surface": "codex",
  "fleet_role": "xo"   // NEW — explicit bootstrap/permission class
}
```

| `fleet_role` | Meaning | Permission class | Doctrine install |
|---|---|---|---|
| `cos` | Chief-of-staff (`cos_agent`) | `leadership` | coordinator + identity-append |
| `meta-xo` | Fleet command coordinator (`xo_agent` clock target) | `leadership` | coordinator + identity-append |
| `ops-xo` | **Fleet operations** — bootstrap, permissions, rename, roster hygiene, topology | `leadership-ops` | coordinator + ops charter |
| `xo` | **Product/project XO** — implementation lane only; **not** fleet-ops owner | `leadership` | coordinator + identity-append |
| `adjutant` | `adjutant_for` mechanical seat | `leadership-adjutant` | adjutant charter path |
| `desk` | Long-lived execution desk | `desk-<lane>` | execution backstop |
| `transient-task-desk` | Short-lived / PR-scoped desk | `desk-transient` | execution + recycle hints |

### 2.2 Ops-xo vs product XO (authority boundary)

| Seat | Accountable for | NOT accountable for |
|---|---|---|
| **`ops-xo`** | Bootstrap/standup, permissions sync, rename execution, roster hygiene, topology doctor, fleet state root writes | Product feature implementation, product desk IC work |
| **`xo` (product)** | Product desks, product goals/backlog, product PR lanes | Fleet-wide permissions rollout, rename waves, roster schema migrations |
| **`meta-xo`** | Fleet command span, heartbeat clock, federation routing | May delegate fleet-ops to `ops-xo` — meta does not substitute for ops accountability |

**Provision before implementation:** A dedicated **`ops-xo` seat** SHOULD be provisioned
before bootstrap doctor, permissions compiler, or rename cutover waves land. Small fleets MAY
co-locate `ops-xo` and `meta-xo` on one human operator channel but MUST use distinct
`fleet_role` tags and roster names (`xo` vs `ops-xo`) so permissions and audit do not conflate
product work with fleet operations.

**Validation (fail-closed at `roster.Load`):**

- `fleet_role: cos` ⇒ `agents[].name` of that row equals roster-level `cos_agent` when set.
- `fleet_role: meta-xo` ⇒ name equals `xo_agent` when this daemon is the primary clock target.
- `fleet_role: ops-xo` ⇒ `IsCoordinator(name)` and MUST NOT be tagged `fleet_role: xo` on the
  same row (mutually exclusive product vs ops class).
- `fleet_role: xo` (product) ⇒ `IsCoordinator(name)`; bootstrap/rename/permissions **tasks**
  default assignee is `ops-xo`, not product `xo`.
- `fleet_role: adjutant` ⇒ `adjutant_for` set and target is coordinator.
- `fleet_role: desk` | `transient-task-desk` ⇒ NOT `IsCoordinator(name)`.
- Absent `fleet_role` ⇒ legacy mode: derive for doctor warnings only.

**Relation to `coordinator: true`:** If present, `fleet_role` wins; `coordinator` boolean
becomes redundant and is deprecated over one roster generation.

### 2.3 Live-expected predicate

Doctor checks B006/B007 apply only to agents marked **live-expected**:

```jsonc
{ "name": "alpha-xo", "fleet_role": "xo", "live_expected": true }
```

| Rule | Definition |
|---|---|
| **Explicit** | `live_expected: true` on `agents[]` row |
| **Implicit (legacy)** | Agent is `xo_agent`, `cos_agent`, or listed as `xo_agent` on any `channels[]` binding |
| **Default** | `live_expected: false` — doctor skips pane-marker checks |

Absent field ⇒ derive implicit rule; emit `LIVE_EXPECTED_DERIVED` info finding when implicit.

### 2.4 Adjutant laminar flow (product requirement)

Bootstrap MUST configure adjutant seats for **laminar leader flow** — the adjutant triages and
buffers non-urgent layer interrupts; the leader (COS / meta-xo / ops-xo / product `xo`) sees a
**consolidated brief at a natural seam**, not mid-thought interjects. Builds on
`openspec/changes/stackable-flotillas-438` (#439); this section captures operator corrections
for bootstrap/standup.

#### Protected windows — adjutant MUST NOT interject to leader

**Mechanical enforcement (load-bearing):** Protected-window suppression MUST be implemented in
watch (`OperatorProtectedWindow` gate before `drainAdjutantSeamFor`) — not prompt-contract alone.
Full detection sources, fail-safe, tests, goal-loop composition:
`openspec/changes/adjutant-operator-protected-window/`.

| Window | Mechanical signal (v1) | Adjutant behavior |
|---|---|---|
| **Operator typing** | Pending `flotilla-relay-queue.json` entry for leader; optional dash bridge compose-active | Buffer; watch suppresses seam inject |
| **Operator active conversation** | `flotilla-<leader>-awaiting` present; active-conversation tail after confirmed relay | Buffer; no leader interject |
| **Leader mid-compose (non-operator)** | Leader `Working` without operator signals above | Buffer non-urgent; seam at idle/settled/evaluation TTL |

These are **operator-typing / active-conversation** protected windows — distinct from
**machine-idle seams** (below). Leader `Working` alone is **not** an operator protected window.

#### Machine-idle seams — injection allowed

| Seam | Signal | Adjutant behavior |
|---|---|---|
| Post-turn idle | Leader `Working→Idle` edge | Inject consolidated brief |
| Settled | Leader idle + ack/settled consumed | Inject brief |
| Evaluation tick | Stale-leader timeout (#439) during **active goal loop** | Ack → evaluate → act-by-tier; do **not** wait for perfect long idle |

**Anti-pattern (forbidden):** Waiting indefinitely for “perfect idle” while the fleet is in an
active goal loop. The adjutant evaluation tick exists precisely to avoid buffer starvation when
the leader stays `Working` on legitimate work.

#### Urgent bypass — skip buffer, deliver to leader immediately

Only these classes cut through the adjutant buffer (align with operating-principles gates +
safety):

| Class | Examples |
|---|---|
| **Money** | New/unaffirmed metered spend, account top-up |
| **Irreversible** | Destructive / no-clean-rollback actions |
| **Divergent fork** | Mutually exclusive approaches requiring operator choice |
| **Incident / safety** | Fleet safety, data-loss risk, detector liveness failure |
| **Officer incapacitation** | Usage-limit downgrade, coordinator unresponsive, stale ack beyond policy |

Configure via roster `urgent_windows[]` (substring match on material reason) plus built-in
**operator relay** passthrough (`KindRelay` — never buffered). Bootstrap doctor check **B011**
verifies adjutant bindings have `urgent_windows` documented when `adjutant_for` is set.

#### Bootstrap artifacts for adjutant lanes

| Path | Purpose |
|---|---|
| `flotilla-<leader>-buffer.json` | Durable non-urgent queue (watch-owned) |
| `flotilla-<leader>-adjutant-charter.md` | Solo-authority tier + seam policy |
| `flotilla-<leader>-buffer-delivered.json` | Consumed-item dedup ledger (#469) |

Validation **V9** (when adjutant configured): non-urgent desk finish buffers; operator relay
reaches leader without adjutant delay; urgent-class reason bypasses buffer per table above.

Validation **V9c**: pending operator relay for leader ⇒ finish-edge seam does **not** inject
adjutant consolidated brief to leader pane (buffer retained until window clears).

Doctor **B011a**: when adjutant configured, verify watch build includes `OperatorProtectedWindow`
seam gate (not prompt-only).

### 2.5 Loop-aware status taxonomy (product requirement)

Plain pane **idle** is NOT an adequate autonomous-fleet loop state — it conflates inactive /
out-of-loop with legitimate in-loop postures (between turns, parked, maintaining, refining,
cleaning). Bootstrap and standup MUST surface a **loop posture** distinct from `surface.State`.

Full taxonomy + derivation: `openspec/changes/loop-aware-status-taxonomy/` (#524).
Implementation: `internal/loopposture` (Derive, ParkStrict default, LoopObserver seam).

| Layer | Field | Officer question |
|---|---|---|
| Pane | `state` | What does the harness show? (`working`, `idle`, `crashed`, …) |
| Loop | `loop_posture` | Is this seat properly in the coordination loop? |

v1 in-loop postures: `composing`, `available`, `parked`, `awaiting-authority`, `blocked`,
optional `maintaining` / `refining` / `cleaning` (+ `goal-active` when native observer reports).
Out-of-loop: `drifted`, `crashed`, `reaped`, `unknown`.

**Parked default: strict** — empty unblocked backlog required; settled+unblocked ⇒ `drifted`.

Doctor **B012**: every `live_expected` agent has derivable `loop_posture` when snapshot fresh;
stale/absent ⇒ `unknown` / `LOOP_POSTURE_UNKNOWN` on live seats. Derivation is in
`internal/loopposture`; runnable bootstrap doctor check is a follow-on.

Validation **V10**: `flotilla status --json` distinguishes `available` vs `parked` vs `drifted`
vs `awaiting-authority` on generic fixtures (covered by `TestBuildStatusJSON_LoopPostureV10`
and `TestDerive_V10Distinguishes`).

## 3. Naming convention — `{identifier}-{role}`

Human and machine readability for federated fleets:

| Pattern | Example | Notes |
|---|---|---|
| fleet ops | `ops-xo`, `ops-adj` | Fleet operations accountability |
| meta | `xo`, `xo-adj` | Fleet command coordinator |
| `{product}-xo` | `alpha-xo` | **Product** XO — implementation lane |
| `{product}-adj` | `alpha-adj` | Adjutant for product XO |
| `{product}-desk` | `alpha-desk` | Stable execution desk |
| `{product}-desk-{scope}` | `alpha-desk-pr123` | Transient task desk |

**Rules:**

- `name` == tmux marker == `FLOTILLA_SELF` (unless `tmux_title` override documented).
- Transient desks SHOULD encode scope in the name (`-pr123`, `-spike-foo`) and
  `fleet_role: transient-task-desk` for permission tier + recycle policy.
- Identifier is organizational (project codename), not a deployment host name.

## 4. Permission shape — leadership vs desks

Bootstrap selects a **permission template** from `deploy/` by `fleet_role` + `surface`:

| Class | Talk to whom | Fleet state R/W | Typical unprompted allows |
|---|---|---|---|
| **Leadership** (`cos`, `meta-xo`, `ops-xo`, product `xo`) | Other coordinators, operator relay, adjutant | Roster dir, backlog, goals, session-mirror; **ops-xo** writes roster hygiene paths | Zero-prompt: `flotilla notify/send/status`, gate, merge (reviewer seats), deploy/reap per `fleet-role-permissions` §0 |
| **Adjutant** | Parent coordinator layer | Buffer sidecars, charter, read roster/goals | Laminar flow only (§2.4); mechanical triage; no merge authority |
| **Desk** | Parent XO only (send path) | Worktree + lane artifacts | Tests, lint, branch push to feature branches; deny merge-to-default |
| **Transient desk** | Parent XO | Same as desk, narrower path globs | Stricter write surface; time-bounded |

**Existing assets (reuse, do not fork):**

- `deploy/grok-coordinator-permission-allowlist.json` — Grok **leadership**
- `deploy/grok-permission-allowlist.json` — Grok **execution**
- Codex coordinator rules — `openspec/changes/codex-coordinator-seat/design.md`
- Claude — `watch-runbook.md` § XO permission posture; desk templates via gatekeeper + settings

**Gatekeeper posture (all classes):** `on_gatekeeper_error: abstain` — documented in
coordinator template; bootstrap copies templates, does not invent per-host deny lists ad hoc.

**Zero approval noise:** Role-authorized leadership flows proceed **without per-command harness
approvals** — see `openspec/changes/fleet-role-permissions/design.md` §0 (PR #521). Desks use
**prompting** or `--always-approve` per lane policy with gatekeeper deny for merge-to-default.

## 5. State root — layout and permissions

Bootstrap treats `<roster-dir>` (directory containing `flotilla.json`) as the **fleet state
root**. Idempotent setup ensures:

| Path | Owner write | Leadership R/W | Desk R/W |
|---|---|---|---|
| `flotilla.json` | operator | read | read |
| `flotilla-secrets.env` | operator | read (env inject) | none |
| `.flotilla-state.md` / backlog | leadership | read/write | read scoped |
| `fleet-goals.json` | leadership | read/write | read |
| `flotilla-detector-state.json` | watch daemon | read | read |
| `flotilla-xo-alive` / per-layer ack | coordinator seat | write (touch) | none |
| `session-mirror/` | watch + seats | read | read (own agent file) |
| `flotilla-<agent>-buffer.json` | watch | read (adjutant) | none |

Bootstrap **does not** chmod secrets world-readable; doctor checks permissions are not group/other
writable. Host-local only — never committed.

## 6. Tmux / flotilla marker — avoid detector orphans

A seat is **detector-visible** when ALL hold:

1. **Roster entry** — agent name in `agents[]`.
2. **Pane marker** — `@flotilla_agent=<name>` via `flotilla register <name>` in the launch line
   (same line as `exec <harness>`).
3. **Launch env** — `FLOTILLA_SELF=<name>`; **coordinators only** (cos, meta-xo, ops-xo,
   product xo, adjutant) also `FLOTILLA_SECRETS=<path>`. Desks MUST NOT export secrets.
4. **Watch enrollment** — roster `change_detector: true` and `heartbeat_interval` set; daemon
   running and writing `flotilla-detector-state.json`.
5. **Surface registered** — `surface` field matches a driver the watch process loaded.

**Codex/Grok coordinator orphan pattern:** Seat runs in tmux but snapshot omits it because (a)
marker missing after `exec`, (b) `FLOTILLA_SELF` unset so notify/send provenance breaks, or (c)
watch started before surface driver registered. Bootstrap launch recipe MUST be:

```bash
tmux send-keys -t <session> \
  'export FLOTILLA_SELF=alpha-xo FLOTILLA_SECRETS=$ROSTER_DIR/flotilla-secrets.env && flotilla register alpha-xo && exec codex' Enter
```

**Idempotent check:** `flotilla bootstrap doctor --roster <path>` (proposed) verifies marker
via `flotilla status --json` / pane probe and compares to roster agent set.

## 7. Idempotent bootstrap doctor (proposed CLI)

New subcommand family: `flotilla bootstrap` with exit codes suitable for CI / agent loops.

| Check ID | Condition | Severity |
|---|---|---|
| `B001` | `go` + `tmux` on PATH | fail |
| `B002` | Roster loads; federation acyclic | fail |
| `B003` | Every desk has supervising XO binding | fail |
| `B004` | `fleet_role` consistent with `IsCoordinator` | warn→fail |
| `B005` | `change_detector` + liveness mode when adjutant present | fail |
| `B006` | Each roster agent: pane marker OR not expected live | warn |
| `B007` | `FLOTILLA_SELF` in launch recipe for live seats | warn |
| `B008` | Detector snapshot fresh (< 3× heartbeat) | warn |
| `B009` | Permission template synced for seat surface+role | warn |
| `B010` | `flotilla register` would succeed (dry-run pane list) | info |
| `B011` | When `adjutant_for` set: buffer path writable; `urgent_windows` or defaults documented | warn |
| `B011a` | When `adjutant_for` set: watch build includes `OperatorProtectedWindow` seam gate (not prompt-only) | warn→fail |
| `B012` | Every `live_expected` agent has derivable `loop_posture` when snapshot fresh (§2.5) | warn |

**Idempotence:** `bootstrap apply` (future) only writes scaffold files that are missing or
older than repo template version; never overwrites operator-edited secrets or roster without
`--force`. `bootstrap doctor` is read-only.

## 8. Bootstrap skill workflow (agent-facing)

The `.claude/skills/flotilla-fleet-bootstrap/SKILL.md` skill orchestrates:

1. **Discover** — read roster; classify agents by `fleet_role` (or derive + warn).
2. **Topology audit** — list desks missing XO; emit binding snippets (generic example).
3. **State root** — verify roster dir permissions and required sidecar paths.
4. **Seat recipes** — for each live agent, emit harness-specific launch line with register +
   env exports.
5. **Permissions** — copy/sync correct `deploy/*-permission-allowlist.json` tier into worktree
   `.claude/settings.local.json` or Grok/Codex equivalent.
6. **Watch** — confirm `change_detector`; restart watch if new surface registered.
7. **Validate** — run minimal validation plan (§9); surface failures to COS, not operator spam.

## 9. Minimal validation plan

Run after bootstrap (manual or skill-driven). Any step failure blocks “fleet standup complete.”

| Step | Action | Pass |
|---|---|---|
| V1 | `flotilla bootstrap doctor --roster $R` | no fail-severity findings |
| V2 | `flotilla status --json` | every expected-live agent non-unknown state |
| V3 | Detector snapshot age | fresh within 3× heartbeat |
| V4 | Operator relay | bare message in fleet-command → meta-XO pane |
| V5 | Cross-seat send | `flotilla send --from xo backend "ping"` → delivered |
| V6 | Coordinator outbound | XO `flotilla notify` → Discord (or webhook dry-run) |
| V7 | Ack path | XO touches ack file; watch clears liveness alert |
| V8 | Permission smoke | coordinator `gh pr view` unprompted; desk `gh pr merge` blocked or prompted per policy |
| V9 | Adjutant laminar flow | non-urgent buffered at leader `Working`; operator relay immediate; seam inject at idle edge |

Transient desks: V4–V7 optional; V1–V3 + parent XO send required. V9 when any `adjutant_for` binding exists.

## 10. Implementation phases

See `tasks.md`. Summary:

1. **Design + skill stub** (this PR) — docs, spec, skill pointer.
2. **Roster schema** — `fleet_role` field + validation tests.
3. **`flotilla bootstrap doctor`** — read-only checks B001–B010.
4. **Permission sync script** — `scripts/bootstrap-sync-permissions.sh` per surface.
5. **`bootstrap apply`** — scaffold launch snippets, settings.local.json from templates.
6. **`llm.md` § Fleet bootstrap** — link skill + doctor.

## 11. References

- `flotilla.example.json` — generic federation topology
- `docs/federation.md`, `docs/coordinator-seat-swap-runbook.md`
- `openspec/changes/codex-coordinator-seat/design.md`
- `deploy/grok-coordinator-permission-allowlist.json`, `deploy/grok-permission-allowlist.json`
- `llm.md` § register + `docs/watch-runbook.md` § prerequisites
- `internal/roster/roster.go` — `IsCoordinator`, adjutant validation
- `openspec/changes/stackable-flotillas-438/design.md` — adjutant laminar flow (#439)