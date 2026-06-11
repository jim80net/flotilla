#!/usr/bin/env bash
# flotilla-voice — systemd user service installer.
#
# GENERATES ~/.config/systemd/user/flotilla-voice.service from the repo template
# deploy/flotilla-voice.service.in + your host-path config deploy/flotilla-voice.env
# (copy it from deploy/flotilla-voice.env.example). This exists so the installed unit
# STOPS DRIFTING: the only host-specific surface is the .env; the unit is never
# hand-edited. Idempotent — safe to re-run (a no-op when nothing changed).
#
# Usage:
#   bash deploy/flotilla-voice-install.sh [ENV_FILE]            install (generate + daemon-reload)
#   bash deploy/flotilla-voice-install.sh --dry-run [ENV_FILE]  preview the diff; write/reload nothing
#   bash deploy/flotilla-voice-install.sh --print   [ENV_FILE]  print the generated unit to stdout
#
# ENV_FILE resolution: positional arg > $FLOTILLA_VOICE_ENV > deploy/flotilla-voice.env
#
# This installer GENERATES + daemon-reloads only. It does NOT restart a running
# flotilla-voice: voice is a live, metered audio surface, so applying a changed unit to the
# live process is the operator's call — it prints the restart command instead.
#
# REMINDER: the binary referenced by FLOTILLA_BIN must be built with `-tags voiceopus`
# (CGO + libopus-dev). The installer can't verify the build tag; a non-voice binary will
# start and immediately exit with a clear rebuild message (visible in journalctl).
set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DEPLOY_DIR="$REPO_DIR/deploy"
TEMPLATE="$DEPLOY_DIR/flotilla-voice.service.in"
# Overridable for tests; defaults to the systemd user unit path.
DEST="${FLOTILLA_VOICE_UNIT_DEST:-$HOME/.config/systemd/user/flotilla-voice.service}"

MODE=install
case "${1:-}" in
  --dry-run) MODE=dry-run; shift ;;
  --print)   MODE=print;   shift ;;
  -h|--help) grep '^#' "$0" | cut -c3- ; exit 0 ;;
esac

ENV_FILE="${1:-${FLOTILLA_VOICE_ENV:-$DEPLOY_DIR/flotilla-voice.env}}"

[[ -f "$TEMPLATE" ]] || { echo "error: template not found: $TEMPLATE" >&2; exit 1; }
if [[ ! -f "$ENV_FILE" ]]; then
  echo "error: host-path config not found: $ENV_FILE" >&2
  echo "       copy the example and edit the five paths for this host:" >&2
  echo "         cp $DEPLOY_DIR/flotilla-voice.env.example $DEPLOY_DIR/flotilla-voice.env" >&2
  exit 1
fi

# Load ONLY the five known keys (so a stray line can never inject shell). Pre-clear them so
# an inherited environment can't leak in.
FLOTILLA_WORKDIR='' FLOTILLA_BIN='' FLOTILLA_VOICE_CONFIG='' FLOTILLA_ROSTER='' FLOTILLA_SECRETS=''
while IFS= read -r line || [[ -n "$line" ]]; do
  line="${line%$'\r'}"
  [[ -z "$line" || "$line" == \#* ]] && continue
  key="${line%%=*}"; val="${line#*=}"
  key="${key//[$' \t']/}"
  # Trim surrounding whitespace from the value so a `KEY = value` habit does not leave a
  # leading space that yields an invalid `ExecStart= %h/...`. Values are taken literally
  # otherwise (no quote-stripping — see the .env.example header).
  val="${val#"${val%%[![:space:]]*}"}"
  val="${val%"${val##*[![:space:]]}"}"
  case "$key" in
    FLOTILLA_WORKDIR|FLOTILLA_BIN|FLOTILLA_VOICE_CONFIG|FLOTILLA_ROSTER|FLOTILLA_SECRETS)
      printf -v "$key" '%s' "$val" ;;
    *) echo "warning: ignoring unknown key in $ENV_FILE: $key" >&2 ;;
  esac
done < "$ENV_FILE"

missing=()
for v in FLOTILLA_WORKDIR FLOTILLA_BIN FLOTILLA_VOICE_CONFIG FLOTILLA_ROSTER FLOTILLA_SECRETS; do
  [[ -n "${!v}" ]] || missing+=("$v")
done
if (( ${#missing[@]} )); then
  echo "error: $ENV_FILE is missing required var(s): ${missing[*]}" >&2
  exit 1
fi

# A value must never itself contain a template token, or a later substitution pass would
# rewrite it (substitution is sequential).
for v in FLOTILLA_WORKDIR FLOTILLA_BIN FLOTILLA_VOICE_CONFIG FLOTILLA_ROSTER FLOTILLA_SECRETS; do
  if [[ "${!v}" == *@FLOTILLA_*@* ]]; then
    echo "error: $v contains a template placeholder token (@FLOTILLA_...@); refusing" >&2
    exit 1
  fi
done

# Generate via pure-bash placeholder substitution — NOT sed/envsubst: the ExecStartPre line
# contains $(seq 1 30)/$i and %h that those tools would mangle.
content="$(cat "$TEMPLATE")"
content="${content//@FLOTILLA_WORKDIR@/$FLOTILLA_WORKDIR}"
content="${content//@FLOTILLA_BIN@/$FLOTILLA_BIN}"
content="${content//@FLOTILLA_VOICE_CONFIG@/$FLOTILLA_VOICE_CONFIG}"
content="${content//@FLOTILLA_ROSTER@/$FLOTILLA_ROSTER}"
content="${content//@FLOTILLA_SECRETS@/$FLOTILLA_SECRETS}"

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
check_path "$FLOTILLA_VOICE_CONFIG" || { echo "error: voice config not found: $FLOTILLA_VOICE_CONFIG (copy deploy/voice.env.example, fill it, chmod 600)" >&2; exit 1; }
check_path "$FLOTILLA_ROSTER"       || { echo "error: roster not found: $FLOTILLA_ROSTER" >&2; exit 1; }
check_path "$FLOTILLA_SECRETS"      || { echo "error: secrets not found: $FLOTILLA_SECRETS" >&2; exit 1; }
check_path "$FLOTILLA_BIN"          || echo "warning: binary not found yet: $FLOTILLA_BIN (build it with -tags voiceopus before starting)" >&2

new_tmp="$(mktemp)"; trap 'rm -f "$new_tmp"' EXIT
printf '%s\n' "$content" > "$new_tmp"

if [[ -f "$DEST" ]] && diff -q "$DEST" "$new_tmp" >/dev/null 2>&1; then
  echo "flotilla-voice.service already up to date (no change): $DEST"
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
# daemon-reload needs an active user D-Bus session; surface a headless failure with the fix.
if ! systemctl --user daemon-reload; then
  echo "error: 'systemctl --user daemon-reload' failed — this needs an active user" >&2
  echo "       D-Bus session (XDG_RUNTIME_DIR / DBUS_SESSION_BUS_ADDRESS). On a" >&2
  echo "       headless host, enable lingering: loginctl enable-linger \"\$USER\"" >&2
  exit 1
fi
echo "Reloaded systemd user units."

# Voice is a live, metered audio surface — applying a changed unit to the live process is the
# operator's explicit call (don't auto-restart it from an installer run).
if systemctl --user is-active --quiet flotilla-voice.service; then
  echo ""
  echo "flotilla-voice is RUNNING — the new unit is loaded but NOT yet applied to the live"
  echo "process. Restart it yourself when ready (it is a live, metered audio surface):"
  echo "  systemctl --user restart flotilla-voice.service"
else
  echo ""
  echo "Next steps (voice is OPT-IN — start only when you want the audio surface live):"
  echo "  systemctl --user enable --now flotilla-voice.service   # start now + on login"
  echo "  journalctl --user -u flotilla-voice -f                 # follow logs"
fi
