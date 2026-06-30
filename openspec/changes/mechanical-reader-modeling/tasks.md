# Tasks — mechanical reader-modeling (TDD, phase-ordered)

Load-bearing properties (assert across paths):
- **(CRUX) structure ≠ modeling** — field PRESENCE is the deterministic tier-1 lint; field QUALITY is
  the tier-2 LLM judge. A slop envelope (`anchor:"my work"`, `decision:"none"`) passes tier-1 but the
  judge (tier-2) is what catches unmodeled content. The spec MUST NOT relabel the discipline as "a
  schema the desk fills."
- **(SEAM) the outbound publish already ships** — Pillar A rides `internal/watch/detector.go`'s
  `MirrorOnFinish` → `cmd/flotilla/mirror.go`'s `deskMirror.run` (the `surface.ResultReader` seam,
  wired at `cmd/flotilla/watch.go:890`). NOT `inject.go:SetMirror` (inbound audit). NOT `notify`.
- **(SECRET-FREE) a desk publishes a brief WITHOUT touching fleet secrets** — honoring
  `cmd/flotilla/pushsnippet.go:29` ("do NOT run flotilla notify … do NOT touch any secrets").
- **(POSTURE) never lose a brief to a lint** — PUBLIC git/GitHub = fail-closed; operator briefs +
  internal channels = warn-with-publish + flag; the firewall refuse is fail-closed on BOTH egresses.
- **(REFUSE) the firewall refuses, never strips** — a runtime strip corrupts the modeled delta.
- **(ORDER) the pipeline runs D → B → C-tier1 → post → ledger** on the runtime path; C-tier2 only on
  the CLI path; the git/GitHub path runs D + tier-1 as a hook.

---

## P0 — brief on the mirror + the envelope + tier-1 structural lint (A + B + C-tier1)

### 1. The reader-map envelope type + schema validation (pure)
- [ ] 1.1 TEST FIRST (`internal/readermap/envelope_test.go`): a well-formed `{audience, anchor, delta,
  decision}` validates; a missing `decision` (neither an action nor `none`) is schema-invalid; an empty
  `anchor` is schema-invalid; an unrecognized `audience` value is ACCEPTED (open-stringly-typed); the
  known values `operator`/`desk:<name>`/`newcomer`/`maintainer` all validate.
- [ ] 1.2 Implement the `Envelope` type + `Validate()` in a NEW `internal/readermap/` package (pure, no
  I/O — testable without tmux/Discord), with `audience` open-stringly-typed and `decision`
  present-or-`none`.

### 2. Tier-1 structural lint (deterministic, pure)
- [ ] 2.1 TEST FIRST (`internal/readermap/lint_test.go`): a body that opens with the `anchor` AND leads
  with the `decision` passes; a body that does neither fails; a slop envelope (`anchor:"my work"`,
  `decision:"none"`, body opening with author-state) FAILS tier-1 structurally with NO model call; the
  lint returns a typed result distinguishing PASS / STRUCTURAL-FAIL (it never judges content).
- [ ] 2.2 Implement `Tier1Lint(env Envelope, body string) LintResult` in `internal/readermap/` (pure,
  deterministic, no model call).

### 3. `flotilla brief <desk>` riding the shipped mirror (secret-free)
- [ ] 3.1 TEST FIRST (`cmd/flotilla/brief_test.go`): `parseBriefArgs` accepts `<desk>` (+ `--roster`);
  `flotilla brief <desk>` injects a brief-REQUEST into the desk's pane (a `send`-class pane injection,
  secret-free — it MUST NOT read `$FLOTILLA_SECRETS` nor call `notify`); the publish is the EXISTING
  mirror firing on the brief turn's finish (assert the brief path does not introduce a second transport).
- [ ] 3.2 TEST (SECRET-FREE): the `brief` command code path never loads secrets and never calls the
  notify path (assert by construction — `brief` takes only the roster, à la `buildPushSnippet`
  `pushsnippet.go:71`).
- [ ] 3.3 Implement `cmd/flotilla/brief.go` (`cmdBrief` + `parseBriefArgs`); register `brief` in
  `cmd/flotilla/main.go`. The brief request instructs the desk to emit an enveloped brief as its
  turn-final (structured output); publication is the mirror, not a desk-invoked primitive.

### 4. The sync pre-post pipeline inside `deskMirror` (B-validate + C-tier1, posture)
- [ ] 4.1 TEST FIRST (`cmd/flotilla/mirror_test.go`): `deskMirror.run` runs envelope-validate + tier-1
  BEFORE the post (inject a recording `post`); an enveloped brief that passes tier-1 posts; a malformed
  envelope on an internal channel WARNS-and-posts (back-compat, never lost) + logs; an un-enveloped
  ordinary turn-final WARNS-and-posts (today's behavior); EVERY outcome still emits exactly one decision
  log line (the mirror's existing invariant).
- [ ] 4.2 Implement the sync pre-post pipeline hook in `deskMirror.run` (`cmd/flotilla/mirror.go`):
  envelope-detect → validate → tier-1, applying the warn-with-publish posture for the internal channel
  egress (the auto-mirror is internal — never fail-closed on a missing envelope; only the firewall
  (P2) suppresses on this path). Preserve OBSERVE-ONLY/BEST-EFFORT for everything that does not
  fail-closed.
- [ ] 4.3 TEST + assert `deskMirrorOnFinish` (`cmd/flotilla/watch.go:890`) wiring is unchanged except for
  threading the pipeline (the `ResultReader`/webhook/`Post` collaborators stay identical; no new secret
  dependency).

### 5. P0 gate
- [ ] 5.1 `openspec validate mechanical-reader-modeling --strict` green.
- [ ] 5.2 `go build ./...` + `go test ./internal/readermap/... ./cmd/flotilla/...` green.
- [ ] 5.3 Manual proof: `flotilla brief <desk>` yields a published Discord brief from the desk's channel,
  secret-free (the #207 fraction-of-desks → every-desk proof).

---

## P1 — the semantic judge + templates (C-tier2)

### 6. The LLM reader-model judge on the CLI path
- [ ] 6.1 TEST FIRST (`internal/readermap/judge_test.go`, with a fake judge backend): the judge reads AS
  the named `audience`; a content-but-unmodeled artifact (wrong anchor / does-not-stand-alone) is judged
  FAIL; a modeled one is judged PASS; the judge is INVOKED only on the CLI path, never in the auto-mirror
  (assert the auto-mirror path does not call the judge).
- [ ] 6.2 Implement the judge SPI + a backend; wire it on the explicit `brief`/`notify` CLI path BEFORE
  the hand-off to the mirror. Posture: PUBLIC git/GitHub = fail-closed; operator briefs + internal
  channels = warn-with-publish + flag.
- [ ] 6.3 Per-audience templates (operator / desk / newcomer / maintainer) that force open-from-the-map
  + lead-with-the-decision in the brief request (the structure that makes the judge's job checkable).

---

## P2 — the runtime firewall refuse (D) + the git hook

### 7. The firewall detector (reuses #202's regex) — refuse, never strip
- [ ] 7.1 TEST FIRST (`internal/readermap/firewall_test.go`): an artifact with a denylisted deployment
  term is REFUSED; one with the #202 `<prefix>:<n>.<m>` / `#<deployment>-c2` pattern (non-generic
  prefix) is REFUSED; a generic-prefix (`flotilla:3.1`) passes; the refusal RETURNS the offending token
  + a generic abstraction (it NEVER returns a rewritten body).
- [ ] 7.2 Implement the firewall detector reusing the static guard's denylist + #202's regex (shared
  source where feasible, so the runtime + static guards never diverge); refuse-bounce only, no rewrite.
- [ ] 7.3 Wire the firewall as STEP 1 of the runtime pipeline in `deskMirror.run` (fail-closed: suppress
  + log on the auto-mirror; bounce the token + abstraction on the CLI path) and on the `notify`/`reply`
  CLI paths.
- [ ] 7.4 The git pre-commit/pre-push hook running the firewall + tier-1 on issue/PR/commit artifacts off
  the runtime path; document it in `docs/private-public-boundary.md`.

---

## P3 — the dash map view (E)

### 8. The per-desk envelope ledger + the dash render
- [ ] 8.1 TEST FIRST: the publish path appends each published envelope to a per-desk `latest-delta.json`
  (alongside the CoS ledger); a torn/absent ledger reads as empty (fail-safe, the `readFileOrEmpty`
  contract).
- [ ] 8.2 Implement the ledger writer in the publish path + the envelope-extended `HistoryDoc` read
  model.
- [ ] 8.3 The dash render: per desk, the latest `anchor`→`delta` + any pending `decision`, pulled not
  pushed (auth-gated by #208 on its merge). #210's full manage-conversations UX stays #210's.

---

## Review + ship (per phase)
- [ ] 9.1 Implementation-trio (systems-review + open-code-review + STORM, parallel, read-only git) on
  each phase's diff; fold findings; iterate clean.
- [ ] 9.2 PARTITION grep clean (`git grep -niE "\bspark\b|/home/jim|general-ml|#spark-c2|ramtank|databento"`
  → zero) before each PR; `openspec validate --all --strict` green; hand the PR to the operator's gate
  (no self-merge).
- [ ] 9.3 Record the deferred follow-ups (P1 judge, P2 firewall + hook, P3 dash, #202 static PR, #210
  UX) as the cross-references they already are (#207 subsumed, #210 builds-on, #202 complements).
