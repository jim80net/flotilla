package surface

import (
	"fmt"
	"strings"

	"github.com/jim80net/flotilla/internal/deliver"
)

// paneCommandSurfaces maps tmux pane_current_command values to registered surface names.
// Keys are lowercase foreground commands observed on live desks.
var paneCommandSurfaces = map[string]string{
	"claude":      "claude-code",
	"claude-code": "claude-code",
	"grok":        "grok",
	"codex":       "codex",
	"opencode":    "opencode",
	"pi":          "pi",
	"aider":       "aider",
}

var genericRuntimeCommands = map[string]bool{
	"node": true, "python": true, "python3": true, "bun": true, "deno": true,
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

// ResolveLiveDriver is the fail-closed live-decision resolver. Unlike the
// compatibility ResolveDriver reader seam, it never falls back to roster state
// when the pane command cannot be read or mapped: actuation and live assessment
// must prove which harness owns the pane at the moment of use.
func ResolveLiveDriver(rosterSurface, pane string, paneCommand func(string) (string, error)) (Driver, string, bool, error) {
	return ResolveLiveDriverWithArgv(rosterSurface, pane, paneCommand, deliver.PaneArgv)
}

// ResolveLiveDriverWithArgv is the injectable form of ResolveLiveDriver.
// Generic runtimes do not name a harness, so their argv is the primary live
// identity and a registered roster surface is the bounded fallback.
func ResolveLiveDriverWithArgv(rosterSurface, pane string, paneCommand func(string) (string, error), paneArgv func(string) ([]string, error)) (Driver, string, bool, error) {
	if pane == "" || paneCommand == nil {
		return nil, "", false, fmt.Errorf("live surface resolution requires a pane and command reader")
	}
	cmd, err := paneCommand(pane)
	if err != nil {
		return nil, "", false, fmt.Errorf("read live command for pane %q: %w", pane, err)
	}
	want := rosterSurface
	if want == "" {
		want = DefaultSurface
	}
	liveSurface, ok := SurfaceFromPaneCommand(cmd)
	if !ok {
		normalized := strings.ToLower(strings.TrimSpace(cmd))
		if !genericRuntimeCommands[normalized] {
			return nil, "", false, fmt.Errorf("unrecognized live command %q for pane %q", cmd, pane)
		}
		if paneArgv != nil {
			if argv, argvErr := paneArgv(pane); argvErr == nil {
				liveSurface, ok = SurfaceFromProcessArgv(argv)
			}
		}
		if !ok {
			if _, registered := Get(want); !registered {
				return nil, "", false, fmt.Errorf("generic live command %q for pane %q has no harness argv and roster surface %q is not registered", cmd, pane, want)
			}
			liveSurface = want
		}
	}
	drv, ok := Get(liveSurface)
	if !ok {
		return nil, liveSurface, liveSurface != want, fmt.Errorf("unknown live surface %q for pane %q", liveSurface, pane)
	}
	return drv, liveSurface, liveSurface != want, nil
}

// SurfaceFromProcessArgv identifies a registered harness named by a runtime's
// argv. It matches exact argv tokens or exact path components, never substrings.
func SurfaceFromProcessArgv(argv []string) (string, bool) {
	found := ""
	for _, arg := range argv {
		components := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(arg)), func(r rune) bool {
			return r == '/' || r == '\\'
		})
		for _, component := range components {
			component = strings.TrimSuffix(component, ".js")
			if name, ok := SurfaceFromPaneCommand(component); ok {
				if found != "" && found != name {
					return "", false
				}
				found = name
				continue
			}
			if _, ok := Get(component); ok {
				if found != "" && found != component {
					return "", false
				}
				found = component
			}
		}
	}
	return found, found != ""
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
