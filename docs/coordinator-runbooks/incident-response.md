# Incident response

Match symptom to section; don't improvise destructive fixes.

## Red main

1. **Identify what's red.** `gh run list` — code vs environmental (billing, runner,
   calendar time-bomb test).
2. **Orphaned-test class.** Deleting code without deleting source-presence lock tests
   → red main on merge.
3. **Fix forward with a PR** — never force-push main.
4. **Why did the gate lie?** Often `go test | tail` without pipefail — find and close
   the hole.
5. **Verify with pipefail:**
   ```bash
   git fetch origin main && git reset --hard origin/main
   set -o pipefail; go test ./... 2>&1 | tail -20
   ```

## Leak response

The public/private partition is load-bearing
([`private-public-boundary.md`](../private-public-boundary.md)).

1. **Scan:** `scripts/check-private-boundary.sh` (and `--issues` for open GitHub
   artifacts). Exit 1 = fail-closed token found.
2. **Scrub** offending public content; re-scan until clean.
3. **Adjudicate benign hits** only after you verified — document, don't blindly rewrite.
4. **Layers:** notify egress firewall, boundary script, doctrine. Never disable the
   firewall; rephrase on false positives.

Hard classes (any deployment): absolute home paths, private org names, host IPs,
chat webhook URLs, API key shapes. Soft: deployment-specific desk codenames — use
your gitignored denylist.

## Fabricated verification

A subordinate verdict built on a check that never ran or used the wrong comparison.

**Worked example (genericized):** a review subagent APPROVED a stacked PR claiming
zero merge conflicts — false. Real merge-tree between siblings showed conflict blocks
including silent-failure traps. The gate-above caught it only by **independently
re-running** merge-tree via merge-base.

Protocol:
1. Re-run the claimed check correctly.
2. Demand honest correction note — what was wrong, how found.
3. Demand methodological root cause (wrong base, over-trusted green on wrong question).
4. File the pattern in your deployment's error taxonomy for future seat scoring.

## Message loss / clipping

Symptoms: truncated operator relay; desk never saw a "sent" message.

1. **Recover:** `flotilla inbox <channel> --limit N`.
2. **Images:** fetch via bot token + channels API; never leak token in public text.
3. **Prevention:** durable channel for decision-gating; `flotilla notify --chunk` for
   long operator bodies.

## Crashed desks

`flotilla status` shows crashed — process gone, pane bare shell.

1. Confirm crashed vs idle vs working.
2. Launch recipe must exist in `flotilla-launch.json` — author if missing.
3. `flotilla resume <agent>`.
4. Whole tmux server dead → fleet-recovery procedure. Watch alive but gateway down →
   gateway-health recovery (don't blindly restart every desk).
5. After recovery: desk reaches idle, liveness updates; re-check your git branch if
   review agents used checkout.

## Session rotation

**Survives:** fleet backlog, liveness files, session mirrors, roster, launch recipes,
daemon-native schedules (`flotilla-schedule-state.json`).

**Dies:** session-local crons inside a harness session — ceremonies belong in the
watch daemon scheduler ([`watch-runbook.md`](../watch-runbook.md)).

**Busy-pane scheduler caveat:** `last_fired` may commit before dispatch confirms.
Verify ceremony artifacts landed (parade dir, scorecards), not just the timestamp.