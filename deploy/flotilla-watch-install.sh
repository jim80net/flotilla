#!/usr/bin/env bash
# flotilla-watch — systemd user service installer.
#
# GENERATES ~/.config/systemd/user/flotilla-watch.service from the repo template
# deploy/flotilla-watch.service.in + your host-path config deploy/flotilla-watch.env
# (copy it from deploy/flotilla-watch.env.example). This exists so the installed unit
# STOPS DRIFTING: the only host-specific surface is the .env; the unit is never
# hand-edited. Idempotent — safe to re-run (a no-op when nothing changed).
#
# Usage:
#   bash deploy/flotilla-watch-install.sh [ENV_FILE]            install (generate + daemon-reload)
#   bash deploy/flotilla-watch-install.sh --dry-run [ENV_FILE]  preview the diff; write/reload nothing
#   bash deploy/flotilla-watch-install.sh --print   [ENV_FILE]  print the generated unit to stdout
#
# ENV_FILE resolution: positional arg > $FLOTILLA_WATCH_ENV > deploy/flotilla-watch.env
#
# This installer GENERATES + daemon-reloads only. It deliberately NEVER restarts a
# running flotilla-watch (the fleet's safety-critical heartbeat clock) — if the unit
# changed and the service is active, it prints the operator-controlled restart command.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DEPLOY_DIR="$REPO_DIR/deploy"
TEMPLATE="$DEPLOY_DIR/flotilla-watch.service.in"
# Overridable for tests; defaults to the systemd user unit path.
DEST="${FLOTILLA_WATCH_UNIT_DEST:-$HOME/.config/systemd/user/flotilla-watch.service}"

MODE=install
case "${1:-}" in
  --dry-run) MODE=dry-run; shift ;;
  --print)   MODE=print;   shift ;;
  -h|--help) grep '^#' "$0" | cut -c3- ; exit 0 ;;
esac

ENV_FILE="${1:-${FLOTILLA_WATCH_ENV:-$DEPLOY_DIR/flotilla-watch.env}}"

[[ -f "$TEMPLATE" ]] || { echo "error: template not found: $TEMPLATE" >&2; exit 1; }
if [[ ! -f "$ENV_FILE" ]]; then
  echo "error: host-path config not found: $ENV_FILE" >&2
  echo "       copy the example and edit the five paths for this host:" >&2
  echo "         cp $DEPLOY_DIR/flotilla-watch.env.example $DEPLOY_DIR/flotilla-watch.env" >&2
  exit 1
fi

# Load ONLY the known keys (so a stray line can never inject shell). Pre-clear them
# so an inherited environment can't leak in. FLOTILLA_BACKLOG_FILE is LOAD-BEARING
# here: the live host EXPORTS it (the binary reads it as a fallback default — see
# cmd/flotilla/watch.go), so without this pre-clear an inherited value would inject
# `--backlog-file` even when the .env omits the optional key, silently breaking the
# byte-identical-when-unset guarantee. The value must come from the .env ONLY.
FLOTILLA_WORKDIR='' FLOTILLA_BIN='' FLOTILLA_ROSTER='' FLOTILLA_SECRETS='' FLOTILLA_ACK_FILE='' FLOTILLA_BACKLOG_FILE='' FLOTILLA_WATCH_INTERVAL='' FLOTILLA_EVENT_POLL_INTERVAL=''
while IFS= read -r line || [[ -n "$line" ]]; do
  line="${line%$'\r'}"
  [[ -z "$line" || "$line" == \#* ]] && continue
  key="${line%%=*}"; val="${line#*=}"
  key="${key//[$' \t']/}"
  # Trim surrounding whitespace from the value so a `KEY = value` habit does not
  # leave a leading space that yields an invalid `ExecStart= %h/...`. Values are
  # taken literally otherwise (no quote-stripping — see the .env.example header).
  val="${val#"${val%%[![:space:]]*}"}"
  val="${val%"${val##*[![:space:]]}"}"
  case "$key" in
    FLOTILLA_WORKDIR|FLOTILLA_BIN|FLOTILLA_ROSTER|FLOTILLA_SECRETS|FLOTILLA_ACK_FILE|FLOTILLA_BACKLOG_FILE|FLOTILLA_WATCH_INTERVAL|FLOTILLA_EVENT_POLL_INTERVAL)
      printf -v "$key" '%s' "$val" ;;
    *) echo "warning: ignoring unknown key in $ENV_FILE: $key" >&2 ;;
  esac
done < "$ENV_FILE"

missing=()
for v in FLOTILLA_WORKDIR FLOTILLA_BIN FLOTILLA_ROSTER FLOTILLA_SECRETS FLOTILLA_ACK_FILE; do
  [[ -n "${!v}" ]] || missing+=("$v")
done
if (( ${#missing[@]} )); then
  echo "error: $ENV_FILE is missing required var(s): ${missing[*]}" >&2
  exit 1
fi

# A value must never itself contain a template token, or a later substitution pass
# would rewrite it (substitution is sequential). Implausible for a real path, but
# cheap to make the substitution provably safe.
for v in FLOTILLA_WORKDIR FLOTILLA_BIN FLOTILLA_ROSTER FLOTILLA_SECRETS FLOTILLA_ACK_FILE FLOTILLA_BACKLOG_FILE FLOTILLA_WATCH_INTERVAL FLOTILLA_EVENT_POLL_INTERVAL; do
  if [[ "${!v}" == *@FLOTILLA_*@* ]]; then
    echo "error: $v contains a template placeholder token (@FLOTILLA_...@); refusing" >&2
    exit 1
  fi
done

# Generate via pure-bash placeholder substitution — NOT sed/envsubst: the
# ExecStartPre line contains $(seq 1 30)/$i and %h that those tools would mangle.
#
# Disable patsub_replacement (bash 5.2+ default-ON): with it on, a literal `&` in a
# ${var//pat/repl} REPLACEMENT expands to the matched text — so a path value
# containing `&` would corrupt the substitution (the `&` becomes the placeholder
# token, surviving the fail-loud guard with a misleading error). We want every value
# substituted LITERALLY. Unsetting it makes `&` literal uniformly for ALL keys, and
# degrades gracefully on bash <5.2 (where the option does not exist and `&` was
# already literal). No substitution here uses `&`-as-matched-text.
shopt -u patsub_replacement 2>/dev/null || true
content="$(cat "$TEMPLATE")"
content="${content//@FLOTILLA_WORKDIR@/$FLOTILLA_WORKDIR}"
content="${content//@FLOTILLA_BIN@/$FLOTILLA_BIN}"
content="${content//@FLOTILLA_ROSTER@/$FLOTILLA_ROSTER}"
content="${content//@FLOTILLA_SECRETS@/$FLOTILLA_SECRETS}"
content="${content//@FLOTILLA_ACK_FILE@/$FLOTILLA_ACK_FILE}"

# OPTIONAL backlog: compute the ExecStart fragment. SET ⇒ " --backlog-file <path>"
# (the leading space is part of the fragment, since the template appends it with no
# separator); UNSET ⇒ "" (byte-identical to a no-backlog unit). The placeholder is
# ALWAYS substituted — to the fragment or to empty — so the fail-loud guard below
# still holds and an unset backlog leaves no trailing space.
if [[ -n "$FLOTILLA_BACKLOG_FILE" ]]; then
  backlog_arg=" --backlog-file $FLOTILLA_BACKLOG_FILE"
else
  backlog_arg=""
fi
content="${content//@FLOTILLA_BACKLOG_ARG@/$backlog_arg}"

latency_args=""
if [[ -n "$FLOTILLA_WATCH_INTERVAL" ]]; then
  latency_args+=" --interval $FLOTILLA_WATCH_INTERVAL"
fi
if [[ -n "$FLOTILLA_EVENT_POLL_INTERVAL" ]]; then
  latency_args+=" --event-poll-interval $FLOTILLA_EVENT_POLL_INTERVAL"
fi
content="${content//@FLOTILLA_LATENCY_ARGS@/$latency_args}"

# Fail loudly if any placeholder survived (a typo'd or newly-added template token).
# The offender-grep charset includes * and . so it always prints SOMETHING that the
# `*@FLOTILLA_*@*` glob can match (a literal `@FLOTILLA_*@` in a template comment would
# trip the glob but be invisible to a [A-Z_]-only grep — an empty, confusing error).
if [[ "$content" == *@FLOTILLA_*@* ]]; then
  echo "error: unsubstituted placeholder(s) remain in the generated unit:" >&2
  printf '%s\n' "$content" | grep -o '@FLOTILLA_[A-Z_.*]*@' | sort -u >&2
  exit 1
fi

if [[ "$MODE" == print ]]; then
  printf '%s\n' "$content"
  exit 0
fi

# Path sanity (install + dry-run only). Expand a leading %h -> $HOME for the check;
# substitution keeps %h literal so systemd still resolves it at runtime.
check_path() {
  local p="$1"
  case "$p" in
    %h)   p="$HOME" ;;       # exact specifier
    %h/*) p="$HOME/${p#%h/}" ;;
  esac
  [[ -e "$p" ]]
}
check_path "$FLOTILLA_ROSTER"  || { echo "error: roster not found: $FLOTILLA_ROSTER" >&2; exit 1; }
check_path "$FLOTILLA_SECRETS" || { echo "error: secrets not found: $FLOTILLA_SECRETS" >&2; exit 1; }
check_path "$FLOTILLA_BIN"     || echo "warning: binary not found yet: $FLOTILLA_BIN (install it before starting)" >&2
# Backlog is OPTIONAL and the XO may not have created it yet on a fresh host, so a
# missing file is a warning (non-fatal), unlike the roster/secrets hard prerequisites.
[[ -z "$FLOTILLA_BACKLOG_FILE" ]] || check_path "$FLOTILLA_BACKLOG_FILE" || \
  echo "warning: backlog file not found yet: $FLOTILLA_BACKLOG_FILE (the XO creates it; the gate is inert until it exists)" >&2

new_tmp="$(mktemp)"; trap 'rm -f "$new_tmp"' EXIT
printf '%s\n' "$content" > "$new_tmp"

if [[ -f "$DEST" ]] && diff -q "$DEST" "$new_tmp" >/dev/null 2>&1; then
  echo "flotilla-watch.service already up to date (no change): $DEST"
  exit 0
fi

if [[ -f "$DEST" ]]; then
  echo "Changes to $DEST:"
  diff -u "$DEST" "$new_tmp" || true
  echo ""
else
  echo "Installing new unit: $DEST"
fi

if [[ "$MODE" == dry-run ]]; then
  echo "(--dry-run: nothing written, nothing reloaded)"
  exit 0
fi

mkdir -p "$(dirname "$DEST")"
cp "$new_tmp" "$DEST"
echo "Installed: $DEST"
# daemon-reload needs an active user D-Bus session; on a headless host (no login
# session) it fails with "Failed to connect to bus". Surface that clearly with the
# fix rather than aborting on systemd's bare error.
if ! systemctl --user daemon-reload; then
  echo "error: 'systemctl --user daemon-reload' failed — this needs an active user" >&2
  echo "       D-Bus session (XDG_RUNTIME_DIR / DBUS_SESSION_BUS_ADDRESS). On a" >&2
  echo "       headless host, enable lingering: loginctl enable-linger \"\$USER\"" >&2
  exit 1
fi
echo "Reloaded systemd user units."

# NEVER auto-restart the safety-critical clock. A reloaded unit takes effect on the
# live process only at the operator's explicit restart.
if systemctl --user is-active --quiet flotilla-watch.service; then
  echo ""
  echo "flotilla-watch is RUNNING — the new unit is loaded but NOT yet applied to the"
  echo "live process. Restart it yourself when ready (it is the fleet's heartbeat clock):"
  echo "  systemctl --user restart flotilla-watch.service"
else
  echo ""
  echo "Next steps:"
  echo "  systemctl --user enable --now flotilla-watch.service   # start now + on login"
  echo "  journalctl --user -u flotilla-watch -f                 # follow logs"
fi
