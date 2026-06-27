# Design — CoS context-integration layer (#108, companion to #105)

> **Design + v1 implementation; companion to `federation-channels` (#105, merged).**
> #105 laid the seams (`watch.Job.OriginChannel` + the validated `cos_agent`); this
> change builds the chief-of-staff mirror + the who-knows-what ledger on them, under
> the autonomous workflow (the systems-review + OCR + STORM trio is the bar).
> Generalizable: a `cos_agent` role, never a deployment's desk name.

## 1. The problem federation creates

Per-XO channels (#105) are the feature — but they fragment the operator's attention
across N channels. Each operator↔XO exchange happens in one channel; **no agent has
the union.** The chief of staff (the meta-XO, operationally) needs the union to
integrate "who-knows-what": which desk was told what, which decision was made in
which side-conversation, what context a newly-dispatched desk is missing. The-fleet does
this by hand in `state/context-ledger.md`; #108 automates it.

## 2. The model: a deterministic mirror feeding a curated ledger

```
  operator ──(per-channel)──► relay ──route by channel──► XO pane        (#105)
                                 │
                                 └─ Injector.SetMirror(Job{…,OriginChannel})  ← #105 seam
                                          │
   XO ──flotilla notify──► operator       │
        │                                 ▼
        └────────────────────────►  context ledger  (deterministic append:
                                     who · to-whom · channel · when · gist)
                                          │
                                          ▼
                                     CoS agent reads + integrates on its heartbeat
                                     (the who-knows-what curation = doctrine)
```

Two layers, deliberately separated:
- **Deterministic substrate (flotilla, no LLM):** every operator↔XO exchange, both
  directions, is appended to a durable **context ledger** with structured fields.
  This is reliable, auditable, and cheap — the productized `state/context-ledger.md`.
- **Curation (the CoS agent, LLM):** the `cos_agent` reads the ledger on its
  heartbeat and integrates the who-knows-what picture (summaries, "alpha doesn't
  know what beta decided", surfacing conflicts). This is doctrine layered on the
  substrate — the substrate stands alone even if no CoS is curating.

## 3. The two mirror directions (both reuse existing seams)

- **Inbound (operator→XO).** The relay (#105) routes by channel and enqueues
  `Job{Agent: targetXO, Message, Kind:"relay", OriginChannel: C}`. The existing
  post-confirmed-delivery hook `Injector.SetMirror(func(Job))`
  (`internal/watch/inject.go:86`, wired at `cmd/flotilla/watch.go:157`) appends a
  ledger entry: `{from: operator, to: targetXO, channel: C, ts, gist}`. No new relay
  path; the hook already fires after every confirmed relay delivery — #108 just adds
  the ledger write alongside today's audit-mirror post.
- **Outbound (XO→operator).** XO replies are `flotilla notify` posts
  (`cmd/flotilla/main.go cmdNotify`). #108 appends a ledger entry there too:
  `{from: <xo>, to: operator, channel: <xo's channel>, ts, gist}`. (Inter-agent
  `send` mirrors are already audited; whether desk↔desk traffic also feeds the
  ledger is a scope knob — default: operator↔XO only, the side-conversations the CoS
  most needs.)

## 4. Config surface (consumes #105's reservations)

```jsonc
{
  "cos_agent": "meta-xo",          // reserved + validated by #105; consumed here
  "cos_ledger": "context-ledger.md" // optional; default <roster-dir>/context-ledger.md
}
```

- `cos_agent` — the chief-of-staff agent. Generalizable; validated (∈ `agents[]`) by
  #105. When unset, #108 is inert (no mirror, no ledger) — fully backward compatible.
- `cos_ledger` — where the deterministic substrate is written. Host-local; like the
  other watch state files, it is the CoS's read source, NOT content-hashed as a wake
  signal it would self-trigger on.

## 5. Ledger format (the who-knows-what substrate)

Append-structured, human- and agent-readable (the productized `state/context-ledger.md`).
One entry per exchange, e.g.:

```
- 2026-06-18T14:03Z · #fleet-alpha · operator → alpha-xo · "ship the cache PR when green"
- 2026-06-18T14:05Z · #fleet-alpha · alpha-xo → operator · "merged; deploying"
```

Deterministic to write (no LLM). The CoS's *integrated* view (summaries, who-knows-
what matrix) is written SEPARATELY by the CoS agent, so flotilla's append never
collides with the CoS's curation (and the change-detector can hash the append region,
not the CoS region, without a self-wake loop).

## 6. DECISIONS for the operator

1. **Ledger maintenance.** (a) Mechanical append only — flotilla writes the
   substrate, the CoS curates in its own file (recommended: clean separation,
   deterministic substrate survives a quiet CoS). (b) CoS-curated in place — flotilla
   posts raw to the CoS, the CoS owns the whole ledger (simpler file, but the
   substrate is only as good as the CoS's uptime). *Recommend (a).*
2. **Mirror delivery to the CoS.** (a) Ledger file the CoS reads on its heartbeat
   (recommended — cheap, no per-message pane spam, integrates on cadence). (b) Post
   to a dedicated CoS channel (operator can read the integrated view too; more Discord
   volume). (c) Inject every exchange into the CoS pane (immediate but noisy +
   costly). *Recommend (a), optionally + (b) for operator visibility.*
3. **Scope of mirrored traffic.** operator↔XO only (default), or also XO↔desk and
   desk↔desk? Broader = more complete who-knows-what, but more volume. *Recommend
   operator↔XO + XO→operator for v1.*
4. **Privacy/retention.** The ledger is a durable record of all coordination — is
   rotation/retention needed, or is append-forever fine (it's host-local)?

## 7. Dependency + phasing

- **Hard dependency on #105 (satisfied):** consumes `watch.Job.OriginChannel` and the
  validated `cos_agent` field, both now merged to main.
- **Phase 1 (#108 v1 — built in this change):** the deterministic substrate — inbound +
  outbound mirror → ledger append, gated on `cos_agent` set; docs. No CoS-curation code
  (that's doctrine, not flotilla code).
- **Phase 2 (tracked in #115):** optional CoS-channel post (decision 6.2b); broader
  scope (6.3); retention (6.4); secret-redaction / per-channel opt-out; a
  machine-parseable (JSONL) form + a monotonic sequence number (closes the
  cross-appender wall-clock ordering gap); a non-local-filesystem guard for the ledger
  path; a CoS doctrine doc (how the `cos_agent` integrates the ledger on its heartbeat,
  like the XO doctrine). These are deferred-and-visible, not lost.

## 8. Non-goals

- **No new authority for the CoS.** The mirror is observe-only; it grants no
  delivery/command path to desks and changes no relay security rule (operator-only +
  webhook-drop untouched). The CoS reads context; it does not gain a back-channel.
- **No LLM in the substrate.** flotilla's ledger write is deterministic; the
  intelligence is the CoS agent's doctrine, layered on top.
- **No deployment desk names in the product.** `cos_agent` is a role knob;
  `alpha-xo` / `state/context-ledger.md` are a private deployment's operational instance, the
  precedent — not the product surface.
