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