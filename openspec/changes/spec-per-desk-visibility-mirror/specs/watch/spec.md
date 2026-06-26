# watch Specification (delta)

## ADDED Requirements

### Requirement: A per-desk visibility mirror posts each non-XO desk's turn-final to its own channel

The system SHALL provide a per-desk VISIBILITY mirror: when a NON-XO desk finishes a turn, the daemon
SHALL post that desk's substantive turn-final to the DESK's OWN channel webhook
(`secrets.Webhook(<desk>)`), under the desk's identity, chunked below Discord's hard content limit (a
per-chunk budget held under 2000 runes for headroom) — so the operator/XO can see what a desk has been
doing in its own channel. This is DISTINCT from the operator↔XO hotline
RETURN leg (which routes a reply to the OPERATOR's origin channel): the visibility mirror fires for
EVERY non-XO desk turn and posts to the DESK's channel, not in response to an operator message.

The visibility mirror SHALL be OBSERVE-ONLY and BEST-EFFORT: it SHALL NEVER affect the desk or
propagate a failure, and it SHALL emit exactly one decision log line per finished desk — a clean SKIP
(no webhook configured, no session-store reader for the surface, or no substantive turn-final), a POST
(the turn-final was mirrored, with its chunk count), or a MIRROR-FAIL (a chunk post failed,
redaction-safe). The turn-final SHALL be read from the harness session store via the surface
`ResultReader` — the SAME extraction `flotilla result` uses, so the CLI and the auto-mirror never
diverge. That extraction SHALL resolve the desk's OWN session by its working directory and SKIP a
colliding desk's session (the lossy project-dir-encoding guard), so a desk's channel never carries
another desk's turn-final. Because it posts via a webhook, the relay's feedback-loop immunity (the
`webhookID` drop) SHALL prevent the mirrored post from re-entering the relay.

The visibility mirror SHALL be TRIGGERED by the change-detector's sampled `Working→Idle` edge at the
heartbeat-interval cadence. It is therefore explicitly BEST-EFFORT and LOSSY: a turn that starts AND
finishes entirely within one detector-tick window is NOT observed and is NOT mirrored. A desk's channel
is consequently a best-effort VIEW of its activity, NOT a reliable or complete record — and this
property SHALL be documented so the channel is not mistaken for a complete log. (Making per-desk
mirroring reliable — per-turn store-completion detection independent of the tick — is a separate,
scoped change, NOT part of this requirement.)

The daemon SHALL emit a startup coverage line naming which non-XO desks WILL mirror (a webhook
resolves) and which will NOT (no webhook ⇒ a per-desk SKIP at runtime), so a mis-provisioned desk is
visible at boot rather than at the first dropped mirror.

#### Scenario: A non-XO desk's turn-final is mirrored to its own channel

- **WHEN** a non-XO desk with a configured webhook finishes a turn that the detector observes (its
  `Working→Idle` edge lands within a tick), and the turn has substantive turn-final text
- **THEN** the desk's turn-final is posted to the desk's own channel webhook, under the desk's identity,
  chunked, and exactly one POST decision line is logged

#### Scenario: A desk with no webhook / no reader / no substantive turn is a clean skip

- **WHEN** the mirror runs for a desk that has no configured webhook, OR whose surface has no
  session-store reader, OR whose finished turn has no substantive turn-final
- **THEN** the mirror posts nothing and logs exactly one SKIP decision line, and the desk is never
  affected

#### Scenario: A sub-tick turn is not mirrored (the documented best-effort lossiness)

- **WHEN** a desk's turn starts and finishes entirely within one change-detector tick window (so the
  detector samples Idle before and Idle after, never observing the `Working→Idle` edge)
- **THEN** that turn is NOT mirrored — the desk channel is a best-effort view, not a complete record
  (this is the documented property, not a silent defect the spec hides)

#### Scenario: The mirrored post does not feed back into the relay

- **WHEN** a desk's turn-final is posted to its channel via the webhook
- **THEN** the relay drops it (the `webhookID` feedback-loop guard), so it is not re-ingested as an
  operator message
