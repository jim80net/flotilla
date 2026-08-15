<!-- flotilla-publication
classification: decision
reader-action: Decide whether the bounded rollout should advance to independent verification.
support: text-only
support-rationale: The example compares a small policy boundary and needs no external media.
-->

# Example authorization review — operator review

## Why you care

The example fleet needs a decision that is narrow enough to reverse and explicit enough to audit. This document is committed fixture content: it contains no live names, paths, credentials, customer data, or deployment state.

## Recommendation

Approve the bounded verification step while holding production mutation. The verifier should check the declared input boundary, the failure terminal, and the ordinary success path independently.

## Evidence

| Question | Fixture answer |
|---|---|
| What changes? | One generic read path advances to verification. |
| What stays held? | Deployment and merge authority remain outside the example. |
| What fails loudly? | Missing, malformed, and unavailable inputs retain distinct outcomes. |

## Long-content control

This deliberately extended paragraph ensures a product walk can inspect wrapping, reading width, section navigation, and document depth without borrowing a real research paper. A useful operator surface must preserve the full recommendation, keep the evidence table readable, and make the next action discoverable even when the source is longer than a card summary. The fixture therefore includes enough prose to cross several mobile lines while remaining generic, deterministic, and safe to publish.

## Decision requested

Choose one:

1. Advance the bounded change to independent verification.
2. Return it for a named missing control.
3. Hold it without authorizing deployment.
