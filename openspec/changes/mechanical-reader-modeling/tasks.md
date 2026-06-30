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

### 1. The reader-map envelope type + schema validation + wire-format/detect (pure)
- [ ] 1.1 TEST FIRST (`internal/readermap/envelope_test.go`): a well-formed `{audience, anchor, delta,
  decision}` validates; a missing `decision` (neither an action nor `none`) is schema-invalid; an empty
  `anchor` is schema-invalid; an unrecognized `audience` value is ACCEPTED (open-stringly-typed); the
  known values `operator`/`desk:<name>`/`newcomer`/`maintainer` all validate.
- [ ] 1.2 Implement the `Envelope` type + `Validate()` in a NEW `internal/readermap/` package (pure, no
  I/O — testable without tmux/Discord), with `audience` open-stringly-typed and `decision`
  present-or-`none`.
- [ ] 1.3 TEST FIRST (`internal/readermap/detect_test.go`): the three-way DETECT predicate on a free-text
  turn-final string — a single parseable ```` ```reader-map ```` block ⇒ PRESENT+PARSEABLE (returns the
  envelope); a `reader-map` block that does not parse ⇒ MALFORMED; NO block ⇒ ABSENT (ordinary post); a
  SECOND `reader-map` block ⇒ MALFORMED. Missing and malformed are DISTINCT outcomes (key on block
  presence vs parse), so a no-brief turn is never conflated with a broken-brief turn.
- [ ] 1.4 Implement `Detect(turnFinal string) (env *Envelope, outcome DetectOutcome)` — locate the
  fenced `reader-map` block, parse, classify PRESENT-PARSEABLE / MALFORMED / ABSENT (pure, deterministic).

### 2. Tier-1 structural lint (PRESENCE only) + render-from-fields (pure)
- [ ] 2.1 TEST FIRST (`internal/readermap/lint_test.go`): tier-1 checks ONLY field presence/non-empty —
  a slop-but-present envelope (`anchor:"my work"`, `delta:"made progress"`, `decision:"none"`) PASSES
  tier-1 (presence satisfied — tier-1 cannot judge content; tier-2 catches the slop); an empty `anchor`
  or a missing `decision` FAILS; the lint returns a typed result PASS / PRESENCE-FAIL, with NO model
  call and NO fuzzy prose match.
- [ ] 2.2 TEST FIRST (`internal/readermap/render_test.go`): the published body is RENDERED from the
  envelope fields in the fixed order `anchor` → `decision` → `delta`/body, so "opens from the reader's
  map, leads with the decision" holds BY CONSTRUCTION (assert the rendered body's prefix is the anchor
  and the decision precedes the delta) — NOT verified by matching desk prose.
- [ ] 2.3 Implement `Tier1Lint(env Envelope) LintResult` (presence-only) + `Render(env Envelope) string`
  in `internal/readermap/` (pure, deterministic, no model call).

### 3. `flotilla brief <desk>` riding the shipped mirror (secret-free)
- [ ] 3.1 TEST FIRST (`cmd/flotilla/brief_test.go`): `parseBriefArgs` accepts `<desk>` (+ `--roster`);
  `flotilla brief <desk>` injects a brief-REQUEST into the desk's pane (a `send`-class pane injection,
  secret-free — it MUST NOT read `$FLOTILLA_SECRETS` nor call `notify`); the brief request instructs the
  desk to emit the reader-map envelope block as its turn-final, and publication is the EXISTING mirror
  publishing the turn that CARRIES the envelope (assert the brief path does not introduce a second
  transport; the brief turn is correlated by the envelope block, not by "the next finish").
- [ ] 3.2 TEST (SECRET-FREE): the `brief` command code path never loads secrets and never calls the
  notify path (assert by construction — `brief` takes only the roster, à la `buildPushSnippet`
  `pushsnippet.go:71`).
- [ ] 3.3 TEST (DARK-DESK): `flotilla brief` pre-checks each named desk's channel webhook resolves and
  REPORTS a desk with no resolvable webhook as dark (its brief cannot publish) at fan-out time — it does
  NOT return success while the brief silently never reaches a channel.
- [ ] 3.4 Implement `cmd/flotilla/brief.go` (`cmdBrief` + `parseBriefArgs` + the dark-desk pre-check);
  register `brief` in `cmd/flotilla/main.go`. Publication is the mirror, not a desk-invoked primitive.

### 4. The sync pre-post pipeline inside `deskMirror` (detect+validate + C-tier1, posture, pipeline-shape)
- [ ] 4.1 TEST FIRST (`cmd/flotilla/mirror_test.go`): `deskMirror.run` runs detect → validate → tier-1
  BEFORE the post (inject a recording `post`); an enveloped brief that passes tier-1 posts (rendered
  body); a malformed envelope on the internal channel WARNS-and-posts + flags (never lost); an
  un-enveloped ordinary turn-final WARNS-and-posts un-flagged (today's behavior); EVERY outcome still
  emits exactly one decision log line (the mirror's existing invariant). On the mirror NOTHING is
  suppressed in P0 (the firewall, the only suppressing step, lands in P2).
- [ ] 4.2 Implement the sync pre-post pipeline in `deskMirror.run` (`cmd/flotilla/mirror.go`) as an
  ORDERED LIST OF PRE-POST STAGES (a pipeline-shape slice), so P2's firewall registers as stage-1 and
  P3's ledger append registers as a post-post stage WITHOUT re-cutting `run`. P0 stages: detect →
  validate → tier-1, warn-with-publish on the internal-channel egress (the mirror has no public egress;
  only the firewall suppresses, and it is P2). Add a fourth log verb for the future SUPPRESS outcome.
  Preserve OBSERVE-ONLY/BEST-EFFORT.
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

### 6. The LLM reader-model judge where the publisher holds the body synchronously
- [ ] 6.1 TEST FIRST (`internal/readermap/judge_test.go`, with a fake judge backend): the judge reads AS
  the named `audience`; a content-but-unmodeled artifact (wrong anchor / does-not-stand-alone) is judged
  FAIL; a modeled one is judged PASS; the judge is INVOKED only where the body is held synchronously
  (`notify`/`reply` + the git/GitHub hook), NEVER synchronously in the auto-mirror (assert the
  auto-mirror path does not call the judge in-line).
- [ ] 6.2 Implement the judge SPI + a backend; wire it on the `notify`/`reply` CLI path (caller-supplied
  body) and the git/GitHub hook (P2). Posture: PUBLIC git/GitHub = fail-closed; operator briefs +
  internal channels = warn-with-publish + flag. NOTE: a mirror-published `brief` body is produced async
  in-pane — the synchronous judge does NOT gate it; an OPTIONAL post-publish judge MAY flag it in the
  ledger (warn, never block — never lose a brief). The brief's quality is the desk's structured-output
  authoring + tier-1 + render.
- [ ] 6.3 Per-audience templates (operator / desk / newcomer / maintainer) that force open-from-the-map
  + lead-with-the-decision in the brief request (the structure the desk authors into the envelope, which
  the render then guarantees).

---

## P2 — the runtime firewall refuse (D) + the git hook

### 7. The firewall detector (reuses #202's regex) — refuse, never strip
- [ ] 7.1 TEST FIRST (`internal/readermap/firewall_test.go`): an artifact with a denylisted deployment
  term is REFUSED; one with the #202 `<prefix>:<n>.<m>` / `#<deployment>-c2` pattern (non-generic
  prefix) is REFUSED; a generic-prefix (`flotilla:3.1`) passes; the refusal RETURNS the offending token
  + a generic abstraction (it NEVER returns a rewritten body).
- [ ] 7.2 Implement the firewall detector reusing the static guard's denylist + #202's regex (shared
  source where feasible, so the runtime + static guards never diverge); refuse-bounce only, no rewrite.
- [ ] 7.3 Wire the firewall as STEP 1 of the runtime pipeline in `deskMirror.run` (register it on the
  P0 pipeline-shape slice — fail-closed: suppress + log + raise an operator-visible signal (flagged
  ledger entry and/or alert-webhook line) on the auto-mirror; bounce the token + abstraction in-context
  on the CLI path) and on the `notify`/`reply` CLI paths (the only behavior added to `notify` — clean
  traffic stays byte-identical). Note the denylist limitation (CLAUDE.md §1): novel coined terms are not
  caught — the firewall is the backstop, not a guarantee.
- [ ] 7.4 The git pre-commit/pre-push hook running the firewall + tier-1 on issue/PR/commit artifacts off
  the runtime path; document it in `docs/private-public-boundary.md`.

---

## P3 — the dash map view (E)

### 8. The per-desk envelope ledger + the dash render
- [ ] 8.1 TEST FIRST: the publish path writes the LATEST published envelope per desk to `latest-delta.json`
  (alongside the CoS ledger) ATOMICALLY (temp + rename); each entry carries a TIMESTAMP; the write is an
  atomic overwrite of the latest record (NOT an unbounded append log — the name + the dash read are
  "latest"); a torn/absent ledger reads as empty (fail-safe, the `readFileOrEmpty` contract).
- [ ] 8.2 Implement the atomic ledger writer (register it as a post-post stage on the pipeline-shape
  slice from task 4.2) + the timestamped, envelope-extended `HistoryDoc` read model.
- [ ] 8.3 The dash render: per desk, the latest `anchor`→`delta`, its AGE from the timestamp (a stale
  delta shown as stale, never as current — the verify-stale-status discipline), + any pending `decision`
  (which CLEARS when the desk publishes a newer envelope with `decision:"none"` — a resolved decision
  never lingers as falsely pending), pulled not pushed (auth-gated by #208 on its merge). #210's full
  manage-conversations UX stays #210's.

---

## Review + ship (per phase)
- [ ] 9.1 Implementation-trio (systems-review + open-code-review + STORM, parallel, read-only git) on
  each phase's diff; fold findings; iterate clean.
- [ ] 9.2 PARTITION clean before each PR: run `scripts/check-private-boundary.sh` (it loads the
  gitignored deployment denylist — the live deployment vocabulary is NEVER hard-coded into a committed
  file, per the script's own contract and CLAUDE.md §1) → exit 0; `openspec validate --all --strict`
  green; hand the PR to the operator's gate (no self-merge).
- [ ] 9.3 Record the deferred follow-ups (P1 judge, P2 firewall + hook, P3 dash, #202 static PR, #210
  UX) as the cross-references they already are (#207 subsumed, #210 builds-on, #202 complements).
