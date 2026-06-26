# Design — Unify the primary-XO Stop-hook into the flotilla-native reply-watcher (#177)

**Status:** DRAFT for the systems-review gate + hydra-ops review.
**Issue:** #177 (filed from #175). Follow-up to the c2-hotline reply-watcher (#178, merged + live).
**Depends on:** #175 (the reply-watcher) — MERGED + deployed.

## 1. The dual-mechanism state (what #175 left)

#175 shipped a flotilla-native reply-watcher that routes a desk-channel XO's reply back to the operator
(content-correlated from the session store, never-silent). It is **gated to FEDERATED c2-channel XOs**
— `isHotlineToChannelXO` excludes `cfg.XOAgent` (`reply.go:200`) — because the PRIMARY XO already has a
return leg via a **host-local `Stop`-hook** (`~/.claude/hooks/flotilla-xo-discord-mirror.sh`, wired in
`~/.claude/settings.json`). So flotilla now has TWO return-leg mechanisms for the same job:
- per-pane host-local Stop-hook (primary XO) — fragile (host-local, claude-only, 4 historical bugs from
  transcript-archaeology), not in the repo.
- flotilla-native reply-watcher (federated XOs) — the generalizable mechanism.

## 2. De-risking finding (VERIFIED — resolves the core design question)

The worry: does the Stop-hook mirror ALL the primary XO's turns (so the replies-only watcher would lose
operator visibility into the XO's self-initiated work)? **NO.** The Stop-hook header states it mirrors a
turn-final *"WHEN the turn was triggered by a genuine operator message (NOT a heartbeat / task-
notification / pure local-command turn)"*, self-gates to the XO pane (`marker == xo_agent`), and
classifies the trigger (strips command blocks, walks back to the text-bearing user message). So it is
**replies-to-operator-only** — the SAME semantics as the watcher (which arms only on `j.Kind=="relay"`,
a genuine operator message the relay already identified). **Unifying loses NO operator visibility.**

And the watcher's relay-gate is STRICTLY more robust: the relay KNOWS the turn is operator-triggered at
arm time + carries the message for content-correlation — no transcript-archaeology (the source of the
hook's 4 bugs), no Stop-vs-flush race (the watcher polls the store to quiescence), no per-pane host
script.

## 3. The change (small)

1. **Remove the primary-XO exclusion.** `isHotlineToChannelXO` drops `j.Agent == cfg.XOAgent`, so the
   watcher arms for the PRIMARY XO too when the operator messages the primary channel
   (`BindingForChannel(primaryChannel).XOAgent == cfg.XOAgent == j.Agent`). `replyDest` resolves the
   primary channel → `cfg.XOAgent` → its webhook (the operator/primary channel) — already works.
2. **Include the primary XO in `logReplyLegCoverage`** (it currently skips `cfg.XOAgent`) so its
   return-leg webhook coverage is visible at boot too.
3. **Retire the Stop-hook (HOST step, coordinated).** Disable the `Stop` hook in `~/.claude/settings.json`
   on the host — otherwise the primary XO **double-posts** (hook + watcher). This is host-local config,
   operator/XO-owned.

## 4. The coordination hazard + cutover (the one thing to get right)

**Double-post:** the moment the new binary runs (a watch restart) WITH the Stop-hook still active, the
primary XO's reply posts TWICE. So the code-deploy and the hook-retirement MUST be coordinated:

- Merging the code to main is SAFE (inert until the next watch restart — merged≠running).
- The CUTOVER (the deploy) must, in one window: (a) retire the Stop-hook in `~/.claude/settings.json`,
  (b) deploy the new binary + restart `flotilla-watch`. Order: retire the hook first (or together), then
  restart — never restart-new-binary while the hook is live.
- **Rollback:** re-enable the Stop-hook + revert the binary; both are independent and reversible.
- **Verify after cutover:** an operator → primary-channel message gets exactly ONE reply (the watcher's,
  attributed), the Stop-hook log shows no new posts, no double-post.

## 5. Scope / not in

- **In:** the exclusion removal + coverage + tests + spec delta; the cutover runbook (§4).
- **NOT in:** deleting the host-local `flotilla-xo-discord-mirror.sh` file (leave it on the host,
  disabled, as a rollback path); changing the federated-XO behavior (unchanged); the visibility-mirror
  gap (#176, separate).

## 6. Open items for hydra-ops

1. Confirm the unification (retire the Stop-hook, watcher covers all XOs) — recommended; the finding
   shows no visibility loss.
2. The cutover is a coordinated host+binary deploy (§4) — XO/operator-timed (it pauses the heartbeat
   clock on the restart, and touches `~/.claude/settings.json`).
