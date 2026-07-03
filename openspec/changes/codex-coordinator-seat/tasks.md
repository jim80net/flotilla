# Tasks — codex-coordinator-seat

## 1. Design (this PR)
- [x] 1.1 proposal.md + design.md + spec deltas
- [ ] 1.2 Design trio (systems-review + open-code-review)
- [x] 1.3 Surface to COS (no self-merge) — PR #261 head 413288f

## 2. Generic harness parity (flotilla-dev — coordinate, no duplicate PRs)
- [ ] 2.1 `harnessAllocationSurface`: honor `surface: "codex"` for coordinators
- [ ] 2.2 `delegatenudge`: `IsManagementHarness` includes codex; harness-neutral `NudgePrompt`
- [ ] 2.3 Seat-swap + supervised-trial runbook (docs; roster template; rollback via `flotilla switch`)
- [ ] 2.4 `flotilla.example.json` generic coordinator-on-codex example (role + surface fields)

## 3. Codex coordinator surface (codex-harness-dev)
- [ ] 3.1 Post-auth fixture capture (working/idle/approval/composer) — operator gate
- [ ] 3.2 `ComposerStateProbe` on codex driver
- [ ] 3.3 Coordinator launch recipe: `FLOTILLA_SELF`, `FLOTILLA_SECRETS`, PATH
- [ ] 3.4 `scaffoldCodexCoordinatorRules` (distinct from execution `flotilla-desk.rules`)
- [ ] 3.5 `xo-outbound` doctrine member (coordinator-only identity-append)
- [ ] 3.6 AGENTS.md budget test (constitutional + xo-outbound < 32 KiB)

## 4. Validation
- [ ] 4.1 Unit/integration: coordinator init scaffolds secrets launch + coordinator rules
- [ ] 4.2 Detector classifier smoke on codex turn-final fixtures
- [ ] 4.3 `go test ./...`

## 5. Supervised trial (post-implementation — operator gate)
- [ ] 5.1 Provision one low-stakes project XO with `surface: "codex"`
- [ ] 5.2 Execute trial script (design §9); rollback drill
- [ ] 5.3 Trial postmortem → promote or revert