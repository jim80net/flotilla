# `flotilla watch` runbook

`flotilla watch` is the long-lived daemon that keeps the fleet's clock and
relays the operator's inbound messages:

- **Clock** (tmux-only — no Discord needed): heartbeats the XO agent on an
  inactivity interval so a turn-based agent keeps advancing clear, already-
  authorized work without operator input, and watches liveness (tick → ack)
  so a dead or context-exhausted XO is surfaced.
- **Relay** (needs the Discord gateway): delivers the operator's channel
  messages into the right agent's pane (`@<agent>` to a desk, bare to the XO).

The clock runs with no Discord configuration at all; the relay activates only
when a `channel_id` + bot token are present.

### Wall-clock schedules (`schedules[]`)

The roster may declare daily wall-clock dispatches the daemon fires without
operator input (flotilla#413). Each entry needs a unique `name`, an `at` time
with an **explicit timezone** (e.g. `12:07Z` or `03:07+00:00`), a `to` agent,
and a `prompt` (inline text or a **host-local file path** — preferred for long
prompts). Durable last-fired state lives beside the roster at
`<roster-dir>/flotilla-schedule-state.json` so a restart does not double-fire or
silently skip a slot missed while the daemon was down (catch-up fires once with
a `[schedule late: …]` prefix). Delivery uses the same injector path as
change-detector wakes (`KindDetector` — dropped when the target pane is busy,
re-evaluated on the next poll).

## Prerequisites

1. **Install the binary:** `go install github.com/jim80net/flotilla/cmd/flotilla@latest` (or `go install ./cmd/flotilla` from a checkout) → `~/go/bin/flotilla`.
2. **Roster** `flotilla.json` (committable, no secrets) with your agents, `xo_agent`, and `heartbeat_interval` (e.g. `"20m"`; `"0"` disables the clock). For the relay also set `channel_id` and `operator_user_id`.
3. **Secrets** `flotilla-secrets.env` (chmod 600): `FLOTILLA_BOT_TOKEN` (relay) and `FLOTILLA_WEBHOOK_<AGENT>` lines (audit mirror + alert/notice posts).
3b. **Org-truth (optional)** `fleet-org.yaml` beside the roster (or `--org-file` / `$FLOTILLA_ORG_FILE`): primary-parent org DAG. Absent ⇒ derive from `channels[]`. Present ⇒ load refuses if `reports_to` disagrees with channel-derived parents or multi-home seats lack `home_channel_id`. See `fleet-org.example.yaml` and `openspec/changes/org-truth-v1/`. **PR3:** after a successful load, visibility-synthesis owed marking (`AgentsAbove`/`AgentsBelow`) and stackable `OwningXO` **consume the compiled org DAG** — not a second channel-only graph. Org compile/agreement failure is **fatal** to `flotilla watch` start (error returned before relay/detector). Startup logs `org-truth DAG source=file|derived`.
3c. **Adjutant arc coalesce (buffer-v2 B1):** operator messages buffered for an adjutant layer share an `arc_id` when they share **leader + channel + operator** within a quiet window (`$FLOTILLA_ADJUTANT_ARC_QUIET`, default `60s`, clamp 45–90s; `0` disables join). Seam drain forwards **one** verbatim multi-body payload per arc (delimiter `---`). See `openspec/changes/adjutant-buffer-v2/`.
3d. **Adjutant operator-loop webhooks (#628):** every `adjutant_for` seat that keeps the operator in the loop must have `FLOTILLA_WEBHOOK_<ADJUTANT>` in secrets **and** `FLOTILLA_SECRETS` + `FLOTILLA_ROSTER` on its launch recipe so `flotilla notify` resolves. Generic example: `FLOTILLA_WEBHOOK_XO_ADJ` for `xo-adj`. Ordinary `mirror-self` and finish-edge turns are ledger-only; a webhook is required for curated notify and explicit parade egress. Install detail: `llm.md` §6 "Adjutant operator-loop egress".
4. **Relay only — enable the bot's Message Content intent:** Discord Developer Portal → your bot → Privileged Gateway Intents → **Message Content** = ON. Without it the gateway connects but message bodies are empty, so the relay sees nothing.
5. **Security — enable Discord two-factor auth on the operator account.** The channel is a command-injection surface gated only by `operator_user_id`; the operator's account is the real boundary (same posture as any privileged operator-gated agent). The bot token can READ the channel, so keep `flotilla-secrets.env` at chmod 600 and never commit it.
6. **XO permission posture.** The XO session must be allowed to (a) `touch` the ack file (its liveness ack) and (b) act on the tick's instruction to advance work — otherwise the watchdog will (correctly) flag it as unresponsive. Run the XO with an allow-list that includes these (the project's posture-(b)).
7. **Register each desk's pane (drift-proof resolution).** flotilla resolves a desk by its pane title, but Claude Code retitles its pane to a task summary every turn, which breaks `send`/heartbeat resolution until re-pinned. Tag each pane once with a stable marker — `flotilla register <name>` inside the pane at launch (see [quickstart §3 → "Make resolution drift-proof"](./quickstart.md#make-resolution-drift-proof-flotilla-register)). To repair an already-drifted desk without interrupting it, the XO runs `flotilla register <name> --pane <target>` from its own pane. Untagged panes still resolve by title (the marker is purely additive).

## Install the service

The systemd unit is **generated** from the repo template
`deploy/flotilla-watch.service.in` + a per-host path config, by
`deploy/flotilla-watch-install.sh`. Never hand-edit the installed
`~/.config/systemd/user/flotilla-watch.service` — edit the env and re-run the
installer (that is what keeps the unit from drifting).

```bash
# 1. Place the roster + secrets wherever you like (the env records the paths).
mkdir -p ~/.config/flotilla
cp /path/to/flotilla.json          ~/.config/flotilla/flotilla.json
cp /path/to/flotilla-secrets.env   ~/.config/flotilla/flotilla-secrets.env && chmod 600 ~/.config/flotilla/flotilla-secrets.env

# 2. Create THIS host's path config from the example and edit the five paths.
cp deploy/flotilla-watch.env.example deploy/flotilla-watch.env
$EDITOR deploy/flotilla-watch.env        # FLOTILLA_WORKDIR/BIN/ROSTER/SECRETS/ACK_FILE

# 3. Generate + install the unit (idempotent; --dry-run first to preview the diff).
bash deploy/flotilla-watch-install.sh --dry-run    # preview; writes nothing
bash deploy/flotilla-watch-install.sh              # generate + daemon-reload

# 4. Enable + start.
systemctl --user enable --now flotilla-watch.service
```

`deploy/flotilla-watch.env` is gitignored (per-host paths, not secret). The
installer validates the roster/secrets paths exist, refuses to emit a unit with
an unsubstituted placeholder, and **never restarts a running clock** — if you
re-run it after a change while the service is live, it prints the
`systemctl --user restart flotilla-watch.service` for you to run when ready. To
change paths later, edit `deploy/flotilla-watch.env` and re-run the installer.

## Keep the binary in sync after a merge (merged ≠ running)

**Merging watch/relay/detector code does NOT change what the live service runs.**
`flotilla-watch.service` execs the binary at `~/go/bin/flotilla` and holds that
in-memory image until the process restarts. So a PR that lands on `origin/main`
is *merged* but not *running* — the daemon keeps the binary it was started with
until you rebuild **and** restart. The installer above (re)generates the systemd
*unit*; it never rebuilds the binary and deliberately never restarts the clock.
Those are two separate deploy steps from the merge.

After merging any PR that touches `cmd/flotilla/watch.go`, `internal/watch/`,
`internal/relay/`, `internal/deliver/`, or `internal/surface/` — anything the
running watch executes — redeploy in three steps:

```bash
# 1. Rebuild the binary from the merged code (safe + non-disruptive: the running
#    daemon keeps its old in-memory image; this only rewrites ~/go/bin/flotilla).
cd /path/to/flotilla && git pull         # land the merge in your build checkout
go install ./cmd/flotilla                # → fresh ~/go/bin/flotilla

# 2. Restart the service to pick up the new binary. THIS is the operator/XO-owned
#    step — it briefly stops the safety-critical heartbeat clock, so do it
#    deliberately, not as a reflex of the merge.
systemctl --user restart flotilla-watch.service

# 3. Refresh constitutional doctrine into every desk's identity file when the merge
#    shipped updated embedded doctrine assets (identity-append blocks are marker-
#    guarded; plain install skips present markers and would strand stale content).
bash deploy/flotilla-doctrine-refresh.sh    # uses deploy/flotilla-watch.env paths
```

Step 3 is identity-file only — it does not restart the watch daemon. Skip it when
the merge did not change `internal/doctrine` embedded assets.

Verify the swap took: the binary's mtime is fresh and the merged HEAD is in the
build checkout —

```bash
ls -l --time-style=full-iso ~/go/bin/flotilla   # mtime == the rebuild you just ran
git -C /path/to/flotilla log --oneline -3        # the merged PR is in HEAD
journalctl --user -u flotilla-watch -n 20        # clean restart: `clock running …`
```

Step 1 is free and reversible (rebuild any time; the running clock is untouched
until the restart). Step 2 is the deploy proper. Splitting them is intentional —
it lets the binary be staged ahead of a restart the operator/XO times safely.
Tracking issue for a one-command convenience wrapper:
[#69](https://github.com/jim80net/flotilla/issues/69).

## Verify

```bash
journalctl --user -u flotilla-watch -f
```

Expect `clock running — XO=… interval=… ack=…`, then either `inbound relay active`
(channel + token present) or `clock-only (relay disabled …)`. Within one
heartbeat interval an idle XO should receive the heartbeat prompt in its pane.
Post a message in the channel as the operator and confirm it lands in the
target pane.

### Confirmed delivery (an operator message is never silently dropped)

A relayed operator message is delivered with **confirmation**, not fire-and-forget. The
daemon submits into the XO pane and then verifies a turn actually started (the
`Idle → Working` edge) before it logs `… delivered to "…" (N bytes)`. So that success line
now means *a turn started*, not merely *the tmux keystrokes ran*. Concretely:

- **The XO is busy when your message arrives** → it is **not** pasted into the active
  composer (that was the silent-drop bug). It is deferred and re-tried every few seconds;
  it lands (and is confirmed) once the XO goes idle. Delivery to other desks continues
  meanwhile.
- **The XO stays busy for ~30s** → you get ONE loud alert: `operator message to "…" is
  QUEUED — the XO has been busy …`. It still delivers when the turn ends.
- **The XO is busy for ~5 min, or crashed, or the submit can't be confirmed** → you get a
  loud alert (`… UNDELIVERABLE …` / `… NOT delivered …`) and the message is dropped rather
  than retried forever. A genuinely wedged/crashed XO is also caught by the liveness
  watchdog (see Down alerts).
- **The XO's composer is input-blocked behind the Claude Code agents panel** → the inline
  background-agents panel / a per-agent message sub-composer can steal input focus from the
  main composer; keystrokes then navigate the overlay instead of submitting. Confirmed delivery
  reads the composer AT THE CURSOR and DETECTS this; it refuses to paste into the wrong place
  (the body is never lost or mis-delivered). **Auto-recovery (#156, opt-in):** when
  `FLOTILLA_SELF_HEAL=1` is set, an OPERATOR-RELAY send to an overlay-blocked, idle desk first
  attempts a bounded **Ctrl-C self-heal** — a Ctrl-C escapes the overlay back to the composer
  (the empirically-correct key; Esc does NOT recover the inline panel). It is bounded and
  **re-probes between each press, stopping the instant the composer is reachable** — never a
  Ctrl-C into a recovered composer (Claude Code's "second Ctrl-C exits" would otherwise kill the
  session), never a Ctrl-C into a Working pane (would interrupt the turn). On recovery the
  message is delivered with no alert. The self-heal is **DEFAULT-OFF** (a destructive primitive)
  and only attempted for relay kinds (a heartbeat/detector tick never fires Ctrl-C); flip
  `FLOTILLA_SELF_HEAL=0` to disable instantly. If self-heal is off, fails, or can't recover, the
  desk gets the loud TERMINAL alert (`… NOT delivered — its composer did not accept the message
  …`) and **recovery is a manual keystroke at the pane** (a Ctrl-C, or click the composer), then
  re-send. The alert hedges to *verify the turn did not already start before re-sending*.

So a dropped operator message is **always** surfaced via the down-alert path — never
silent. If you see a delivery alert, the message did **not** reach the XO; re-send once the
XO is idle (or recover it). `flotilla send` from the shell behaves the same: it prints
`delivered … — turn confirmed` on success, or exits non-zero with `… is busy … NOT
delivered` / `… is input-blocked behind the Claude Code agents panel …` so you know to retry.

**Codex selector safety (#692).** A highlighted selector or modal overlay is not a
composer even though its selected row uses the same `›` glyph. Delivery and `flotilla
switch` fail closed without sending keys until the selector is dismissed in the target
pane. Clear the overlay manually, confirm the normal composer footer is visible, and
retry. A passive limit banner above that normal footer is only a notice: delivery and
switch phase 0 may proceed. Exact rate-limit overlay prose remains pending a live
capture under #690; safety depends on the selector/composer structure, not those words.

Heartbeat / change-detector ticks flow through the same confirmed delivery (so they too
recover a dropped Enter), but they are **time-relative**: a tick that arrives while the XO
is busy is dropped, not deferred (the next tick re-evaluates), and a failed tick never
escalates (XO liveness is the watchdog's job, see Down alerts). The trade is a small added
per-tick cost — up to ~1.75 s of confirm polling on a tick that has to retry — paid off the
delivery worker, so it never stalls other desks.

### Operator channel acknowledgement contract

The unacked-message backstop accepts either an explicit fleet webhook reply in the
origin channel or a mechanical turn-final marker. On confirmed operator-relay delivery,
watch records the exact `(origin channel, message ID, delivered seat)` tuple. The next
substantive turn-final from that seat consumes the pending tuple and writes a durable
marker under `<roster-dir>/flotilla-operator-acks/`; the desk does not need to remember
or echo an acknowledgement token. The backstop checks that exact channel/message marker
before alerting, so an older acknowledgement cannot settle a newer operator message and
another seat's turn-final cannot settle the delivery.

Outbox sends are outside this contract. A `KindSend` job, including a canceled or
superseded sender→recipient epoch, never creates an operator marker; only a confirmed
operator relay with origin metadata can do so. An alert therefore means watch found
neither a channel reply nor the exact mechanical marker. Increasing the age threshold is
not part of the acknowledgement policy.

### At-least-once ingestion — gateway-gap recovery (#161)

Confirmed delivery (above) guards the **delivery** layer — after a message reaches the
relay. A separate failure class is a message that **never reaches the relay at all**: the
Discord gateway websocket drops, and on a failed resume discordgo re-identifies (replaying
no `MESSAGE_CREATE` events), so an operator message sent during that window is lost with no
trace. (Measured on the live fleet: ~3 such gap-losing reconnects/day.)

The daemon closes this with a **REST-based catch-up reconciler**, independent of the
websocket (REST works precisely when the socket is unhealthy):

- It keeps a **durable per-channel cursor** (`--relay-cursor-file`, default
  `<roster-dir>/flotilla-relay-cursor.json`) of the highest message it has processed.
- Every ~30 s — and **immediately on every gateway reconnect** — it fetches each bound
  channel's messages after the cursor and recovers any operator message the live path
  missed. The live path and the reconciler share a dedup set, so a message delivered live
  is never re-delivered.
- **First boot tail-initializes** the cursor (it never replays old history). A daemon
  restart resumes from the durable cursor, so messages sent while it was down are recovered
  too.
- **Disposition:** a few recent recovered messages are **auto-delivered** in order with a
  one-line `recovered N operator message(s) … via catch-up` notice. A **bulk or very old**
  backlog (e.g. after a long outage) is **NOT** blind-injected — you get a loud alert
  pointing you at `flotilla inbox` to review and re-send the still-relevant ones.
- **The backstop watches itself:** if its REST sweep fails repeatedly (total outage), it
  raises ONE alert that the at-least-once backstop is DOWN (live delivery continues), and
  re-arms on recovery — so a silently-dead backstop can't re-create the very gap it fixes.

It is **non-fatal**: if the REST client can't start, the daemon logs a warning and runs
live-relay-only (the clock and live relay are unaffected). On startup you'll see
`relay catch-up backstop active (cursor=…)`.

### Reading or recovering a channel by hand — `flotilla inbox`

To read recent messages of a bound channel directly over REST (e.g. to recover a message,
or just to see what was said) — no daemon, read-only:

```bash
flotilla inbox <channel> [--limit N]      # <channel> = a binding role or a raw channel id
flotilla inbox fleet-command --limit 30
flotilla inbox 1511357941893304462
```

It prints the recent messages oldest-first with each one's timestamp, an authorship flag
(`[OP]` operator, `[wh]` webhook/mirror, `[..]` other), id, and full (multi-line) content.
A role shared by several channels is rejected as ambiguous — pass the channel id. (It is
read-only; it does not re-inject — the catch-up reconciler already does automatic recovery.)

### Relay open is non-fatal (cold-boot / transient-network resilience)

The safety-critical clock (heartbeat + watchdog) and the optional inbound relay
are decoupled at startup: **a relay-gateway open failure NEVER takes down the
clock.** If the Discord gateway can't open at startup — most commonly a cold-boot
DNS blip where `systemd-resolved` isn't answering yet ~6s into a power-failure
reboot, or any transient network hiccup — `flotilla watch` logs a degraded
warning to the journal, keeps the clock running, and retries the gateway in the
background with bounded exponential backoff (5s → … → 2m cap) until it succeeds
or the daemon is shut down. The journal will show:

```
flotilla watch: WARNING — inbound relay failed to open (…); running CLOCK-ONLY and retrying in the background. The safety-critical heartbeat/watchdog is unaffected.
…
flotilla watch: inbound relay active (recovered)
```

The per-attempt degraded warnings go to **stderr (journald) only**, never the
Discord down-alert webhook — that webhook needs the same network that just
failed. The unit also carries a best-effort `ExecStartPre` that waits up to ~60s
for `discord.com` to resolve (always exiting 0, so it can never block the clock);
the code-side retry is the actual correctness guarantee, the pre-flight just
narrows the window where the relay starts degraded.

**Sustained-down escalation (one Discord alert, not a silent retry-forever).** A
normal boot-DNS-blip recovers within the first one or two retries. If the relay is
*still* down after **5 consecutive failed attempts**, that is no longer a
transient blip — it is a genuine misconfiguration (most often a bad bot token) or
a real outage. By the 5th attempt the network is almost certainly back, so
`flotilla watch` escalates **exactly once** via the operator down-alert webhook
(falling back to stderr if no webhook is configured):

```
⚠️ flotilla watch: relay still down after 5 attempts (last error: …) — if this persists, check the bot token / network. The safety-critical clock is unaffected; retries continue.
```

It does **not** alert again for the same down-episode (one alert per sustained
outage), and it keeps retrying forever in the background — so a long outage still
self-heals on recovery (you'll see `inbound relay active (recovered)` when it
does). The escalation covers the **startup** down-episode the controller manages:
once the relay is up, the retry goroutine exits and a *later* disconnect is handled
by discordgo's internal auto-reconnect (not re-escalated via this path). This
replaces the silent-misconfiguration guard the old `StartLimitBurst` give-up used to
provide (it now surfaces the same condition loudly instead of killing the daemon).

This is the fix for the 2026-06-10 power-failure crash-loop: previously a failed
`gw.Open()` returned a fatal error, killing the already-running clock; the unit's
`StartLimitBurst=5` then exhausted all restarts in ~76s (before DNS settled) and
landed the service permanently `failed` — taking the down-alert relay with it, so
the operator got no alert. `Restart=on-failure` + the start-limit still guard
*genuinely* fatal startup errors (a malformed roster, an unknown surface, a
missing `operator_user_id` alongside a configured `channel_id`).

## Clock-only mode

Omit `channel_id` (or the bot token) to run the heartbeat + watchdog with no
Discord relay — useful to give the XO a self-continuing clock before the relay
is configured.

## Change-detector (heartbeat v2 — opt-in)

The legacy clock wakes the XO **every interval** with a generic "do your duties"
prompt — so the XO burns context on every tick even when nothing changed. Set
`change_detector: true` in the roster to switch to **heartbeat v2**: a
deterministic, no-LLM detector that each tick snapshots the fleet (every desk's
assessed surface state + the optional external signal-file's hash), diffs it against
the prior snapshot, and **wakes the XO only on a material change** — with a prompt
naming what changed. An idle fleet costs **$0/tick**, and the XO's context is
rotated after each settled handling (via the surface driver: claude-code →
`/clear`), so even a busy fleet never accumulates XO context.

```jsonc
// roster:
{ "xo_agent": "xo", "heartbeat_interval": "20m",
  "change_detector": true, "liveness_ping_mode": "none" }
```

**What counts as material** (v1): a desk transition INTO an actionable state
(`shell` = crashed, or — when a driver emits them — `errored`/`awaiting-input`/
`awaiting-approval`), a desk `working → idle` ("finished a turn"), or a change in
the optional **external signal-file**'s hash (`--signal-file`). A desk merely
*resuming* work (`→ working`) is deliberately NOT material — that keeps the wake set
tight. The XO's own `working → idle` feeds **self-continuation** (below), never a
desk-finished wake.

**Steady-state awaiting backstop:** transition detection remains the fast path,
but a pane continuously observed in `awaiting-input` or `awaiting-approval` for
15 minutes emits one additional material wake to its owning coordinator layer.
This covers a pane that was already blocked when watch started. Switching between
the two awaiting states remains one episode; leaving both states re-arms the pane.
A daemon restart begins a fresh 15-minute observation window, avoiding an immediate
fleet-wide restart burst while preventing a pre-existing wedge from staying silent
forever.

> **The XO's own state tracker (`.flotilla-state.md`) is NOT a wake signal.** The
> heartbeat instructs the XO to keep the tracker current, so hashing it as a wake
> source would self-wake the XO on its own writes (a loop until it settles). The
> tracker is only the continuation prompt's `{{tracker}}` read-source. Genuine
> *external* state the XO must react to (a desk or tool dropping a signal) goes
> through the separate, optional `--signal-file` — a file the XO does **not** write.

**Self-continuation + the markers.** On the XO's own `working → idle` the detector
wakes it once to advance the next authorized step, rotating context between steps;
the XO signals "nothing to do" with the **settle marker** and signals "I'm
awaiting the operator" with the **awaiting marker** (which vetoes the rotate). A
hard cap (`--max-self-continuations`, default 3) bounds a runaway. The exact
lifecycle the XO must follow is in [`xo-doctrine.md`](./xo-doctrine.md#the-change-detector-heartbeat-v2-and-the-discipline-it-demands)
— wire it into the XO's standing instructions, and permit the marker `touch`/`rm`
plus `tmux send-keys` (the rotate path) in the XO's allow-list.

### Return-to-frontier authority and delegation

The frontier sidecar distinguishes `origin: authored` from `origin: derived`.
Directly saved coordinator/operator frames are authored. A seam interrupt may derive
a return-to pointer from the backlog only when no non-empty frontier exists; derived
state is a fallback and never overwrites an existing authored or legacy frame.

Delegated work is not the coordinator's return-to. Mark an `[in-flight]`, `[pending]`, or `[next]`
backlog line either with `[delegated]` or with `DELEGATED —` immediately after its
status marker (for example, `- [in-flight] DELEGATED — implementation owned by a
desk; do NOT re-dispatch`). Frontier derivation skips those lines and chooses the
first non-delegated unblocked item. Detection runs before the 120-character pointer
truncation; when every unblocked item is delegated, no derived frontier is written.

**Liveness ping mode** (`liveness_ping_mode`, the C1b tradeoff) — switchable
without a rebuild:

| mode | idle cost | wedge-detection window | use when |
|------|-----------|------------------------|----------|
| `none` *(default)* | true $0-idle (a wide safety ping only at ~2K×interval) | ~2K×interval on a truly-idle fleet (a **crash is still immediate**) | you want the cheapest idle; a wedged XO on an idle fleet has nothing to miss |
| `interval` | a cheap ack-only ping every K-1 intervals | strict **K×interval** | you want the legacy-tight wedge window and don't mind a ping per idle interval |
| `consecutive` | a ping every K-1 intervals | ~K-1+2 intervals (2 missed pings) | a middle ground |

`K` is `--max-missed-acks` (default 3); `N` (the ping cadence) defaults from the
mode and can be overridden with `--max-quiet-intervals`. The default `none`
honors the project ruling that the $0-idle win is the point and the
slow-idle-wedge cost is near-zero; flip it per deployment if you need the strict
window.

> **`interval`/`consecutive` assume the XO's ack round-trip fits in under one
> interval.** In `interval` mode the ping fires at `K-1` and the alert at `K`, so
> only ~1 interval is left for the wake → turn → `touch` → next-tick round-trip; a
> healthy-but-slow XO on a short interval can then false-alert "wedged." If you
> run a short interval, widen the gap with `--max-quiet-intervals` (a smaller `N`)
> or a larger `--max-missed-acks` (a larger `K`) — or stay on `none`, where the
> safety ping at `2K` precedes the alert at `2K+1` with a full window of slack.

**Detector files** (all default under the roster dir; override via flag or env):
`--snapshot-file` (the diff state), `--awaiting-file`, `--settled-file`,
`--tracker-file` (`.flotilla-state.md`, the XO's `{{tracker}}` read-source — NOT
hashed as a wake signal), and `--signal-file` (`$FLOTILLA_SIGNAL_FILE`, **optional**,
unset by default — the external-signal wake source; a file the XO does not write).
The snapshot is written atomically and
read fail-safe: a missing/corrupt snapshot cold-starts (one conservative wake);
a persistent write failure raises a loud alert and degrades to in-memory-only
(never wake-every-tick). Liveness state is kept independent of the snapshot, so a
snapshot outage can never blind the watchdog.

The legacy always-wake heartbeat is unchanged when `change_detector` is unset.

## Down alerts

**Legacy clock:** when the XO misses `--max-missed-acks` consecutive acks
(default 3) or its pane falls back to a shell, `flotilla watch` posts a one-line
`⚠️ XO … restart needed` to the channel (via the XO webhook), or to stderr if no
webhook is configured.

**Change-detector:** liveness is re-grounded on **wall-clock ack age** (since v2
no longer prompts the XO every interval): the detector alerts when the ack file's
age exceeds the mode-derived window while the XO is not a shell, and on a crash
(two consecutive shell reads — debounced so a transient tmux blip never
false-alarms). A wide-mode (`none`) idle fleet keeps the XO acking via the wide
safety ping.

In both cases the alert fires once on the down-transition and clears on recovery.
Recovery is manual today: `flotilla resume <xo>` restarts the XO from its
host-local launch recipe (see the quickstart's `flotilla resume` section).
`resume` is the deterministic building block the future opt-in
`watch --resume-xo` will call on the down-transition to auto-respawn the XO
(guarded by a storm rate-limiter) — that auto-composition is the next milestone;
the manual command lands first. Under the change-detector, a freshly-restarted
XO is not auto-woken to resume coordination (a restart implies fresh context and
the operator is in the loop) — re-engage it with an operator message, which
clears any settled state and lands in its pane; the next material change or
safety ping then resumes the normal cadence.

## Coordinator session-mirror without pane Working→Idle (#572)

Desk finish-edge mirrors fire on detector `Working→Idle`. A **pane-less or
remote-control coordinator** (CoS / XO) often never emits that edge — Assess
cannot see a useful pane — so `CoordinatorMirrorOnFinish` stays quiet and
dash conversations miss the turn-final.

**Trigger-independent path:** harness Stop hooks (Claude:
`deploy/flotilla-xo-discord-mirror.sh`) call:

```bash
flotilla mirror-self --from <coord> --roster <path> --secrets <path> --file <turn-final>
```

That runs the same `deskMirror` pipeline as the detector (reader-model →
session-mirror jsonl → CoS ledger for coordinators). The operator-channel step is
ledger-only by default; an explicit pending parade marker is the only turn-final
allow. Missing webhooks never prevent the session-mirror write (#506/#572/#683).

Desks keep using detector `MirrorOnFinish`; the Stop hook self-gates to
`xo_agent` / `cos_agent` / `coordinator:true` / `*-xo` so desks are not dual-posted.

## Recycle a desk's chapter — `flotilla recycle` (#157)

`flotilla recycle <desk>` closes a desk's chapter and restarts it with a **fresh
context window**, preserving the chapter via the desk's **own handoff** — so a
desk never has to run until it compacts (post-compaction quality degrades). The
**mechanism** is flotilla's; the **trigger** is the XO's judgment (it decides
WHEN a chapter is logically complete). It is the context-preserving sibling of
`resume`: `resume` (re)starts a *dead* desk and restores no context; `recycle`
gracefully closes a *live* desk after its handoff is durable, then relaunches and
hands the fresh session the handoff.

### How to run it

The XO runs `flotilla recycle <desk>` **in a pane it controls** (e.g. its own
shell) and reads the phase-by-phase result on that command's stdout — status
never goes into the recycled desk's composer. **Run `--dry-run` first** to
preview the resolved plan (pane, recipe, the designated handoff path, the exact
handoff/takeover turns it would inject) without acting:

```bash
flotilla recycle backend --dry-run     # preview, no action, no lock
flotilla recycle backend               # execute the fail-closed pipeline
```

The outcome is also written to `~/.flotilla/<desk>/last-recycle.json` (atomic
write), so it survives the process / a relay outage and a cold-pickup XO can read
it. The whole pipeline can take up to ~7.5 minutes worst-case (a handoff turn is
multi-minute; the close/boot/takeover edges are seconds) — beyond that, suspect
the command itself is wedged.

### The fail-closed pipeline (and its guarantee)

1. **Idle precondition** — waits for the desk to be idle at a cleared composer
   (the XO often triggers mid-turn); never injects into a busy pane. Recycle
   assesses the **live pane harness** (`pane_current_command`), not only the
   roster/overlay surface (#586): a cutover lag (roster still `claude-code`
   while the pane runs `grok`) would otherwise leave the composer
   Undetermined forever and abort as busy-desk on a parked empty composer.
   Standing backlog `[blocked]` items only affect status `loop_posture`
   ("idle blocked"); they do **not** block phase-0 when the pane is idle and
   the composer is cleared.
2. **Handoff** — injects a NON-INTERACTIVE turn telling the desk to write a
   handoff (per the `/handoff` FORMAT, not the interactive skill) to a designated
   gitignored path as an **untracked file** (never `git add`/`commit`), then stop.
   Gates on that file going **absent→present on disk** AND non-trivial AND the turn
   returning to an idle cleared composer. **If the handoff is not durably confirmed,
   recycle ABORTS — the desk keeps running, nothing is closed** (the worst case is a
   no-op recycle, never a lost handoff: *at-most-once handoff-artifact-loss*).
3. **Graceful close** — only after the handoff is durable. A desk launched as its
   pane's direct process (the live fleet's `claude --remote-control`) would *close*
   the pane on `/exit` rather than drop to a shell, so recycle sets the pane's
   `remain-on-exit` on first (the exit then leaves a *dead* pane to revive) and
   confirms the close by the pane being **dead** (or a shell verdict) before
   relaunching — the relaunch is an unconditional `-k`, so this confirmation is
   what prevents killing a still-live session. `remain-on-exit` is restored after.
4. **Relaunch** — reuses the pane id, so the `@flotilla_agent` marker survives and
   the control channel re-binds for free; reuses the hardened `resume` path.
5. **Takeover** — points the fresh session at the handoff with an imperative
   begin-immediately turn, then watches for it to start working.

Requirements: its surface is **recycle-capable** (today: Claude Code and grok).
A non-recycle-capable / copy-mode / self-targeted (recycling the XO's own pane)
desk is **refused cleanly**, never silently degraded.

> **The launch recipe must be a COLD start.** Recycle relaunches via the desk's
> launch recipe verbatim, so a recipe that resumes the prior session (e.g.
> `claude --continue` / `--resume`) would relaunch into the OLD bloated context —
> silently negating the "fresh context window" the whole feature exists for (the
> handoff is still written and injected, but into a non-fresh session). Recycle
> cannot detect this, so eyeball the `relaunch:` line in `--dry-run` first and
> ensure the recipe is a cold launch (no `--continue`/`--resume`).

### Recovery from an abort (state-aware)

- **Handoff never confirmed** → the desk is still running with its context — no
  recovery needed; investigate why the desk didn't write/commit and re-run.
- **Close did not confirm a shell** → the desk MAY still be live; investigate, and
  if confirmed dead recover with `flotilla resume <desk> --force` (`resume`
  refuses a non-shell pane without `--force`).
- **Relaunched but the marker mismatched** → the fresh session is LIVE but
  contextless; hand it the chapter with
  `flotilla send <desk> 'read <handoff-path> and take over per it, begin immediately'`.
- **A wedged composer / overlay blocked the close** → context is preserved and the
  desk is still running; clear the overlay (or set `FLOTILLA_SELF_HEAL=1`) and
  re-run.

### Remote-desk coordination — parlay via message, never an in-pane menu

A recycled desk is **remote-driven**: the injected takeover turn tells it to
surface any clarification via a **flotilla message** (`flotilla notify` / a
channel message), NEVER an in-pane interactive prompt (`AskUserQuestion` / a
menu). A remote XO over the relay cannot answer an in-pane menu — keystrokes
navigate it, they do not select. This is a flotilla coordination invariant, not a
per-desk preference.

### Content-first seam briefs (Wave 0.2)

Bare finish-edge buffer keys (`…finished a turn (working→idle)`) are **mechanical**.
They are auto-consumed at the adjutant seam and **never** listed under `Needs you:`.
Only judgment items produce a leader inject brief.

### Operator-channel turn-final policy (#683)

Finish-edge mirror and `mirror-self` always append the session-mirror ledger, but
default to no Discord post. The explicit parade marker carries a random per-request
token, expires fail-quiet, and is claimed only when the completing turn matches that
token after the durable append succeeds. Curated `flotilla notify` posts directly
and bypasses the turn-final mirror.

`flotilla notify` also strips any #472 dispatch footer from the operator-facing
body while retaining the original in the durable context ledger. Recipients settle
that protocol with `flotilla dispatch-ack <nonce>` instead of echoing machine text
in operator prose.

### Undelivered dispatch → adjutant first (#628)

`dispatch undelivered` / `dispatch undelivered-ack` journal lines always stay loud.
When the recipient's owning XO (or primary XO) has an `adjutant_for` binding, the
first age crossing injects a triage wake to that adjutant — **not** the operator
webhook. Operator Discord is second-layer (after ~3× the first age, if still
undelivered). No adjutant → operator remains the only Discord surface.

### Operator notify + fleet status (#625)

Coordinator-class Discord reports should include fleet posture. Use:

```sh
flotilla notify --from <coordinator> --with-fleet-status --file body.md
```

This appends a compressed **Status of the fleet** block from the detector
snapshot (same source as `flotilla status --json`): histogram + working /
blocked / awaiting lists, skipping the `--from` agent and its adjutant. If the
body already has `**Status of the fleet**` or `**Fleet status**`, the flag is
a no-op (idempotent). Snapshot read failure appends `(unavailable)` — never
silent omit when the flag is set.

### Chapter-end auto-recycle (#443)

When a desk **finishes a meaningful body of work** (lane-done: backlog unblocked
empty + settled/PR-merged/coordinator-mark turn-final), the watch daemon
auto-dispatches `flotilla recycle <desk>` so sessions do not accumulate chapters.

| Env | Default | Effect |
|---|---|---|
| `FLOTILLA_CHAPTER_END_RECYCLE` | ON | Auto-recycle on lane-done finish edges |

**Stacked-PR suppression:** a mid-stack PR merge with remaining `[in-flight]` /
`[next]` backlog items does **not** recycle (would destroy stack context).

**Coordinators:** chapter-end uses `flotilla recycle <coord> --self` — handoff +
in-place rotate + takeover, never bare `/clear`, never process-kill the seat that
issued the command (#437). **`--self` does not change model or surface** (no
process respawn, no re-read of `flotilla-launch.json` for a new harness binary).

#### Coordinator model/surface cutover (#437 reopen)

When a coordinator must move to a new harness or model (e.g. recipe flipped to
grok-4.5), **`--self` is the wrong tool** — it only rotates context in the live
process. Cutover is the same full recycle desks use, run from a **non-target**
pane so the command is not killed with the seat:

```bash
# From the adjutant pane, meta-XO, or watch host — NOT from the coordinator's own pane.
# Launch recipe is resolved the same way as `flotilla resume` (workspace overlay +
# flotilla-launch.json); --dry-run shows the relaunch line first.
flotilla recycle cos --dry-run
flotilla recycle cos

# Optional: adjutant-driven pattern for a project XO
flotilla recycle alpha-xo
```

| Invocation | Where | Effect |
|---|---|---|
| `flotilla recycle <coord> --self` | Own pane or external | Handoff + rotate + takeover; **same process/model** |
| `flotilla recycle <coord>` | External only | Full close + **respawn with launch recipe** (model/surface cutover) |
| `flotilla recycle <coord>` | Own pane | **Refused** (would kill the driver) |

Own-pane `--self` skips the phase-0 idle wait (the session driving the command
cannot register idle while the command runs); phase-1 still requires a durable
handoff and idle∧cleared after the handoff turn. Prefer external full recycle
for cutovers so phase-0 and recipe relaunch both apply normally.

**Ceremonies:** ride the same primitive (recycle → fresh session → ceremony
prompt). No special ephemeral runner (#435 withdrawn).

### Abort escalation (#436)

Fail-closed aborts are **not log-only**. On non-zero recycle:

1. Loud log with abort class + recovery command
2. Durable `~/.flotilla/<desk>/last-recycle-abort.txt`
3. Inject into the owning coordinator's pane when resolvable

Busy-desk (phase 0 / re-verify) aborts **retry** a small bound before final
escalation. Subagent/list-nav overlays during close are self-healed when
`FLOTILLA_SELF_HEAL=1`.
