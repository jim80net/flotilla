#!/usr/bin/env sh
# Regenerate site/status.json as a REAL `flotilla status --json` run against the
# generic DEMO fleet in this directory — so the landing widget renders genuine
# command output, not hand-authored data.
#
#   ⚠ LEAK GUARD: this runs against demo-roster.json (generic xo/backend/…), NEVER
#   a real deployment's roster. Do not point the public widget at a real fleet —
#   that exposes real desk names. Keep the demo roster generic.
#
# Run from the repo root:  sh tools/landing-status/gen.sh
set -eu

DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$DIR/../.." && pwd)"

go run "$ROOT/cmd/flotilla" status --json \
  --roster "$DIR/demo-roster.json" \
  --snapshot-file "$DIR/demo-detector-state.json" \
  >"$ROOT/site/status.json"

echo "wrote $ROOT/site/status.json from a real \`flotilla status --json\` run (demo fleet)"
