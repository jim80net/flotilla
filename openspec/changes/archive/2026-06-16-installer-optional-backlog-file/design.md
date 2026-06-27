## Context

The backlog gate (`Backlog-gated goal-driven continuation`, watch spec) is opt-in via the daemon
flag `--backlog-file` (`cmd/flotilla/watch.go:58`, default `os.Getenv("FLOTILLA_BACKLOG_FILE")`).
The anti-drift installer generates the systemd user unit from a template + a host-path `.env` so the
unit never drifts; it knew only the five required keys. Enabling the gate therefore required a
hand-written systemd drop-in — the exact drift the installer prevents. This change adds an optional
installer key so the gate is enabled the same anti-drift way.

## Goals / Non-Goals

- **Goal:** an optional `FLOTILLA_BACKLOG_FILE` key that, when set, appends ` --backlog-file <path>`
  to `ExecStart`; when unset, yields a byte-identical (functional) unit to today.
- **Non-Goal:** any Go daemon change (the flag + env default already exist); the `Environment=`
  variant (rejected — see Decisions); changing the backlog gate's runtime behavior.

## Decisions

### Decision: ExecStart-arg form, not `Environment=`
Every other config in the unit (roster, secrets, ack-file) is an `ExecStart` argument, so a reader
sees the flag explicitly in `systemctl cat` and there is ONE config mechanism. The `Environment=`
form was only used in the fleet's hand-made drop-in because the installer didn't support the flag yet.
- **Alternatives considered:** `Environment=FLOTILLA_BACKLOG_FILE=…` (the binary reads it) — rejected
  for splitting config across two mechanisms and hiding the gate from the ExecStart line.

### Decision: computed-fragment placeholder `@FLOTILLA_BACKLOG_ARG@`
A static template cannot conditionally omit text, and the installer's fail-loud guard rejects any
surviving `@FLOTILLA_*@`. So the placeholder is appended directly to `ExecStart` (no separating
space) and the installer ALWAYS substitutes it — to ` --backlog-file <path>` (leading space part of
the fragment) when set, or to `""` when unset. This preserves the pure-bash `${content//@TOK@/…}`
substitution style AND the fail-loud guard, and the unset path leaves no trailing space.
- **Alternatives considered:** a second template file for the backlog variant (rejected — doubles the
  anti-drift surface); post-processing the rendered text with `sed` (rejected — the template
  deliberately avoids `sed`/`envsubst` because the `ExecStartPre` line contains `$(seq 1 30)`/`%h`).

### Decision: pre-clear `FLOTILLA_BACKLOG_FILE` (the correctness hinge)
The live host EXPORTS `FLOTILLA_BACKLOG_FILE` (the binary reads it). Bash inherits exported env vars
as shell variables, so without pre-clearing it the installer's `[[ -n "$FLOTILLA_BACKLOG_FILE" ]]`
test would be true from the inherited value and inject `--backlog-file` even when the `.env` omits
the key — silently breaking the byte-identical-when-unset guarantee on exactly the host this
enables. The value must come from the `.env` only. A dedicated regression test
(`TestInstallerBacklogInheritedEnvNoLeak`) locks this.

### Decision: missing backlog file is a warning, not an error
The roster/secrets checks are errors because those are hard prerequisites. The backlog file is
written by the XO and may not exist at install time on a fresh host, and the gate is inert until it
exists — so a missing file is a non-fatal warning (mirroring the `FLOTILLA_BIN` warning).

## Risks / Trade-offs

- **Risk (FIXED — systems-review P2):** bash 5.2+ enables `patsub_replacement` by default, under
  which a literal `&` in a `${var//pat/repl}` REPLACEMENT expands to the matched text — so a path
  value containing `&` would corrupt the substitution into the placeholder token, surviving the
  fail-loud guard with a misleading "unsubstituted placeholder" error. This was pre-existing for all
  five required keys; this change would have extended it to the sixth. **Fix:** the installer now
  `shopt -u patsub_replacement` before substituting, so every value is substituted LITERALLY for ALL
  keys (degrades gracefully on bash <5.2 where the option is absent and `&` was already literal).
  Locked by `TestInstallerBacklogPathWithAmpersand`.
- **Risk (HARDENED — systems-review P3):** a future template comment containing the literal token
  pattern `@FLOTILLA_*@` would trip the fail-loud glob guard (`*@FLOTILLA_*@*`) but be invisible to
  the offender-printing grep (`@FLOTILLA_[A-Z_]*@`), yielding an empty/confusing error. **Mitigation:**
  the descriptive comment is worded to avoid the literal pattern, AND the offender-grep charset now
  includes `*` and `.` (`@FLOTILLA_[A-Z_.*]*@`) so the error can never be empty. The regression tests
  render the real template and would fail on any survivor.
- **Trade-off:** the byte-identity guarantee is asserted at the FUNCTIONAL-directive level (the
  `funcLineRe` regression), not raw-byte, because this change adds non-functional comment lines to
  the template. systemd acts only on the directives, so functional identity is the correct invariant.
- **Known limitation (pre-existing, NOT fixed here):** template values are not shell-quoted in the
  generated `ExecStart`, so a path containing spaces would be word-split by systemd into multiple
  arguments. This affects all six keys equally, is implausible for a real fleet path, and quoting it
  uniformly is out of scope for this change (it would restructure the template for all keys).

## Migration

After merge, the XO replaces the live fleet drop-in
(`~/.config/systemd/user/flotilla-watch.service.d/backlog.conf`) with the `.env` key + re-runs the
installer. That deploy touches the safety-critical heartbeat clock and is the XO's to execute.
