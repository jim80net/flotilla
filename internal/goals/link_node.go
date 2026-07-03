package goals

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// linkWorkItemYAMLNode appends a work item by editing the yaml AST in place so
// coordinator comments and surrounding structure survive the operation.
func linkWorkItemYAMLNode(raw []byte, goalID string, item WorkItem) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("goals: link: parse yaml: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("goals: link: empty yaml document")
	}
	root := doc.Content[0]
	goal := findGoalNode(root, goalID)
	if goal == nil {
		return nil, fmt.Errorf("goals: link: unknown goal id %q", goalID)
	}
	wiSeq := mappingGet(goal, "work_items")
	if nodeHasWorkItem(wiSeq, item) {
		// No structural change — return the original bytes verbatim.
		return raw, nil
	}
	newItem := buildWorkItemNode(item)
	if wiSeq == nil {
		wiSeq = &yaml.Node{Kind: yaml.SequenceNode}
		goal.Content = append(goal.Content,
			scalarKey("work_items"),
			wiSeq,
		)
	}
	wiSeq.Content = append(wiSeq.Content, newItem)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("goals: link: marshal yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("goals: link: marshal yaml: %w", err)
	}
	out := buf.Bytes()
	if _, err := ParseYAML(out); err != nil {
		return nil, err
	}
	return out, nil
}

func findGoalNode(root *yaml.Node, goalID string) *yaml.Node {
	if root == nil {
		return nil
	}
	switch root.Kind {
	case yaml.MappingNode:
		if goals := mappingGet(root, "goals"); goals != nil && goals.Kind == yaml.SequenceNode {
			if g := findGoalInSequence(goals, goalID); g != nil {
				return g
			}
		}
	case yaml.DocumentNode:
		if len(root.Content) > 0 {
			return findGoalNode(root.Content[0], goalID)
		}
	}
	return nil
}

func findGoalInSequence(seq *yaml.Node, goalID string) *yaml.Node {
	for _, g := range seq.Content {
		if found := findGoalMapping(g, goalID); found != nil {
			return found
		}
	}
	return nil
}

func findGoalMapping(goal *yaml.Node, goalID string) *yaml.Node {
	if goal == nil || goal.Kind != yaml.MappingNode {
		return nil
	}
	if id := mappingScalar(goal, "id"); id == goalID {
		return goal
	}
	if children := mappingGet(goal, "children"); children != nil && children.Kind == yaml.SequenceNode {
		if found := findGoalInSequence(children, goalID); found != nil {
			return found
		}
	}
	return nil
}

func mappingGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func mappingScalar(m *yaml.Node, key string) string {
	v := mappingGet(m, key)
	if v == nil || v.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(v.Value)
}

func nodeHasWorkItem(seq *yaml.Node, item WorkItem) bool {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return false
	}
	for _, wi := range seq.Content {
		if workItemNodeMatches(wi, item) {
			return true
		}
	}
	return false
}

func workItemNodeMatches(wi *yaml.Node, item WorkItem) bool {
	if wi == nil || wi.Kind != yaml.MappingNode {
		return false
	}
	kind := mappingScalar(wi, "kind")
	if kind != strings.TrimSpace(item.Kind) {
		return false
	}
	switch kind {
	case "issue":
		return mappingScalar(wi, "ref") == strings.TrimSpace(item.Ref)
	case "backlog":
		match := mappingScalar(wi, "match")
		if match == "" {
			match = mappingScalar(wi, "marker")
		}
		return match == strings.TrimSpace(item.Match)
	case "inline":
		return mappingScalar(wi, "text") == strings.TrimSpace(item.Text)
	case "desk":
		agent := mappingScalar(wi, "agent")
		if agent == "" {
			agent = mappingScalar(wi, "ref")
		}
		return agent == strings.TrimSpace(item.Agent)
	default:
		return false
	}
}

func buildWorkItemNode(item WorkItem) *yaml.Node {
	content := []*yaml.Node{
		scalarKey("kind"),
		scalarVal(strings.TrimSpace(item.Kind)),
	}
	switch strings.TrimSpace(item.Kind) {
	case "issue":
		content = append(content, scalarKey("ref"), scalarVal(strings.TrimSpace(item.Ref)))
	case "backlog":
		content = append(content, scalarKey("marker"), scalarVal(strings.TrimSpace(item.Match)))
	case "inline":
		content = append(content, scalarKey("text"), scalarVal(item.Text))
		if item.Done {
			content = append(content, scalarKey("done"), scalarBool(true))
		}
	case "desk":
		content = append(content, scalarKey("agent"), scalarVal(strings.TrimSpace(item.Agent)))
	}
	if label := strings.TrimSpace(item.Label); label != "" {
		content = append(content, scalarKey("label"), scalarVal(label))
	}
	return &yaml.Node{Kind: yaml.MappingNode, Content: content}
}

func scalarKey(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: s}
}

func scalarVal(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s, Style: yaml.DoubleQuotedStyle}
}

func scalarBool(v bool) *yaml.Node {
	val := "false"
	if v {
		val = "true"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: val}
}
