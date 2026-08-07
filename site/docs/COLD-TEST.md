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
- Absolute candidate and fixture paths injected into tmux, so an existing tmux
  server's inherited `PATH` cannot select a host executable

## 1. Install and make both tested executables reachable

```text
TEST_ROOT=$(mktemp -d /tmp/flotilla-docs-cold-v4.XXXXXX)
mkdir -p "$TEST_ROOT/bin" "$TEST_ROOT/work"
export PATH="$TEST_ROOT/bin:$PATH"
GOBIN="$TEST_ROOT/bin" go install ./cmd/flotilla
go build -o "$TEST_ROOT/bin/claude" ./site/docs/testdata/coldagent
"$TEST_ROOT/bin/flotilla" version
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

## 4. Run an isolated variant of the published registration sequence

The public page uses bare `flotilla` and `claude` names after its `PATH`
prerequisite. That is appropriate for a user install but is not a sufficient
test isolation boundary: an existing tmux server can retain an older `PATH`.
This controlled run therefore injected the candidate and fixture by their
concrete absolute paths. It does not claim that this is the page's exact
bare-name command.

```text
TEST_ROOT=/tmp/flotilla-docs-cold-v4.MqVkWN
tmux new-session -d -s flotilla-docs-cold-v4 -c "$TEST_ROOT/work"
tmux send-keys -t flotilla-docs-cold-v4 \
  "$TEST_ROOT/bin/flotilla register infra && exec $TEST_ROOT/bin/claude" Enter
registered infra (marker @flotilla_agent=infra)
pane_current_command=claude
pane_pid=841216
readlink -f /proc/841216/exe
/tmp/flotilla-docs-cold-v4.MqVkWN/bin/claude
Claude Code cold-test fixture
❯
```

The resolved executable identity and fixture-only banner distinguish the
committed network-free fixture from a real provider process that happens to be
named `claude`. The fixture exercises the real Claude Code surface probe rather
than weakening delivery to a shell.

## 5. Deliver and verify the first instruction

From `$TEST_ROOT/work`, the candidate CLI was again invoked by absolute path:

```text
"$TEST_ROOT/bin/flotilla" send --from me --roster "$TEST_ROOT/work/flotilla.json" infra \
  "Report the repository status."
delivered to infra (pane flotilla-docs-cold-v4:0.0) — turn confirmed
```

The same pane whose executable resolved to the fixture printed
`accepted: Report the repository status.` and returned to a cleared composer.
The server-minted nonce was then checked with the same absolute candidate:

```text
"$TEST_ROOT/bin/flotilla" dispatch-status --roster "$TEST_ROOT/work/flotilla.json" flotilla-dispatch-21ffdcdd
nonce=flotilla-dispatch-21ffdcdd disposition=delivered me→infra id=b4ff2cbf25cd3ad4 inbound pending durable ack
```

This closes the earlier evidence gap: the cold run ends in confirmed delivery
with a durable inbound record, not `busy_outbox`.

## Command-reference existence check

The same cold-built `flotilla help` contained every family shown on
`commands.html`: `send`, `dispatch-status`, `dispatch-ack`, `status`, `dash`,
`quality`, `register`, `resume`, `recycle`, `switch`, `workspace`, `doctrine`,
and `goals`.
