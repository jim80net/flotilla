#!/usr/bin/env bash
# install-hooks.sh — point this repo's git at the version-controlled hooks in
# scripts/hooks (currently: the pre-push partition backstop). Idempotent.
#
#   scripts/install-hooks.sh
#
# This sets core.hooksPath to scripts/hooks for THIS clone only (a local config; it is
# never pushed). The hooks are a convenience backstop — CI's private-boundary job is the
# enforcing authority, so a contributor who skips this is still gated at the PR.
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
git -C "$repo_root" config core.hooksPath scripts/hooks
chmod +x "$repo_root/scripts/hooks/pre-push"
echo "installed: core.hooksPath → scripts/hooks (pre-push partition backstop active)"
