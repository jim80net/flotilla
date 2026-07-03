// Package backlog parses the fleet backlog — the data structure the goal-driven loop drains so
// the XO cannot settle (go idle) while authorized work remains. It is the generalizable flotilla
// capability behind the change-detector's backlog gate (internal/watch); the backlog file's
// CONTENTS are deployment-circumstantial.
//
// THE ITEM-LINE CONTRACT. A backlog item is a markdown list line in the "## Backlog" section
// carrying a leading bracketed STATUS MARKER:
//
//   - [in-flight] <text>      dispatched / being driven  → UNBLOCKED (actionable)
//   - [next] <text>           not started yet            → UNBLOCKED (actionable)
//   - [blocked] <text>        waiting on the operator     → operator-blocked (the OPEN-QUESTIONS ledger; drive PREP, don't settle on it)
//   - [needs-attention] <text> deprioritized stuck item   → operator-blocked (open-questions ledger)
//   - [awaiting-auth] <text>  pending an operator go/no-go → awaiting-authorization (the AUTHORIZATIONS ledger; settle-neutral, distinct from blocked)
//   - [done] <text>           complete                    → excluded (drained)
//
// The OPEN-QUESTIONS ledger ([blocked]/[needs-attention]) and the AUTHORIZATIONS ledger
// ([awaiting-auth]) are the two SETTLE-NEUTRAL classes: neither is actionable, so neither enters
// Unblocked, but they are counted separately so "blocked on a question" is not conflated with
// "awaiting an authorization" (the per-recipient heartbeat judgment and the dash both read the
// two counts). The authorizations marker is the EXACT token `awaiting-auth` (case-insensitive on
// the word, fixed spelling): a near-miss like `[awaiting-authorization]` is UNRECOGNIZED and falls
// through to the fail-safe (Malformed + actionable) — so it fails LOUD, never silently settling.
//
// The marker word is matched case-insensitively. A `[x]` checkbox is accepted as done; a leading
// `~~strike~~` or a `✅` is also read as done (lenient). Numbered (`1.`) and bulleted (`-`/`*`/`+`)
// list lines both qualify — the MARKER, not the glyph, carries the status.
//
// FAIL-SAFE (the operator's contract clause): Parse is a TOTAL function — it never panics and
// never errors, so it cannot crash the wake loop. An item line with NO recognized status marker
// (or an unrecognized marker) is FLAGGED (counted in Malformed) AND treated as UNBLOCKED — erring
// toward keep-driving + surfacing, NEVER silently dropped or misclassified. The caller raises a
// loud alert when Malformed > 0 (or when a present file has no "## Backlog" section), so a format
// slip is loud rather than a silent no-op.
package backlog

import (
	"regexp"
	"strings"
)

// Status is the backlog's settle-relevant classification.
type Status struct {
	Unblocked    []string // ordered unblocked item raw lines (file priority) — the drive queue (the gate's trigger)
	Blocked      int      // operator-blocked items ([blocked]/[needs-attention]) — the OPEN-QUESTIONS ledger
	AwaitingAuth int      // awaiting-authorization items ([awaiting-auth]) — the AUTHORIZATIONS ledger (settle-neutral, distinct from Blocked)
	Done         int      // completed items — informational / test-observable
	Malformed    int      // item lines lacking a recognized [status] marker (flagged; ALSO counted in Unblocked)
	Items        int      // total item lines seen in the section — informational / test-observable
	Found        bool     // a "## Backlog" section heading was located (distinguishes absent from present-but-empty)
}

// itemLine matches a markdown list item (numbered or bulleted) and captures the text after the
// marker. An indented continuation line (no list glyph) does NOT match, so it is not a new item.
var itemLine = regexp.MustCompile(`^\s*(?:\d+\.|[-*+])\s+(\S.*)$`)

// Parse classifies the "## Backlog" section of a markdown backlog. Total + fail-safe (see package
// doc): never panics; a markerless item errs toward Unblocked + Malformed.
func Parse(md string) Status {
	var st Status
	inSection := false
	for _, raw := range strings.Split(md, "\n") {
		// A "## " heading toggles the section: enter on "## Backlog…", exit on any other "## ".
		if strings.HasPrefix(raw, "## ") {
			if strings.HasPrefix(raw, "## Backlog") {
				inSection = true
				st.Found = true
			} else {
				inSection = false
			}
			continue
		}
		if !inSection {
			continue
		}
		m := itemLine.FindStringSubmatch(raw)
		if m == nil {
			continue // blank line, continuation line, or prose — not an item
		}
		st.Items++
		switch classify(m[1]) {
		case clsDone:
			st.Done++
		case clsBlocked:
			st.Blocked++
		case clsAwaitingAuth:
			st.AwaitingAuth++
		case clsUnblocked:
			st.Unblocked = append(st.Unblocked, strings.TrimSpace(raw))
		default: // clsMalformed — err toward driving AND flag
			st.Malformed++
			st.Unblocked = append(st.Unblocked, strings.TrimSpace(raw))
		}
	}
	return st
}

type cls int

const (
	clsMalformed cls = iota
	clsUnblocked
	clsBlocked
	clsAwaitingAuth
	clsDone
)

// classify maps an item's post-marker text to a class via its LEADING bracketed status marker.
// It defers the marker vocabulary to markerOf (the single source of truth shared with
// ClassifyLine / MatchInBacklog) and maps the normalized token onto the settle-relevant class.
func classify(rest string) cls {
	switch markerOf(rest) {
	case "done":
		return clsDone
	case "blocked", "needs-attention":
		return clsBlocked // the OPEN-QUESTIONS ledger
	case "awaiting-auth":
		return clsAwaitingAuth // the AUTHORIZATIONS ledger — exact token only (a near-miss falls through to Malformed)
	case "in-flight", "next":
		return clsUnblocked
	default: // "malformed"
		return clsMalformed // an unrecognized marker — flag it, don't guess
	}
}

// markerOf extracts an item's normalized leading status marker from the text AFTER the list
// glyph. Only the leading `[marker]` is consulted (so a `[link]` later in the text never
// misclassifies, and the lowercase prose word "done" inside the text never counts as the done
// marker). It returns one of "in-flight", "next", "blocked", "needs-attention", "awaiting-auth",
// "done", or "malformed" (an unrecognized/missing marker; a leading `~~strike~~` or `✅` reads as
// done). It is the ONE place the marker vocabulary lives; classify, ClassifyLine, and
// MatchInBacklog all consult it so the settle semantics can never drift between the whole-file
// Parse and the per-line resolvers the goals view uses.
func markerOf(rest string) string {
	if strings.HasPrefix(rest, "[") {
		if end := strings.IndexByte(rest, ']'); end > 1 {
			switch tok := strings.ToLower(strings.TrimSpace(rest[1:end])); tok {
			case "x":
				return "done"
			case "pending":
				return "in-flight" // ratified goals spec lists [pending] as an in-flight synonym
			case "done", "blocked", "needs-attention", "awaiting-auth", "in-flight", "next":
				return tok
			default:
				return "malformed"
			}
		}
	}
	// No leading bracket marker. Lenient done detection (a struck or ✅-marked line); else malformed.
	if strings.HasPrefix(rest, "~~") || strings.Contains(rest, "✅") {
		return "done"
	}
	return "malformed"
}

// ClassifyLine classifies a SINGLE markdown list line by its leading status marker, returning the
// normalized marker token markerOf yields ("in-flight", "next", "blocked", "needs-attention",
// "awaiting-auth", "done", or "malformed"), or "" when the line is not a list item at all. It is
// the per-line sibling of Parse — the SAME itemLine grammar and the SAME marker vocabulary — used
// by the goals view to resolve one attached backlog item's status without re-parsing the file.
func ClassifyLine(raw string) string {
	m := itemLine.FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	return markerOf(m[1])
}

// MatchInBacklog returns the normalized marker of the FIRST "## Backlog" item line whose text
// contains substr (case-insensitive), and whether any item matched. It applies the SAME section
// grammar as Parse (the "## Backlog" heading toggles the section; any other "## " exits it) so a
// goal's attached backlog item resolves against exactly the lines Parse would classify. A blank or
// whitespace-only substr never matches (returns false) — an empty match string must not silently
// bind to the first backlog line.
func MatchInBacklog(md, substr string) (string, bool) {
	needle := strings.ToLower(strings.TrimSpace(substr))
	if needle == "" {
		return "", false
	}
	inSection := false
	for _, raw := range strings.Split(md, "\n") {
		if strings.HasPrefix(raw, "## ") {
			inSection = strings.HasPrefix(raw, "## Backlog")
			continue
		}
		if !inSection {
			continue
		}
		m := itemLine.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		if strings.Contains(strings.ToLower(raw), needle) {
			return markerOf(m[1]), true
		}
	}
	return "", false
}
