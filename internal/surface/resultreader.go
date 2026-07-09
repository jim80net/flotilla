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

// ResolveDriver picks the Driver for a desk pane. When the live harness on the pane
// (pane_current_command) disagrees with the roster/overlay surface — common during model
// cutover before launch.json and the roster converge — the LIVE harness wins so assess,
// composer, recycle, and result-store paths match the process actually running (#586, #573).
//
// paneCommand may be nil (or return an error): then the roster surface is used unchanged.
func ResolveDriver(rosterSurface, pane string, paneCommand func(string) (string, error)) (drv Driver, liveSurface string, drift bool, err error) {
	want := rosterSurface
	if want == "" {
		want = DefaultSurface
	}
	liveSurface = want
	if paneCommand != nil && pane != "" {
		if cmd, cmdErr := paneCommand(pane); cmdErr == nil {
			if mapped, ok := SurfaceFromPaneCommand(cmd); ok && mapped != want {
				liveSurface = mapped
				drift = true
			}
		}
	}
	drv, ok := Get(liveSurface)
	if !ok {
		return nil, liveSurface, drift, fmt.Errorf("unknown surface %q for agent pane %q", liveSurface, pane)
	}
	return drv, liveSurface, drift, nil
}

// ResolveResultReader picks the ResultReader for a desk pane. Live harness wins on drift —
// same policy as ResolveDriver — so `flotilla result` reads the session store that actually
// exists (e.g. grok store while roster still says claude-code).
func ResolveResultReader(rosterSurface, pane string, paneCommand func(string) (string, error)) (rr ResultReader, drv Driver, liveSurface string, drift bool, err error) {
	drv, liveSurface, drift, err = ResolveDriver(rosterSurface, pane, paneCommand)
	if err != nil {
		return nil, nil, liveSurface, drift, err
	}
	rr, ok := drv.(ResultReader)
	if !ok {
		return nil, drv, liveSurface, drift, fmt.Errorf("surface %q has no session-store result reader", liveSurface)
	}
	return rr, drv, liveSurface, drift, nil
}
