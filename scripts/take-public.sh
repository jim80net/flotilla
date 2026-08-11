#!/usr/bin/env bash
# Publication decision runner: prove remote all-ref history clean before the
# requested publish/mirror command. Written is not readable, present is not
# reachable; this gate asks what the remote can actually serve to a fetcher.
set -euo pipefail

if [ "${1:-}" != "--" ] || [ "$#" -lt 2 ]; then
  echo "usage: scripts/take-public.sh -- <publish-or-mirror-command> [args...]" >&2
  exit 2
fi
shift

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "take-public: not a git repository" >&2
  exit 1
}

bash "$repo_root/scripts/check-private-boundary.sh" --history
exec "$@"
