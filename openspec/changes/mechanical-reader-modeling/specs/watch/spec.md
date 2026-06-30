# watch Specification (delta)

## ADDED Requirements

### Requirement: The per-desk mirror runs a synchronous reader-modeling pipeline before each post

`deskMirror.run` (`cmd/flotilla/mirror.go`) SHALL, BEFORE its existing post, run the synchronous
reader-modeling pre-post pipeline — **(1) firewall refuse-check → (2) envelope detect+validate →
(3) tier-1 structural lint**. Because the mirror has NO public egress (every mirror post is an internal
Discord channel), the ONLY step that SUPPRESSES a post on this path is the firewall refuse (a private
leak); envelope-validate and tier-1 are **warn-with-publish** here — a structurally-deficient or
un-enveloped turn-final is flagged but still published (never lost). The pipeline SHALL run SYNCHRONOUSLY
inside `run` (before the post) precisely because a Discord message cannot be un-sent and the firewall
refusal must happen before publish; it SHALL NOT be deferred to the async `MirrorDispatch` goroutine in
a way that lets a leaking artifact reach Discord. The mirror SHALL remain OBSERVE-ONLY and BEST-EFFORT
for everything EXCEPT a firewall refusal (which suppresses the post + raises an operator-visible
signal). Every pipeline outcome SHALL continue to emit exactly one decision log line (the mirror's
existing one-line-per-outcome invariant), so a suppression is never silent. **Phasing:** the
envelope-validate + tier-1 arms land in P0; the firewall refuse arm is the P2 increment (per this
change's phasing) — in the P0-shipped state the mirror pipeline is validate + tier-1 (warn) only, with
no suppression.

#### Scenario: The mirror runs the sync pipeline before posting, suppressing only on a firewall leak

- **WHEN** `deskMirror.run` is about to post a desk's turn-final
- **THEN** it runs the firewall refuse-check, then envelope detect+validate, then tier-1 synchronously
  before the post; it suppresses the post ONLY if the firewall refuses (a leak), and otherwise publishes
  (warn-with-publish for a validate/tier-1 deficiency, since the mirror has no public egress)

#### Scenario: A firewall hit suppresses the auto-mirror post and logs

- **WHEN** a desk's auto-mirrored turn-final contains a known-denylist private deployment specific
- **THEN** the mirror suppresses the post (the leak never reaches Discord), emits its one decision log
  line naming the suppression, AND raises an operator-visible signal (a flagged ledger entry and/or an
  alert-webhook line) so the withheld turn-final does not vanish silently

#### Scenario: An un-enveloped ordinary turn-final is still mirrored

- **WHEN** the auto-mirror handles an ordinary (non-brief) turn-final that carries no envelope and no
  firewall hit
- **THEN** it is warned-and-published as today (the absence of an envelope on an internal channel post is
  the back-compat branch, not a fail-closed)

#### Scenario: The tier-2 judge never runs in the auto-mirror

- **WHEN** the best-effort auto-mirror publishes a turn-final
- **THEN** the tier-2 semantic LLM judge does NOT run on that path (only the firewall + envelope validate
  + tier-1 structural lint run synchronously); the judge runs only on the explicit CLI publish path
