# The public/private boundary

flotilla is a **public, open-source product** that you run on your **own private
deployment** — your fleet, with your desk names, your org, your broker or data
vendor, your accounts. The product and your deployment are two different things,
and the boundary between them is load-bearing.

**A flotilla public artifact NEVER contains a deployment-specific identifier.**
Public artifacts (code, tests, docs, README, the landing site, issues, PRs,
commit messages) describe **generic flotilla capabilities**. The specifics of any
one deployment — which desks exist, what they're named, which org runs them, what
broker or data vendor they talk to, what accounts they hold — live **only** in
that deployment's roster and host-local config, which are gitignored and never
published.

This is one principle: a *capability* is generalizable and public; a *deployment*
is circumstantial and private. Every fleet artifact has those two layers, and
only the generalizable layer ships.

## What is private (never put it in a public artifact)

| Class | Examples (private) | Generic abstraction (public) |
|---|---|---|
| Desk / agent names | a primary XO's name; a federated XO's name; a worker desk's name | "the primary XO", "a federated XO", "a desk"; in fixtures use generic roles — `xo`, `backend`, `frontend`, `data` |
| Org / venture names | the fleet's org/company/project names | "a private deployment", "the fleet" |
| Broker / data vendor | the specific broker; the specific market-data vendor | "a broker", "a data vendor" |
| Domain specifics | instrument tickers; account ids; a strategy's name; a high-consequence daemon's name | "a high-consequence action/system", drop tickers, "an external account" |
| Approval-sensitive desks | the desk that places real orders | "an approval-sensitive desk", "a high-consequence desk" |
| Host / path / channel | absolute home paths; hostnames; live chat channel ids; private-repo refs | `$HOME` / `/home/operator/...`, "the host", placeholder ids |

Keep the **feature**, strip the **deployment**. A redaction is a *generalization*,
never a deletion — a reader must still fully understand the generic capability.
Fixtures and examples should read as a **reference a developer learns from**
(model them on `flotilla.example.json`), not as anonymized copies of one fleet.

## What is NOT private (these are the product — keep them)

- Product commands & units: `flotilla watch`, `flotilla dash`, `flotilla doctor`,
  `flotilla voice`, and their `*.service` / `*.env` artifacts — including the
  `flotilla-dash` web-surface feature (the command/service, **not** a desk that
  happens to be named after it).
- Generic roles & concepts: "XO", "desk", "federation", "c2 channel", "the operator".
- The public repo slug `jim80net/flotilla`, the LICENSE author, public CLI/model
  names (grok, aider, opencode, …), and intentional fake placeholders in fixtures.

## The guard

`scripts/check-private-boundary.sh` greps for leaks across the tracked tree (and,
with `--issues`, open issues + PRs) and **fails on a hit**. It runs in CI on every
push and PR (`.github/workflows/ci.yml`, the `private-boundary` job). It has two
layers:

1. **Built-in, deployment-agnostic patterns** — leaks that are private for
   *anyone*: absolute home paths revealing a username, chat webhook URLs, common
   secret shapes. No configuration; these protect every clone out of the box.
2. **Your deployment denylist** — the names only *your* fleet uses. These are
   **not** shipped in the guard (that would publish your vocabulary). You provide
   them yourself:
   - copy `.flotilla/private-denylist.example` → `.flotilla/private-denylist`
     (gitignored) and list your terms, one per line; and/or
   - in CI, set the `FLOTILLA_PRIVATE_DENYLIST` repo secret to the same content,
     so the list is never committed.

   Keep the denylist high-signal (tokens with no legitimate generic use) so the
   guard never flaps; genuinely ambiguous deployment words are caught by review
   against this doc, not by the guard.

   **The guard misses what it doesn't know — review for these leak classes the
   high-signal denylist won't catch:**
   - **Lowercase / generic-looking identifiers** — a `tmux` SESSION name
     (`myfleet:3.1` as a fixture pane target), a host short-name, an org slug
     in lowercase. A denylist keyed on the capitalized brand (`\bMyfleet\b`)
     sails right past a lowercase `myfleet:3.1` in a test fixture. Use a generic
     session name (`flotilla:3.1`) in fixtures.
   - **Provenance comments** — "verified live on the `<fleet>` fleet,
     `<date>`", "the REAL bytes from `<deployment>`". These narrate where a
     fixture came from and name the deployment doing it. Say "a running
     deployment" instead. The fixture's *value* (the captured bytes) is the
     product; *whose* fleet produced it is the deployment.
   The partition is the author's responsibility; add any such token you coin to
   YOUR deployment denylist so the backstop catches the next one.

## If a breach happens

1. **Assess exposure** — forks / clones / stars. Near-zero propagation means an
   in-place redaction is effective; there is nothing cloned to chase.
2. **Redact every surface** — issues, PR titles/bodies, inline review comments,
   code, tests, docs, the landing site. Rewrite each private token to its generic
   abstraction (table above), preserving the technical meaning.
3. **Verify** — run the guard, and re-grep issues + PRs independently, until zero
   genuine hits remain.
4. **Commit-message history** — branches can be cleaned by a history rewrite +
   force-push. This is a heavy, deliberate decision (it rewrites public history
   and invalidates existing clones / open PRs), so do it early, when exposure is
   near-zero, or accept the residual and rely on the guard to stop new leaks.
   GitHub's `refs/pull/*` (frozen PR refs) and orphaned objects are not reachable
   by a normal clone and age out of GitHub's garbage collection; a force-push
   cannot rewrite them.
5. **Binary media** (demo gif/mp4) can't be text-scrubbed — verify against its
   tracked HTML source, and visually audit / regenerate if the source is
   unavailable.
