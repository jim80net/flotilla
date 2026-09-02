# GML-108 forced-switch fixture — 2026-09-02 UTC

No live coordinator pane was switched for this evidence. The injected switch fixture models
an uncooperative weekly-limit-dead Grok FROM pane for the generic `backend` example role
(`Assess=Working`, copy-mode/cursor state unreadable, no handoff file, and
`ErrNoGracefulClose`) and a named fallback launch.

Command:

```text
go test -count=1 ./cmd/flotilla -run 'TestRunSwitch(Force|Phase1Abort)|TestForcedSwitchTakeoverTurn|TestParseSwitchArgs' -v
```

Relevant result:

```text
=== RUN   TestRunSwitchForceBypassesUncooperativeFromHandoff
flotilla: switch: WARNING — forcing "backend" from grok to claude-code without a cooperative FROM handoff; in-flight context may be lost
flotilla: switch: "backend" FROM surface "grok" has no graceful close — using the operator-authorized --force respawn-kill; in-flight context may be lost
--- PASS: TestRunSwitchForceBypassesUncooperativeFromHandoff (0.00s)
=== RUN   TestForcedSwitchTakeoverTurnDoesNotReferenceMissingHandoff
--- PASS: TestForcedSwitchTakeoverTurnDoesNotReferenceMissingHandoff (0.00s)
=== RUN   TestRunSwitchPhase1Abort
--- PASS: TestRunSwitchPhase1Abort (0.00s)
```

The forced fixture asserts that the pane respawns with the exact named TO launch and receives
only the post-relaunch fresh-start instruction. The existing Phase-1 control asserts that a
missing durable handoff aborts before close or respawn, leaving the FROM pane untouched.
