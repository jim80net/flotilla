# PR authoring — write for the human reviewer

Pull requests are a **public, reader-facing surface** in the flotilla tree — the same
reader-modeling standard as operator comms applies here. A reviewer opens your PR cold
from `main`: no issue-thread memory, no coordination chat, no shared vocabulary from
your desk session. The title and description must answer **what changed** and **why it
matters** in plain language a capable engineer can follow without decoding internal
shorthand.

This doc is the **canonical home** for PR title/description rules. Agents load the
companion skill [`.claude/skills/flotilla-pr-authoring/SKILL.md`](../.claude/skills/flotilla-pr-authoring/SKILL.md)
for the mechanical workflow (file-based delivery, `gh` invocation, Mermaid checks).

**Principle:** [OPERATING-PRINCIPLES.md §5](./OPERATING-PRINCIPLES.md) (reader-modeling).
**Large diffs:** escalate to the `comprehensive-pr-description` skill (installed in the
agent's skill path) for the full chunk/Mermaid/template treatment.
**Review gate:** coordinators follow [`coordinator-runbooks/merge-gate.md`](./coordinator-runbooks/merge-gate.md).

## The reader you are writing for

Model one person: a **technically capable reviewer who has not been in your session**.
They need to:

1. Understand the **purpose** of the change in the first screenful.
2. Follow **logically distinct units of work** — not a commit log, not a file inventory.
3. See **why** each unit exists (problem → approach), with jargon glossed on first use.
4. Trust the **verification** section because it cites checkable facts, not vibes.

If the description reads like it was written for a linter, a merge bot, or another
agent's context window, rewrite it. Gate vocabulary (`CLEAR`, `cubic`, `P1`,
`flotilla-dispatch-*`) belongs in coordination traffic or a detail footer — not in
the lead paragraph.

## Titles

The title is the **one-line answer** to "what does this PR introduce?"

| Avoid | Prefer |
|---|---|
| `fix stuff` / `updates` / `WIP` | `Skip Discord mirror when notify posted within 3 minutes` |
| `feat/org-truth-pr4` (branch name as title) | `Expose org DAG on dash topology API and goals spokes` |
| `Address review feedback` | `Reject cyclic fleet-org.yaml at load time with path in error` |
| Internal codenames without gloss | Generic capability names from `flotilla.example.json` roles |

**Shape:** imperative or outcome phrase; ≤ ~72 characters when practical; no trailing
period. Name the **behavior or capability**, not the process (`Phase 2`, `PR4`,
`gate fixes`).

## Descriptions — structure by size

### Small PRs (single concern, few files)

A tight description is enough:

```markdown
## Summary

<2–4 sentences: what changed mechanically and why.>

## Test plan

- [x] <what you ran>
```

### Medium and large PRs

Use the **logical-chunk** shape (full template in `comprehensive-pr-description`):

1. **TL;DR** — one paragraph with the headline outcome.
2. **Why this PR exists** — problem, timing, approach; cite issues/PRs with one-line glosses.
3. **Logical chunks** — each with **What** / **Why**; 6–12 chunks for large diffs.
4. **Verification** — table of surfaces and states (test counts, CI, review tools).
5. **Test plan** — checklist; unchecked items explain deferral.
6. **References** — issues, sibling PRs, design docs, each glossed.

Organize by **what changed**, not by commit SHA or file path. Phases and branch
names are authoring metadata, not reader structure.

## Mermaid diagrams — judicious, correctly formatted

Diagrams **replace paragraphs** when they show structure or flow the reader would
otherwise have to reconstruct. Skip them for two-step flows prose handles fine.

| Showing | Mermaid type |
|---|---|
| Decision / refactor flow | `flowchart` (LR or TB) |
| Process interactions | `sequenceDiagram` |
| State machine | `stateDiagram-v2` |
| Schema / FK relationships | `erDiagram` |
| Package or module hierarchy | `flowchart TB` with `subgraph` |

### Formatting rules (GitHub rendering)

GitHub renders Mermaid only inside **fenced code blocks** tagged `mermaid`. Common
failures:

1. **Missing blank lines** — put an empty line before and after the fence so the
   block is not glued to a heading or list item.
2. **Indented fences** — top-level fences start at column 0 in the markdown file.
3. **Special characters in labels** — wrap node text in double quotes when it
   contains `()`, `[]`, or `:`.
4. **Subgraph syntax** — use `subgraph id [Label]`; test render before pushing.

Example — in the PR body file, separate the heading, prose, diagram, and closing
prose with **blank lines**. The `mermaid` fence is top-level (column 0), not nested
inside a list item:

    ## Data path

    The watch daemon and dash share one org DAG source.

    ```mermaid
    flowchart LR
        Channels["channels[] in roster"] --> Derive["DeriveFromChannels"]
        OrgFile["fleet-org.yaml"] --> Agree["Agree + validate"]
        Derive --> Config["Config.Org()"]
        Agree --> Config
        Config --> Watch["watch synthesis"]
        Config --> Dash["/api/topology"]
    ```

    Both consumers read the same derived graph.

After pushing the body, **open the PR in the browser** and confirm diagrams render.
Syntax errors show as plain text and are easy to miss in the terminal.

## Mechanical delivery — write to disk, never interpolate through Bash

PR bodies are **multi-line markdown** with backticks, quotes, `$`, and Mermaid
braces. Passing them through Bash string interpolation (`-b "$(cat <<'EOF'…)"`,
`echo`, `printf`, heredocs inside `$(…)`) corrupts content silently or turns
Mermaid into shell syntax errors.

**Rule:** compose the description in a **file** (editor or `Write` tool), then
submit that file bytes verbatim.

### Create

```bash
mkdir -p .claude/pr-bodies
# compose at .claude/pr-bodies/pr-<N-or-branch>.md — never inline in gh -b
gh pr create \
  --title="Expose org DAG on dash topology API" \
  --body-file=.claude/pr-bodies/pr-my-feature.md
```

### Update existing PR

```bash
mkdir -p .claude/pr-bodies
gh pr view <N> --json body --jq '.body' > .claude/pr-bodies/pr-<N>.md
# edit the file on disk (additive updates preferred — see comprehensive-pr-description)
gh pr edit <N> --body-file=.claude/pr-bodies/pr-<N>.md
# verify the edit took:
gh pr view <N> --json body --jq '.body' | head -5
```

If `gh pr edit` fails on classic project cards, use the REST API (body as raw JSON
string — preserves newlines):

```bash
jq -Rs '{body: .}' < .claude/pr-bodies/pr-<N>.md > /tmp/pr-body.json
gh api -X PATCH repos/<owner>/<repo>/pulls/<N> --input /tmp/pr-body.json
```

Use `jq -Rs` (raw + slurp), not `--field body=@file`, which chokes on markdown
quotes.

### Dash tracker precedent

`internal/dash/tracker/gh.go` already submits issue bodies via stdin
(`gh issue create --body-file=-`) so shell parsing cannot reinterpret content.
PR authoring follows the same discipline.

### Anti-patterns (do not)

- `gh pr create -b "$(python -c 'print("""...""")")"` — quote/escape roulette.
- Heredoc inside command substitution for the final body.
- Piping through `sed`/`awk` to "fix" Mermaid after composition — edit the source file.
- Composing from memory on `gh pr edit` when a description already exists — fetch
  to disk first, then add.

Direct GitHub API calls (`gh api`, `PATCH` pulls) are fine when the body file is
the payload source.

## Public/private boundary

PR text is **public**. Use generic roles and examples from `flotilla.example.json`
(`xo`, `backend`, `frontend`, …). Never put deployment roster paths, real operator
handles, guild ids, or fleet-specific desk names in titles or bodies. Redact to the
**generic capability**; see [`private-public-boundary.md`](./private-public-boundary.md).

## Quick self-check before opening the PR

1. **Title** — states the capability/outcome, not the branch or gate phase.
2. **Lead** — a stranger learns what changed without opening the diff.
3. **Chunks** — logical units with What/Why, not commit/file lists.
4. **Diagrams** — each earns its space; blank lines around fences; verified in browser.
5. **Delivery** — body read from `.claude/pr-bodies/*.md` via `--body-file` or REST.
6. **Verification** — specific counts and surfaces, not "tests pass."
7. **Boundary** — no deployment-specific identifiers.