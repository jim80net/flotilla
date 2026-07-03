# Design — codex coordinator seat v1

**Status:** Design for COS review. Builds on merged codex execution driver (#259, `bfe6f4f9`).
**Goal:** A codex-harness agent can hold a coordinator seat (XO/CoS) with parity on outbound
operator comms, detector-driven loop, gate workflows, and long-lived rotate/recycle — gated by a
supervised trial of one low-stakes project XO.

**Reference:** `internal/surface/codex.go`, `docs/xo-doctrine.md`, `docs/inter-harness.md`,
`openspec/changes/codex-surface-driver/design.md`.

---

## 1. Gap analysis — execution driver vs coordinator seat

| Capability | Execution desk (#259) | Coordinator seat (this design) |
|---|---|---|
| Surface driver Assess/Submit/Rotate | Shipped | Reuse |
| `ResultReader` / turn-final read | `codexstore` shipped | Reuse (`readDeskTurnFinal`, mirror, synthesis) |
| `RecycleBridge` handoff | Shipped (`.flotilla/handoffs/`) | Reuse for rotate/recycle |
| `ComposerStateProbe` | **Not v1** (spinner fallback) | **Required** — coordinator send volume |
| Launch env | Secret-free | **`FLOTILLA_SELF` + `FLOTILLA_SECRETS`** |
| Outbound operator path | N/A (pull-participant) | **`flotilla notify`** per `docs/xo-doctrine.md` |
| Outbound fleet path | `flotilla send` (secret-free) | Same + high volume |
| `.codex/rules` | Execution no-self-merge backstop | **Coordinator rules** (reviewer merge allowed) |
| `harnessAllocationSurface` | N/A (execution only) | **Blocked** — forces `claude-code` today |
| `delegatenudge` | N/A | **Blocked** — `IsManagementHarness` is Claude-only |
| Doctrine in AGENTS.md | ~15 KiB constitutional set | Same + **xo-outbound** (~2 KiB); budget audit |
| Heartbeat skills in host dir | `visibility-synthesis` | Codex cwd = worktree — **skills path gap** (§6) |

---

## 2. Outbound paths

### 2.1 Operator — `flotilla notify` (secrets-bearing)

Per `docs/xo-doctrine.md`, the XO posts operator-facing replies to Discord via `flotilla notify`.
Unlike execution desks (`docs/inter-harness.md`, `pushsnippet.go`), **coordinators ARE provisioned
`$FLOTILLA_SECRETS`** — a provisioning contract, not a binary guarantee.

Coordinator launch recipe MUST export:

```text
FLOTILLA_SELF=<agent>
FLOTILLA_SECRETS=<path>   # from roster/deploy convention
```

and ensure `flotilla` is on PATH. One-line notify collapses to `flotilla notify "<reply>"`.

**Codex rules:** coordinator `.codex/rules` MUST NOT forbid `flotilla notify` (execution desks
are trained to never touch secrets; coordinators are the opposite).

### 2.2 Fleet — `flotilla send` (secret-free)

Unchanged from inter-harness doctrine. Coordinator dispatches desks via tmux injection; no
secrets required. High send volume is why §3 (ComposerStateProbe) is load-bearing.

### 2.3 Turn-final egress — mirror + notify

Two operator-visible egress paths coexist (`docs/xo-doctrine.md`):

1. **Discretionary:** XO calls `flotilla notify` on genuine operator messages.
2. **Mechanical:** `deskMirrorOnFinish` / XO Stop-hook mirror posts turn-final verbatim.

Both require substantive turn-final text from `codexstore.LatestResult` — already wired when
`surface: "codex"` resolves. The coordinator MUST follow the `executive-mini-brief` shape
(installed as identity-append) because the mirror posts mechanically.

---

## 3. Confirmed delivery — ComposerStateProbe

#259 deliberately omitted `ComposerStateProbe` on codex v1 (`confirm.go` Working-spinner
fallback). For execution desks, occasional spinner-only confirmation is tolerable; for a
coordinator injecting fleet traffic, **unconfirmed submits are unacceptable** (silent drops,
paste races — see `flotilla-confirmed-delivery` skill).

**v1 coordinator gate:** codex MUST implement `ComposerStateProbe` before any supervised trial.
Markers sourced post-operator-auth (same gate as working/idle/approval fixture revalidation in
`codex-surface-driver` post-auth follow-ups).

Until probe ships, codex coordinators are **design-only** — do not roster one on a live fleet.

---

## 4. Mini-brief turn-final flow

End-to-end path (no new primitives):

```
Codex turn completes → codexstore records agent_message in rollout JSONL
  → detector finish hook → readDeskTurnFinal (ResultReader)
    → delegatenudge.Check / idlehold.Check / stranded.Check (classifiers)
    → deskMirrorOnFinish (non-XO desks) OR XO mirror hook (coordinator)
    → Discord webhook post (verbatim)
```

**Codex-specific validation:** post-auth, capture a coordinator turn-final fixture and assert
`LatestResult` returns prose suitable for mirror + classifiers (tool-call noise absent from
stored agent_message).

**Management-harness fix (flotilla-dev):** `delegatenudge.Check(text, surface)` must treat
`codex` as a management harness so IC-ing detection fires; nudge copy must be harness-neutral
("coordinator seat" not "Claude seat").

---

## 5. Gate-verdict workflows

Coordinators own judgment: review diffs, run/trust CI, merge **others'** PRs, surface PRs upward.
Execution codex rules forbid `gh pr merge` entirely; **coordinator rules differ:**

| Action | Execution desk rule | Coordinator rule |
|---|---|---|
| `gh pr merge` on reviewed PR | forbidden | **allowed** (independent-review merge) |
| `git push` to default branch | forbidden | forbidden (same) |
| force-push | forbidden prefixes | forbidden prefixes (same residuals as #259) |
| `flotilla notify` | must not be provisioned | **required capability** |

Scaffold `flotilla-coordinator.rules` (distinct filename from `flotilla-desk.rules`) on
`workspace init` when `harnessAllocationSurface` resolves to `codex` **and**
`cfg.IsCoordinator(agent)`.

---

## 6. Doctrine delivery — AGENTS.md size limits

Codex loads merged AGENTS.md chain from cwd with default `project_doc_max_bytes` = **32 KiB**
([Codex AGENTS.md guide](https://developers.openai.com/codex/guides/agents-md)).

**Current budget (measured):**

| Component | Bytes (approx) |
|---|---|
| workspace init stub | ~120 |
| 5 identity-append members | ~14,700 |
| **Subtotal constitutional** | **~14,800** |
| Headroom before xo-outbound | ~17,200 |

**Coordinator addition:** new identity-append member `xo-outbound` — distilled from
`docs/xo-doctrine.md` (~2 KiB): notify-on-operator-message, no-notify-on-heartbeat, env vars,
2000-char Discord limit. Installed **only for coordinators** (`doctrine.Install` coordinator
filter — new helper or member `CoordinatorOnly` flag).

**Heartbeat-skill gap:** `visibility-synthesis` installs to **host workspace**
(`~/.flotilla/<agent>/skills/`), but codex cwd is the **git worktree**. Synthesis wakes inject
prompt text from the daemon (watch path), so the skill file is advisory — not load-bearing for
v1. If codex cannot invoke skills by path, duplicate a one-line pointer in AGENTS.md:
`On synthesis wakes, follow the visibility-synthesis discipline in your host workspace skills/.`

**Overflow policy:** if a deployment's stub + operator prose exceeds 32 KiB, document
`--config project_doc_max_bytes=<n>` in the coordinator launch recipe (operator-tuned; not
flotilla default).

---

## 7. Detector-driven loop compatibility

The change-detector loop is surface-agnostic (`internal/watch/detector.go`) — it calls
`surface.Get(agentSurface(cfg, name))` per desk. For a codex coordinator:

| Detector feature | Codex readiness |
|---|---|
| Material wake on Working→Idle | Needs post-auth Assess fixtures |
| `readDeskTurnFinal` classifiers | Ready once ResultReader works |
| Delegation nudge (#232) | Blocked on `IsManagementHarness` |
| Idle-hold / stranded breaks | Ready (surface-agnostic) |
| Synthesis `SynthRead` | Ready (ResultReader) |
| Post-handle XO rotate | `codex` `/clear` shipped |
| Rate-limit streak clear | Generic |

**Coordinator rotate:** long-lived XO under change-detector gets rotated after each settled
handling — codex `/clear` is the reset. **Recycle** for compaction uses portable markdown
handoff (`.flotilla/handoffs/recycle-*.md`) — shipped in #259; coordinator trial should
exercise one recycle before declaring ready.

---

## 8. `harnessAllocationSurface` — coordinator on codex

Today (`workspace.go:232`):

```go
if cfg.IsCoordinator(agent) {
    return "claude-code"
}
```

**Target (flotilla-dev implements):** respect explicit roster surface for coordinators:

```go
if cfg.IsCoordinator(agent) {
    if rosterSurface == "codex" { return "codex" }
    return "claude-code"  // default management harness unchanged
}
```

Roster pattern for trial:

```json
{ "name": "alpha-xo", "surface": "codex", "role": "xo" }
```

`workspace init` then scaffolds AGENTS.md + coordinator codex rules + secrets-bearing launch.

---

## 9. Supervised trial — eventual gate

Design toward **one low-stakes project XO** (not meta-XO / CoS):

**Preconditions**

- Operator codex auth complete; ComposerStateProbe live-validated
- Host binary ≥ merge commit; `flotilla-watch` restarted after first codex roster entry
- `flotilla doctrine install --refresh alpha-xo` with coordinator members
- Rollback path documented: `flotilla switch alpha-xo --to claude-code` (harness-subscription-switching)

**Trial script (48h operator-present)**

1. Relay: operator message → XO pane → `flotilla notify` reply visible in Discord
2. Dispatch: `flotilla send` to grok desk → material wake → XO handles in fresh context
3. Gate: XO reviews a trivial PR (merge via `gh pr merge` — coordinator rules allow)
4. Detector: trigger idle-hold break with synthetic holding turn-final (staging)
5. Rotate: settled handling triggers `/clear`; ack file touched
6. Rollback drill: switch to Claude without fleet stall

**Success:** all six without silent-drop submits or doctrine budget overflow. **Failure:** revert
roster surface to `claude-code`; postmortem in private channel.

---

## 10. Implementation phases

| Phase | Owner | Deliverable |
|---|---|---|
| **0** | codex-harness-dev | This design PR (COS gate) |
| **1** | flotilla-dev | `harnessAllocationSurface` + `delegatenudge` parity; seat-swap runbook |
| **2** | codex-harness-dev | Post-auth fixtures + `ComposerStateProbe` |
| **3** | codex-harness-dev | Coordinator launch env, `flotilla-coordinator.rules`, `xo-outbound` member |
| **4** | joint | Supervised trial on one project XO |

Phases 1 and 2 may parallelize after phase 0 merges; phase 4 is blocked on 1–3.

---

## 11. Residual risks (honest)

- **ComposerStateProbe** may require composer chrome not yet characterized — trial blocked until
  post-auth capture.
- **prefix_rule** argv-prefix residuals from #259 apply to coordinator rules too — not a security
  boundary.
- **AGENTS.md 32 KiB** is tight for operator-customized stubs; monitor on trial.
- **Delegation nudge patterns** were tuned on Claude turn-finals; codex prose may differ —
  re-tune patterns if false-positive/negative rate is wrong on trial.