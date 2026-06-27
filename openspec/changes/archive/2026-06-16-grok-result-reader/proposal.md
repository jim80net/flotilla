## Why

`desk-e` (xAI's official grok CLI) is a PULL-ONLY desk: the XO reads it by capturing its
pane, but `tmux capture-pane` returns only the VISIBLE tail. A long grok turn (an 11-minute
research result) scrolls off — the XO can't read the full result from the pane. The full,
canonical result lives in grok's structured session store
(`~/.grok/sessions/<cwd>/<session>/chat_history.jsonl`, the last `assistant` entry — verified
10,287 chars for one research turn). #58 part A made the detector correctly ASSESS grok; part B makes
the desk's full output READABLE so it is a fully-coordinated pull desk.

## What Changes

- **`ResultReader` — an optional driver capability** (`internal/surface`): a `Driver` MAY implement
  `LatestResult(pane string) (string, error)` to return the full text of the desk's latest
  completed turn from its harness session store (when the pane capture is insufficient). Drivers
  that don't implement it are unaffected (the XO falls back to capture-pane).
- **The grok driver implements `ResultReader`** via a new `internal/grokstore` reader: resolve the
  pane's cwd, find the active grok session for that cwd in `~/.grok/active_sessions.json`, and
  return the last `assistant` entry's content from that session's `chat_history.jsonl`. The store
  parsing (the grok-specific format) is the circumstantial part; the interface + command are the
  generalizable flotilla capability ("read a desk's full latest result from its harness session
  store").
- **`flotilla result <agent>`** — a new read-only command: resolve the agent's driver + pane; if the
  driver is a `ResultReader`, print the full latest result; otherwise report that the surface has
  no session-store reader (use the pane capture). Read-only — it never writes a pane.
- **`deliver.PaneCWD(pane)`** — a small primitive returning a pane's current working directory
  (`tmux display-message #{pane_current_path}`), used to key the grok session store.

## Non-Goals

- A live/streaming tail of an in-progress turn — this reads the latest COMPLETED result (the
  detector's `Assess` already reports working/idle).
- A sqlite (`grok.db`) reader — the JSONL store is sufficient and adds no CGO/sqlite dependency.
- Auto-relaying grok results to the operator — `flotilla result` is a read; the XO decides what to
  relay (the XO remains the sole Discord identity).
- The grok `AwaitingApproval` gate markers / multi-line submit — tracked #58 follow-ups.
