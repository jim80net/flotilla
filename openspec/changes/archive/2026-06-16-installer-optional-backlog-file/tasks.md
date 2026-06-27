## 1. TDD — installer regression (the functional-identity lock)

- [x] 1.1 TEST (SET) `TestInstallerBacklogSetAppendsArg`: render a 6-key env (5 + `FLOTILLA_BACKLOG_FILE=/srv/fleet/backlog.md`) → `ExecStart` ends with exactly ` --backlog-file /srv/fleet/backlog.md` (single-spaced, no surviving placeholder).
- [x] 1.2 TEST (UNSET, the no-regression lock) `TestInstallerBacklogUnsetOmitsArg` + the existing `TestInstallerGeneratesExpectedFunctionalUnit`: the 5-key fixture's `ExecStart` stays exactly `…--ack-file /srv/fleet/xo-alive` — no `--backlog-file`, no trailing space.
- [x] 1.3 TEST (inherited-env no-leak, guards the pre-clear) `TestInstallerBacklogInheritedEnvNoLeak`: export `FLOTILLA_BACKLOG_FILE` in the installer subprocess env but render the 5-key fixture → `ExecStart` has no `--backlog-file`.

## 2. IMPL — installer/template/.env plumbing

- [x] 2.1 `deploy/flotilla-watch.service.in`: append `@FLOTILLA_BACKLOG_ARG@` to the `ExecStart` line directly after `@FLOTILLA_ACK_FILE@` (no separating space); document the computed-fragment mechanism in a comment.
- [x] 2.2 `deploy/flotilla-watch-install.sh`: pre-clear `FLOTILLA_BACKLOG_FILE=''` (load-bearing — the host exports it); add it to the key allowlist (loaded, not warned); DO NOT add it to the required-missing loop; add it to the template-token-in-value safety check; compute `backlog_arg` (` --backlog-file <path>` when set, else `""`) and substitute `@FLOTILLA_BACKLOG_ARG@`; non-fatal warning if the backlog file is missing.
- [x] 2.3 `deploy/flotilla-watch.env.example`: add the OPTIONAL 6th key, COMMENTED OUT, documenting gate-OFF-when-unset + byte-identical-when-unset semantics.

## 3. Verify

- [x] 3.1 `go test ./...` green (10 installer tests incl. the 4 new ones); `go vet ./...` clean; `bash -n` clean.
- [x] 3.2 `openspec validate installer-optional-backlog-file`.
- [x] 3.3 `/open-code-review` (CLEAN, 0 comments) + systems-review (CLEAN-WITH-NITS, no P1) on the diff. Folded: P2 `&`-in-path substitution corruption → `shopt -u patsub_replacement` (root-cause, all keys) + `TestInstallerBacklogPathWithAmpersand`; P3 glob/grep mismatch → offender-grep charset hardened. P3 space-splitting noted as pre-existing out-of-scope limitation.
- [x] 3.4 PR #83 → XO review + merge (MERGED 2026-06-16). (After merge: XO does the fleet drop-in → `.env` cutover.)
