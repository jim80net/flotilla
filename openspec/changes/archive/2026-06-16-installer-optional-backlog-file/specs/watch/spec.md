# watch Specification (delta)

## ADDED Requirements

### Requirement: Deploy-surface enablement of the backlog gate

The anti-drift installer SHALL support an OPTIONAL `FLOTILLA_BACKLOG_FILE` key that enables the
backlog gate. The installer (`deploy/flotilla-watch-install.sh`) generates the systemd user unit
from `deploy/flotilla-watch.service.in` + a host-path `.env`; the key enables the backlog gate (see
"Backlog-gated goal-driven continuation") by appending ` --backlog-file <path>` to the generated
`ExecStart`. The key SHALL be
OPTIONAL: the installer SHALL NOT add it to the required-key check, so an `.env` without it still
generates a valid unit. When the key is UNSET (absent or commented), the generated unit SHALL be
byte-identical (at the functional-directive level) to a unit generated before this key existed — the
gate is OFF and there SHALL be no `--backlog-file` argument and no trailing space. When the key is
SET, the generated `ExecStart` SHALL contain exactly one ` --backlog-file <path>` argument using the
value taken from the `.env` file ONLY (an inherited/exported `FLOTILLA_BACKLOG_FILE` from the
process environment SHALL NOT leak into the generated unit). A backlog file that does not yet exist
at install time SHALL produce a non-fatal warning, not an error (the XO creates the file; the gate
is inert until it exists), in contrast to the roster/secrets hard-prerequisite errors. The
configuration SHALL be expressed as an `ExecStart` argument (NOT a systemd `Environment=` directive),
consistent with the existing roster/secrets/ack-file arguments.

#### Scenario: Backlog key set adds the argument
- **WHEN** the `.env` sets `FLOTILLA_BACKLOG_FILE=/srv/fleet/backlog.md`
- **THEN** the generated `ExecStart` ends with ` --backlog-file /srv/fleet/backlog.md` (exactly one argument, single-spaced)

#### Scenario: Backlog key unset is byte-identical to a no-backlog install
- **WHEN** the `.env` omits `FLOTILLA_BACKLOG_FILE` (absent or commented out)
- **THEN** the generated `ExecStart` contains no `--backlog-file` argument and no trailing space — identical to the unit generated before the key existed

#### Scenario: An inherited environment value does not leak
- **WHEN** `FLOTILLA_BACKLOG_FILE` is exported in the installer's process environment but the `.env` omits it
- **THEN** the generated `ExecStart` contains no `--backlog-file` argument (the value is read from the `.env` only)

#### Scenario: Existing five-key installs are unaffected
- **WHEN** an existing `.env` with only the five required keys is re-run through the installer
- **THEN** generation succeeds and the unit is unchanged (the optional key's absence is not a missing-required error)
