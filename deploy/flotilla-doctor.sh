#!/usr/bin/env bash
# flotilla-doctor — gateway-health ESCALATOR for the flotilla-watch daemon.
#
# ============================================================================
#  SAFETY INVARIANT — READ THIS FIRST
# ----------------------------------------------------------------------------
#  This doctor NEVER restarts flotilla-watch. It does not run
#  `systemctl restart flotilla-watch`, `kill`, or any process control on the
#  safety-critical heartbeat clock. Its ONLY action on a sustained gateway-down
#  is to ESCALATE: (a) a best-effort operator notify, and (b) a headless
#  `claude -p "/recover-flotilla …"` recovery agent that DIAGNOSES the real
#  cause (DNS first — the 2026-06-12 9-hour outage was DNS, not flotilla) and
#  applies the *right* fix. Whether a restart is warranted is the recovery
#  skill's decision after diagnosis — a blind restart fixes nothing when the
#  cause is a dead resolver, and it would violate the "only the operator
#  restarts the safety clock" doctrine.
#  Rationale: flotilla openspec change `watch-gateway-doctor` (escalation, not
#  restart) and the `recover-flotilla` skill (the diagnosis runbook).
# ============================================================================
#
# WHAT IT DETECTS
#   flotilla-watch's relay-open failure is non-fatal by design: on a
#   gateway/DNS failure the daemon degrades to clock-only and retries the
#   gateway in the background, so systemd shows the process `active` while the
#   Discord connection is dead. systemd's Restart=on-failure never fires
#   because nothing crashed. This doctor catches that silent "alive but
#   disconnected" state from OUTSIDE the daemon.
#
# HOW IT RUNS
#   A Type=oneshot service fired by a ~3-minute timer. Each run is a cheap,
#   pure-bash health check (NO LLM in the cheap path — the LLM only fires on a
#   confirmed-sustained escalation). Strikes accumulate across ticks; with a
#   3-minute cadence and a 3-strike threshold that is ~9 minutes confirmed-down
#   before any escalation — and a cooldown then prevents re-spawning the agent
#   every tick.
#
# USAGE
#   flotilla-doctor.sh --self NAME --secrets PATH --workdir DIR --bin PATH \
#       --claude PATH --skill NAME --state-dir DIR \
#       [--threshold N] [--cooldown S] [--recheck S]
set -euo pipefail

# ---- defaults (every tunable is a flag/env with a sane default) -------------
SELF=""
SECRETS=""
WORKDIR=""
BIN=""
CLAUDE=""
SKILL="recover-flotilla"
STATE_DIR=""
THRESHOLD="${FLOTILLA_DOCTOR_THRESHOLD:-3}"
COOLDOWN_SECONDS="${FLOTILLA_DOCTOR_COOLDOWN:-1800}"
RECHECK_SECONDS="${FLOTILLA_DOCTOR_RECHECK:-15}"
CLAUDE_TIMEOUT="${FLOTILLA_DOCTOR_CLAUDE_TIMEOUT:-600}"

WATCH_UNIT="flotilla-watch.service"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --self)      SELF="$2";              shift 2 ;;
    --secrets)   SECRETS="$2";           shift 2 ;;
    --workdir)   WORKDIR="$2";           shift 2 ;;
    --bin)       BIN="$2";               shift 2 ;;
    --claude)    CLAUDE="$2";            shift 2 ;;
    --skill)     SKILL="$2";             shift 2 ;;
    --state-dir) STATE_DIR="$2";         shift 2 ;;
    --threshold) THRESHOLD="$2";         shift 2 ;;
    --cooldown)  COOLDOWN_SECONDS="$2";  shift 2 ;;
    --recheck)   RECHECK_SECONDS="$2";   shift 2 ;;
    -h|--help)   grep '^#' "$0" | cut -c3-; exit 0 ;;
    *) echo "flotilla-doctor: unknown argument: $1" >&2; exit 2 ;;
  esac
done

for req in SELF SECRETS WORKDIR BIN CLAUDE STATE_DIR; do
  if [[ -z "${!req}" ]]; then
    flag="${req,,}"        # SELF -> self, STATE_DIR -> state_dir
    flag="--${flag//_/-}"  # state_dir -> state-dir
    echo "flotilla-doctor: missing required ${flag} (or its value)" >&2
    exit 2
  fi
done

mkdir -p "$STATE_DIR" 2>/dev/null || true

LOCK_FILE="$STATE_DIR/flotilla-doctor.lock"
STRIKE_FILE="$STATE_DIR/flotilla-doctor-strikes"
ESCALATED_AT_FILE="$STATE_DIR/flotilla-doctor-escalated-at"
DOCTOR_LOG="$STATE_DIR/flotilla-doctor.log"

now_epoch() { date +%s; }

log() {
  # Timestamped line to both the doctor log and stderr (journal captures stderr).
  local msg="[$(date -u +%Y-%m-%dT%H:%M:%SZ)] $*"
  printf '%s\n' "$msg" >>"$DOCTOR_LOG" 2>/dev/null || true
  printf '%s\n' "$msg" >&2
}

# ---- single-flight: never overlap doctor runs -------------------------------
# Open fd 9 on the lock file, then flock it; if another run holds it, exit 0.
# NB: keep the 2>/dev/null SCOPED to this single redirect (a subshell) — a bare
# `exec 9>file 2>/dev/null` would permanently silence fd 2 for the whole script
# and swallow every log line. We only want to tolerate a failure to OPEN the fd.
lock_ok=0
if ( exec 9>"$LOCK_FILE" ) 2>/dev/null; then
  exec 9>"$LOCK_FILE" && lock_ok=1
fi
if (( lock_ok )) && command -v flock >/dev/null 2>&1; then
  if ! flock -n 9; then
    log "another flotilla-doctor run holds the lock — exiting"
    exit 0
  fi
fi

# ---- the cheap health check (pure bash, no LLM) -----------------------------
# gateway_healthy: prints nothing, returns:
#   0  -> healthy (watch active, MainPID resolves, >=1 ESTABLISHED :443 from PID)
#   1  -> unhealthy (socket genuinely absent / unit inactive / no MainPID)
#   2  -> indeterminate (ss errored) — do NOT treat as unhealthy/escalate
gateway_healthy() {
  if ! systemctl --user is-active --quiet "$WATCH_UNIT"; then
    return 1   # not active -> not a gateway-health problem this doctor escalates
                # (a crashed/inactive daemon is systemd's Restart job, not ours)
  fi

  local pid
  pid="$(systemctl --user show -p MainPID --value "$WATCH_UNIT" 2>/dev/null || echo 0)"
  [[ "$pid" =~ ^[0-9]+$ ]] || pid=0
  if [[ "$pid" -eq 0 ]]; then
    return 1   # active but no MainPID -> degenerate; treat as unhealthy
  fi

  # flotilla only talks to Discord, so ANY ESTABLISHED :443 socket owned by the
  # watch PID == gateway up. `ss -tnpH` is no-header, numeric, with process info.
  local ss_out ss_rc
  ss_out="$(ss -tnpH 2>/dev/null)"
  ss_rc=$?
  if [[ "$ss_rc" -ne 0 ]]; then
    # ss itself failed (not "no sockets"). Be conservative: an ss failure alone
    # must NOT escalate. Report indeterminate so the caller does not strike.
    return 2
  fi

  # Match a line owned by our PID that has an ESTABLISHED :443 endpoint. ss puts
  # the process as `users:(("flotilla",pid=1234,fd=7))`. State column is "ESTAB".
  if printf '%s\n' "$ss_out" \
       | grep -F "pid=${pid}," \
       | grep -qE '(:443[[:space:]]|:443$)'; then
    return 0   # healthy
  fi
  return 1     # socket genuinely absent -> unhealthy
}

read_strikes() {
  local n
  n="$(cat "$STRIKE_FILE" 2>/dev/null || echo 0)"
  [[ "$n" =~ ^[0-9]+$ ]] || n=0
  printf '%s' "$n"
}

clear_strikes() { rm -f "$STRIKE_FILE" 2>/dev/null || true; }

# ---- main flow --------------------------------------------------------------
# `rc=0; gateway_healthy || rc=$?` (NOT `gateway_healthy; rc=$?`): under
# `set -e` a bare non-zero return from the function would abort the script
# before `rc=$?` runs. The `|| rc=$?` form both captures the code (1/2) and
# tells `set -e` the non-zero was handled.
rc=0; gateway_healthy || rc=$?
if [[ "$rc" -eq 2 ]]; then
  log "ss unavailable/errored — treating as indeterminate, NOT escalating this tick"
  exit 0
fi
if [[ "$rc" -eq 0 ]]; then
  if [[ -f "$STRIKE_FILE" ]]; then
    log "gateway healthy — clearing strike file"
  fi
  clear_strikes
  exit 0
fi

# Unhealthy on the first look. Re-check ONCE after a short sleep to avoid
# catching a momentary reconnect between ticks.
log "gateway appears DOWN — re-checking once in ${RECHECK_SECONDS}s"
sleep "$RECHECK_SECONDS"
rc=0; gateway_healthy || rc=$?
if [[ "$rc" -eq 2 ]]; then
  log "ss indeterminate on recheck — NOT escalating this tick"
  exit 0
fi
if [[ "$rc" -eq 0 ]]; then
  log "gateway recovered on recheck — clearing strikes"
  clear_strikes
  exit 0
fi

# Still down after the recheck. Strike it.
strikes="$(read_strikes)"
strikes=$(( strikes + 1 ))
printf '%s' "$strikes" >"$STRIKE_FILE" 2>/dev/null || true
log "gateway still DOWN after recheck — strike ${strikes}/${THRESHOLD}"

if [[ "$strikes" -lt "$THRESHOLD" ]]; then
  log "below threshold (${strikes} < ${THRESHOLD}) — waiting for more confirmation"
  exit 0
fi

# ---- ESCALATION (threshold reached) -----------------------------------------
# Cooldown: do not re-spawn the recovery agent every tick while it works /
# while the operator is acting.
nowt="$(now_epoch)"
if [[ -f "$ESCALATED_AT_FILE" ]]; then
  last="$(cat "$ESCALATED_AT_FILE" 2>/dev/null || echo 0)"
  [[ "$last" =~ ^[0-9]+$ ]] || last=0
  age=$(( nowt - last ))
  if [[ "$age" -lt "$COOLDOWN_SECONDS" ]]; then
    log "in escalation cooldown (${age}s < ${COOLDOWN_SECONDS}s since last) — not re-spawning"
    exit 0
  fi
fi
printf '%s' "$nowt" >"$ESCALATED_AT_FILE" 2>/dev/null || true

log "ESCALATING — gateway confirmed down for >= ${THRESHOLD} strikes"

# Build a status payload the recovery skill can use directly. Every probe is
# best-effort: a degraded environment (DNS down, no dig) must not crash the
# script before it can escalate.
payload="$(mktemp 2>/dev/null || echo "$STATE_DIR/flotilla-doctor-payload.$$")"
cleanup_payload() { rm -f "$payload" 2>/dev/null || true; }
trap cleanup_payload EXIT

# is-active prints the state ("active"/"inactive"/"failed") to stdout AND returns
# non-zero for any non-active state, so do NOT `|| echo unknown` (that would append
# a second line). Fall back to "unknown" only when stdout is genuinely empty.
watch_active="$(systemctl --user is-active "$WATCH_UNIT" 2>/dev/null)" || true
[[ -n "$watch_active" ]] || watch_active="unknown"
main_pid="$(systemctl --user show -p MainPID --value "$WATCH_UNIT" 2>/dev/null || echo 0)"
[[ "$main_pid" =~ ^[0-9]+$ ]] || main_pid=0

# :443 socket dump for the watch PID (the core evidence — empty == gateway down).
sock_dump="$(ss -tnpH 2>/dev/null | grep -F "pid=${main_pid}," | grep ':443' || true)"
[[ -n "$sock_dump" ]] || sock_dump="(no established :443 socket owned by pid=${main_pid})"

# Recent journal tail.
journal_tail="$(journalctl --user -u flotilla-watch --since "30 min ago" --no-pager 2>/dev/null | tail -25 || true)"
[[ -n "$journal_tail" ]] || journal_tail="(journal unavailable)"

# Ack-file age — we don't know the ack path here, so report the freshest mtime of
# any flotilla-xo-alive-style ack file under the state dir, if present.
ack_age="(ack file not located under state dir)"
ack_file="$(ls -1 "$STATE_DIR"/flotilla*xo-alive "$STATE_DIR"/*xo-alive 2>/dev/null | head -1 || true)"
if [[ -n "$ack_file" && -e "$ack_file" ]]; then
  ack_mtime="$(stat -c %Y "$ack_file" 2>/dev/null || echo 0)"
  [[ "$ack_mtime" =~ ^[0-9]+$ ]] || ack_mtime=0
  ack_age="$(( nowt - ack_mtime ))s old ($ack_file)"
fi

# Per-resolver DNS probe (the #1 root cause). getent always; dig per-resolver if present.
dns_probe="getent hosts discord.com: $(getent hosts discord.com 2>/dev/null | head -1 || echo 'FAILED')"
if command -v dig >/dev/null 2>&1; then
  for r in 75.75.75.75 1.1.1.1 100.100.100.100; do
    ans="$(timeout 3 dig +tries=1 +time=2 @"$r" discord.com +short 2>/dev/null | head -1 || true)"
    [[ -n "$ans" ]] || ans="TIMEOUT"
    dns_probe="${dns_probe}"$'\n'"dig @${r} discord.com: ${ans}"
  done
else
  dns_probe="${dns_probe}"$'\n'"(dig not installed — per-resolver probe skipped)"
fi

{
  echo "flotilla-doctor escalation — gateway confirmed DOWN (>= ${THRESHOLD} strikes, ~$(( THRESHOLD * 3 ))min confirmed)"
  echo "as_of: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo ""
  echo "flotilla-watch.service active: ${watch_active}"
  echo "MainPID: ${main_pid}"
  echo "ack-file age: ${ack_age}"
  echo ""
  echo "ESTABLISHED :443 sockets owned by watch PID:"
  echo "${sock_dump}"
  echo ""
  echo "per-resolver DNS probe (discord.com):"
  echo "${dns_probe}"
  echo ""
  echo "journalctl --user -u flotilla-watch (last 25 lines, 30 min):"
  echo "${journal_tail}"
} >"$payload" 2>/dev/null || true

# (a) Best-effort operator notify. During a DNS outage this itself cannot reach
# Discord — wrap so a failure is logged, not fatal.
if "$BIN" notify --from "$SELF" --secrets "$SECRETS" --file "$payload" >>"$DOCTOR_LOG" 2>&1; then
  log "operator notify sent"
else
  log "operator notify FAILED (likely the same DNS outage that downed the gateway) — continuing to recovery agent"
fi

# (b) Fire the intelligent recovery agent, time-bounded and non-fatal.
# NOTE: we deliberately do NOT pass --dangerously-skip-permissions. The headless
# agent runs under the user's gatekeeper allowlist (fail-closed); a DNS edit or a
# restart it decides on goes through the normal permission surface.
summary="flotilla-watch active=${watch_active} pid=${main_pid}, NO established :443 socket — gateway confirmed down >= ${THRESHOLD} strikes. Status payload: ${payload}"
log "spawning recovery agent: claude --print \"/${SKILL} …\" (timeout ${CLAUDE_TIMEOUT}s, cwd ${WORKDIR})"
set +e
( cd "$WORKDIR" && timeout "$CLAUDE_TIMEOUT" "$CLAUDE" --print "/${SKILL} ${summary}" ) >>"$DOCTOR_LOG" 2>&1
claude_rc=$?
set -e
log "recovery agent exited rc=${claude_rc} (124 == timed out at ${CLAUDE_TIMEOUT}s)"

# Do NOT reset the strike counter here — a later healthy tick clears it. The
# cooldown above is what prevents re-spawning every tick.
exit 0
