package readermap

import (
	"encoding/json"
	"strings"
)

// FenceTag is the info-string that labels the fenced code block carrying the
// reader-map envelope JSON inside an otherwise free-text turn-final. A desk emits
// its envelope as a ```reader-map fenced block; the publish path locates and parses
// it. The tag (not a position convention) is what lets the mirror tell a brief
// turn-final from ordinary prose without the desk calling any publish primitive. It
// is exported so the brief-request prompt (which tells the desk this exact tag) and
// the detector share ONE source and can never drift.
const FenceTag = "reader-map"

// DetectOutcome is the three-way classification of a turn-final with respect to a
// reader-map envelope. The detect predicate keys on block PRESENCE; the validity
// (does the JSON parse) is the second axis — so a desk that simply did not emit a
// brief (Absent) is NEVER conflated with one that emitted a broken brief (Malformed).
type DetectOutcome int

const (
	// OutcomeAbsent: no reader-map block — an ordinary (un-enveloped) turn-final,
	// handled by the back-compat warn-and-publish branch.
	OutcomeAbsent DetectOutcome = iota
	// OutcomePresent: exactly one reader-map block that parses as JSON into an
	// Envelope (its FIELD presence is then judged by Tier1Lint, separately).
	OutcomePresent
	// OutcomeMalformed: a reader-map block is present but does not parse as JSON, OR
	// more than one reader-map block is present. A trivially-fixable structural
	// defect — handled fail-closed per the malformed-envelope posture.
	OutcomeMalformed
)

func (o DetectOutcome) String() string {
	switch o {
	case OutcomeAbsent:
		return "absent"
	case OutcomePresent:
		return "present"
	case OutcomeMalformed:
		return "malformed"
	default:
		return "unknown"
	}
}

// Detect locates the reader-map envelope inside a free-text turn-final and
// classifies the outcome. It returns the parsed Envelope ONLY on OutcomePresent
// (a single, JSON-parseable ```reader-map block); on OutcomeAbsent (no block) and
// OutcomeMalformed (unparseable JSON, or a second block) it returns nil. "Parseable"
// here means the block body json.Unmarshals into an Envelope without a syntax error
// — empty/missing FIELDS still parse to OutcomePresent and are caught later by
// Tier1Lint, keeping the detect (presence) and lint (field presence) axes distinct.
func Detect(turnFinal string) (*Envelope, DetectOutcome) {
	bodies := extractFencedBlocks(turnFinal, FenceTag)
	switch len(bodies) {
	case 0:
		return nil, OutcomeAbsent
	case 1:
		var e Envelope
		if err := json.Unmarshal([]byte(bodies[0]), &e); err != nil {
			return nil, OutcomeMalformed
		}
		return &e, OutcomePresent
	default:
		// More than one reader-map block: ambiguous which is the delta — malformed.
		return nil, OutcomeMalformed
	}
}

// extractFencedBlocks returns the bodies of every fenced code block whose
// info-string (the text after the opening backtick run) equals tag. It is a small,
// standard-library-only Markdown fence scanner: an opening fence is a line whose
// trimmed content is a run of >=3 backticks immediately followed by the tag; the
// block runs until the next line whose trimmed content is a >=3 backtick run (the
// closing fence). An unterminated opening fence yields no block (it is not a
// well-formed block). This is intentionally minimal — flotilla turn-finals are
// plain text, not full CommonMark — and avoids a Markdown dependency.
func extractFencedBlocks(text, tag string) []string {
	var blocks []string
	// Normalize CRLF so a Windows-authored turn-final's fence lines match and the
	// collected body does not carry stray '\r' into the rendered delta.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		open := strings.TrimSpace(lines[i])
		if !isFenceWithTag(open, tag) {
			continue
		}
		// Collect the body until the closing bare-backtick fence.
		var body []string
		closed := false
		for j := i + 1; j < len(lines); j++ {
			if isBareFence(strings.TrimSpace(lines[j])) {
				closed = true
				i = j // resume scanning AFTER the closing fence
				break
			}
			body = append(body, lines[j])
		}
		if closed {
			blocks = append(blocks, strings.Join(body, "\n"))
		}
		// An unterminated fence is not a block; stop scanning (nothing well-formed
		// follows an unterminated fence in plain turn-final text).
		if !closed {
			break
		}
	}
	return blocks
}

// isFenceWithTag reports whether trimmed is an opening fence (>=3 backticks)
// whose info-string equals tag (e.g. "```reader-map").
func isFenceWithTag(trimmed, tag string) bool {
	n := leadingBackticks(trimmed)
	if n < 3 {
		return false
	}
	info := strings.TrimSpace(trimmed[n:])
	return info == tag
}

// isBareFence reports whether trimmed is a closing fence: a run of >=3 backticks
// with no trailing info-string.
func isBareFence(trimmed string) bool {
	n := leadingBackticks(trimmed)
	return n >= 3 && strings.TrimSpace(trimmed[n:]) == ""
}

// leadingBackticks returns the count of backticks at the start of s.
func leadingBackticks(s string) int {
	n := 0
	for n < len(s) && s[n] == '`' {
		n++
	}
	return n
}
