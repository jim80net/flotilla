package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/dash"
	"github.com/jim80net/flotilla/internal/harnessquality"
	"github.com/jim80net/flotilla/internal/loopposture"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/utilization"
	"github.com/jim80net/flotilla/internal/watch"
)

// cmdStatus prints a one-line-per-desk view of the fleet's last-known state. It
// reads existing durable artifacts — the detector snapshot (per-desk assessed state +
// the XO's settled flag), the XO liveness ack file, and administrative CLOSE-OUT
// markers — so it starts no daemon, resolves no panes, and writes no new state.
//
// The states come from a SNAPSHOT (the detector's view as of its last tick), NOT
// a live pane probe, so status always reports the snapshot's age: a stale read
// must never be mistaken for a live one. Without a readable snapshot (no `watch`
// running, or change_detector off) it still lists the roster, with every desk as
// "unknown".
func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	snapshotPath := fs.String("snapshot-file", os.Getenv("FLOTILLA_SNAPSHOT_FILE"), "change-detector snapshot file (default <roster-dir>/flotilla-detector-state.json)")
	ackPath := fs.String("ack-file", os.Getenv("FLOTILLA_ACK_FILE"), "XO liveness ack file (default <roster-dir>/flotilla-xo-alive)")
	asJSON := fs.Bool("json", false, "emit machine-readable JSON instead of the text table")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := roster.Load(*rosterPath)
	if err != nil {
		return err
	}
	// Mirror watch's default-path resolution EXACTLY so status reads precisely
	// what watch writes (same env vars, same <roster-dir>/… fallbacks).
	rosterDir := filepath.Dir(*rosterPath)
	if *snapshotPath == "" {
		*snapshotPath = filepath.Join(rosterDir, "flotilla-detector-state.json")
	}
	// The XO is the explicit xo_agent, else the first agent (watch's own rule).
	// roster.Load guarantees a non-empty Agents slice, so [0] is safe.
	xo := cfg.XOAgent
	if xo == "" {
		xo = cfg.Agents[0].Name
	}
	*ackPath = roster.ResolveLayerClockPath(rosterDir, xo, *ackPath, "flotilla-xo-alive", "alive")

	snap, snapOK := watch.LoadSnapshot(*snapshotPath)
	now := time.Now()
	// Snapshot freshness for loop_posture: same 3× heartbeat order as dash.
	snapFresh := false
	if snapOK {
		if age, ok := fileAge(*snapshotPath, now); ok {
			snapFresh = age <= dash.FreshnessThreshold(cfg.HeartbeatDur())
		}
	}
	loopByAgent := loopposture.LoadFleetEvidence(cfg, xo, rosterDir, snap, snapOK, snapFresh)
	dispositions := statusSeatDispositions(rosterDir, cfg)
	if *asJSON {
		// generated_at is the snapshot's mtime (when watch last wrote it) — the
		// honest "as of" for the states below. Empty when there is no snapshot.
		generatedAt := ""
		if fi, statErr := os.Stat(*snapshotPath); statErr == nil {
			generatedAt = fi.ModTime().UTC().Format(time.RFC3339)
		}
		doc := buildStatusJSON(cfg, xo, generatedAt, snap, loopByAgent, dispositions)
		doc.Quality = harnessquality.LoadSummary(filepath.Dir(*rosterPath), now)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(doc)
	}
	writeStatusWithDispositions(os.Stdout, cfg, xo, *snapshotPath, *ackPath, snap, snapOK, now, loopByAgent, dispositions)
	writeQualitySummary(os.Stdout, harnessquality.LoadSummary(filepath.Dir(*rosterPath), now))
	return nil
}

// statusDoc is the `--json` shape. It is deliberately a SUPERSET of the
// landing-page widget contract — an `agents` array plus a `generated_at` stamp —
// so the public sample status.json can be a real `flotilla status --json` run
// against a demo roster rather than hand-authored data. #524 adds loop_posture
// beside pane state; older consumers ignore unknown fields.
type statusDoc struct {
	GeneratedAt      string                 `json:"generated_at"`
	GeneratedAtScope string                 `json:"generated_at_scope,omitempty"`
	XO               string                 `json:"xo,omitempty"`
	Utilization      utilization.Summary    `json:"utilization"`
	Quality          harnessquality.Summary `json:"harness_quality"`
	Agents           []statusItem           `json:"agents"`
}

func writeQualitySummary(out io.Writer, summary harnessquality.Summary) {
	if summary.State != "available" {
		fmt.Fprintf(out, "harness quality — unavailable (%s)\n", summary.Diagnostic)
		return
	}
	fmt.Fprintf(out, "harness quality — events:%d classified:%.1f%% gates:%d bounce:%.1f%% completions:%d rework:%.1f%%\n",
		summary.TotalEvents, summary.TaggingCoveragePercent, summary.GateEvents, summary.BounceRatePercent,
		summary.CompletionEvents, summary.ReworkRatePercent)
}

type statusItem struct {
	Name           string                  `json:"name"`
	Role           string                  `json:"role,omitempty"`             // "hub" for the XO, else omitted
	Surface        string                  `json:"surface,omitempty"`          // effective surface driver
	State          string                  `json:"state"`                      // pane / surface.State label
	LoopPosture    string                  `json:"loop_posture,omitempty"`     // operator-facing #524 vocabulary
	RawLoopPosture string                  `json:"raw_loop_posture,omitempty"` // retained when display normalization differs
	QueueState     string                  `json:"queue_state"`                // empty | has-work | unknown
	Usage          *watch.UsageObservation `json:"usage,omitempty"`
}

// buildStatusJSON assembles the --json document. Pure (no I/O) so it is
// unit-testable with an in-memory snapshot; cmdStatus supplies generated_at
// (the snapshot's mtime), the loaded snapshot, and pre-derived loop evidence.
func buildStatusJSON(cfg *roster.Config, xo, generatedAt string, snap watch.Snapshot, loopByAgent map[string]loopposture.Evidence, dispositions map[string]statusSeatDisposition) statusDoc {
	doc := statusDoc{GeneratedAt: generatedAt, XO: xo, Agents: make([]statusItem, 0, len(cfg.Agents))}
	if generatedAt != "" {
		doc.GeneratedAtScope = "detector_snapshot_only"
	}
	for _, a := range cfg.Agents {
		evidence, evidenceOK := loopByAgent[a.Name]
		rawPosture := deriveAgentPosture(a.Name, snap, loopByAgent)
		displayPosture := loopposture.OperatorDisplay(rawPosture)
		state := deskStateLabel(snap, a.Name)
		queueState := utilization.QueueState(evidenceOK && evidence.BacklogKnown, evidence.UnblockedN)
		switch dispositions[a.Name] {
		case statusSeatClosedOut:
			state = "closed-out"
			displayPosture = "unavailable"
			queueState = utilization.QueueUnknown
		case statusSeatUnknown:
			state = "unknown"
			displayPosture = "unavailable"
			queueState = utilization.QueueUnknown
		}
		item := statusItem{
			Name:        a.Name,
			Surface:     effectiveSurface(agentSurface(cfg, a.Name)),
			State:       state,
			LoopPosture: string(displayPosture),
			QueueState:  queueState,
		}
		if dispositions[a.Name] == statusSeatOpen && rawPosture != displayPosture {
			item.RawLoopPosture = string(rawPosture)
		}
		if usage, ok := snap.Usage[a.Name]; ok {
			item.Usage = &usage
		}
		if a.Name == xo {
			item.Role = "hub"
		}
		doc.Agents = append(doc.Agents, item)
	}
	doc.Utilization = summarizeStatusItems(doc.Agents)
	return doc
}

func summarizeStatusItems(items []statusItem) utilization.Summary {
	agents := make([]utilization.Agent, 0, len(items))
	for _, item := range items {
		agents = append(agents, utilization.Agent{
			State: item.State, LoopPosture: item.LoopPosture,
			RawLoopPosture: item.RawLoopPosture, QueueState: item.QueueState,
		})
	}
	return utilization.Build(agents)
}

// effectiveSurface resolves an agent's surface name for display: empty means
// the default driver, which the docs name "claude-code". Callers pass the
// overlay-first configured surface (agentSurface), not the raw roster field.
func effectiveSurface(s string) string {
	if s == "" {
		return "claude-code"
	}
	return s
}

// writeStatus renders the report. It is split from cmdStatus (which does flag +
// file I/O) so the formatting is unit-testable with an in-memory snapshot and a
// pinned clock — no roster file, no daemon, no real time.
func writeStatus(out io.Writer, cfg *roster.Config, xo, snapshotPath, ackPath string, snap watch.Snapshot, snapOK bool, now time.Time, loopByAgent map[string]loopposture.Evidence) {
	writeStatusWithDispositions(out, cfg, xo, snapshotPath, ackPath, snap, snapOK, now, loopByAgent, nil)
}

func writeStatusWithDispositions(out io.Writer, cfg *roster.Config, xo, snapshotPath, ackPath string, snap watch.Snapshot, snapOK bool, now time.Time, loopByAgent map[string]loopposture.Evidence, dispositions map[string]statusSeatDisposition) {
	// Freshness header — the desk states below are as of the snapshot's mtime,
	// not a live probe. Always surface that (or its absence).
	if snapOK {
		if age, ok := fileAge(snapshotPath, now); ok {
			fmt.Fprintf(out, "flotilla status — pane states as of %s ago (snapshot); administrative dispositions read now (%s)\n", humanizeAge(age), snapshotPath)
		} else {
			fmt.Fprintf(out, "flotilla status (%s)\n", snapshotPath)
		}
	} else {
		fmt.Fprintf(out, "flotilla status — no readable detector snapshot at %s\n", snapshotPath)
		fmt.Fprintln(out, "  (run `flotilla watch` with change_detector: true to populate it; desks shown as unknown)")
	}
	utilSummary := buildStatusJSON(cfg, xo, "", snap, loopByAgent, dispositions).Utilization
	fmt.Fprintf(out, "Fleet — %s\n", utilization.Line(utilSummary))
	if read := utilization.WallRead(utilSummary); read != "" {
		fmt.Fprintf(out, "Next — %s\n", read)
	}
	fmt.Fprintln(out)

	// XO liveness line: who, last-ack age, and settled/active (settled only when
	// the snapshot is readable — without it we can't assert the flag).
	ackDesc := "never acked"
	if age, ok := fileAge(ackPath, now); ok {
		ackDesc = "last ack " + humanizeAge(age) + " ago"
	}
	if snapOK {
		fmt.Fprintf(out, "XO %s · %s · %s\n\n", xo, ackDesc, settledDesc(snap.XOSettled))
	} else {
		fmt.Fprintf(out, "XO %s · %s\n\n", xo, ackDesc)
	}

	// One aligned line per roster desk: name, pane state, loop_posture, usage, (XO).
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, a := range cfg.Agents {
		marker := ""
		if a.Name == xo {
			marker = "(XO)"
		}
		posture := loopposture.OperatorDisplay(deriveAgentPosture(a.Name, snap, loopByAgent))
		state := deskStateLabel(snap, a.Name)
		switch dispositions[a.Name] {
		case statusSeatClosedOut:
			state = "closed-out"
			posture = "unavailable"
		case statusSeatUnknown:
			state = "unknown"
			posture = "unavailable"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", a.Name, state, posture, usageLabel(snap, a.Name, now), marker)
	}
	_ = w.Flush()
}

type statusSeatDisposition uint8

const (
	statusSeatOpen statusSeatDisposition = iota
	statusSeatClosedOut
	statusSeatUnknown
)

// statusSeatDispositions resolves administrative truth without collapsing read failure
// into a factual close-out claim. Unknown remains unavailable for dispatch, while the
// displayed state honestly says that the disposition could not be established.
func statusSeatDispositions(rosterDir string, cfg *roster.Config) map[string]statusSeatDisposition {
	dispositions := make(map[string]statusSeatDisposition)
	if cfg == nil {
		return dispositions
	}
	for _, agent := range cfg.Agents {
		if closed, present := cfg.RecipientClosedOutDisposition(agent.Name); present {
			if closed {
				dispositions[agent.Name] = statusSeatClosedOut
			} else {
				dispositions[agent.Name] = statusSeatOpen
			}
			continue
		}
		exists, _, err := closeOutDocumentState(rosterDir, agent.Name)
		switch {
		case err != nil:
			dispositions[agent.Name] = statusSeatUnknown
		case exists:
			dispositions[agent.Name] = statusSeatClosedOut
		default:
			dispositions[agent.Name] = statusSeatOpen
		}
	}
	return dispositions
}

func usageLabel(snap watch.Snapshot, name string, now time.Time) string {
	observation, ok := snap.Usage[name]
	if !ok {
		return "—"
	}
	label := fmt.Sprintf("%d%% %s", observation.RemainingPercent, observation.Window)
	if !observation.StaleAfter.IsZero() && now.After(observation.StaleAfter) {
		label += " stale"
	}
	return label
}

func deriveAgentPosture(name string, snap watch.Snapshot, loopByAgent map[string]loopposture.Evidence) loopposture.Posture {
	if ev, ok := loopByAgent[name]; ok {
		return loopposture.Derive(ev)
	}
	// No evidence map: pane-only derivation (backlog unknown ⇒ cannot strict-park).
	return loopposture.Derive(loopposture.FromSnapshot(snap, name, false, false, true, backlog.Status{}))
}

// deskStateLabel renders a desk's snapshot state with the operator-facing
// vocabulary. StateShell is shown as "crashed" — the docs' established term for
// "the agent process is gone, the pane dropped to a bare shell" (the detector's
// own logs call it "shell"; the operator reads "crashed"). A desk absent from
// the snapshot (added since the last tick, or no snapshot at all — DeskStates is
// then nil, which reads as a miss) is "unknown".
func deskStateLabel(snap watch.Snapshot, name string) string {
	st, ok := snap.DeskStates[name]
	if !ok {
		return "unknown"
	}
	if st == surface.StateShell {
		return "crashed"
	}
	return st.String()
}

// settledDesc renders the XO's snapshot settled flag: "settled (idle)" when the
// XO has reported idle (or hit the self-continuation cap) and will not be
// self-woken until an external change or an operator message; "active" otherwise.
func settledDesc(settled bool) string {
	if settled {
		return "settled (idle)"
	}
	return "active"
}

// fileAge returns how long ago path was last modified, relative to now. ok=false
// when the file does not exist or cannot be stat'd.
func fileAge(path string, now time.Time) (time.Duration, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return now.Sub(fi.ModTime()), true
}

// humanizeAge renders a duration as a compact age (rounded to the second):
// "9s", "3m12s", "1h4m", "2d3h". A negative input (clock skew — a file stamped
// in the future) clamps to "0s" rather than printing a misleading negative.
func humanizeAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}
