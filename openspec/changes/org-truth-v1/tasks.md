# Tasks — org-truth v1

**Branch:** `openspec/org-truth-v1`  
**Gate:** product XO reviews design; chief of staff merges (no self-merge).  
**Dispatch:** `flotilla-dispatch-d81ad664`

## PR plan (phased)

### PR0 — Openspec only (this change)

- [x] 0.1 `proposal.md` — problem, unified DAG, fail-closed multi-home
- [x] 0.2 `design.md` — loader spine, migration, dash/watch inputs, generic examples
- [x] 0.3 Spec deltas: `specs/{roster,watch,dash,goals}/spec.md`
- [x] 0.4 `tasks.md` — this file
- [x] 0.4b `fleet-org.example.yaml` — generic fixture aligned with `flotilla.example.json`
- [x] 0.4c design §9 defaults locked (multi-parent / strict goals / YAML primary)
- [x] 0.4d private-boundary scrub (no host-local fleet-home path tokens)
- [ ] 0.5 product-XO design gate (surface; do not self-merge)
- [ ] 0.6 chief-of-staff merge of openspec PR

### PR1 — Loader + derive-from-channels (behavior-compatible)

- [ ] 1.1 Package `internal/org` (or `internal/roster/org`) with `LoadFile`, `DAG`, `Compile`, `DeriveFromChannels`
- [ ] 1.2 Wire roster load to call derive path always; store DAG on config or side accessor
- [ ] 1.3 Golden tests: `AgentsAbove`/`AgentsBelow`/`OwningXO` parity vs current synthesis for example-shaped fixtures
- [ ] 1.4 Improve `assertSynthesisAcyclic` error text (name both agents + channel ids)
- [ ] 1.5 `go test -race` on roster/org

### PR2 — Optional `fleet-org.yaml` + agreement refuse

- [x] 2.1 Schema parse (YAML); reject cycles, unknown parents, dup ids
- [x] 2.2 Agreement check vs channel-derived edges when file present
- [x] 2.3 One `home_channel_id` per node invariant
- [x] 2.4 `--org-file` / `FLOTILLA_ORG_FILE` on watch + dash
- [x] 2.5 `fleet-org.example.yaml` + docs blurb in `docs/ARCHITECTURE.md` / watch-runbook note
- [x] 2.6 Fixtures: agree, disagree, mutual-home, duplicate-home

### PR3 — Watch consumes compiled DAG

- [ ] 3.1 Synthesis routing / owed marking reads DAG when available
- [ ] 3.2 `OwningXO` prefers `DAG.Parent`
- [ ] 3.3 Fatal start on org compile failure (integration test or cmd-level)
- [ ] 3.4 Watch runbook one-pager delta (`docs/watch-runbook.md`)

### PR4 — Dash topology + Goals layout bridge

- [ ] 4.1 `/api/topology` includes `org_source` + node parent list from DAG
- [ ] 4.2 Goals org layout parent spokes use same DAG (coordinate with `dash-org-graph-v2`)
- [ ] 4.3 Optional goals diagnostic field for owner/org mismatch
- [ ] 4.4 `FLOTILLA_ORG_STRICT_GOALS` refuse path + test

### PR5 — Doctor / derive UX (optional follow-on)

- [ ] 5.1 `flotilla org derive` prints recommended YAML from channels (stdout only)
- [ ] 5.2 `flotilla doctor` reports org source + cycle risk without starting watch
- [ ] 5.3 Promote relevant requirements into `openspec/specs/` on archive

## Explicit non-goals in implementation PRs

- No deployment-specific agent names in tests or examples
- No adjutant coalesce/disaggregate (blocked on `adjutant-buffer-v2` after this merges)
- No automatic Discord channel rewrite

## Supersede / merge notes

| Change | Disposition |
|--------|-------------|
| `dash-org-graph-v2` | **Keep** for UI/schema; topology/layout **consume** org-truth DAG after PR4 |
| `authority-domains-org-chart` | Orthogonal (repos); no change |
| `visibility-synthesis` / stackable | **Consumers**; PR3 rewires inputs only |
