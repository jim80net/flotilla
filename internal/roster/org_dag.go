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
