# Design — flotilla-dash: a native web interface (cnc + issue tracker)

> **Status: design (Phase 0).** This change lands the proposal + design + spec +
> phased tasks. Per the standard flow it is reviewed by the systems-review +
> open-code-review + STORM trio; clearing that trio is the bar to proceed to
> implementation (operator directive 2026-06-18). The build is phased
> (read-cnc → tracker → control-cnc) with a checkpoint at each phase boundary.
>
> **The #106 fork is already resolved.** The operator chose **(A) optional
> pluggable dash** and **disavowed the no-daemon/loopback constraint** the
> original #106 "tension" rested on (a real server is fine). So this design does
> not re-litigate "web vs no-web"; it designs the pluggable dash directly.

## 1. Where we are today (grounded in the code)

- **Fleet state is a snapshot the `watch` daemon writes.** The change-detector
  persists `Snapshot{ DeskStates map[name]surface.State, SignalHash, XOSettled }`
  atomically each tick (`internal/watch/snapshot.go:23`, `Save` at `:72`). It is
  the only fleet-state artifact that survives a restart.
- **`flotilla status` is already a read-only consumer of that snapshot.** It
  loads the snapshot + ack file, emits `{ generated_at, xo, agents[] }` where
  each agent is `{ name, role?, surface, state }`, and **starts no daemon,
  resolves no panes, writes no state** (`cmd/flotilla/status.go:20-75`,
  `buildStatusJSON` at `:100`). `generated_at` is the snapshot's mtime — the
  honest "as of" (`:63`). This is the exact discipline the dash must follow.
- **The topology lives in the roster.** `Config.Bindings()` returns the
  channel↔XO bindings (`internal/roster/roster.go:288`); each `Channel` is
  `{ ChannelID, XOAgent, Members[], Role }` (`:48`). `IsXO` (`:321`) and
  `ChannelForXO` (`:340`) expose the federation org chart. The legacy single
  `channel_id`/`xo_agent` is the degenerate one-binding case.
- **Control = confirmed delivery into a pane.** `cmdSend` resolves the agent's
  surface driver, resolves the pane, and calls `surface.Confirm{…}.Submit(drv,
  pane, message)` — idle-gate → submit → confirm the Idle→Working edge → Enter
  retry — returning typed errors (`ErrBusy`/`ErrTransient`/`ErrCrashed`/
  `ErrUnconfirmed`) (`cmd/flotilla/main.go:300-317`). `flotilla notify` posts to
  the operator under a webhook (`:341`); `flotilla resume` (re)starts a dead desk
  from its host-local launch recipe (`cmd/flotilla/resume.go`). These are the
  three control verbs the dash exposes — **reused, not reinvented.**
- **Coordination history is the CoS ledger.** `cos.Append` writes one structured
  `Entry{ Time, Channel, From, To, Gist }` per operator↔XO exchange, atomic
  append, single physical line (`internal/cos/ledger.go:73`, `Line` at `:106`).
- **The work queue is the backlog.** `backlog.Parse` classifies a markdown
  "## Backlog" section into `Status{ Unblocked[], Blocked, Done, Malformed,
  Items, Found }` (`internal/backlog/backlog.go:48`); the goal-loop drives the
  `Unblocked` queue (`internal/watch/detector.go:353` `continueXO`).
- **The web ethos is already set.** `site/` is plain HTML/CSS/JS with no build
  step; `site/app.js` fetches `status.json` (real `flotilla status --json`
  output) and renders a table. The dash extends this ethos: stdlib HTTP +
  `html/template` + `embed.FS`, vanilla JS, no SPA framework, no bundler.

**The synthesis:** every datum the cnc surface needs already exists as a durable
artifact, and every control verb already exists as a tested library call. The
dash is genuinely a *presentation + action* layer — not a new subsystem with new
truth. That is the whole reason it fits.

## 2. Architecture: a reader + a thin action proxy

```
                         ┌───────────────────────────────────────────┐
   browser  ──HTTP──►    │   flotilla dash   (net/http, loopback def.) │
  (vanilla JS,           │                                             │
   SSE + fetch)          │  read model            control handlers     │
                         │  ─────────             ───────────────      │
                         │  • snapshot reader ──┐  • route  → Confirm.Submit
                         │  • roster/topology   │  • notify → discord.Post
                         │  • cos ledger        │  • resume → resume recipe
                         │  • backlog           │  (fail-closed auth gate)
                         │  • tracker (gh)      │                       │
                         └───────┬──────────────┴───────────┬──────────┘
                reads (never writes)              calls (existing libs)
                         ▼                                   ▼
   flotilla-detector-state.json   roster   cos-ledger   tmux panes (via surface/deliver)
   flotilla-xo-alive (ack)        .json    .md          GitHub Issues (via gh)
        ▲
        └── written by `flotilla watch` (the single writer — unchanged)
```

Two halves, deliberately separated:

- **Read model (always available).** Pure functions over the existing artifacts.
  No pane probing, no daemon. The dash detects fleet changes by watching the
  snapshot file's **mtime** (stat-poll, ~1 s; stdlib only — no fsnotify
  dependency in Phase 1) and pushes an SSE event when it changes; a JSON-poll
  endpoint is the fallback for clients without SSE. The read model is harmless,
  so it is available the moment the server starts, regardless of auth.
- **Control handlers (privileged, gated).** Thin proxies over the *existing*
  delivery library and `gh`. Each maps an HTTP action to exactly one library
  call and returns its typed result. Control is the only thing the auth gate
  protects (read is host-shell-trust on loopback; see §7).

### Why read the snapshot rather than run a probe

Running a second pane prober in the dash would (a) duplicate the detector's
debounce/shell-streak logic (`internal/watch/detector.go:442` `debounce`), (b)
double the tmux `capture-pane` load, and (c) create two sources of fleet truth
that can disagree. `flotilla status` already proved the reader pattern is
sufficient and honest (it surfaces snapshot age so a stale read is never shown as
live — `cmd/flotilla/status.go:131`). The dash inherits that exactly: every view
that shows desk state shows the snapshot's `generated_at` age.

**Consequence (named, not hidden), with a THREE-STATE freshness model.** The
distinction the operator needs at the exact moment they open the dash is *which*
no-fresh-data case they are in. The board therefore distinguishes three states,
not two (this is sharper than `status`'s binary present/absent at
`cmd/flotilla/status.go:137`):

1. **ABSENT** — no snapshot file at all (`watch --change_detector` never ran on
   this roster dir). Banner: "no detector snapshot — start `flotilla watch
   --change_detector`." Every desk `unknown`.
2. **STALE** — a snapshot exists but its mtime age exceeds a freshness threshold
   derived from the watch tick cadence (proposed: `> 3 × heartbeat_interval`, the
   same K-window the detector's liveness uses, `internal/watch/detector.go`
   `livenessParams`). Banner (RED): "snapshot is N old — `flotilla watch` may be
   down." Desk states shown but marked stale.
3. **FRESH** — snapshot age within the threshold. States shown live.

The dash does **not** silently fall back to its own probe — that would
reintroduce the divergence we just rejected. This is the correct coupling: the
dash presents the fleet that `watch` is observing, and tells the operator
honestly whether that observation is live, stale, or absent.

## 3. The cnc read surfaces

1. **Fleet board.** One card/row per roster desk: name, surface, assessed state
   (idle / working / awaiting-input / awaiting-approval / errored / crashed /
   unknown — the `surface.State` vocabulary, `internal/surface/surface.go:26`,
   with `StateShell`→"crashed" per `status`'s mapping at
   `cmd/flotilla/status.go:172`), the XO marked as hub, the XO's liveness (ack
   age) and settled flag, and the **snapshot freshness** prominently. The JSON
   behind it is a **superset of the existing `flotilla status --json` contract**
   (`{ generated_at, xo, agents[{name,role,surface,state}] }`) so the landing
   widget and the dash speak the same shape.
2. **Federation topology.** The channel↔XO bindings rendered as the org chart:
   each channel → its XO (hub) → its members (desks or sub-fleet XOs), with the
   meta-XO → project-XOs → desks recursion made visible (`Config.Bindings()`).
   For a single-fleet roster this is the one degenerate binding — still correct,
   just one box. This is the "make the hierarchy evident in the interface" goal
   of F#105, realized as a picture.
3. **Coordination history.** A reverse-chronological view of the CoS ledger
   (`internal/cos/ledger.go` lines: `time · channel · from → to · "gist"`) and
   the backlog drive-queue (`backlog.Status` — what the XO is being driven on,
   what's blocked, what's done). Together these answer "what just happened across
   the fleet and what's queued" — the reporting gap #102 named.

All three are read-only and live-updating (SSE on snapshot/ledger/backlog mtime
change). They form the **default landing view** of the dash and require no auth
on loopback.

## 4. The cnc control surfaces

Three actions, each a thin proxy over an existing, tested library call. The dash
adds **no new delivery mechanism** — it is a UI over `send`/`notify`/`resume`.

| Dash action            | Maps to (existing)                                   | Typed outcomes surfaced to the UI |
|------------------------|------------------------------------------------------|-----------------------------------|
| Route an instruction   | `surface.Confirm.Submit(drv, pane, msg)` (the `cmdSend` path, `cmd/flotilla/main.go:304`) | delivered+confirmed / busy / transient / crashed / unconfirmed |
| Post an operator note  | `discord.Post(webhook, from, msg)` (the `cmdNotify` path, `:395`) | posted / webhook-missing / over-length |
| Resume a crashed desk  | the `flotilla resume` recipe path (`cmd/flotilla/resume.go`) | resumed / no-recipe / live-refused / ambiguous |

Routing uses the SAME case-insensitive, `@`-tolerant target resolution as Discord
addressing (an empty target → the XO; `@name`/`name` → the canonical agent). It
differs in scope deliberately: the Discord relay (`relay.Route` +
`memberResolver`) scopes `@name` to the typed-in channel's members, but the dash
is a host-local operator console with no channel context, so it resolves
ROSTER-WIDE — the operator can address any desk. For a single-fleet roster these
coincide; for a federated roster the dash is intentionally boundary-transcending
(the operator owns the whole fleet). The confirmed-delivery contract (idle-gate,
composer-clear confirmation, never silent-drop — see the
`flotilla-confirmed-delivery` invariants) is preserved because the dash calls
`Confirm.Submit` verbatim; it does not re-implement submission.

**Why in-process, not shelling out to `flotilla send`.** The dash IS the
`flotilla` binary, so it links `internal/surface`, `internal/deliver`,
`internal/discord`, `internal/roster` directly — the same packages `cmdSend`
calls. Calling the library in-process is idiomatic Go, avoids a subprocess per
action, and surfaces the *typed* error (`ErrBusy` vs `ErrCrashed` …) directly to
the UI instead of parsing stderr.

**Stale-board vs live-action (the read model is advisory; the action is
authoritative).** The board's desk state is the snapshot's (possibly stale) view,
but every control action re-validates the LIVE pane at action time:
`Confirm.Submit` idle-gates on a live `Assess` (`internal/surface/confirm.go`),
and `resume` refuses a live pane without `--force` via its liveness interlock
(`cmd/flotilla/resume.go:145-148`). So the typed outcome can legitimately differ
from what a stale board showed — the UI presents that as "the desk's LIVE state
was X" (e.g. "board showed idle; the desk is actually busy — not delivered,
retry"), never as a bare failure. The board age is for *display*; the library
call is the source of truth for the *action*.

**Dash actions are mirrored to the CoS ledger (self-observability).** A control
action issued from the dash is an operator↔fleet exchange exactly like one issued
over Discord, so the dash appends it to the CoS ledger
(`internal/cos/ledger.go`) with a dash-provenance marker (e.g.
`from = "operator(dash)"`). This makes "what did the dash do" auditable in the
same who-knows-what record as Discord traffic, and closes the
otherwise-unobserved gap of a new long-running process taking privileged actions.
(Best-effort, like `notify`'s own ledger append — a ledger failure never fails
the delivery.)

## 5. Concurrency: cross-process pane serialization (a real hazard, specified)

This is the one correctness hazard the control path must close — surfaced by the
review trio and verified against the code.

**The mechanism today.** A confirmed delivery is a multi-step sequence — submit →
poll `Assess` → re-send Enter — that *releases* the per-pane flock BETWEEN its
tmux calls (`internal/deliver/lock.go`: the flock is per-`Send`/`SendEnter`/
`Assess` call, not held across the sequence). The window between the submit and
the Enter-retry is closed today by an IN-PROCESS, per-pane mutex held across the
whole transaction: the `watch` injector holds `PaneMutexes` so the detector's
`/clear` rotate cannot interleave (`internal/watch/panemutex.go:5-15`,
`internal/surface/confirm.go:118-122` — both state this explicitly).

**Why the dash breaks the assumption.** The dash is a SEPARATE OS process from
`flotilla watch` (a distinct long-running subcommand). It therefore cannot share
watch's in-memory `PaneMutexes`. If the dash drives `Confirm.Submit` on a desk
while the watch detector concurrently rotates that same desk (`continueXO` →
`/clear`, `internal/watch/detector.go:353`), the cross-process flock — which only
serializes individual tmux calls — lets the `/clear` keystrokes interleave
between the dash's submit and its retry: exactly the composer corruption
`PaneMutexes` exists to prevent, now reachable because the second writer is in
another process. (The same latent exposure already exists for `flotilla send`
run by hand while `watch` is rotating — the dash amplifies it, it does not invent
it.)

**The fix (root-cause, specified).** Generalize the in-process per-pane mutex to a
**cross-process pane-transaction lock** in `internal/deliver`: a per-pane lock
file (e.g. `~/.flotilla/pane-locks/<key>.txn`) acquired ONCE around the entire
confirmed-delivery transaction and around the detector's context-rotate — so the
two can never interleave regardless of which process each runs in. Every
confirmed-delivery caller (`cmdSend`, the dash control handler, the watch
injector) and the detector's rotate acquire it; the existing per-call flock stays
as the lower layer. This is a small, additive change that also hardens the
pre-existing `send`-vs-`watch` race.

**Ownership & gating.** This lock lives in core (`internal/deliver` +
a one-line acquire in the watch rotate path), so it is a **shared-core touchpoint
coordinated with the core/desk-core lane**, and it is gated to **Phase 3**
(control) — the read-only Phase 1 takes NO pane action and needs none of this.
Until it lands, the dash MUST NOT expose pane-driving control. The spec records
this as an explicit control-path requirement, and Phase 3's tests add "dash route
+ watch rotate to the same pane do not interleave" (today `panemutex_test.go`
only covers the two in-daemon writers).

## 6. The native issue tracker (GitHub-backed)

The operator's intent (charter, 2026-06-18): a **native flotilla-dash feature**,
**GitHub-backed**, **NOT the #103 pluggable abstraction**; Linear/Jira deferred;
**no premature multi-tracker abstraction.**

- **Backend = GitHub Issues via the `gh` CLI.** `gh` is the operator's/XO's
  existing GitHub habit and is already authenticated on the host — but note
  honestly: **flotilla has zero `gh` shell-out code today, so this is the first
  code coupling to `gh`.** The tracker package shells out to `gh issue
  list/view/create/comment/edit/close --json <explicit-fields>` and parses the
  JSON. To keep the coupling disciplined: (a) request an **explicit `--json`
  field set** (never the default shape) so the parsed contract is pinned and a
  `gh` output change is detected, not silently mis-read; (b) **defensive parse** —
  an unparseable/empty response is a typed error, not a panic; (c) `gh`'s non-zero
  exits and non-JSON failure modes (**not authenticated / rate-limited / repo not
  found / network down**) map to clear typed errors surfaced to the UI, never
  swallowed (the project's silent-failure discipline). **No new secret, no GitHub
  token wiring in Phase 1** — `gh`'s existing auth is reused. (Direct REST via a
  PAT is a documented future alternative if a tokenless `gh` host ever needs it;
  it is NOT built now — that would be exactly the speculative generality #103/#104
  warn against.)
- **`gh` invocation hygiene (injection-safe).** Arguments derived from a request
  (issue title/body/labels) are passed so they can never be read as flags:
  use a `--` option terminator and pass free-form bodies via **stdin**
  (`gh issue create --body-file -`), validate issue numbers as integers, and
  **pin the target repo at startup** (`--repo` flag / working-dir resolution) —
  the repo is NEVER taken from a request body, so a client cannot retarget an
  arbitrary repo.
- **One internal package, one backend, a minimal seam.** `internal/dash/tracker`
  exposes a small Go interface (`List / Get / Create / Comment / Label / Close`)
  with **one** implementation (`gh`). The interface exists only so the HTTP
  handlers and tests don't bind to a subprocess directly (testability —
  inject a fake) — NOT to host multiple trackers. There is **no strategy
  registry, no config-selected provider** (that is #103's job, explicitly
  deferred). If #103 ever lands, this single backend slots behind its interface
  unchanged.
- **Native UI.** A list view (open issues, labels, the `operator-idea` label the
  XO uses), an issue detail view (body + comments), and create/comment/close
  forms — rendered in the dash next to the fleet, so an idea raised becomes a
  tracked issue without leaving the cnc surface. This directly serves the
  "idea/issue tracking" half of the #106 mandate and the visible-backlog goal of
  #103's "Why now."
- **Repo scope.** The tracker targets one repo (the dash's working-directory repo
  by default; configurable via a `--repo owner/name` flag), matching how `gh`
  resolves a repo. Multi-repo is a later ergonomic, not v1.

Creating/closing issues is a **write** to GitHub (outward-facing), so it sits
behind the same auth gate as cnc control (§7) and the UI confirms destructive
verbs (close) explicitly.

## 7. Security posture (fail-closed, consistent with the rest of flotilla)

The dash exposes two privilege tiers:

- **Read (fleet board, topology, history, issue *viewing*)** — harmless
  observation of local artifacts.
- **Control + issue *writes* (route/notify/resume, create/comment/close)** —
  privileged: injects into agent panes and writes to GitHub.

**The threat model includes the operator's own BROWSER as an untrusted actor.**
This is the correction the review trio forced: "loopback == host-shell trust" is
true for *network reachability of the socket*, but a web page the operator visits
in the SAME browser can issue requests to `127.0.0.1` without any host shell. So
loopback is NOT automatically safe for a control surface — the browser is the
attacker. The control surface is defended on loopback regardless, by three
mechanisms (the trio's B1/B2):

- **Custom-header requirement on every state-changing request.** Control + write
  endpoints require a custom request header (e.g. `X-Flotilla-Dash: 1`). A
  cross-origin page can only send it via a CORS *preflight*, which the dash never
  approves — so a forged "simple request" POST from a malicious page is rejected.
  This defeats browser CSRF **on loopback too** (it does not depend on a token).
- **`Host`-header allowlist (anti-DNS-rebinding).** Every handler validates the
  `Host` header against an allowlist (`127.0.0.1:<port>`, `[::1]:<port>`,
  `localhost:<port>`, plus the configured bind host). This closes the DNS-
  rebinding path where a remote page rebinds its hostname to `127.0.0.1` and
  reaches the loopback dash.
- **Server-side `Origin`/`Referer` check** on state-changing requests as
  defense-in-depth alongside the custom header.

The bind/auth trust model layered on top:

- **Default bind = `127.0.0.1` (loopback).** On loopback the socket is reachable
  only from the host (where "who has a shell" == "who can already run `flotilla
  send`" — total, per the resume launch-file note `cmd/flotilla/main.go:221-223`,
  "recipes are shell-run, so anyone who can write it can already write your
  secrets"). So loopback needs no bearer token — but it STILL enforces the
  custom-header + `Host`-allowlist + `Origin` checks above (the browser-attacker
  defense).
- **Non-loopback bind REFUSES to start without a bearer token.** If `--bind`
  names any non-loopback address (LAN, `0.0.0.0`), the server validates at
  startup that a token is configured and **fails closed** otherwise — the same
  load-time fail-closed discipline the roster uses throughout
  (`internal/roster/roster.go:188-268`). When a token is set, control + issue-
  write endpoints require it (`Authorization: Bearer …`, constant-time compare);
  read endpoints require it too on a non-loopback bind by default.
- **Token provenance (avoid `ps` exposure).** The token is read from
  `$FLOTILLA_DASH_TOKEN` or `--auth-token-file` by preference; a literal
  `--auth-token` flag is accepted but warns (it is visible in `ps`). The token is
  never logged.
- **SSE auth (`EventSource` cannot send `Authorization`).** Because the browser
  `EventSource` API cannot set an `Authorization` header, the `/events` stream
  does NOT rely on a bearer header. On loopback it follows the open-read posture
  (custom-header/`Host`-checked like everything else); on a non-loopback bind it
  is authorized via a short-lived `HttpOnly`, `SameSite=Strict` cookie minted by
  an authenticated POST — so the token never travels in a URL or a log. Read
  endpoints on non-loopback use the same cookie.
- **`html/template` + JSON, no script injection.** Pages render via
  `html/template` (contextual auto-escaping); all dynamic fleet/issue data
  reaches the page via `fetch` of JSON endpoints, never server-rendered into a
  `<script>` literal — so a desk name / issue title / ledger gist can never become
  stored XSS on a control surface.

This keeps the dash's security story *consistent with* the relay's operator-only,
fail-closed model — the boundary is "reaches the bind address, passes the
browser-CSRF defenses, and (off-loopback) holds the token" instead of "is the
operator on Discord."

## 8. Frontend & transport (stdlib-first)

- **Server:** Go `net/http`, routes registered on a `*http.ServeMux`. Handlers
  are thin: read handlers call the read model and encode JSON; the page handler
  renders `html/template`; control handlers call the delivery library.
- **Assets:** HTML/CSS/JS embedded via `embed.FS` so the binary is
  self-contained (no asset path to configure) — consistent with one-binary
  flotilla.
- **Live updates — a specified SSE hub, not just "emit on change."** Server-Sent
  Events (`text/event-stream`) on a single `/events` endpoint. The mechanism:
  ONE shared background poller stats the snapshot / ledger / backlog and signals a
  fan-out hub when any changes; the change key is `(mtime, size)` (not mtime
  alone — a same-second rewrite of equal mtime but different size is still caught;
  a same-second same-size change is reconciled by the `/api/status` poll
  fallback, which is the authoritative read). The hub registers each client on
  connect and **deregisters on disconnect** (`Request.Context().Done()`), fans out
  **non-blocking** (a slow client is dropped, never blocks the poller), and caps
  concurrent connections. `http.Server` read/write/idle timeouts are set so a
  stuck SSE client cannot pin a goroutine forever. SSE is one-way
  (server→browser), needs no new dependency, and reconnects automatically; the
  `/api/status` JSON endpoint is both the poll fallback and the
  reconcile-on-reconnect read.
- **No framework, no build step.** Vanilla JS modules, same as `site/`. This
  keeps the dash inspectable and dependency-light, matching the project's stated
  "small CLI, stdlib-first" character. **Named ceiling:** vanilla JS is right for
  the read views and the modest tracker/control forms; if a later view needs
  heavy client-side *stateful editing* (well beyond `site/app.js`'s ~136 read-only
  lines), that is the trigger to reconsider a build step — not a v1 concern, but a
  tracked one so the no-build choice is revisited deliberately, not by default.

## 9. Phasing (checkpoint at each boundary)

The dash is large; it is built in risk order — **read before write, harmless
before privileged** — with a `phase-checkpoint` at each boundary (the phase plan
is not blanket authorization for all phases).

- **Phase 0 — design + trio (this change).** proposal + design + spec + tasks;
  systems-review + OCR + STORM; iterate clean; report.
- **Phase 1 — dash server + read cnc.** `flotilla dash` command, the read model,
  the fleet board + federation topology + coordination history, SSE live updates,
  loopback bind, the `status`-superset JSON. **Zero blast radius** (pure reader).
  This alone delivers the #102 reporting surface.
- **Phase 2 — native issue tracker.** `internal/dash/tracker` (gh backend) +
  the list/detail/create/comment/close UI, behind the auth gate for writes.
- **Phase 3 — cnc control.** route / notify / resume actions via the delivery
  library, behind the auth gate, with the non-loopback token requirement and the
  typed-outcome UI.

Each phase is independently shippable and independently reviewable. Phases 2 and
3 are order-independent (both depend only on Phase 1's server skeleton); the
order above leads with the tracker because it carries no pane-injection risk.

## 10. Alternatives considered (and rejected)

- **A separate `flotilla-dash` binary / hosted service.** Rejected: breaks the
  one-binary model and reintroduces exactly the "hosted control plane" the #106
  discussion warned about. A subcommand is the flotilla-idiomatic shape.
- **The dash runs its own pane prober.** Rejected (§2): duplicates the
  detector, doubles tmux load, creates a second source of truth. Read the
  snapshot `watch` already writes.
- **Build the #103 pluggable multi-tracker abstraction now.** Rejected by
  operator directive: Linear/Jira deferred; "don't build a premature multi-
  tracker abstraction." One GitHub backend, minimal seam.
- **A JS framework (React/Vue) + bundler.** Rejected: a build step and a
  dependency tree are inconsistent with the stdlib-first, inspectable ethos and
  buy nothing for the modest interactivity the dash needs (vanilla JS + SSE
  suffice).
- **Shell out to `flotilla send` for control.** Rejected (§4): the dash links
  the delivery library directly; in-process gives typed errors and no subprocess
  per action.

## 11. Open questions (for the trio / operator — none is a v1 blocker)

1. **Read-on-loopback auth default.** v1 leaves read endpoints *token*-free on
   loopback (host-shell trust) and gates only writes/control with the token; note
   the browser-CSRF defenses (custom header + `Host`-allowlist + `Origin`) apply
   to state-changing requests on loopback REGARDLESS — only the *bearer token* is
   skipped on loopback. Is token-free read on loopback the right default, or should
   read also require the token whenever one is set? (Proposed: token-free read on
   loopback, gated on non-loopback — least friction for the common single-host
   case.)
2. **Reporting digest (#102) overlap.** The dash renders the live picture; should
   it also generate the *periodic push* digest (#102), or does that stay with the
   XO/`notify` path? (Proposed: the dash owns the pull/live view; periodic push
   stays XO-driven — the dash is a surface, not a scheduler.)
3. **fsnotify vs stat-poll for live updates.** v1 uses stat-poll (no dependency);
   if sub-second latency is wanted, fsnotify is a small dependency. (Proposed:
   stat-poll for v1; revisit only if latency is felt.)

## 12. Scope of change & framing

- **Untouched in Phases 0–2:** the `watch` daemon, the detector, the snapshot
  format, the relay, federation routing, the CoS ledger format, the backlog
  format, the surface drivers, and the confirmed-delivery contract — the dash
  reads and calls them, it does not modify them.
- **One shared-core touchpoint in Phase 3 (control only):** the cross-process
  pane-transaction lock (§5) adds a lock to `internal/deliver` and a one-line
  acquire to the detector's context-rotate path. It is additive (it changes no
  format or contract), it hardens a pre-existing `send`-vs-`watch` race, and it is
  **coordinated with the core/desk-core lane** — not built in the read-only
  Phase 1.
- **No new third-party Go dependency:** stdlib HTTP + `embed`; `gh` is a
  subprocess (Phase 2), not a Go dependency.
- **Positioning is unchanged:** the dash is **optional and pluggable**; a fleet
  that never starts it is identical to today.
- **Framing (product, dogfooded — not bespoke tooling):** flotilla-dash is a
  flotilla PRODUCT capability — a native interface any adopter can opt into —
  that we happen to dogfood on this fleet. The generalizable mechanism (a local
  web surface over the artifacts any flotilla deployment writes + the delivery
  library every deployment has) is what ships; the *contents* it renders (this
  fleet's roster, channels, issues) are circumstantial. This framing is why the
  read core leads: it is the broadly-useful, low-risk product surface, valuable
  to a single-fleet adopter (live board) and most distinctive for a multi-fleet
  one (federation topology).
