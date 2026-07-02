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
import json, os, pathlib, shlex, shutil, subprocess, sys, time

allowlist_path, launch_path, revert = sys.argv[1], sys.argv[2], int(sys.argv[3])
PLAIN = "bash -lc 'grok -m grok-composer-2.5-fast'"

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

def find_revert_backup(launch_path: pathlib.Path) -> pathlib.Path | None:
    parent = launch_path.parent
    candidates = sorted(
        parent.glob("flotilla-launch.json.bak-*"),
        key=lambda p: p.stat().st_mtime,
        reverse=True,
    )
    for path in candidates:
        try:
            with open(path) as f:
                data = json.load(f)
        except (OSError, json.JSONDecodeError):
            continue
        for recipe in data.get("agents", {}).values():
            cmd = recipe.get("launch", "")
            if "grok" in cmd and "--deny" not in cmd:
                return path
    return None

def restore_settings(cwd: str, name: str) -> None:
    sp = pathlib.Path(cwd) / ".claude" / "settings.local.json"
    if not sp.exists():
        print(f"settings skip {name}: already absent")
        return
    git_ok = subprocess.run(
        ["git", "-C", cwd, "rev-parse", "--git-dir"], capture_output=True
    ).returncode == 0
    if git_ok:
        tracked = (
            subprocess.run(
                ["git", "-C", cwd, "ls-files", "--error-unmatch", ".claude/settings.local.json"],
                capture_output=True,
            ).returncode
            == 0
        )
        head = (
            subprocess.run(
                ["git", "-C", cwd, "cat-file", "-e", "HEAD:.claude/settings.local.json"],
                capture_output=True,
            ).returncode
            == 0
        )
        if tracked and head:
            subprocess.run(
                ["git", "-C", cwd, "checkout", "HEAD", "--", ".claude/settings.local.json"],
                check=True,
            )
            print(f"settings restored {name}: git HEAD")
            return
    sp.unlink()
    print(f"settings restored {name}: deleted sync-written file")

if revert:
    launch_file = pathlib.Path(launch_path)
    backup = find_revert_backup(launch_file)
    if backup is None:
        print("sync-grok-permissions: no pre-sync launch backup found", file=sys.stderr)
        sys.exit(1)
    shutil.copy2(launch_file, launch_file.with_name(launch_file.name + f".bak-pre-revert-{int(time.time())}"))
    with open(backup) as f:
        backup_doc = json.load(f)
    with open(launch_file) as f:
        launch = json.load(f)
    for name, recipe in launch.get("agents", {}).items():
        if "grok" not in recipe.get("launch", ""):
            continue
        pre = backup_doc.get("agents", {}).get(name, {}).get("launch", PLAIN)
        recipe["launch"] = pre
        cwd = recipe.get("cwd", "")
        if cwd:
            restore_settings(cwd, name)
    with open(launch_file, "w") as f:
        json.dump(launch, f, indent=2)
        f.write("\n")
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

print(f"allow rules: {len(allow)} | deny rules: {len(deny)} (never-autonomous only)")
print(f"launch recipes updated: {updated_recipes} in {launch_path}")
print(f"worktree settings written: {updated_worktrees}")
PY

echo "sync-grok-permissions: done"