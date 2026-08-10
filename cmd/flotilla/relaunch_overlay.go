package main

import (
	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/workspace"
)

// reconcileRelaunchOverlay derives routing state from the pane after a confirmed
// relaunch. Launch selection is only an expectation: every mapped live command
// is persisted, even when it differs from that expectation. Only an unreadable
// or unmapped observation clears the overlay.
func reconcileRelaunchOverlay(agent, target, selectedSlot string, chain launch.Recipe, defaultSurface string, metadata workspace.ActiveOverlay, paneCommand func(string) (string, error)) error {
	cmd, err := paneCommand(target)
	if err != nil {
		return workspace.ClearActiveOverlay(agent)
	}
	liveSurface, ok := surface.SurfaceFromPaneCommand(cmd)
	if !ok {
		return workspace.ClearActiveOverlay(agent)
	}
	var selected *launch.ResolvedSlot
	matches := make([]launch.ResolvedSlot, 0, 1)
	for _, candidate := range chain.Slots() {
		candidateSurface := candidate.Surface
		if candidate.Name == workspace.SlotPrimary && candidateSurface == "" {
			candidateSurface = defaultSurface
		}
		if candidate.Name == selectedSlot {
			copy := candidate
			copy.Surface = candidateSurface
			selected = &copy
		}
		if candidateSurface == liveSurface {
			candidate.Surface = candidateSurface
			matches = append(matches, candidate)
		}
	}
	var observed launch.ResolvedSlot
	switch {
	case selected != nil && selected.Surface == liveSurface:
		observed = *selected
	case len(matches) == 1:
		observed = matches[0]
	default:
		return workspace.ClearActiveOverlay(agent)
	}
	metadata.Slot = observed.Name
	metadata.Surface = liveSurface
	metadata.Provider = observed.Provider
	metadata.SubscriptionID = observed.SubscriptionID
	return workspace.WriteActiveOverlay(agent, metadata)
}
