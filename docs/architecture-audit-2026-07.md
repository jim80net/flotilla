# flotilla architecture audit — July 2026

**Scope.** Style and structure review of the flotilla codebase (Go 1.26, single
module, ~30.5k non-test LOC) ahead of a public contributor push: SOLID seams,
cyclomatic complexity, typing discipline, and test architecture. Every finding
cites `file:line` and names a concrete remediation. Two of the safe, behavior-
preserving wins were landed in this branch (see §"Landed"); the rest are
recommendations, sized so a new contributor could pick one up.

**Method.** `go list`/import-graph for layering; `gocyclo` + direct reads for
complexity; grep + read for typing and test patterns. Baseline was green and
stayed green: `go build ./...`, `go vet ./...`, and `go test -race ./...` all
pass before and after the landed refactors.

**Headline.** The codebase is in good structural health. The dependency graph is
a **clean DAG with no import cycles**, the two load-bearing seams (the surface
driver SPI and the transport bus) are genuinely well-designed, and error handling
is idiomatic. The real debt is **concentration**, not rot: a handful of god-sized
functions and structs in the daemon bootstrap and the detector, plus a brittle
class of front-end "asset-grep" tests. None of it blocks contribution; all of it
is worth paying down before the surface area gets more eyes.

---

## What's sound (call this out to new contributors)

These are strengths, not findings — a new reader should study them as the house style.

- **Clean layering, no cycles.** `deliver`/`roster`/`discord`/`surface`/`transport`
  are the foundation; `watch`/`dash` are the coordinators; `cmd/flotilla` is the
  only composition root. Verified via `go list` import graph.
- **The surface Driver SPI** (`internal/surface/surface.go:61`) is a model of
  interface segregation: a minimal required `Driver`, with optional capabilities
  (`ResultReader`, `ReplyReader`, `ComposerStateProbe`, `RateLimitProbe`) as
  separate interfaces resolved by type assertion + fallback. Adding a capability
  never widens the core or breaks an existing surface. Typed `State`, `Strategy`,
  and `ComposerDisposition` enums with `String()` methods; sentinel errors
  (`ErrBusy`, `ErrNoGracefulClose`) instead of stringly signals.
- **The transport bus** (`internal/transport/transport.go:26`) seals its
  `Destination` behind an unexported `isDestination()` marker so a
  credential-bearing webhook URL can never leak across the seam as a raw string,
  and splits init-time registration from daemon-start construction for its live
  resources.
- **Idiomatic error handling.** 633 `fmt.Errorf` (mostly wrapping with `%w`),
  sentinel errors, **zero** `log.Fatal`, and only **3** `panic` — all three are
  init-time "embedded asset missing" fail-fast (`internal/dash/assets.go:31`,
  `internal/doctrine/doctrine.go:257`, `internal/sessionmirror/build.go:55`),
  which is the correct use of panic.
- **`any`/`interface{}` is disciplined** — the handful of uses are JSON
  encode/decode helpers and variadic log args (`internal/dash/server.go:383`,
  `internal/watch/inject.go:293`), not type-erased seams. No finding here.
- **The test suite is race-clean** and locks real behavior for the hard parts
  (the detector state machine, the injector's busy-defer, the dash read-model).

---

## P1 — structural risks (worth scheduling)

### P1-1 · `cmdWatch` is a 1,200-line function with cyclomatic complexity 101
`cmd/flotilla/watch.go:89` (`func cmdWatch`)

The daemon's entire bootstrap lives in one function: flag declaration (~28 flags),
env fallback + path defaulting, secrets loading, transport construction, detector
config assembly (dozens of closures), and the run loop. gocyclo measures **101** —
the next-highest function in the tree is 43. It is the single hardest thing in the
repo for a newcomer to read, and the riskiest to change, because every concern is
interleaved in one lexical scope.

**Remediation.** Decompose into named phases, each independently testable:
`parseWatchFlags(args) (watchOpts, error)` → `resolveStatePaths(&opts)` →
`loadWatchSecrets(opts)` → `buildDetectorConfig(cfg, opts, …) watch.DetectorConfig`
→ `runWatchLoop(...)`. This is behavior-preserving but touches the daemon
entrypoint, so it wants its own PR with the full `-race` suite as the gate — **not**
folded into an unrelated change. Deferred here for that reason.

### P1-2 · `DetectorConfig` is a 46-field god-struct (34 injected closures)
`internal/watch/detector.go:61-286` (`type DetectorConfig struct`)

Every feature increment (visibility synthesis, the recursive desk-heartbeat,
rate-limit probing, adaptive interval, the delegation/idle-hold/stranded nudges)
appended its own cluster of fields to one flat struct: **46 exported fields, 34 of
them `func` closures**. The struct is 226 lines. It is dependency injection done
for testability (a real strength — the whole state machine is unit-testable without
tmux/clock/fs), but the flat shape means single-responsibility is lost at the
config boundary: a contributor adding a feature has no signpost for where its wiring
belongs, and the constructor's defaulting logic (`NewDetector`, gocyclo 26) grows
with it.

**Remediation.** Group by feature into embedded sub-structs —
`SynthConfig`, `HeartbeatConfig`, `RateLimitConfig`, `MirrorConfig`,
`LivenessConfig` — each with its own nil-inert defaulting. Behavior-preserving
(the closures don't change), but it re-types every call site in `cmd/flotilla`,
so it's a dedicated PR. Filing it, not landing it.

### P1-3 · `cmd/flotilla` is a god-package (8,535 LOC) holding real domain logic
`cmd/flotilla/` (33 non-test files)

The composition root carries more than wiring. `switch.go` (1,165 LOC, `runSwitch`
gocyclo 43 at `cmd/flotilla/switch.go:150`) and `recycle.go` (628 LOC) hold the
actual harness-switchover and session-recycle *policy*, not just command parsing.
Domain logic in `package main` can't be imported or unit-tested by another package
and doesn't show up in the internal dependency graph, so it's invisible to the
layering review.

**Remediation.** Lift the policy into internal packages (an `internal/switchover`,
an extended `internal/surface/recycle` — which already exists at
`internal/surface/recycle.go`), leaving thin `cmdSwitch`/`cmdRecycle` shims that
parse flags and call the package. Incremental and safe per-command.

---

## P2 — coupling and typing (medium)

### P2-1 · Two stringly-typed enums remain (one fixed this pass)
`internal/watch/inject.go` (`JobKind` — **fixed**, see Landed) ·
`internal/watch/detector.go:252` (`LivenessPingMode string`) ·
`internal/goals/types.go:29` (`WorkItem.Kind string`)

`Job.Kind` was a raw string compared across ~10 sites; it is now a typed `JobKind`
(landed). Two more remain:

- **`LivenessPingMode`** — a `string` with values `"none"/"interval"/"consecutive"`
  validated by a string-switch in `internal/roster/roster.go:234`. Contained and
  low-churn; a typed `PingMode` with constants + a `parsePingMode` is a clean
  follow-up.
- **`WorkItem.Kind`** — a `string` (`"issue"/"backlog"/"inline"/"desk"`)
  string-switched in `internal/goals/link.go:46`, `parse.go:146`, and
  `link_node.go:141`. **Higher care**: it is serialized to both JSON and YAML
  config (`types.go:29`, `json:"kind"`), and in `link_node.go` the `.Kind` selector
  is *overloaded* with `yaml.Node.Kind` (a different type) in the same functions —
  a blanket rename would be error-prone. A typed `WorkItemKind string` preserves the
  wire format (string marshals identically) but needs the yaml-node sites
  disambiguated first. Worth doing, deliberately.

**Remediation.** Same pattern as the landed `JobKind`: `type X string` + named
constants + a parse/validate helper; keep the wire values byte-identical.

### P2-2 · Front-end behavior is locked only by brittle asset-grep tests
`internal/dash/server_test.go:280` (`TestGoalsCanvasAssets`, gocyclo 40) ·
`internal/dash/goals_test.go`

The embedded dashboard JS (`goals.js`, `dash.js`) has **no behavior/DOM test**. Its
only lock is a 70-line sweep of `strings.Contains(js, "<identifier>")` asserting
that JS symbol names (`setupPanZoom`, `structuralSig`, `openDrawer`, `nodeActivate`,
`layoutOrg`, …) appear in the bundle. This is doubly weak: it is **brittle**
(renaming a behavior-identical function breaks the test) and **hollow** (it passes
even if the function body is gutted, as long as the identifier string still appears
somewhere). Each feature increment appends more markers, so the test grows without
adding real coverage — the gocyclo-40 is coverage theater, not assurance.

**Remediation.** Add one headless behavior smoke test for the canvas (a JS DOM
harness, or a Go test that drives the SSE + rendered HTML and asserts *structure*,
not symbol strings). Failing that, collapse the symbol sweeps to a single
"asset served and non-empty" check and stop treating symbol presence as a
behavior lock — it gives false confidence to a reviewer.

### P2-3 · Inconsistent duration-flag failure policy (loud vs silent)
`cmd/flotilla/watch.go:143` (loud) vs `cmd/flotilla/watch.go:1356`
(`optionalDuration`, silent)

`--interval` and `--event-poll-interval` return a hard error on an unparseable
duration. The adaptive-tuning flags (`--interval-warm`, `--interval-floor`, …) go
through `optionalDuration`, which **silently** falls back to the default on a parse
error or non-positive value. Same conceptual operation, two behaviors: a typo in
`--interval-warm 8mm` is silently ignored, so the operator gets the default and no
signal. This is a latent config-surprise, not a bug today.

**Remediation.** One `durationFlag(name, flagVal, envKey) (time.Duration, error)`
that errors loudly for all duration flags, or — if silent-default is intentional
for the tuning knobs — a one-line doc comment on `optionalDuration` stating that
choice so it reads as deliberate.

### P2-4 · The `Transport` core interface is wide (11 methods)
`internal/transport/transport.go:26`

`Transport` mixes inbound (`Subscribe`), outbound (`Post`,
`PostWithAttachments`), addressing (`Destinations`, `ResolveDestination`), and
chunking (`MaxContentRunes`, `Chunk`). The optional capabilities are already
segregated (`CatchUp`, `RecentHistory`, `InboundTarget`), so this is a mild ISP
note, not a real problem — but the chunking pair is arguably a formatting concern
that a second transport author must reimplement. Low priority.

**Remediation.** Optional: split the two chunking methods into a
`ContentChunker` capability interface (same optional-assert pattern the package
already uses). Only worth it when a second transport actually lands.

---

## P3 — style nits (low)

### P3-1 · Feature-flag comment creep in the detector
`internal/watch/detector.go:146-285`

The flat `DetectorConfig` (P1-2's root) carries long paragraph comments per
feature cluster inline, so understanding one field means reading several
increments' worth of context. Grouping into sub-structs (P1-2) also fixes this;
listed separately as the *readability* facet for a contributor who only wants to
add one closure.

### P3-2 · Confirm the 35 ignored-error assignments are all best-effort
`_ = …` at 35 non-test sites (e.g. `internal/dash`, `cmd/flotilla`)

Most are legitimately best-effort (a mirror/notify post whose failure must not
affect delivery). Worth a one-pass sweep to confirm none swallow a load-bearing
error; annotate each surviving one with a short `// best-effort: …` so the intent
is explicit rather than inferred.

---

## Top-10 summary

| # | Sev | Finding | Evidence | Remediation |
| --- | --- | --- | --- | --- |
| 1 | P1 | `cmdWatch` — 1,200 LOC, cyclo 101 | `cmd/flotilla/watch.go:89` | Split into parse/resolve/build/run phases |
| 2 | P1 | `DetectorConfig` — 46-field god-struct (34 closures) | `internal/watch/detector.go:61` | Group into feature sub-structs |
| 3 | P1 | `cmd/flotilla` god-package holds domain logic | `cmd/flotilla/switch.go:150`, `recycle.go` | Lift policy into `internal/` |
| 4 | P2 | `JobKind` stringly enum | `internal/watch/inject.go` | **Landed** — typed `JobKind` |
| 5 | P2 | `LivenessPingMode` / `WorkItem.Kind` stringly enums | `detector.go:252`, `goals/types.go:29` | Typed constants (WorkItem needs care) |
| 6 | P2 | Front-end locked only by asset-grep tests | `internal/dash/server_test.go:280` | Behavior smoke test; stop symbol-sweeps |
| 7 | P2 | Duration-flag failure policy inconsistent | `watch.go:143` vs `watch.go:1356` | One loud `durationFlag` helper |
| 8 | P2 | `Transport` core interface wide (11 methods) | `internal/transport/transport.go:26` | Optional `ContentChunker` capability |
| 9 | P3 | Detector feature-comment creep | `detector.go:146` | Sub-structs (with #2) |
| 10 | P3 | Confirm 35 `_ =` ignored errors are best-effort | 35 non-test sites | Sweep + annotate intent |

---

## Landed in this branch (behavior-preserving, `-race` green before + after)

1. **Typed `JobKind` enum** replaces the stringly-typed `Job.Kind`
   (`internal/watch/inject.go`) — `KindDefault`/`KindRelay`/`KindHeartbeat`/
   `KindDetector` constants; ~10 construction/comparison sites converted. Wire
   values unchanged, `Job.Kind` is not serialized, so it's byte-identical on disk
   and in the audit log. New `TestJobKindWireValuesAndPolicy` locks the wire values
   and the relay-vs-tick policy.
2. **`defaultPath` helper** deduplicates the eight copy-pasted unset-path
   defaulting blocks in `cmdWatch` (`cmd/flotilla/watch.go`). New `TestDefaultPath`
   proves empty-falls-back / supplied-value-preserved.

Everything else above is deliberately left as a recommendation — the P1 items carry
behavior risk (the daemon entrypoint, every detector call site) and belong in
dedicated PRs with the full suite as the gate, not folded into a style pass.
