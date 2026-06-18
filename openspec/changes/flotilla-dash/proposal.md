## Why

flotilla is driven from a chat channel today (Discord) and inspected from the CLI
(`flotilla status` — `cmd/flotilla/status.go`). Both are excellent for what they
are, but two operator needs are under-served:

1. **Command-and-control (cnc) at a glance.** The live fleet picture is a
   one-line-per-desk text table (`flotilla status`) or a stream of Discord
   messages. There is no *spatial* surface that shows the whole fleet, the
   **federation topology** (the channel↔XO bindings from F#105 —
   `internal/roster/roster.go:288` `Bindings()`), the coordination history (the
   CoS who-knows-what ledger, `internal/cos/ledger.go`), and the work queue (the
   goal-loop backlog, `internal/backlog/backlog.go`) together — and lets the
   operator *act* (route an instruction, wake a desk, resume a crashed one)
   without composing a Discord message. Issue #102 named this gap directly:
   "DRIVING the fleet … and REPORTING back (underdeveloped)."

2. **A first-class issue/idea tracker.** The XO already files operator ideas as
   GitHub issues (the visible idea backlog — see #103's "Why now"), but the
   operator reads and triages them on github.com, away from the fleet they
   belong to. There is no native flotilla surface that puts the idea/issue
   backlog next to the fleet doing the work.

This change designs **flotilla-dash**: a native, optional, **pluggable web
interface** served by the `flotilla` binary itself (`flotilla dash`), covering
**(a) fleet command-and-control** and **(b) a native, GitHub-backed issue
tracker.** It is the "pluggable dash" direction the operator chose for #106:
> *(operator 2026-06-18)* chose **(A) pluggable dash** … the dash is
> UNCONSTRAINED — it can be a proper web dashboard (a small server is fine),
> still framed as an optional/pluggable interface alongside CLI + Discord + voice.

It is, by construction, a **presentation layer over mechanisms flotilla already
has** (#106's framing) — it does not invent a second source of fleet truth.

## What Changes

- **A new `flotilla dash` subcommand** that starts a small local HTTP server
  (Go `net/http` + `html/template` + `embed.FS`; vanilla JS, no build step —
  matching the existing `site/` ethos). It is served by the existing `flotilla`
  binary as one more long-running subcommand alongside `watch` and `voice` (no
  separate binary; no external service to operate).
- **A read model that consumes the artifacts `flotilla watch` already writes** —
  the detector snapshot (`flotilla-detector-state.json`,
  `internal/watch/snapshot.go`), the XO liveness ack file, the roster
  (topology), the CoS ledger, and the backlog file. The dash is a **reader**;
  `watch` remains the single writer of fleet state, so the dash can never
  diverge from or double-probe the panes. (Same discipline `flotilla status`
  already follows — `cmd/flotilla/status.go:20`.)
- **cnc read views** — a live fleet board (per-desk state + freshness + XO
  liveness/settled), the federation topology (channel↔XO bindings as the org
  chart), and the coordination history (the CoS ledger + backlog drive-queue).
  These views are the presentation layer issue #102 asked for.
- **cnc control actions** — route an instruction to an XO/desk, post an operator
  message, and resume a crashed desk, all through the **existing confirmed-
  delivery library** (`internal/surface.Confirm.Submit`, the same path
  `cmd/flotilla/send.go`→`cmdSend` and `flotilla resume` use) — never raw
  `tmux send-keys`. Control is **fail-closed**: loopback-only by default; a
  non-loopback bind REFUSES to start without a configured bearer token.
- **A native, GitHub-backed issue tracker** — list / view / create / comment /
  label / close issues, surfaced natively in the dash, backed by the repo's
  GitHub Issues (via the `gh` CLI already used across the project's tooling).
  This is a **dash-native feature, NOT the #103 pluggable `IssueTracker`
  abstraction** — Linear/Jira are explicitly deferred (operator 2026-06-18), so
  this change builds **no multi-tracker strategy registry**; it talks to GitHub
  directly and keeps the seam minimal (one internal package, one backend).

This change lands the **design + spec + tasks** (Phase 0). Per the standard
flow, the design is reviewed by the systems-review + open-code-review + STORM
trio; clearing that trio is the bar to proceed to implementation (operator
directive 2026-06-18). Implementation is **phased** (read cnc → tracker →
control cnc) with a checkpoint at each phase boundary.

## Capabilities

### Added Capabilities
- `dash`: the local web server (`flotilla dash`), the read model over the
  existing watch/roster/cos/backlog artifacts, the cnc read views (fleet board,
  federation topology, coordination history), the cnc control actions (route /
  notify / resume via the confirmed-delivery library), the native GitHub-backed
  issue tracker, and the fail-closed security posture (loopback default; bearer
  token required for any non-loopback bind).

### Modified Capabilities
- None. The dash is a pure reader of `watch`/`cos`/`backlog`/`roster` artifacts
  and a pure caller of the existing `surface`/`deliver` delivery library and the
  `gh` CLI. No existing capability's mechanism, config, or contract changes.

## Impact

- **Optional + opt-in.** A fleet that never runs `flotilla dash` is byte-for-byte
  unchanged. The dash adds a subcommand and an `internal/dash` package; it does
  not touch the `send`/`notify`/`watch`/`voice`/`federation` paths.
- **Presentation over existing truth.** The dash reads the same snapshot +
  roster the rest of flotilla already produces; it introduces no second fleet-
  state store and no second pane prober.
- **Privileged surface, fail-closed.** cnc control can inject into panes and
  create GitHub issues; the security boundary is the dash's bind address + auth.
  Default loopback (host-shell trust — equivalent to who can already run
  `flotilla send`); non-loopback requires a token or the server refuses to
  start.
- **Affected surfaces (Phase 1+ build):** `cmd/flotilla/main.go` (one switch
  arm + usage), a new `cmd/flotilla/dash.go` (flag/wiring), a new `internal/dash`
  package (server, read model, control handlers, auth), an `internal/dash/tracker`
  sub-package (GitHub issues via `gh`), embedded static assets, and docs (a
  `docs/dash-runbook.md` + a README roadmap line). No migration; no new
  third-party dependency in Phase 1 (stdlib HTTP + `gh` subprocess).
- **One shared-core touchpoint (Phase 3 only, coordinated with flotilla-dev):**
  the control path requires per-pane serialization that holds ACROSS processes
  (the dash is a separate process from `flotilla watch`, so it cannot share
  watch's in-process pane mutex — see design §5). The fix generalizes that
  serialization to a **cross-process pane-transaction lock** in
  `internal/deliver`, acquired by every confirmed-delivery caller AND the
  detector's context-rotate. This also hardens a latent `flotilla send`-vs-`watch`
  race that exists today. It is a small, additive core change, gated to Phase 3
  and coordinated with the core (flotilla-dev) lane — NOT built in the read-only
  Phase 1.
- **Relationship to sibling issues:** the dash's read/reporting views ARE the
  first-class reporting surface #102 asked for (the periodic *push* digest stays
  with the XO/`notify` path). The native tracker is NOT #103 (no pluggable multi-
  tracker abstraction is built here); if #103 ever lands, the dash's GitHub
  backend can be re-expressed behind that interface without changing the UI.
