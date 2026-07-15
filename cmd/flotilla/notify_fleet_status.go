package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/jim80net/flotilla/internal/dash"
	"github.com/jim80net/flotilla/internal/loopposture"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/status"
	"github.com/jim80net/flotilla/internal/watch"
)

// withFleetStatus appends a compressed fleet posture block when requested (#625).
// Pure w.r.t. the block loader so tests can inject fixtures without Discord I/O.
func withFleetStatus(body string, enabled bool, loadBlock func() (string, error)) string {
	if !enabled {
		return body
	}
	if status.HasFleetStatusHeader(body) {
		return body
	}
	block, err := loadBlock()
	if err != nil || block == "" {
		return status.AppendFleetStatus(body, status.UnavailableBlock())
	}
	return status.AppendFleetStatus(body, block)
}

// loadFleetStatusBlock reads the same artifacts as `flotilla status --json` and
// compresses them. skipFrom is the --from agent (self + adjutant omitted from lists).
func loadFleetStatusBlock(rosterPath, skipFrom string) (string, error) {
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		return "", err
	}
	rosterDir := filepath.Dir(rosterPath)
	snapshotPath := filepath.Join(rosterDir, "flotilla-detector-state.json")
	xo := cfg.XOAgent
	if xo == "" && len(cfg.Agents) > 0 {
		xo = cfg.Agents[0].Name
	}
	snap, snapOK := watch.LoadSnapshot(snapshotPath)
	now := time.Now()
	snapFresh := false
	if snapOK {
		if age, ok := fileAge(snapshotPath, now); ok {
			snapFresh = age <= dash.FreshnessThreshold(cfg.HeartbeatDur())
		}
	}
	loopByAgent := loopposture.LoadFleetEvidence(cfg, xo, rosterDir, snap, snapOK, snapFresh)
	generatedAt := ""
	if fi, err := os.Stat(snapshotPath); err == nil {
		generatedAt = fi.ModTime().UTC().Format(time.RFC3339)
	}
	doc := buildStatusJSON(cfg, xo, generatedAt, snap, loopByAgent)
	sdoc := status.Doc{
		GeneratedAt: doc.GeneratedAt,
		XO:          doc.XO,
		Agents:      make([]status.Agent, 0, len(doc.Agents)),
	}
	for _, a := range doc.Agents {
		sdoc.Agents = append(sdoc.Agents, status.Agent{
			Name: a.Name, Role: a.Role, State: a.State,
			LoopPosture: a.LoopPosture, RawLoopPosture: a.RawLoopPosture,
		})
	}
	adj := ""
	if skipFrom != "" {
		adj = cfg.AdjutantFor(skipFrom)
	}
	return status.CompressBlock(sdoc, status.CompressOptions{
		Skip: status.SkipSet(skipFrom, adj),
	}), nil
}
