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
//   - [blocked] <text>        waiting on the operator     → operator-blocked (drive PREP, don't settle on it)
//   - [needs-attention] <text> deprioritized stuck item   → operator-blocked
//   - [done] <text>           complete                    → excluded (drained)
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
	Unblocked []string // ordered unblocked item raw lines (file priority) — the drive queue (the gate's trigger)
	Blocked   int      // operator-blocked items — informational / test-observable (not read by the gate today)
	Done      int      // completed items — informational / test-observable
	Malformed int      // item lines lacking a recognized [status] marker (flagged; ALSO counted in Unblocked)
	Items     int      // total item lines seen in the section — informational / test-observable
	Found     bool     // a "## Backlog" section heading was located (distinguishes absent from present-but-empty)
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
	clsDone
)

// classify maps an item's post-marker text to a class via its LEADING bracketed status marker.
// Only the leading `[marker]` is consulted (so a `[link]` later in the text never misclassifies,
// and the lowercase prose word "done" inside the text never counts as the done marker).
func classify(rest string) cls {
	if strings.HasPrefix(rest, "[") {
		if end := strings.IndexByte(rest, ']'); end > 1 {
			switch strings.ToLower(strings.TrimSpace(rest[1:end])) {
			case "done", "x":
				return clsDone
			case "blocked", "needs-attention":
				return clsBlocked
			case "in-flight", "next":
				return clsUnblocked
			default:
				return clsMalformed // an unrecognized marker — flag it, don't guess
			}
		}
	}
	// No leading bracket marker. Lenient done detection (a struck or ✅-marked line); else malformed.
	if strings.HasPrefix(rest, "~~") || strings.Contains(rest, "✅") {
		return clsDone
	}
	return clsMalformed
}
