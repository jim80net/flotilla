package main

import (
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/workspace"
)

// reconcileRelaunchOverlay derives routing state from the pane after a confirmed
// relaunch. Launch selection is only an expectation: an unreadable, unknown, or
// different live command clears the overlay so passive routing can apply its
// live-wins policy instead of persisting unverified intent.
func reconcileRelaunchOverlay(agent, target, slot, selectedSurface string, metadata workspace.ActiveOverlay, paneCommand func(string) (string, error)) error {
	cmd, err := paneCommand(target)
	if err != nil {
		return workspace.ClearActiveOverlay(agent)
	}
	liveSurface, ok := surface.SurfaceFromPaneCommand(cmd)
	if !ok || liveSurface != selectedSurface {
		return workspace.ClearActiveOverlay(agent)
	}
	if slot != workspace.SlotPrimary && metadata.SwitchToken != "" {
		metadata.Slot = slot
		metadata.Surface = liveSurface
		return workspace.WriteActiveOverlay(agent, metadata)
	}
	return workspace.ReconcileActiveOverlay(agent, slot, liveSurface)
}
