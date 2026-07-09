# Tasks — fleet-wide rename rollout

Planning-only until COS merges this design and operator affirms cutover. **No live renames.**

## Phase 0 — Plan (this PR)

- [x] Identity inventory (design.md §2)
- [x] Dependency graph + staged phases (§3–4)
- [x] Shim / rollback / validation (§5–8)
- [x] Public-private partition review (§9)
- [x] Coordination with bootstrap + permissions (§5.3, §11)
- [x] Skill stub for planning desk
- [ ] COS review + merge (independent reviewer)

## Phase 1 — Operator inventory (host-local, post-merge)

- [ ] Export rename matrix (`old_name`, `new_name`, `fleet_role`, `identifier`, parent XO)
- [ ] Resolve orphan desks — add `channels[]` bindings before any rename
- [ ] Operator sign-off on matrix + phase schedule
- [ ] Create gitignored `rename-plan.json` + checkpoint root

## Phase 2 — Bootstrap prerequisites

- [ ] Merge `fleet-bootstrap-standup` — `fleet_role` on all agents
- [ ] Merge `fleet-role-permissions` — canonical policy + sync command
- [ ] Run permissions sync for leadership seats (pre-cutover baseline)

## Phase 3 — Pilot cutover (one transient desk)

- [ ] Pick generic trial: `grok-desk` → `alpha-desk-prNNN` on trial roster
- [ ] Execute per-desk atomic recipe (design §4 Phase 2)
- [ ] Validate V-R1–V-R7
- [ ] 24h soak; document surprises in private runbook

## Phase 4 — Fleet waves

- [ ] Wave A: remaining transient-task-desks
- [ ] Wave B: stable execution desks (per project-XO)
- [ ] Wave C: adjutants
- [ ] Wave D: project-XOs
- [ ] Wave E: meta-XO + COS
- [ ] Append context-ledger migration stanza (no historical rewrite)

## Phase 5 — Tooling (optional implementation PRs)

- [ ] `former_names[]` in `roster.Agent` + load validation
- [ ] `flotilla rename doctor` — orphan detect, matrix lint, webhook key preview
- [ ] `flotilla rename migrate-snapshot` — `desk_states` key rewrite
- [ ] Dash deprecation badge for `former_names`

## Phase 6 — Closeout

- [ ] Remove webhook dual-keys and workspace symlinks
- [ ] Run V-R8–V-R12 fleet-wide
- [ ] Update operator runbook (host-local) with final name map
- [ ] Retire planning desk or fold into bootstrap skill