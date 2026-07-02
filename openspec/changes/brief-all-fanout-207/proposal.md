# Proposal — `flotilla brief --all` closes #207 fleet fan-out gap

## Problem

`flotilla brief <desk>` ships (#214) but coordinators still fan out with free-text
`flotilla send "post your brief…"`, which desks answer in-pane (correct per the
secret-free invariant) without publishing to their channels (#207).

The openspec fleet-wide scenario requires fan-out to every non-primary-XO desk with
dark-desk reporting at fan-out time. The single-desk CLI alone does not make the
mechanical path the obvious coordinator operation.

## Change

- Add `flotilla brief --all` (and interleaved flag/agent parsing): inject a brief
  request into every roster agent except the primary `xo_agent`, with per-desk dark
  pre-check, continue-on-error, non-zero exit if any desk failed.
- Document coordinator doctrine: use `flotilla brief`, never free-text publish asks.

## Closes

GitHub #207 (with live proof that mirror publishes enveloped turn-finals).