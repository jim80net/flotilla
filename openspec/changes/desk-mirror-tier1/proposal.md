## Why

The operator cannot see desk activity. Desk work happens in tmux panes (ephemeral) and never
reaches Discord — today only the XO pane mirrors, via a circumstantial Spark `Stop` hook
(`~/.claude/hooks/flotilla-xo-discord-mirror.sh`). The per-desk channels + webhooks exist and are
proven, but nothing auto-populates them, so the meta-XO is **hand-cranking** desk summaries into
the channels by hand. Every hour this is delayed is an hour the operator can't see the fleet.

This change is the **fast-tracked, minimal first cut** that opens the door: each desk auto-mirrors
its turn-final output to its own home channel, fired off the change-detector's existing
working→idle edge. It is **Tier 1** of the stratified visibility design (handoff
`.claude/handoffs/20260620-visibility-and-constitutional-skillset-design.md`). The XO/meta
synthesis tiers (2 & 3) and the installable skill-set surface are the heavier **Change B** and are
deliberately deferred — Change B's scope must not gate Change A's door.

## What Changes

- **Locate-from-outside (net-new `internal/claudestore`).** The `watch` daemon reads pane STATE,
  never a `Stop`-hook payload, so it must locate the transcript itself. `LatestTurnText(pane)`:
  resolve pane cwd (`deliver.PaneCWD`) → encode (every `/` and `.` → `-`) → glob
  `~/.claude/projects/<encoded-cwd>/*.jsonl` → newest by mtime. **Verified by
  live probe (2026-06-20):** the encoding holds for every live desk; dirs hold multiple sessions
  (newest-mtime = active).
- **Extraction (PORT the XO hook, do NOT re-derive).** The hook is the working reference for the
  hard turn-final extraction (the 4 bug-fixes). Walk the JSONL in reverse to the last
  `type=="assistant"` entry with a non-empty `text` content block; concatenate its `text` blocks
  (skip `thinking`/`tool_use`); handle `content` as list OR string; **skip `isSidechain` entries**
  (subagent output, not the desk's own turn) and the non-message entry types
  (`system`/`attachment`/`file-history-snapshot`/…). Strip command tags
  (`<command-*>`/`<local-command-stdout>`/`<system-reminder>`) and classify substantive-vs-noise.
- **Bounded chunker (net-new in `internal/discord`).** `flotilla notify` rejects >2000 runes and
  `discord.Post` clamps; neither splits. Add a ≤1900-rune paragraph-boundary chunker the mirror
  uses (and that the generalized XO mirror + a future `notify --chunk` share).
- **The trigger (`internal/watch` detector).** Add a `MirrorOnFinish func(agent string)`
  collaborator to `DetectorConfig`, invoked from `runTail` (OUTSIDE `d.mu`, like Wake/Rotate) for
  each **non-XO** desk whose state went `Working→Idle` this tick. The edge IS granularity-(b) by
  construction (a desk only settles to Idle after a completed unit of work; cold-start and
  `Working→Shell`/`Unknown` are NOT finishes). Default nil ⇒ inert (no behavior change when unset).
- **The wiring (`cmd/flotilla/watch.go`).** `MirrorOnFinish` resolves the desk's webhook
  (`secrets.Webhook(agent)` — already channel-bound), reads the turn-final
  (`claudestore.LatestTurnText`), skips noise, chunks, and posts via `discord.Post`. It is
  **observe-only + best-effort** (a mirror failure NEVER affects delivery or the tick) and writes a
  **one-line-per-decision log** (POST / SKIP-reason / CHUNK-FAIL) — the original mirror bugs
  survived for weeks because failures exited silently.

## Out of scope (deferred to Change B)

- Tiers 2 & 3 (the XO / meta-XO synthesis SKILL) and the routing accessor `ChannelsAwareOf`.
- The installable default skill/rule set + the Rule of Three.
- The roll-call announce-on-spawn (a small fast-follow; can ride Change A or a follow-up).
- Channel/webhook PROVISIONING (Change A reuses existing per-desk webhooks; provisioning is a later
  `flotilla provision` extension).
- The transcript-flush stabilization loop: the detector edge fires AFTER Idle is confirmed (later
  than the Stop hook), so the turn is already flushed; v1 reads once. The decision-log is the canary
  — if truncation shows, a minimal file-quiet re-read is the fast-follow.

## Impact

- **New capability spec:** `fleet-visibility`.
- **Affected code:** new `internal/claudestore`; `internal/discord` (chunker); `internal/surface`
  (claude driver gains `ResultReader` so `flotilla result` works for claude too, sharing the seam);
  `internal/watch/detector.go` (the `MirrorOnFinish` side-effect); `cmd/flotilla/watch.go` (wiring).
- **Risk:** LOW — additive + inert-by-default + observe-only (never touches the delivery or tick
  path). Touches the detector tail, so the trio confirms the side-effect runs outside `d.mu` and
  never stalls the clock.
