#!/usr/bin/env bash
# Self-test for sync-grok-readonly-permissions.sh launch-command generation.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

python3 - "$ROOT/deploy/grok-permission-allowlist.json" <<'PY'
import json, shlex, subprocess, sys
from pathlib import Path

doc = json.loads(Path(sys.argv[1]).read_text())
allow = doc["tiers"]["read_unprompted"]["allow"]
deny = doc["tiers"]["never_autonomous"]["deny"]

args = ["grok", "-m", "grok-composer-2.5-fast"]
for rule in allow:
    args.extend(["--allow", rule])
for rule in deny:
    args.extend(["--deny", rule])
inner = " ".join(shlex.quote(a) for a in args)
cmd = f"bash -lc {shlex.quote(inner)}"
subprocess.run(["bash", "-n", "-c", cmd], check=True)

if any(r == "Bash(git *)" for r in allow):
    raise SystemExit("blanket Bash(git *) must not be in read tier")

for frag in ("git add", "git commit", "git checkout", "git pull", "git stash"):
    if any(frag in d.lower() for d in deny):
        raise SystemExit(f"authoring-breaking deny contains {frag!r}")

if not any("host *" in r for r in allow):
    raise SystemExit("expected spaced allow rule Bash(host *)")

if not any("git * show" in r for r in allow):
    raise SystemExit("expected git -C read variant Bash(git * show*)")

if not any("git * push" in r and "force" in r for r in deny):
    raise SystemExit("expected git -C never-autonomous deny variant")

plus_denies = [d for d in deny if " push +" in d or " push *+*" in d]
if len(plus_denies) < 2:
    raise SystemExit("expected +refspec force-push deny variants")

colon_denies = [d for d in deny if " :main" in d or " :master" in d or " :refs/heads/main" in d]
if len(colon_denies) < 4:
    raise SystemExit("expected :refspec main/master deletion deny variants")

refs_denies = [d for d in deny if "refs/heads/main" in d or "refs/heads/master" in d]
if len(refs_denies) < 4:
    raise SystemExit("expected refs/heads/main|master push deny variants")

print(f"sync launch cmd selftest: OK ({len(allow)} allow, {len(deny)} deny)")
PY

# nounset: must not abort before resolving default launch path.
out="$(bash -u "${ROOT}/scripts/sync-grok-readonly-permissions.sh" 2>&1 || true)"
if [[ "$out" != *"missing launch file"* ]]; then
  echo "nounset selftest: expected missing launch file error, got: $out" >&2
  exit 1
fi
if [[ "$out" == *"unbound variable"* ]]; then
  echo "nounset selftest: FLOTILLA_ROSTER unset must not trip nounset" >&2
  exit 1
fi
echo "nounset selftest: OK"

# apply/revert round-trip: backup + state must exist; revert restores pre-sync launch.
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
launch="${tmpdir}/flotilla-launch.json"
cat >"$launch" <<'JSON'
{
  "agents": {
    "grok-a": {
      "launch": "bash -lc 'grok -m grok-composer-2.5-fast --custom-flag'",
      "cwd": "PLACEHOLDER_CWD"
    }
  }
}
JSON
python3 - "$launch" "$tmpdir" <<'PY'
import json, pathlib, sys
launch, tmp = sys.argv[1], pathlib.Path(sys.argv[2])
doc = json.loads(pathlib.Path(launch).read_text())
doc["agents"]["grok-a"]["cwd"] = str(tmp)
pathlib.Path(launch).write_text(json.dumps(doc, indent=2) + "\n")
settings = tmp / ".claude" / "settings.local.json"
settings.parent.mkdir(parents=True)
settings.write_text('{"permissions":{"allow":[],"deny":[]}}\n')
PY

FLOTILLA_LAUNCH="$launch" bash "${ROOT}/scripts/sync-grok-readonly-permissions.sh" >/dev/null
backup="${launch}.bak-grok-permissions-sync"
state="${launch}.grok-permissions-sync-state.json"
if [[ ! -f "$backup" || ! -f "$state" ]]; then
  echo "revert selftest: apply must create launch backup and sync state" >&2
  exit 1
fi
second_out="$(FLOTILLA_LAUNCH="$launch" bash "${ROOT}/scripts/sync-grok-readonly-permissions.sh" 2>&1 || true)"
if [[ "$second_out" != *"run --revert before re-applying"* ]]; then
  echo "revert selftest: second apply without revert must refuse, got: $second_out" >&2
  exit 1
fi
if grep -q -- '--custom-flag' "$launch"; then
  echo "revert selftest: apply should replace launch command" >&2
  exit 1
fi

FLOTILLA_LAUNCH="$launch" bash "${ROOT}/scripts/sync-grok-readonly-permissions.sh" --revert >/dev/null
if ! grep -q -- '--custom-flag' "$launch"; then
  echo "revert selftest: revert should restore original launch command" >&2
  exit 1
fi
if [[ -f "$state" ]]; then
  echo "revert selftest: state file should be removed after revert" >&2
  exit 1
fi
if ! python3 - "${tmpdir}/.claude/settings.local.json" <<'PY'
import json, sys
doc = json.loads(open(sys.argv[1]).read())
perms = doc.get("permissions", {})
if perms.get("allow") != [] or perms.get("deny") != []:
    raise SystemExit(1)
PY
then
  echo "revert selftest: should restore pre-sync settings.local.json" >&2
  exit 1
fi
echo "revert round-trip selftest: OK"