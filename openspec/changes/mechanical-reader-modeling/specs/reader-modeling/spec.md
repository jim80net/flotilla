# reader-modeling Specification (delta)

## ADDED Requirements

### Requirement: `flotilla brief` publishes a desk's reader-modeled brief deterministically via the shipped mirror

The system SHALL provide `flotilla brief <desk>` — a single operation that elicits a reader-modeled
brief from a desk and publishes it to that desk's own Discord channel. The brief SHALL be published by
the EXISTING per-desk mirror (`internal/watch/detector.go`'s `MirrorOnFinish` → `cmd/flotilla/mirror.go`'s
`deskMirror.run`, fed the turn-final via the `surface.ResultReader` seam wired in `deskMirrorOnFinish`,
`cmd/flotilla/watch.go:890`), NOT by `flotilla notify` and NOT by a new transport. The desk SHALL never
touch fleet secrets to publish a brief (honoring the smart-desk secret-free invariant trained by
`cmd/flotilla/pushsnippet.go:29`). `flotilla brief <desk>` SHALL inject a brief-request into the desk's
pane (a `send`-class injection); the desk SHALL respond by emitting an enveloped brief (the reader-map
envelope, below) as its turn-final, and the mirror publishes the turn that CARRIES the envelope.
Determinism therefore means: a desk publishes WITHOUT calling a forbidden primitive (`notify`) or
touching a secret — NOT that any arbitrary subsequent turn is the brief. Because the mirror fires on
turn-finish, the brief turn-final SHALL be CORRELATED to the brief by the presence of the envelope
marker (below), so an unrelated intervening turn is published as an ordinary (un-enveloped) turn-final
and is NOT mistaken for the brief. The scope of `flotilla brief` is the desk's **channel** surface; the
raw pane surface is explicitly out of scope.

`flotilla brief` SHALL pre-check, at fan-out time, that each named desk's channel webhook resolves, and
SHALL REPORT any desk with no resolvable webhook as a "dark" desk (its brief cannot be published) —
rather than returning success while the desk's brief silently never reaches a channel (the unconfigured-
webhook re-skin of the #207 failure).

#### Scenario: A brief call yields a published Discord brief, secret-free

- **WHEN** `flotilla brief <desk>` runs against a desk whose channel webhook is configured
- **THEN** the desk produces an enveloped brief, the brief's turn-final is published to the desk's
  channel under the desk's own webhook identity via the existing mirror path, and the desk never invokes
  `flotilla notify` nor reads any fleet secret

#### Scenario: The brief turn is correlated by the envelope, not by "next finish"

- **WHEN** a desk emits an unrelated intervening turn before its brief response
- **THEN** the intervening turn is published as an ordinary un-enveloped turn-final (the back-compat
  branch) and only the turn carrying the reader-map envelope is treated as the brief

#### Scenario: A dark desk is reported at fan-out, not silently dropped

- **WHEN** `flotilla brief` is fanned out and a named desk has no resolvable channel webhook
- **THEN** the command reports that desk as dark (its brief cannot be published) rather than returning
  success while the brief silently never reaches a channel

#### Scenario: A fleet-wide brief fan-out reaches every configured desk's channel

- **WHEN** `flotilla brief` is fanned out to every non-XO desk
- **THEN** every desk with a configured channel webhook publishes its brief (the #207 failure — only a
  fraction published because the fan-out relied on each desk translating a free-text request into the
  forbidden `notify` — does not recur), and any dark desk is named

### Requirement: Every published artifact carries a reader-map delta envelope

Every artifact published on the reader-modeling path SHALL carry a structured **reader-map delta
envelope** with the fields `audience`, `anchor`, `delta`, and `decision`. `audience` SHALL be
open-stringly-typed (the known values are `operator`, `desk:<name>`, `newcomer`, `maintainer`; an
unrecognized value SHALL be accepted, not rejected, so the audience set is extensible without a schema
change). `decision` SHALL be either the one action the reader must take, or the explicit string `none`.
The envelope SHALL be authored by the **desk's own structured output** — i.e. the desk's LLM exercises
the reader-modeling judgment at authoring time — and the publish path SHALL VALIDATE the schema (it
SHALL NOT derive the anchor or decision itself, because lint-derivation cannot manufacture that
judgment). The envelope SHALL be the I/O contract between the author and the tier-2 judge, and the
uniform data the dash ledger renders.

#### Scenario: A brief carries a well-formed envelope

- **WHEN** a desk emits a brief with `{audience, anchor, delta, decision}` all present and well-formed
- **THEN** the publish path validates the schema and proceeds to publish

#### Scenario: A missing decision field fails validation

- **WHEN** an enveloped artifact omits `decision` (neither an action nor the explicit `none`)
- **THEN** the envelope is schema-invalid and is handled by the lint posture for its egress (fail-closed
  for a public artifact; warn-with-publish + flag for an internal channel post)

#### Scenario: An unrecognized audience value is accepted

- **WHEN** an envelope carries an `audience` outside the known set (an extension value)
- **THEN** the envelope is accepted as schema-valid (the audience field is open-stringly-typed), not
  rejected

### Requirement: The envelope is carried in a labeled fenced block detectable in a free-text turn-final

The reader-map envelope SHALL be carried as a single fenced code block whose info-string is `reader-map`
(a Markdown fenced block opened with a `reader-map` tag) containing the envelope JSON, emitted by the
desk as part of its turn-final — because the mirror's only input is a free-text turn-final (the
`surface.ResultReader` `LatestResult(pane)` string), the envelope MUST be locatable and parseable inside
ordinary prose. The publish path SHALL define a deterministic three-way DETECT predicate on the
turn-final text:

- **a parseable `reader-map` block is present** → the turn-final is an enveloped artifact; validate the
  schema + run the pipeline;
- **a `reader-map` block is present but does NOT parse** (malformed) → the artifact is MALFORMED (a
  trivially-fixable structural defect), handled fail-closed per the malformed-envelope posture;
- **no `reader-map` block is present** → the turn-final is an ordinary (un-enveloped) post, handled by
  the back-compat warn-and-publish branch.

"Missing" (no block) and "malformed" (block present, unparseable) SHALL be DISTINCT outcomes — the
detect predicate keys on block PRESENCE, the validity outcome on whether it PARSES — so the posture
never conflates a desk that simply did not emit a brief with one that emitted a broken one. A turn-final
MAY carry at most one `reader-map` block; a second block SHALL be a malformed outcome.

#### Scenario: A present, parseable block is detected as an enveloped artifact

- **WHEN** a turn-final contains a single parseable `reader-map`-tagged fenced block
- **THEN** the artifact is treated as enveloped and run through the validate + lint pipeline

#### Scenario: A present-but-unparseable block is malformed, not missing

- **WHEN** a turn-final contains a `reader-map` block that does not parse as the envelope schema
- **THEN** the outcome is MALFORMED (handled fail-closed per the malformed posture), distinct from the
  no-block back-compat branch

#### Scenario: No block is the back-compat ordinary-post branch

- **WHEN** a turn-final contains no `reader-map` block
- **THEN** it is an ordinary un-enveloped post (warn-and-publish on an internal channel; fail-closed on
  a public artifact), never treated as a malformed envelope

### Requirement: Tier-1 structural lint runs synchronously inside the mirror before the post

The publish path SHALL enforce a deterministic **tier-1 structural lint** with NO model call, checking
ONLY field PRESENCE and non-emptiness: the envelope schema is valid, `anchor` is non-empty, and
`decision` is present (an action or the explicit `none`). The "open from the reader's map, lead with the
decision" SHAPE SHALL be guaranteed BY CONSTRUCTION — the published body is RENDERED from the envelope
fields in a fixed order (`anchor` → `decision` → `delta`/body) — NOT verified by a fuzzy string match.
This keeps tier-1 purely deterministic and prevents it from smuggling in the content judgment the design
assigns to tier-2: tier-1 cannot tell a real anchor from a slop one (`anchor:"my work"` is a present,
non-empty string and PASSES tier-1) — that is exactly what the tier-2 judge exists to catch. Tier-1
SHALL run SYNCHRONOUSLY inside the mirror pipeline BEFORE the post (a Discord message cannot be
un-sent) — the same synchronous pre-post slot the fail-closed firewall (D) occupies. On the Discord
runtime mirror, which has NO public egress (every mirror post is an internal channel), tier-1 SHALL be
**warn-with-publish** — a structurally-deficient brief is flagged but never lost. Tier-1 SHALL
**fail-closed** only on the git/GitHub static hook path (public artifacts), where a missing/empty field
blocks the artifact.

#### Scenario: A slop-but-present envelope PASSES tier-1 (and is caught by tier-2)

- **WHEN** an enveloped artifact has `anchor:"my work"`, `delta:"made progress"`, `decision:"none"` —
  all present and non-empty
- **THEN** tier-1 PASSES it (presence is satisfied) — tier-1 cannot judge content; the tier-2 semantic
  judge is what catches the unmodeled anchor (structure ≠ modeling)

#### Scenario: A missing field warns-with-publish on the mirror, fail-closes on a public artifact

- **WHEN** an enveloped artifact omits a required field (e.g. an empty `anchor`)
- **THEN** on the Discord runtime mirror it is warned-and-published (never lost — there is no public
  egress there), and on the git/GitHub hook path the public artifact is fail-closed (blocked)

#### Scenario: The body opens with the anchor by construction, not by a string match

- **WHEN** an enveloped brief is published
- **THEN** its body is rendered from the envelope fields in the fixed `anchor` → `decision` → `delta`
  order, so "opens from the reader's map, leads with the decision" holds structurally — tier-1 does not
  fuzzy-match prose to verify it

### Requirement: Tier-2 semantic judge runs only on the willing-to-wait CLI path

The publish path SHALL provide a **tier-2 semantic judge** — an LLM reading the artifact AS the named
`audience` and assessing whether the `anchor` is really that reader's map entry, whether the body opens
from the reader's map, and whether the artifact stands alone cold. The judge attaches ONLY where the
PUBLISHER HOLDS THE BODY SYNCHRONOUSLY and is willing to wait: the `notify`/`reply` CLI (the caller
supplies the body) and the git/GitHub pre-commit/pre-push hook (the artifact exists before the commit).
The judge SHALL NEVER run in the best-effort auto-mirror (a slow judge would stall or be skipped).
Because a `flotilla brief` body is produced asynchronously IN THE PANE and published by the auto-mirror
(not held by the `brief` CLI call), the synchronous tier-2 judge does NOT gate a mirror-published brief
in-line; a mirror brief's quality is supplied by the desk's structured-output AUTHORING (Pillar B — the
desk's own LLM does the modeling) plus the tier-1 presence check and the render-from-fields shape, and
the judge MAY flag it POST-publish in the envelope ledger (a warn/flag, never a block — never lose a
brief). The judge SHALL cost a model call and SHALL be skippable without blocking any internal-channel
post.

#### Scenario: A content-but-unmodeled public artifact fails the judge

- **WHEN** an artifact bound for a PUBLIC git/GitHub egress (via the hook) passes tier-1 structurally but
  the tier-2 judge, reading as the audience, finds it written from the author's internal state (wrong
  anchor, does not stand alone)
- **THEN** the artifact is fail-closed (blocked) on the public egress

#### Scenario: The judge never blocks a mirror-published brief in-line

- **WHEN** a `flotilla brief` body is produced in-pane and published by the auto-mirror
- **THEN** the synchronous tier-2 judge does not gate it in-line (the brief is shaped by authoring +
  tier-1 + render); the judge may flag it post-publish in the ledger, but the brief is never blocked

#### Scenario: The judge never runs in the auto-mirror

- **WHEN** any turn-final is published by the best-effort auto-mirror
- **THEN** the tier-2 semantic judge does not run synchronously on that path (only the firewall + envelope
  detect/validate + tier-1 run there)

### Requirement: The lint posture is fail-closed public, warn-with-publish for briefs and internal channels

The reader-modeling path SHALL apply this posture so the operator NEVER loses a brief to a lint. The
posture is keyed on the EGRESS, not on the failure kind — the firewall refuse (D) is the sole exception
(fail-closed on both egresses; see the firewall requirement):

- **PUBLIC git/GitHub artifacts** (issues/PRs/commits, via the static hook) SHALL be **fail-closed** —
  any lint failure (a missing required field, a malformed envelope, or a tier-2 judge failure) blocks
  the artifact. Latency is acceptable there.
- **Operator briefs + internal Discord channel posts** (the runtime mirror) SHALL be
  **warn-with-publish + flag** — the post is ALWAYS delivered and the lint failure is recorded and
  surfaced, never dropped. This holds for a malformed envelope AND for a tier-2 judge failure alike: on
  the mirror there is no public egress, so a lint never blocks a brief.
- The missing-vs-malformed distinction (from the detect predicate) governs the FLAG, not the
  publish/block decision on the mirror: a malformed envelope is flagged as a fixable structural defect;
  an absent envelope on an ordinary turn-final is the silent back-compat branch (no flag) — both still
  publish on the mirror.

The only mechanical block on the runtime mirror is the firewall refuse (a private leak); a lint never
blocks there. This is the deliberate trade: never-lose-a-brief outranks never-publish-a-deficient-brief.

#### Scenario: An operator brief that fails a lint is still published

- **WHEN** an operator brief or internal channel post fails the tier-2 judge or carries a malformed
  envelope
- **THEN** the post is still published on the mirror, and the lint failure is recorded and flagged — the
  brief is never lost to the lint (only a firewall leak refusal can withhold it)

#### Scenario: A public artifact that fails a lint is blocked

- **WHEN** a public git/GitHub artifact (via the static hook) fails any lint tier or carries a malformed
  envelope
- **THEN** the artifact is fail-closed (blocked) until the failure is fixed

### Requirement: The firewall REFUSES a private leak, never strips it

The publish path SHALL run every outbound artifact through the private-firewall detector (the static
guard's deployment denylist plus #202's `<prefix>:<n>.<m>` / `#<deployment>-c2` pattern). On a hit it
SHALL **REFUSE** the artifact and bounce to the desk the offending token together with its generic
abstraction as a suggestion the desk applies in-context — it SHALL NEVER silently rewrite the artifact.
A runtime strip is FORBIDDEN because generalizing a deployment specific inside a sentence whose meaning
depends on it would corrupt the modeled delta the operator's map ingests — worse than a refusal. The
firewall refuse SHALL be fail-closed on BOTH egresses (Discord runtime + git/GitHub static): a
**known-denylist** leak is never published. The firewall is a DENYLIST and inherits its limitation
(CLAUDE.md §1): it catches enumerated terms + the #202 pattern, but it does NOT catch a novel deployment
term a desk coins — so the firewall is the backstop, not a guarantee of airtightness, and the spec does
not over-claim that ALL leaks are caught. On the auto-mirror (no interactive desk to bounce to mid-turn)
a hit SHALL SUPPRESS the post AND raise an operator-visible signal — a flagged "withheld for a possible
leak" entry in the envelope ledger (E) and/or an alert-webhook line — so a withheld brief does not
vanish into a journald line no human reads; on the CLI path the desk is bounced the token + abstraction
in-context. Either way the mirror's one-decision-log-line invariant is preserved (a SUPPRESS is logged).
This firewall REUSES #202's regex at runtime egress (shared source where feasible, so the runtime and
static guards never diverge) and does NOT subsume #202 (which ships as its own static guard PR for
committed fixtures that never traverse the publish path).

#### Scenario: An artifact carrying a deployment specific is refused, not rewritten

- **WHEN** an outbound artifact on the publish path contains a private deployment specific (a denylisted
  term or the #202 session:window.pane pattern)
- **THEN** the artifact is REFUSED and never published, the desk is given the offending token and its
  generic abstraction as an in-context suggestion, and the artifact is NEVER silently rewritten

#### Scenario: The auto-mirror suppresses a leaking post and surfaces it to the operator

- **WHEN** an ordinary auto-mirror turn-final contains a known-denylist deployment specific
- **THEN** the post is suppressed (the leak is never published), the suppression is logged on the
  mirror's one decision line, AND an operator-visible signal is raised (a flagged "withheld" ledger
  entry and/or an alert-webhook line) so the withheld turn-final does not vanish silently

#### Scenario: A novel coined term is NOT caught by the denylist firewall

- **WHEN** a turn-final carries a deployment specific that is NOT an enumerated denylist term and does
  not match the #202 pattern
- **THEN** the firewall does not catch it (the denylist limitation, CLAUDE.md §1) — the firewall is the
  backstop, not a guarantee; the partition remains the desk's responsibility

#### Scenario: A committed fixture leak is caught by the static guard, separately

- **WHEN** a private specific is committed into a fixture (which never traverses the publish path)
- **THEN** it is caught by the static `scripts/check-private-boundary.sh` / #202's static guard, not by
  the runtime firewall (the two egresses are guarded separately)

### Requirement: The publish pipeline runs in a fixed order on the runtime path

On the Discord runtime path the publish pipeline SHALL run in this fixed order: **(1) firewall
refuse-check** → **(2) envelope validate** → **(3) tier-1 structural lint (synchronous, pre-post)** →
**(4) post via the mirror** → **(5) record to the envelope ledger**. The firewall SHALL run FIRST so no
modeling work is wasted on an artifact that will be refused. The tier-2 semantic judge SHALL run only on
the explicit CLI path before it hands off to the mirror, never in the best-effort auto-mirror. The
git/GitHub static path SHALL run the firewall + the tier-1 structural lint as a pre-commit/pre-push
hook.

#### Scenario: The firewall runs before any modeling work

- **WHEN** an artifact that would also fail the envelope/lint contains a private leak
- **THEN** the firewall refuses it FIRST, before the envelope validate or the lint runs (no wasted
  modeling work)

#### Scenario: The git/GitHub path is guarded by a hook running firewall + structural lint

- **WHEN** a desk authors an issue/PR/commit via `gh`/`git` (off the Discord runtime path)
- **THEN** a pre-commit/pre-push hook runs the firewall + the tier-1 structural lint on that artifact (it
  does not traverse the runtime mirror pipeline)

### Requirement: A per-desk envelope ledger is the dash's read model

The publish path SHALL maintain a new **per-desk envelope ledger** (`latest-delta.json` per desk),
written alongside the existing CoS ledger, recording the latest published envelope per desk. Each ledger
entry SHALL carry a **timestamp** (when the envelope was published). The ledger SHALL be written
ATOMICALLY (temp file + rename), so the dash's `readFileOrEmpty` torn-read-as-empty fallback is the rare
exception, not the steady state. The file holds the LATEST record per desk (an atomic overwrite of the
latest envelope), consistent with its name — it is NOT an unbounded append log (the dash renders "the
latest," so a growing log would contradict the name and the read). The dash SHALL read it via the
existing pure-reader-over-files pattern (`readFileOrEmpty` → an envelope-extended `HistoryDoc`) and
render, per desk, the latest `anchor`→`delta`, its **age** (from the timestamp — a stale delta SHALL be
shown as stale, never as current state, per the operator's verify-stale-status discipline), and any
**pending** `decision` — glanceable, *pulled* not pushed (it SHALL NOT introduce a new live surface to
babysit). A `decision` SHALL clear when the desk publishes a newer envelope whose `decision` is `none`
(the dash renders the LATEST envelope's decision, so a resolved decision does not linger as falsely
pending). This ledger + render is the data model and view that #210 builds on; #210's full
see-and-manage-conversations UX and its dedicated UX-designer desk remain #210's scope and are NOT
delivered by this change.

#### Scenario: The dash renders a desk's latest delta with age, pulled

- **WHEN** the operator opens the dash after a desk has published enveloped artifacts
- **THEN** the dash renders that desk's latest `anchor`→`delta`, its age from the timestamp (a stale
  delta shown as stale), and any pending `decision` — cold-readable, without pushing a new live surface

#### Scenario: A resolved decision clears, it does not linger as pending

- **WHEN** a desk publishes a newer envelope with `decision:"none"` after an earlier one carried an
  action
- **THEN** the dash renders the latest envelope's `decision` (no pending decision), so an already-acted
  decision does not corrupt the operator's map by lingering

#### Scenario: The ledger uses the existing read-model pattern with an atomic write

- **WHEN** the publish path writes the ledger and the dash reads it
- **THEN** the write is atomic (temp + rename) and the read is via `readFileOrEmpty` into an
  envelope-extended `HistoryDoc` (the existing pure-reader-over-files pattern) — not `BoardDoc` (desk
  states) and not the inbound `SetMirror` audit store

### Requirement: Existing notify and send are unchanged; un-enveloped posts degrade safely

This change SHALL NOT alter the success path of `flotilla notify` or `flotilla send`: for clean traffic
(no firewall hit) they keep working byte-identically. `brief` is a NEW command that composes the existing
mirror; the envelope is additive. The ONLY behavior added to `notify`/`reply` is the firewall refuse (D,
P2) — a leaking `notify` is refused rather than published; this is an additive safety guard, not a
regression of clean traffic. An un-enveloped ordinary turn-final on the auto-mirror SHALL warn-and-publish
(today's behavior preserved), and an un-enveloped PUBLIC git/GitHub artifact SHALL fail closed (per the
posture).

#### Scenario: notify and send keep working unchanged for clean traffic

- **WHEN** `flotilla notify` or `flotilla send` is invoked with no firewall leak after this change ships
- **THEN** its behavior is byte-identical to before (the reader-modeling path composes the mirror; it
  does not edit the notify/send success path — only the P2 firewall refuse is added, and only on a leak)

#### Scenario: An un-enveloped ordinary turn-final still mirrors

- **WHEN** the auto-mirror publishes an ordinary (non-brief) turn-final that carries no envelope
- **THEN** it is warned-and-published (today's mirror behavior), not dropped
