# watch Specification (delta)

## ADDED Requirements

### Requirement: The per-desk mirror runs a synchronous reader-modeling pipeline before each post

`deskMirror.run` (`cmd/flotilla/mirror.go`) SHALL, BEFORE its existing post, run the synchronous
reader-modeling pre-post pipeline — **(1) firewall refuse-check → (2) envelope validate → (3) tier-1
structural lint** — and SHALL suppress the post when the firewall refuses or a public-egress lint
fail-closes. The pipeline SHALL run SYNCHRONOUSLY inside `run` (before the post) precisely because a
Discord message cannot be un-sent; it SHALL NOT be deferred to the async `MirrorDispatch` goroutine in
a way that lets a refused artifact reach Discord. The mirror SHALL remain OBSERVE-ONLY and BEST-EFFORT
for everything EXCEPT a fail-closed firewall refusal (which suppresses the post) and a public-egress
lint failure: an ordinary internal channel turn-final that merely lacks an envelope SHALL still be
warned-and-published (today's behavior preserved). Every pipeline outcome SHALL continue to emit exactly
one decision log line (the mirror's existing one-line-per-outcome invariant), so a suppression is never
silent.

#### Scenario: The mirror runs the sync pipeline before posting

- **WHEN** `deskMirror.run` is about to post a desk's turn-final
- **THEN** it runs the firewall refuse-check, then envelope validate, then the tier-1 structural lint
  synchronously before the post, and only posts if none of them suppresses the artifact

#### Scenario: A firewall hit suppresses the auto-mirror post and logs

- **WHEN** a desk's auto-mirrored turn-final contains a private deployment specific
- **THEN** the mirror suppresses the post (the leak never reaches Discord) and emits its one decision log
  line naming the suppression, rather than posting

#### Scenario: An un-enveloped ordinary turn-final is still mirrored

- **WHEN** the auto-mirror handles an ordinary (non-brief) turn-final that carries no envelope and no
  firewall hit
- **THEN** it is warned-and-published as today (the absence of an envelope on an internal channel post is
  the back-compat branch, not a fail-closed)

#### Scenario: The tier-2 judge never runs in the auto-mirror

- **WHEN** the best-effort auto-mirror publishes a turn-final
- **THEN** the tier-2 semantic LLM judge does NOT run on that path (only the firewall + envelope validate
  + tier-1 structural lint run synchronously); the judge runs only on the explicit CLI publish path
