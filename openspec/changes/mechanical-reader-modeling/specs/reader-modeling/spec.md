# reader-modeling Specification (delta)

## ADDED Requirements

### Requirement: `flotilla brief` publishes a desk's reader-modeled brief deterministically via the shipped mirror

The system SHALL provide `flotilla brief <desk>` — a single operation that elicits a reader-modeled
brief from a desk and publishes it to that desk's own Discord channel. The brief SHALL be published by
the EXISTING per-desk mirror (`internal/watch/detector.go`'s `MirrorOnFinish` → `cmd/flotilla/mirror.go`'s
`deskMirror.run`, the `surface.ResultReader` turn-final seam), NOT by `flotilla notify` and NOT by a
new transport. The desk SHALL never touch fleet secrets to publish a brief (honoring the smart-desk
secret-free invariant trained by `cmd/flotilla/pushsnippet.go`). Determinism SHALL come from the mirror
firing on the brief turn's finish without desk cooperation — a desk SHALL NOT be required to remember to
call a publish primitive, and a desk that merely answers the brief request in-pane SHALL still have its
brief published. The scope of `flotilla brief` is the desk's **channel** surface; the raw pane surface
is explicitly out of scope.

#### Scenario: A brief call yields a published Discord brief, secret-free

- **WHEN** `flotilla brief <desk>` runs against a desk whose channel webhook is configured
- **THEN** the desk produces a brief, the brief's turn-final is published to the desk's channel under
  the desk's own webhook identity via the existing mirror path, and the desk never invokes
  `flotilla notify` nor reads any fleet secret

#### Scenario: A desk that replies in-pane is still published

- **WHEN** a desk responds to the brief request as an ordinary in-pane turn (it does not itself call any
  publish command)
- **THEN** the mirror firing on the turn's finish publishes that turn-final to the desk's channel, so
  the brief reaches the operator's channel without depending on the desk's discipline

#### Scenario: A fleet-wide brief fan-out reaches every desk's channel

- **WHEN** `flotilla brief` is fanned out to every non-XO desk
- **THEN** every desk with a configured channel webhook publishes its brief (the #207 failure — only a
  fraction published because the fan-out relied on each desk translating a free-text request into the
  forbidden `notify` — does not recur)

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

### Requirement: Tier-1 structural lint runs synchronously inside the mirror before the post

The publish path SHALL enforce a deterministic **tier-1 structural lint** with NO model call: the
envelope schema is valid, `decision` is present-or-explicit-`none`, the body opens with the `anchor`
and leads with the `decision`. Tier-1 SHALL run SYNCHRONOUSLY inside `deskMirror` BEFORE the post, so a
fail-closed refusal happens before publish (a Discord message cannot be un-sent). Tier-1 SHALL only ever
block on trivially-fixable missing structure — it SHALL NOT judge content — so it never traps a desk
mid-incident.

#### Scenario: A slop envelope fails tier-1 structurally

- **WHEN** an enveloped artifact has `anchor:"my work"` and a body that neither opens with the anchor nor
  leads with the decision
- **THEN** tier-1 fails the artifact structurally (per its egress posture) without any model call

#### Scenario: Tier-1 runs before the post, not after

- **WHEN** tier-1 fail-closes an artifact on a public egress
- **THEN** the artifact is never posted (the refusal precedes publish), because tier-1 runs
  synchronously inside the mirror before the post

### Requirement: Tier-2 semantic judge runs only on the willing-to-wait CLI path

The publish path SHALL provide a **tier-2 semantic judge** — an LLM reading the artifact AS the named
`audience` and assessing whether the `anchor` is really that reader's map entry, whether the body opens
from the reader's map, and whether the artifact stands alone cold. The tier-2 judge SHALL run ONLY on
the explicit, willing-to-wait CLI path (`brief`/`notify`), BEFORE the path hands off to the mirror, and
SHALL NEVER run in the best-effort auto-mirror (a slow judge would stall or be skipped on the auto
path). The judge SHALL cost a model call and SHALL be skippable on the auto-mirror without blocking the
post.

#### Scenario: A content-but-unmodeled public artifact fails the judge

- **WHEN** an artifact bound for a PUBLIC git/GitHub egress passes tier-1 structurally but the tier-2
  judge, reading as the audience, finds it written from the author's internal state (wrong anchor, does
  not stand alone)
- **THEN** the artifact is fail-closed (blocked) on the public egress

#### Scenario: The judge never runs in the auto-mirror

- **WHEN** an ordinary turn-final is published by the best-effort auto-mirror (not an explicit CLI brief)
- **THEN** the tier-2 semantic judge does not run on that path (only tier-1 + the firewall run
  synchronously there)

### Requirement: The lint posture is fail-closed public, warn-with-publish for briefs and internal channels

The reader-modeling path SHALL apply this posture so the operator NEVER loses a brief to a lint:

- PUBLIC git/GitHub artifacts (issues/PRs/commits) SHALL be **fail-closed** — any lint failure (incl. a
  missing or malformed envelope) blocks the artifact.
- Operator briefs and internal Discord channel posts SHALL be **warn-with-publish + flag** — the post is
  always delivered, and a lint failure is recorded and surfaced, never dropped.
- A malformed envelope (present but schema-invalid) SHALL be fail-closed everywhere (a trivially-fixable
  structural defect, not a content trap).
- An ABSENT envelope on an ordinary auto-mirror turn-final SHALL be the back-compat warn-and-publish
  branch (today's mirror behavior preserved for non-brief turn-finals).

#### Scenario: An operator brief that fails a lint is still published

- **WHEN** an operator brief or internal channel post fails the tier-2 judge (or carries no envelope)
- **THEN** the post is still published, and the lint failure is recorded and flagged — the brief is
  never lost to the lint

#### Scenario: A public artifact that fails a lint is blocked

- **WHEN** a public git/GitHub artifact fails any lint tier (or carries a malformed envelope)
- **THEN** the artifact is fail-closed (blocked) until the failure is fixed

### Requirement: The firewall REFUSES a private leak, never strips it

The publish path SHALL run every outbound artifact through the private-firewall detector (the static
guard's deployment denylist plus #202's `<prefix>:<n>.<m>` / `#<deployment>-c2` pattern). On a hit it
SHALL **REFUSE** the artifact and bounce to the desk the offending token together with its generic
abstraction as a suggestion the desk applies in-context — it SHALL NEVER silently rewrite the artifact.
A runtime strip is FORBIDDEN because generalizing a deployment specific inside a sentence whose meaning
depends on it would corrupt the modeled delta the operator's map ingests — worse than a refusal. The
firewall refuse SHALL be fail-closed on BOTH egresses (Discord runtime + git/GitHub static): a private
leak is never published. On the auto-mirror (no interactive desk to bounce to mid-turn) a hit SHALL
SUPPRESS the post and log loudly; on the CLI path the desk is bounced the token + abstraction. This
firewall REUSES #202's regex at runtime egress and does NOT subsume #202 (which ships as its own static
guard PR for committed fixtures that never traverse the publish path).

#### Scenario: An artifact carrying a deployment specific is refused, not rewritten

- **WHEN** an outbound artifact on the publish path contains a private deployment specific (a denylisted
  term or the #202 session:window.pane pattern)
- **THEN** the artifact is REFUSED and never published, the desk is given the offending token and its
  generic abstraction as an in-context suggestion, and the artifact is NEVER silently rewritten

#### Scenario: The auto-mirror suppresses a leaking post

- **WHEN** an ordinary auto-mirror turn-final contains a private deployment specific
- **THEN** the post is suppressed and the refusal is logged loudly (the leak is never published), since
  there is no interactive desk turn to bounce to

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

The publish path SHALL maintain a new append-only **per-desk envelope ledger** (`latest-delta.json` per
desk), written alongside the existing CoS ledger, recording each published envelope. The dash SHALL read
this ledger via the existing pure-reader-over-files pattern (`readFileOrEmpty` → an envelope-extended
`HistoryDoc`) and render, per desk, the latest `anchor`→`delta` and any pending `decision` — glanceable,
*pulled* not pushed (it SHALL NOT introduce a new live surface to babysit). This ledger + render is the
data model and view that #210 builds on; #210's full see-and-manage-conversations UX and its dedicated
UX-designer desk remain #210's scope and are NOT delivered by this change.

#### Scenario: The dash renders a desk's latest delta and pending decision, pulled

- **WHEN** the operator opens the dash after a desk has published enveloped artifacts
- **THEN** the dash renders that desk's latest `anchor`→`delta` and any pending `decision` from the
  envelope ledger, cold-readable, without pushing a new live surface

#### Scenario: The ledger uses the existing read-model pattern, not a new store

- **WHEN** the dash reads the envelope ledger
- **THEN** it reads via `readFileOrEmpty` into an envelope-extended `HistoryDoc` (the existing
  pure-reader-over-files pattern), not via `BoardDoc` (desk states) and not via the inbound `SetMirror`
  audit store

### Requirement: Existing notify and send are unchanged; un-enveloped posts degrade safely

This change SHALL NOT alter `flotilla notify` or `flotilla send`: they keep working byte-identically.
`brief` is a NEW command that composes the existing mirror; the envelope is additive. An un-enveloped
ordinary turn-final on the auto-mirror SHALL warn-and-publish (today's behavior preserved), and an
un-enveloped PUBLIC git/GitHub artifact SHALL fail closed (per the posture).

#### Scenario: notify and send keep working unchanged

- **WHEN** `flotilla notify` or `flotilla send` is invoked after this change ships
- **THEN** its behavior is byte-identical to before (the reader-modeling path composes the mirror; it
  does not edit notify or send)

#### Scenario: An un-enveloped ordinary turn-final still mirrors

- **WHEN** the auto-mirror publishes an ordinary (non-brief) turn-final that carries no envelope
- **THEN** it is warned-and-published (today's mirror behavior), not dropped
