# Tasks — fleet role permissions (focused desk)

Separate lane from dash and fleet-bootstrap topology. COS gate before implementation merges.

## Phase 0 — Design + prototype (this PR)

- [x] Route A vs B evaluation + hybrid recommendation (`design.md` §3–5)
- [x] `deploy/flotilla-permissions/canonical-roles.json` prototype
- [x] Spec + skill stub
- [x] Operator correction: zero approval noise / autonomous fleet design criteria (`design.md` §0)
- [x] Operator correction: ops-xo vs product XO authority boundary (aligned with PR #520 §2.2)
- [x] Cubic bounce fixes: bootstrap forward-ref, adjutant tier split, design_criteria consumer
- [x] Mechanical adjutant protected-window design (`adjutant-operator-protected-window/`)
- [ ] COS review + merge (independent reviewer)

## Phase 1 — Compiler

- [ ] `scripts/compile-flotilla-permissions.sh` — JSON → gatekeeper TOML overlays; enforce
      `policy.design_criteria`; emit header comment in artifacts
- [ ] Emit grok allowlist JSON (backward compatible with `deploy/grok-*-permission-allowlist.json`)
- [ ] Emit Claude `permissions` fragment for merge into `settings.local.json`
- [ ] Emit Codex rules snippet + documented hook install line
- [ ] CI: compile + diff check committed materialized outputs

## Phase 2 — Bootstrap permissions subcommand

- [ ] `flotilla bootstrap permissions doctor` — stamp, hook, drift vs canonical
- [ ] `flotilla bootstrap permissions sync --agent <name>` — idempotent materialize
- [ ] Checks P001–P003 + P009: leadership must allow register/touch/status + full §0.1 flow set
- [ ] Integration test with fake worktree + mock settings paths

## Phase 3 — Gatekeeper integration

- [ ] Document overlay include path in flotilla + gatekeeper README cross-link
- [ ] Optional: `gatekeeper.toml` `include` directive for `flotilla-<role>.toml` (if needed)

## Phase 4 — Deprecate hand-maintained deploy JSON

- [ ] Generate `deploy/grok-coordinator-permission-allowlist.json` from canonical
- [ ] Generate `deploy/grok-permission-allowlist.json` from canonical
- [ ] Update `sync-grok-readonly-permissions.sh` to consume generated output

## Phase 5 — Supervised validation

- [ ] Run validation P1–P11 on generic trial roster (`flotilla.example.json` names)
- [ ] P9: full heartbeat cycle zero approval modals on coordinator seat
- [ ] Codex coordinator: confirm hook deny under auto policy (P8) + autonomy gap doctor (P11)
- [ ] Operator sign-off; remove Full Access stopgap on trial seat