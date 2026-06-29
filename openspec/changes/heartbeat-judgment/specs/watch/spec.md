# watch Specification (delta)

## MODIFIED Requirements

### Requirement: Recursive per-agent desk heartbeat

The change-detector SHALL heartbeat EVERY monitored non-primary-XO agent (leaf desks AND federated
sub-flotilla XOs) on a per-agent cadence, so an agent that goes Idle mid-task is re-engaged rather than
silently stalling — BUT ONLY when the per-agent heartbeat judgment warrants it (see "The heartbeat
judgment and its two ledgers"). This is the recursive downstream complement to the XO heartbeat (the
system clock): the XO clock drives the primary XO; this drives everything below it, and — because the
detector monitors the whole agent tree — the federation topology makes it a cascade (a sub-XO is
heartbeated and re-engages its own desks). The primary XO is NEVER desk-heartbeated (it keeps its own
clock).

**Trigger (Idle-gated, cadence-bounded, JUDGMENT-warranted).** Each tick, for every monitored agent
that is NOT the primary XO and is NOT roster-opted-out: when the agent's assessed state is `Idle`, it is
NOT desk-settled, its per-agent quiet-tick counter has reached the desk-heartbeat cadence (the heartbeat
interval), AND the heartbeat judgment warrants a beat for that agent (there is outstanding actionable
work for it), the detector SHALL deliver that agent a heartbeat turn. An agent for which the judgment
does NOT warrant a beat (no live actionable work — everything done, blocked-and-tracked, or
awaiting-authorization) SHALL be treated like a settled agent for that tick: NO beat, and NO cap or
cadence accrual (it is legitimately idle, not wedged). A Working agent SHALL NOT be heartbeated (it is
progressing); a heartbeat to a busy agent SHALL be dropped (the per-agent busy-defer), never queued to
interrupt a turn. The heartbeat SHALL use the audit-suppressed delivery kind (so it is NOT mirrored to
the operator channel). The decision SHALL run under the detector's single-writer lock and the delivery
off that lock (like the synthesis wake), so it never stalls the tick or the operator-wake path. The
per-recipient backlog read the judgment depends on is FILE I/O and SHALL NOT run under the detector
lock (it would violate the detector's load-bearing off-mutex invariant, the same one synthesis and the
visibility mirror honor); it SHALL be performed OFF the lock and the resulting per-recipient warrant
SHALL be supplied to the under-lock decision as pure, already-computed data (the two-phase structure of
"The heartbeat judgment and its two ledgers").

**DEFAULT-ON with a per-agent roster opt-OUT (the HARD eligibility gate).** Desk heartbeats SHALL be ON
by default for the general fleet (the directive is universal); a per-agent roster opt-OUT
(`heartbeat: false`, a pointer flag whose ABSENCE means ON) SHALL exclude a deliberately-quiet desk.
APPROVAL-SENSITIVE / ACTION desks (any desk that places orders or spends) SHALL be opt-OUT BY DEFAULT
until the claude driver can distinguish an approval-blocked desk from an idle one (claude's assessment
is binary — an approval-blocked claude desk reads Idle — so a default-on heartbeat to such a desk risks
landing text in an approval modal; tracked unblock). This eligibility gate is a HARD gate: the heartbeat
judgment SHALL NOT override it — an opted-out or approval-sensitive desk is NEVER heartbeated even when
the judgment would otherwise warrant a beat. The heartbeat prompt SHALL additionally be NON-AUTHORIZING
as defense-in-depth ("advance only ALREADY-AUTHORIZED steps; if a permission/approval is pending, do NOT
approve it on a heartbeat — reply idle").

**The desk-continuation prompt (re-trigger-first; ledger-recording).** A DEDICATED desk-heartbeat prompt
(distinct from the XO's): it carries the agent's OWN settle-marker path, teaches the touch-when-idle
contract inline, is non-authorizing (above), does NOT claim the agent's context is rotated (desks are not
rotated by this mechanism), and does NOT instruct reading a durable tracker a leaf desk may not keep. The
prompt SHALL instruct that an idle desk is USUALLY a transient technical fault (a rate-limit, or a turn
that ended before the work was done), so the DEFAULT response to a heartbeat is to RE-TRIGGER — resume
the next already-authorized in-flight step from durable state — and that the agent SHALL NEVER sit idle:
if GENUINELY blocked on the current item it SHALL do opportunistic authorized work instead. The prompt
SHALL instruct the agent to RECORD a blocker into the right ledger so the judgment can settle it: mark a
blocking question/dependency `[blocked]`/`[needs-attention]` (the open-questions ledger) and mark a
pending operator authorization `[awaiting-auth]` (the authorizations ledger), then — only once every item
is done, blocked-and-tracked, or awaiting-authorization — reply idle and touch its settle marker. A
per-agent workspace `HEARTBEAT.md` MAY override it.

**Per-agent settle + re-arm (mirroring the XO, but per-agent).** A desk that replied idle / touched its
OWN settle marker (a per-agent path, e.g. `<roster-dir>/flotilla-<agent>-settled`, NOT the XO's shared
marker) SHALL be suppressed from further heartbeats until it is re-armed. An operator/XO message
delivered to that agent SHALL re-arm it (clear its settled state + reset its quiet/cap counters) — the
relay's accept hook SHALL fire this for EVERY target agent, not only the XO. Fail-safe: an unreadable
settle marker SHALL be treated as NOT settled (keep heartbeating). The per-agent quiet/cap counters are
in-memory (reset on restart, like the XO's); the per-agent settled state is the durable file marker (so
a restart does not re-poke a settled desk).

**Cap → escalate → stop (the wedged-desk safety).** After a bounded number (default 3) of consecutive
heartbeats with NO progress (no transition into Working and no settle between heartbeats), the detector
SHALL STOP heartbeating that agent and raise ONE LOUD operator-visible alert (the watchdog/alert path,
not a quiet wake) escalating the wedged agent to its owning XO — for a leaf desk, the XO of the channel
it is a member of (falling back to the primary XO); for a sub-XO, its synthesis parent / the primary XO.
The alert SHALL fire ONCE on the cap-crossing edge (not every tick). A transition into Working SHALL
reset the cap; the same re-arm that clears settle SHALL clear the stopped state (a re-engaged agent is
heartbeatable again — never wedged forever). An input-blocked drop (a focused modal) SHALL NOT count as
a failed heartbeat toward the cap.

There are TWO paths by which a stuck item stops being poked, and they are distinct:
- **Ledger-mark (the PRIMARY path).** A compliant agent that hits a blocker MARKS the stuck item into
  the right ledger — `[blocked]`/`[needs-attention]` (a question/dependency) or `[awaiting-auth]` (a
  pending authorization). The item leaves the actionable set, the judgment stops warranting a beat, and
  the agent settles CAP-NEUTRAL — no escalation, because a correctly-parked item is NOT a wedge. This is
  intended: the cap does NOT fire for an agent that has honestly recorded that its remaining work is
  blocked-and-tracked or awaiting-authorization.
- **Cap (the BACKSTOP).** The cap exists for the NON-compliant case: an agent that has live
  `[in-flight]` work it is NOT progressing AND will NOT mark its blocker. Such an agent stays warranted
  (it has actionable work by definition), keeps being beaten, and — after capN no-progress beats —
  escalates once and stops.

Accordingly the judgment SHALL NOT mask a wedge: a genuine wedge (live actionable work, no progress, no
ledger-mark) stays warranted and the cap still fires. An agent that DOES mark its stuck item settles
cap-neutral by design; the operator-visibility of those ledger-parked items is OUT OF SCOPE for this
change and is the surfacing backstop tracked in issue #193 (the ledger-mark settles the beat here; that
issue makes the parked item visible to the operator/XO).

**Cold-start + recursion safety.** On the detector's cold-start tick (baseline seeding), NO agent SHALL
be owed a heartbeat (no restart-storm). A sub-XO that is the PRIMARY XO of another running watch daemon
SHALL be opt-OUT of the parent's desk heartbeat (heartbeated by its own clock OR the parent, never
both). The mechanism SHALL be byte-inert when disabled — no behavior change to today's XO clock,
materiality wakes, visibility mirror, or synthesis — AND byte-inert when the heartbeat judgment is
unwired (an unwired judgment SHALL default to always-warranted, so the trigger is identical to the
pre-judgment recursive heartbeat).

#### Scenario: An idle desk WITH live actionable work is re-engaged within one cadence

- **WHEN** a non-XO desk (not opted out) finishes a turn and remains Idle without touching its settle
  marker, its per-agent quiet cadence elapses, and its backlog has at least one actionable
  (`[in-flight]`/`[next]`) item
- **THEN** the detector delivers it a non-authorizing desk-continuation heartbeat (audit-suppressed), so
  it advances its task or replies idle — it does not silently stall

#### Scenario: A settled desk is suppressed, then re-armed by an operator message

- **WHEN** a desk replies idle and touches its per-agent settle marker
- **THEN** it is not heartbeated again until re-armed — and an operator/XO message delivered to that desk
  re-arms it (clears its settled state + resets its quiet/cap counters), so a re-tasked desk resumes
  heartbeating

#### Scenario: A wedged desk escalates once and stops, not infinite-poke

- **WHEN** a desk receives the cap number of consecutive heartbeats with no progress and no settle
- **THEN** the detector stops heartbeating it and raises ONE loud operator-visible alert escalating it to
  its owning XO; a later transition into Working (or an operator re-arm) makes it heartbeatable again

#### Scenario: An approval-sensitive desk is opt-OUT by default even when work is outstanding

- **WHEN** the roster contains an approval-sensitive / order-placing desk (e.g. one that places orders)
  with no explicit heartbeat flag, and that desk's backlog has actionable items
- **THEN** it is NOT desk-heartbeated by default (the claude-approval-ambiguity HARD carve-out is never
  overridden by the judgment), until the claude approval classifier lands and the opt-out is flipped

#### Scenario: The desk heartbeat does not interfere with the XO clock or double-drive a sub-XO

- **WHEN** the detector runs with desk heartbeats enabled
- **THEN** the primary XO is never desk-heartbeated (it keeps its own clock), a sub-XO Working on a
  synthesis turn is not also heartbeated (Idle-gated; a synthesis wake resets its quiet counter), and
  with the mechanism disabled the detector's XO-clock / materiality / mirror / synthesis behavior is
  byte-unchanged

## ADDED Requirements

### Requirement: The heartbeat judgment and its two ledgers

The change-detector SHALL decide whether a desk heartbeat is warranted PER RECIPIENT, as a JUDGMENT
re-evaluated every tick, by asking: is there OUTSTANDING ACTIONABLE WORK for this recipient? The judgment
SHALL warrant a beat when the recipient has an actionable to-do that is NOT already blocked-and-tracked
(recorded in its OPEN-QUESTIONS ledger) AND NOT awaiting operator authorization (recorded in its
AUTHORIZATIONS ledger); it SHALL withhold a beat (treat the recipient as legitimately idle) when no such
live actionable work exists. This refines the static per-recipient eligibility boolean (the roster
opt-OUT) into a dynamic per-recipient judgment: the SAME recipient SHALL flip between warranted and
not-warranted across ticks purely from the evolving content of its ledgers, WITHOUT any roster change.

**The two ledgers SHALL be two status classes on the recipient's backlog**, parsed by the documented,
total/fail-safe backlog status-marker contract: the OPEN-QUESTIONS ledger SHALL be the
`[blocked]`/`[needs-attention]` class; the AUTHORIZATIONS ledger SHALL be a distinct `[awaiting-auth]`
class. An item in EITHER ledger SHALL NOT count as actionable (it never enters the unblocked/actionable
set). The warrant predicate SHALL be exactly: warranted WHEN the parsed backlog is NOT proven to be a
cleanly-parsed empty actionable set — i.e. `warranted = NOT(Found) OR len(Unblocked) > 0`. The
`NOT(Found)` arm is load-bearing: a present, readable backlog file that has NO `## Backlog` section
parses to `Found=false, Unblocked=nil`, which CANNOT prove the recipient has no work, so it SHALL
warrant a beat (NOT suppress). Suppression SHALL require a backlog that is BOTH `Found` AND whose
actionable set is empty. (The parser flags a present-but-sectionless file; the warrant read SHALL alert
ONCE on the edge into that state, mirroring the goal-loop backlog gate's `!Found && non-empty-content`
alert, so a format slip is loud rather than a silent always-beat.)

**Per-recipient resolution and the missing-ledger fallback (load-bearing).** The judgment SHALL read
the RECIPIENT's OWN backlog (e.g. `<roster-dir>/flotilla-<recipient>-backlog.md`) so "live actionable
work for THIS recipient" is a genuine per-recipient read. A recipient that keeps NO per-recipient
backlog file SHALL fall back to ALWAYS-WARRANTED (the pre-judgment recursive-heartbeat behavior) — it
SHALL NOT fall back to the shared fleet backlog. Falling back to the shared backlog would warrant EVERY
ledger-less recipient whenever the fleet drive queue is non-empty (the fleet's queue is the XO's work,
not THIS desk's), re-introducing the indiscriminate poking this change exists to end and making the
judgment a near-no-op on a fresh deployment where no desk yet keeps its own ledger. So: a per-recipient
ledger PRESENT ⇒ judge by it; a per-recipient ledger ABSENT ⇒ warranted (the desk has not opted into the
judgment, so it is driven exactly as #183). The read SHALL be fresh each tick (not content-hashed — it
is the recipient's own output) and SHALL be performed OFF the detector lock at the detector seam (the
roster performs no I/O, and the lock holds no file I/O); the resulting per-recipient warrant SHALL be
supplied to the under-lock per-desk decision as pure, already-computed data (the same two-phase split as
the visibility synthesis: read+parse off-lock, snapshot a pure per-recipient warrant map, decide
under-lock).

**The judgment SHALL compose with — never override — the HARD eligibility gate.** It SHALL be evaluated
only AFTER the roster eligibility gate (XO-exclusion, approval-sensitive opt-OUT, explicit `heartbeat`
flag) has passed; a recipient that the HARD gate excludes SHALL NEVER be beaten regardless of how much
actionable work it has. The judgment can only ever SUPPRESS a beat the recipient would otherwise receive;
it SHALL NOT cause a beat to an ineligible, settled, stopped, or non-idle recipient.

**Fail-safe toward warranted.** The judgment SHALL fail toward WARRANTED (keep the recipient moving),
never toward suppressed (which would re-introduce the silent-stall regression): an ABSENT, UNREADABLE, or
mid-write-torn recipient backlog SHALL be treated as warranted; a present-but-SECTIONLESS backlog
(`Found=false`) SHALL be treated as warranted (it cannot prove no work); a MALFORMED item SHALL be
treated as actionable (warranted) and flagged; suppression SHALL require a backlog that is BOTH `Found`
AND whose actionable set is provably empty. When the judgment is unwired — or when a recipient keeps no
per-recipient backlog file — the detector SHALL default to always-warranted (the recursive heartbeat
behaves exactly as before this change).

#### Scenario: Live actionable work warrants a heartbeat

- **WHEN** an eligible idle desk's resolved backlog has an `[in-flight]` or `[next]` item that is neither
  blocked-and-tracked nor awaiting-authorization
- **THEN** the judgment warrants a beat and the desk is heartbeated on its cadence

#### Scenario: All work blocked-and-tracked warrants NO heartbeat

- **WHEN** an eligible idle desk's resolved backlog has items but every one is `[blocked]` /
  `[needs-attention]` (the open-questions ledger), `[done]`, or `[awaiting-auth]` — no actionable item
- **THEN** the judgment does NOT warrant a beat; the desk is treated as legitimately idle (no beat, no
  cap or cadence accrual) until fresh actionable work appears or it is re-engaged

#### Scenario: Awaiting operator authorization warrants NO heartbeat

- **WHEN** an eligible idle desk's only remaining work is marked `[awaiting-auth]` (the authorizations
  ledger)
- **THEN** the judgment does NOT warrant a beat — the desk is correctly idle pending an operator decision,
  not silently stalled and not pointlessly poked

#### Scenario: The HARD eligibility gate is never overridden by the judgment

- **WHEN** an approval-sensitive (or explicitly `heartbeat:false`) desk has a backlog full of actionable
  `[in-flight]` items
- **THEN** the desk is STILL not heartbeated — the warrant-true judgment cannot resurrect a beat the HARD
  eligibility gate withheld

#### Scenario: An idle-on-heartbeat desk re-triggers rather than sitting idle

- **WHEN** a warranted desk receives a heartbeat and its idleness was a transient technical fault (a
  rate-limit, or a turn that ended before its in-flight work was done)
- **THEN** per the desk-continuation contract it RE-TRIGGERS the next already-authorized step (or, if
  genuinely blocked, does opportunistic work and records the blocker into the right ledger) — it never
  sits idle

#### Scenario: An unreadable per-recipient backlog fails toward warranted

- **WHEN** the recipient's backlog is absent, unreadable, or torn mid-write
- **THEN** the judgment treats the recipient as warranted (keep heartbeating), so a missing ledger can
  never silently stall an otherwise-eligible desk

#### Scenario: A desk with no per-recipient ledger falls back to always-warranted, not the shared backlog

- **WHEN** an eligible idle desk keeps NO per-recipient backlog file and the shared fleet backlog has
  unblocked items (the XO's drive queue)
- **THEN** the judgment treats THIS desk as warranted by the missing-ledger fallback (driven exactly as
  before this change) and SHALL NOT consult the shared fleet backlog to warrant it — a busy fleet queue
  does not indiscriminately poke every ledger-less desk

#### Scenario: A present-but-sectionless backlog fails toward warranted and alerts once

- **WHEN** the recipient's backlog file is present and readable but has no `## Backlog` section (parses
  to `Found=false`)
- **THEN** the judgment treats the recipient as warranted (it cannot prove there is no work) and raises
  ONE operator-visible alert on the edge into that unparseable state — a format slip is loud, never a
  silent always-beat

#### Scenario: A desk that marks its stuck item settles cap-neutral and remains operator-visible

- **WHEN** an eligible idle desk marks its only remaining item `[blocked]` (or `[awaiting-auth]`) in its
  own backlog
- **THEN** the judgment stops warranting a beat and the desk settles CAP-NEUTRAL (no escalation — a
  correctly-parked item is not a wedge), and the parked item remains visible to the operator/XO through
  the ledger-surfacing backstop (tracked in issue #193), so it is not silently dropped

#### Scenario: The awaiting-authorization ledger is observable in the dash read-model

- **WHEN** a recipient's backlog has `[awaiting-auth]` items and the dash coordination-history read-model
  is built from the parsed backlog
- **THEN** the read-model SHALL expose the awaiting-authorization count alongside the blocked count, so
  the authorizations ledger is visible in the dash (not collapsed into blocked) and the surfaceability
  rationale for splitting `[awaiting-auth]` out holds end-to-end
