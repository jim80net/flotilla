# Pull-by-push message buffer

Status: implemented as the default inter-agent and operator-relay delivery path.

## Decision

The durable per-recipient buffer is the source of truth. `flotilla send` commits
the full body to `flotilla-<recipient>-buffer.json`; the pane receives only a
best-effort instruction to run `flotilla pull`. Operator relay bodies take the
same path. Pane submission is a nudge, never the message transaction.

This is intentionally “pull by push”: push retains its useful wake-up property,
but it carries no unique data and its failure is logged as a non-event. A seat
that is busy, model-limited, crashed, or input-blocked can accumulate a visible
backlog without losing messages.

## Measured reasons for the change

The production observations that authorized this design are part of its
acceptance evidence, not anecdotal background:

- Four seats remained unable to execute for hours on a model limit while the
  detector showed `idle · available`. Panes were alive and messages arrived and
  registered normally. Push could not distinguish “wedged” from “busy”; the
  recipient buffer makes the unpulled backlog directly countable.
- The consumed registry proved to be a floor on arrivals, not a census. Four
  full bodies reached a recipient and none registered; the entire gap matched a
  sender INPUT-BLOCKED window. Pull now stamps `pulled_at` in the authoritative
  record, so arrival and its record are one operation.
- A busy coordinator starved its own inbox because body delivery waited for an
  idle composer. Pull inverts that relationship: hard work can delay the nudge,
  but it cannot delay or reject the durable arrival.
- A stop-work order could not overtake the work it stopped. It rode the same
  push queue and was measured as much as fifteen minutes late; two merges landed
  under withdrawn authority. A seat that pulls before each action sees a newly
  buffered stop before the next step. Operator interrupts now carry only a pull
  nudge; the stop body is already durable when the interrupt is attempted.

## Durable model and commands

Each recipient file has a version, per-sender next-sequence map, and retained
entries. An entry contains immutable ID, sender, recipient, full body, dispatch
nonce, sender sequence, enqueue time, and optional supersession/migration data.
`pulled_at` and `acknowledged_at` are the only lifecycle mutations.

- `flotilla pull` is identity-scoped by `FLOTILLA_SELF`. Under the recipient
  file lock it stamps every unread entry once and returns every unacknowledged
  entry. A second pull performs no write and returns the same backlog; a new
  urgent message is visible immediately instead of waiting behind a batch lease.
  This intentionally has no batch cap or lease expiry: until acknowledged, even
  a five-figure backlog is replayed in full on every pull. That is the lossless
  first-release contract; recipients must acknowledge handled rows promptly.
- `flotilla dispatch-ack <nonce>` records the existing consumed-registry fact
  and marks the matching buffer entry acknowledged. Non-dispatch/operator rows
  use `flotilla buffer ack <id>`. History is retained; ack only removes the row
  from future pulls.
- `flotilla buffer inspect <seat>` and `--all` are third-party read surfaces for
  pending, unread, already-pulled, superseded, and oldest-age counts.
- `flotilla send --supersedes <id>` atomically links an old instruction to its
  replacement. Pull output prints both `superseded-by` and `supersedes`, so FIFO
  order never masquerades as current authority.
- `flotilla cancel <id>` does not erase history. It links the target to a new,
  pull-visible cancellation dispatch and nudges the recipient best-effort. A
  buffer-ID miss fails closed. Generation-wide legacy outbox cancellation is
  reachable only through the explicit `--legacy-outbox` compatibility flag.

Per-sender ordering is the monotonic `sender_sequence`, allocated while holding
the recipient file lock. Cross-sender order is observed enqueue order and is not
promoted to an authority rule; explicit supersession is the currency rule.

## Nudge failures

The required transaction ends when the buffer rename succeeds. Pane resolution,
idle gating, submit, and confirm all happen after that boundary. Any failure is
logged as `nudge missed ... non-fatal`; the command succeeds and no “message not
delivered” alert is emitted. Retrying a nudge cannot duplicate a body because it
contains no body.

## Migration of legacy deferrals

Watch startup and ticks idempotently migrate every legacy per-sender outbox row:

1. Insert it into the recipient buffer with the same ID, body, sender, recipient,
   original enqueue time, and recorded deferral count.
2. Only after the recipient-buffer insert succeeds, remove the old outbox row.
3. Nudge each affected recipient once; a missed nudge changes no disposition.

The original ID is the idempotency key across a crash between steps 1 and 2.
Rows with 5,000 deferrals therefore become ordinary unpulled buffer entries with
`migrated_from=sender-outbox` and `legacy_deferrals=5000`; their retry loops and
stale “delivery failure” escalations stop. Rolling migration also catches rows
written by an older sender binary without ever resuming full-body push.

## Safety invariants

1. No pane operation occurs before durable body commit.
2. Pull and enqueue serialize on one recipient-file lock; first-read recording
   cannot race away an arrival.
3. Pull is non-destructive. Only an explicit durable ack hides a row.
4. Supersession is stored and displayed, never inferred from FIFO.
5. A nudge carries no unique data and cannot report body-delivery failure.
6. Migration is insert-before-remove and ID-idempotent.
7. Every seat pulls before acting, including before continuing remembered work.
   A cancellation is another buffered message, so this pull-before-act rule is
   what guarantees a durable stop is observed before the next authorized step.
