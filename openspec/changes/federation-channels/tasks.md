# Tasks — federation-channels

> **Design-first, then build under the autonomous workflow.** Phase 0 produced the
> design + spec; the review trio cleared it; the channel-federation feature is the
> operator's standing ask (issue #101), so Phase 1 (Transport A / v1) proceeded under
> the autonomous dev+review workflow — no per-PR ratification gate. The §6 transport
> fork was resolved for v1 by **Transport A** (single-host pane injection); **Transport
> B remains design-only and is NOT built here** (Phase 2, a separate change).

## Phase 0 — design + review (this change)

- [x] 0.1 Proposal (`proposal.md`): why, what changes, capabilities, impact.
- [x] 0.2 Design (`design.md`): recursive hub-spoke model, channel↔XO bindings,
      multi-channel inbound routing, the §6 cross-tier transport fork (A vs B) with
      the relay security analysis, config surface (backward compatible), phasing.
- [x] 0.3 Spec deltas: new `federation` capability + `watch` multi-channel relay delta.
- [x] 0.4 `/systems-review` + `/open-code-review` on the design; iterate to clean.
      DONE: iteration-1 systems-review (canonical comment on PR #105) — design clean after
      3 refinements folded in (the one-relay-per-channel invariant, single-guild assumption,
      recursion clarification).
- [x] 0.5 v1 scope resolved to Transport A (single-host); the build proceeded under the
      autonomous workflow (operator's standing ask, issue #101). Transport B (the §6 fork's
      cross-host arm) stays design-only → Phase 2.

## Phase 1 — multi-channel inbound + Transport A (v1)

- [x] 1.1 `internal/roster`: add `Channel` binding type + `Config.Channels []Channel`;
      keep top-level `channel_id`/`xo_agent` as the one-binding degenerate form.
      DONE: `Channel` type + `Channels` field + `Bindings()`/`BindingForChannel()`
      (legacy → one synthesized binding; members = all agents; XO defaults to first
      agent) — `internal/roster/roster.go`.
- [x] 1.2 Roster validation (fail-closed): xo_agent/members exist; channel_ids unique;
      an agent is the xo of ≤1 binding; legacy form vs `channels[]` mutually exclusive.
      DONE + tested (`internal/roster/federation_test.go`: valid federated roster, 7
      fail-closed cases, backward-compat synthesized binding, clock-only no-binding).
- [x] 1.3 `internal/discord/gateway.go`: accept a SET of channel ids; admit any bound
      channel; pass origin `channelID` into the handler (`MessageHandler` gains it).
      DONE: `MessageHandler func(channelID, webhookID, authorID, content)`; `NewGateway(token,
      channelIDs []string, handler)` with an admit-set; Open logs the channel set.
- [x] 1.4 `internal/watch/relay.go`: look up the binding by origin channel; run the
      EXISTING `Accept`/`Route` against that binding's xo_agent + member resolver.
      DONE: `Relay.Handle(channelID,…)` → `cfg.BindingForChannel` → `relay.Route` with a
      `memberResolver(binding.Members)` scoped resolver; `xoAgent` dropped from `Relay`/`NewRelay`
      (it is now per-binding). `relay.Accept`/`Route` unchanged.
- [x] 1.5 `cmd/flotilla/watch.go`: wire the channel set + per-binding routing; preserve
      single-channel behavior when no `channels[]` is present.
      DONE: gateway-open gated on `len(cfg.Bindings()) > 0` (was `cfg.ChannelID != ""`); opens over
      the whole `channelIDs` set; a daemon with no binding runs clock-only (no gateway) — the §7
      one-relay-per-channel separation. Single-channel path byte-equivalent.
- [x] 1.6 Transport A: confirm the meta-XO→project-XO path is plain `flotilla send`
      (pane injection); no relay change. Document the single-host constraint.
      DONE: no code (send/inject path reused verbatim; `relay.Accept` not broadened); documented in
      the quickstart "Federated fleets" → "Delivery between tiers (Transport A, single-host)".
- [x] 1.7 Per-XO outbound: ensure `notify`/mirror post to the XO's own channel webhook;
      validate `FLOTILLA_WEBHOOK_<XO>` per bound XO.
      DONE: `notify`/mirror already select the webhook by `--from` (no code change needed — the
      requirement is that the per-XO webhook be created in that XO's channel, a setup concern);
      documented in the quickstart. v1 note recorded: the relay's own notices/mirror post under the
      relay daemon's alert webhook, not per origin channel (documented limitation).
- [x] 1.7a CoS-mirror SEAM (for companion #108 — see design §8; do NOT build the mirror here):
      add `OriginChannel` to `watch.Job` and have the relay set it when routing an operator
      message, so the existing `Injector.SetMirror` hook can later post per-channel traffic to
      the CoS with full context. Keep the existing mirror behavior unchanged.
      DONE: `Job.OriginChannel` (additive); the relay sets it on the routed job; the mirror hook
      already receives the whole Job. v1 behavior unchanged (only carried). Tested via SetMirror.
- [x] 1.7b CoS-mirror SEAM: reserve a top-level `cos_agent` config field — parse + validate
      (must name an agent in `agents[]` when set) but do NOT act on it in v1; #108 consumes it.
      DONE: `Config.CosAgent` + fail-closed validation + test.
- [x] 1.8 Tests: relay routing by channel (alpha desk vs project-XO vs meta-XO),
      per-channel operator-only + self-mirror-drop, validation failures, backward-compat
      single-fleet roster unchanged, the Job carries OriginChannel, `cos_agent` validation.
      DONE: `internal/watch/relay_test.go` rewritten — backward-compat (legacy binding), unbound-
      channel drop, route-by-origin-channel, member-scope isolation, per-channel auth + self-mirror
      drop, OriginChannel-on-Job, onAccepted target. Roster validation cases in `federation_test.go`
      (1.2). `go test -race ./...` green.
- [x] 1.9 Docs: quickstart "federated fleets" section; example federated roster;
      setup-helper usage.
      DONE: `docs/quickstart.md` §6 "Federated fleets" — the model, an example `channels[]` roster,
      origin-channel routing + scope isolation, the one-relay/many-clocks topology, Transport A
      single-host, per-XO outbound.
- [x] 1.10 `/systems-review` + `/open-code-review` + `/storm` on the implementation diff; iterate.
      DONE (trio run on main..design/federation-channels):
      • systems-review: CLEAN — 0 P1s (security preserved per-channel, ripple complete, validation
        comprehensive, backward-compat exact). 2 P2s = documented §7 v1 limitations.
      • OCR: 1 finding (Bindings() slice aliasing vs legacy alloc) → RESOLVED: documented the
        read-only contract on Bindings().
      • STORM: MERGEABLE, no v1 blockers. New failure mode surfaced (partial Message-Content intent →
        blank inject) → RESOLVED: relay drops empty/whitespace operator messages (+ test) + quickstart
        callout. Daemon role-fusion + topology warts → tracked as #111–#114; meta-XO trust scope named
        in design §11. Strengthened the "list meta-XO first" doc callout.

## Phase 2 — Transport B (Discord-bus, cross-host) — SEPARATE change, later

- [ ] 2.1 Parent-allow-list security spec: `Accept` = operator OR pinned parent
      identity, never self-mirror, never foreign webhooks; scenarios for each.
- [ ] 2.2 Per-binding `parent` identity in config; meta-XO delivers via channel post.
- [ ] 2.3 Feedback-loop + foreign-injection tests; cross-host runbook.

## Phase 3 — ergonomics (later)

- [ ] 3.1 Setup helper: create per-XO + fleet-command channels + per-XO webhooks (idempotent).
- [ ] 3.2 (Optional) single-daemon clock multiplexing over multiple XOs.
- [ ] 3.3 (Optional) meta-XO doctrine: cross-fleet reporting cadence in `#fleet-command`.
