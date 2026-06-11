package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveTracker returns the path the change-detector should hash for an agent: the
// workspace state.md when it exists AND is non-empty, else `fallback` (the caller's
// already-resolved --tracker-file / default). An empty scaffolded state.md does NOT
// hijack the tracker (mirrors StatePointer) — only a real, populated tracker takes
// over, so `flotilla workspace init` never silently blinds the detector.
//
// This is the SINGLE source the caller MUST use for BOTH the hash target AND the
// continuation prompt's {{tracker}} placeholder, so the path the XO is told to read
// can never diverge from the path the detector hashes (the P1-2 invariant).
func ResolveTracker(agent, fallback string) (string, error) {
	dir, err := Dir(agent)
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, StateFileName)
	if info, serr := os.Stat(p); serr == nil && !info.IsDir() && info.Size() > 0 {
		return p, nil
	}
	return fallback, nil
}

// ResolvePrompt resolves an agent's continuation prompt: the workspace HEARTBEAT.md
// (when present AND non-empty — an empty scaffolded file never replaces the built-in,
// which would send a blank prompt) used as a template, else `builtin`. In whichever,
// it substitutes {{tracker}} and {{settle}} from the resolved paths; the caller appends
// any ack instruction. The `builtin` MUST itself use the {{tracker}}/{{settle}}
// placeholders so that the no-workspace result is byte-identical to interpolating the
// paths into the prompt directly (the additive-on-no-workspace requirement).
func ResolvePrompt(agent, builtin, tracker, settle string) (string, error) {
	tmpl := builtin
	dir, err := Dir(agent)
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, HeartbeatFileName)
	raw, rerr := os.ReadFile(p)
	switch {
	case rerr == nil:
		if content := strings.TrimRight(string(raw), "\r\n"); strings.TrimSpace(content) != "" {
			tmpl = content
		}
	case !os.IsNotExist(rerr):
		return "", fmt.Errorf("read heartbeat prompt %q: %w", p, rerr)
	}
	tmpl = strings.ReplaceAll(tmpl, "{{tracker}}", tracker)
	tmpl = strings.ReplaceAll(tmpl, "{{settle}}", settle)
	return tmpl, nil
}
