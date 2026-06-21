package doctrine

import (
	"fmt"
	"os"
	"strings"
)

// Action is what the install did for one member. It is reported per member so the
// caller can print a kept/appended/skipped line, mirroring `workspace init`'s
// kept/created reporting.
type Action string

const (
	// ActionAppended: the member's marked block was absent and was appended once.
	ActionAppended Action = "appended"
	// ActionSkipped: the member's opening marker was already present, so the append
	// was skipped (operator edits inside/around the block are left untouched).
	ActionSkipped Action = "skipped"
)

// Result reports the install outcome for one member.
type Result struct {
	Member string
	Action Action
	// Reason is a short human-readable note (e.g. why an append was skipped).
	Reason string
}

// Install applies each member to the agent's identity file at identityPath, keying
// idempotency on each member's mechanism. v1's only mechanism is identity-append:
// the member's marked block is appended IFF its opening marker is ABSENT from the
// file, else the marker is detected and the append is skipped — so operator edits
// inside or around the block survive a re-install untouched.
//
// identityPath MUST already exist (the workspace's identity file is always written
// by `workspace init`); Install appends into it rather than creating it. The loop
// iterates the given members and dispatches by Mechanism, so it is member-count- and
// member-content-agnostic.
func Install(identityPath string, members []Member) ([]Result, error) {
	existing, err := os.ReadFile(identityPath)
	if err != nil {
		return nil, fmt.Errorf("read identity file %q: %w", identityPath, err)
	}
	content := string(existing)

	results := make([]Result, 0, len(members))
	for _, m := range members {
		switch m.Mechanism {
		case MechanismIdentityAppend:
			// MECHANISM COUPLING: the seed (workspace init) and `doctrine install` both
			// call Install over the WHOLE member set, but Install only handles
			// identity-append — any other mechanism hard-errors below. The day a 2nd
			// mechanism ships it MUST be added here AT THE SAME TIME, or every caller
			// passing a member of the new kind starts erroring. anyAppended(results)
			// derives the write decision; appendOnce just returns the (possibly updated)
			// content, so no per-member "changed" flag is needed.
			res, newContent, ierr := appendOnce(m, content)
			if ierr != nil {
				return results, ierr
			}
			content = newContent
			results = append(results, res)
		default:
			return results, fmt.Errorf("member %q: unsupported mechanism %q", m.Name, m.Mechanism)
		}
	}

	// Write back once, only if any member appended (a pure all-skip install must not
	// rewrite the file — that would needlessly churn its mtime and could disturb a
	// concurrent reader for no benefit).
	if anyAppended(results) {
		if err := os.WriteFile(identityPath, []byte(content), 0o644); err != nil {
			return results, fmt.Errorf("write identity file %q: %w", identityPath, err)
		}
	}
	return results, nil
}

// appendOnce returns the install Result for an identity-append member and the
// (possibly updated) file content. If the member's opening marker is already present
// it returns a skip and leaves content unchanged; otherwise it appends the member's
// marked block (separated from prior content by a blank line) and reports an append.
func appendOnce(m Member, content string) (Result, string, error) {
	if m.OpenMarker == "" || m.CloseMarker == "" {
		return Result{}, content, fmt.Errorf("identity-append member %q has no marker fence", m.Name)
	}
	if strings.Contains(content, m.OpenMarker) {
		return Result{Member: m.Name, Action: ActionSkipped, Reason: "marker present"}, content, nil
	}
	// Ensure exactly one blank line separates prior content from the appended block,
	// so a stub that does or does not end in a newline both produce clean output.
	sep := "\n\n"
	switch {
	case content == "":
		sep = ""
	case strings.HasSuffix(content, "\n\n"):
		sep = ""
	case strings.HasSuffix(content, "\n"):
		sep = "\n"
	}
	return Result{Member: m.Name, Action: ActionAppended}, content + sep + m.Content, nil
}

func anyAppended(results []Result) bool {
	for _, r := range results {
		if r.Action == ActionAppended {
			return true
		}
	}
	return false
}
