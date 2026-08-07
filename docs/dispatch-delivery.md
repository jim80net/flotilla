# Dispatch delivery observability (CNS Stratum A)

Tracks durable send lifecycle for inter-agent `flotilla send` and dropped-dispatch
resume (#472 / #475 / #614 / #616).

## Artifacts (roster-adjacent)

| File | Role |
|------|------|
| `flotilla-<recipient>-buffer.json` | Authoritative message bodies: buffered, pulled, and retained ack history |
| `flotilla-<sender>-outbox.json` | Legacy push rows only; migrated insert-before-remove into recipient buffers |
| `flotilla-<recipient>-inbound.json` | Legacy confirmed pane deliveries awaiting durable ack (#472) |
| `flotilla-dispatch-consumed.json` | Durable consumed registry — nonce (+ payload hash) (#614) |
| `flotilla-chapter-hold` | Optional marker: hold non-urgent reinjects during chapter (#616) |

## Dispositions

- **buffered** — durable in the recipient buffer; not yet pulled
- **pulled** — recipient pull recorded arrival; pending durable ack
- **queued / delivered** — legacy outbox / inbound states during migration
- **consumed** — settled (durable ack, legacy turn-final ack, MERGED suppress, or manual)
- **undelivered** — age bound exceeded (outbox or unacked inbound)

## Coordinator recipients

Coordinator seats use the same recipient buffer as every execution seat. This
removes the former send-time `coordinator-recipient` settlement shortcut: a body
is now an arrival only when the coordinator pulls it, and handling is recorded
when the coordinator runs the footer's `dispatch-ack`.

## Desk-visible buffered ack

Every send emits a machine-readable line after the recipient-buffer rename:

```text
BUFFERED id=<id> sender=<s> recipient=<r> status=buffered sequence=<n>
```

The full body is durable at that point. Pane nudge success is not a disposition.

## CLI

```bash
flotilla dispatch-status [--roster <path>] <nonce>
flotilla dispatch-ack [--roster <path>] <nonce>
flotilla pull [--roster <path>] [--json]
flotilla buffer inspect [<seat>] [--all] [--json]
```

`dispatch-status` resolves disposition across consumed → recipient buffer →
legacy inbound → legacy outbox.
After handling a dispatch, its recipient runs `dispatch-ack`; the command writes
the consumed registry first and then stamps the buffer entry acknowledged. A
crash between those steps converges on retry. `$FLOTILLA_SELF` identifies the
recipient, and one seat cannot acknowledge another seat's pending nonce.

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
