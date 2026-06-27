// Package deliver injects an instruction into an agent's tmux pane. For a
// turn-based agent, typing the text into its pane IS the wake — there is
// nothing to poll and no relay to run.
package deliver

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// agentMarker is the tmux per-pane user-option flotilla tags a pane with, so a
// desk is resolvable by a STABLE key immune to title drift. Claude Code (and
// other TUIs) retitle their pane to a task summary every turn, which breaks
// title-based resolution; a user-option set once (via `flotilla register`)
// survives every retitle. It is surface-agnostic (any TUI's pane can carry it),
// which also preps the drivable-surfaces lane.
const agentMarker = "@flotilla_agent"

// paneListFormat asks tmux for every pane as "<target>\t<title>\t<marker>".
// The marker field is empty for an untagged pane (tmux expands an unset
// user-option to the empty string).
const paneListFormat = "#{session_name}:#{window_index}.#{pane_index}\t#{pane_title}\t#{" + agentMarker + "}"

// commandTimeout bounds every tmux invocation so a wedged tmux server cannot
// hang flotilla indefinitely.
const commandTimeout = 10 * time.Second

// submitSettleDelay gives the receiving TUI time to ingest the bracketed paste
// before the submitting Enter. Without it the Enter races the pane's paste
// ingestion and is dropped, leaving the message UNSUBMITTED in the composer.
// Validated empirically: BOTH a single-line paste and a multi-line paste (which
// Claude Code collapses to "[Pasted text +N lines]") failed to submit with no
// delay and submitted reliably with it. There is no deterministic tmux-level
// signal for "the target app finished ingesting the paste" (tmux wait-for syncs
// tmux clients, not the target application), so a settle delay is necessary.
const submitSettleDelay = 250 * time.Millisecond

// clearComposeDelay gives the TUI time to register a typed slash command in its
// composer before the submitting Enter. Matches the gap used in the live
// verification on claude 2.1.161 (type "/clear", wait, Enter); harmless because
// ClearContext runs only on idle context-rotate, never on a latency-sensitive
// path.
const clearComposeDelay = 1 * time.Second

// bufferName returns a per-process tmux buffer name. tmux paste buffers live in
// the tmux SERVER, shared across processes, so a fixed name would let two
// concurrent `flotilla send` invocations overwrite each other's payload and
// cross-deliver (agent A receiving agent B's message). Scoping the name to the
// pid keeps concurrent sends independent.
func bufferName() string {
	return fmt.Sprintf("flotilla-send-%d", os.Getpid())
}

// slashKeysArgs returns the `tmux send-keys` argv that types a slash command into
// a TUI pane as LITERAL keystrokes (`-l`), so it reaches the composer as literal
// text rather than being parsed as tmux key names. This is the empirically-
// verified injection method (claude 2.1.161) — deliberately NOT the bracketed
// paste `Send` uses, which is for message bodies and is unverified for slash
// commands. It is surface-agnostic: the command (`/clear`, `/new`, `/new-chat`, …)
// is the caller's. Split out as a pure function so the argv is testable without tmux.
func slashKeysArgs(target, cmd string) []string {
	return []string{"send-keys", "-t", target, "-l", "--", cmd}
}

// InjectSlash injects a context-reset slash command into the target pane as
// literal keystrokes, then submits with Enter — resetting that session's context
// window while leaving process/session/pane and any Remote-Control binding intact.
// The command is surface-specific (the caller's): claude-code `/clear`, aider
// `/clear`, grok `/new`, cursor `/new-chat`, … It does NOT verify the result. Only
// call when the pane is idle: a slash injected mid-turn is undefined. (Surface
// drivers whose rotate strategy is RestartProcess must NEVER call this — the slash
// would land as literal composer text; see internal/surface.)
func InjectSlash(target, cmd string) error {
	// A slash writes the same composer as Send, so serialize it against other writers
	// to this pane (same per-pane lock; bounded acquire → drop rather than block).
	lock, err := acquirePaneLock(target)
	if err != nil {
		return err
	}
	defer lock.Release()

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, "tmux", slashKeysArgs(target, cmd)...).Run(); err != nil {
		return fmt.Errorf("tmux send-keys %s: %w", cmd, err)
	}
	select {
	case <-time.After(clearComposeDelay):
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", target, "--", "Enter").Run(); err != nil {
		return fmt.Errorf("tmux send-keys enter: %w", err)
	}
	return nil
}

// ClearContext injects Claude Code's `/clear` (verified on claude 2.1.161) — the
// reference claude-code reset. It is a thin wrapper over InjectSlash so the
// claude-code driver's behavior is byte-identical to before InjectSlash was
// generalized out of it.
func ClearContext(target string) error { return InjectSlash(target, "/clear") }

// ResolvePane returns the tmux target (session:window.pane) of the pane for the
// agent resolution key `want`. It resolves by a stable `@flotilla_agent` marker
// first (immune to title drift) and falls back to the pane title; it errors if
// no pane — or more than one — matches, so an ambiguous fleet never silently
// mis-delivers. `want` is the agent's resolution key (its name, or its roster
// `tmux_title` override), the same value `flotilla register` records as the
// marker.
func ResolvePane(want string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "list-panes", "-a", "-F", paneListFormat).Output()
	if err != nil {
		return "", fmt.Errorf("tmux list-panes: %w", err)
	}
	return parsePane(string(out), want)
}

// parseFields splits one tmux list-panes line "<target>\t<title>\t<marker>"
// into its three fields, ROBUST to a literal tab inside the title. A greedy
// SplitN would mis-assign a tab-containing title's tail to the marker field,
// silently un-resolving a registered desk (a TUI's pane title is external and
// can contain a tab; the marker field we control is roster-validated tab-free).
// We exploit the two invariants instead: the target (a tmux session:window.pane
// id) never contains a tab, and the marker never contains a tab — so the target
// is everything before the FIRST tab, the marker everything after the LAST tab,
// and the title is whatever lies between (tabs and all). Fewer than two tabs
// degrades gracefully: one tab → "<target>\t<title>" (no marker); none →
// target-only.
func parseFields(line string) (target, title, marker string) {
	first := strings.IndexByte(line, '\t')
	if first < 0 {
		return line, "", ""
	}
	target = line[:first]
	last := strings.LastIndexByte(line, '\t')
	if last == first {
		// Exactly one tab: a 2-field variant — the second field is the title and
		// there is no marker.
		return target, line[first+1:], ""
	}
	return target, line[first+1 : last], line[last+1:]
}

// classifyPanes is the shared lower scan of tmux list-panes output for an agent
// resolution key `want`. It returns every MARKER match and every TITLE match
// (each a tmux target), preserving the two-tier precedence the resolvers above
// apply. Lines are "<target>\t<title>\t<marker>"; the marker is empty for an
// untagged pane. Field extraction (parseFields) is robust to a literal TAB inside
// the title and to 1-/2-field format variants. Both parsePane (the delivery
// resolver, `(string, error)`) and Resolve (the resume resolver, 3-way
// outcome) call this so the marker-vs-title precedence is defined once.
func classifyPanes(output, want string) (markerMatches, titleMatches []string) {
	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		if line == "" {
			continue
		}
		target, title, marker := parseFields(line)
		// An empty marker never matches (an untagged pane is title-only); only a
		// non-empty marker equal to want is authoritative.
		if marker != "" && marker == want {
			markerMatches = append(markerMatches, target)
		}
		if titleMatchesName(title, want) {
			titleMatches = append(titleMatches, target)
		}
	}
	return markerMatches, titleMatches
}

// parsePane finds the unique target for an agent in tmux list-panes output, with
// a two-tier precedence so a title-drifting desk stays resolvable:
//
//  1. MARKER (authoritative): a pane whose `@flotilla_agent` user-option equals
//     `want`. A pane tagged once via `flotilla register` resolves here forever,
//     regardless of how its title drifts. If two panes carry the same marker the
//     fleet is mis-tagged → ambiguity error (never silently pick one).
//  2. TITLE (fallback, only when no pane carries `@flotilla_agent == want`):
//     the prior exact / single-glyph-prefix title match, so an UNtagged fleet —
//     or an untagged agent within a partially-tagged fleet — keeps working
//     exactly as before. (A marker on some OTHER agent's pane does NOT suppress
//     this agent's title match; only a marker equal to THIS `want` does.)
//
// Split out so the precedence is testable without a running tmux server.
func parsePane(output, want string) (string, error) {
	markerMatches, titleMatches := classifyPanes(output, want)

	// Tier 1: the stable marker wins outright when present.
	switch len(markerMatches) {
	case 1:
		return markerMatches[0], nil
	default:
		if len(markerMatches) > 1 {
			return "", fmt.Errorf("ambiguous: %d tmux panes tagged @flotilla_agent=%q (%s) — re-tag the right one with: flotilla register %s --pane <target>",
				len(markerMatches), want, strings.Join(markerMatches, ", "), want)
		}
	}

	// Tier 2: title fallback for an untagged fleet.
	switch len(titleMatches) {
	case 0:
		return "", fmt.Errorf("no tmux pane for agent %q (no @flotilla_agent marker and no matching title) — tag it with: flotilla register %s", want, want)
	case 1:
		return titleMatches[0], nil
	default:
		return "", fmt.Errorf("ambiguous: %d tmux panes titled %q (%s) — tag the right one with: flotilla register %s --pane <target>",
			len(titleMatches), want, strings.Join(titleMatches, ", "), want)
	}
}

// TagPane records the stable @flotilla_agent marker on a pane (`flotilla
// register`), so the pane resolves by key regardless of later title drift. key
// is the agent's resolution key (name, or tmux_title override) — the same value
// ResolvePane matches against.
func TagPane(target, key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	// `--` stops flag parsing so a key beginning with '-' (a dash-leading agent
	// name) is taken as the option VALUE, not mistaken for a tmux flag.
	if err := exec.CommandContext(ctx, "tmux", "set-option", "-p", "-t", target, "--", agentMarker, key).Run(); err != nil {
		return fmt.Errorf("tmux set-option %s for pane %q: %w", agentMarker, target, err)
	}
	// Read the marker back and confirm it landed on the intended pane with the
	// intended value. A typo'd `--pane` target, or a tmux quirk that drops the
	// option, would otherwise report success while leaving the desk silently
	// unresolvable — the exact failure this command exists to prevent.
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{"+agentMarker+"}").Output()
	if err != nil {
		return fmt.Errorf("tmux verify %s for pane %q: %w", agentMarker, target, err)
	}
	if got := strings.TrimRight(string(out), "\n"); got != key {
		return fmt.Errorf("tmux %s read-back mismatch for pane %q: set %q but read %q (wrong --pane target?)", agentMarker, target, key, got)
	}
	return nil
}

// titleMatchesName reports whether a tmux pane title belongs to the agent named
// want. Claude Code renames its pane to "<status-glyph> <name>" (e.g.
// "✳ desk-a"), so exact equality fails on a live session. We accept the bare
// name, or a SINGLE-rune glyph prefix followed by the name. Constraining the
// prefix to one rune rejects both "desk" (substring) and "build desk-a"
// (an unrelated multi-word pane) as matches for "desk-a". This is the FALLBACK
// tier — once a pane is `flotilla register`-tagged, the marker resolves it and
// the title no longer matters.
func titleMatchesName(title, want string) bool {
	// An empty want must never match (e.g. an empty title against an empty key);
	// the marker tier already requires a non-empty value, and resolution keys are
	// roster-validated non-empty, so this is a defensive self-guard.
	if want == "" {
		return false
	}
	title = strings.TrimSpace(title)
	if title == want {
		return true
	}
	glyph, rest, found := strings.Cut(title, " ")
	return found && rest == want && utf8.RuneCountInString(glyph) == 1
}

// Send delivers text to the target pane as a SINGLE submission. The message is
// loaded into a tmux buffer and bracketed-pasted (-p), so embedded newlines are
// inserted literally instead of each acting as Enter; a single trailing Enter
// then submits the whole message. Routing the text through a buffer (stdin) also
// keeps it out of argv entirely, so a message beginning with '-' is never
// mistaken for a tmux flag.
//
// Caveat: bracketed paste inserts literal newlines ONLY while the receiving
// application has bracketed-paste mode enabled (Claude Code's TUI does — this is
// validated end-to-end). For a target that lacks it, tmux converts each LF to a
// CR and every newline would submit, so a non-Claude or modal target needs
// revalidation before relying on multi-line delivery.
func Send(target, text string) error {
	// Serialize the non-atomic paste-sequence against every other writer to this pane
	// (send / the watch Injector / voice). A bounded acquire drops the delivery rather
	// than blocking the heartbeat clock, which holds this same lock.
	lock, err := acquirePaneLock(target)
	if err != nil {
		return err
	}
	defer lock.Release()

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	buf := bufferName()
	load := exec.CommandContext(ctx, "tmux", "load-buffer", "-b", buf, "-")
	load.Stdin = strings.NewReader(text)
	if err := load.Run(); err != nil {
		return fmt.Errorf("tmux load-buffer: %w", err)
	}
	// -p: bracketed paste (literal newlines); -d: delete the buffer afterwards.
	if err := exec.CommandContext(ctx, "tmux", "paste-buffer", "-d", "-p", "-b", buf, "-t", target).Run(); err != nil {
		return fmt.Errorf("tmux paste-buffer: %w", err)
	}
	// Give the TUI time to ingest the bracketed paste before submitting. This is
	// required for ALL pastes, not just multi-line: empirically a single-line
	// paste followed by an immediate Enter is also dropped (a race between the
	// pane's paste ingestion and the submitting keystroke). See submitSettleDelay.
	select {
	case <-time.After(submitSettleDelay):
	case <-ctx.Done():
		return ctx.Err()
	}
	// Submit. "Enter" is a key name (no -l); -- guards a dash-leading target.
	if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", target, "--", "Enter").Run(); err != nil {
		return fmt.Errorf("tmux send-keys enter: %w", err)
	}
	return nil
}

// sendCtrlJArgs builds the ordered `tmux send-keys` argv sequence that types `text`
// into a pane using Ctrl+J (`C-j`) as the in-composer NEWLINE (not bracketed paste),
// then submits with a final `Enter`. Per line: `send-keys -l -- <line>` (literal text,
// so a line beginning with '-' or containing key-like tokens is typed verbatim);
// between lines: `send-keys -- C-j` (the newline keystroke). Split out as a pure
// function so the sequence is testable without a tmux server. The LAST element is
// always the submitting `Enter`.
func sendCtrlJArgs(target, text string) [][]string {
	lines := strings.Split(text, "\n")
	seq := make([][]string, 0, 2*len(lines)+1)
	for i, line := range lines {
		if i > 0 {
			seq = append(seq, []string{"send-keys", "-t", target, "--", "C-j"})
		}
		seq = append(seq, []string{"send-keys", "-t", target, "-l", "--", line})
	}
	seq = append(seq, []string{"send-keys", "-t", target, "--", "Enter"})
	return seq
}

// SendCtrlJ delivers text to the target pane as a SINGLE submission using Ctrl+J for
// in-composer newlines instead of a bracketed paste, then a final Enter. This is the
// per-driver alternate to Send for a harness that does NOT enable bracketed-paste mode
// (where Send's literal newlines would instead submit each line early), or whose tmux
// newline is Ctrl+J. Like Send it serializes against other writers (the per-pane lock)
// and is bounded by commandTimeout, with a submitSettleDelay before the submitting
// Enter. A driver selects this by wiring its `send` field to SendCtrlJ.
func SendCtrlJ(target, text string) error {
	lock, err := acquirePaneLock(target)
	if err != nil {
		return err
	}
	defer lock.Release()

	// Each tmux invocation gets its OWN commandTimeout. The 2N+1-command sequence
	// (vs Send's fixed 3) would deplete a single shared budget for a very long body;
	// commandTimeout exists to bound EACH op against a wedged tmux server, not to cap
	// the whole sequence, so a per-command timeout is the correct semantic here. (The
	// total wall-time scales with the message size, which flotilla controls — turn/wake
	// prompts are small; a concurrent writer's lock-acquire is bounded and drops rather
	// than blocking the clock.)
	run := func(args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		return exec.CommandContext(ctx, "tmux", args...).Run()
	}

	seq := sendCtrlJArgs(target, text)
	// Type the lines + interleaved C-j newlines (everything except the final Enter).
	for _, args := range seq[:len(seq)-1] {
		if err := run(args); err != nil {
			return fmt.Errorf("tmux send-keys (ctrl-j newline): %w", err)
		}
	}
	// Settle before submitting, matching Send's submitSettleDelay rationale (the
	// composer must register the typed text before the Enter, or the Enter races it).
	time.Sleep(submitSettleDelay)
	if err := run(seq[len(seq)-1]); err != nil {
		return fmt.Errorf("tmux send-keys enter: %w", err)
	}
	return nil
}

// sendEnterArgs builds the `tmux send-keys` argv that submits a SINGLE Enter to a pane.
// "Enter" is a key NAME (no -l), so it is the submitting keystroke (not literal text);
// `--` guards a dash-leading target from being parsed as a tmux flag. Split out as a pure
// function so the argv is testable without a tmux server, mirroring sendCtrlJArgs /
// slashKeysArgs. It is identical to the trailing Enter step of Send (tmux.go) and
// InjectSlash — factored here as the reusable confirmed-delivery retry keystroke.
func sendEnterArgs(target string) []string {
	return []string{"send-keys", "-t", target, "--", "Enter"}
}

func sendCtrlCArgs(target string) []string {
	return []string{"send-keys", "-t", target, "--", "C-c"}
}

// SendCtrlC sends a single Ctrl-C (SIGINT keystroke) to the target pane under the per-pane lock. It
// is the SELF-HEAL primitive (internal/surface self-heal): a Ctrl-C escapes Claude Code's inline
// agents-panel / per-agent-message overlay back to the main composer. It is DESTRUCTIVE — Claude
// Code's documented contract is "the first press clears the prompt input and a second press exits
// Claude Code", so a Ctrl-C into an already-recovered (empty main) composer would EXIT the session.
// The CALLER (surface.selfHeal) must therefore gate every press on (a) the pane being Idle (a Ctrl-C
// into a running turn interrupts it) and (b) the composer still being an overlay (never a recovered
// composer) — this primitive only sends the key. The marked log line is the audit signal the
// exit-after-heal detector correlates with a subsequent Shell drop. Bounded by commandTimeout; takes
// the per-pane cross-process lock like SendEnter.
func SendCtrlC(target string) error {
	lock, err := acquirePaneLock(target)
	if err != nil {
		return err
	}
	defer lock.Release()

	log.Printf("flotilla: deliver: self-heal Ctrl-C → %s", target)
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, "tmux", sendCtrlCArgs(target)...).Run(); err != nil {
		return fmt.Errorf("tmux send-keys C-c: %w", err)
	}
	return nil
}

// SendEnter submits a single Enter to the target pane under the per-pane lock. It is the
// IDEMPOTENT retry for confirmed delivery (internal/surface.ConfirmSubmit): when a prior
// Send pasted the body but its submitting Enter was dropped in the paste-ingestion race
// (submitSettleDelay mitigates but does not eliminate this on a loaded host), the body sits
// unsubmitted in the composer and a bare Enter submits it. Crucially it NEVER re-pastes, so
// it cannot double-submit the message body — re-running Send would paste a second copy.
// (Whether a bare Enter on an already-consumed idle composer submits nothing is a separate
// TUI property validated out-of-band; the no-double-submit guarantee here is structural —
// this primitive only ever sends a key, never text.) Like Send it takes the per-pane
// cross-process lock (bounded acquire → drop, never blocking the heartbeat clock) and is
// bounded by commandTimeout.
func SendEnter(target string) error {
	lock, err := acquirePaneLock(target)
	if err != nil {
		return err
	}
	defer lock.Release()

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, "tmux", sendEnterArgs(target)...).Run(); err != nil {
		return fmt.Errorf("tmux send-keys enter: %w", err)
	}
	return nil
}
