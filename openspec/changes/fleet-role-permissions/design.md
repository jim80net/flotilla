# Design — fleet role permissions

**Focused desk** — role-based permission scheme for flotilla seats. Public-safe; generic role
names only. Sibling to `fleet-bootstrap-standup` (topology/doctor), not a sub-task of dash work.

**Status:** Design for COS review. Prototype JSON ships in this PR; compiler + bootstrap sync
follow in implementation PRs.

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
minimal prompts for leadership and hard constraints for desks.

---

## 2. Role classes and authority baseline

Aligns with `fleet_role` from bootstrap design (`cos` | `xo` | `adjutant` | `desk` |
`transient-task-desk`).

### 2.1 Leadership baseline (COS, XO)

| Capability | Allow unprompted | Deny / gate |
|---|---|---|
| **Message** | `flotilla send`, `flotilla notify` (coordinator secrets) | — |
| **Inspect fleet** | `flotilla status`, read roster/goals/backlog/detector snapshot | — |
| **Write fleet state** | touch ack/settled; append backlog; edit goals (role-scoped paths) | secrets write |
| **tmux / detector** | `tmux capture-pane`, read-only tmux list; `flotilla register` (bootstrap) | destructive tmux |
| **Git/gh read** | status, log, diff, pr view, checks | — |
| **Merge authority** | `gh pr merge` (independent-review seats only) | `git push` default branch, `+refspec`, force |
| **Deploy** | build/test/lint; restart user services **only** when roster-authorized deploy role | prod destructive |

### 2.2 Adjutant

| Capability | Allow | Deny |
|---|---|---|
| Read fleet state + buffer sidecars | yes | secrets |
| Mechanical triage, charter read/write | yes | `gh pr merge`, `flotilla notify` (unless explicitly delegated) |
| Buffer append paths | via watch, not direct pane spam | merge-completing git |

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
| Minimal leadership noise | Allow rules front-load `flotilla *`, `gh pr view`, reads; deny spine catches foot-guns |
| Constrained desks | Deny merge/push-to-main wins even under codex `never` / grok `bypassPermissions` (live-verified) |
| Easy audit | Single TOML per role + gatekeeper decision logs; regex reviewable in PR |
| Gatekeeper compat | **Native fit** — adapters already stable for all three harnesses |

**Cons:**

- Requires gatekeeper binary on PATH per seat (acceptable — already fleet standard).
- Codex needs hook trust (`--dangerously-bypass-hook-trust` or interactive trust once).
- Grok needs `/hooks-trust` on project dir.
- Native harness "allow lists" still needed for **prompt friction reduction** on leadership —
  gatekeeper allow auto-approves matched calls but unmatched calls still prompt in prompting mode.

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
| Minimal leadership noise | Good for Claude/Grok allows; Codex has **no** rich allow glob — rules/hooks only |
| Constrained desks | **Weak on grok always-approve** — CLI deny unreliable; codex needs hooks anyway for hard deny |
| Easy audit | Three files per seat; drift between `deploy/grok-*.json` and live settings |
| Gatekeeper compat | Duplicates deny logic already in gatekeeper.toml |

---

## 5. Decision matrix

| Criterion | Route A (gatekeeper + overlays) | Route B (native only) | **Recommended hybrid** |
|---|---|---|---|
| Public-safe canonical policy | Strong | Weak (3 schemas) | **Strong** — JSON in flotilla |
| Idempotent bootstrap | Strong (setup + stamp) | Partial (grok only) | **Strong** |
| Leadership low noise | Strong (allow rules) | Strong Claude/Grok; weak Codex | **Allow via native where effective; gatekeeper allow duplicates for codex** |
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
4. **Route B remains** the friction-reduction layer for Claude/Grok leadership allows; **Route A**
   is the authoritative deny + codex-hard-gate layer.

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

Leadership `xo` tier includes unprompted:

```text
flotilla status*, flotilla send*, flotilla notify*, flotilla register*
touch <roster-dir>/flotilla-*-alive, flotilla-*-settled
gh pr merge* (allow), git push origin main* (deny)
```

Desk tier denies `gh pr merge*`, `flotilla notify*`, default-branch push patterns.

---

## 7. Bootstrap integration (permissions slice)

Runs as a **subcommand of the permissions desk**, not folded into dash:

```bash
flotilla bootstrap permissions doctor --roster $R   # stamp + hook presence + drift
flotilla bootstrap permissions sync --agent alpha-xo --roster $R
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

---

## 10. References

- `github.com/jim80net/claude-gatekeeper` — canonical + adapters (Route A)
- `deploy/grok-coordinator-permission-allowlist.json`, `deploy/grok-permission-allowlist.json`
- `scripts/sync-grok-readonly-permissions.sh`
- `openspec/changes/fleet-bootstrap-standup/design.md` — topology, `fleet_role`, doctor
- `openspec/changes/codex-coordinator-seat/design.md` — codex coordinator posture
- `docs/coordinator-seat-swap-runbook.md` § Grok permission posture