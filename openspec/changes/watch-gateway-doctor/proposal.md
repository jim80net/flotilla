## Why

On 2026-06-12 the flotilla fleet was silently offline for ~9 hours. The
`flotilla-watch` daemon was `active` the entire time — its relay-open failure is
non-fatal by design, so on a Discord-gateway/DNS failure it degrades to clock-only
and retries the gateway in the background. systemd's `Restart=on-failure` never
fired because nothing crashed. Heartbeats and the inbound operator relay were dead,
but every external liveness signal said "the process is up." The root cause was DNS
(the tailscale MagicDNS resolver `100.100.100.100` timing out on `discord.com`), not
a flotilla bug — and crucially, a blind `systemctl restart` would NOT have fixed it
(the restarted daemon hits the same dead resolver). See the `recover-flotilla`
skill, which encodes the diagnosis-first runbook.

The `watch-heartbeat-sidechannel` change (in flight) names the in-daemon
escalation-trigger set for the change-detector, but it deliberately does NOT touch
liveness and is not a gateway-health probe. There is no mechanism today that detects
the specific "process alive but gateway down" state from OUTSIDE the daemon. That is
the gap this change fills: an external, deterministic watchdog that confirms a
sustained gateway-down and escalates to the diagnosis runbook — without ever
restarting the safety-critical clock.

## What Changes

- **Add an external gateway-health doctor** — a `Type=oneshot` systemd user service
  (`flotilla-doctor.service`) fired by a ~3-minute timer (`flotilla-doctor.timer`).
  Each run is a cheap, pure-bash health check with NO LLM in the cheap path: it
  confirms `flotilla-watch` is active, resolves its MainPID, and checks for at least
  one ESTABLISHED `:443` socket owned by that PID (flotilla only talks to Discord, so
  any established `:443` from its PID means the gateway is up).
- **Confirm before acting.** A single down read is re-checked once after a short
  sleep (avoids catching a momentary reconnect). Sustained-down accumulates strikes
  across ticks; at a default threshold of 3 with a 3-minute cadence, that is ~9
  minutes confirmed-down before any escalation — set against a 9-HOUR real outage.
- **Escalate, never restart.** On reaching the threshold the doctor (a) fires a
  best-effort operator notify with a status payload (gateway state, MainPID, the
  `:443` socket dump, a journal tail, ack-file age, and a per-resolver DNS probe),
  and (b) spawns a time-bounded headless `claude --print "/recover-flotilla <status>"`
  recovery agent that DIAGNOSES the real cause (DNS first) and applies the right fix.
  A cooldown prevents re-spawning the agent every tick.
- **Deploy artifacts** mirroring the existing `flotilla-watch` deploy surface: the
  escalator script, a templated `.service.in`, a static `.timer`, an idempotent
  pure-bash installer (`@PLACEHOLDER@` substitution, `--dry-run`/`--print`,
  fail-loud-on-leftover-placeholder), an `.env.example`, a host-neutral test fixture,
  and a Go installer regression test symmetric with `watch_install_test.go`.

## Capabilities

### Added Capabilities
- `watch`: an external gateway-health watchdog that escalates on sustained
  gateway-down. It is a NEW requirement ADDED to the `watch` capability; it does not
  modify any existing liveness requirement, the heartbeat window, or the daemon's
  spec'd behavior — it observes from outside and escalates.

## Impact

- **No daemon code change.** Pure deploy/ops + a Go installer test. The watchdog
  observes `flotilla-watch` from outside (systemctl + ss + journalctl); it imports no
  daemon internals.
- **Never restarts the safety-critical clock.** The doctor's only actions are notify
  + spawn the recovery agent. Whether a restart is warranted is the recovery skill's
  decision after diagnosis — a blind restart fixes nothing when the cause is DNS, and
  it would violate the "only the operator restarts the safety clock" doctrine the
  watch installer already enforces.
- **No new dependency.** Pure-bash check; `dig` is used only when present (the
  per-resolver DNS probe degrades gracefully without it). The LLM fires only on a
  confirmed escalation, under the user's gatekeeper allowlist (fail-closed — the
  doctor deliberately does NOT pass `--dangerously-skip-permissions`).
- **Backward-compatible / opt-in.** Nothing runs until the operator enables
  `flotilla-doctor.timer`; the installer prints the enable command rather than
  auto-enabling.
- **Cross-reference:** the in-daemon escalation-trigger naming lives in
  `watch-heartbeat-sidechannel`; this doctor is the complementary EXTERNAL probe for
  the one state that change cannot observe from inside the daemon (gateway-down while
  the process is alive). The two are orthogonal and compose.
