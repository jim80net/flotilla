## MODIFIED Requirements

### Requirement: A surface driver MAY expose the context-preservation policy a recycle drives

The system SHALL define an OPTIONAL `RecycleBridge` capability that a surface driver MAY implement,
exposing the per-harness context-preservation policy a recycle drives as three pieces: `HandoffPath(cwd, token)` — the recycle-DESIGNATED handoff
artifact path for this harness (the driver owns the convention, e.g. claude
`<cwd>/.claude/handoffs/<date>-recycle-<token>.md`, grok `<cwd>/.flotilla/handoffs/recycle-<token>.md`);
`HandoffTurn(designatedPath)` — the
NON-INTERACTIVE handoff instruction TEXT; and `TakeoverTurn(designatedPath)` — the
IMPERATIVE takeover instruction TEXT. The two turn methods SHALL return TEXT (the command delivers it
via confirmed delivery); they SHALL NOT themselves inject. The handoff turn SHALL instruct the desk to
write a handoff (per the handoff FORMAT, not the interactive handoff skill) to the designated path as an
untracked gitignored file, explicitly forbidding `git add` / `git commit` (durability is filesystem-
based, #218), NOT ask for confirmation (it is remote-driven), then stop. The takeover turn SHALL
instruct a freshly-relaunched session to read the designated path and take over, DELETE the handoff file
from disk (`rm -f`) as its first action after reading, BEGIN WORK IMMEDIATELY (NOT ask whether to
start), and state that the session is remote-driven and must surface any clarification via a flotilla
message, never an in-pane interactive prompt. Neither turn SHALL invoke the human-interactive
handoff/takeover skills (which pause for a confirmation / a "shall I start?"). The handoff PATH SHALL be
harness-agnostic (a markdown file); only the per-harness convention and wording differ. A caller SHALL
type-assert the capability and, when it is ABSENT, REFUSE to recycle the desk cleanly (naming the
surface) rather than silently degrading to a context-losing restart. **Claude Code and grok both
implement the bridge** (grok added 2026-06-23, #158); a further harness's bridge remains a separate,
gated change.

#### Scenario: A recycle-capable surface supplies the designated path and the non-interactive turns

- **WHEN** a recycle drives a surface that implements `RecycleBridge`
- **THEN** `HandoffPath` yields the designated artifact path, `HandoffTurn` yields a non-interactive
  instruction naming that exact path (write untracked, do not commit to git, do not ask to confirm,
  stop), and `TakeoverTurn` yields an imperative begin-work-immediately instruction naming that exact
  path, instructing read → delete from disk → work, and telling the remote-driven session to parlay via a
  flotilla message