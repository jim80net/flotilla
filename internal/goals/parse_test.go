package goals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseYAML_TreeFlattenAndEdges(t *testing.T) {
	const y = `
version: 1
goals:
  - id: g-root
    title: Root
    status: active
    children:
      - id: ws-active
        title: Active
        status: active
        depends_on: [ws-done]
        work_items:
          - kind: backlog
            match: "[in-flight] shipping it"
      - id: ws-done
        title: Done
        status: achieved
        work_items:
          - kind: backlog
            match: "[done] shipped"
`
	f, err := ParseYAML([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Goals) != 3 {
		t.Fatalf("flat goals = %d, want 3", len(f.Goals))
	}
	byID := indexGoals(f.Goals)
	if byID["ws-active"].Parent != "g-root" || byID["ws-done"].Parent != "g-root" {
		t.Errorf("children parent refs wrong: ws-active=%q ws-done=%q", byID["ws-active"].Parent, byID["ws-done"].Parent)
	}
	if len(byID["ws-active"].DependsOn) != 1 || byID["ws-active"].DependsOn[0] != "ws-done" {
		t.Errorf("depends_on = %v", byID["ws-active"].DependsOn)
	}
}

func TestParseYAML_MarkerAliasCompiledToMatch(t *testing.T) {
	f, err := ParseYAML([]byte(`version: 1
goals:
  - id: g
    title: G
    work_items:
      - kind: backlog
        marker: "[blocked] wait"
`))
	if err != nil {
		t.Fatal(err)
	}
	wi := f.Goals[0].WorkItems[0]
	if wi.Match != "[blocked] wait" {
		t.Errorf("marker should compile to match, got %+v", wi)
	}
}

func TestParseYAML_DeskRefAliasCompiledToAgent(t *testing.T) {
	f, err := ParseYAML([]byte(`version: 1
goals:
  - id: g
    title: G
    work_items:
      - kind: desk
        ref: builder
`))
	if err != nil {
		t.Fatal(err)
	}
	wi := f.Goals[0].WorkItems[0]
	if wi.Agent != "builder" {
		t.Errorf("desk ref alias → agent, got %+v", wi)
	}
}

func TestParseYAML_RejectsDuplicateEmptyCycleDependsOn(t *testing.T) {
	cases := []struct {
		name, y, want string
	}{
		{"duplicate", "version: 1\ngoals:\n  - {id: x, title: A}\n  - {id: x, title: B}\n", "duplicate"},
		{"empty id", "version: 1\ngoals:\n  - {id: '', title: A}\n", "empty id"},
		{"null node", "version: 1\ngoals:\n  -\n", "null"},
		{"dangling depends_on", "version: 1\ngoals:\n  - {id: a, title: A, depends_on: [ghost]}\n", "depends_on"},
		{"unknown parent flat", "version: 1\ngoals:\n  - {id: a, title: A, parent: ghost}\n", "unknown parent"},
		{"cycle flat", "version: 1\ngoals:\n  - {id: a, title: A, parent: b}\n  - {id: b, title: B, parent: a}\n", "cyclic"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseYAML([]byte(tc.y)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestParseYAML_ParentDisagreesWithStructure(t *testing.T) {
	y := `version: 1
goals:
  - id: root
    title: Root
    children:
      - id: kid
        title: Kid
        parent: other
`
	if _, err := ParseYAML([]byte(y)); err == nil || !strings.Contains(err.Error(), "disagrees") {
		t.Fatalf("parent/structure mismatch must error, got %v", err)
	}
}

func TestParseYAML_Malformed(t *testing.T) {
	if _, err := ParseYAML([]byte("version: 1\ngoals: not-a-list")); err == nil {
		t.Fatal("malformed yaml must error")
	}
}

func TestParseYAML_NoGoalsIsEmptySlice(t *testing.T) {
	f, err := ParseYAML([]byte("version: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Goals == nil || len(f.Goals) != 0 {
		t.Errorf("no goals → empty slice, got %v", f.Goals)
	}
}

func TestLoadYAML_MissingFileIsEmptyNotError(t *testing.T) {
	f, err := LoadYAML(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Goals) != 0 {
		t.Errorf("missing file → empty, got %d goals", len(f.Goals))
	}
}

func TestCompileYAML_RoundTripJSON(t *testing.T) {
	raw := []byte(`version: 1
default_view: true
goals:
  - id: a
    title: A
    conversation_agent: builder
    children:
      - id: b
        title: B
        depends_on: [a]
`)
	j, err := CompileYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(j), `"match"`) && strings.Contains(string(raw), "match") {
		// no match in this fixture — ok
	}
	if !strings.Contains(string(j), `"conversation_agent": "builder"`) {
		t.Errorf("compiled json missing conversation_agent: %s", j)
	}
}

func TestParse_CommittedExample(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "fleet-goals.example.yaml"))
	if err != nil {
		t.Skipf("example not in tree: %v", err)
	}
	if _, err := ParseYAML(raw); err != nil {
		t.Fatalf("fleet-goals.example.yaml must parse: %v", err)
	}
}

func indexGoals(gs []Goal) map[string]Goal {
	m := make(map[string]Goal, len(gs))
	for _, g := range gs {
		m[g.ID] = g
	}
	return m
}
