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
  names (claude-code, codex, grok, …), and intentional fake placeholders in fixtures.

## The guard

`scripts/check-private-boundary.sh` greps for leaks across the tracked tree (and,
with `--issues`, open issues + PRs; with `--file <path>`, one file's contents — the
mode the pre-commit / pre-push hooks and the conformance test use). It **fails on a
fail-closed hit** and runs in CI on every push and PR (`.github/workflows/ci.yml`,
the `private-boundary` job). It has two fail-closed layers plus an advisory third:

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
3. **Your deployment warnlist (advisory)** — your domain *vocabulary*, loaded the
   same gitignored way but **never a failure**: a hit prints a `WARN` section and
   exits 0. See "The advisory WARN tier" below.

## Two egresses, one partition: the static guard AND the runtime firewall

A deployment specific can reach the public in two ways, and each has its own guard:

- **The static egress** — a specific committed into the tree, an issue, or a PR.
  Guarded by `scripts/check-private-boundary.sh` (above) in CI.
- **The runtime egress** — a specific a desk publishes through flotilla itself: an
  auto-mirrored turn-final, a `flotilla notify` to the operator, a routed hotline
  reply. Guarded by the **runtime firewall** (`internal/readermap`), the runtime
  half of this same partition.

The two guards share their **data** (the gitignored deny/warn term lists below), not
their code — the runtime firewall is Go (RE2) and the static guard is bash (PCRE), so
they cannot share regex. A **conformance test** (`firewall_conformance_test.go`) feeds
a shared fixture corpus through both and fails on any verdict mismatch, so they can
never silently diverge on the shared term-list surface.

### The runtime firewall (refuse, never rewrite)

Before flotilla publishes any outbound artifact it runs it through the firewall, which
returns one of three verdicts:

- **REFUSE** — a denylist term, a built-in generic leak (a username-revealing home
  path, a webhook URL, a secret shape), or the canonical tmux `<desk>:<window>.<pane>`
  / `#<deployment>-c2` reference. The artifact is **withheld and never rewritten** —
  generalizing a specific *inside a sentence whose meaning depends on it* would corrupt
  the message, which is worse than withholding it. What "withheld" means depends on the
  egress: the auto-mirror **suppresses** the post and raises the operator alert; the
  `notify` CLI **bounces** the offending token + its generic abstraction to the desk to
  fix in-context; the daemon reply-watcher **suppresses** the route and **escalates**
  (read-the-pane). Either way the leak is never published.
- **WARN** — see the advisory tier below.
- **OK** — published unchanged (clean traffic is byte-identical to before the firewall).

The firewall is a **denylist + pattern** backstop: it catches enumerated terms and the
canonical pattern, but **not a novel deployment word a desk coins** — the partition is
still the author's responsibility (§ above). It is the net, not the substitute.

### The advisory WARN tier (domain vocabulary)

Beside the fail-closed denylist sits an **advisory** tier: your deployment's domain
**vocabulary** — the jargon that, woven into ordinary prose or an example/branch name,
would deanonymize your fleet even with no hard identifier present. It is loaded exactly
like the denylist, from a gitignored source (the vocabulary is never committed):

- copy `.flotilla/private-warnlist.example` → `.flotilla/private-warnlist`
  (gitignored), one term/PCRE-regex per line; and/or
- in CI, set the `FLOTILLA_PRIVATE_WARNLIST` repo secret to the same content.

A warnlist hit is **advisory on both egresses**: the runtime publishes anyway with an
operator-visible advisory; the static guard prints a `WARN` section and **exits 0**. It
is high-false-positive by construction (a common word that doubles as domain jargon), so
it earns a human glance, never a block — a human adjudicates each WARN. A **denylist hit
still refuses** (denylist precedence); the WARN tier only adds an advisory class below
the fail-closed one.

### Three layers: pre-commit, pre-push, CI

The same static guard runs at three points (additive; none weakens another):

| Layer | When | What is scanned | Authority |
|-------|------|-----------------|-----------|
| **pre-commit** (`scripts/hooks/pre-commit`) | Before a commit is created | **Staged** added lines (`git diff --cached`) | Local backstop (`--no-verify` bypasses) |
| **pre-push** (`scripts/hooks/pre-push`) | Before push leaves the clone | Added lines in the **push range** (+ gofmt/vet) | Local backstop (`--no-verify` bypasses) |
| **CI** (`private-boundary` job) | Every push and PR | Tracked tree (+ open issues/PRs with denylist secret) | **Enforcing gate of record** |

Install local hooks with `scripts/install-hooks.sh` (sets `core.hooksPath` →
`scripts/hooks` for this clone only). Pre-commit catches a leak at commit time so
it never becomes a commit object; pre-push is a second local net; CI remains the
authority when a contributor skips hooks. Fail-closed hits exit 1 (block);
advisory WARN prints and exits 0.

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
