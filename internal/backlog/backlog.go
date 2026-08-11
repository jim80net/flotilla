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

// ScanResult is the authoritative structural view of a backlog document. Items
// are ordered by file position. The scanner alone owns section recognition,
// Markdown item boundaries, and normalized marker classification.
type ScanResult struct {
	Found bool
	Items []Item
}

// Item is one Markdown list item in the Backlog section. Head preserves the
// trimmed list line used by Parse and MatchInBacklog. Raw additionally includes
// indented continuation lines for hygiene metrics. Nested bullets are Items of
// their own, matching Parse's historical fail-safe behavior.
type Item struct {
	StartLine      int
	Head           string
	Raw            string
	Classification string
}

// itemLine matches a markdown list item (numbered or bulleted) and captures the text after the
// marker. An indented continuation line (no list glyph) does NOT match, so it is not a new item.
var itemLine = regexp.MustCompile(`^\s*(?:\d+\.|[-*+])\s+(\S.*)$`)

// Scan returns the single authoritative structural interpretation of md.
func Scan(md string) ScanResult {
	result := ScanResult{Items: []Item{}}
	lines := strings.Split(md, "\n")
	inSection := false
	var current *Item
	flush := func() {
		if current == nil {
			return
		}
		result.Items = append(result.Items, *current)
		current = nil
	}
	for index, raw := range lines {
		if strings.HasPrefix(raw, "## ") {
			flush()
			inSection = strings.HasPrefix(raw, "## Backlog")
			result.Found = result.Found || inSection
			continue
		}
		if !inSection {
			continue
		}
		if item, ok := scanItemLine(raw, index+1); ok {
			flush()
			current = &item
			continue
		}
		if current != nil && strings.TrimSpace(raw) != "" &&
			(strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t")) {
			current.Raw += "\n" + raw
		}
	}
	flush()
	return result
}

func scanItemLine(raw string, line int) (Item, bool) {
	match := itemLine.FindStringSubmatch(raw)
	if match == nil {
		return Item{}, false
	}
	return Item{
		StartLine: line, Head: strings.TrimSpace(raw), Raw: raw,
		Classification: markerOf(match[1]),
	}, true
}

// Parse classifies the "## Backlog" section of a markdown backlog. Total + fail-safe (see package
// doc): never panics; a markerless item errs toward Unblocked + Malformed.
func Parse(md string) Status {
	scan := Scan(md)
	st := Status{Found: scan.Found}
	for _, item := range scan.Items {
		st.Items++
		switch classOf(item.Classification) {
		case clsDone:
			st.Done++
		case clsBlocked:
			st.Blocked++
		case clsAwaitingAuth:
			st.AwaitingAuth++
		case clsUnblocked:
			st.Unblocked = append(st.Unblocked, item.Head)
		default: // clsMalformed — err toward driving AND flag
			st.Malformed++
			st.Unblocked = append(st.Unblocked, item.Head)
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

// classOf maps the scanner's normalized marker onto a settle-relevant class.
func classOf(marker string) cls {
	switch marker {
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
// done). It is the ONE place the marker vocabulary lives; Scan, ClassifyLine,
// and MatchInBacklog all consult it so settle semantics can never drift between the whole-file
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
	item, ok := scanItemLine(raw, 1)
	if !ok {
		return ""
	}
	return item.Classification
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
	for _, item := range Scan(md).Items {
		if strings.Contains(strings.ToLower(item.Head), needle) {
			return item.Classification, true
		}
	}
	return "", false
}
