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
- [x] 1.1 TEST FIRST (`internal/readermap/envelope_test.go`): a well-formed `{audience, anchor, delta,
  decision}` validates; a missing `decision` (neither an action nor `none`) is schema-invalid; an empty
  `anchor` is schema-invalid; an unrecognized `audience` value is ACCEPTED (open-stringly-typed); the
  known values `operator`/`desk:<name>`/`newcomer`/`maintainer` all validate.
- [x] 1.2 Implement the `Envelope` type + `Validate()` in a NEW `internal/readermap/` package (pure, no
  I/O — testable without tmux/Discord), with `audience` open-stringly-typed and `decision`
  present-or-`none`.
- [x] 1.3 TEST FIRST (`internal/readermap/detect_test.go`): the three-way DETECT predicate on a free-text
  turn-final string — a single parseable ```` ```reader-map ```` block ⇒ PRESENT+PARSEABLE (returns the
  envelope); a `reader-map` block that does not parse ⇒ MALFORMED; NO block ⇒ ABSENT (ordinary post); a
  SECOND `reader-map` block ⇒ MALFORMED. Missing and malformed are DISTINCT outcomes (key on block
  presence vs parse), so a no-brief turn is never conflated with a broken-brief turn.
- [x] 1.4 Implement `Detect(turnFinal string) (env *Envelope, outcome DetectOutcome)` — locate the
  fenced `reader-map` block, parse, classify PRESENT-PARSEABLE / MALFORMED / ABSENT (pure, deterministic).

### 2. Tier-1 structural lint (PRESENCE only) + render-from-fields (pure)
- [x] 2.1 TEST FIRST (`internal/readermap/lint_test.go`): tier-1 checks ONLY field presence/non-empty —
  a slop-but-present envelope (`anchor:"my work"`, `delta:"made progress"`, `decision:"none"`) PASSES
  tier-1 (presence satisfied — tier-1 cannot judge content; tier-2 catches the slop); an empty `anchor`
  or a missing `decision` FAILS; the lint returns a typed result PASS / PRESENCE-FAIL, with NO model
  call and NO fuzzy prose match.
- [x] 2.2 TEST FIRST (`internal/readermap/render_test.go`): the published body is RENDERED from the
  envelope fields in the fixed order `anchor` → `decision` → `delta`/body, so "opens from the reader's
  map, leads with the decision" holds BY CONSTRUCTION (assert the rendered body's prefix is the anchor
  and the decision precedes the delta) — NOT verified by matching desk prose.
- [x] 2.3 Implement `Tier1Lint(env Envelope) LintResult` (presence-only) + `Render(env Envelope) string`
  in `internal/readermap/` (pure, deterministic, no model call).

### 3. `flotilla brief <desk>` riding the shipped mirror (secret-free)
- [x] 3.1 TEST FIRST (`cmd/flotilla/brief_test.go`): `parseBriefArgs` accepts `<desk>` (+ `--roster`);
  `flotilla brief <desk>` injects a brief-REQUEST into the desk's pane (a `send`-class pane injection,
  secret-free — it MUST NOT read `$FLOTILLA_SECRETS` nor call `notify`); the brief request instructs the
  desk to emit the reader-map envelope block as its turn-final, and publication is the EXISTING mirror
  publishing the turn that CARRIES the envelope (assert the brief path does not introduce a second
  transport; the brief turn is correlated by the envelope block, not by "the next finish").
- [x] 3.2 TEST (SECRET-FREE invariant — about the DESK, not cmdBrief): the brief INJECTION never makes
  the desk hold secrets and never calls `notify` (the desk-forbidden path, `pushsnippet.go:29`) — the
  desk answers in-pane and the watch daemon's mirror (which holds secrets) publishes. cmdBrief is
  orchestrator-run, so it MAY read `--secrets` for the dark-desk pre-check (3.3); the invariant is the
  DESK-secret-free publish + no-notify, NOT that cmdBrief never touches secrets.
- [x] 3.3 TEST (DARK-DESK): when `--secrets` is provided, `flotilla brief` pre-checks each named desk's
  channel webhook resolves and REPORTS a desk with no resolvable webhook as dark (its brief cannot
  publish) at fan-out time — it does NOT return success while the brief silently never reaches a channel.
  Without `--secrets`, the pre-check is skipped with a note (the injection still proceeds).
- [x] 3.4 Implement `cmd/flotilla/brief.go` (`cmdBrief` + `parseBriefArgs` + the dark-desk pre-check);
  register `brief` in `cmd/flotilla/main.go`. Publication is the mirror, not a desk-invoked primitive.

### 4. The sync pre-post pipeline inside `deskMirror` (detect+validate + C-tier1, posture, pipeline-shape)
- [x] 4.1 TEST FIRST (`cmd/flotilla/mirror_test.go`): `deskMirror.run` runs detect → validate → tier-1
  BEFORE the post (inject a recording `post`); an enveloped brief that passes tier-1 posts (rendered
  body); a malformed envelope on the internal channel WARNS-and-posts + flags (never lost); an
  un-enveloped ordinary turn-final WARNS-and-posts un-flagged (today's behavior); EVERY outcome still
  emits exactly one decision log line (the mirror's existing invariant). On the mirror NOTHING is
  suppressed in P0 (the firewall, the only suppressing step, lands in P2).
- [x] 4.2 Implement the sync pre-post pipeline in `deskMirror.run` (`cmd/flotilla/mirror.go`) as an
  ORDERED LIST OF PRE-POST STAGES (a pipeline-shape slice), so P2's firewall registers as stage-1 and
  P3's ledger append registers as a post-post stage WITHOUT re-cutting `run`. P0 stages: detect →
  validate → tier-1, warn-with-publish on the internal-channel egress (the mirror has no public egress;
  only the firewall suppresses, and it is P2). Add a fourth log verb for the future SUPPRESS outcome.
  Preserve OBSERVE-ONLY/BEST-EFFORT.
- [x] 4.3 TEST + assert `deskMirrorOnFinish` (`cmd/flotilla/watch.go:890`) wiring is unchanged except for
  threading the pipeline (the `ResultReader`/webhook/`Post` collaborators stay identical; no new secret
  dependency).

### 5. P0 gate
- [x] 5.1 `openspec validate mechanical-reader-modeling --strict` green.
- [x] 5.2 `go build ./...` + `go test ./internal/readermap/... ./cmd/flotilla/...` green.
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

### 7. The firewall detector (refuse, never strip) + the advisory WARN tier + the git hook

Design-trio folded (P1/P2 findings): (a) **P2 OWNS the `<prefix>:<n>.<m>` pattern** — #202's pattern is
unbuilt, so "reuse #202's regex" is vacuous; P2 defines it as the canonical Go source and #202's static
guard MIRRORS it (a conformance test enforces equivalence). (b) **Share the DATA, not the CODE** — the
bash guard is PCRE (uses lookahead, `check-private-boundary.sh:41`) and Go is RE2 (no lookahead), so the
runtime + static guards CANNOT share regex code; they share the gitignored TERM LISTS and a conformance
test asserts identical verdicts. (c) **The P2 operator-visible signal is the ALERT-WEBHOOK line, NOT the
ledger** (the ledger is P3). (d) `Check` is PURE; the file/env I/O lives OUTSIDE `internal/readermap`.

- [ ] 7.1 TEST FIRST (`internal/readermap/firewall_test.go`, PURE — injected term-set, no I/O): a
  denylisted term → REFUSE; the `<prefix>:<n>.<m>` / `#<deployment>-c2` pattern with a NON-allowlisted
  prefix → REFUSE; an ALLOWLISTED generic prefix (`flotilla:3.1`, `session:1.2` — the precise allowlist
  MUST be enumerated, since `session:window.pane` is a legitimate generic tmux shape used across the tree,
  e.g. `recycle.go`, `internal/deliver/tmux.go`) → OK; a built-in generic leak (a non-allowlisted
  `/home/<user>` path, a webhook URL, a secret shape) → REFUSE; a refusal RETURNS the offending token +
  a generic abstraction and NEVER a rewritten body; a warnlist domain-vocab term (no denylist hit) → WARN
  (advisory); a term on BOTH lists → REFUSE (denylist precedence); an ABSENT/empty denylist+warnlist →
  only the generic patterns apply (no error, generic prose → OK — mirroring `check-private-boundary.sh`'s
  generic-always / deployment-only-if-configured model). Typed `FirewallResult{Decision: Refuse|Warn|OK,
  Token, Abstraction, WarnTerms}` (egress-agnostic; the SUPPRESS-vs-bounce rendering of a Refuse is the
  caller's job, 7.3).
- [ ] 7.2 Implement the PURE detector `Check(text, termset) FirewallResult` in
  `internal/readermap/firewall.go` (no I/O — preserves the package's pure/testable contract). Put the I/O
  loader `LoadFirewall()` in `cmd/flotilla` (or a new I/O-bearing package), reading the SAME gitignored
  sources the bash guard uses (`.flotilla/private-denylist` / `$FLOTILLA_PRIVATE_DENYLIST`; a NEW
  `.flotilla/private-warnlist` / `$FLOTILLA_PRIVATE_WARNLIST`) + the built-in generic patterns + the
  canonical `<prefix>:<n>.<m>` pattern (P2 owns it). NEVER hard-code deployment vocabulary. Refuse-bounce
  only, no rewrite. Compile the alternation ONCE at load (not per-check — the hot auto-mirror path). RE2
  cannot express the bash guard's negative-lookahead allowlist, so re-express it as match-then-filter
  against the enumerated generic-prefix/home-placeholder allowlist.
- [ ] 7.2b PARTITION (P1 — the trading-vocab leak class): add `/.flotilla/private-warnlist` to
  `.gitignore`; ship `.flotilla/private-warnlist.example` (illustrative placeholders only, mirroring
  `private-denylist.example`); add `.flotilla/private-warnlist.example` to `check-private-boundary.sh`'s
  `SELF_EXCLUDE`. The leakscan WARN fold MUST be hand-reviewed for vocab leakage — `check-private-boundary.sh`
  self-excludes itself from its own scan, so a domain term hard-coded INTO the script would not be caught.
- [ ] 7.3 Wire the firewall as STAGE 1 of `deskMirror.run`'s pre-post pipeline (the P0 suppress seam —
  on a Refuse set `suppress=true`; thread the daemon's existing `alert func(string)` (`watch.go:148`) into
  `deskMirror` (a NEW field, wired in `deskMirrorOnFinish`) and raise it on a Refuse → the ALERT-WEBHOOK
  line is the P2 operator-visible "withheld for a possible leak" signal, NOT the P3 ledger; a WARN raises
  the same advisory and STILL posts). The MANUAL `notify` CLI path: Refuse → bounce token+abstraction
  in-context, inserted as a pure pre-check AFTER `resolveMessage` and BEFORE `tr.Post` (`main.go`), so
  clean traffic is byte-identical. The `reply` path is the DAEMON reply-watcher (not a willing-to-wait
  CLI turn) — Refuse → SUPPRESS the route + `escalate(...)` (it already has an escalate collaborator),
  NOT bounce. Denylist limitation (CLAUDE.md §1): novel coined terms are not caught; the WARN tier
  narrows but does not close that gap.
- [ ] 7.4 The git **pre-push** hook (fail-closed) is the local backstop; **CI's `private-boundary` job is
  the enforcing authority** (a local hook is `--no-verify`-bypassable). The hook scans the push RANGE/diff
  (not the whole tree — `check-private-boundary.sh`'s `scan_tree` greps the tracked tree, so add a
  staged/range mode or scope the hook to the diff). Fold the advisory WARN tier into
  `check-private-boundary.sh` (a `private-warnlist` pass: WARN section + exit 0 — closing the #151
  domain-vocab class on the CI/issue scan). Document both in `docs/private-public-boundary.md`. A
  CONFORMANCE TEST feeds a shared fixture corpus through BOTH the Go firewall and the bash guard and fails
  on any verdict mismatch (the only real "never diverge" guarantee, given two regex engines).
- [x] 7.5 RESOLVED (COS, 2026-06-30): on the PUBLIC git/CI egress a warnlist hit is **exit-0 ADVISORY**
  (build it that way — it is a reversible flag). Reasoning: the WARN tier is high-false-positive by
  construction (e.g. "flattens"), so a human-ack gate would train reflexive rubber-stamping and defeat
  the guard when a real leak appears; the HARD tier (refuse+bounce) is the actual protection and always
  blocks; advisory keeps a visible review trail without friction. (Operator informed of the knob; a
  one-flag flip to hard-block-on-WARN is available if he wants it — do NOT hold impl for it.)

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
