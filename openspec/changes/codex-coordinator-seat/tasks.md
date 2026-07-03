# Tasks — codex-coordinator-seat

## 1. Design
- [x] 1.1 proposal.md + design.md + spec deltas
- [x] 1.2 Design trio (COS independent gate on PR #261)
- [x] 1.3 Surface to COS — PR #261 merged (`cb87d53`)

## 2. Generic harness parity (flotilla-dev — do NOT duplicate in this lane)
- [x] 2.0 Lane seam flagged to flotilla-dev (COS dispatched); coordinate before touching `harnessAllocationSurface` or `delegatenudge`
- [x] 2.1 `harnessAllocationSurface`: honor `surface: "codex"` for coordinators — **flotilla-dev** (PR #262)
- [x] 2.2 `delegatenudge`: `IsManagementHarness` includes codex; harness-neutral `NudgePrompt` — **flotilla-dev** (PR #262)
- [x] 2.3 Seat-swap + supervised-trial runbook — **flotilla-dev** (PR #262)
- [x] 2.4 `flotilla.example.json` coordinator-on-codex example — **flotilla-dev** (PR #262)

## 3. Codex coordinator surface (codex-harness-dev)
- [ ] 3.1 Post-auth fixture capture (working/idle/approval/composer) — **[blocked: operator codex login]**
- [x] 3.2 `ComposerStateProbe` on codex driver — binary-sourced `›` classifier; post-auth revalidation still on 3.1
- [x] 3.3 Coordinator launch recipe: `FLOTILLA_SELF`, `FLOTILLA_SECRETS`, PATH
- [x] 3.4 `scaffoldCodexCoordinatorRules` (distinct from execution `flotilla-desk.rules`)
- [x] 3.5 `xo-outbound` doctrine member (coordinator-only identity-append)
- [x] 3.6 AGENTS.md budget test (constitutional + xo-outbound < 32 KiB)
- [x] 3.7 **Code guard:** `workspace init` refuses codex coordinator until codex implements `ComposerStateProbe` (fail-closed in binary, not doctrine-only)

## 4. Validation
- [x] 4.1 Unit/integration: coordinator init scaffolds secrets launch + coordinator rules (PR #262 + 3.2)
- [ ] 4.2 Detector classifier smoke on codex turn-final fixtures — blocked on 3.1
- [x] 4.3 `go test ./...`

## 5. Supervised trial (post-implementation — operator gate)
- [ ] 5.1 Provision one low-stakes project XO with `surface: "codex"`
- [ ] 5.2 Execute trial script (design §9); rollback drill
- [ ] 5.3 Trial postmortem → promote or revert