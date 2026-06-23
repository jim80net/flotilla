#!/usr/bin/env bash
# flotilla-dash — systemd user service installer.
#
# GENERATES ~/.config/systemd/user/flotilla-dash.service from the repo template
# deploy/flotilla-dash.service.in + your host-path config deploy/flotilla-dash.env
# (copy it from deploy/flotilla-dash.env.example). This exists so the installed unit
# STOPS DRIFTING: the only host-specific surface is the .env; the unit is never
# hand-edited. Idempotent — safe to re-run (a no-op when nothing changed).
#
# Usage:
#   bash deploy/flotilla-dash-install.sh [ENV_FILE]            install (generate + daemon-reload)
#   bash deploy/flotilla-dash-install.sh --dry-run [ENV_FILE]  preview the diff; write/reload nothing
#   bash deploy/flotilla-dash-install.sh --print   [ENV_FILE]  print the generated unit to stdout
#
# ENV_FILE resolution: positional arg > $FLOTILLA_DASH_ENV > deploy/flotilla-dash.env
#
# This installer GENERATES + daemon-reloads only. It deliberately NEVER enables/starts
# or restarts the dash — it PRINTS the enable/start (or restart) command as a next step
# for the operator/XO to run, mirroring the watch installer's no-auto-start discipline.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DEPLOY_DIR="$REPO_DIR/deploy"
TEMPLATE="$DEPLOY_DIR/flotilla-dash.service.in"
# Overridable for tests; defaults to the systemd user unit path.
DEST="${FLOTILLA_DASH_UNIT_DEST:-$HOME/.config/systemd/user/flotilla-dash.service}"

MODE=install
case "${1:-}" in
  --dry-run) MODE=dry-run; shift ;;
  --print)   MODE=print;   shift ;;
  -h|--help) grep '^#' "$0" | cut -c3- ; exit 0 ;;
esac

ENV_FILE="${1:-${FLOTILLA_DASH_ENV:-$DEPLOY_DIR/flotilla-dash.env}}"

[[ -f "$TEMPLATE" ]] || { echo "error: template not found: $TEMPLATE" >&2; exit 1; }
if [[ ! -f "$ENV_FILE" ]]; then
  echo "error: host-path config not found: $ENV_FILE" >&2
  echo "       copy the example and edit the paths for this host:" >&2
  echo "         cp $DEPLOY_DIR/flotilla-dash.env.example $DEPLOY_DIR/flotilla-dash.env" >&2
  exit 1
fi

# Load ONLY the known keys (so a stray line can never inject shell). Pre-clear them so
# an inherited shell environment can't leak into THIS GENERATE step. The repo case is
# load-bearing: the installer's key (FLOTILLA_DASH_REPO) is the SAME name the binary
# reads as a fallback (cmd/flotilla/dash.go), so the live host may export it — without
# the pre-clear an inherited value would inject `--repo` even when the .env omits it.
# The secrets case differs by name: the installer's key is FLOTILLA_DASH_SECRETS but the
# binary's fallback is FLOTILLA_SECRETS — the installer never reads FLOTILLA_SECRETS, so
# at generate time `--secrets` is driven by the .env key ONLY (the pre-clear of
# FLOTILLA_DASH_SECRETS guards the symmetric case). The RUNTIME path (the binary
# inheriting an ambient FLOTILLA_SECRETS/FLOTILLA_DASH_REPO when the flag is absent) is
# closed separately by the unit's UnsetEnvironment= (see flotilla-dash.service.in).
FLOTILLA_DASH_WORKDIR='' FLOTILLA_DASH_BIN='' FLOTILLA_DASH_ROSTER='' FLOTILLA_DASH_BIND='' FLOTILLA_DASH_REPO='' FLOTILLA_DASH_SECRETS=''
while IFS= read -r line || [[ -n "$line" ]]; do
  line="${line%$'\r'}"
  [[ -z "$line" || "$line" == \#* ]] && continue
  key="${line%%=*}"; val="${line#*=}"
  key="${key//[$' \t']/}"
  # Trim surrounding whitespace from the value so a `KEY = value` habit does not leave
  # a leading space that yields an invalid `ExecStart= %h/...`. Values are taken
  # literally otherwise (no quote-stripping — see the .env.example header).
  val="${val#"${val%%[![:space:]]*}"}"
  val="${val%"${val##*[![:space:]]}"}"
  case "$key" in
    FLOTILLA_DASH_WORKDIR|FLOTILLA_DASH_BIN|FLOTILLA_DASH_ROSTER|FLOTILLA_DASH_BIND|FLOTILLA_DASH_REPO|FLOTILLA_DASH_SECRETS)
      printf -v "$key" '%s' "$val" ;;
    *) echo "warning: ignoring unknown key in $ENV_FILE: $key" >&2 ;;
  esac
done < "$ENV_FILE"

missing=()
for v in FLOTILLA_DASH_WORKDIR FLOTILLA_DASH_BIN FLOTILLA_DASH_ROSTER FLOTILLA_DASH_BIND; do
  [[ -n "${!v}" ]] || missing+=("$v")
done
if (( ${#missing[@]} )); then
  echo "error: $ENV_FILE is missing required var(s): ${missing[*]}" >&2
  exit 1
fi

# A value must never itself contain a template token, or a later substitution pass
# would rewrite it (substitution is sequential). Implausible for a real path, but
# cheap to make the substitution provably safe.
for v in FLOTILLA_DASH_WORKDIR FLOTILLA_DASH_BIN FLOTILLA_DASH_ROSTER FLOTILLA_DASH_BIND FLOTILLA_DASH_REPO FLOTILLA_DASH_SECRETS; do
  if [[ "${!v}" == *@FLOTILLA_*@* ]]; then
    echo "error: $v contains a template placeholder token (@FLOTILLA_...@); refusing" >&2
    exit 1
  fi
done

# Generate via pure-bash placeholder substitution — NOT sed/envsubst: the values may
# contain %h and the template's PATH line that those tools would mangle.
#
# Disable patsub_replacement (bash 5.2+ default-ON): with it on, a literal `&` in a
# ${var//pat/repl} REPLACEMENT expands to the matched text, corrupting a value
# containing `&`. We want every value substituted LITERALLY; unsetting it makes `&`
# literal uniformly and degrades gracefully on bash <5.2.
shopt -u patsub_replacement 2>/dev/null || true
content="$(cat "$TEMPLATE")"
content="${content//@FLOTILLA_DASH_WORKDIR@/$FLOTILLA_DASH_WORKDIR}"
content="${content//@FLOTILLA_DASH_BIN@/$FLOTILLA_DASH_BIN}"
content="${content//@FLOTILLA_DASH_ROSTER@/$FLOTILLA_DASH_ROSTER}"
content="${content//@FLOTILLA_DASH_BIND@/$FLOTILLA_DASH_BIND}"

# OPTIONAL --repo / --secrets: compute each ExecStart fragment. SET ⇒ " --flag <val>"
# (the leading space is part of the fragment, since the template appends with no
# separator); UNSET ⇒ "" (byte-identical to an omitted option). Both placeholders are
# ALWAYS substituted — to the fragment or to empty — so the fail-loud guard below still
# holds and an unset option leaves no trailing space.
if [[ -n "$FLOTILLA_DASH_REPO" ]]; then repo_arg=" --repo $FLOTILLA_DASH_REPO"; else repo_arg=""; fi
if [[ -n "$FLOTILLA_DASH_SECRETS" ]]; then secrets_arg=" --secrets $FLOTILLA_DASH_SECRETS"; else secrets_arg=""; fi
content="${content//@FLOTILLA_DASH_REPO_ARG@/$repo_arg}"
content="${content//@FLOTILLA_DASH_SECRETS_ARG@/$secrets_arg}"

# Fail loudly if any placeholder survived (a typo'd or newly-added template token).
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
    %h)   p="$HOME" ;;
    %h/*) p="$HOME/${p#%h/}" ;;
  esac
  [[ -e "$p" ]]
}
check_path "$FLOTILLA_DASH_ROSTER" || { echo "error: roster not found: $FLOTILLA_DASH_ROSTER" >&2; exit 1; }
check_path "$FLOTILLA_DASH_BIN"    || echo "warning: binary not found yet: $FLOTILLA_DASH_BIN (install it before starting)" >&2
# Secrets is OPTIONAL (notify is disabled without it), so a missing/unset file is a
# warning, not a hard prerequisite like the roster.
[[ -z "$FLOTILLA_DASH_SECRETS" ]] || check_path "$FLOTILLA_DASH_SECRETS" || \
  echo "warning: secrets file not found yet: $FLOTILLA_DASH_SECRETS (the operator-note action is inert until it exists)" >&2

new_tmp="$(mktemp)"; trap 'rm -f "$new_tmp"' EXIT
printf '%s\n' "$content" > "$new_tmp"

if [[ -f "$DEST" ]] && diff -q "$DEST" "$new_tmp" >/dev/null 2>&1; then
  echo "flotilla-dash.service already up to date (no change): $DEST"
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
# session) it fails with "Failed to connect to bus". Surface that clearly with the fix
# rather than aborting on systemd's bare error.
if ! systemctl --user daemon-reload; then
  echo "error: 'systemctl --user daemon-reload' failed — this needs an active user" >&2
  echo "       D-Bus session (XDG_RUNTIME_DIR / DBUS_SESSION_BUS_ADDRESS). On a" >&2
  echo "       headless host, enable lingering: loginctl enable-linger \"\$USER\"" >&2
  exit 1
fi
echo "Reloaded systemd user units."

# NEVER auto-start/enable — the operator/XO owns that (mirrors the watch installer).
# If a flotilla-dash unit is already active (e.g. the transient `systemd-run` unit
# stood up during the live deploy), the reloaded persistent unit is loaded but NOT yet
# applied to the live process — print the restart-to-apply command.
if systemctl --user is-active --quiet flotilla-dash.service; then
  echo ""
  echo "flotilla-dash is already RUNNING (e.g. the transient deploy unit). The persistent"
  echo "unit is loaded but NOT yet applied to the live process. Apply it when ready:"
  echo "  systemctl --user enable --now flotilla-dash.service   # persist across reboot + restart now"
else
  echo ""
  echo "Next steps (operator/XO runs these — the installer does not auto-start):"
  echo "  systemctl --user enable --now flotilla-dash.service   # start now + on boot"
  echo "  journalctl --user -u flotilla-dash -f                 # follow logs"
fi
# WantedBy=default.target only auto-starts at boot when user lingering is enabled
# (otherwise a --user manager runs only during a login session). Surface it as a
# first-class durability prerequisite, not just a daemon-reload-failure footnote.
if command -v loginctl >/dev/null 2>&1 && [[ "$(loginctl show-user "$USER" -p Linger --value 2>/dev/null)" != "yes" ]]; then
  echo ""
  echo "note: user lingering is OFF — the dash will NOT auto-start after a reboot until you run:"
  echo "  loginctl enable-linger \"$USER\""
fi
