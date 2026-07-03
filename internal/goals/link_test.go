package goals

import (
	"strings"
	"testing"
)

const linkFixtureYAML = `version: 1
goals:
  - id: root
    title: Root
    children:
      - id: child
        title: Child
        work_items:
          - kind: issue
            ref: owner/repo#1
`

func TestLinkWorkItemYAML_AppendsIssue(t *testing.T) {
	out, err := LinkWorkItemYAML([]byte(linkFixtureYAML), "child", WorkItem{
		Kind: "issue",
		Ref:  "owner/repo#2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "owner/repo#2") {
		t.Fatalf("linked issue missing from yaml:\n%s", out)
	}
	f, err := ParseYAML(out)
	if err != nil {
		t.Fatal(err)
	}
	var child *Goal
	for i := range f.Goals {
		if f.Goals[i].ID == "child" {
			child = &f.Goals[i]
			break
		}
	}
	if child == nil || len(child.WorkItems) != 2 {
		t.Fatalf("child work items = %+v", child)
	}
}

func TestLinkWorkItemYAML_Idempotent(t *testing.T) {
	item := WorkItem{Kind: "backlog", Match: "[in-flight] ship it"}
	out1, err := LinkWorkItemYAML([]byte(linkFixtureYAML), "child", item)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := LinkWorkItemYAML(out1, "child", item)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseYAML(out2)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range f.Goals {
		if g.ID != "child" {
			continue
		}
		count := 0
		for _, wi := range g.WorkItems {
			if wi.Kind == "backlog" && wi.Match == "[in-flight] ship it" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("expected one backlog item, got %d", count)
		}
	}
}

func TestLinkWorkItemYAML_UnknownGoal(t *testing.T) {
	if _, err := LinkWorkItemYAML([]byte(linkFixtureYAML), "missing", WorkItem{
		Kind: "inline",
		Text: "x",
	}); err == nil || !strings.Contains(err.Error(), "unknown goal") {
		t.Fatalf("want unknown goal error, got %v", err)
	}
}
