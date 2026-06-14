#!/usr/bin/env bash
# flotilla-doctor — gateway-health escalator installer (systemd user timer + service).
#
# GENERATES ~/.config/systemd/user/flotilla-doctor.service from the repo template
# deploy/flotilla-doctor.service.in + your host-path config deploy/flotilla-doctor.env
# (copy it from deploy/flotilla-doctor.env.example), COPIES the escalator script
# deploy/flotilla-doctor.sh to FLOTILLA_DOCTOR_DEST, and COPIES the static timer
# deploy/flotilla-doctor.timer into the systemd user dir. This exists so the
# installed unit STOPS DRIFTING: the only host-specific surface is the .env; the unit
# is never hand-edited. Idempotent — safe to re-run (a no-op when nothing changed).
#
# Usage:
#   bash deploy/flotilla-doctor-install.sh [ENV_FILE]            install (generate + copy + daemon-reload)
#   bash deploy/flotilla-doctor-install.sh --dry-run [ENV_FILE]  preview the diff; write/reload nothing
#   bash deploy/flotilla-doctor-install.sh --print   [ENV_FILE]  print the generated unit to stdout
#
# ENV_FILE resolution: positional arg > $FLOTILLA_DOCTOR_ENV > deploy/flotilla-doctor.env
#
# Unlike the watch installer (which never auto-restarts the safety-critical clock),
# the doctor is NOT the clock — it is the external escalator. It is still NOT
# auto-enabled (match the watch installer's conservatism: print the enable command
# as a next step), but enabling its timer is safe.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DEPLOY_DIR="$REPO_DIR/deploy"
TEMPLATE="$DEPLOY_DIR/flotilla-doctor.service.in"
DOCTOR_SH_SRC="$DEPLOY_DIR/flotilla-doctor.sh"
TIMER_SRC="$DEPLOY_DIR/flotilla-doctor.timer"
# Overridable for tests; defaults to the systemd user unit paths.
DEST="${FLOTILLA_DOCTOR_UNIT_DEST:-$HOME/.config/systemd/user/flotilla-doctor.service}"
TIMER_DEST="${FLOTILLA_DOCTOR_TIMER_DEST:-$HOME/.config/systemd/user/flotilla-doctor.timer}"

MODE=install
case "${1:-}" in
  --dry-run) MODE=dry-run; shift ;;
  --print)   MODE=print;   shift ;;
  -h|--help) grep '^#' "$0" | cut -c3- ; exit 0 ;;
esac

ENV_FILE="${1:-${FLOTILLA_DOCTOR_ENV:-$DEPLOY_DIR/flotilla-doctor.env}}"

[[ -f "$TEMPLATE" ]]      || { echo "error: template not found: $TEMPLATE" >&2; exit 1; }
[[ -f "$DOCTOR_SH_SRC" ]] || { echo "error: escalator script not found: $DOCTOR_SH_SRC" >&2; exit 1; }
[[ -f "$TIMER_SRC" ]]     || { echo "error: timer not found: $TIMER_SRC" >&2; exit 1; }
if [[ ! -f "$ENV_FILE" ]]; then
  echo "error: host-path config not found: $ENV_FILE" >&2
  echo "       copy the example and edit the paths for this host:" >&2
  echo "         cp $DEPLOY_DIR/flotilla-doctor.env.example $DEPLOY_DIR/flotilla-doctor.env" >&2
  exit 1
fi

# Load ONLY the known keys (so a stray line can never inject shell). Pre-clear them so
# an inherited environment can't leak in. The first 8 are REQUIRED placeholders; the
# last 3 are OPTIONAL tunables baked into the .env (the doctor also has flag/env
# defaults, so they are not template placeholders and not required here).
FLOTILLA_DOCTOR_DEST='' FLOTILLA_SELF='' FLOTILLA_SECRETS='' FLOTILLA_WORKDIR=''
FLOTILLA_BIN='' FLOTILLA_CLAUDE_BIN='' FLOTILLA_RECOVER_SKILL='' FLOTILLA_DOCTOR_STATE_DIR=''
FLOTILLA_DOCTOR_THRESHOLD='' FLOTILLA_DOCTOR_COOLDOWN='' FLOTILLA_DOCTOR_RECHECK=''
while IFS= read -r line || [[ -n "$line" ]]; do
  line="${line%$'\r'}"
  [[ -z "$line" || "$line" == \#* ]] && continue
  key="${line%%=*}"; val="${line#*=}"
  key="${key//[$' \t']/}"
  # Trim surrounding whitespace from the value (a `KEY = value` habit) so it does not
  # leave a leading space in the generated ExecStart. Values are otherwise literal.
  val="${val#"${val%%[![:space:]]*}"}"
  val="${val%"${val##*[![:space:]]}"}"
  case "$key" in
    FLOTILLA_DOCTOR_DEST|FLOTILLA_SELF|FLOTILLA_SECRETS|FLOTILLA_WORKDIR|\
    FLOTILLA_BIN|FLOTILLA_CLAUDE_BIN|FLOTILLA_RECOVER_SKILL|FLOTILLA_DOCTOR_STATE_DIR|\
    FLOTILLA_DOCTOR_THRESHOLD|FLOTILLA_DOCTOR_COOLDOWN|FLOTILLA_DOCTOR_RECHECK)
      printf -v "$key" '%s' "$val" ;;
    *) echo "warning: ignoring unknown key in $ENV_FILE: $key" >&2 ;;
  esac
done < "$ENV_FILE"

# The 8 placeholder keys are required; the 3 tunables are optional.
REQUIRED_KEYS=(FLOTILLA_DOCTOR_DEST FLOTILLA_SELF FLOTILLA_SECRETS FLOTILLA_WORKDIR \
               FLOTILLA_BIN FLOTILLA_CLAUDE_BIN FLOTILLA_RECOVER_SKILL FLOTILLA_DOCTOR_STATE_DIR)
missing=()
for v in "${REQUIRED_KEYS[@]}"; do
  [[ -n "${!v}" ]] || missing+=("$v")
done
if (( ${#missing[@]} )); then
  echo "error: $ENV_FILE is missing required var(s): ${missing[*]}" >&2
  exit 1
fi

# A value must never itself contain a template token, or a later substitution pass
# would rewrite it (substitution is sequential).
for v in "${REQUIRED_KEYS[@]}"; do
  if [[ "${!v}" == *@FLOTILLA_*@* ]]; then
    echo "error: $v contains a template placeholder token (@FLOTILLA_...@); refusing" >&2
    exit 1
  fi
done

# Generate via pure-bash placeholder substitution — NOT sed/envsubst (keep parity with
# the watch/voice installers; values may contain %h which those tools would mangle).
content="$(cat "$TEMPLATE")"
content="${content//@FLOTILLA_DOCTOR_DEST@/$FLOTILLA_DOCTOR_DEST}"
content="${content//@FLOTILLA_SELF@/$FLOTILLA_SELF}"
content="${content//@FLOTILLA_SECRETS@/$FLOTILLA_SECRETS}"
content="${content//@FLOTILLA_WORKDIR@/$FLOTILLA_WORKDIR}"
content="${content//@FLOTILLA_BIN@/$FLOTILLA_BIN}"
content="${content//@FLOTILLA_CLAUDE_BIN@/$FLOTILLA_CLAUDE_BIN}"
content="${content//@FLOTILLA_RECOVER_SKILL@/$FLOTILLA_RECOVER_SKILL}"
content="${content//@FLOTILLA_DOCTOR_STATE_DIR@/$FLOTILLA_DOCTOR_STATE_DIR}"

# Fail loudly if any placeholder survived (a typo'd or newly-added template token).
if [[ "$content" == *@FLOTILLA_*@* ]]; then
  echo "error: unsubstituted placeholder(s) remain in the generated unit:" >&2
  printf '%s\n' "$content" | grep -o '@FLOTILLA_[A-Z_]*@' | sort -u >&2
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
    %h)   p="$HOME" ;;
    %h/*) p="$HOME/${p#%h/}" ;;
  esac
  [[ -e "$p" ]]
}
check_path "$FLOTILLA_SECRETS"    || { echo "error: secrets not found: $FLOTILLA_SECRETS" >&2; exit 1; }
check_path "$FLOTILLA_WORKDIR"    || { echo "error: workdir not found: $FLOTILLA_WORKDIR" >&2; exit 1; }
check_path "$FLOTILLA_CLAUDE_BIN" || echo "warning: claude binary not found yet: $FLOTILLA_CLAUDE_BIN (install it before the doctor can escalate)" >&2
check_path "$FLOTILLA_BIN"        || echo "warning: flotilla binary not found yet: $FLOTILLA_BIN (install it before starting)" >&2

new_tmp="$(mktemp)"; trap 'rm -f "$new_tmp"' EXIT
printf '%s\n' "$content" > "$new_tmp"

# Detect whether anything actually changes (unit, script, or timer) for the
# idempotent "no change" message.
unit_changed=1
[[ -f "$DEST" ]] && diff -q "$DEST" "$new_tmp" >/dev/null 2>&1 && unit_changed=0
script_changed=1
[[ -f "$FLOTILLA_DOCTOR_DEST" ]] && diff -q "$FLOTILLA_DOCTOR_DEST" "$DOCTOR_SH_SRC" >/dev/null 2>&1 && script_changed=0
timer_changed=1
[[ -f "$TIMER_DEST" ]] && diff -q "$TIMER_DEST" "$TIMER_SRC" >/dev/null 2>&1 && timer_changed=0

if (( unit_changed == 0 && script_changed == 0 && timer_changed == 0 )); then
  echo "flotilla-doctor already up to date (no change): $DEST"
  exit 0
fi

if [[ -f "$DEST" ]]; then
  if (( unit_changed )); then
    echo "Changes to $DEST:"
    diff -u "$DEST" "$new_tmp" || true
    echo ""
  fi
else
  echo "Installing new unit: $DEST"
fi
(( script_changed )) && echo "Escalator script will be (re)installed: $FLOTILLA_DOCTOR_DEST"
(( timer_changed ))  && echo "Timer will be (re)installed: $TIMER_DEST"

if [[ "$MODE" == dry-run ]]; then
  echo "(--dry-run: nothing written, nothing reloaded)"
  exit 0
fi

# Install the unit.
mkdir -p "$(dirname "$DEST")"
cp "$new_tmp" "$DEST"
echo "Installed: $DEST"

# Install the escalator script (chmod 0755).
mkdir -p "$(dirname "$FLOTILLA_DOCTOR_DEST")"
cp "$DOCTOR_SH_SRC" "$FLOTILLA_DOCTOR_DEST"
chmod 0755 "$FLOTILLA_DOCTOR_DEST"
echo "Installed escalator script: $FLOTILLA_DOCTOR_DEST"

# Install the timer.
mkdir -p "$(dirname "$TIMER_DEST")"
cp "$TIMER_SRC" "$TIMER_DEST"
echo "Installed timer: $TIMER_DEST"

# daemon-reload needs an active user D-Bus session; surface a headless failure clearly.
if ! systemctl --user daemon-reload; then
  echo "error: 'systemctl --user daemon-reload' failed — this needs an active user" >&2
  echo "       D-Bus session (XDG_RUNTIME_DIR / DBUS_SESSION_BUS_ADDRESS). On a" >&2
  echo "       headless host, enable lingering: loginctl enable-linger \"\$USER\"" >&2
  exit 1
fi
echo "Reloaded systemd user units."

# The doctor timer is NOT the safety-critical clock, so enabling it is safe — but
# match the watch installer's conservatism and PRINT the enable command rather than
# auto-enabling, so the operator stays in control of what runs on their host.
echo ""
echo "Next steps:"
echo "  systemctl --user enable --now flotilla-doctor.timer   # start the ~3-min health check"
echo "  systemctl --user list-timers flotilla-doctor.timer    # confirm the schedule"
echo "  journalctl --user -u flotilla-doctor -f               # follow escalation decisions"
