# Design — fleet role permissions

**Focused desk** — role-based permission scheme for flotilla seats. Public-safe; generic role
names only. Sibling to **fleet bootstrap/standup** (topology/doctor) — ships in
[PR #520](https://github.com/jim80net/flotilla/pull/520) (`design/fleet-bootstrap-standup-clean`);
path `openspec/changes/fleet-bootstrap-standup/design.md` is valid **after #520 merges**. Until
then, treat bootstrap §2 (ops-xo vs product XO) as the paired contract. Not a sub-task of dash work.

**Status:** Design for COS review. Prototype JSON ships in this PR; compiler + bootstrap sync
follow in implementation PRs.

---

## 0. Design criteria — autonomous fleet (operator correction)

**Target:** **Zero approval noise** for role-authorized fleet operation — not merely low noise.
The steady-state fleet is **autonomous**: normal COS / XO / adjutant work proceeds **without
per-command harness approvals** when the role authorizes the action.

### 0.1 Role-authorized flows (zero-prompt when permitted)

| Flow class | Examples (leadership) | Desk |
|---|---|---|
| **Communication** | `flotilla send`, `flotilla notify`, coordinator relay | `flotilla send` to parent XO only |
| **State read/write** | roster, backlog, goals, session-mirror read; ack/settled `touch`; buffer sidecars | lane worktree + goals read |
| **Status / inspect** | `flotilla status`, tmux list/capture, detector snapshot read | `flotilla status`, lane git read |
| **Dispatch** | `flotilla send <desk>`, schedules, dropped-dispatch resume | receive only (no notify) |
| **Gate / review** | `gh pr view/diff/checks/review`, CI read, independent review reads | same read tier; no merge |
| **Merge** | `gh pr merge` on reviewer seats (hierarchy-relative; **no self-merge**) | **deny** unless `elevation.merge` |
| **Deploy** | build/test/lint; roster-authorized service restart | build/test in lane |
| **Reap** | `flotilla recycle`, rotate/handoff orchestration, `flotilla register` | recycle self; no fleet-state write |

**Pass criterion:** A coordinator executing a full heartbeat cycle (status → send desks → touch
ack → notify operator surface) SHALL NOT surface a harness approval modal for any step in the
allowed set.

### 0.2 Safety without per-command prompting

Safety does **not** come from prompting the operator or COS on every normal command. It comes
from:

| Control | Mechanism |
|---|---|
| **Role boundaries** | `fleet_role` + canonical policy; desk cannot merge/notify/push-default |
| **No self-merge** | Doctrine + deny patterns; merge authority is hierarchy-relative |
| **Lane scoping** | desk worktree globs; transient desks narrower |
| **Audit logs** | session-mirror, Discord mirror, gatekeeper decision log |
| **Reversible / idempotent ops** | bootstrap sync, doctor, touch ack, send with confirm layer |
| **Operator gates (only)** | money spend, irreversible/destructive, genuine divergent forks |

Full Access and per-command approval storms are **failure modes** to eliminate — not the
steady-state control plane.

### 0.3 Materialization implication

Hybrid A′ MUST materialize **both** gatekeeper allow rules **and** native auto-approve tiers so
unmatched-call prompting cannot block authorized leadership flows. For Codex/Grok coordinators,
native `approval_policy=never` / `--always-approve` is acceptable **only** when gatekeeper deny
spine + role overlay fully constrain the seat — never as an unscoped escape hatch.

---

## 1. Problem

| Pain today | Root cause |
|---|---|
| Codex coordinator spammed with approvals | No role-tier allow surface; Full Access is the escape hatch |
| Grok desk `gh pr merge` runs under `--always-approve` | Native CLI deny unreliable; gatekeeper deny is the real block |
| Claude XO merge allowed but push-to-main needs regex | Glob permissions miss env-prefix / refspec shapes |
| Three harnesses, three JSON blobs in `deploy/` | No single auditable role matrix |
| Bootstrap copies wrong template | `fleet_role` + `surface` → template mapping not formalized |

**Goal:** One auditable **canonical role policy** materialized idempotently per harness, with
**zero approval noise** for role-authorized leadership/adjutant flows and hard constraints for
desks (§0).

---

## 2. Role classes and authority baseline

Aligns with `fleet_role` from bootstrap design (PR #520 §2): `cos` | `meta-xo` | `ops-xo` |
`xo` (product/project XO) | `adjutant` | `desk` | `transient-task-desk`.

**Authority boundary:** Product XOs (`fleet_role: xo`) are **not** accountable fleet-ops owners.
`ops-xo` holds bootstrap, permissions sync, rename, and roster-hygiene authority. Provision
`ops-xo` before implementation waves (bootstrap §2.2).

### 2.1 Leadership baseline (COS, meta-xo, ops-xo, product xo)

| Capability | Allow unprompted (§0) | Deny / gate |
|---|---|---|
| **Message** | `flotilla send`, `flotilla notify` (coordinator secrets) | — |
| **Inspect fleet** | `flotilla status`, read roster/goals/backlog/detector snapshot | — |
| **Write fleet state** | touch ack/settled; append backlog; edit goals (role-scoped) | secrets write |
| **Fleet ops** (`ops-xo` only) | `flotilla bootstrap*`, permissions sync, rename doctor, roster hygiene writes | product XO lacks by default |
| **tmux / detector** | `tmux capture-pane`, read-only tmux list; `flotilla register` | destructive tmux |
| **Git/gh read** | status, log, diff, pr view, checks, review | — |
| **Merge authority** | `gh pr merge` (independent-review seats only) | `git push` default branch, `+refspec`, force |
| **Deploy** | build/test/lint; roster-authorized service restart | prod destructive |

Product `xo` inherits leadership baseline **without** fleet-ops write paths unless explicitly
elevated (audited). `ops-xo` extends leadership with fleet-ops capabilities in canonical JSON.

### 2.2 Adjutant (separate tier — not leadership)

**Laminar flow contract (operator product requirement):** Adjutants buffer non-urgent layer
interrupts and inject at **machine-idle seams** only. They MUST NOT interject into the leader
pane during **operator typing** or **operator↔leader active conversation** protected windows.
Urgent bypass (skip buffer, leader immediate): money, irreversible, divergent fork,
incident/safety, officer incapacitation/usage-limit, and operator relay. MUST NOT wait
indefinitely for perfect idle during an active goal loop — evaluation tick applies. Full policy:
bootstrap design §2.4 (PR #520) + `stackable-flotillas-438`.

| Capability | Allow | Deny |
|---|---|---|
| Read fleet state + buffer sidecars | yes | secrets |
| Mechanical triage, charter read/write | yes | `gh pr merge`, `flotilla notify` to leader during protected window |
| Buffer append paths | via watch, not direct leader pane spam | merge-completing git |
| Seam injection | consolidated brief at idle/settled/evaluation tick | mid-thought interject |

### 2.3 Desk (execution + transient)

| Capability | Allow | Deny |
|---|---|---|
| Lane worktree R/W | yes (scoped glob) | roster dir write, secrets |
| Tests, lint, feature-branch git | yes | `gh pr merge`, push default branch, force push |
| `flotilla send` to parent only | via harness discipline | `flotilla notify` |
| Elevated desk | explicit `fleet_role` + `elevation: merge` rare | default |

**Invariant:** Desks do not hold merge-completing powers unless `elevation` is set in canonical
policy (audited, operator-approved).

---

## 3. Route A — gatekeeper core + harness adapters

**Architecture (already shipped in `jim80net/claude-gatekeeper`):**

```
canonical ToolCall + Verdict
        ↑ adapter (claude | codex | grok)
        ↑ engine evaluates layered TOML rules
deploy/flotilla-permissions/overlays/<fleet_role>.toml
        + gatekeeper.toml (global deny spine)
        + project .gatekeeper/gatekeeper.toml (optional)
```

**Memex-style split:**

| Layer | Owner repo | Contents |
|---|---|---|
| **Core engine** | `claude-gatekeeper` | `canonical`, `engine`, PCRE2 rules, `on_error` posture |
| **Harness adapters** | `claude-gatekeeper` | `adapter/claude`, `adapter/codex`, `adapter/grok` |
| **Fleet role overlays** | `flotilla` | `deploy/flotilla-permissions/overlays/*.toml` generated from `canonical-roles.json` |
| **Bootstrap materializer** | `flotilla` | `flotilla bootstrap permissions sync` → hook install + overlay copy |

**Pros:**

| Criterion | Score |
|---|---|
| Public-safe | One policy in flotilla repo; no host paths in rules |
| Idempotent bootstrap | `claude-gatekeeper setup --harness <h>` + overlay copy with version stamp |
| Zero leadership approval noise | Allow rules + native auto-approve cover full §0.1 flow set; deny spine catches foot-guns |
| Constrained desks | Deny merge/push-to-main wins even under codex `never` / grok `bypassPermissions` (live-verified) |
| Easy audit | Single TOML per role + gatekeeper decision logs; regex reviewable in PR |
| Gatekeeper compat | **Native fit** — adapters already stable for all three harnesses |

**Cons:**

- Requires gatekeeper binary on PATH per seat (acceptable — already fleet standard).
- Codex needs hook trust (`--dangerously-bypass-hook-trust` or interactive trust once).
- Grok needs `/hooks-trust` on project dir.
- Native harness allow lists are **required** for zero-prompt leadership — gatekeeper allow alone
  does not suppress all harness UI friction; both layers must cover §0.1 authorized flows.

---

## 4. Route B — native per-harness permission config

Materialize role policy **only** in harness-native stores:

| Harness | Native allow surface | Native deny surface | Hook seam |
|---|---|---|---|
| **Claude** | `.claude/settings.local.json` `permissions.allow[]` | same `deny[]` | optional PreToolUse hook |
| **Grok** | launch `--allow` flags + `settings.local.json` | `--deny` (hard only in prompting mode) | `~/.grok/hooks/gatekeeper.json` |
| **Codex** | `approval_policy` + project rules | `.codex/rules` deny prose; **no** glob array like Claude | `~/.codex/hooks.json` PreToolUse |

**Pros:**

- No extra binary for agents that lack gatekeeper install.
- Claude operators already understand `settings.local.json`.
- Grok `--always-approve` desks stay fast when combined with branch protection only.

**Cons:**

| Criterion | Score |
|---|---|
| Public-safe | Possible but **three divergent schemas** to keep in sync |
| Idempotent bootstrap | `sync-grok-readonly-permissions.sh` exists; Claude/Codex lack unified sync |
| Zero leadership approval noise | Good for Claude/Grok allows; Codex needs hook+rules coverage for full §0.1 set |
| Constrained desks | **Weak on grok always-approve** — CLI deny unreliable; codex needs hooks anyway for hard deny |
| Easy audit | Three files per seat; drift between `deploy/grok-*.json` and live settings |
| Gatekeeper compat | Duplicates deny logic already in gatekeeper.toml |

---

## 5. Decision matrix

| Criterion | Route A (gatekeeper + overlays) | Route B (native only) | **Recommended hybrid** |
|---|---|---|---|
| Public-safe canonical policy | Strong | Weak (3 schemas) | **Strong** — JSON in flotilla |
| Idempotent bootstrap | Strong (setup + stamp) | Partial (grok only) | **Strong** |
| Leadership zero approval noise | Strong (allow rules) | Strong Claude/Grok; weak Codex | **Dual-layer allow — native + gatekeeper — until Codex parity; doctor fails on gaps** |
| Desk hard constraints | **Strong** (hook deny) | Weak grok/codex auto | **Strong** |
| Auditability | Strong | Fragmented | **Strong** — canonical JSON + generated artifacts |
| Gatekeeper compat | Native | Forks deny rules | **Extends gatekeeper, does not fork** |
| Implementation cost | Medium (compiler) | High (3 ongoing sync paths) | Medium |

### Recommendation: **Hybrid A′** (canonical core → gatekeeper + native materialization)

1. **`deploy/flotilla-permissions/canonical-roles.json`** — versioned source of truth (this PR).
2. **Compiler** (Phase 2) emits:
   - `overlays/<role>.toml` for gatekeeper (deny spine + role allows)
   - `materialized/claude/<role>.permissions.json` fragment
   - `materialized/grok/<role>.allowlist.json` (extends existing deploy shape)
   - `materialized/codex/<role>.rules` snippet + hook pointer doc
3. **`flotilla bootstrap permissions sync --role <fleet_role> --surface <surface>`** — idempotent:
   - runs `claude-gatekeeper setup --harness <surface>`
   - copies overlay into `~/.config/gatekeeper/overlays/` or project `.gatekeeper/`
   - merges native allow fragment into worktree settings
   - writes `flotilla-permissions-sync.stamp` (role, surface, schema_version)
4. **Route B** supplies native auto-approve for Claude/Grok leadership; **Route A** supplies
   authoritative deny + codex-hard-gate + allow spine. **Both** are required for §0 zero-noise
   steady state — not optional friction reduction.

**Not recommended:** Native-only (Route B) as sole scheme — grok always-approve and codex
`approval_policy=never` proved CLI/settings deny is not a reliable desk constraint.

---

## 6. Canonical prototype (`deploy/flotilla-permissions/canonical-roles.json`)

Ships in this PR as the auditable contract. Fields:

- `schema_version` — bump on breaking changes; bootstrap doctor checks stamp
- `roles.<fleet_role>.capabilities` — machine tags (`flotilla.notify`, `gh.merge`, …)
- `roles.<fleet_role>.bash_allow[]` / `bash_deny[]` — glob patterns (materialize to harness)
- `roles.<fleet_role>.elevation` — optional desk elevation flags
- `policy.on_gatekeeper_error` — `abstain` (fleet default)
- `policy.design_criteria` — machine tag consumed by compiler + doctor (not decorative metadata)

**`design_criteria` consumer (Phase 1 compiler):** `scripts/compile-flotilla-permissions.sh`
SHALL read `policy.design_criteria` and:

1. Emit a comment header in every generated overlay/materialized artifact citing the criteria
2. Fail compile if value ≠ `zero_approval_noise_for_role_authorized_ops` (schema mismatch guard)
3. Emit `flotilla-permissions-sync.stamp` field `design_criteria` for doctor drift checks

`flotilla bootstrap permissions doctor` SHALL fail `PERM_DESIGN_CRITERIA_DRIFT` when on-disk
stamp disagrees with canonical `policy.design_criteria`.

Leadership tiers (`meta-xo`, `ops-xo`, product `xo`, `cos`) include **zero-prompt** (§0.1):

```text
flotilla status*, flotilla send*, flotilla notify*, flotilla register*, flotilla recycle*
touch <roster-dir>/flotilla-*-alive, flotilla-*-settled
gh pr view/diff/checks/review/merge* (merge allow on reviewer seats)
git push origin main* (deny)
go test*, go build*, make test* (deploy lane)
```

Doctor check **P009**: any §0.1 leadership flow missing from compiled allow set ⇒ `PERM_AUTONOMY_GAP`.

Desk tier denies `gh pr merge*`, `flotilla notify*`, default-branch push patterns.

---

## 7. Bootstrap integration (permissions slice)

Runs as a **subcommand of the permissions desk**, not folded into dash:

```bash
flotilla bootstrap permissions doctor --roster $R   # stamp + hook presence + drift
flotilla bootstrap permissions sync --agent ops-xo --roster $R
```

**Idempotence:** sync no-ops when `flotilla-permissions-sync.stamp` matches `schema_version` +
role + surface. `--force` regenerates from canonical JSON.

**Detector orphan prevention (permissions angle):** leadership tier MUST allow:

- `flotilla register <self>`
- `touch` on layer ack paths
- `flotilla status`

Bootstrap doctor fails if coordinator role policy denies these (check P001–P003 in spec).

**State-root paths** (read/write by role): see bootstrap design §5; permissions compiler emits
path-scoped Read/Write rules where harness supports it (Claude Read/Write tools; bash rm deny).

---

## 8. Implementation path

| Phase | Deliverable | Repo |
|---|---|---|
| **0** | This design + `canonical-roles.json` + skill stub | flotilla |
| **1** | `scripts/compile-flotilla-permissions.sh` → overlays + grok JSON | flotilla |
| **2** | `flotilla bootstrap permissions {doctor,sync}` | flotilla |
| **3** | Gatekeeper: optional `flotilla` overlay include path in docs | claude-gatekeeper |
| **4** | Replace hand-maintained `deploy/grok-*-permission-allowlist.json` with generated | flotilla |
| **5** | COS validation on live codex coordinator seat (supervised trial) | operator host |

---

## 9. Validation plan (permissions-specific)

| ID | Check |
|---|---|
| P1 | Leadership: `flotilla status` unprompted |
| P2 | Leadership: `touch` ack file unprompted |
| P3 | Leadership: `flotilla notify` dry-run / webhook test |
| P4 | Desk: `gh pr merge` **blocked** under auto-approve (hook deny) |
| P5 | Desk: `git push origin main` blocked |
| P6 | Coordinator: `gh pr merge` allowed on trial PR |
| P7 | `bootstrap permissions doctor` — no drift vs canonical |
| P8 | Codex coordinator: hook deny canary under `approval_policy=never` |
| P9 | Full heartbeat cycle (status → send → touch ack → notify): **zero** approval modals |
| P10 | Adjutant triage path (status + buffer read + charter write): zero approval modals |
| P11 | `bootstrap permissions doctor` reports no `PERM_AUTONOMY_GAP` for leadership roles |

**Autonomy regression:** Any harness upgrade that re-introduces per-command prompts on §0.1
flows is a **release blocker** for coordinator seats until allow materialization is patched.

---

## 10. References

- `github.com/jim80net/claude-gatekeeper` — canonical + adapters (Route A)
- `deploy/grok-coordinator-permission-allowlist.json`, `deploy/grok-permission-allowlist.json`
- `scripts/sync-grok-readonly-permissions.sh`
- `openspec/changes/fleet-bootstrap-standup/design.md` (PR #520, pending merge) — topology,
  `fleet_role`, ops-xo boundary, doctor
- `openspec/changes/codex-coordinator-seat/design.md` — codex coordinator posture
- `docs/coordinator-seat-swap-runbook.md` § Grok permission posture