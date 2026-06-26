# Proposal — unify-xo-reply-leg: the flotilla-native watcher is the return leg for ALL XOs (#177)

## Why

#175 (#178, merged + live) shipped a flotilla-native reply-watcher for FEDERATED c2-channel XOs, but
EXCLUDED the primary XO because it already had a return leg via a host-local `Stop`-hook
(`~/.claude/hooks/flotilla-xo-discord-mirror.sh`). That left TWO divergent mechanisms for one job — the
fragile per-pane host-local hook (claude-only, 4 historical bugs from transcript-archaeology) and the
generalizable watcher.

VERIFIED de-risking finding (design §2): the Stop-hook is **replies-to-operator-only** (its header:
mirror "WHEN the turn was triggered by a genuine operator message (NOT a heartbeat/…)"), the SAME
semantics as the watcher (`j.Kind=="relay"`). So unifying loses NO operator visibility — and the
watcher's relay-gate is strictly more robust (it knows the message at arm time; no transcript walk, no
Stop-vs-flush race, no host script).

## What changes

1. Remove the primary-XO exclusion in `isHotlineToChannelXO` (the watcher arms for the primary XO too).
2. Include the primary XO in `logReplyLegCoverage` (boot coverage covers all XOs).
3. Retire the host-local Stop-hook (HOST step, coordinated cutover — §4 of design) to avoid the primary
   XO double-posting (hook + watcher).

## Impact

- **Code:** `cmd/flotilla/reply.go` (drop the `j.Agent == cfg.XOAgent` exclusion + the coverage skip);
  `cmd/flotilla/reply_wiring_test.go` (primary-XO now arms).
- **Spec:** MODIFY the #175 `watch` requirement (drop the federated-only scoping + the "primary XO's
  reply path unchanged" clause). **DEPENDENCY:** this MODIFY needs `desk-reply-routing` (the #175
  change) ARCHIVED first (it is merged+deployed but not yet archived, so the requirement is not yet in
  the main spec). Sequence: archive desk-reply-routing → land this spec delta.
- **Host (coordinated cutover):** disable the Stop hook in `~/.claude/settings.json`, deploy the new
  binary + restart `flotilla-watch` IN ONE WINDOW (never restart-new-binary while the hook is live →
  double-post). Reversible (re-enable hook + revert binary). Operator/XO-timed.

## Not in
- Deleting the host-local hook file (leave it, disabled, as a rollback path).
- #176 (visibility-mirror gap) — separate.
