## Why

`flotilla send` is the inter-agent (XO↔desk) coordination bus. Today every `send`
also mirrors the delivered message to the operator's Discord channel by default
(`cmd/flotilla/main.go:222-230`). That mirror was useful as a v0 audit trail, but
in steady fleet operation it **clutters the operator's Discord** with intra-flotilla
chatter the operator does not need — the coordination already lives in the tmux
panes. The operator is currently passing `--no-mirror` by hand on every send as a
stopgap.

Make inter-agent mirroring **default-off**: intra-flotilla `send` traffic stays in
the panes, and only the operator-facing `flotilla notify` posts to Discord. The
per-call `--no-mirror` already exists; this makes "off" the persistent default via a
roster setting, with a per-call `--mirror` override for the occasional case where an
audit copy IS wanted.

## What Changes

- Add a roster setting `mirror_inter_agent` (bool, **default false** = off). When
  false/absent, `flotilla send` does NOT mirror; when true, it mirrors (the old
  behavior).
- Add a per-call `--mirror` flag (force on) alongside the existing `--no-mirror`
  (force off); the two are mutually exclusive. Precedence: explicit flag → roster
  setting → default-off.
- `flotilla notify` is **UNAFFECTED** — it is the operator-facing path and always
  posts. The mirror's best-effort + never-leak-the-webhook properties are unchanged
  for the case where it does mirror.

## Capabilities

### Modified Capabilities
- `send`: the audit mirror is now default-off, gated by `mirror_inter_agent` (roster)
  with a per-call `--mirror`/`--no-mirror` override.

## Impact

- **Code:** `internal/roster` (the new field); `cmd/flotilla` send mirror gating +
  the `--mirror` flag + usage; docs.
- **Behavior change (not purely additive):** the default flips from mirror-on to
  mirror-off. To restore the old always-mirror behavior, set
  `mirror_inter_agent: true`. `notify` and the `--no-mirror` semantics are unchanged.
- **Future (noted, not now):** routing inter-agent vs operator traffic to distinct
  Discord channels (the XO flagged this as a later step).
