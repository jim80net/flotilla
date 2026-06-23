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
	// a crypto/rand nonce, so the path is unique and absent-at-HEAD by construction.
	HandoffPath(cwd, token string) string
	// HandoffTurn returns the NON-INTERACTIVE, self-committing handoff instruction TEXT to
	// deliver: write a handoff (per the handoff FORMAT, not the interactive skill) to
	// designatedPath, force-commit it to the current branch (git add -f, so a gitignored
	// handoffs dir does not block it), NOT ask for confirmation (remote-driven), then stop.
	HandoffTurn(designatedPath string) string
	// TakeoverTurn returns the IMPERATIVE takeover instruction TEXT for the freshly-
	// relaunched session: read designatedPath and take over, BEGIN WORK IMMEDIATELY (NOT
	// ask whether to start), and — being remote-driven — surface any clarification via a
	// flotilla message, never an in-pane interactive prompt (a remote XO cannot answer an
	// in-pane menu over the relay). The PATH is harness-agnostic markdown; only the wording
	// is per-harness.
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
