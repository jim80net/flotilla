# Tasks — fleet bootstrap / standup

Phased implementation after design PR merges (COS gate). Check off in order; parallelize only
where noted.

## Phase 0 — Design gate (this PR)

- [x] `proposal.md` + `design.md` + `specs/fleet-bootstrap/spec.md`
- [x] `.claude/skills/flotilla-fleet-bootstrap/SKILL.md` stub
- [ ] COS review + merge (independent reviewer; builder does not self-merge)

## Phase 1 — Roster role metadata

- [ ] Add `fleet_role` to `Agent` struct (`internal/roster/roster.go`) with validation table
      from design §2
- [ ] Extend `flotilla.example.json` with `fleet_role` on each example agent
- [ ] Unit tests: cos/xo/adj/desk/transient + mismatch warnings
- [ ] Doctor stub: derive vs explicit role diff (warn-only)

## Phase 2 — `flotilla bootstrap doctor`

- [ ] `cmd/flotilla/bootstrap.go` — `doctor` subcommand, read-only
- [ ] `internal/bootstrap/doctor.go` — checks B001–B010
- [ ] `internal/bootstrap/topology.go` — desk-has-XO audit
- [ ] JSON + human output; exit code 1 on fail-severity
- [ ] Tests with fixture rosters from `testdata/bootstrap/`

## Phase 3 — Permission sync

- [ ] `scripts/bootstrap-sync-permissions.sh` — map `fleet_role`+`surface` → deploy template
- [ ] Worktree writer for `.claude/settings.local.json` (Claude desks)
- [ ] Document Grok/Codex equivalents in skill (no secret paths in repo)
- [ ] Idempotent: skip when template version stamp matches

## Phase 4 — Apply + launch recipes

- [ ] `flotilla bootstrap apply --roster` — scaffold only missing files
- [ ] Emit per-agent launch one-liner (register + FLOTILLA_SELF + exec)
- [ ] Coordinator env: `FLOTILLA_SECRETS` reminder
- [ ] `llm.md` new § "Fleet bootstrap" linking skill + doctor

## Phase 5 — Validation harness

- [ ] `scripts/bootstrap-validate.sh` — runs V1–V8 from design §9
- [ ] Optional: CI fixture roster dry-run (doctor only, no live tmux)