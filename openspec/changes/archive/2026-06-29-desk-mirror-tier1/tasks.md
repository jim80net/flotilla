# Tasks — desk-mirror-tier1 (the visibility door)

## 1. `internal/claudestore` — locate + extract (TDD)

- [x] 1.1 `encodeProjectDir(cwd) string` — every `/` and `.` → `-`. TEST against the probe-verified
      pairs (e.g. `/home/operator/workspace/github.com/jim80net/flotilla-dash` →
      `-home-operator-workspace-github-com-jim80net-flotilla-dash`).
- [x] 1.2 `LatestSession(cwd) (path string, ok bool)` — glob `~/.claude/projects/<enc>/*.jsonl`,
      newest by mtime; ok=false when the dir/glob is empty. TEST with a temp `$HOME` fixture +
      multiple jsonl with controlled mtimes (relative, not hardcoded dates).
- [x] 1.3 `lastTurnText(jsonlPath) (string, ok bool)` — reverse-walk to the last `type=="assistant"`
      entry with a non-empty `text` block; concat its `text` blocks (skip `thinking`/`tool_use`);
      handle `content` list OR string; SKIP `isSidechain==true` and non-message entry types. TEST
      with a fixture transcript that ends with tool_result/system/attachment AFTER the last assistant
      text (proves the walk-back), a sidechain entry (proves it's skipped), and a thinking+text
      assistant entry (proves only text is taken).
- [x] 1.4 `stripAndClassify(text) (clean string, substantive bool)` — strip
      `<command-*>`/`<local-command-stdout>`/`<local-command-caveat>`/`<system-reminder>` tags;
      substantive=false when the residue is empty/whitespace. TEST the command-tag-poisoning case.
- [x] 1.5 `LatestTurnText(pane) (text string, ok bool, err error)` — compose 1.1–1.4 via
      `deliver.PaneCWD(pane)`. ok=false when no session / no substantive turn-final.

## 2. `internal/discord` — bounded chunker (TDD)

- [x] 2.1 `ChunkContent(text string, limit int) []string` — split on paragraph (`\n\n`) boundaries;
      hard-split any single paragraph longer than `limit`; never exceed `limit` runes per chunk.
      TEST: short text → 1 chunk; multi-paragraph over limit → N chunks each ≤ limit, order
      preserved; a single >limit paragraph → hard-split. Use runes, not bytes (parity with
      `MaxContentRunes`).

## 3. `internal/surface` — claude `ResultReader` (share the seam)

- [x] 3.1 Implement `surface.ResultReader` on the claude-code driver via
      `claudestore.LatestTurnText`, so `flotilla result <claude-desk>` works (only grok has it
      today) and the auto-mirror + the CLI read through one path. TEST the interface assertion.

## 4. `internal/watch` detector — the `MirrorOnFinish` side-effect (TDD)

- [x] 4.1 Add `MirrorOnFinish func(agent string)` to `DetectorConfig` (default nil ⇒ inert). In
      `tickLocked`, record each NON-XO desk whose `prev==Working && cur==Idle` this tick; in
      `runTail`, invoke `MirrorOnFinish` for each (OUTSIDE `d.mu`). Suppress on cold start; only
      `Working→Idle` (NOT Shell/Unknown) is a finish. TEST: a desk Working→Idle ⇒ exactly one
      MirrorOnFinish(desk); the XO is NOT mirrored (it has its own path); cold start ⇒ none;
      Working→Shell ⇒ none; runs in the tail (does not block OperatorWake — extend the existing
      tail-outside-mu test pattern).

## 5. `cmd/flotilla/watch.go` — wiring (best-effort, observe-only, logged)

- [x] 5.1 Wire `MirrorOnFinish`: `secrets.Webhook(agent)` (skip if none) → `claudestore.LatestTurnText`
      → skip if not substantive → `discord.ChunkContent` → `discord.Post` per chunk under the desk
      identity. NEVER errors upward (observe-only). One journald line per decision
      (POST n-chunks / SKIP:<reason> / CHUNK-FAIL). Gate behind roster config (default-on once a
      webhook resolves; a per-agent opt-out).

## 6. Verify + gate

- [x] 6.1 `go build ./... && go test ./... -race` green; `go vet` clean.
- [x] 6.2 `openspec validate desk-mirror-tier1 --strict`.
- [ ] 6.3 Trio (systems-review + open-code-review + STORM) on the implementation — confirm the
      side-effect runs outside `d.mu`, never stalls the clock, never affects delivery; the extraction
      matches the hook's bug-fixes; the chunker is correct.
- [ ] 6.4 PR; CI green; cubic via GraphQL `reviewThreads.isResolved` (author cubic-dev-ai). Report
      trio-clean → the primary XO merges the moment it lands.
