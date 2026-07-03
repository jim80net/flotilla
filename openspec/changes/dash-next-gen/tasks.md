# Tasks — dash-next-gen (#267)

## 0. Design gate (this PR)

- [x] 0.1 `proposal.md` + `design.md` + spec deltas + tasks
- [x] 0.1b COS gate fix round — `depends_on` + `conversation_agent`, vacuous-achieved guard,
  status precedence, inline/desk roll-up, SessionMirrorRecord `verbose` field, GoalsDoc `edges[]`,
  proposal Impact paths, cubic thread replies
- [ ] 0.2 Design trio review (systems-review + open-code-review + STORM)
- [ ] 0.3 COS independent gate — design approved before implementation

## 1. Session mirror core (flotilla-dev)

- [ ] 1.1 `internal/sessionmirror/` — record schema, append with ring buffer, pure `BuildHistory`
- [ ] 1.2 Extend `deskMirror.run` — fanout write after `readerModelInternal` (no second read)
- [ ] 1.3 Suppress path — no ledger append on firewall refuse (parity with Discord withhold)
- [ ] 1.4 XO/coordinator finish hook — same fanout for coordinator mirrors
- [ ] 1.5 Unit tests — info/debug/verbose derivation; suppress; retention cap
- [ ] 1.6 Integration test — Working→Idle → jsonl entry + Discord post unchanged

## 2. Dash session-mirror read path (flotilla-dev)

- [ ] 2.1 `GET /api/session-mirror?agent=&limit=` + readmodel builder
- [ ] 2.2 SSE `fileSigs` — poll `session-mirror/` mtimes
- [ ] 2.3 `FLOTILLA_DASH_MIRROR_VERBOSITY` env (`info`|`debug`)
- [ ] 2.4 Conversations thread — merge session-mirror + CoS ledger chronologically
- [ ] 2.5 Replace `renderReaderMapPlaceholder` stub with live session-mirror render
- [ ] 2.6 Unify backlog path — dash + watch same `backlog_file` roster key

## 3. Goals DAG core (flotilla-dev)

- [ ] 3.1 `internal/goals/` — YAML schema (`depends_on`, `conversation_agent`), load, acyclic
  validate, compile to JSON with `edges[]`
- [ ] 3.2 `fleet-goals.yaml` example in `flotilla.example.json` comment block (generic goals only;
  align with flotilla-dash `fleet-goals.example.yaml`)
- [ ] 3.3 Roll-up computation — authored+computed precedence (incl. `awaiting` from
  `[awaiting-auth]`), vacuous guard, inline/desk resolution
- [ ] 3.4 `GET /api/goals`, `GET /api/goals/{id}` (`GoalsDoc` with `edges[]`)
- [ ] 3.5 `flotilla goals compile|validate|link` CLI (minimal — validate + link first)
- [ ] 3.6 Issue `goal-id:` trailer parser in tracker read path
- [ ] 3.7 Unit tests — acyclic reject, roll-up blocked/awaiting/in-flight/achieved

## 4. Dash Goals UI (flotilla-dash desk — coordinate)

- [ ] 4.1 Goals tab in `index.html` navigation (parity with Conversations/Issues)
- [ ] 4.2 Tree navigation + goal detail panel (`goals.js` or extend `dash.js`)
- [ ] 4.3 Work item drill-in — issue link, backlog highlight
- [ ] 4.4 Optional graph visualization mode (follow-on polish)
- [ ] 4.5 Default landing tab switch (phase 2 — after coordinators populate graph)

## 5. Validation & docs

- [ ] 5.1 `go test ./internal/sessionmirror/... ./internal/goals/... ./internal/dash/... ./cmd/flotilla/...`
- [ ] 5.2 Runbook section in `docs/coordinator-seat-swap-runbook.md` or new `docs/goals-dag.md` (generic)
- [ ] 5.3 Archive spec deltas to `openspec/specs/` on implementation merge
- [ ] 5.4 Operator supervised trial — coordinators maintain goals for one project (operator gate)

**Out of scope for this change (tracked separately):** grok desk merge-deny enforcement and
private-boundary CI denylist wiring — flotilla#278 (deploy-security follow-up).

## Lane notes

- **Do not** duplicate read paths in flotilla-dash desk — consume flotilla-dev APIs.
- Coordinate before editing `cmd/flotilla/mirror.go` or `internal/dash/readmodel.go` across lanes.
- Pillar E `latest-delta.json` references in docs — update to session-mirror jsonl on D1 merge.
- `fleet-goals.example.yaml` on flotilla-dash `feat/dash-goals-map-267` is the live contract fixture;
  design MUST stay aligned (especially `depends_on`, `conversation_agent`).