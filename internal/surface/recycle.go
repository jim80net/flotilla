package surface

// RecycleBridge is an OPTIONAL Driver capability: the per-harness context-preservation
// policy a `flotilla recycle` drives. A surface that implements it can be context-
// preservingly recycled; a surface WITHOUT it makes recycle REFUSE cleanly (never a
// silent context-losing restart). Claude Code is the reference.
//
// The driver owns the per-harness CONVENTIONS (where handoffs live; the exact turn
// wording); the COMMAND owns the lifecycle + delivery. The two "turn" methods return
// TEXT (pure, unit-testable without tmux); the command delivers them via CONFIRMED
// delivery so it knows the turn actually started. CRUCIALLY the turns are
// RECYCLE-SPECIFIC and NON-INTERACTIVE — they do NOT invoke the human-interactive
// /handoff or /takeover skills (which pause for "Is anything missing?" / "Shall I
// start?"); they instruct the desk to produce the same artifacts non-interactively.
// (Close — the graceful exit — is on the core Driver interface, NOT here: a surface may
// have a clean close without a recycle bridge, and vice versa; recycle requires both.)
type RecycleBridge interface {
	// HandoffPath returns the recycle-DESIGNATED handoff artifact path for this harness,
	// given the desk's cwd and a recycle-unique token. The driver owns the convention
	// (claude: <cwd>/.claude/handoffs/recycle-<token>.md). Naming the path up front makes
	// detection EXACT (no mtime, no baseline set-difference) and hands the takeover phase
	// the precise path. The token (command-supplied) leads with a timestamp and ends with
	// a crypto/rand nonce, so the path is unique and absent-on-disk by construction.
	HandoffPath(cwd, token string) string
	// HandoffTurn returns the NON-INTERACTIVE handoff instruction TEXT to deliver: write a
	// handoff (per the handoff FORMAT, not the interactive skill) to designatedPath as an
	// untracked gitignored file (never git add/commit — #218), NOT ask for confirmation
	// (remote-driven), then stop.
	HandoffTurn(designatedPath string) string
	// TakeoverTurn returns the IMPERATIVE takeover instruction TEXT for the freshly-
	// relaunched session: read designatedPath and take over, then — as its FIRST action —
	// delete the handoff file from disk (deployment-specific; must never enter version
	// control), then BEGIN WORK IMMEDIATELY (NOT ask whether to start), and — being remote-
	// driven — surface any clarification via a flotilla message, never an in-pane interactive
	// prompt (a remote XO cannot answer an in-pane menu over the relay). The PATH is harness-
	// agnostic markdown; only the wording is per-harness. read → delete → work.
	TakeoverTurn(designatedPath string) string
}

// RecycleSupport type-asserts the OPTIONAL RecycleBridge capability so the recycle command
// can REFUSE cleanly (naming the surface) when it is absent, rather than silently degrading
// to a context-losing restart. Mirrors the ResultReader / ComposerStateProbe type-assert
// pattern. (The command separately requires ComposerStateProbe — the Idle ∧ ComposerCleared
// gates need it — so a recycle-capable surface implements BOTH.)
func RecycleSupport(d Driver) (RecycleBridge, bool) {
	rb, ok := d.(RecycleBridge)
	return rb, ok
}

// PortableMarkdownHandoffTurn is the shared non-interactive handoff instruction for
// harness-agnostic recycle bridges (grok, codex — #158). The driver owns the path
// convention; the turn wording is identical across those surfaces.
func PortableMarkdownHandoffTurn(designatedPath string) string {
	return "You are being RECYCLED by flotilla (an automated, REMOTE-DRIVEN chapter close — " +
		"no human is at this pane to answer prompts). Do exactly this, then stop:\n" +
		"1. Write a complete handoff (objective, completed work, current state, remaining work, " +
		"gotchas — enough for a fresh session to resume cold) to this EXACT path: " + designatedPath + "\n" +
		"2. Do NOT commit the handoff to git — it MUST remain an untracked file on disk (the path is " +
		"gitignored; flotilla detects durability from the file itself, not version control). Do NOT run " +
		"`git add` or `git commit` on it.\n" +
		"3. Do NOT ask me to confirm or review, do NOT ask \"is anything missing\" — just write and stop. " +
		"flotilla will close and relaunch this desk once the file lands on disk."
}

// PortableMarkdownTakeoverTurn is the shared imperative takeover instruction for
// harness-agnostic recycle bridges (grok, codex — #158/#218).
func PortableMarkdownTakeoverTurn(designatedPath string) string {
	return "You are a freshly-recycled flotilla desk with a clean context window, and you are " +
		"REMOTE-DRIVEN (a remote XO drives you over the relay; no human is at this pane). " +
		"Do this in order:\n" +
		"1. Read this handoff in full and take over per it: " + designatedPath + "\n" +
		"2. Then, as your first action after reading, DELETE the handoff file from disk so " +
		"deployment-specific content cannot linger in the worktree (it is gitignored and must never " +
		"enter version control; you have read it now): `rm -f \"" + designatedPath + "\"` (the -f avoids " +
		"a spurious failure if the file is already gone; the quotes guard a path with spaces).\n" +
		"3. Then BEGIN WORK IMMEDIATELY on the handoff's remaining work — do NOT ask \"shall I start?\" or " +
		"wait for confirmation. If you genuinely need a clarification, surface it via a flotilla MESSAGE " +
		"(e.g. `flotilla notify --from <your-name> \"...\"`), NEVER an in-pane interactive prompt — a " +
		"remote XO cannot answer an in-pane menu over the relay (keystrokes navigate it, they don't select)."
}
