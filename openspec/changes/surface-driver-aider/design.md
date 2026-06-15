# Design: surface-driver Phase 2 — the `aider` driver (full state set, drop-in second harness)

## The interface being implemented (cite)

`internal/surface/surface.go:61-73`:

```go
type Driver interface {
    Name() string
    Submit(pane, text string) error  // inject one turn (per-surface keystrokes)
    Assess(pane string) State        // resolve rendered state (captures pane itself)
    Rotate(pane string) error        // SlashCommand drivers inject the reset
    RotateStrategy() Strategy        // SlashCommand vs RestartProcess
}
```

`State` (`surface.go:14-22`) spans
`Unknown/Shell/Working/Idle/AwaitingInput/AwaitingApproval/Errored`. The
claude-code driver (`internal/surface/claude.go:59-77`) emits only the first
four non-reserved states (`Shell`/`Working`/`Idle`, never `AwaitingApproval`/
`Errored`). The aider driver is the **first to emit the full set** — that is the
load-bearing contribution of this change.

The registry (`surface.go:78-90`) is populated by each driver's `init()` calling
`Register`; `RotateContext` (`surface.go:101-106`) enforces the
no-slash-into-`RestartProcess` guard. The change-detector's materiality gate
(`internal/watch/materiality.go:24-32`) already routes
`AwaitingApproval`/`Errored` as actionable entries — wired but dormant ("activate
automatically when a driver begins emitting them — no dead mandated branch",
materiality.go:21-23). This change makes them live.

---

## Harness survey (code-level — file:line, not README)

Three candidates were evaluated by reading their actual source (open) or official
docs (closed), against three criteria: (a) tmux-pane TUI with a pastable
composer; (b) the full `State` set parseable from rendered pane text — especially
the dormant `AwaitingApproval`/`Errored`; (c) creds + metered spend to run it.

### Aider — `github.com/Aider-AI/aider` (open, Python) — **CHOSEN**

- **(a) TUI + composer:** `prompt_toolkit` `PromptSession` (`aider/io.py:358`),
  REPL loop `base_coder.py:876-892` (`get_input()`→`io.get_input()`,
  io.py:523-666), bracketed paste on by default, plus a `{`/`}` multi-line marker
  (io.py:694-727). Persistent composer. ✅
- **(b) full state set:**
  - **Idle** — a `─` rule (io.py:509-514) then a prompt line ending in `> `
    (or `ask> `/`architect> `/`multi> `), io.py:545-552. Deterministic.
  - **Working** — `WaitingSpinner` text `"Waiting for " + model`
    (base_coder.py:1440), scanner glyphs `░█`/`=#` (waiting.py:64); auto-retry
    shows `Retrying in N seconds...` (base_coder.py:1486).
  - **AwaitingApproval** — **single chokepoint** `io.confirm_ask()`
    (io.py:806-925) emits the invariant token `(Y)es/(N)o` (io.py:832) for EVERY
    confirmation: `Add file to the chat?` (base_coder.py:1772), `Run shell
    command?` (base_coder.py:2455, `explicit_yes_required=True`), `Allow edits to
    file…?` (base_coder.py:2226), `Create new file?` (base_coder.py:2207),
    `Edit the files?` (architect_coder.py:17), `Try to proceed anyway?`
    (base_coder.py:1415), etc. **The cleanest approval surface of the three.**
  - **Errored** — `io.tool_error()` renders **red** (io.py:284-285) — color, not
    a keyword; API errors carry the `exceptions.py:13-57` descriptions
    (`Check your API key`, `rate limited`, …); fatal uncaught → report.py:145
    banner `An uncaught exception occurred:` then process exit to shell.
  - **Shell-crash** — process exit → bare shell (`deliver.IsShell`).
- **(c) spend:** LiteLLM backend → any provider; **local ollama = $0**
  (models.py:931-932); BYO key for paid models. Build + live-capture cost **$0**.
- **Reset:** `/clear` (commands.py:411-415, prints `All chat history cleared.`)
  and `/reset` — in-process. → **SlashCommand**, not RestartProcess.

### Cursor `cursor-agent` (closed) — rejected for Phase 2

- (a) ✅ persistent composer (docs `cursor.com/docs/cli/using`); also a headless
  `--print --output-format stream-json` mode with `tool_call`/`result` events.
- (b) **closed source → every TUI render signature is UNVERIFIED, live-capture
  only.** The structured stream is strong for working/done/tool-rejected but has
  **no first-class approval event** and signals errors only via exit code — so the
  dormant `AwaitingApproval` state has no clean representation on either path.
- (c) Cursor subscription; Auto mode "included," frontier models draw a pool.
  Free tier unconfirmed. → spend decision required.
- Reset: `/new-chat` (SlashCommand).

### Grok CLI `superagent-ai/grok-cli` (`grok-dev` v1.1.7, open, TS/OpenTUI) — rejected for Phase 2

- (a) ✅ OpenTUI `<textarea>` composer (app.tsx:3918-3939), bracketed paste
  (app.tsx:3344-3356).
- (b) Idle/Working are cleanly classifiable (`Message Grok...`,
  `Planning next moves`, spinner `⬒⬔⬓⬕`, `esc to interrupt`). **But it
  auto-executes tools — there is NO per-edit/per-shell approval prompt** (only a
  crypto-payment `Payment required` panel, app.tsx:5635, and a Plan-mode
  `Confirm` tab). The dormant `AwaitingApproval` state is essentially absent —
  and unprompted shell execution is itself a safety concern for a fleet desk.
  Errored is inline with no marker (must regex specific `STATUS_MESSAGES`).
- (c) **paid xAI credits, no free tier** (client.ts:46, default `grok-4.3`). →
  spend decision required for any live capture.
- Reset: `/new` (SlashCommand).

### Verdict

Aider is the most tractable on every load-bearing axis: source-verifiable full
state set, the cleanest approval surface (exactly the dormant state we light up),
and $0 build+validation. It is also a `SlashCommand` reset surface, so it does
not exercise the `RestartProcess` guard — that path stays proven by Phase 1's
stub test and will get a real exercise when cursor/grok land (Phase 3+).

---

## The `aider` driver (`internal/surface/aider.go`)

Mirrors the claude-code driver's injectable-primitive pattern (claude.go:15-33)
so the state-mapping is unit-testable without a live tmux server.

```go
package surface

func init() { Register(newAider()) }

type aider struct {
    paneCommand func(string) (string, error) // deliver.PaneCommand
    isShell     func(string) bool            // deliver.IsShell
    capturePane func(string) (string, error) // deliver.CapturePane
    classify    func(string) State           // parseAiderState (pure, tail-scoped)
    send        func(string, string) error   // deliver.Send
    inject      func(string, string) error   // deliver.InjectSlash
}

func (aider) Name() string                    { return "aider" }
func (a aider) Submit(pane, text string) error { return a.send(pane, text) }   // bracketed paste + Enter
func (a aider) Rotate(pane string) error       { return a.inject(pane, "/clear") }
func (aider) RotateStrategy() Strategy          { return SlashCommand }
```

### `Submit` — reuse `deliver.Send`

Aider's `prompt_toolkit` composer enables bracketed paste by default, so the
existing `deliver.Send` (bracketed paste of the body + a single submitting Enter,
tmux.go:271-308) is the right submission method — identical mechanism to
claude-code. **Live-capture task** confirms multi-line paste lands as literal
newlines in aider's composer (the `deliver.Send` caveat at tmux.go:266-270 — only
bracketed-paste-mode targets get literal newlines — applies; prompt_toolkit
qualifies, but it is confirmed on the host, not assumed).

### `Assess` — the full state set, tail-scoped, IDLE-POSITIVE (the hard part)

```
Assess(pane):
  cmd, err := paneCommand(pane)
  if err != nil:        return StateUnknown   // transient tmux glitch, NOT a crash (mirror claude.go:60-64)
  if isShell(cmd):      return StateShell      // process gone (incl. report.py fatal exit)
  captured, err := capturePane(pane)
  if err != nil:        return StateUnknown      // glitch ⇒ Unknown (non-material), NOT a false "finished" (see polarity note)
  return classify(captured)                     // pure, tail-scoped marker ladder
```

`classify` (pure `parseAiderState(string) State`, the testable core — the
aider analogue of `deliver.ParseBusy`) scopes its scan to the **bottom ~12 lines**
of the capture, exactly as `ParseBusy` scopes to the tail (busy.go:42-44). This
is the load-bearing discipline: aider prints errors and approvals into the
scrollback and then returns to a prompt, so a whole-buffer scan would
false-positive on a stale string. Only the live bottom region decides state.

**The polarity inversion vs claude-code (the load-bearing design decision).**
claude-code's `Assess` makes **Working** the positively-detected state and
**Idle** the default (claude.go:73-76), because Claude's working marker
(`esc to interrupt` / `(Ns ·`, busy.go:19,46) is present the ENTIRE turn — so
"no working marker" reliably means idle. **Aider is the opposite:** its
`Waiting for <model>` spinner STOPS once tokens arrive (base_coder.py:1837-1838),
and the streaming-markdown phase shows neither a working marker nor a `> ` prompt.
If Idle were the default, a mid-stream aider pane would classify Idle → the
detector would read `Working→Idle` = "finished a turn" (materiality.go:51) and
**wake the XO prematurely, mid-stream.** Therefore the aider driver makes **Idle
the positively-detected state (the prompt is back) and Working the default** — a
readable pane that is not at its prompt, not awaiting approval, and not showing a
live error is presumed still working. This is precisely the per-surface variation
the driver abstraction exists to capture, and a key input to the Phase-3
externalization (idle-polarity is a per-driver attribute).

**Precedence ladder (highest first), with rationale:**

1. **`AwaitingApproval`** — the tail contains the approval token `(Y)es/(N)o`
   (io.py:832). Wins over everything: an open approval prompt is the live state
   and demands XO action. (This is the dormant state, now emitted.)
2. **`Idle`** — the tail's **last non-empty line** is a recognized prompt
   (`> `/`ask> `/`architect> `/`multi> `, io.py:545-552). POSITIVE detection: the
   desk is "finished" only when its prompt has returned. (A prompt below an error
   means recovered → Idle wins over Errored, which is why Idle precedes it.)
3. **`Errored`** — the tail contains a known non-retryable error phrase from the
   `exceptions.py` set (e.g. `Check your API key`, an auth-failure description) OR
   the `An uncaught exception occurred:` banner (report.py:145) AND the last line
   is NOT a prompt. Best-effort and narrow by honest design: most aider errors
   resolve to `Shell` (fatal), `Working` (auto-retry), `AwaitingApproval`
   (`Try to proceed anyway?`), or `Idle` (recovered), so `Errored` only catches a
   recognized error that is the LIVE bottom state at sample time. Durable error
   capture (tailing aider's `.aider.chat.history.md` or a `--verbose` log) is a
   Phase-3 option, noted in out-of-scope.
4. **`Working`** — the DEFAULT: none of the above matched (mid-stream, the
   pre-stream `Waiting for <model>` spinner, an `Retrying in N seconds...`
   auto-retry, or any non-prompt non-error state). Auto-retry is deliberately
   Working, not Errored — it is self-healing, and `anything→Working` is
   non-material (materiality.go:43), so a retrying desk stays silent.

This idle-positive ladder also means the driver depends on NEITHER the ambiguous
shading glyphs (`░`/`█`/`=`/`#`, which collide with diffs/tables/progress bars)
NOR the `\r`-rewritten TTY-gated spinner (waiting.py:38,144) — only on the stable
prompt, approval-token, and error-phrase strings. The single thing live-capture
MUST pin precisely is the **prompt regex** (the Idle anchor); getting it wrong
fails SAFE toward Working (a stuck-looking desk is noticed) rather than toward a
spurious "finished" wake.

**Honesty about the markers:** the strings above are derived from aider source at
the cited lines, NOT yet observed inside flotilla's tmux PTY. The spec scenarios
are written against the CONTRACT (given a pane showing X → state Y) and tested
with source-derived fixtures; a dedicated **live-capture task** (§ tasks 4) runs
aider against local ollama ($0), records real pane frames for each state, and
locks the fixtures/regexes — above all the prompt (Idle) anchor — to what
actually renders. One remaining soft spot the live capture confirms: aider's
error signal is partly ANSI-red (io.py:284-285) and `tmux capture-pane -p` strips
escapes (busy.go:26), so the plain-text phrase-list is the Errored detector here;
full color-aware capture (an `-e` variant) is a noted Phase-3 refinement, not
required because the phrase-list covers the actionable cases (auth, rate-limit,
uncaught banner).

### `Rotate` — `/clear`, via the generalized injector

Aider's reset `/clear` (commands.py:411) is, by coincidence, the same literal as
claude-code's. Rather than reuse the claude-specific `deliver.ClearContext`
(whose doc binds it to Claude), generalize the primitive:

- Add `deliver.InjectSlash(target, cmd string) error` — the current body of
  `ClearContext` (literal `send-keys -l -- <cmd>`, compose delay, Enter;
  tmux.go:76-99), parameterized on the command, under the same per-pane lock.
- Re-express `deliver.ClearContext(target)` as `InjectSlash(target, "/clear")`
  so the claude-code driver's call and tests stay **byte-identical**
  (`clearKeysArgs` already takes the cmd, tmux.go:65 — only `ClearContext`
  hard-codes `/clear`).
- The aider driver's `inject` field is `deliver.InjectSlash`; `Rotate` calls
  `inject(pane, "/clear")`.

This is the elegant foundational move: the reset command becomes
driver-declared, so driver #3 (grok `/new`, cursor `/new-chat`) needs no further
deliver change. `RotateStrategy()` is `SlashCommand`, so `RotateContext`
(surface.go:101-106) routes to `Rotate` and injects — unchanged.

---

## Lighting up the dormant escalation (no watch change)

With the aider driver emitting `AwaitingApproval`/`Errored`, the existing
materiality gate fires for aider desks automatically:

- `material(prev, AwaitingApproval)` → `actionableEntry` true (materiality.go:26)
  → wakes the XO ("entered awaiting-approval"). An aider desk blocked on
  `Run shell command? (Y)es/(N)o` now surfaces to the XO.
- `material(prev, Errored)` → actionable → wakes the XO ("entered errored").
- `Working→Idle` ("finished a turn") already fires (materiality.go:51) — aider's
  turn completion surfaces exactly like claude-code's.

No code in `internal/watch` changes; the spec asserts the end-to-end behavior as
a scenario so the wiring is regression-locked.

---

## Why externalization is Phase 3 (not this change)

The registry today (`surface.go:78-90`) is an in-process `map` filled by `init()`.
"Externalizing" it could mean **config-declared** drivers (a data blob of
per-state regexes + reset command interpreted by one generic driver) or
**out-of-process** drivers (an LSP-style subprocess protocol). Both are deferred,
for first-principles reasons:

1. **N=1 cannot define the variation surface.** Until Aider exists in-tree, the
   only data point is Claude. The real axes of variation — tail-scoped
   multi-state classification, phrase/ANSI error detection Claude never needed,
   the approval-token scan, retry-as-Working — only become visible with a second
   concrete driver. A config schema or plugin protocol designed against N=2 is
   honest; against N=1 it is a guess that would be rebuilt.
2. **The in-tree Aider driver is not throwaway.** It becomes the permanent
   reference / test oracle that any future config-driven driver must match
   byte-for-byte. No rework is created by doing it in Go first.
3. **Out-of-process is a large, separate concern** (subprocess protocol,
   lifecycle, security boundary, version skew). Folding it in would couple two
   unrelated risks and balloon the blast radius. It deserves its own openspec
   change and its own review gates.
4. **Cost/benefit today favors in-tree.** Adding the next 1-2 drivers in-tree is
   ~one file each; externalization pays off at N≥4 or when a third party must add
   a harness without a PR — neither is the current need.

Phase 2 deliberately factors `parseAiderState` as a pure, self-contained state
classifier so that Phase 3 can lift it (and claude's) into whatever
externalization shape the N=2 evidence recommends.

---

## Test plan (TDD)

1. **`deliver.InjectSlash`**: arg test — `send-keys -l -- <cmd>` then Enter for an
   arbitrary cmd; `ClearContext` still issues exactly `/clear` (claude byte-identity
   regression).
2. **`parseAiderState` (table-driven, the core)** — EVERY branch, tail-scoped:
   - `(Y)es/(N)o` approval prompt at bottom → `AwaitingApproval`
   - `Run shell command? … (Y)es/(N)o` → `AwaitingApproval`
   - **stale** `(Y)es/(N)o` in scrollback + `> ` prompt at bottom → `Idle`
     (proves tail-scoping)
   - `Waiting for <model>` / scanner glyphs / `Retrying in N seconds...` → `Working`
   - `Check your API key` (no prompt below, not retrying) → `Errored`
   - **stale** error phrase in scrollback + `> ` at bottom → `Idle`
   - `> ` / `ask> ` / `architect> ` / `multi> ` → `Idle`
   - empty / unrecognized → `Idle` (fail-open)
3. **`aider.Assess`** (stubbed paneCommand/isShell/capturePane + real
   `parseAiderState`): panecommand-error→`Unknown`; shell→`Shell`;
   capture-error→`Idle`; then the classifier cases above route through.
4. **Live-capture confirmation (build gate, $0 local ollama):** run an aider desk
   in a tmux pane against `ollama_chat/<model>`; capture real frames for
   idle/working/awaiting-approval/errored/crash; confirm or correct the
   §2 fixtures; record the captures as the fixtures of record. (Confirms the
   spinner-in-PTY and multi-line-paste questions too.)
5. **`aider.Submit`/`Rotate`/`RotateStrategy`**: Submit routes to `deliver.Send`;
   Rotate routes to `InjectSlash(pane,"/clear")`; RotateStrategy is `SlashCommand`;
   `Register`ed as `aider` (`Get("aider")` ok).
6. **Materiality end-to-end** (watch test, no watch code change): a snapshot
   transition into `AwaitingApproval` / `Errored` for a non-XO desk produces a
   material wake reason ("entered awaiting-approval" / "entered errored").
7. **Startup validation**: a roster with `surface:"aider"` passes
   `surface.Get` and starts; `surface:"nope"` still errors (watch.go:66).
8. `gofmt`/`go vet`/`go build`/`go test -race ./...` green.

## Out of scope (Phase 3+, separate changes)

- Externalize the registry (config-declared / out-of-process) — informed by the
  N=2 evidence this change produces.
- grok + cursor drivers (paid creds + weaker/absent approval surfaces; closed
  TUI for cursor).
- ANSI-color-aware error capture (a `deliver.CapturePane -e` variant) — the
  plain-text phrase-list covers the actionable aider error cases here.
- Durable error capture (tailing aider's `.aider.chat.history.md` or a
  `--verbose` log) to catch print-then-return-to-prompt errors the live-tail scan
  misses — a Phase-3 robustness option; `Errored` is best-effort/live-only here.
- The `RestartProcess` guard's real-driver exercise (lands with cursor/grok if
  any is restart-only; Phase 1's stub test already proves the guard).
