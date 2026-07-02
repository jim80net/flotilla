#!/usr/bin/env bash
# flotilla-doctrine-refresh — fleet-wide constitutional doctrine refresh.
#
# Runs `flotilla doctrine install --refresh --all` against the roster so updated
# embedded identity-append assets reach already-installed identity files (marker-
# detected skip alone strands stale blocks — see issue #252).
#
# This script NEVER restarts flotilla-watch or rebuilds the binary. The operator's
# deploy sequence after a doctrine-bearing merge is:
#   1. go install ./cmd/flotilla          (fresh binary with new embedded assets)
#   2. systemctl --user restart flotilla-watch.service   (when ready)
#   3. bash deploy/flotilla-doctrine-refresh.sh          (this script)
#
# Usage:
#   bash deploy/flotilla-doctrine-refresh.sh [ENV_FILE]
#
# ENV_FILE resolution: positional arg > $FLOTILLA_DOCTRINE_REFRESH_ENV > deploy/flotilla-watch.env
# (reuses FLOTILLA_BIN + FLOTILLA_ROSTER from the watch host-path config by default).
set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DEPLOY_DIR="$REPO_DIR/deploy"
ENV_FILE="${1:-${FLOTILLA_DOCTRINE_REFRESH_ENV:-$DEPLOY_DIR/flotilla-watch.env}}"

[[ -f "$ENV_FILE" ]] || {
  echo "error: host-path config not found: $ENV_FILE" >&2
  echo "       copy deploy/flotilla-watch.env.example → deploy/flotilla-watch.env" >&2
  exit 1
}

FLOTILLA_BIN='' FLOTILLA_ROSTER=''
while IFS= read -r line || [[ -n "$line" ]]; do
  line="${line%$'\r'}"
  [[ -z "$line" || "$line" == \#* ]] && continue
  key="${line%%=*}"; val="${line#*=}"
  key="${key//[$' \t']/}"
  val="${val#"${val%%[![:space:]]*}"}"
  val="${val%"${val##*[![:space:]]}"}"
  case "$key" in
    FLOTILLA_BIN|FLOTILLA_ROSTER) printf -v "$key" '%s' "$val" ;;
    *) ;;
  esac
done < "$ENV_FILE"

missing=()
for v in FLOTILLA_BIN FLOTILLA_ROSTER; do
  [[ -n "${!v}" ]] || missing+=("$v")
done
if (( ${#missing[@]} )); then
  echo "error: $ENV_FILE is missing required var(s): ${missing[*]}" >&2
  exit 1
fi

case "$FLOTILLA_BIN" in
  %h)   FLOTILLA_BIN="$HOME" ;;
  %h/*) FLOTILLA_BIN="$HOME/${FLOTILLA_BIN#%h/}" ;;
esac
case "$FLOTILLA_ROSTER" in
  %h)   FLOTILLA_ROSTER="$HOME" ;;
  %h/*) FLOTILLA_ROSTER="$HOME/${FLOTILLA_ROSTER#%h/}" ;;
esac

[[ -x "$FLOTILLA_BIN" ]] || {
  echo "error: flotilla binary not found or not executable: $FLOTILLA_BIN" >&2
  exit 1
}
[[ -f "$FLOTILLA_ROSTER" ]] || {
  echo "error: roster not found: $FLOTILLA_ROSTER" >&2
  exit 1
}

echo "Refreshing constitutional doctrine for all roster agents (--refresh --all)"
echo "  binary: $FLOTILLA_BIN"
echo "  roster: $FLOTILLA_ROSTER"
exec "$FLOTILLA_BIN" doctrine install --refresh --all --roster "$FLOTILLA_ROSTER"