# Public quickstart cold-test transcript

Artifact binding: the immutable Git commit containing this transcript. No
provider call, Discord credential, private roster, or deployed fleet was used.

## Environment

- Fresh temporary `GOBIN`
- Fresh two-agent public-example roster
- `FLOTILLA_ROSTER` and `FLOTILLA_SELF` removed from the test process
- Controlled local executable named `claude` used only to hold a tmux pane; it
  reads stdin and performs no model or network work

## Commands and observed results

```text
TEST_ROOT=/tmp/flotilla-docs-cold
GOBIN=$TEST_ROOT/bin go install ./cmd/flotilla
$TEST_ROOT/bin/flotilla version
flotilla 0.0.1
```

```text
flotilla status --roster $TEST_ROOT/work/flotilla.json
flotilla status - no readable detector snapshot at $TEST_ROOT/work/flotilla-detector-state.json
Fleet - 0 of 2 seats working
infra     unknown  unknown
research  unknown  unknown
```

`unknown` is the expected cold state before the detector writes a snapshot.

```text
tmux new-session -d -s flotilla-docs-cold
tmux send-keys -t flotilla-docs-cold \
  'flotilla register infra && exec claude' Enter
registered infra (marker @flotilla_agent=infra)
```

```text
flotilla send --from me --roster $TEST_ROOT/work/flotilla.json infra \
  "Report the repository status."
flotilla: infra is busy (mid-turn) - NOT delivered after 3 retries - queued to durable outbox
QUEUED sender=me recipient=infra status=busy_outbox
```

The controlled process does not render an idle coding-agent composer, so the
correct result is durable queuing rather than a false delivery. Unit and race
tests cover confirmed delivery against the supported surface fixtures.

Finally, `flotilla help` was matched against every command family shown on
`commands.html`: send, dispatch-status, dispatch-ack, status, dash, quality,
register, resume, recycle, switch, workspace, doctrine, and goals.
