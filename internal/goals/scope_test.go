package goals

import (
	"strings"
	"testing"
)

func TestNormalizeScope_V1ToV2(t *testing.T) {
	cases := map[string]string{
		"fleet": "flotilla", "project": "desk",
		"flotilla": "flotilla", "desk": "desk", "task": "task", "": "",
	}
	for in, want := range cases {
		if got := NormalizeScope(in); got != want {
			t.Errorf("NormalizeScope(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeScope_TrimsWhitespace(t *testing.T) {
	if got := NormalizeScope("  task  "); got != "task" {
		t.Errorf("NormalizeScope trims whitespace, got %q", got)
	}
}

func TestCompileJSON_EmitsV2Scopes(t *testing.T) {
	raw := []byte("version: 1\ngoals:\n  - {id: root, title: R, scope: fleet}\n  - {id: child, title: C, scope: project, parent: root}\n")
	b, err := CompileYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"scope": "flotilla"`) || !strings.Contains(s, `"scope": "desk"`) {
		t.Fatalf("compiled json should emit v2 scopes, got:\n%s", b)
	}
	if strings.Contains(s, `"scope": "fleet"`) || strings.Contains(s, `"scope": "project"`) {
		t.Fatalf("compiled json must not retain v1 scope tokens, got:\n%s", b)
	}
}
