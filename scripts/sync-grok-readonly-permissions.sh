#!/usr/bin/env bash
# Sync the canonical two-tier grok permission policy to:
#   1. Each grok desk worktree: <cwd>/.claude/settings.local.json
#   2. Each grok launch recipe in flotilla-launch.json (--allow/--deny CLI flags)
#
# Tier read_unprompted: safe reads unprompted (Bash(git *) covers git -C <path> show).
# Tier never_autonomous: hard-deny only coordinator/irreversible ops — NEVER deny
# ordinary git add/commit/feature-branch push/checkout (authoring desks brick otherwise).
#
# Usage:
#   ./scripts/sync-grok-readonly-permissions.sh          # apply policy
#   ./scripts/sync-grok-readonly-permissions.sh --revert # restore pre-sync launch + settings
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ALLOWLIST="${ROOT}/deploy/grok-permission-allowlist.json"
LAUNCH="${FLOTILLA_LAUNCH:-}"
if [[ -z "$LAUNCH" ]]; then
  _roster="${FLOTILLA_ROSTER:-}"
  if [[ -n "$_roster" ]]; then
    LAUNCH="${_roster%/flotilla.json}/flotilla-launch.json"
  else
    LAUNCH="${ROOT}/flotilla-launch.json"
  fi
fi

REVERT=0
if [[ "${1:-}" == "--revert" ]]; then
  REVERT=1
fi

if [[ ! -f "$LAUNCH" ]]; then
  echo "sync-grok-permissions: missing launch file $LAUNCH (set FLOTILLA_LAUNCH)" >&2
  exit 1
fi

python3 - "$ALLOWLIST" "$LAUNCH" "$REVERT" <<'PY'
import json, os, pathlib, shlex, shutil, sys

allowlist_path, launch_path, revert = sys.argv[1], sys.argv[2], int(sys.argv[3])

LAUNCH_BACKUP_SUFFIX = ".bak-grok-permissions-sync"
STATE_SUFFIX = ".grok-permissions-sync-state.json"
SETTINGS_BACKUP_NAME = "settings.local.json.bak-grok-permissions-sync"

# Authoring-breaking deny fragments — sync refuses to ship if present.
FORBIDDEN_DENY_FRAGMENTS = (
    "git add",
    "git commit",
    "git checkout",
    "git pull",
    "git stash",
    "git merge",
    "git rm",
    "git mv",
    "git cherry-pick",
    "git revert",
    "git tag",
)

def launch_file() -> pathlib.Path:
    return pathlib.Path(launch_path)

def backup_path() -> pathlib.Path:
    return launch_file().with_name(launch_file().name + LAUNCH_BACKUP_SUFFIX)

def state_path() -> pathlib.Path:
    return launch_file().with_name(launch_file().name + STATE_SUFFIX)

def desk_settings_backup(cwd: str) -> pathlib.Path:
    return pathlib.Path(cwd) / ".claude" / SETTINGS_BACKUP_NAME

def validate_desk_revert(name: str, desk_state: dict) -> str | None:
    cwd = desk_state.get("cwd", "")
    if not cwd:
        return f"{name}: no cwd in state"
    before = desk_state.get("settings_before", "absent")
    if before == "absent":
        return None
    backup = desk_state.get("settings_backup")
    if not backup:
        return f"{name}: settings_before=present but no backup path in state"
    if not pathlib.Path(backup).is_file():
        return f"{name}: settings backup missing: {backup}"
    return None

def restore_desk_settings(name: str, desk_state: dict) -> None:
    cwd = desk_state.get("cwd", "")
    settings_path = pathlib.Path(cwd) / ".claude" / "settings.local.json"
    before = desk_state.get("settings_before", "absent")
    if before == "absent":
        if settings_path.exists():
            settings_path.unlink()
            print(f"settings restored {name}: removed sync-created file")
        else:
            print(f"settings skip {name}: already absent")
        return
    backup = desk_state["settings_backup"]
    settings_path.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(backup, settings_path)
    print(f"settings restored {name}: from backup")

def cleanup_desk_backups(state: dict) -> None:
    for desk_state in state.get("desks", {}).values():
        backup = desk_state.get("settings_backup")
        if backup:
            pathlib.Path(backup).unlink(missing_ok=True)

if revert:
    launch = launch_file()
    backup = backup_path()
    state_file = state_path()
    if not backup.is_file():
        print(
            "sync-grok-permissions: no pre-sync launch backup found "
            f"({backup}); run apply before --revert",
            file=sys.stderr,
        )
        sys.exit(1)
    if not state_file.is_file():
        print(
            f"sync-grok-permissions: missing sync state {state_file}; run apply before --revert",
            file=sys.stderr,
        )
        sys.exit(1)
    with open(state_file) as f:
        state = json.load(f)
    errors = [
        err
        for name, desk_state in state.get("desks", {}).items()
        if (err := validate_desk_revert(name, desk_state))
    ]
    if errors:
        print("sync-grok-permissions: revert preflight failed (no changes made):", file=sys.stderr)
        for err in errors:
            print(f"  {err}", file=sys.stderr)
        sys.exit(1)
    shutil.copy2(backup, launch)
    for name, desk_state in state.get("desks", {}).items():
        restore_desk_settings(name, desk_state)
    cleanup_desk_backups(state)
    state_file.unlink(missing_ok=True)
    print(f"reverted launch from {backup}")
    sys.exit(0)

if not pathlib.Path(allowlist_path).is_file():
    print(f"sync-grok-permissions: missing {allowlist_path}", file=sys.stderr)
    sys.exit(1)

with open(allowlist_path) as f:
    doc = json.load(f)

allow = doc["tiers"]["read_unprompted"]["allow"]
deny = doc["tiers"]["never_autonomous"]["deny"]

for rule in deny:
    lower = rule.lower()
    for frag in FORBIDDEN_DENY_FRAGMENTS:
        if frag in lower:
            print(
                f"sync-grok-permissions: refuse — deny rule {rule!r} would brick authoring ({frag})",
                file=sys.stderr,
            )
            sys.exit(1)

perms = {"permissions": {"allow": allow, "deny": deny}}

def grok_launch_cmd():
    args = ["grok", "-m", "grok-composer-2.5-fast"]
    for rule in allow:
        args.extend(["--allow", rule])
    for rule in deny:
        args.extend(["--deny", rule])
    inner = " ".join(shlex.quote(a) for a in args)
    return f"bash -lc {shlex.quote(inner)}"

launch = launch_file()
backup = backup_path()
state_file = state_path()
if backup.is_file() or state_file.is_file():
    print(
        "sync-grok-permissions: prior sync snapshot exists "
        f"({backup.name} / {state_file.name}); run --revert before re-applying",
        file=sys.stderr,
    )
    sys.exit(1)

with open(launch) as f:
    launch_doc = json.load(f)

# Snapshot before any mutation so --revert can restore exact pre-sync state.
shutil.copy2(launch, backup)
desk_state: dict[str, dict] = {}
for name, recipe in launch_doc.get("agents", {}).items():
    if "grok" not in recipe.get("launch", ""):
        continue
    cwd = recipe.get("cwd", "")
    if not cwd:
        continue
    settings_path = pathlib.Path(cwd) / ".claude" / "settings.local.json"
    entry: dict = {"cwd": cwd}
    if settings_path.is_file():
        backup = desk_settings_backup(cwd)
        backup.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(settings_path, backup)
        entry["settings_before"] = "present"
        entry["settings_backup"] = str(backup)
    else:
        entry["settings_before"] = "absent"
        entry["settings_backup"] = None
    desk_state[name] = entry

state_doc = {
    "version": 1,
    "launch_backup": str(backup_path()),
    "desks": desk_state,
}
with open(state_path(), "w") as f:
    json.dump(state_doc, f, indent=2)
    f.write("\n")

settings_doc = json.dumps(perms, indent=2) + "\n"
launch_cmd = grok_launch_cmd()
updated_worktrees = 0
updated_recipes = 0

for name, recipe in launch_doc.get("agents", {}).items():
    cmd = recipe.get("launch", "")
    if "grok" not in cmd:
        continue
    cwd = recipe.get("cwd", "")
    if not cwd:
        print(f"skip {name}: no cwd", file=sys.stderr)
        continue
    recipe["launch"] = launch_cmd
    updated_recipes += 1
    claude_dir = pathlib.Path(cwd) / ".claude"
    claude_dir.mkdir(parents=True, exist_ok=True)
    settings_path = claude_dir / "settings.local.json"
    settings_path.write_text(settings_doc)
    updated_worktrees += 1
    print(f"synced {name}: {settings_path}")

with open(launch, "w") as f:
    json.dump(launch_doc, f, indent=2)
    f.write("\n")

print(f"allow rules: {len(allow)} | deny rules: {len(deny)} (never-autonomous only)")
print(f"launch backup: {backup_path()}")
print(f"sync state: {state_path()}")
print(f"launch recipes updated: {updated_recipes} in {launch}")
print(f"worktree settings written: {updated_worktrees}")
PY

echo "sync-grok-permissions: done"