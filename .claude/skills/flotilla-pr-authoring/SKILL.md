---
name: flotilla-pr-authoring
description: Write PR titles and descriptions for human reviewers cold from main — plain-language reader modeling, judicious Mermaid, and file-based delivery via gh --body-file or REST (never Bash string interpolation). Use when opening or updating a PR, when the operator asks for a better PR description, or when a description reads like robot/gate boilerplate.
type: skill
queries:
  - "write PR title and description"
  - "PR description reads like robots"
  - "gh pr create body mermaid heredoc"
  - "update PR description flotilla"
  - "pr body-file delivery"
keywords:
  - pr-authoring
  - body-file
  - reader-modeling
  - mermaid
  - gh pr create
  - gh pr edit
boost: 0.05
---

# flotilla PR authoring — human reader, mechanical delivery

Canonical prose: [`docs/pr-authoring.md`](../../../docs/pr-authoring.md). This skill is
the **workflow** agents run when opening or revising a pull request in the flotilla repo
(or any repo following the same standard).

## When to use

- `gh pr create` / `gh pr edit` is imminent
- Operator or reviewer says PRs "look written for robots"
- Body contains Mermaid, tables, or code fences
- Any temptation to use a Bash heredoc or `-b "$(…)"` for the body

## When to escalate

| Situation | Skill / doc |
|---|---|
| Large diff (> ~10 commits, multiple subsystems, new packages) | `comprehensive-pr-description` |
| Merge review procedure | `docs/coordinator-runbooks/merge-gate.md` |
| Public vs deployment identifiers | `docs/private-public-boundary.md` |

## Reader model (one sentence)

Write for a **capable engineer who just opened the PR from `main`** — no session context,
no issue-thread memory. Lead with what changed and why; gloss jargon on first use.

## Title

- Outcome or capability in plain language (not branch name, not "address feedback").
- Generic roles from `flotilla.example.json` when naming fleet concepts.
- See the avoid/prefer table in `docs/pr-authoring.md`.

## Body workflow (mandatory)

### 1. Compose on disk

```bash
mkdir -p .claude/pr-bodies
```

Write the full markdown to `.claude/pr-bodies/pr-<branch-or-number>.md` using the
`Write` / `Edit` tools or your editor. **Never** hold the canonical body only in a
shell variable.

### 2. Updating an existing PR — fetch first

```bash
gh pr view <N> --json body --jq '.body' > .claude/pr-bodies/pr-<N>.md
wc -l .claude/pr-bodies/pr-<N>.md
```

- If ≥ ~20 lines: **additive** edits only (see `comprehensive-pr-description`).
- If placeholder (< 5 lines): compose from scratch per `docs/pr-authoring.md`.

### 3. Mermaid checklist

Before submit:

- [ ] Blank line **before** and **after** each ` ```mermaid ` fence
- [ ] Fence at column 0 (not nested under a list item)
- [ ] Node labels with `()`, `:`, or `[]` wrapped in double quotes
- [ ] Diagram earns its space (structure/flow prose cannot carry alone)

### 4. Submit verbatim

**Create:**

```bash
gh pr create --title="<plain-language title>" \
  --body-file=.claude/pr-bodies/pr-<branch>.md
```

**Edit:**

```bash
gh pr edit <N> --body-file=.claude/pr-bodies/pr-<N>.md
gh pr view <N> --json body --jq '.body' | head -5
```

If edit fails (classic project cards), REST fallback:

```bash
jq -Rs '{body: .}' < .claude/pr-bodies/pr-<N>.md > /tmp/pr-body.json
gh api -X PATCH repos/<owner>/<repo>/pulls/<N> --input /tmp/pr-body.json
```

### 5. Verify rendering

Open the PR in the browser; confirm Mermaid blocks render (not raw text).

## Do not

- `gh pr create -b "$(cat <<EOF …)"` or any interpolated multiline string
- Gate vocabulary (`CLEAR`, `cubic`, dispatch nonces) in the lead paragraph
- Commit-log or file-list structure for medium/large PRs
- Deployment-specific names in public bodies (`docs/private-public-boundary.md`)
- Screenshots/recordings captured from a LIVE deployment — pixels leak desk
  names and goal text past the text-based boundary guard; committed/embedded
  media must be rendered from the example fixture state only (and a pushed
  image stays publicly reachable via the frozen pull ref even after deletion)

## See also

- [`docs/pr-authoring.md`](../../../docs/pr-authoring.md) — canonical rules
- [`internal/dash/tracker/gh.go`](../../../internal/dash/tracker/gh.go) — `--body-file=-` precedent
- Principle 5 — [`docs/OPERATING-PRINCIPLES.md`](../../../docs/OPERATING-PRINCIPLES.md)