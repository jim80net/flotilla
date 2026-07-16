package roster

import (
	"fmt"

	"github.com/jim80net/flotilla/internal/org"
)

// Synthesis routing — the visibility-synthesis (B2) read/owed/post derivations over the
// federation membership graph, plus the load-time acyclicity assertion. These are pure
// read-only derivations over Bindings(); none mutates a binding's Members slice (the
// read-only-slice contract documented on Bindings).
//
// THE FLEET-COMMAND OVERLOAD (the implement-gate P0). `members[]` carries two opposite
// meanings. In a per-XO HOME channel, members is the agent's PARENT up-link (e.g.
// the data desk's channel lists [xo]). In the fleet-command BROADCAST channel
// it is the meta-XO's full command DOWN-list (every agent it can address). Read as a
// synthesis edge, the broadcast channel inverts the hierarchy — a leaf desk that is a
// member of it would treat the broadcaster (the meta-XO) as a subordinate, and the graph
// cycles. So a role=="fleet-command" channel contributes ZERO synthesis edges: it is
// excluded from AgentsBelow, AgentsAbove, and the DAG. It REMAINS a POST target
// (OwnedChannels includes it) — the meta-XO posts its Tier-3 synthesis INTO #c2.

// IsFleetCommand reports whether a channel is the fleet-command/broadcast channel, whose
// members are command targets rather than synthesis parents (see the package note above).
func (ch Channel) IsFleetCommand() bool { return ch.Role == "fleet-command" }

// OwnedChannels returns every channel id an agent is the XO of — the synthesis POST
// target. It generalizes ChannelForXO (which returns only the first/home channel) to the
// multi-hub case, and INCLUDES any fleet-command channel the agent owns (the meta-XO
// posts its Tier-3 synthesis into the fleet-command channel it owns). Order-stable
// (binding order); empty for an agent that owns no channel.
func (c *Config) OwnedChannels(agent string) []string {
	var out []string
	for _, ch := range c.Bindings() {
		if ch.XOAgent == agent {
			out = append(out, ch.ChannelID)
		}
	}
	return out
}

// AgentsBelow returns the tier BELOW an agent — its synthesis READ set.
//
// Org-truth v1 PR3: after Load attaches orgDAG, prefer the compiled DAG's Children
// so visibility-synthesis and channel membership cannot diverge. During attachOrgDAG
// (orgDAG still nil) and for agents not present in a file-sourced DAG, fall back to
// the channel-membership rules below.
//
// Channel path: for every NON-fleet-command channel whose members list the agent, that
// channel's XO is a subordinate. Two exclusions, both load-bearing: the channel's own
// XO (`!= agent` — read strictly below, never your own channel: the self-loop guard) and
// fleet-command channels (a broadcast channel's members are command targets, not
// subordinates — without this a leaf desk would "synthesize" the meta-XO and the graph
// would cycle). De-duplicated and order-stable.
//
// Fleet-command OWNER supplement: a seat provisioned ONLY into the broadcast channel
// (no home channel listing the owner as parent yet) is still a synthesis read target
// for the owner — e.g. a newly added flotilla-backlog-xo under cos. Execution desks
// (supervisor-observer home channels) and project-XOs already reachable via the
// standard home-channel path are excluded.
func (c *Config) AgentsBelow(agent string) []string {
	if out, ok := c.orgChildren(agent); ok {
		return out
	}
	return c.agentsBelowFromChannels(agent)
}

// AgentsAbove returns the synthesizing PARENTS of an agent — the agents OWED a synthesis
// when this agent finishes.
//
// After Load, the compiled DAG is canonical for both file and channel sources:
// Parents are reporting parents (single primary for file, possibly many derived).
// During attachOrgDAG and for agents absent from a file-sourced DAG, fall back to
// channel membership.
//
// Channel path: members (minus self) of the NON-fleet-command channels the agent OWNS,
// plus (for coordinator members of a fleet-command channel) the broadcast channel's
// owner. Exact relational inverse of AgentsBelow on the channel path
// (C ∈ AgentsBelow(P) ⟺ P ∈ AgentsAbove(C)).
func (c *Config) AgentsAbove(agent string) []string {
	if out, ok := c.orgParents(agent); ok {
		return out
	}
	return c.agentsAboveFromChannels(agent)
}

// orgParents returns DAG parents when orgDAG covers this agent.
// ok=false means fall back to channel derivation (orgDAG nil, or file DAG without this node).
func (c *Config) orgParents(agent string) ([]string, bool) {
	d := c.orgDAG
	if d == nil {
		return nil, false
	}
	if _, in := d.Nodes[agent]; !in && d.Source == org.SourceFile {
		return nil, false
	}
	return cloneAgentList(d.Parents[agent]), true
}

// orgChildren returns DAG children when orgDAG covers this agent.
func (c *Config) orgChildren(agent string) ([]string, bool) {
	d := c.orgDAG
	if d == nil {
		return nil, false
	}
	if _, in := d.Nodes[agent]; !in && d.Source == org.SourceFile {
		return nil, false
	}
	return cloneAgentList(d.Children[agent]), true
}

func cloneAgentList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func (c *Config) agentsBelowFromChannels(agent string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ch := range c.Bindings() {
		if ch.IsFleetCommand() || ch.XOAgent == agent {
			continue
		}
		if !memberOf(ch.Members, agent) || seen[ch.XOAgent] {
			continue
		}
		seen[ch.XOAgent] = true
		out = append(out, ch.XOAgent)
	}
	for _, ch := range c.Bindings() {
		if !ch.IsFleetCommand() || ch.XOAgent != agent {
			continue
		}
		for _, m := range ch.Members {
			if m == agent || seen[m] || !c.fleetCommandSynthesisMember(agent, m) {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

func (c *Config) agentsAboveFromChannels(agent string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ch := range c.Bindings() {
		if ch.IsFleetCommand() || ch.XOAgent != agent {
			continue
		}
		for _, m := range ch.Members {
			if m == agent || seen[m] {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
	}
	for _, ch := range c.Bindings() {
		if !ch.IsFleetCommand() || !memberOf(ch.Members, agent) {
			continue
		}
		owner := ch.XOAgent
		if owner == agent || seen[owner] || !c.fleetCommandSynthesisMember(owner, agent) {
			continue
		}
		seen[owner] = true
		out = append(out, owner)
	}
	return out
}

// fleetCommandSynthesisMember reports whether member is a fleet-command broadcast
// direct report of owner for synthesis — an explicit coordinator seat registered only
// in the broadcast channel (no project-home channel listing owner as parent, and not
// an execution desk with a supervisor-observer home channel). coordinator:false opts
// out even when the seat is broadcast-only (#491); coordinator:true or IsCoordinator
// opts in — broadcast membership alone is never sufficient.
func (c *Config) fleetCommandSynthesisMember(owner, member string) bool {
	if member == owner {
		return false
	}
	if a, err := c.Agent(member); err == nil && a.Coordinator != nil && !*a.Coordinator {
		return false
	}
	if a, err := c.Agent(member); err == nil && a.Coordinator != nil && *a.Coordinator {
		// explicit coordinator:true — fall through to topology checks
	} else if !c.IsCoordinator(member) {
		return false
	}
	for _, ch := range c.Bindings() {
		if ch.IsFleetCommand() || ch.XOAgent == owner {
			continue
		}
		if memberOf(ch.Members, owner) && ch.XOAgent == member {
			return false
		}
	}
	for _, ch := range c.Bindings() {
		if ch.IsFleetCommand() || ch.XOAgent != member {
			continue
		}
		if c.channelIsSupervisorObserverHome(ch, nil) {
			return false
		}
	}
	return true
}

// assertSynthesisAcyclic returns an error if the synthesis-edge graph contains a cycle, so
// a federation that would form a synthesis feedback loop refuses to start (fail-closed,
// like every other roster invariant). Edges run ch.XOAgent → m for each member m of each
// binding, EXCLUDING (1) self-edges (m == ch.XOAgent — a home-channel self-membership is
// not a cycle) and (2) fleet-command channels (a broadcast channel contributes no
// synthesis edges). A genuine cycle is a mutual membership between two distinct
// non-fleet-command channels. Runs once at load, not on the synthesis hot path.
//
// Error text names both agents on the back-edge and both channel ids when recoverable
// (org-truth v1 PR1).
func (c *Config) assertSynthesisAcyclic() error {
	type edge struct {
		to, ch string
	}
	adj := map[string][]edge{}
	for _, ch := range c.Bindings() {
		if ch.IsFleetCommand() {
			continue
		}
		for _, m := range ch.Members {
			if m != ch.XOAgent {
				adj[ch.XOAgent] = append(adj[ch.XOAgent], edge{to: m, ch: ch.ChannelID})
			}
		}
	}
	const white, gray, black = 0, 1, 2
	color := map[string]int{}
	// tree edge into node: child → (parentAgent, channel that produced parent→child)
	treeFrom := map[string]string{}
	treeCh := map[string]string{}
	var errCycle error
	var visit func(string) bool
	visit = func(u string) bool {
		color[u] = gray
		for _, e := range adj[u] {
			v := e.to
			switch color[v] {
			case gray:
				// Back-edge u→v closes a cycle. treeCh[u] is how DFS reached u (the
				// other half of a 2-cycle when v is the DFS root).
				chClose := e.ch
				chTree := treeCh[u]
				if chTree == "" {
					chTree = treeCh[v]
				}
				if chTree != "" && chTree != chClose {
					errCycle = fmt.Errorf("synthesis routing would cycle: agents %q and %q (channels %q ↔ %q)", u, v, chClose, chTree)
				} else if chClose != "" {
					errCycle = fmt.Errorf("synthesis routing would cycle: agents %q and %q (channel %q)", u, v, chClose)
				} else {
					errCycle = fmt.Errorf("synthesis routing would cycle: agent %q is reachable from itself through the channel-membership graph (a mutual membership between two distinct non-fleet-command channels)", v)
				}
				_ = treeFrom
				return true
			case white:
				treeFrom[v] = u
				treeCh[v] = e.ch
				if visit(v) {
					return true
				}
			}
		}
		color[u] = black
		return false
	}
	for _, ch := range c.Bindings() {
		if color[ch.XOAgent] == white && visit(ch.XOAgent) {
			return errCycle
		}
	}
	return nil
}

// OwningXO resolves the XO that OWNS a desk — the cap-escalation target for the recursive
// desk-heartbeat (#183 §8e: a wedged desk surfaces LOUDLY to its owning XO). It is topology-robust:
//
//  0. Org-truth v1 PR3: when the compiled org DAG names a PrimaryParent for the agent, that
//     parent is the owner (file DAG single reports_to, or derived primary).
//  1. Federated home-channel shape — a desk that OWNS a (non-fleet-command) home channel naming its
//     parent: AgentsAbove(agent) resolves the parent (a leaf → its project-XO, a project-XO → the
//     meta-XO). The first parent is the owner.
//  2. Legacy star — a leaf owns no channel (AgentsAbove is EMPTY); the owner is the XO of the
//     (non-fleet-command) channel the desk is a MEMBER of.
//  3. Fallback — neither resolves (the root, or an unknown agent): the supplied primaryXO.
//
// The fleet-command broadcast channel is excluded from the membership scan (its members are command
// targets, not an ownership relation — the same load-bearing exclusion AgentsBelow/AgentsAbove make).
// Read-only over Bindings(); never mutates a Members slice.
func (c *Config) OwningXO(agent, primaryXO string) string {
	// Prefer org DAG primary parent when the agent is covered (file or derived).
	if d := c.orgDAG; d != nil {
		if _, in := d.Nodes[agent]; in || d.Source != org.SourceFile {
			if p := d.PrimaryParent(agent); p != "" {
				return p
			}
		}
	}
	if parents := c.AgentsAbove(agent); len(parents) > 0 {
		return parents[0]
	}
	// The membership-scan fallback is for the LEGACY STAR ONLY (a leaf that owns no channel). In the
	// federated home-channel shape a PARENT's home channel lists its child-XOs as members, so scanning
	// memberships for an agent that DOES own a channel would invert the hierarchy (e.g. the meta is a
	// member of each project-XO's home channel → it would resolve to a project-XO). An agent with an
	// empty AgentsAbove that ALSO owns a channel is the federated ROOT → the primaryXO fallback.
	if len(c.OwnedChannels(agent)) == 0 {
		for _, ch := range c.Bindings() {
			if ch.IsFleetCommand() || ch.XOAgent == agent {
				continue
			}
			if memberOf(ch.Members, agent) {
				return ch.XOAgent
			}
		}
	}
	return primaryXO
}

// memberOf reports whether name is in members.
func memberOf(members []string, name string) bool {
	for _, m := range members {
		if m == name {
			return true
		}
	}
	return false
}
