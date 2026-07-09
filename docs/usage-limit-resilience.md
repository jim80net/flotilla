# Usage-limit resilience — per-seat downgrade policy (#466 / #510)

When a desk **or coordinator** hits provider usage or rate limits, the fleet should
**downgrade** to an operator-ratified fallback tier instead of stalling until limits
reset. The policy lives in the **host-local launch recipe** (not the committable
roster): each roster agent's `primary` + ordered `fallbacks[]` harness slots declare
the downgrade chain.

See `flotilla-launch.example.json` for a committed shape. Copy and adapt it to
`<roster-dir>/flotilla-launch.json` or `~/.flotilla/<agent>/launch.json`.

## Slot fields (per harness)

| Field | Purpose |
|---|---|
| `surface` | Driver name (`claude-code`, `grok`, `codex`, …) |
| `provider` | Logical provider (`anthropic`, `xai`, `openai`, …) — load-bearing for server-side failover |
| `launch` | Shell command including the **model pin** for this tier |
| `model` | Operator-facing metadata (Sonnet, GPT 5.5, latest Grok, …) |
| `subscription_id` | Optional billing bucket within a provider (account-side throttles poison one bucket) |

Recipe-level `cwd`, `tmux`, and `state` are shared across slots — only the foreground
harness process changes on switch.

## Operator-ratified downgrade tiers (directive 2026-07-06)

| Seat class | Preferred tier | Typical degraded tier |
|---|---|---|
| Coordinator (XO / CoS) | Claude Opus (judgment) | Claude Sonnet 5, then latest Grok |
| Execution desk | Latest Grok (workhorse) | GPT 5.5 (Codex), then Sonnet |

Exact model strings belong in the host-local `launch` command — flotilla does not
hard-code vendor model IDs.

## How failover selection uses the chain

The existing `flotilla switch` + auto-switch machinery (`selectFailoverTarget`) reads
the chain in order:

1. **Account-side throttle** — prefer the first healthy slot with the **same provider**
   (in-provider model downgrade, e.g. Opus → Sonnet on `anthropic`).
2. **Server-side throttle** — skip the poisoned provider entirely; pick the first healthy
   slot on a **different** provider (cross-harness, e.g. Claude → Grok).

Manual: `flotilla switch <agent> --to fallback-0` (or `--to grok` / slot name).
Restore preferred tier: `flotilla switch <agent> --to primary` when limits clear
(or let auto-revert do it — see below).

## Auto-switch eligibility (#510)

Watch auto-switch is **ON by default** (`surface.AutoSwitchEnabled` — disable explicitly
with `FLOTILLA_AUTOSWITCH=0`, `false`, `no`, or `off`). When enabled, it applies to:

- **Execution desks** and **coordinators** (primary XO, channel XOs, CoS, other
  `IsCoordinator` seats) that are not `approval_sensitive`
- Seats currently on the primary surface (`claude-code`) — Claude-storm relocation;
  already-relocated seats stay sticky until restore

`approval_sensitive` desks still require explicit `flotilla switch … --confirm` (GATE-4).

On a coordinator exhaustion edge the watch daemon also:

1. Raises a **loud operator alert** (never silent)
2. Injects an **urgent adjutant note** when an adjutant is bound (prompt-contract:
   recognize leader exhaustion and escalate)
3. After a successful coordinator auto-switch, **re-notifies** `AgentsBelow` (and the
   adjutant) that the leader is back on the new harness

Detector-enqueued `switch --auto` uses a **kill+relaunch fallback** when Claude
graceful-close hangs (#437) — the handoff is durable before that step. Manual
(non-auto) switches still abort on unconfirmed close.

## Auto-restore preferred tier (#510 / #466 phase 2)

Auto-revert is **ON by default** (`surface.AutoRevertEnabled` — disable with
`FLOTILLA_AUTOREVERT=0`). When a seat is on a non-primary `active-harness.json` slot
and rate-limit probes report clear for two consecutive folds **and** the primary
provider is not under active poison cooldown, watch dispatches
`flotilla switch <agent> --to primary`.

## Ledger / turn-final provenance

After a switch, `~/.flotilla/<agent>/active-harness.json` names the live slot and
`last-switch.json` records the transition. Turn-finals authored during a downgrade
window should note the active tier so reviewers know which model produced the work.

## Related

- Harness switching design: `docs/harness-subscription-switching.md`
- `flotilla switch` command: `cmd/flotilla/switch.go`
- Launch schema: `internal/launch/launch.go`
- Issue: coordinator/adjutant resuscitation (#510); desk shape (#466)
