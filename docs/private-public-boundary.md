# The public/private boundary

`jim80net/flotilla` is a **public, open-source product**. It is dogfooded on a
private deployment (one operator's fleet), but the product and the deployment are
two different things — and the boundary between them is load-bearing.

**A flotilla public artifact NEVER contains a deployment-specific identifier.**
Public artifacts (code, tests, docs, README, the landing site, issues, PRs,
commit messages) describe **generic flotilla capabilities**. The specifics of any
one deployment — which desks exist, what they're named, which org runs them, what
broker or data vendor they talk to, what accounts they hold — live **only** in the
private deployment's roster and host-local config, which are gitignored and never
published.

This is the [`separate-circumstantial-from-generalizable`] rule hardened into a
repo invariant: every fleet artifact has two layers, and only the *generalizable*
layer is public.

## What is private (never put it in a public artifact)

| Class | Examples (private) | Generic abstraction (public) |
|---|---|---|
| Desk / agent names | a primary XO's name; a federated XO's name; a worker desk's name | "the primary XO", "a federated XO", "a desk", `xo-1`, `desk-a` (fixtures) |
| Org / venture names | the fleet's org/company/project names | "a private deployment", "the fleet" |
| Broker / data vendor | the specific broker; the specific market-data vendor | "a broker", "a data vendor" |
| Trading specifics | instrument tickers; account ids; a strategy's name; a trading daemon's name | "a high-consequence action/system", drop tickers, "an external account" |
| Approval-sensitive desks | "real-money desk", "the desk that places orders" | "an approval-sensitive desk", "a high-consequence desk" |
| Host / path / channel | absolute home paths; hostnames; live Discord channel ids; private-repo issue refs | `$HOME`/`/home/operator/...`, "the host", placeholder ids |

Keep the **feature**, strip the **deployment**. A redaction is a *generalization*,
never a deletion — a reader must still fully understand the generic capability.

## What is NOT private (these are the product — keep them)

- Product commands & units: `flotilla watch`, `flotilla dash`, `flotilla doctor`,
  `flotilla voice`, and their `*.service` / `*.env` deployment artifacts — including
  the `flotilla-dash` web-surface feature (the command/service, **not** a desk that
  happens to be named after it).
- Generic roles & concepts: "XO", "desk", "federation", "c2 channel", "the operator".
- The public repo slug `jim80net/flotilla`, the LICENSE author, public CLI/model
  names (grok, aider, opencode, …), and intentional fake placeholders in fixtures.

## The guard

`scripts/check-private-boundary.sh` greps a denylist of known-private identifiers
across the tracked tree (and, with `--issues`, open issues + PRs) and **fails on a
hit**. It runs in CI on every push and PR (`.github/workflows/ci.yml`, the
`private-boundary` job), so a deployment specific can never silently re-enter the
public surface.

When you coin a new private identifier in the deployment, **add it to the denylist**
in the guard. The denylist is intentionally high-signal (tokens with no legitimate
generic use) so the guard never flaps; genuinely ambiguous deployment words
(instrument tickers, common nouns that double as a desk name) are caught by review
against this doc, not by the guard.

## If a breach happens

1. **Assess exposure** — forks/clones/stars (near-zero propagation means in-place
   redaction is effective; there is nothing cloned to chase).
2. **Redact every surface** — issues, PR titles/bodies, inline review comments,
   code, tests, docs, the landing site. Rewrite each private token to its generic
   abstraction (table above), preserving the technical meaning.
3. **Verify** — run the guard, and re-grep issues+PRs independently, until zero
   genuine hits remain.
4. **Residual:** commit-message history is immutable without a force-push (which is
   banned here); given near-zero exposure the residual is accepted and the guard
   prevents new ones. A history-rewrite is a separate, operator-authorized decision.
5. **Binary media** (demo gif/mp4) can't be text-scrubbed — verify against its
   tracked HTML source, and visually audit/regenerate if the source is unavailable.

[`separate-circumstantial-from-generalizable`]: the fleet-coordination rule that a
capability is public, a deployment is private.
