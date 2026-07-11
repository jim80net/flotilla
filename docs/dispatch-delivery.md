# Dispatch delivery observability (CNS Stratum A)

Tracks durable send lifecycle for inter-agent `flotilla send` and dropped-dispatch
resume (#472 / #475 / #614 / #616).

## Artifacts (roster-adjacent)

| File | Role |
|------|------|
| `flotilla-<sender>-outbox.json` | Pending sends not yet pane-confirmed (#475) |
| `flotilla-<recipient>-inbound.json` | Confirmed pane deliveries awaiting turn-final ack (#472) |
| `flotilla-dispatch-consumed.json` | Durable consumed registry — nonce (+ payload hash) (#614) |
| `flotilla-chapter-hold` | Optional marker: hold non-urgent reinjects during chapter (#616) |

## Dispositions

- **queued** — in sender outbox; recipient busy / not yet confirmed
- **delivered** — inbound ledger pending turn-final ack
- **consumed** — settled (turn-final ack, MERGED suppress, or manual)
- **undelivered** — age bound exceeded (outbox or unacked inbound)

## Desk-visible queued ack

When a send lands in the busy outbox, stdout includes a machine-readable line:

```text
QUEUED id=<id> sender=<s> recipient=<r> status=busy_outbox
```

(`status=already_queued` on dedup.)

## CLI

```bash
flotilla dispatch-status [--roster <path>] <nonce>
```

Resolves disposition across consumed → inbound → outbox.

## Roster discovery (#615)

`flotilla send` (and `dispatch-status`) resolve the roster when `--roster` /
`$FLOTILLA_ROSTER` is unset or the default path is missing:

1. `$FLOTILLA_ROSTER` (fail-closed if set but missing)
2. `./flotilla.json` in cwd
3. Walk toward root: `<dir>/flotilla.json`, then `<dir>/state/flotilla.json`
4. `~/.flotilla/$FLOTILLA_SELF/launch.json` → `"roster"` hint

## Dropped-dispatch suppress

On Working→Idle finish, reinject is **suppressed** when:

1. Nonce is already in the consumed registry
2. All cited `PR #N` are MERGED (checker; production may wire `gh` later)
3. `flotilla-chapter-hold` is active (hold — does not consume)

Turn-final ack of a nonce durable-consumes it so resume storms cannot re-task.

## Undelivered routing — adjutant first (#628)

Age-crossed undelivered (outbox or inbound-ack) always journals. Operator Discord
is **not** the first surface when a layer adjutant exists:

| Layer | When | Where |
|-------|------|--------|
| **Journal** | Every first L1 fire | watch log (`dispatch undelivered…`) |
| **L1** | Age ≥ inbound 15m / outbox `StaleMaxAge` | Detector wake → `AdjutantFor(OwningXO(recipient))`, else primary `AdjutantFor(xo)` |
| **L2** | After L1 watched ≥ inbound L1 age, wall age still ≥ 3× L1, not grandfathered | Operator webhook, **max 2/tick** + summary |

**Deploy storm guards (post-#630 L2 mass alert):**

- **Grandfather:** first observation already past L2 wall age → L1 adjutant/journal only; mark L2 without operator Discord.
- **Watched window:** L2 requires min time since process-local L1 (not pure `DeliveredAt` alone).
- **Rate limit:** at most 2 operator L2 alerts per tick; remainder deferred with one summary line.

No dual-fire of operator + adjutant on the first crossing. No adjutant → operator
remains the only Discord path (legacy), with the same grandfather for past-L2 cold start.

### False-positive suppress (ack already present)

Before the undelivered scan, the sweep **reconciles** inbound ledgers:

1. Drop entries whose nonce is already in `flotilla-dispatch-consumed.json`
2. If the recipient's latest turn-final acknowledges the nonce (#472 matcher),
   remove the inbound entry and durable-consume

So a desk that already turn-final-acked never produces `undelivered-ack` spam
when the Working→Idle finish edge was missed.
