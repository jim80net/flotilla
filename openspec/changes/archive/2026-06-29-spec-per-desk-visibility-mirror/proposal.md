# Proposal — spec the per-desk visibility mirror + its known tick-lossiness (#176, spec-only)

## Why

The watch daemon has a **per-desk visibility mirror** (`deskMirrorOnFinish` → `deskMirror.run`,
`cmd/flotilla/{watch.go,mirror.go}`): when a NON-XO desk finishes a turn, it posts that desk's
turn-final to the desk's OWN channel webhook (under the desk's identity, chunked) so the operator/XO can
see what a desk has been doing in its own channel. It is a SHIPPED, load-bearing-for-visibility
mechanism — but it has **no spec requirement at all** (an institutional-knowledge gap, flagged during
#175). And it has a KNOWN, easy-to-mistake LOSSINESS: it is triggered by the change-detector's
`Working→Idle` sampling at the heartbeat-interval cadence (20m on the live fleet), so a turn that starts
AND finishes entirely within one tick window is never observed and never mirrored. So a desk's channel
is a **best-effort** view, NOT a reliable/complete record — but nothing says so, so it is easy to read
the channel as complete.

This change is **SPEC-ONLY** (operator directive #176): capture the intended behavior + the lossiness as
its own `watch` requirement, so the gap is documented and the property is explicit. A scoped FIX (making
per-desk mirroring reliable via per-turn store-completion detection) is a SEPARATE follow-on, NOT bundled
here.

## What changes

ADD a `watch` requirement: "A per-desk visibility mirror posts each non-XO desk's turn-final to its own
channel (best-effort)." It SHALL state:
- The destination is the DESK's OWN channel webhook (`secrets.Webhook(agent)`), under the desk's
  identity, chunked — distinct from the operator-hotline RETURN leg (which routes to the operator's
  channel; #175/#177).
- It is OBSERVE-ONLY + BEST-EFFORT: it never affects the desk; every outcome (a clean SKIP for no
  webhook / no session-store reader / no substantive turn, a POST, or a MIRROR-FAIL) emits exactly one
  decision log line; a failure NEVER propagates.
- The turn-final is read from the harness session store via the surface `ResultReader` — the SAME path
  `flotilla result` uses (CLI and auto-mirror never diverge).
- **KNOWN LOSSINESS (documented, not a defect to hide):** it is triggered by the change-detector's
  sampled `Working→Idle` edge at the heartbeat-interval cadence, so a turn entirely within one tick
  window is NOT mirrored. The desk channel is therefore a best-effort view, NOT a complete record.
- It posts via a webhook, so the relay's feedback-loop immunity (the `webhookID` drop) keeps it from
  re-entering the relay.
- A boot coverage line (`logMirrorCoverage`) names which desks will mirror (have a webhook) and which
  will not, so a mis-provisioned desk is visible at startup.

## Impact
- **Spec:** `watch` — ADD the per-desk-visibility-mirror requirement. NO code change.
- **NOT in:** the reliability FIX (#176's scoped follow-on — separate PR per the operator directive);
  any change to the operator-hotline return leg (#175/#177).
