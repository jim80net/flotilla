package goals

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// yamlFile is the fleet-goals.yaml top-level shape. Goals may be nested (children:) or
// flat (parent: refs); both compile to the flat File contract.
type yamlFile struct {
	Version     int         `yaml:"version"`
	DefaultView bool        `yaml:"default_view"`
	Goals       []*yamlGoal `yaml:"goals"`
}

type yamlGoal struct {
	ID                string         `yaml:"id"`
	Title             string         `yaml:"title"`
	Description       string         `yaml:"description"`
	Scope             string         `yaml:"scope"`
	Parent            *yamlParent    `yaml:"parent"`
	Owner             string         `yaml:"owner"`
	Status            string         `yaml:"status"`
	ConversationAgent string         `yaml:"conversation_agent"`
	TopologyChannelID string         `yaml:"topology_channel_id"`
	Priorities        []string       `yaml:"priorities"`
	Milestones        []string       `yaml:"milestones"`
	DependsOn         []string       `yaml:"depends_on"`
	WorkItems         []yamlWorkItem `yaml:"work_items"`
	Children          []*yamlGoal    `yaml:"children"`
}

// yamlParent accepts `parent: null` or `parent: some-id` in YAML.
type yamlParent struct {
	value string
	isSet bool
}

func (p *yamlParent) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	switch value.Kind {
	case yaml.ScalarNode:
		if value.Tag == "!!null" || value.Value == "null" || value.Value == "~" {
			p.isSet, p.value = true, ""
			return nil
		}
		p.isSet, p.value = true, strings.TrimSpace(value.Value)
		return nil
	case yaml.AliasNode:
		return p.UnmarshalYAML(value.Alias)
	default:
		return fmt.Errorf("goals: parent must be a string or null")
	}
}

type yamlWorkItem struct {
	Kind   string `yaml:"kind"`
	Agent  string `yaml:"agent"`
	Ref    string `yaml:"ref"`
	Match  string `yaml:"match"`
	Marker string `yaml:"marker"` // authoring alias → compiled to Match
	Text   string `yaml:"text"`
	Done   bool   `yaml:"done"`
	Label  string `yaml:"label"`
}

// ParseYAML decodes and validates fleet-goals.yaml bytes into a flat File. Malformed YAML,
// duplicate ids, cycles, dangling depends_on, and null list entries fail closed.
func ParseYAML(raw []byte) (File, error) {
	var yf yamlFile
	if err := yaml.Unmarshal(raw, &yf); err != nil {
		return File{}, fmt.Errorf("goals: parse yaml: %w", err)
	}
	goals := yf.Goals
	if goals == nil {
		goals = []*yamlGoal{}
	}
	flat := make([]Goal, 0, len(goals))
	if err := flattenGoals(goals, "", &flat); err != nil {
		return File{}, err
	}
	f := File{Version: yf.Version, DefaultView: yf.DefaultView, Goals: flat}
	if err := f.validate(); err != nil {
		return File{}, err
	}
	return f, nil
}

func flattenGoals(nodes []*yamlGoal, structuralParent string, out *[]Goal) error {
	for _, n := range nodes {
		if n == nil {
			return fmt.Errorf("goals: a node is null (malformed yaml list entry)")
		}
		id := strings.TrimSpace(n.ID)
		if id == "" {
			return fmt.Errorf("goals: a node has an empty id")
		}
		parent := structuralParent
		if n.Parent != nil && n.Parent.isSet {
			parent = strings.TrimSpace(n.Parent.value)
		}
		if structuralParent != "" && n.Parent != nil && n.Parent.isSet && parent != structuralParent {
			return fmt.Errorf("goals: node %q parent %q disagrees with structural parent %q",
				id, parent, structuralParent)
		}
		items := make([]WorkItem, 0, len(n.WorkItems))
		for _, wi := range n.WorkItems {
			items = append(items, normalizeWorkItem(wi))
		}
		*out = append(*out, Goal{
			ID:                id,
			Title:             n.Title,
			Description:       n.Description,
			Scope:             n.Scope,
			Parent:            parent,
			Owner:             n.Owner,
			Status:            n.Status,
			ConversationAgent: strings.TrimSpace(n.ConversationAgent),
			TopologyChannelID: strings.TrimSpace(n.TopologyChannelID),
			Priorities:        append([]string(nil), n.Priorities...),
			Milestones:        append([]string(nil), n.Milestones...),
			DependsOn:         append([]string(nil), n.DependsOn...),
			WorkItems:         items,
		})
		if err := flattenGoals(n.Children, id, out); err != nil {
			return err
		}
	}
	return nil
}

func normalizeWorkItem(wi yamlWorkItem) WorkItem {
	out := WorkItem{
		Kind:  strings.TrimSpace(wi.Kind),
		Agent: strings.TrimSpace(wi.Agent),
		Match: strings.TrimSpace(wi.Match),
		Ref:   strings.TrimSpace(wi.Ref),
		Text:  wi.Text,
		Done:  wi.Done,
		Label: strings.TrimSpace(wi.Label),
	}
	switch out.Kind {
	case "desk":
		if out.Agent == "" && out.Ref != "" {
			out.Agent = out.Ref
		}
	case "backlog":
		if out.Match == "" && wi.Marker != "" {
			out.Match = strings.TrimSpace(wi.Marker)
		}
	}
	return out
}

func (f File) validate() error {
	ids := make(map[string]bool, len(f.Goals))
	for _, g := range f.Goals {
		if strings.TrimSpace(g.ID) == "" {
			return fmt.Errorf("goals: a goal has an empty id (every node needs a unique slug)")
		}
		if ids[g.ID] {
			return fmt.Errorf("goals: duplicate goal id %q", g.ID)
		}
		ids[g.ID] = true
	}
	parent := make(map[string]string, len(f.Goals))
	for _, g := range f.Goals {
		if g.Parent != "" && !ids[g.Parent] {
			return fmt.Errorf("goals: goal %q references unknown parent %q", g.ID, g.Parent)
		}
		parent[g.ID] = g.Parent
	}
	for _, g := range f.Goals {
		seen := map[string]bool{}
		for cur := g.ID; cur != ""; cur = parent[cur] {
			if seen[cur] {
				return fmt.Errorf("goals: cyclic parent chain detected at goal %q (goals must be acyclic)", g.ID)
			}
			seen[cur] = true
		}
	}
	for _, g := range f.Goals {
		seenDep := make(map[string]bool, len(g.DependsOn))
		for _, dep := range g.DependsOn {
			if strings.TrimSpace(dep) == "" {
				return fmt.Errorf("goals: goal %q has an empty depends_on entry", g.ID)
			}
			if dep == g.ID {
				return fmt.Errorf("goals: goal %q cannot depend_on itself", g.ID)
			}
			if seenDep[dep] {
				return fmt.Errorf("goals: goal %q has duplicate depends_on entry %q", g.ID, dep)
			}
			seenDep[dep] = true
			if !ids[dep] {
				return fmt.Errorf("goals: goal %q references unknown depends_on target %q", g.ID, dep)
			}
		}
	}
	return nil
}
