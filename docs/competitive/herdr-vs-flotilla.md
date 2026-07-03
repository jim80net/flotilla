# Competitive Analysis: herdr.dev vs flotilla

*Research: 2026-06-18. Sources: herdr.dev + its docs/socket-api/integrations/compare/blog pages, the herdr GitHub repo (README + rendered docs), and the flotilla README. Maturity metrics (stars/releases) are point-in-time WebFetch reads — treat as directional.*

## What each product is

- **herdr** (herdr.dev, github.com/ogulcancelik/herdr) — an **agent-aware terminal multiplexer** ("one terminal for the whole herd" / "tmux for AI coding agents"). A Rust/Ratatui client-server multiplexer: workspaces/tabs/real-PTY panes with mouse support, persistent detach/reattach, SSH remoting, and — its differentiator over tmux/Zellij — **semantic per-agent state detection** (idle/working/done/blocked) in a sidebar, plus a local Unix-socket JSON API so agents can drive the terminal.
- **flotilla** (github.com/jim80net/flotilla) — a **drop-in coordination layer over existing harnesses**. A hub "XO" agent fans work to domain "desk" agents, collects replies, and keeps a durable auditable record of inter-agent traffic, over tmux panes + a Discord channel. Go, MIT.

**Crucial distinction: herdr is a RUNTIME/visibility layer; flotilla is a COORDINATION/delegation layer. Different altitudes of the same stack — more complementary than competing.**

## herdr — what actually ships (docs-grounded)

Mature, real product: ~6.1k GitHub stars, v0.7.0 (2026-06-15), 890 commits, active. Single Rust binary, no Electron/hosted control plane/accounts. Ships: workspaces/tabs/PTY panes w/ mouse split, detach/reattach persistence, SSH remote attach, copy-mode, 18 themes, notifications, opt-in session restore. **Agent state detection for 14+ agents** (Claude Code, Codex, Amp, Droid, OpenCode, Grok, Hermes, Cursor, Antigravity, Kimi, Copilot, Qoder, Kiro, Pi …) via process-name + output heuristics, plus deeper **native hook/plugin integrations** for several. **Socket API**: newline-delimited JSON — pane/tab CRUD, git-worktree checkout, send-text/keys, read pane content, layout export/apply, event subscriptions + wait-for-state.

**Marketed-vs-shipped (load-bearing finding):** herdr's own socket-API docs state the API does **NOT** enable inter-agent messaging or control — *"agents cannot spawn or command other agents."* Coordination is *indirect* (shared session state + lifecycle events + wait-on-status). There is **no hub/XO, no delegation, no Discord/Slack/chat/mobile surface, no notify-to-phone.** herdr deliberately drew its boundary at the multiplexer.

## Feature-by-feature

| Capability | herdr | flotilla | Notes |
|---|---|---|---|
| Core altitude | Terminal multiplexer / agent runtime | Coordination/delegation over harnesses | Complementary layers |
| Persistent PTY panes, detach/reattach | ✅ mature | ➖ relies on your tmux | herdr ahead — owns the runtime |
| Mouse-first TUI, themes, copy-mode | ✅ rich | ❌ not its job | herdr ahead |
| SSH remote attach | ✅ native | ➖ via your tmux/ssh | herdr ahead |
| Per-agent state detection | ✅ 14+ (heuristics + native hooks) | ✅ drivers for Claude Code, Codex, Grok (render markers live-captured) | herdr ahead on breadth |
| Socket/JSON API for agent→terminal control | ✅ extensive | ➖ CLI send/notify, not a terminal API | herdr richer |
| **Hub-and-spoke delegation (one→many)** | ❌ explicitly NOT supported | ✅ core (XO→desks) | **flotilla — central differentiator** |
| **Confirmed-delivery inter-agent messaging** | ❌ indirect state-share | ✅ send refuses dead panes | **flotilla ahead** |
| **Durable auditable inter-agent transcript** | ➖ session/event logs | ✅ every instruction+reply mirrored+timestamped | **flotilla ahead** |
| **Discord/chat/drive-from-phone** | ❌ none | ✅ Discord, per-agent webhooks | **flotilla — herdr absent** |
| Change-detector heartbeat (cost-throttled) | ➖ event subs, no XO clock | ✅ watch clock + ctx rotation | flotilla ahead on autonomy-loop |
| Federation (meta-XO over project-XOs) | ❌ | ✅ ships | flotilla — herdr absent |
| Maturity (stars/releases/cadence) | ✅ ~6.1k★, v0.7.0 | ➖ early | herdr far ahead |
| License | AGPL-3.0 + commercial | MIT | flotilla more permissive |

**Approach divergence:** herdr = build-the-runtime-agents-live-in (self-hosted binary, AGPL+commercial, terminal-centric). flotilla = coordinate-the-harnesses-you-already-run (MIT, chat-centric, drive-from-phone). *herdr makes panes smart; flotilla makes a fleet a team.*

## Where flotilla can compete

Honest framing: herdr is more mature, more popular, more polished, and broader on state detection; flotilla should NOT try to out-multiplex it. But herdr drew its boundary at the multiplexer ("agents cannot command each other"), leaving the entire coordination / delegation / remote-human-in-the-loop space open.

**flotilla's genuine differentiators (herdr does none):**
1. Hub-and-spoke delegation with a real broker (XO→desks).
2. Confirmed-delivery + durable auditable inter-agent transcript (a governance/compliance product, not a log).
3. Drive-the-whole-fleet-from-Discord-on-your-phone (herdr's sharpest absence).
4. MIT-licensed, drop-in coordination over existing harnesses (vs herdr's AGPL+commercial heavier adopt).

**Gaps flotilla should consider adopting from herdr:**
- Native hook/plugin state integrations instead of pure output-heuristic drivers (more reliable; the path to 14+ agents).
- Session persistence/restore as a first-class concern (herdr owns it; flotilla leans on the operator's tmux).
- Maturity signals: cut releases, grow visible adoption (the star gap will matter to evaluators).

**Competitive angles:**
1. **"herdr watches the herd; flotilla RUNS the herd."** Position as the coordination layer above a multiplexer — and ship a **herdr integration** (consume its socket-API state instead of re-deriving it). Turn the competitor into a substrate.
2. Own **remote human-in-the-loop** (Discord/phone + confirmed delivery + audit) — the axis herdr is structurally absent on.
3. Sell the **audit/compliance** story (attributable who-told-whom-what across a fleet).
4. **Bounded autonomous loops + the change-detector clock** — coordination-economics herdr's event subs don't package.
5. **Federation** (meta-XO → project-XOs → desks) — natural for a coordination layer, unnatural for a multiplexer; neither ships it yet.
