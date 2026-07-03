package goals

import "strings"

// NormalizeScope rewrites v1 scope tokens to v2 on compile (fleet→flotilla, project→desk).
// The legacy leaf alias scope=desk (pre-v2 task) is left unchanged here — depth-aware
// disambiguation happens in the dash read model at render time.
func NormalizeScope(scope string) string {
	trimmed := strings.TrimSpace(scope)
	switch trimmed {
	case "fleet":
		return "flotilla"
	case "project":
		return "desk"
	default:
		return trimmed
	}
}

// NormalizeFileScopes applies NormalizeScope to every goal in a compiled file.
func NormalizeFileScopes(f *File) {
	for i := range f.Goals {
		f.Goals[i].Scope = NormalizeScope(f.Goals[i].Scope)
	}
}
