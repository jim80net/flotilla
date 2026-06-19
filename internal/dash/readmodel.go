// Package dash is the read model + local web server behind `flotilla dash` — an
// OPTIONAL, pluggable web interface over the artifacts `flotilla watch` already
// writes. Phase 1 is a PURE READER: it derives every datum from the detector
// snapshot, the XO liveness ack file, the roster, the CoS ledger, and the
// backlog file. It runs no pane prober, writes no fleet state, and starts no
// daemon — `flotilla watch` remains the single writer of fleet state, so the
// dash can never diverge from or double-probe the fleet (design §2).
//
// This file is the read model: PURE functions over already-loaded artifacts
// (the HTTP layer does the file I/O and supplies the loaded values + a clock).
// That mirrors cmd/flotilla/status.go's testable buildStatusJSON/writeStatus
// split — every builder here is unit-testable with in-memory inputs and a
// pinned clock, with no roster file, no daemon, and no real time.
package dash

import (
	"strconv"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
)

// defaultHeartbeat is the freshness-threshold fallback when the roster declares
// no heartbeat cadence (heartbeat disabled). The detector snapshot only exists
// when change_detector ran, which requires a positive heartbeat_interval, so a
// real snapshot always has a real cadence; this fallback only governs the age
// banner for a roster that has a snapshot but a zero parsed interval. 20m is the
// documented common heartbeat (internal/roster/roster.go comments).
const defaultHeartbeat = 20 * time.Minute

// FreshnessThreshold derives the snapshot staleness threshold from the watch
// tick cadence: a snapshot older than 3× the heartbeat interval is STALE (the
// same K-window order the detector's liveness uses). A non-positive interval
// (heartbeat disabled) falls back to defaultHeartbeat so the threshold is always
// meaningful.
func FreshnessThreshold(heartbeat time.Duration) time.Duration {
	if heartbeat <= 0 {
		heartbeat = defaultHeartbeat
	}
	return 3 * heartbeat
}

// Freshness is the three-state snapshot freshness (design §3): the operator
// needs to know WHICH no-fresh-data case they are in at the moment they open the
// dash, so this is sharper than status's binary present/absent.
type Freshness int

const (
	// FreshnessAbsent: no snapshot file at all (watch --change_detector never ran
	// on this roster dir). Every desk reads "unknown".
	FreshnessAbsent Freshness = iota
	// FreshnessStale: a snapshot exists but its age exceeds the threshold —
	// watch may be down; states are shown but marked stale.
	FreshnessStale
	// FreshnessFresh: snapshot age within the threshold; states shown live.
	FreshnessFresh
)

// String renders the freshness as the lowercase wire label the board JSON and
// the frontend share.
func (f Freshness) String() string {
	switch f {
	case FreshnessAbsent:
		return "absent"
	case FreshnessStale:
		return "stale"
	case FreshnessFresh:
		return "fresh"
	default:
		return "absent"
	}
}

// AgentItem is one desk in the fleet-board JSON. It is byte-shape-compatible with
// cmd/flotilla/status.go's statusItem so the board JSON is a strict SUPERSET of
// the `flotilla status --json` contract (the landing widget, site/app.js,
// consumes exactly these fields).
type AgentItem struct {
	Name    string `json:"name"`
	Role    string `json:"role,omitempty"`    // "hub" for the XO, else omitted
	Surface string `json:"surface,omitempty"` // effective surface driver
	State   string `json:"state"`             // same label set as `flotilla status`
}

// FreshnessInfo is the board's freshness banner (the superset's addition over the
// base status contract).
type FreshnessInfo struct {
	State            string `json:"state"`             // "absent" | "stale" | "fresh"
	Age              string `json:"age,omitempty"`     // humanized snapshot age (omitted when absent)
	AgeSeconds       int64  `json:"age_seconds"`       // snapshot age in seconds (0 when absent)
	ThresholdSeconds int64  `json:"threshold_seconds"` // the staleness threshold
	Message          string `json:"message"`           // operator-facing banner text
}

// XOLiveness is the XO's ack age + settled flag (the superset's second addition).
type XOLiveness struct {
	Agent         string `json:"agent"`
	Acked         bool   `json:"acked"`             // an ack file exists
	AckAge        string `json:"ack_age,omitempty"` // humanized (omitted when never acked)
	AckAgeSeconds int64  `json:"ack_age_seconds"`   // 0 when never acked
	Settled       bool   `json:"settled"`           // only meaningful when SettledKnown
	SettledKnown  bool   `json:"settled_known"`     // false when no snapshot (cannot assert)
}

// BoardDoc is the fleet-board JSON: a superset of the status `--json` shape
// (`generated_at` + `xo` + `agents[]`) plus the three-state freshness and the XO
// liveness. The base fields are preserved exactly so the landing widget and the
// dash speak the same contract.
type BoardDoc struct {
	GeneratedAt string        `json:"generated_at"` // snapshot mtime (RFC3339); "" when absent
	XO          string        `json:"xo,omitempty"`
	Agents      []AgentItem   `json:"agents"`
	Freshness   FreshnessInfo `json:"freshness"`
	XOLiveness  XOLiveness    `json:"xo_liveness"`
}

// BoardInputs are the already-loaded, already-stat'd values the HTTP layer
// supplies to BuildBoard. Keeping the builder pure (no file I/O, no real clock)
// is what makes the read model unit-testable.
type BoardInputs struct {
	Cfg         *roster.Config
	XO          string         // resolved XO (xo_agent, else Agents[0])
	GeneratedAt string         // snapshot mtime RFC3339, "" when absent
	Snap        watch.Snapshot // the loaded snapshot (zero value when absent)
	SnapOK      bool           // a snapshot file was read
	SnapAge     time.Duration  // snapshot mtime age (valid only when SnapOK)
	AckOK       bool           // an ack file exists
	AckAge      time.Duration  // ack mtime age (valid only when AckOK)
	Threshold   time.Duration  // freshness threshold (FreshnessThreshold(heartbeat))
}

// BuildBoard assembles the fleet-board document. Pure: no I/O, no real time.
func BuildBoard(in BoardInputs) BoardDoc {
	fresh := assessFreshness(in.SnapOK, in.SnapAge, in.Threshold)

	doc := BoardDoc{
		GeneratedAt: in.GeneratedAt,
		XO:          in.XO,
		Agents:      make([]AgentItem, 0, len(in.Cfg.Agents)),
		Freshness:   buildFreshnessInfo(fresh, in.SnapAge, in.SnapOK, in.Threshold),
		XOLiveness: XOLiveness{
			Agent:        in.XO,
			Acked:        in.AckOK,
			Settled:      in.Snap.XOSettled,
			SettledKnown: in.SnapOK, // without a snapshot we cannot assert the flag
		},
	}
	if in.AckOK {
		doc.XOLiveness.AckAge = humanizeAge(in.AckAge)
		doc.XOLiveness.AckAgeSeconds = int64(in.AckAge.Round(time.Second).Seconds())
	}
	for _, a := range in.Cfg.Agents {
		item := AgentItem{
			Name:    a.Name,
			Surface: effectiveSurface(a.Surface),
			State:   deskStateLabel(in.Snap, a.Name),
		}
		if a.Name == in.XO {
			item.Role = "hub"
		}
		doc.Agents = append(doc.Agents, item)
	}
	return doc
}

// assessFreshness maps the snapshot presence + age onto the three-state model.
func assessFreshness(snapOK bool, age, threshold time.Duration) Freshness {
	if !snapOK {
		return FreshnessAbsent
	}
	if age > threshold {
		return FreshnessStale
	}
	return FreshnessFresh
}

// buildFreshnessInfo renders the freshness banner. The message is the
// operator-facing line the frontend shows verbatim.
func buildFreshnessInfo(f Freshness, age time.Duration, snapOK bool, threshold time.Duration) FreshnessInfo {
	info := FreshnessInfo{
		State:            f.String(),
		ThresholdSeconds: int64(threshold.Round(time.Second).Seconds()),
	}
	if snapOK {
		info.Age = humanizeAge(age)
		info.AgeSeconds = int64(age.Round(time.Second).Seconds())
	}
	switch f {
	case FreshnessAbsent:
		info.Message = "no detector snapshot — start `flotilla watch` with change_detector: true to populate fleet state (all desks shown as unknown)"
	case FreshnessStale:
		info.Message = "snapshot is " + humanizeAge(age) + " old (threshold " + humanizeAge(threshold) + ") — `flotilla watch` may be down; desk states may be out of date"
	case FreshnessFresh:
		info.Message = "live — states as of " + humanizeAge(age) + " ago"
	}
	return info
}

// TopologyChannel is one channel→XO binding in the topology JSON.
type TopologyChannel struct {
	ChannelID string   `json:"channel_id"`
	XOAgent   string   `json:"xo_agent"`
	Members   []string `json:"members"`
	Role      string   `json:"role,omitempty"`
}

// TopologyDoc is the federation org chart: the effective channel↔XO bindings.
// For a single-fleet (legacy channel_id) roster this is the one synthesized
// binding — still rendered, just one box. A clock-only daemon (no channel_id,
// no channels[]) yields an empty Channels with an explanatory Note.
type TopologyDoc struct {
	Channels []TopologyChannel `json:"channels"`
	Note     string            `json:"note,omitempty"`
}

// BuildTopology renders the roster's effective bindings. Pure (reads cfg only).
func BuildTopology(cfg *roster.Config) TopologyDoc {
	bindings := cfg.Bindings()
	doc := TopologyDoc{Channels: make([]TopologyChannel, 0, len(bindings))}
	for _, ch := range bindings {
		// Copy Members defensively: Bindings() shares the Config's slice header in
		// the federation path (its documented read-only contract), and the board's
		// JSON must not alias roster-owned memory.
		members := make([]string, len(ch.Members))
		copy(members, ch.Members)
		doc.Channels = append(doc.Channels, TopologyChannel{
			ChannelID: ch.ChannelID,
			XOAgent:   ch.XOAgent,
			Members:   members,
			Role:      ch.Role,
		})
	}
	if len(doc.Channels) == 0 {
		doc.Note = "no channel bindings configured (a clock-only daemon: no channel_id and no channels[]) — there is no federation topology to render"
	}
	return doc
}

// LedgerEntry is one CoS who-knows-what exchange. Raw is ALWAYS the original
// rendered line (provenance + the fallback when structured parsing fails); the
// parsed fields are populated only when the line matches the cos.Line format.
type LedgerEntry struct {
	Time    string `json:"time,omitempty"`
	Channel string `json:"channel,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Gist    string `json:"gist,omitempty"`
	Raw     string `json:"raw"`
	Parsed  bool   `json:"parsed"`
}

// BacklogInfo is the backlog drive-queue classification (a flat projection of
// backlog.Status — what the XO is being driven on, what's blocked, what's done).
type BacklogInfo struct {
	Found     bool     `json:"found"`
	Unblocked []string `json:"unblocked"`
	Blocked   int      `json:"blocked"`
	Done      int      `json:"done"`
	Malformed int      `json:"malformed"`
	Items     int      `json:"items"`
}

// HistoryDoc is the coordination-history JSON: the CoS ledger entries
// (reverse-chronological — most recent first) and the backlog classification.
type HistoryDoc struct {
	Ledger  []LedgerEntry `json:"ledger"`
	Backlog BacklogInfo   `json:"backlog"`
}

// BuildHistory assembles the coordination history. Pure: the HTTP layer reads
// the two files and passes their contents (each "" when the file is absent).
func BuildHistory(ledgerRaw, backlogRaw string) HistoryDoc {
	doc := HistoryDoc{Ledger: parseLedger(ledgerRaw)}
	st := backlog.Parse(backlogRaw)
	doc.Backlog = BacklogInfo{
		Found:     st.Found,
		Unblocked: append([]string(nil), st.Unblocked...),
		Blocked:   st.Blocked,
		Done:      st.Done,
		Malformed: st.Malformed,
		Items:     st.Items,
	}
	if doc.Backlog.Unblocked == nil {
		doc.Backlog.Unblocked = []string{}
	}
	return doc
}

// parseLedger splits the ledger file into entries in REVERSE-chronological order
// (the file is appended chronologically; the operator wants newest first). Blank
// lines are skipped; every non-blank line yields an entry (parsed when it matches
// the cos.Line format, else carried verbatim as Raw).
func parseLedger(raw string) []LedgerEntry {
	out := []LedgerEntry{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, ParseLedgerLine(line))
	}
	// Reverse in place → most recent first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// ParseLedgerLine parses one rendered cos.Line:
//
//   - <RFC3339> · <channel> · <from> → <to> · "<gist>"
//
// On any deviation it returns an entry with only Raw set (Parsed=false) so a
// malformed or future-format line is never dropped or mis-rendered — it is shown
// verbatim. The gist is %q-rendered by cos.Line, so it is unquoted with
// strconv.Unquote.
func ParseLedgerLine(line string) LedgerEntry {
	entry := LedgerEntry{Raw: line}
	body := strings.TrimPrefix(line, "- ")
	// Four fields separated by " · "; the gist (last field) is quoted and may
	// itself contain the separator, so split into exactly four.
	parts := strings.SplitN(body, " · ", 4)
	if len(parts) != 4 {
		return entry
	}
	fromTo := strings.SplitN(parts[2], " → ", 2)
	if len(fromTo) != 2 {
		return entry
	}
	gist, err := strconv.Unquote(parts[3])
	if err != nil {
		return entry
	}
	entry.Time = parts[0]
	entry.Channel = parts[1]
	entry.From = fromTo[0]
	entry.To = fromTo[1]
	entry.Gist = gist
	entry.Parsed = true
	return entry
}

// effectiveSurface resolves an agent's surface name for display: an empty roster
// surface means the default driver, which the docs name "claude-code". Mirrors
// cmd/flotilla/status.go:effectiveSurface — the dash read model is package
// internal/dash and cannot import the package-main command, so the tiny,
// stable status-vocabulary helpers are reimplemented here (with a parity test);
// status.go remains the contract of record.
func effectiveSurface(s string) string {
	if s == "" {
		return "claude-code"
	}
	return s
}

// deskStateLabel renders a desk's snapshot state with the operator-facing
// vocabulary. StateShell renders "crashed" (status.go's established term); a desk
// absent from the snapshot (or no snapshot at all — DeskStates nil) is "unknown".
// Mirrors cmd/flotilla/status.go:deskStateLabel (see effectiveSurface note).
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

// humanizeAge renders a duration as a compact age (rounded to the second):
// "9s", "3m12s", "1h4m", "2d3h". A negative input (clock skew) clamps to "0s".
// Mirrors cmd/flotilla/status.go:humanizeAge (see effectiveSurface note).
func humanizeAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m" + strconv.Itoa(int(d.Seconds())%60) + "s"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h" + strconv.Itoa(int(d.Minutes())%60) + "m"
	default:
		return strconv.Itoa(int(d.Hours())/24) + "d" + strconv.Itoa(int(d.Hours())%24) + "h"
	}
}
