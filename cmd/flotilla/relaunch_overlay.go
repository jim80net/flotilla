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
func reconcileRelaunchOverlay(agent, target string, chain launch.Recipe, defaultSurface string, metadata workspace.ActiveOverlay, paneCommand func(string) (string, error)) error {
	cmd, err := paneCommand(target)
	if err != nil {
		return workspace.ClearActiveOverlay(agent)
	}
	liveSurface, ok := surface.SurfaceFromPaneCommand(cmd)
	if !ok {
		return workspace.ClearActiveOverlay(agent)
	}
	liveSlot := workspace.SlotObservedUnslotted
	for _, candidate := range chain.Slots() {
		candidateSurface := candidate.Surface
		if candidate.Name == workspace.SlotPrimary && candidateSurface == "" {
			candidateSurface = defaultSurface
		}
		if candidateSurface == liveSurface {
			liveSlot = candidate.Name
			break
		}
	}
	if metadata.SwitchToken != "" {
		metadata.Slot = liveSlot
		metadata.Surface = liveSurface
		return workspace.WriteActiveOverlay(agent, metadata)
	}
	return workspace.ReconcileActiveOverlay(agent, liveSlot, liveSurface)
}
