package doctrine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Action is what the install did for one member. It is reported per member so the
// caller can print a kept/appended/skipped/created line, mirroring `workspace init`'s
// kept/created reporting.
type Action string

const (
	// ActionAppended: an identity-append member's marked block was absent and was
	// appended once.
	ActionAppended Action = "appended"
	// ActionSkipped: an identity-append member's opening marker was already present,
	// so the append was skipped (operator edits inside/around the block are left
	// untouched).
	ActionSkipped Action = "skipped"
	// ActionCreated: a whole-file (heartbeat-skill) member's target file was absent
	// and was written.
	ActionCreated Action = "created"
	// ActionKept: a whole-file (heartbeat-skill) member's target file already existed
	// and was left untouched (operator edits survive).
	ActionKept Action = "kept"
)

// Result reports the install outcome for one member.
type Result struct {
	Member string
	Action Action
	// Reason is a short human-readable note (e.g. why an append was skipped).
	Reason string
}

// Install applies each member to an agent's workspace, keying idempotency on each
// member's mechanism. It dispatches per member:
//
//   - identity-append: the member's marked block is appended into the identity file
//     (<workspaceDir>/<identityFile>) IFF its opening marker is ABSENT, else the
//     marker is detected and the append is skipped — operator edits inside or around
//     the block survive a re-install untouched.
//   - heartbeat-skill: the member's content is written as a WHOLE FILE to
//     <workspaceDir>/<member.TargetFile>, keyed on a STAT of that file — created if
//     absent (its parent dirs created too), kept if present (operator edits survive).
//
// The identity file <workspaceDir>/<identityFile> MUST already exist (the workspace's
// identity file is always written by `workspace init`); Install appends into it rather
// than creating it. The caller passes the already-resolved identity base filename so
// this package stays dependency-free — it does NOT import internal/workspace to derive
// the name (design Q-D said "derive from workspace.IdentityFileName", but doing so here
// would force a workspace import; the caller already holds that surface, so it resolves
// the filename and passes it, keeping doctrine pure).
//
// The loop iterates the given members and dispatches by Mechanism, so it is
// member-count- and member-content-agnostic.
func Install(workspaceDir, identityFile string, members []Member) ([]Result, error) {
	identityPath := filepath.Join(workspaceDir, identityFile)
	existing, err := os.ReadFile(identityPath)
	if err != nil {
		return nil, fmt.Errorf("read identity file %q: %w", identityPath, err)
	}
	content := string(existing)
	identityTouched := false

	results := make([]Result, 0, len(members))
	for _, m := range members {
		switch m.Mechanism {
		case MechanismIdentityAppend:
			// MECHANISM COUPLING: the seed (workspace init) and `doctrine install` both
			// call Install over the WHOLE member set. The day a new mechanism ships it
			// MUST be added here AT THE SAME TIME, or every caller passing a member of the
			// new kind starts erroring (the default arm below). The identity-append arm
			// accumulates into `content`; the write-back fires only if it changed.
			res, newContent, ierr := appendOnce(m, content)
			if ierr != nil {
				return results, ierr
			}
			if res.Action == ActionAppended {
				identityTouched = true
			}
			content = newContent
			results = append(results, res)
		case MechanismHeartbeatSkill:
			// Whole-file: its OWN write, disjoint from the identity-content write-back.
			// It does NOT route through appendOnce (which hard-errors on an empty marker)
			// and does NOT gate on identityTouched (that gates only the identity file).
			res, ierr := installWholeFile(m, workspaceDir)
			if ierr != nil {
				return results, ierr
			}
			results = append(results, res)
		default:
			return results, fmt.Errorf("member %q: unsupported mechanism %q", m.Name, m.Mechanism)
		}
	}

	// Write the identity file back once, only if an identity-append member actually
	// appended (a pure all-skip identity install must not rewrite the file — that would
	// needlessly churn its mtime and could disturb a concurrent reader for no benefit).
	// This gate is independent of any whole-file write above, which already happened.
	if identityTouched {
		if err := os.WriteFile(identityPath, []byte(content), 0o644); err != nil {
			return results, fmt.Errorf("write identity file %q: %w", identityPath, err)
		}
	}
	return results, nil
}

// installWholeFile installs a heartbeat-skill member as a whole file at
// <workspaceDir>/<m.TargetFile>: STAT-based idempotency — an existing target is KEPT
// (operator edits survive), an absent one is CREATED via its own write (its parent
// dirs, e.g. "skills/", created as needed). An empty TargetFile is a config error.
func installWholeFile(m Member, workspaceDir string) (Result, error) {
	if m.TargetFile == "" {
		return Result{}, fmt.Errorf("heartbeat-skill member %q has no TargetFile", m.Name)
	}
	// Defense-in-depth (cubic P2): TargetFile is registry-controlled today, but it MUST be a
	// workspace-relative, non-traversing path so a future member (or an edited registry) can never
	// write OUTSIDE the agent's workspace. Reject an absolute path or one that escapes via `..`.
	if filepath.IsAbs(m.TargetFile) {
		return Result{}, fmt.Errorf("heartbeat-skill member %q TargetFile %q must be workspace-relative, not absolute", m.Name, m.TargetFile)
	}
	target := filepath.Join(workspaceDir, m.TargetFile)
	if rel, err := filepath.Rel(filepath.Clean(workspaceDir), target); err != nil ||
		rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Result{}, fmt.Errorf("heartbeat-skill member %q TargetFile %q escapes the workspace dir", m.Name, m.TargetFile)
	}
	if _, err := os.Stat(target); err == nil {
		return Result{Member: m.Name, Action: ActionKept, Reason: "file present"}, nil
	} else if !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("stat skill file %q: %w", target, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return Result{}, fmt.Errorf("create skill dir for %q: %w", target, err)
	}
	if err := os.WriteFile(target, []byte(m.Content), 0o644); err != nil {
		return Result{}, fmt.Errorf("write skill file %q: %w", target, err)
	}
	return Result{Member: m.Name, Action: ActionCreated}, nil
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
