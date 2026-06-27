# flotilla — agent guide (read before you change anything)

flotilla is a **public, open-source product**: a fleet-coordination tool (tmux
delivery + an audited chat coordination bus, hub-and-spoke routing). It is built
by **dogfooding it on a private deployment** — but the product and any one
deployment are two different things, and that boundary is the first thing to
internalize before you touch this repo.

## 1. The public/private partition — the load-bearing rule

**Every artifact in this repo is PUBLIC. Never put a deployment-specific
identifier in one.** Code, tests, fixtures, docs, the landing site, issues, pull
requests, and commit messages all describe **generic flotilla capabilities**.
The specifics of any one deployment — which agents/"desks" exist, what they're
named, which org runs them, what external services they use, absolute home
paths, real chat ids — live ONLY in that deployment's roster and host-local
config, which are gitignored and never published.

Keep the **feature**, strip the **deployment**. When you write a fixture, a
comment, or a doc, use the generic roles from `flotilla.example.json`
(`xo`, `backend`, `frontend`, `data`, …) — a reference a new developer learns
from — not your own fleet's names. A redaction is a *generalization*, never a
deletion: a reader must still fully understand the generic capability.

Enforcement (but the enforcement is a **net, not a substitute** for the framing
above):
- `docs/private-public-boundary.md` — the full doctrine (what's private, what's
  the product, the breach runbook).
- `scripts/check-private-boundary.sh` + the `private-boundary` CI job — fails on
  a known-private token. A denylist only catches what it already knows; it does
  NOT catch a novel deployment term you coin. **The partition is your
  responsibility; the guard is the backstop.**

## 2. The private firewall — where deliberation lives

Public PRs and issues carry **generic product work only**. **Strategic
deliberation, decisions held for the operator, internal status notes, and
to-do lists do NOT go in a public PR or issue** — they belong behind the
private firewall: the local filesystem (gitignored working files) and the
operator's private channel. Using the issue/PR system as a scratchpad is the
exact habit that drags private context into the public surface. When you are
holding something for an operator conversation, write it to a local/private
file and raise it on the private channel — not as a public artifact.

## 3. Why this file exists (the gap it closes)

flotilla previously had **no agent constitution establishing this partition** —
only an install guide (`llm.md`). The result was predictable: agents dogfooding
the tool wrote their real fleet's identifiers into the public tree, issues, and
commit history, and a privacy leak followed. The *framing* was the root cause;
the cleanup was the symptom. This file makes the partition first-class so the
next contributor never has to rediscover it.

## 4. Constitutional learnings propagate UP into this file

flotilla is built by dogfooding it on a live fleet. When that dogfooding
surfaces a **framework-level** gap — a principle the tool itself should have
taught and didn't (the partition above, and the private firewall, are the first
two) — the fix belongs **here, in the public constitution**, not only in the
private deployment's own rules. A gap found while dogfooding is a *product* gap.
Add the generalizable principle to this file so the next contributor and the
next deployment inherit it. The long-term aim is for flotilla to be an operating
system for agentic work; these semantics are part of that operating system.

## 5. Setup

To install and configure flotilla, see `llm.md`.
