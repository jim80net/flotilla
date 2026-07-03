package goals

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// LinkWorkItemYAML appends a work item to goalID in the yaml source. An identical
// attachment (same kind and identifying field) is a no-op. The returned bytes are
// re-validated fail-closed before the caller writes them.
func LinkWorkItemYAML(raw []byte, goalID string, item WorkItem) ([]byte, error) {
	goalID = strings.TrimSpace(goalID)
	if goalID == "" {
		return nil, fmt.Errorf("goals: link: goal id is required")
	}
	if err := validateLinkItem(item); err != nil {
		return nil, err
	}
	var yf yamlFile
	if err := yaml.Unmarshal(raw, &yf); err != nil {
		return nil, fmt.Errorf("goals: link: parse yaml: %w", err)
	}
	if yf.Goals == nil {
		return nil, fmt.Errorf("goals: link: unknown goal id %q", goalID)
	}
	if !linkInGoals(yf.Goals, goalID, item) {
		return nil, fmt.Errorf("goals: link: unknown goal id %q", goalID)
	}
	out, err := yaml.Marshal(&yf)
	if err != nil {
		return nil, fmt.Errorf("goals: link: marshal yaml: %w", err)
	}
	if _, err := ParseYAML(out); err != nil {
		return nil, err
	}
	return out, nil
}

// LinkWorkItemFile reads yamlPath, links the item onto goalID, writes yaml back,
// and refreshes the compiled json cache at jsonPath.
func LinkWorkItemFile(yamlPath, jsonPath, goalID string, item WorkItem) error {
	raw, err := os.ReadFile(yamlPath)
	if err != nil {
		return fmt.Errorf("goals: link: read %q: %w", yamlPath, err)
	}
	out, err := LinkWorkItemYAML(raw, goalID, item)
	if err != nil {
		return err
	}
	if err := os.WriteFile(yamlPath, out, 0o600); err != nil {
		return fmt.Errorf("goals: link: write %q: %w", yamlPath, err)
	}
	f, err := ParseYAML(out)
	if err != nil {
		return err
	}
	return WriteJSON(jsonPath, f)
}

func validateLinkItem(item WorkItem) error {
	switch strings.TrimSpace(item.Kind) {
	case "issue":
		if strings.TrimSpace(item.Ref) == "" {
			return fmt.Errorf("goals: link: --issue requires owner/repo#N")
		}
	case "backlog":
		if strings.TrimSpace(item.Match) == "" {
			return fmt.Errorf("goals: link: --backlog requires match text")
		}
	case "inline":
		if strings.TrimSpace(item.Text) == "" {
			return fmt.Errorf("goals: link: --inline requires text")
		}
	case "desk":
		if strings.TrimSpace(item.Agent) == "" {
			return fmt.Errorf("goals: link: --desk requires agent name")
		}
	default:
		return fmt.Errorf("goals: link: unsupported work item kind %q (try issue, backlog, inline, desk)", item.Kind)
	}
	return nil
}

func linkInGoals(nodes []*yamlGoal, goalID string, item WorkItem) bool {
	for _, n := range nodes {
		if n == nil {
			continue
		}
		if strings.TrimSpace(n.ID) == goalID {
			if !hasWorkItem(n.WorkItems, item) {
				n.WorkItems = append(n.WorkItems, workItemToYAML(item))
			}
			return true
		}
		if linkInGoals(n.Children, goalID, item) {
			return true
		}
	}
	return false
}

func hasWorkItem(items []yamlWorkItem, item WorkItem) bool {
	for _, wi := range items {
		n := normalizeWorkItem(wi)
		if n.Kind != strings.TrimSpace(item.Kind) {
			continue
		}
		switch n.Kind {
		case "issue":
			if n.Ref == strings.TrimSpace(item.Ref) {
				return true
			}
		case "backlog":
			if n.Match == strings.TrimSpace(item.Match) {
				return true
			}
		case "inline":
			if n.Text == item.Text {
				return true
			}
		case "desk":
			if n.Agent == strings.TrimSpace(item.Agent) {
				return true
			}
		}
	}
	return false
}

func workItemToYAML(item WorkItem) yamlWorkItem {
	wi := yamlWorkItem{
		Kind:  strings.TrimSpace(item.Kind),
		Agent: strings.TrimSpace(item.Agent),
		Ref:   strings.TrimSpace(item.Ref),
		Text:  item.Text,
		Done:  item.Done,
		Label: strings.TrimSpace(item.Label),
	}
	if wi.Kind == "backlog" {
		wi.Marker = strings.TrimSpace(item.Match)
	}
	return wi
}
