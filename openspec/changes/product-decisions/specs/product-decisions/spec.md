# product-decisions Specification

## Purpose

A canonical, openspec-tracked register of RATIFIED operator product / positioning / process
decisions. Capability-level decisions live in their own capability specs; this register is
the home for the **product-level** calls that otherwise live only in commit bodies, chat,
and rules — so they are tracked, citeable, and **never re-asked**. The README and any
strategy / release / landing material DERIVE from this register. Each requirement records
the decision, its provenance (operator statement and/or enacting commit), and that it is
settled.

## ADDED Requirements

### Requirement: Ratified decisions are tracked here and not re-asked

This register SHALL be the source of truth for ratified product / positioning / process
decisions. A decision recorded here SHALL NOT be re-opened or presented as an "open
question" in any draft, strategy memo, or review; derivative material (README, landing,
release docs) SHALL cite the decided answer rather than re-litigate it. A draft that
re-opens a ratified decision is a defect to fix, not a question to answer. New ratified
decisions SHALL be appended here (with provenance) as they are made.

#### Scenario: A strategy draft references a settled decision

- **WHEN** a strategy / release / positioning draft needs a decision recorded in this register
- **THEN** it cites the decided answer (and the README where the decision is enacted) and does NOT present it as an open fork

#### Scenario: A new decision is ratified

- **WHEN** the operator ratifies a new product / positioning / process decision
- **THEN** it is appended to this register with its provenance, so the next reader finds it instead of re-asking

### Requirement: Positioning — flotilla is a drop-in chief of staff

The product SHALL be positioned as **"a drop-in chief of staff for the AI coding harnesses
you've already built"** — a **pluggable coordination layer** in which one hub agent (the XO)
fans work to domain desks, collects replies, and keeps a durable auditable record, driven
from a chat channel. The **README is the canonical statement** of this positioning. This is
RATIFIED — operator 2026-06-18 (*"Q1 was definitely answered and the current README is the
result"*), enacted in PRs #96 / #107 (commits `5ef0f38`, `0720b35`, `3450996`),
README.md:3-14. The one-liner SHALL NOT be re-presented as an open choice.

#### Scenario: The one-liner is needed

- **WHEN** a doc or site needs the product one-liner
- **THEN** it uses the README's statement of record, not a fresh "Option A/B/C" fork

### Requirement: No-daemon and no-lock-in are not differentiators

flotilla SHALL NOT use no-daemon, no-hosted-service, no-lock-in, or
built-on-substrate-you-already-have as product differentiators or requirements in any
public copy. This was explicitly DISAVOWED by the operator on 2026-06-18 (no-daemon /
no-new-binary is not a real product requirement; drop it and references to it), enacted in
PR #96 (commit 5ef0f38). It is also inaccurate, since flotilla watch IS a daemon. True
technical statements that happen to share words (for example, each agent stays an ordinary,
independently-controlled session) remain fine; only the positioning use is banned.

#### Scenario: Copy is drafted that leans on "no daemon"

- **WHEN** marketing / README / landing copy is drafted
- **THEN** it leads with the coordination value, and does NOT reintroduce "no new daemon / no lock-in" as a selling point

### Requirement: Chat-first — the chat channel is the whole interface

The public framing SHALL lead with the **chat channel as the primary interface** ("you drive
the fleet from a chat channel, even from your phone; once it's running there's no terminal to
babysit"), with the CLI presented as the under-the-hood mechanism. RATIFIED — enacted in the
chat-first README (PRs #96 / #107, README.md:21-38).

#### Scenario: The pitch is ordered

- **WHEN** the README / landing orders its pitch
- **THEN** the chat-channel experience is the lead and the CLI is "under the hood", not the headline

### Requirement: herdr is complementary, not a competitor

herdr SHALL be treated as a **complementary** runtime/visibility layer at a different
altitude, not a competitor, and flotilla SHALL NOT take a hard dependency on or tie itself to
it. RATIFIED — operator 2026-06-18; `docs/competitive/herdr-vs-flotilla.md:10,22` (*"more
complementary than competing"*).

#### Scenario: herdr comes up in positioning or design

- **WHEN** a competitive or design discussion references herdr
- **THEN** it is framed as a complementary layer, and no hard tie-in / dependency is proposed without a fresh operator decision

### Requirement: The public surface uses generic examples only

Every public-facing artifact (README, quickstart, demo asset, landing site) SHALL use
generic example desks (`infra` / `research` / `data`) and SHALL NEVER reference the private
deployment's desks or domain (the trading daemon / its desks). RATIFIED — enacted in the
current README/quickstart; operator's separate-circumstantial-from-generalizable discipline.

#### Scenario: A demo or doc is authored

- **WHEN** a public demo, screenshot, or doc example is produced
- **THEN** it uses `infra`/`research`/`data`-style generic desks, never the private deployment's real desks

### Requirement: Workflow posture — trio-gated autonomy, operator decides strategy

A design SHALL proceed to implementation once it clears the review trio (systems-review +
open-code-review + STORM), with no separate per-design operator-ratification gate; clean-gated
non-major work SHALL merge without an operator nod. The operator's review is reserved for
**strategy, major / fundamentally-significant / controversial choices, money, irreversible /
outward-facing actions, and genuine divergent-direction forks**. RATIFIED — operator
2026-06-18 (commit `db0864a` body; the autonomous-workflow directive).

#### Scenario: A clean-gated non-major PR is ready

- **WHEN** a non-major change has clean CI + a clean review trio
- **THEN** it merges without waiting for an operator nod, and only the carve-outs (strategy / major / money / irreversible / divergent) are surfaced to the operator

### Requirement: The landing site / dashboard is a separate dedicated desk

The landing-site / dashboard ("flotilla-dash") SHALL be owned by a **separate dedicated
desk**, not the core-flotilla XO; core work stays on the core repo and CLI. RATIFIED —
operator 2026-06-18. (Greenlight is settled; the core XO does not need to re-ask whether to
spawn it.)

#### Scenario: Dashboard / landing work arises

- **WHEN** landing-site or dashboard work is needed
- **THEN** it is routed to the dedicated flotilla-dash desk, and the core XO stays on core-flotilla work

### Requirement: Capability decisions live in their capability specs

Decisions that have a canonical capability home SHALL be recorded and cited there, not
duplicated here; this register POINTS to them. The capability specs of record include:
`federation` (recursive hub-and-spoke; Transport A for v1, Transport B deferred), `cos`
(observe-only context mirror), `surface` (per-agent drivers; unknown surface = clear error;
pull-participants + opt-in smart desks), `provision` (mechanical channel provisioning),
`backlog` (goal-driven loop), `watch` (change-detector v2 default), `voice`, and the
`agent-workspace` change (per-desk `~/.flotilla/<agent>/`, identity via
`--append-system-prompt-file`).

#### Scenario: A capability-level decision is needed

- **WHEN** a reader needs a decision about a built capability (e.g. federation transport, surface-driver contract)
- **THEN** they consult that capability's spec (linked here), which is the record of that decision
