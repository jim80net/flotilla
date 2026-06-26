# watch Specification (delta)

## ADDED Requirements

### Requirement: The c2 hotline has a never-silent return leg

The system SHALL mechanically provide the RETURN leg of the operator↔XO hotline: a c2 channel is the
operator's hotline to its XO (an operator message in a bound channel routes to that channel's
`xo_agent` via `BindingForChannel→XOAgent`), and when such a message is confirmed-delivered to the XO,
the XO's resulting turn-final SHALL be routed back to that ORIGIN channel, attributed to the XO, for
EVERY such turn (not best-effort).

The return leg SHALL detect the XO's reply from the harness SESSION STORE (the ground truth of
completed turns), NOT from pane-rendered state and NOT from the change-detector's heartbeat-cadence
sampling: it SHALL snapshot the XO's active-transcript assistant-turn COUNT at confirmed delivery and,
once that count increases and the transcript read is stable (quiescent), extract the latest text-bearing
assistant turn as the verbatim reply. Counting assistant turns (rather than per-entry timestamps) keeps
the mechanism uniform across harnesses whose stores do not record timestamps. This SHALL reliably
capture a fast, queued, or sub-heartbeat-interval turn (which the detector-tick path silently drops).

The reply SHALL be posted to the origin channel's webhook, resolved
`BindingForChannel(originChannel)→XOAgent→Webhook`, under the XO's identity, chunked to Discord's
limit. Because the reply is posted via a webhook, the relay's feedback-loop immunity (the `webhookID`
drop) SHALL prevent it from re-entering the relay.

Every NON-route outcome SHALL raise a LOUD operator alert (NOT a journald-only skip), extending the
"a dropped operator message is never silent" guarantee to the return leg: no new assistant turn within
the bounded window, an unresolved origin-channel webhook, or a failed post. The watcher SHALL be
per-XO single — a newer hotline message supersedes and re-anchors the prior — and SHALL NOT emit a
stale reply to a superseded origin channel. The return leg SHALL be read-only with respect to the XO's
pane (it acquires no pane transaction lock) and SHALL NOT change the inbound relay, the detector tick,
the primary XO's existing reply path, or the per-desk visibility mirror.

#### Scenario: A federated c2-channel XO's reply routes back to the operator

- **WHEN** the operator sends a message in a c2 channel whose `xo_agent` is a federated XO, and that XO
  produces a turn-final in response
- **THEN** the XO's verbatim turn-final is posted back to that c2 channel (attributed to the XO),
  detected from the session store's assistant-turn-count increase — including when the turn completes
  faster than the heartbeat interval

#### Scenario: A reply that never arrives is escalated, never silently dropped

- **WHEN** an operator message is confirmed-delivered to a channel's XO but no new assistant turn
  appears within the bounded window (or the origin-channel webhook cannot be resolved, or the post
  fails)
- **THEN** a LOUD operator alert is raised naming the XO and channel (e.g. "read its pane"), rather than
  the reply being dropped silently

#### Scenario: The return-leg reply does not feed back into the relay

- **WHEN** the XO's reply is posted to the origin channel via the channel webhook
- **THEN** the relay drops it (the `webhookID` feedback-loop guard), so the reply is not re-ingested as
  a new operator message

#### Scenario: A superseding hotline message re-anchors the watcher

- **WHEN** a second operator message is delivered to the same XO before the first reply has routed
- **THEN** the watcher re-anchors to the second message's origin channel and does not emit a stale
  reply to the first channel
