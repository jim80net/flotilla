# Public quickstart cold-test transcript

Artifact binding: the immutable Git commit containing this transcript. The run
used only public-example names, a fresh temporary root, tmux, and a controlled
network-free terminal fixture. It made no provider or Discord call.

## Environment and control

- Fresh temporary `GOBIN` and work directory
- `FLOTILLA_ROSTER` and `FLOTILLA_SELF` removed from tested CLI processes
- The exact two-agent JSON shown on `quickstart.html`
- `site/docs/testdata/coldagent` built with the output name `claude`; it accepts
  terminal input and returns to a cleared Claude Code-style composer, without a
  model, credential, session store, or network access

## 1. Install and make both tested executables reachable

```text
TEST_ROOT=$(mktemp -d /tmp/flotilla-docs-cold-v3.XXXXXX)
mkdir -p "$TEST_ROOT/bin" "$TEST_ROOT/work"
export PATH="$TEST_ROOT/bin:$PATH"
GOBIN="$TEST_ROOT/bin" go install ./cmd/flotilla
go build -o "$TEST_ROOT/bin/claude" ./site/docs/testdata/coldagent
flotilla version
flotilla 0.0.1
```

The earlier transcript installed into a fresh `GOBIN` but did not add that
directory to `PATH`. This run exports it before invoking either executable.

## 2. Define the two sessions exactly as published

`$TEST_ROOT/work/flotilla.json` contained:

```json
{
  "agents": [
    {"name": "infra"},
    {"name": "research"}
  ]
}
```

No private roster or environment value was copied into the fixture.

## 3. Verify the honest cold state

```text
flotilla status --roster "$TEST_ROOT/work/flotilla.json"
flotilla status - no readable detector snapshot at $TEST_ROOT/work/flotilla-detector-state.json
Fleet - 0 of 2 seats working
infra     unknown  unknown
research  unknown  unknown
```

`unknown` is the expected state before the detector writes a snapshot.

## 4. Run the published tmux and registration sequence

The test shell was started in `$TEST_ROOT/work` with the exported `PATH`, then
ran the same two commands as the page, using a unique session name:

```text
tmux new-session -d -s flotilla-docs-cold-v3
tmux send-keys -t flotilla-docs-cold-v3 \
  'flotilla register infra && exec claude' Enter
registered infra (marker @flotilla_agent=infra)
pane_current_command=claude
```

The first control process used a shell-like display and was correctly treated
as busy. It was replaced with the committed fixture above, which exercises the
real Claude Code surface probe rather than weakening delivery to a shell.

## 5. Deliver and verify the first instruction

From `$TEST_ROOT/work`:

```text
flotilla send --from me --roster ./flotilla.json infra \
  "Report the repository status."
delivered to infra (pane flotilla-docs-cold-v3:0.0) — turn confirmed
```

The pane contained `accepted: Report the repository status.` and returned to a
cleared composer. The server-minted nonce was then checked independently:

```text
flotilla dispatch-status --roster ./flotilla.json flotilla-dispatch-4d1dfdc4
nonce=flotilla-dispatch-4d1dfdc4 disposition=delivered me→infra id=58737014fa1af6fc inbound pending durable ack
```

This closes the earlier evidence gap: the cold run ends in confirmed delivery
with a durable inbound record, not `busy_outbox`.

## Command-reference existence check

The same cold-built `flotilla help` contained every family shown on
`commands.html`: `send`, `dispatch-status`, `dispatch-ack`, `status`, `dash`,
`quality`, `register`, `resume`, `recycle`, `switch`, `workspace`, `doctrine`,
and `goals`.
