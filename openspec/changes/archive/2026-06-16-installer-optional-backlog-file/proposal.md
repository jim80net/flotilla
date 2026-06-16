## Why

The `Backlog-gated goal-driven continuation` requirement (watch spec) is opt-in via the daemon's
`--backlog-file` flag, but the anti-drift installer (`deploy/flotilla-watch-install.sh`, which
generates `~/.config/systemd/user/flotilla-watch.service` from `flotilla-watch.service.in` + a
host-path `.env`) only knew the five REQUIRED keys. A host that wanted the backlog gate had to
hand-write a systemd drop-in (`flotilla-watch.service.d/backlog.conf`) — exactly the unit drift the
installer exists to prevent. The Spark host did this; the proper fix is an installer-supported
optional key so the gate is enabled the same anti-drift way as every other config.

## What Changes

- **A 6th, OPTIONAL `.env` key `FLOTILLA_BACKLOG_FILE`.** SET ⇒ the generated `ExecStart` gains
  ` --backlog-file <path>`; UNSET ⇒ the arg is omitted ENTIRELY (no `--backlog-file ''`, no trailing
  space) and the unit is byte-identical to a no-backlog install. The five existing keys stay
  REQUIRED — the new key is NOT added to the required-missing check, so existing installs do not
  break.
- **Mechanism: a computed-fragment placeholder `@FLOTILLA_BACKLOG_ARG@`** appended directly to the
  `ExecStart` line in the template (no separating space). The installer ALWAYS substitutes it — to
  ` --backlog-file <path>` (leading space included) when set, or to `""` when unset — so the
  existing fail-loud "no placeholder survives" guard still holds, and the unset path yields the
  identical line as today.
- **The inherited-env pre-clear is extended to `FLOTILLA_BACKLOG_FILE`.** The live host EXPORTS this
  var (the binary reads it as the flag's env default), so without the pre-clear an inherited value
  would inject `--backlog-file` even when the `.env` omits the key — breaking the
  byte-identical-when-unset guarantee on exactly the host this enables. The value MUST come from the
  `.env` only.
- **Consistency choices:** ExecStart-arg form (NOT `Environment=`), matching the existing
  roster/secrets/ack-file args — one config mechanism, visible in `systemctl cat`. A missing backlog
  file at install time is a non-fatal WARNING (the XO creates it; the gate is inert until it exists),
  unlike the roster/secrets hard-prerequisite errors.

No Go daemon code changes: `cmd/flotilla/watch.go:58` already reads `--backlog-file` /
`FLOTILLA_BACKLOG_FILE`. This is purely the installer/template/`.env` plumbing + its regression test.

## Impact

- Affected specs: `watch` (ADD: deploy-surface enablement of the backlog gate).
- Affected code: `deploy/flotilla-watch-install.sh`, `deploy/flotilla-watch.service.in`,
  `deploy/flotilla-watch.env.example`, `cmd/flotilla/watch_install_test.go` (regression tests).
- Zero behavior change for existing installs (gate OFF when the key is absent/commented).
