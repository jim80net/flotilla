# Tasks — federation-channels

> **Design-first.** Phase 0 produces the design + spec for ratification. Phase 1+
> build tasks are enumerated but **unchecked — do not implement until the operator
> ratifies the design (especially the §6 transport fork).**

## Phase 0 — design + review (this change)

- [x] 0.1 Proposal (`proposal.md`): why, what changes, capabilities, impact.
- [x] 0.2 Design (`design.md`): recursive hub-spoke model, channel↔XO bindings,
      multi-channel inbound routing, the §6 cross-tier transport fork (A vs B) with
      the relay security analysis, config surface (backward compatible), phasing.
- [x] 0.3 Spec deltas: new `federation` capability + `watch` multi-channel relay delta.
- [ ] 0.4 `/systems-review` + `/open-code-review` on the design; iterate to clean.
- [ ] 0.5 Surface to the XO → operator ratification of the §6 fork + open questions
      (design.md §10). **Gate: no Phase 1 work before ratification.**

## Phase 1 — multi-channel inbound + Transport A (AFTER ratification)

- [x] 1.1 `internal/roster`: add `Channel` binding type + `Config.Channels []Channel`;
      keep top-level `channel_id`/`xo_agent` as the one-binding degenerate form.
      DONE: `Channel` type + `Channels` field + `Bindings()`/`BindingForChannel()`
      (legacy → one synthesized binding; members = all agents; XO defaults to first
      agent) — `internal/roster/roster.go`.
- [x] 1.2 Roster validation (fail-closed): xo_agent/members exist; channel_ids unique;
      an agent is the xo of ≤1 binding; legacy form vs `channels[]` mutually exclusive.
      DONE + tested (`internal/roster/federation_test.go`: valid federated roster, 7
      fail-closed cases, backward-compat synthesized binding, clock-only no-binding).
- [ ] 1.3 `internal/discord/gateway.go`: accept a SET of channel ids; admit any bound
      channel; pass origin `channelID` into the handler (`MessageHandler` gains it).
- [ ] 1.4 `internal/watch/relay.go`: look up the binding by origin channel; run the
      EXISTING `Accept`/`Route` against that binding's xo_agent + member resolver.
- [ ] 1.5 `cmd/flotilla/watch.go`: wire the channel set + per-binding routing; preserve
      single-channel behavior when no `channels[]` is present.
- [ ] 1.6 Transport A: confirm the meta-XO→project-XO path is plain `flotilla send`
      (pane injection); no relay change. Document the single-host constraint.
- [ ] 1.7 Per-XO outbound: ensure `notify`/mirror post to the XO's own channel webhook;
      validate `FLOTILLA_WEBHOOK_<XO>` per bound XO.
- [ ] 1.7a CoS-mirror SEAM (for companion #108 — see design §8; do NOT build the mirror here):
      add `OriginChannel` to `watch.Job` and have the relay set it when routing an operator
      message, so the existing `Injector.SetMirror` hook can later post per-channel traffic to
      the CoS with full context. Keep the existing mirror behavior unchanged.
- [x] 1.7b CoS-mirror SEAM: reserve a top-level `cos_agent` config field — parse + validate
      (must name an agent in `agents[]` when set) but do NOT act on it in v1; #108 consumes it.
      DONE: `Config.CosAgent` + fail-closed validation + test. (1.7a OriginChannel is part of
      the relay/inject ripple — deferred to the next session, see handoff.)
- [ ] 1.8 Tests: relay routing by channel (alpha desk vs project-XO vs meta-XO),
      per-channel operator-only + self-mirror-drop, validation failures, backward-compat
      single-fleet roster unchanged, the Job carries OriginChannel, `cos_agent` validation.
- [ ] 1.9 Docs: quickstart "federated fleets" section; example federated roster;
      setup-helper usage.
- [ ] 1.10 `/systems-review` + `/open-code-review` + `/storm` on the implementation diff; iterate.

## Phase 2 — Transport B (Discord-bus, cross-host) — SEPARATE change, later

- [ ] 2.1 Parent-allow-list security spec: `Accept` = operator OR pinned parent
      identity, never self-mirror, never foreign webhooks; scenarios for each.
- [ ] 2.2 Per-binding `parent` identity in config; meta-XO delivers via channel post.
- [ ] 2.3 Feedback-loop + foreign-injection tests; cross-host runbook.

## Phase 3 — ergonomics (later)

- [ ] 3.1 Setup helper: create per-XO + fleet-command channels + per-XO webhooks (idempotent).
- [ ] 3.2 (Optional) single-daemon clock multiplexing over multiple XOs.
- [ ] 3.3 (Optional) meta-XO doctrine: cross-fleet reporting cadence in `#fleet-command`.
