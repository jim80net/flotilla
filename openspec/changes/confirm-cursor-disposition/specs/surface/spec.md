# surface Specification (delta)

## MODIFIED Requirements

### Requirement: A surface driver MAY report the composer disposition at the cursor

A surface driver MAY implement an OPTIONAL `ComposerStateProbe` that SHALL be READ-ONLY and report
the disposition of the composer AT THE TERMINAL CURSOR — the focused input line. It SHALL read the
pane cursor row and classify the line the cursor sits on into: Cleared (empty — submitted), Pending
(a body remains — not submitted), Queued (the input is queued behind a modal/turn and will deliver),
SubAgent (a per-agent message sub-composer holds focus), ListNav (the cursor is on an agent-list
row), or Undetermined (no readable cursor/prompt). Reading at the cursor (not a fixed bottom-of-pane
window) is REQUIRED so a sub-composer rendered above a docked panel is not missed. Whitespace
trimming SHALL treat the non-breaking space (U+00A0) Claude Code renders after the prompt as
whitespace. A caller that gets Undetermined (or a driver without the probe) SHALL fall back to the
existing Working-spinner signal — never treating an unreadable composer as cleared.

#### Scenario: A per-agent message sub-composer is detected at the cursor

- **WHEN** the cursor sits on a per-agent message sub-composer (the prompt body begins "Message @")
- **THEN** the probe reports SubAgent

#### Scenario: A queued message is detected as a soft-success state

- **WHEN** the cursor sits on the queued-message prompt ("Press up to edit queued messages")
- **THEN** the probe reports Queued (the message will deliver; it is not lost)

#### Scenario: The composer body is read even above a docked panel

- **WHEN** the focused composer is rendered above a docked agents panel (outside the bottom-of-pane window)
- **THEN** the probe reads the composer at the cursor and reports its true disposition, not Undetermined

### Requirement: Confirmed delivery is the authority; a persistently-pending composer is blocked

Confirmed delivery SHALL treat the composer state as the GROUND TRUTH for delivery, not a pre-paste
geometry/cursor prediction. After submitting, if the composer remains PENDING (the body provably
remains) through the bounded Enter-only retries and the grace window, the delivery SHALL be reported
BLOCKED (a distinct input-blocked failure), never silently successful. A Cleared (stable) composer or
a Queued state SHALL be reported as confirmed delivery (Queued is a soft-success — the message will
deliver). The confirm mechanism's no-re-paste invariant SHALL be preserved (retries are Enter-only).

#### Scenario: A composer that stays pending after the retries is blocked

- **WHEN** the composer holds the body through the retries + grace (the submit never lands)
- **THEN** the delivery is reported input-blocked (BLOCKED), regardless of the cursor/geometry

#### Scenario: A queued submit is a soft-success, not a failure

- **WHEN** after submitting, the composer enters the queued state (the agent is busy/modal)
- **THEN** the delivery is reported confirmed (queued — will deliver), not a failure or an alarm

### Requirement: Confirmed delivery refuses to paste only where a paste would mis-deliver

Confirmed delivery SHALL refuse to paste BEFORE submitting ONLY when the cursor is provably on a
focus-stealing overlay that would MIS-DELIVER the body — a per-agent message sub-composer (SubAgent)
or an agent-list row (ListNav). It SHALL return the input-blocked failure with the reason, and SHALL
NOT paste (a paste into a sub-composer would send to the wrong recipient AND the post-submit check
would false-confirm it as delivered). For every other composer state the orchestration SHALL proceed
to submit and let the post-submit composer state be the authority. The cursor/glyph classification
SHALL be used ONLY to supply the refuse decision for these two states and the alert reason — never as
a general reachability gate.

#### Scenario: A send to a sub-composer-focused desk is refused before pasting

- **WHEN** confirmed delivery is invoked and the cursor is on a per-agent message sub-composer
- **THEN** no paste is attempted and the caller receives the input-blocked failure with the sub-composer reason

#### Scenario: A desk whose cursor is on the main composer is submitted to, then judged by the result

- **WHEN** the cursor is on the main composer (empty or user text)
- **THEN** the body is submitted and the delivery verdict comes from the post-submit composer state
  (cleared/queued = delivered; persistently pending = blocked), NOT from the pre-paste position
