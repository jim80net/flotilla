package roster

import "github.com/jim80net/flotilla/internal/org"

// attachOrgDAG snapshots AgentsAbove/AgentsBelow into cfg.orgDAG after synthesis
// validation succeeds (org-truth v1 PR1). Exact parity with synthesis routing.
func (c *Config) attachOrgDAG() {
	names := make([]string, 0, len(c.Agents))
	for _, a := range c.Agents {
		names = append(names, a.Name)
	}
	c.orgDAG = org.Snapshot(c.effectiveXOAgent(), org.SourceDerived, names, c.AgentsAbove, c.AgentsBelow)
}

// Org returns the compiled org-truth DAG attached at Load (nil only on a zero Config).
func (c *Config) Org() *org.DAG {
	if c == nil {
		return nil
	}
	return c.orgDAG
}
