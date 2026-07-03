package goals

import (
	"fmt"
	"os"
	"strings"
)

// LinkWorkItemYAML appends a work item to goalID in the yaml source. An identical
// attachment (same kind and identifying field) is a no-op. The returned bytes are
// re-validated fail-closed before the caller writes them. Comments and existing
// formatting are preserved via an in-place yaml.Node edit (not a struct round-trip).
func LinkWorkItemYAML(raw []byte, goalID string, item WorkItem) ([]byte, error) {
	goalID = strings.TrimSpace(goalID)
	if goalID == "" {
		return nil, fmt.Errorf("goals: link: goal id is required")
	}
	if err := validateLinkItem(item); err != nil {
		return nil, err
	}
	return linkWorkItemYAMLNode(raw, goalID, item)
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
	if err := writeFilePreserveMode(yamlPath, out); err != nil {
		return err
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
