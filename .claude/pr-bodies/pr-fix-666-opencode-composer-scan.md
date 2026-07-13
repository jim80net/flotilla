## Summary

This follow-up fixes OpenCode composer-state probing so it finds the visible composer row in the pane instead of assuming the terminal cursor is parked on that row.

The live failure mode was an idle OpenCode desk whose cursor sat elsewhere in the pane while the composer input row still rendered correctly. The probe now checks the cursor row first, then falls back to scanning the captured pane for the composer chrome and classified body. That keeps the probe fail-closed on unknown layouts while allowing idle desks to report `Cleared` when the composer is visibly present.

## Test plan

- [x] `go test ./internal/surface -run 'TestClassifyOpenCodeComposerLine|TestClassifyHiddenOpenCodeComposer|TestOpenCodeComposerStateWiring'`
- [x] `go test ./...`
