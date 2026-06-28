# watch Specification (delta)

## ADDED Requirements

### Requirement: Recursive per-agent desk heartbeat

The change-detector SHALL heartbeat EVERY monitored non-primary-XO agent (leaf desks AND federated
sub-flotilla XOs) on a per-agent cadence, so an agent that goes Idle mid-task is re-engaged rather than
silently stalling. This is the recursive downstream complement to the XO heartbeat (the system clock):
the XO clock drives the primary XO; this drives everything below it, and — because the detector monitors
the whole agent tree — the federation topology makes it a cascade (a sub-XO is heartbeated and
re-engages its own desks). The primary XO is NEVER desk-heartbeated (it keeps its own clock).

**Trigger (Idle-gated, cadence-bounded).** Each tick, for every monitored agent that is NOT the primary
XO and is NOT roster-opted-out: when the agent's assessed state is `Idle`, it is NOT desk-settled, and
its per-agent quiet-tick counter has reached the desk-heartbeat cadence (the heartbeat interval), the
detector SHALL deliver that agent a heartbeat turn. A Working agent SHALL NOT be heartbeated (it is
progressing); a heartbeat to a busy agent SHALL be dropped (the per-agent busy-defer), never queued to
interrupt a turn. The heartbeat SHALL use the audit-suppressed delivery kind (so it is NOT mirrored to
the operator channel). The decision SHALL run under the detector's single-writer lock and the delivery
off that lock (like the synthesis wake), so it never stalls the tick or the operator-wake path.

**DEFAULT-ON with a per-agent roster opt-OUT.** Desk heartbeats SHALL be ON by default for the general
fleet (the directive is universal); a per-agent roster opt-OUT (`heartbeat: false`, a pointer flag whose
ABSENCE means ON) SHALL exclude a deliberately-quiet desk. APPROVAL-SENSITIVE / ACTION desks (any desk
that places orders or spends) SHALL be opt-OUT BY DEFAULT until the claude driver can distinguish an
approval-blocked desk from an idle one (claude's assessment is binary — an approval-blocked claude desk
reads Idle — so a default-on heartbeat to such a desk risks landing text in an approval modal; tracked
unblock). The heartbeat prompt SHALL additionally be NON-AUTHORIZING as defense-in-depth ("advance only
ALREADY-AUTHORIZED steps; if a permission/approval is pending, do NOT approve it on a heartbeat — reply
idle").

**The desk-continuation prompt.** A DEDICATED desk-heartbeat prompt (distinct from the XO's): it carries
the agent's OWN settle-marker path, teaches the touch-when-idle contract inline, is non-authorizing
(above), does NOT claim the agent's context is rotated (desks are not rotated by this mechanism), and
does NOT instruct reading a durable tracker a leaf desk may not keep ("continue your in-flight task; if
you've lost the thread, reply idle"). A per-agent workspace `HEARTBEAT.md` MAY override it.

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

**Cold-start + recursion safety.** On the detector's cold-start tick (baseline seeding), NO agent SHALL
be owed a heartbeat (no restart-storm). A sub-XO that is the PRIMARY XO of another running watch daemon
SHALL be opt-OUT of the parent's desk heartbeat (heartbeated by its own clock OR the parent, never
both). The mechanism SHALL be byte-inert when disabled — no behavior change to today's XO clock,
materiality wakes, visibility mirror, or synthesis.

#### Scenario: An idle desk is re-engaged within one cadence

- **WHEN** a non-XO desk (not opted out) finishes a turn and remains Idle without touching its settle
  marker, and its per-agent quiet cadence elapses
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

#### Scenario: An approval-sensitive desk is opt-OUT by default

- **WHEN** the roster contains an approval-sensitive / order-placing desk (e.g. one that places orders)
  with no explicit heartbeat flag
- **THEN** it is NOT desk-heartbeated by default (the claude-approval-ambiguity carve-out), until the
  claude approval classifier lands and the opt-out is flipped

#### Scenario: The desk heartbeat does not interfere with the XO clock or double-drive a sub-XO

- **WHEN** the detector runs with desk heartbeats enabled
- **THEN** the primary XO is never desk-heartbeated (it keeps its own clock), a sub-XO Working on a
  synthesis turn is not also heartbeated (Idle-gated; a synthesis wake resets its quiet counter), and
  with the mechanism disabled the detector's XO-clock / materiality / mirror / synthesis behavior is
  byte-unchanged
