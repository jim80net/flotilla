# Dispatch delivery observability (CNS Stratum A)

Tracks durable send lifecycle for inter-agent `flotilla send` and dropped-dispatch
resume (#472 / #475 / #614 / #616).

## Artifacts (roster-adjacent)

| File | Role |
|------|------|
| `flotilla-<sender>-outbox.json` | Pending sends not yet pane-confirmed (#475) |
| `flotilla-<recipient>-inbound.json` | Confirmed pane deliveries awaiting durable ack (#472) |
| `flotilla-dispatch-consumed.json` | Durable consumed registry — nonce (+ payload hash) (#614) |
| `flotilla-chapter-hold` | Optional marker: hold non-urgent reinjects during chapter (#616) |

## Dispositions

- **queued** — in sender outbox; recipient busy / not yet confirmed
- **delivered** — inbound ledger pending durable ack
- **consumed** — settled (durable ack, legacy turn-final ack, MERGED suppress,
  coordinator-recipient send-time settle, or manual)
- **undelivered** — age bound exceeded (outbox or unacked inbound)

## Coordinator recipients (#707)

Coordinator seats keep **no inbound pending row** — their finish is deliberately
not ack-gated, so tracking would grow finish evaluation unbounded (#472). A
confirmed send to a coordinator instead settles **straight into the consumed
registry** with reason `coordinator-recipient`. That reason asserts confirmed
delivery only, not that the work was addressed. `dispatch-status` therefore
resolves the nonce (never `unknown` after a confirmed delivery), and a
coordinator running the footer's `dispatch-ack` converges on the already-durable
path instead of erroring `not pending`.

Two guards keep this settlement from leaking onto other seats' dispatches:

- **Own-footer attribution.** Only the message's own trailing #472 footer nonce
  settles. A coordinator-directed report that merely *quotes* another
  dispatch's nonce in prose settles nothing (nonces are reused across hops for
  outbox dedup, so a quoted nonce is usually another seat's live dispatch).
- **Hop-scoped matching.** A `coordinator-recipient` entry settles only its own
  recipient's hop. The same dispatch text forwarded verbatim to a desk keeps
  that desk's reinject / escalation / undelivered supervision alive, the desk's
  own `dispatch-ack` still records its real settlement (which then takes
  lookup preference over the hop entry), and the row-scrub sweeps ignore hop
  entries for other seats.

## Desk-visible queued ack

When a send lands in the busy outbox, stdout includes a machine-readable line:

```text
QUEUED id=<id> sender=<s> recipient=<r> status=busy_outbox
```

(`status=already_queued` on dedup.)

## CLI

```bash
flotilla dispatch-status [--roster <path>] <nonce>
flotilla dispatch-ack [--roster <path>] <nonce>
```

`dispatch-status` resolves disposition across consumed → inbound → outbox.
After handling a dispatch, its recipient runs `dispatch-ack`; the command writes
the consumed registry first and then clears the inbound row, so a crash between
those steps is healed by the watch sweep. `$FLOTILLA_SELF` identifies the recipient,
and one seat cannot acknowledge another seat's pending nonce.

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

Durable ack of a nonce suppresses reinjection so resume storms cannot re-task.
Turn-final nonce/snippet matching remains a backward-compatible reconciliation path.

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
