## Why

Once federation lands per-XO channels (`federation-channels` / #105), the
operator's side-conversations **fragment across N channels** — operator↔alpha-XO
in `#fleet-alpha`, operator↔beta-XO in `#fleet-beta`, operator↔meta-XO in
`#fleet-command`. No single agent sees them all, so **who-knows-what context is
siloed**: a desk acts without the cross-fleet picture; the chief of staff can't
integrate decisions made in a channel it wasn't in.

The live Spark fleet already compensates for this **by hand**, in an operational
`state/context-ledger.md` (a who-knows-what ledger the chief of staff maintains).
Operator directive 2026-06-18 (#108): **productize it** — every operator↔XO
exchange should be mirrored to the chief of staff, and the who-knows-what ledger
should be automated rather than hand-kept.

This change is **design-first** and is the **companion to #105**: #105 lays the
seams (the routed `Job` carries its `OriginChannel`; a `cos_agent` config field is
reserved + validated), and this change builds the behavior on top. It does not
block #105.

## What Changes

- **Add the `cos` capability — a chief-of-staff context-integration layer.** A
  configured `cos_agent` (generalizable — **a role, not any deployment's desk
  name**; Spark's is `hydra-ops`, but the product ships a `cos_agent` knob)
  receives a mirror of operator↔XO traffic across **all** channels and is the home
  of the who-knows-what ledger.
- **Mirror both directions of operator↔XO traffic to the CoS:**
  - **Inbound (operator→XO):** the relay already routes by channel (#105) and the
    `Job` carries `OriginChannel` (#105 seam). The existing post-confirmed-delivery
    mirror hook (`Injector.SetMirror`) records each routed operator message to the
    CoS context substrate, tagged with the origin channel + target XO.
  - **Outbound (XO→operator):** `flotilla notify` (the XO's reply path) records the
    reply to the same substrate, tagged with the XO + its channel.
- **Automate the who-knows-what ledger.** flotilla maintains a durable,
  append-structured **context ledger** (the productized `state/context-ledger.md`):
  a deterministic, no-LLM record of who-told-whom-what-where-when. The CoS agent
  reads + integrates it on its heartbeat cadence (its curation is doctrine, layered
  on the deterministic substrate).
- **A genuine design fork for the operator** (design.md): how the ledger is
  maintained (mechanical append vs CoS-curated) and how the mirror reaches the CoS
  (a ledger file the CoS reads vs a CoS channel post vs pane injection).

## Capabilities

### Added Capabilities
- `cos`: the chief-of-staff context-integration layer — per-channel mirror of
  operator↔XO traffic to a configured `cos_agent`, the automated who-knows-what
  context ledger, and the CoS's integration cadence.

## Impact

- **Design-first; depends on #105's seams.** Buildable only after #105 lands
  `OriginChannel` on `watch.Job` and the `cos_agent` config field. Enumerated,
  unchecked build tasks.
- **Deterministic substrate, no new authority.** The mirror is observe-only — it
  records traffic the relay/notify already handle; it grants the CoS no delivery
  authority and changes no relay security rule (operator-only + webhook-drop are
  untouched). The CoS reads context; it does not gain a back-channel to command
  desks.
- **Generalizable, not Spark-specific.** `cos_agent` + a configurable ledger path;
  no deployment desk names baked in. `state/context-ledger.md` is the operational
  precedent, not the product surface.
- **Affected surfaces (when built):** the `Injector.SetMirror` hook
  (`internal/watch/inject.go` / `cmd/flotilla/watch.go`), `flotilla notify`
  (`cmd/flotilla/main.go`), a new ledger writer, `internal/roster` (consume the
  reserved `cos_agent`), and docs.
