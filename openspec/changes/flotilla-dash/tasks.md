# Tasks — flotilla-dash

> **Design-first, then build under the autonomous workflow, PHASED.** Phase 0
> produces the design + spec + this task plan and runs the review trio
> (systems-review + open-code-review + STORM). Clearing the trio is the bar to
> proceed to implementation (operator directive 2026-06-18). The build is phased
> in RISK ORDER — read before write, harmless before privileged — with a
> `phase-checkpoint` at each boundary (the phase plan is NOT blanket authorization
> for all phases; Phase N+1 is re-confirmed at the Phase N boundary).

## Phase 0 — design + review (this change)

- [x] 0.1 Proposal (`proposal.md`): why, what changes, capabilities, impact, and
      the relationship to #102 (reporting) / #103 (deferred tracker abstraction) / #106.
- [x] 0.2 Design (`design.md`): the reader + thin-action-proxy architecture, the
      read model over existing watch/roster/cos/backlog artifacts, the cnc read
      surfaces, the control actions over the confirmed-delivery library, the native
      GitHub-backed tracker (single backend, minimal seam), the fail-closed security
      posture, the stdlib frontend/SSE transport, phasing, alternatives, open questions.
- [x] 0.3 Spec delta: new `dash` capability (`specs/dash/spec.md`).
- [x] 0.4 `/systems-review` + `/open-code-review` + `/storm` on the design; iterate to clean.
      ROUND 1: systems-review = REQUEST CHANGES (P1 cross-process pane race; P2 DNS-rebinding;
      P3 gh identity/error + stale-vs-live); OCR = CLEAN (nits: citation, omitempty, positioning);
      STORM = NEEDS CHANGES B+ (B1 "no CSRF by construction" false; B2 rebinding; B3 PaneMutexes
      cross-process; B4 SSE auth; B5 gh injection/repo-pin; B6 token-in-ps; B9-11 UX). ALL folded in
      (design §3 three-state, §4 stale-vs-live + dash→ledger, §5 NEW cross-process lock, §6 gh hygiene,
      §7 browser-attacker defenses + SSE cookie + token provenance, §8 SSE hub mechanism; spec: 5 new
      requirements/scenarios). STORM's "read-only tracker" fork DECLINED — operator explicitly
      prioritized the native tracker; not re-litigating a decided scope (folded the coupling concerns
      as gh hygiene instead).
- [ ] 0.4b Re-run the trio on the revised design to confirm clean (round 2).
- [ ] 0.5 Report trio-clean to the operator's XO; proceed to Phase 1 under the autonomous
      workflow. No genuine fundamental fork surfaced (the one "fork" the trio raised contradicts an
      explicit operator directive, so it is decided, not open).

## Phase 1 — dash server + read cnc (zero blast radius)

- [x] 1.1 `cmd/flotilla/main.go`: add the `dash` switch arm + usage block.
- [x] 1.2 `cmd/flotilla/dash.go`: flags (`--roster`, `--snapshot-file`, `--ack-file`,
      `--tracker-file`, `--bind` [default `127.0.0.1:8787`], `--repo` for the tracker —
      accepted, unused), default-path resolution mirroring `status` EXACTLY.
      NOTE: the `--auth-token`/`$FLOTILLA_DASH_TOKEN` machinery is deferred to the control
      phase — it is coupled to the write-auth gate + the SSE-cookie auth that makes a
      non-loopback bind safe. Phase 1 is loopback-ONLY and fails closed on any non-loopback
      bind (a strict superset of "non-loopback without a token fails closed"), so an inert
      Phase-1 token flag would be a footgun. Tracked for the control phase (§3.2).
- [x] 1.3 `internal/dash`: the read model — load snapshot (`watch.LoadSnapshot`),
      ack age, roster bindings (`Config.Bindings()`), CoS ledger, backlog (`backlog.Parse`).
      Pure functions, unit-tested with in-memory artifacts + a pinned clock.
- [x] 1.4 `internal/dash`: the HTTP server (`net/http` + `ServeMux`), `embed.FS`
      assets, `html/template` page render, `/api/status` (the `flotilla status --json`
      SUPERSET), `/api/topology`, `/api/history` JSON endpoints.
- [x] 1.5 `internal/dash`: SSE `/events` hub — ONE shared poller keyed on
      `(mtime,size)` of snapshot/ledger/backlog; per-client register/deregister on
      disconnect (`Request.Context().Done()`); non-blocking fan-out (drop slow clients);
      connection cap; `http.Server` read/idle timeouts. `/api/status` JSON poll =
      fallback + reconcile-on-reconnect read.
- [x] 1.6 Frontend (vanilla JS, no build): fleet board (three-state freshness),
      federation topology org chart, coordination history; SSE live-update wiring; dynamic
      data via `fetch`ed JSON only (never server-rendered into `<script>`); reuse `site/` CSS.
- [x] 1.7 Loopback bind by default; `Host`-header allowlist on every handler (anti-rebinding,
      lands in P1 so the infra exists before writes); the three-state empty board (absent/stale/fresh).
- [x] 1.8 Tests: read-model purity (snapshot → board JSON; topology from bindings;
      ledger/backlog parse), the status-superset contract (name/state always; role for XO;
      effective surface), the three-state freshness paths, single-fleet (one-binding) topology,
      SSE hub (emit on `(mtime,size)` change, client deregister-on-disconnect), `Host`-allowlist
      rejection. `go test -race ./...`.
- [x] 1.9 Docs: `docs/dash-runbook.md` (start, bind, what it reads, no-snapshot note);
      README roadmap line. Cold-tested the runbook's commands.
- [x] 1.10 `/systems-review` + `/open-code-review` + `/storm` on the Phase 1 diff; iterate clean.
      ROUND 1: all three converged on ONE real defect — the SSE hub could deadlock/leak a goroutine
      on graceful shutdown (a producer send blocking forever once run() exited on ctx-cancel).
      FIXED with a hub `done` channel guarding every producer select (verified: shutdown with a live
      SSE client now exits in ~0.1s, was a 5s hang + leak). Also folded: OCR's statSig silent-swallow
      (documented as a deliberate change-trigger choice — authoritative freshness is in loadBoard),
      STORM S1 (escapeHtml `'`), U1 ("settled?"→"settled unknown"), U2 (stale banner → RED per §3).
      M1 (status-helper duplication) kept — justified (pkg main is unimportable), parity-tested, TODO
      filed for a future shared-package extraction. All other axes (reader-only, loopback fail-closed,
      Host-allowlist, status-superset, freshness, ledger parse, aliasing, XSS) verified CLEAN.
- [ ] 1.11 **Phase-1 checkpoint:** report what landed / what's deferred / proposed Phase 2.

## Phase 2 — native GitHub-backed issue tracker

- [x] 2.1 `internal/dash/tracker`: a minimal Go interface (`List/Get/Create/Comment/Label/Close`)
      with ONE `gh`-backed implementation (`gh.go`); parse `gh … --json=<explicit-fields>` (pinned
      `listFields`/`detailFields`); defensive parse (unparseable → ErrParse, empty list → []Issue not
      error); map gh non-zero/non-JSON exits (unauthenticated / rate-limited / repo-not-found /
      network) to typed sentinel errors via `classify` (patterns verified against gh 2.45 live
      stderr). Inject-a-fake `ghRunner` for tests. NO strategy registry, NO config-selected provider.
- [x] 2.2 `--repo owner/name` resolution PINNED AT STARTUP (`resolveTrackerRepo`; default via
      `tracker.ResolveDefaultRepo` = the working-dir repo as `gh` resolves; `$FLOTILLA_DASH_REPO`
      also honored) — never request-derived. Injection-safe invocation: bodies via stdin
      (`--body-file=-`), titles/labels via `--flag=value`, `--` option terminator before positional
      numbers, issue numbers validated as positive integers, repo validated against `repoPattern`.
- [x] 2.3 Tracker UI (`assets/tracker.js` + index/CSS): Fleet/Issues tab nav; list (open issues +
      one-click `operator-idea` filter + state filter), detail (body + comments + GitHub link),
      create / comment / label / close forms; close is confirmed explicitly (window.confirm).
- [x] 2.4 Gate issue WRITES behind the browser-CSRF defense (`requireWrite`: custom `X-Flotilla-Dash`
      header + Origin/Referer allowlist, on loopback too); method-gated (writes POST-only via the mux);
      reads follow the open-on-loopback read posture. (Bearer token + SSE cookie stay Phase 3.)
- [x] 2.5 Tests: backend against a fake `gh` runner (list/get/create/comment/label/close happy + the
      gh-down typed-error paths: unauth / rate-limited / repo-not-found / network / unparseable);
      handler CSRF gating on writes (missing-header → 403 no gh call, cross-origin → 403, non-browser
      allowed, GET-on-write rejected); injection-safe arg passing (evil title/body/leading-dash, repo
      pin unretargetable); over-length / empty / missing-repo / invalid-number errors. Plus a live
      (env-gated) integration test exercising the REAL execRunner read path. `go test -race ./...` green.
- [x] 2.6 Docs: tracker section in `docs/dash-runbook.md` (gh auth prerequisite, `--repo` pinning,
      write-CSRF posture, `operator-idea` label) — gh commands cold-tested; README roadmap updated.
- [ ] 2.7 `/systems-review` + `/open-code-review` + `/storm` on the Phase 2 diff; iterate clean.
- [ ] 2.8 **Phase-2 checkpoint:** report; proposed Phase 3.

## Phase 3 — cnc control actions

- [ ] 3.0 **(shared-core, coordinate with flotilla-dev)** cross-process pane-transaction
      lock in `internal/deliver` (per-pane lock file held across the whole confirmed-delivery
      transaction) + a one-line acquire in the detector's context-rotate path. Hardens the
      pre-existing `send`-vs-`watch` race. PREREQUISITE for pane-driving control — control is
      not exposed until this lands.
- [ ] 3.1 `internal/dash`: the three control handlers — route (`surface.Confirm.Submit`
      via the `cmdSend` path + `relay.Route` addressing, acquiring the 3.0 cross-process lock),
      notify (`discord.Post`), resume (the `flotilla resume` recipe path, per-agent locked +
      button debounce) — each returning the library's TYPED outcome; each mirrored to the CoS
      ledger with dash provenance (best-effort).
- [ ] 3.2 Fail-closed security: non-loopback bind REFUSES to start without a token (startup
      validation); control + write endpoints require `Authorization: Bearer` (token) when set,
      constant-time compare, token from env/file (warn on `--auth-token`), never logged;
      browser-CSRF defense (custom header + Origin) on ALL state-changing requests incl. loopback;
      SSE on non-loopback authorized by a short-lived HttpOnly SameSite cookie (no URL token).
- [ ] 3.3 Control UI: a route composer (`@desk` aware), an operator-note form, a resume
      button on crashed desks; surface each typed outcome distinctly (busy/crashed/unconfirmed…);
      a stale-state confirm dialog that restates the desk's live state+age (stale ≠ failure).
- [ ] 3.4 Tests: each control handler maps to the right library call and surfaces each typed
      error; the 3.0 lock — dash route + watch rotate to the same pane do NOT interleave; auth
      gate (loopback token-free vs non-loopback-requires-token; missing/invalid token → 401/403
      with no side effect); the fail-closed startup refusal; browser-CSRF rejection on loopback;
      dash→ledger provenance entry. `go test -race ./...`.
- [ ] 3.5 Docs: control section in `docs/dash-runbook.md` (the trust model, the browser-attacker
      defenses, the non-loopback token requirement + SSH-tunnel-to-loopback remote recipe, the
      typed outcomes).
- [ ] 3.6 `/systems-review` + `/open-code-review` + `/storm` on the Phase 3 diff; iterate clean.
- [ ] 3.7 **Phase-3 checkpoint:** report; archive the openspec change when all phases land.

## Phase 4 — ergonomics (later, optional)

- [ ] 4.1 (Optional) fsnotify for sub-second live updates if stat-poll latency is felt.
- [ ] 4.2 (Optional) multi-repo tracker scope.
- [ ] 4.3 (Optional) a #102 periodic-digest view (pull); periodic PUSH stays XO/notify-driven.
