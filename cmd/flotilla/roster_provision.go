package main

import "github.com/jim80net/flotilla/internal/roster"

// provisionedRosterAgent is the shared provisioning seam for workspace init and
// register. Legacy rosters remain loadable everywhere; these provisioning paths
// fill a missing immutable seat_id before continuing, then reload so every later
// step observes the committed roster identity.
func provisionedRosterAgent(path, name string) (*roster.Config, roster.Agent, error) {
	cfg, err := roster.Load(path)
	if err != nil {
		return nil, roster.Agent{}, err
	}
	agent, err := cfg.Agent(name)
	if err != nil {
		return nil, roster.Agent{}, err
	}
	if agent.SeatID != "" {
		return cfg, agent, nil
	}
	if _, _, err := roster.EnsureSeatID(path, name); err != nil {
		return nil, roster.Agent{}, err
	}
	cfg, err = roster.Load(path)
	if err != nil {
		return nil, roster.Agent{}, err
	}
	agent, err = cfg.Agent(name)
	if err != nil {
		return nil, roster.Agent{}, err
	}
	return cfg, agent, nil
}
