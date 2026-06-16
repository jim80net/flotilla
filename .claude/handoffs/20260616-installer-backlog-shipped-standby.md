# Handoff: installer optional `FLOTILLA_BACKLOG_FILE` SHIPPED → flotilla-dev STANDBY

**Date:** 2026-06-16
**Branch:** main @ f352e4e (synced with origin/main)
**Working directory:** /home/jim/workspace/github.com/jim80net/flotilla

## Objective

flotilla-dev session for the flotilla product repo (`jim80net/flotilla`). This session
implemented the installer follow-up that the prior session fully designed, then wrapped to
standby with the authorized queue drained. The live fleet is the Spark/your-org deployment;
the live roster + state live under `/home/jim/workspace/github.com/your-org/your-repo/state/`,
NOT in this repo.

## Session Summary

Implemented the optional `FLOTILLA_BACKLOG_FILE` installer key (design was 100% resolved in the
prior handoff `20260616-installer-optional-backlog-file.md`), straight to TDD. Shipped via two
PRs (feature + doc-only archive), both merged. Folded one real systems-review finding (a bash
5.2 `&`-substitution bug) as a root-cause fix. Captured three generic-craft learnings as global
skills. Queue now drained; rotating to standby.

## Completed Work

### PR #83 — installer optional `FLOTILLA_BACKLOG_FILE` key (MERGED)

**PR:** #83 — https://github.com/jim80net/flotilla/pull/83 (merged 2026-06-16, commit `fa6b215`)
**Problem:** The backlog gate (`Backlog-gated goal-driven continuation`, watch spec) is opt-in via
the daemon flag `--backlog-file`, but the anti-drift installer only knew the 5 required keys — so
enabling the gate required a hand-written systemd drop-in (the exact unit drift the installer
prevents). The live Spark host has such a drop-in (`~/.config/systemd/user/flotilla-watch.service.d/backlog.conf`).
**Fix (installer/template/.env plumbing only — NO Go daemon changes; `cmd/flotilla/watch.go:58`
already reads the flag/env):**
- `deploy/flotilla-watch.service.in` — appended a computed-fragment placeholder
  `@FLOTILLA_BACKLOG_ARG@` directly to the `ExecStart` line (no separating space).
- `deploy/flotilla-watch-install.sh` — pre-clear the new key (load-bearing, see below); allowlist
  it (loaded, not warned); NOT in the required-missing check (optional); token-safety guard
  extended; compute ` --backlog-file <path>` when set / `""` when unset and substitute the
  placeholder; non-fatal warning if the backlog file is absent.
- `deploy/flotilla-watch.env.example` — commented-out optional 6th key documenting the semantics.
**Behavior:** SET ⇒ ExecStart gains ` --backlog-file <path>`; UNSET ⇒ omitted entirely (no
`--backlog-file ''`, no trailing space), unit byte-identical (functional-directive level) to a
no-backlog install. Gate OFF when absent = today's default, zero behavior change for existing installs.
**Tests (10 installer tests, 4 new — all green):** `TestInstallerBacklogSetAppendsArg`,
`TestInstallerBacklogUnsetOmitsArg`, `TestInstallerBacklogInheritedEnvNoLeak`,
`TestInstallerBacklogPathWithAmpersand` (+ the existing functional-identity regression that pins
the unset baseline). `go test ./...` + `go vet` green, `bash -n` clean, CI green.
**Review:** `/open-code-review` CLEAN (0 comments). systems-review CLEAN-WITH-NITS (no P1) — folded:
- **P2 root-cause:** bash 5.2+ enables `patsub_replacement` by default; a literal `&` in a `${//}`
  REPLACEMENT expands to the matched text, corrupting any path with `&` (pre-existing for all 5
  keys). Fixed with `shopt -u patsub_replacement 2>/dev/null || true` before substituting (literal
  for all keys, version-safe) + a regression test. (Verified live on bash 5.2.21.)
- **P3:** hardened the fail-loud offender-grep charset (`@FLOTILLA_[A-Z_.*]*@`) so a glob-pattern
  survivor can never print an empty error.
- **P3 (noted, out of scope):** unquoted values word-split on spaces — pre-existing for all keys.

### PR #84 — archive the openspec change (MERGED, doc-only)

**PR:** #84 — https://github.com/jim80net/flotilla/pull/84 (merged 2026-06-16, commit `f352e4e`)
Folded the ADDED `watch` requirement "Deploy-surface enablement of the backlog gate" into
`openspec/specs/watch/spec.md` (verified zero content loss — pure append, `+1 / ~0 / -0`) and
moved the change to `openspec/changes/archive/2026-06-16-installer-optional-backlog-file/`.
`openspec validate --specs` → 5 passed. Merged autonomously (doc-only archive chore).

### Key learnings captured (global skills — generic craft, persisted on disk, no PR)
- `~/.claude/skills/bash-patsub-replacement-ampersand/` — the bash-5.2 `&`/`patsub_replacement` gotcha.
- `~/.claude/skills/installer-preclear-exported-env-keys/` — pre-clear file-read keys the live host exports.
- `~/.claude/skills/edit-lost-after-checkout/` — UPDATED with the `git checkout <ref> -- <files>`
  path-restore + `git stash` sweep facets (a checkout/stash round-trip to spot-check byte-identity
  silently reverted my uncommitted deploy-file edits this session; recovered from the stash).

## Current State

### Git
```
main @ f352e4e (= origin/main)
f352e4e chore(openspec): archive completed change installer-optional-backlog-file (#58) (#84)
fa6b215 feat(deploy): installer optional FLOTILLA_BACKLOG_FILE key (#58) (#83)
2a766f0 chore: session assets — installer optional FLOTILLA_BACKLOG_FILE handoff (#82)
```
Working tree clean. Active openspec changes: `watch-gateway-doctor` (13/16), `watch-heartbeat-sidechannel`
(9/21), `agent-workspace` (19/22), `discord-voice` (✓ Complete) — NONE are this session's work.

### My open PRs
- **#62** `feat(surface): the cursor driver SKELETON` — **[HELD]** INERT until operator-present
  live-capture. NOT actionable without the operator. Left as-is.

## What is explicitly NOT mine (do not touch)

- **The Spark cutover for the backlog key is the XO's.** Replacing the live drop-in
  (`~/.config/systemd/user/flotilla-watch.service.d/backlog.conf`) with the `.env` key + re-running
  the installer + restarting the heartbeat clock touches the safety-critical clock — the XO does it
  deliberately. (When it happens: add `FLOTILLA_BACKLOG_FILE=<path>` to the host's
  `deploy/flotilla-watch.env`, `bash deploy/flotilla-watch-install.sh`, then the operator-controlled
  `systemctl --user restart flotilla-watch.service`, then `rm` the now-redundant drop-in + reload.)

## Remaining Work / Next Item

### Voice Phase-1 build [XO-GATED — do NOT start unprompted]
**What:** the likely next build. **Greenlit but METERED Grok-Voice spend** — a genuine
operator/XO decision gate (money). The XO will confirm the design is build-ready before kicking it
off. The `discord-voice` openspec change is ✓ Complete; voice Phase-1 build-complete handoff exists
(`.claude/handoffs/20260611-voice-phase1-build-complete.md`). **Do NOT begin without the XO's
explicit go** — it is not free authorized work (it spends metered external API).

### Tracked follow-ups (carried, NOT this session's scope)
- grok official-CLI `AwaitingApproval` gate markers — needs a live capture of that state (#58).
- grok multi-line / bracketed-paste submit validation (#58).
- Cross-process atomicity for confirmed delivery (operator `flotilla send` racing a daemon confirm).
- Voice confirmation inherit (#72).

## Gotchas & Environment Notes
- `go` is at `/usr/local/go/bin` (not on PATH): prefix go commands with `export PATH=$PATH:/usr/local/go/bin`.
- Reviews: this repo has NO cubic — `/open-code-review` + systems-review are the gates of record.
- `openspec validate`: bare `--strict` prints a menu in this non-interactive shell; use
  `--specs` / `--changes` / `<item-name>`.
- Merge policy: merge on clean gates; the XO claims the hard code-review gate on substantive PRs,
  this dev session autonomously merges doc-only archive chores (e.g. #84).
- **Do NOT do `git stash` / `git checkout <ref> -- <files>` round-trips to spot-check while you have
  uncommitted work** — it silently reverts working-tree edits (no commits yet ⇒ reverts to
  origin/main). Use `git show <ref>:<path>` to a temp file instead. (Bit me this session; recovered.)

## To Resume
1. Read this file.
2. `cd /home/jim/workspace/github.com/jim80net/flotilla && git checkout main && git pull --ff-only`
3. Wait for the XO to assign the next item (likely voice Phase-1, after its money gate clears). Do
   not start metered-spend work unprompted.
