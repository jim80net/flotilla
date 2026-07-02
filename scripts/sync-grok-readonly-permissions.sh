#!/usr/bin/env bash
# Sync the canonical grok read-only permission allowlist to:
#   1. Each grok desk worktree: <cwd>/.claude/settings.local.json
#   2. Each grok launch recipe in flotilla-launch.json (--allow/--deny CLI flags)
#
# Grok honors BOTH .claude/settings.local.json (project settings, loaded at session
# start per `grok inspect`) AND launch-time --allow/--deny flags (always enforced).
# Prefix rules like Bash(git show*) do NOT match `git -C <path> show` — use Bash(git *)
# with explicit git-write denies instead.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ALLOWLIST="${ROOT}/deploy/grok-readonly-allowlist.json"
LAUNCH="${FLOTILLA_LAUNCH:-${FLOTILLA_ROSTER%/flotilla.json}/flotilla-launch.json}"
if [[ ! -f "$LAUNCH" ]]; then
  LAUNCH="$(dirname "${FLOTILLA_ROSTER:-$ROOT/flotilla.json}")/flotilla-launch.json"
fi

if [[ ! -f "$ALLOWLIST" ]]; then
  echo "sync-grok-permissions: missing $ALLOWLIST" >&2
  exit 1
fi
if [[ ! -f "$LAUNCH" ]]; then
  echo "sync-grok-permissions: missing launch file $LAUNCH (set FLOTILLA_LAUNCH)" >&2
  exit 1
fi

python3 - "$ALLOWLIST" "$LAUNCH" <<'PY'
import json, os, pathlib, sys

allowlist_path, launch_path = sys.argv[1], sys.argv[2]
with open(allowlist_path) as f:
    perms = json.load(f)

allow = perms["permissions"]["allow"]
deny = perms["permissions"]["deny"]

def grok_launch_cmd():
    parts = ["grok", "-m", "grok-composer-2.5-fast"]
    for rule in allow:
        parts.append(f'--allow \\"{rule}\\"')
    for rule in deny:
        parts.append(f'--deny \\"{rule}\\"')
    inner = " ".join(parts)
    return f"bash -lc '{inner}'"

with open(launch_path) as f:
    launch = json.load(f)

settings_doc = json.dumps(perms, indent=2) + "\n"
launch_cmd = grok_launch_cmd()
updated_worktrees = 0
updated_recipes = 0

for name, recipe in launch.get("agents", {}).items():
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

with open(launch_path, "w") as f:
    json.dump(launch, f, indent=2)
    f.write("\n")

print(f"launch recipes updated: {updated_recipes} in {launch_path}")
print(f"worktree settings written: {updated_worktrees}")
PY

echo "sync-grok-permissions: done"