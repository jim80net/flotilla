#!/usr/bin/env bash
# Self-test for deploy/grok-coordinator-permission-allowlist.json posture.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

EMBED="${ROOT}/cmd/flotilla/grok_coordinator_allowlist.json"
DEPLOY="${ROOT}/deploy/grok-coordinator-permission-allowlist.json"
if ! cmp -s "$EMBED" "$DEPLOY"; then
  echo "embedded allowlist drifted from deploy/ — sync cmd/flotilla/grok_coordinator_allowlist.json" >&2
  exit 1
fi

python3 - "$DEPLOY" <<'PY'
import json, sys
from pathlib import Path

doc = json.loads(Path(sys.argv[1]).read_text())
policy = doc.get("policy", {})
if policy.get("on_gatekeeper_error") != "abstain":
    raise SystemExit("policy.on_gatekeeper_error must be abstain")

allow = doc["tiers"]["read_unprompted"]["allow"]
deny = doc["tiers"]["never_autonomous"]["deny"]

if not any("flotilla notify" in r for r in allow):
    raise SystemExit("coordinator allow tier must include flotilla notify")
if not any("flotilla send" in r for r in allow):
    raise SystemExit("coordinator allow tier must include flotilla send")
if any("gh pr merge" in d for d in deny):
    raise SystemExit("coordinator deny tier must not block gh pr merge")

plus_denies = [d for d in deny if " push +" in d or " push *+*" in d]
if len(plus_denies) < 2:
    raise SystemExit("expected +refspec force-push deny variants")

if not any("refs/heads/main" in d for d in deny):
    raise SystemExit("expected refs/heads/main push deny")

for frag in ("git add", "git commit", "git checkout"):
    if any(frag in d.lower() for d in deny):
        raise SystemExit(f"coordinator deny must not brick authoring via {frag!r}")

print(f"grok coordinator allowlist selftest: OK ({len(allow)} allow, {len(deny)} deny, abstain-on-error)")
PY