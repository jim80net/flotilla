package surface

import (
	"fmt"
	"strings"
)

// paneCommandSurfaces maps tmux pane_current_command values to registered surface names.
// Keys are lowercase foreground commands observed on live desks.
var paneCommandSurfaces = map[string]string{
	"claude":   "claude-code",
	"grok":     "grok",
	"codex":    "codex",
	"opencode": "opencode",
}

// SurfaceFromPaneCommand maps a pane's foreground command to a registered surface name.
// ok is false when the command is empty, a shell, or not a known harness.
func SurfaceFromPaneCommand(cmd string) (name string, ok bool) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "", false
	}
	name, ok = paneCommandSurfaces[strings.ToLower(cmd)]
	return name, ok
}

// ResolveResultReader picks the ResultReader for a desk pane. When the live harness on the pane
// (pane_current_command) disagrees with the roster surface — common during model cutover before
// launch.json and the roster converge — the LIVE harness wins so `flotilla result` reads the
// session store that actually exists (e.g. claude-code transcript while roster still says grok).
func ResolveResultReader(rosterSurface, pane string, paneCommand func(string) (string, error)) (rr ResultReader, drv Driver, liveSurface string, drift bool, err error) {
	want := rosterSurface
	if want == "" {
		want = DefaultSurface
	}
	liveSurface = want
	if paneCommand != nil {
		if cmd, cmdErr := paneCommand(pane); cmdErr == nil {
			if mapped, ok := SurfaceFromPaneCommand(cmd); ok && mapped != want {
				liveSurface = mapped
				drift = true
			}
		}
	}
	drv, ok := Get(liveSurface)
	if !ok {
		return nil, nil, liveSurface, drift, fmt.Errorf("unknown surface %q for agent pane %q", liveSurface, pane)
	}
	rr, ok = drv.(ResultReader)
	if !ok {
		return nil, drv, liveSurface, drift, fmt.Errorf("surface %q has no session-store result reader", liveSurface)
	}
	return rr, drv, liveSurface, drift, nil
}
