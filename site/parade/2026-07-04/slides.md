Set Your Agents Free — Independence Day 2026
🇺🇸 On America's 250th birthday, flotilla ships a **drop-in chief of staff** for the AI coding agents you already run — Claude Code, Codex, Grok, and more. One hub coordinates your fleet; you drive strategy, not terminal shuffling.
**[Browse the one-month story below →]** · **[Set it up in minutes](../#start)**

---
Inaugural Parade — one hub, one month
**[240 commits](https://github.com/jim80net/flotilla/commits/main), [239 merged pull requests](https://github.com/jim80net/flotilla/pulls?q=is%3Apr+is%3Amerged), 32 days** — from [June 2](https://github.com/jim80net/flotilla/commit/e8f0591) to July 4, 2026.
This deck is the flotilla **product** parade: what shipped in the open-source repo. A live fleet dogfoods it daily — their wins march in reflection, never by name.

---
The founding idea — June 2
It began with a [LICENSE and a one-line promise](https://github.com/jim80net/flotilla/commit/a1bfd5b):

> **"Coordinate a fleet of AI coding agents from a single hub — with a durable, auditable record of everything they say to each other."**

The diagnosis was simple: run several long-lived coding agents and *you* become the message bus — shuffling between terminals, holding the org chart in your head, leaving no record. flotilla's bet was to build the coordination layer on substrate you already have — a terminal multiplexer and a chat channel — [not a new daemon or a hosted service](https://github.com/jim80net/flotilla/commit/a1bfd5b).

---
Era I — Birth: the send, and the clock that never stops
The first two pull requests are the whole heartbeat of the product.

- **[#1 — `send`](https://github.com/jim80net/flotilla/pull/1):** deliver an instruction by typing it into an agent's pane, and mirror every message to a chat channel. Injecting the text *is* the wake — nothing to poll. The audit trail was a first-class feature from commit one.
- **[#2 — `watch`](https://github.com/jim80net/flotilla/pull/2):** the inbound relay plus a **self-continuing hub clock** — the executive officer that checks in on its desks every tick without a human turning the crank.
- Then the clock got *smart*: [directive heartbeats (#3)](https://github.com/jim80net/flotilla/pull/3), an [operator-facing outbound path (#12)](https://github.com/jim80net/flotilla/pull/12), and a [change-detector that wakes only on a material change (#22)](https://github.com/jim80net/flotilla/pull/22) instead of burning cycles.

---
Era II — Many harnesses, one fleet
An early architectural bet: don't marry one agent. [PR #21](https://github.com/jim80net/flotilla/pull/21) put **Claude Code behind a Driver abstraction, byte-for-byte identical** — and then the fleet learned to speak to everyone:

- [`aider` (#52)](https://github.com/jim80net/flotilla/pull/52) · [`opencode` (#56)](https://github.com/jim80net/flotilla/pull/56) · [`grok` (#59)](https://github.com/jim80net/flotilla/pull/59) · [`Codex` (#259)](https://github.com/jim80net/flotilla/pull/259)
- [Inter-harness pull-only messaging (#64)](https://github.com/jim80net/flotilla/pull/64) and [secure push-to-hub for "smart" desks (#66)](https://github.com/jim80net/flotilla/pull/66) — agents of different make, coordinating in one fleet.

The promise sharpened: **drop-in agentize the harness you already run, don't replace it.**

---
Era III — Federation and the Chief of Staff
As fleets grew past one hub, flotilla grew a hierarchy. [PR #105](https://github.com/jim80net/flotilla/pull/105) gave each flotilla **its own channel and a fleet-command return leg**; [PR #109](https://github.com/jim80net/flotilla/pull/109) introduced the **Chief of Staff** — a coordinator-of-coordinators with a context mirror. [Mechanical channel provisioning (#119)](https://github.com/jim80net/flotilla/pull/119) and [one XO hubbing multiple channels (#123)](https://github.com/jim80net/flotilla/pull/123) made the org chart real infrastructure, not a diagram.

---
Era IV — The Dash: a native window into the fleet
The chat channel was the audit trail; the **dashboard** became the operator's cockpit. Built in phases off [its design (#122)](https://github.com/jim80net/flotilla/pull/122):

- [Phase 1 — a read-only command-and-control web surface (#124)](https://github.com/jim80net/flotilla/pull/124)
- [Phase 2 — a native, GitHub-backed issue tracker (#125)](https://github.com/jim80net/flotilla/pull/125)
- [Phase 3 — a live control surface with an operator note, on a cross-process pane lock (#126, #129)](https://github.com/jim80net/flotilla/pull/129)

![The flotilla dashboard's live fleet map](goals-map-before-pinwheel.png)

---
Era V — Doctrine became mechanical
The hardest lesson of the month: **a promise to "do better" is not enforcement — plumbing is.** flotilla turned its operating hard-won rules into installed, mechanical doctrine:

- [An installable constitutional skillset + the Rule of Three span-of-control (#137)](https://github.com/jim80net/flotilla/pull/137)
- [No-self-merge as a doctrine member — the review *is* the merge gate (#144)](https://github.com/jim80net/flotilla/pull/144)
- [A mechanical detector that breaks the "idle-hold" antipattern (#223)](https://github.com/jim80net/flotilla/pull/223)
- [The Flotilla Operating Principles, shipped as an installed constitution (#233)](https://github.com/jim80net/flotilla/pull/233)

Every correction became a gate, a fail-closed path, or a corrected default — never a note-to-self.

---
Era VI — Resilience and a portable coordinator seat
A fleet that runs unattended must heal itself and never depend on one vendor:

- [`flotilla switch` — cross-harness desk failover (#203)](https://github.com/jim80net/flotilla/pull/203) and [auto-switch workers off a sustained rate-limit storm (#228)](https://github.com/jim80net/flotilla/pull/228)
- [A pluggable Transport bus (#188)](https://github.com/jim80net/flotilla/pull/188) that later carried [the live dashboard as a first-class ingress (#199)](https://github.com/jim80net/flotilla/pull/199)
- [Adaptive heartbeat cadence — a policy engine that speeds up and slows down with the work (#247)](https://github.com/jim80net/flotilla/pull/247)
- [A harness-portable coordinator seat, so the XO/CoS role runs on Codex or grok too (#261, #263)](https://github.com/jim80net/flotilla/pull/263)

---
Era VII — Goals: the fleet's purpose, made visible
The dash learned to show not just *what desks are doing* but *why*. The [Goals view (#277)](https://github.com/jim80net/flotilla/pull/277) rendered the fleet's purpose hierarchy live; [contract edges compiled from YAML (#281)](https://github.com/jim80net/flotilla/pull/281) and a [pan/zoom Fleet Situation Map (#282)](https://github.com/jim80net/flotilla/pull/282) turned it into a navigable map; the [org-graph v2 (#315, #316)](https://github.com/jim80net/flotilla/pull/316) added a hub-and-spoke org layout, harness badges, priorities, and milestones.

![The fleet's goals, as a live map](goals-map-realdepth.png)

---
The last 48 hours — operator feedback, same day
July 3–4 was a transformation: the operator lived in the product and the fleet answered every note, same-day.

- **[Warm-light theme as the default (#332)](https://github.com/jim80net/flotilla/pull/332)** — a design-book evolution; the cockpit stopped looking like every other AI dashboard.
- **[A decision brief inside the respond modal (#348)](https://github.com/jim80net/flotilla/pull/348), auto-triggered for operator-gated goals ([#352](https://github.com/jim80net/flotilla/pull/352))** — because *"a bare 'waiting on you' label is not decidable."*
- **[The executive mini-brief — a sixth constitutional member (#239)](https://github.com/jim80net/flotilla/pull/239)** — every operator-facing message leads with a plain-language bottom line.
- **[Reader-modeled the landing page and the docs (#328, #344)](https://github.com/jim80net/flotilla/pull/344)** — write to the reader's mental map, or it's slop.

---
The parade and the mind-map
Two capabilities that made *today* possible:

- **[Parade formation — an operator-triggered fleet ceremony (#336)](https://github.com/jim80net/flotilla/pull/336)**, later given [its own archive page served by the dash (#373)](https://github.com/jim80net/flotilla/pull/373). This deck is the first product parade to march from the init commit.
- **[A mind-map layout for the goals map (#364)](https://github.com/jim80net/flotilla/pull/364), [tuned to stay overlap-free at real depth (#377)](https://github.com/jim80net/flotilla/pull/377)** — the fleet's purpose, finally legible at a glance.

![The mind-map goals layout](goals-map-realdepth.png)

---
What ships today
An honest inventory of what you get, right now:

- **Coordinate** any mix of harnesses — Claude Code, grok, aider, opencode, Codex — from one hub, with [`send`](https://github.com/jim80net/flotilla/pull/1), a [self-continuing adaptive clock](https://github.com/jim80net/flotilla/pull/247), and [`notify`](https://github.com/jim80net/flotilla/pull/12) for operator replies.
- **Federate** across flotillas with a [Chief of Staff over project XOs over desks (#105, #109)](https://github.com/jim80net/flotilla/pull/109).
- **See** it all in a [native dashboard](https://github.com/jim80net/flotilla/pull/126) — fleet map, [issue tracker](https://github.com/jim80net/flotilla/pull/125), [live goals graph](https://github.com/jim80net/flotilla/pull/277), and conversation threads.
- **Trust** it: [installed constitutional doctrine](https://github.com/jim80net/flotilla/pull/233), [no-self-merge review gates](https://github.com/jim80net/flotilla/pull/144), [cross-harness failover](https://github.com/jim80net/flotilla/pull/203), and a [public/private firewall](https://github.com/jim80net/flotilla/pull/187) so a private deployment never leaks into a public repo.

---
Set Your Agents Free
From a [LICENSE on June 2](https://github.com/jim80net/flotilla/commit/e8f0591) to a self-hosting, self-healing, self-documenting fleet-coordination product on July 4 — **[240 commits, 239 merged pull requests](https://github.com/jim80net/flotilla/pulls?q=is%3Apr+is%3Amerged)**, and a doctrine that turns every lesson into plumbing.

Happy Independence Day. **[Set it up in minutes →](../#start)** · **[Star on GitHub ↗](https://github.com/jim80net/flotilla)**