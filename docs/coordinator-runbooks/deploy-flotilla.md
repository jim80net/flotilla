# Deploy flotilla — binary build and service restart

When a PR merges into this repository and touches running code, the merged commit
does **nothing** until you rebuild the binary and restart the services that run it.
Pair with [`watch-runbook.md`](../watch-runbook.md) and
[`dash-runbook.md`](../dash-runbook.md) for unit installation.

Substitute your host paths below (`$FLOTILLA_BIN`, `$FLOTILLA_SRC`, `$ROSTER_DIR`).

## Ground truth

| Piece | Typical path |
|---|---|
| Binary | `$HOME/go/bin/flotilla` |
| Source | your checkout of this repo |
| Dash service | `flotilla-dash.service` (user unit) |
| Watch service | `flotilla-watch.service` (user unit) |
| Working directory | roster-adjacent dir (units pass `--roster`, `--backlog`, …) |

## Deploy procedure

### 1. Build in a throwaway clone

Do not `git reset --hard` a live checkout desks may be using.

```bash
BUILD=/tmp/flotilla-build-$(date +%s)
git clone "$FLOTILLA_SRC" "$BUILD"
cd "$BUILD"
git fetch origin main && git reset --hard origin/main
git rev-parse HEAD    # record deployed SHA
```

### 2. Test with pipefail

```bash
set -o pipefail
go test ./... 2>&1 | tail -20
```

If red, STOP — do not deploy red main ([`incident-response.md`](./incident-response.md)).

### 3. Build to staging path

```bash
go build -o "$BUILD/flotilla-new" ./cmd/flotilla
```

### 4. Stage-swap with backup

```bash
cp "$FLOTILLA_BIN" "$FLOTILLA_BIN.bak-pre-$(git rev-parse --short HEAD)-$(date +%s)"
cp "$BUILD/flotilla-new" "$FLOTILLA_BIN-stage"
mv "$FLOTILLA_BIN-stage" "$FLOTILLA_BIN"
```

Rollback: restore the `.bak-pre-*` file and restart services.

### 5. Restart affected services

```bash
systemctl --user restart flotilla-dash.service
# Restart flotilla-watch ONLY when daemon code changed (relay, heartbeat,
# scheduler, surface drivers) — dash-only HTML/JS does not need a watch bounce.
systemctl --user restart flotilla-watch.service
```

Use `systemctl --user restart`, not kill+start chains — a stale PID on the port
once served old code while a new process "started."

### 6. Verify shape, not just "up"

```bash
systemctl --user is-active flotilla-dash.service flotilla-watch.service
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8787/
```

For visible changes, curl the page and grep for a string only the new code emits.

### 7. Ledger it

Append to fleet backlog: SHA deployed, services restarted, verify result, rollback path.

## Risky deploys — veto window

Routine reversible deploys ship without a nod. For high blast-radius changes (daemon
relay, scheduler semantics, no one-command rollback):

1. **Announce** via `flotilla notify`: what, blast radius, rollback, veto window open.
2. **~15-minute window** — operator may veto or approve early.
3. **Silence past window** — execute and report verified outcome.

Proceed-unless-vetoed, not wait-for-approval (Principle 3).

## Landing page (GitHub Pages)

```bash
gh workflow run "Deploy landing page (GitHub Pages)" -R <owner>/flotilla
```

Public surfaces: run `scripts/check-private-boundary.sh` before publishing
deployment-derived content.