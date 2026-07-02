#!/usr/bin/env bash
# Headless two-tier validation for deploy/grok-permission-allowlist.json.
#
# Read-tier probes exercise unprompted allows. Deny-tier probes use NON-LIVE
# targets only — if a deny rule is misconfigured and the command runs, there is
# zero blast radius (nonexistent PR, scratch ref deleted after the run).
#
# Usage: ./scripts/validate-grok-permission-tiers.sh [repo-cwd]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ALLOWLIST="${ROOT}/deploy/grok-permission-allowlist.json"
CWD="${1:-$ROOT}"
SCRATCH_REF="refs/heads/tmp-deny-probe"
FAKE_PR="999999"

cleanup_scratch_ref() {
  git -C "$CWD" push origin ":${SCRATCH_REF}" 2>/dev/null || true
}

trap cleanup_scratch_ref EXIT
cleanup_scratch_ref

python3 - "$ALLOWLIST" "$CWD" "$FAKE_PR" "$SCRATCH_REF" <<'PY'
import json, subprocess, sys

allowlist_path, cwd, fake_pr, scratch_ref = sys.argv[1:5]

with open(allowlist_path) as f:
    doc = json.load(f)
allow = doc["tiers"]["read_unprompted"]["allow"]
deny = doc["tiers"]["never_autonomous"]["deny"]

def grok(prompt: str) -> tuple[int, str]:
    args = [
        "grok", "-p", prompt,
        "-m", "grok-composer-2.5-fast",
        "--max-turns", "3",
        "--output-format", "plain",
    ]
    for rule in allow:
        args.extend(["--allow", rule])
    for rule in deny:
        args.extend(["--deny", rule])
    r = subprocess.run(args, cwd=cwd, capture_output=True, text=True, timeout=120)
    out = (r.stdout or "") + (r.stderr or "")
    return r.returncode, out

def denied(out: str) -> bool:
    lower = out.lower()
    return "permission policy" in lower or "denied by permission" in lower

tests = [
    (
        "read-git-C-show",
        f"Run shell only, no commentary: git -C {cwd} show HEAD:go.mod | head -1",
        lambda out, rc: rc == 0 and "module " in out,
    ),
    (
        "deny-gh-merge-fake-pr",
        f"Run shell only: gh pr merge {fake_pr} --merge 2>&1",
        lambda out, rc: denied(out),
    ),
    (
        "deny-force-push-scratch-ref",
        f"Run shell only: git -C {cwd} push --force origin HEAD:{scratch_ref} 2>&1",
        lambda out, rc: denied(out),
    ),
    (
        "authoring-checkout-not-hard-denied",
        f"Run shell only: git -C {cwd} checkout - 2>&1",
        lambda out, rc: not denied(out),
    ),
]

failed = []
for name, prompt, pred in tests:
    try:
        rc, out = grok(prompt)
    except subprocess.TimeoutExpired:
        print(f"FAIL {name}: grok timed out")
        failed.append(name)
        continue
    ok = pred(out, rc)
    status = "PASS" if ok else "FAIL"
    print(f"{status} {name} grok_exit={rc}")
    if not ok:
        print("  ", out[:300].replace("\n", " "))
        failed.append(name)

if failed:
    print("FAILED:", ", ".join(failed))
    sys.exit(1)
print("ALL PASS (deny probes used non-live targets only)")
PY

echo "validate-grok-permission-tiers: done"