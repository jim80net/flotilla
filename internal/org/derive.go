package org

// Channel is a federation binding input for documentation and pure tests.
// It mirrors roster.Channel without importing internal/roster (avoids cycles).
type Channel struct {
	ChannelID string
	XOAgent   string
	Members   []string
	Role      string
}

// IsFleetCommand reports whether Role is the broadcast/command channel.
func (ch Channel) IsFleetCommand() bool { return ch.Role == "fleet-command" }

// DeriveFromChannels is the channel-compat entry point named in the org-truth
// design. PR1 implements exact synthesis parity by Snapshot'ing a loaded
// roster's AgentsAbove/AgentsBelow (see roster.attachOrgDAG) rather than
// re-encoding fleet-command coordinator rules here — those rules live in
// internal/roster/synthesis.go and must stay single-sourced.
//
// This helper builds a *minimal* parent map from non-fleet-command home channels
// only (XO → members as "member reports_to XO" is WRONG for the home-channel
// convention: home channel members list the *parent*). Correct home-channel
// edge: for channel with xo=A members=[P], A reports_to P.
//
// Prefer Snapshot after roster.Load for production parity. This function exists
// so the DeriveFromChannels API is real and unit-testable for the home-channel
// star without a full roster load.
func DeriveFromChannels(root string, agentNames []string, channels []Channel) *DAG {
	parents := map[string][]string{}
	for _, name := range agentNames {
		parents[name] = nil
	}
	// ownsAny: agent is xo_agent of any channel (including fleet-command) — not a pure leaf.
	ownsAny := map[string]bool{}
	for _, ch := range channels {
		ownsAny[ch.XOAgent] = true
		if ch.IsFleetCommand() {
			continue
		}
	}
	// One canonical reports-to interpretation. A member that owns a channel is a
	// parent observing its child's home channel; a member-only leaf reports to the
	// channel XO (legacy star). This distinction prevents the two shapes from
	// producing opposite edges or a synthetic two-node cycle.
	for _, ch := range channels {
		if ch.IsFleetCommand() {
			continue
		}
		for _, m := range ch.Members {
			if m == ch.XOAgent {
				continue
			}
			if ownsAny[m] && ch.XOAgent != root {
				parents[ch.XOAgent] = appendUnique(parents[ch.XOAgent], m)
			} else {
				// The configured fleet root is parentless. A root-owned group that
				// lists another channel owner is an observation/down-list overlap,
				// not evidence that the root reports to that owner.
				if ch.XOAgent == root && ownsAny[m] {
					continue
				}
				parents[m] = appendUnique(parents[m], ch.XOAgent)
			}
		}
	}
	children := map[string][]string{}
	for child, ps := range parents {
		for _, p := range ps {
			children[p] = appendUnique(children[p], child)
		}
	}
	return Snapshot(root, SourceDerived, agentNames, func(a string) []string {
		return parents[a]
	}, func(a string) []string {
		return children[a]
	})
}

func appendUnique(ss []string, x string) []string {
	for _, s := range ss {
		if s == x {
			return ss
		}
	}
	return append(ss, x)
}
