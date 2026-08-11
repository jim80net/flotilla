package roster

import (
	"fmt"

	"github.com/jim80net/flotilla/internal/org"
)

// LoadOptions configures optional org-truth compilation (org-truth v1 PR2).
type LoadOptions struct {
	// OrgFile is an explicit path from --org-file / FLOTILLA_ORG_FILE.
	// Empty ⇒ discover <roster-dir>/fleet-org.yaml (optional; absent = derive-only).
	// Non-empty ⇒ that path is required to exist and load.
	OrgFile string
}

// attachOrgDAG builds the org-truth DAG after synthesis validation.
//
//   - No org file (or default path missing) → canonical reports-to derivation.
//   - Org file present → Compile + CheckHomes + Agree; store the file DAG
//     (single primary reports_to per design §9).
func (c *Config) attachOrgDAG(rosterPath string, opts LoadOptions) error {
	names := make([]string, 0, len(c.Agents))
	for _, a := range c.Agents {
		names = append(names, a.Name)
	}
	channels := make([]org.Channel, 0, len(c.Bindings()))
	for _, ch := range c.Bindings() {
		channels = append(channels, org.Channel{ChannelID: ch.ChannelID, XOAgent: ch.XOAgent, Members: ch.Members, Role: ch.Role})
	}
	derived := org.DeriveFromChannels(c.effectiveXOAgent(), names, channels)
	for _, ch := range c.Bindings() {
		if !ch.IsFleetCommand() {
			continue
		}
		for _, m := range ch.Members {
			if m != ch.XOAgent && c.fleetCommandSynthesisMember(ch.XOAgent, m) {
				derived.AddParent(m, ch.XOAgent)
			}
		}
	}
	if err := derived.ValidateStructural(); err != nil {
		return fmt.Errorf("derived org DAG: %w", err)
	}

	// The #942 crown: once an explicit parent exists or every seat has an ID,
	// roster parent edges are canonical. Channels remain a validated
	// routing/synthesis view; the interim org YAML is no longer read. A fully
	// legacy or partially-ID-filled roster continues through the old derived/file
	// path until its one-shot migration completes.
	seatName := make(map[string]string, len(c.Agents))
	for _, agent := range c.Agents {
		if agent.SeatID != "" {
			seatName[agent.SeatID] = agent.Name
		}
	}
	if c.HasStructuredHierarchy() {
		parents := make(map[string][]string, len(c.Agents))
		children := make(map[string][]string, len(c.Agents))
		for _, agent := range c.Agents {
			if agent.Parent == "" {
				continue
			}
			parent := seatName[agent.Parent]
			parents[agent.Name] = []string{parent}
			children[parent] = append(children[parent], agent.Name)
			if channelParents := derived.Parents[agent.Name]; len(channelParents) > 0 && !containsString(channelParents, parent) {
				return fmt.Errorf("roster parent/channel disagreement for agent %q: parent seat %q resolves to %q, channel view reports %v", agent.Name, agent.Parent, parent, channelParents)
			}
		}
		rosterDAG := org.Snapshot(c.effectiveXOAgent(), org.SourceRoster, names,
			func(name string) []string { return parents[name] },
			func(name string) []string { return children[name] },
		)
		for _, agent := range c.Agents {
			node := rosterDAG.Nodes[agent.Name]
			if agent.Coordinator != nil && *agent.Coordinator {
				node.Kind = org.KindCoordinator
			} else {
				node.Kind = org.KindDesk
			}
			if channelID, ok := c.ChannelForAgent(agent.Name); ok {
				node.HomeChannelID = channelID
			}
			rosterDAG.Nodes[agent.Name] = node
		}
		if err := rosterDAG.ValidateStructural(); err != nil {
			return fmt.Errorf("roster org DAG: %w", err)
		}
		c.orgDAG = rosterDAG
		return nil
	}

	orgPath, required, err := org.ResolvePath(rosterPath, opts.OrgFile)
	if err != nil {
		return err
	}
	f, err := org.OpenOptional(orgPath, required)
	if err != nil {
		return err
	}
	if f == nil {
		c.orgDAG = derived
		return nil
	}
	fileDAG, err := org.Compile(f)
	if err != nil {
		return fmt.Errorf("org file %q: %w", orgPath, err)
	}
	if err := org.CheckHomes(f, c.nonFleetHomes()); err != nil {
		return fmt.Errorf("org file %q: %w", orgPath, err)
	}
	if err := org.Agree(fileDAG, derived); err != nil {
		return fmt.Errorf("org file %q: %w", orgPath, err)
	}
	c.orgDAG = fileDAG
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// nonFleetHomes maps agent → non-fleet-command channel ids they own as xo_agent.
func (c *Config) nonFleetHomes() map[string][]string {
	out := map[string][]string{}
	for _, ch := range c.Bindings() {
		if ch.IsFleetCommand() {
			continue
		}
		out[ch.XOAgent] = append(out[ch.XOAgent], ch.ChannelID)
	}
	return out
}

// Org returns the compiled org-truth DAG attached at Load (nil only on a zero Config).
func (c *Config) Org() *org.DAG {
	if c == nil {
		return nil
	}
	return c.orgDAG
}
