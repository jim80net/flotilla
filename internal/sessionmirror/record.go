package sessionmirror

import (
	"time"

	"github.com/jim80net/flotilla/internal/readermap"
)

// DefaultMaxEntries is the per-agent ring-buffer retention when the caller does not
// override MaxEntries on Append.
const DefaultMaxEntries = 200

// DefaultVerboseCap is the per-entry verbose field size cap (runes). Entries beyond
// this fail-closed with truncation — v1 stores full text up to the cap.
const DefaultVerboseCap = 256_000

// DebugRecord is the structured dash debug rendering for one mirror event.
type DebugRecord struct {
	Info       string              `json:"info"`
	Envelope   *readermap.Envelope `json:"envelope,omitempty"`
	MirrorNote string              `json:"mirror_note,omitempty"`
	Firewall   *FirewallDiag       `json:"firewall,omitempty"`
}

// FirewallDiag carries advisory firewall metadata for debug rendering.
type FirewallDiag struct {
	WarnTerms []string `json:"warn_terms,omitempty"`
}

// Record is one append-only session-mirror ledger entry.
type Record struct {
	TS         string      `json:"ts"`
	Agent      string      `json:"agent"`
	Verbose    string      `json:"verbose"`
	Info       string      `json:"info"`
	Debug      DebugRecord `json:"debug"`
	Suppressed bool        `json:"suppressed"`
}

// Input is the pure builder input for a non-suppressed mirror event.
type Input struct {
	Agent        string
	At           time.Time
	Verbose      string
	Info         string
	MirrorNote   string
	Envelope     *readermap.Envelope
	FirewallWarn []string
	VerboseCap   int // 0 ⇒ DefaultVerboseCap
}

// NewRecord builds a ledger Record from mirror pipeline outputs. Suppressed events
// are not appended — this builder is for the publish path only.
func NewRecord(in Input) Record {
	capN := in.VerboseCap
	if capN <= 0 {
		capN = DefaultVerboseCap
	}
	verbose := truncateRunes(in.Verbose, capN)

	var fw *FirewallDiag
	if len(in.FirewallWarn) > 0 {
		fw = &FirewallDiag{WarnTerms: append([]string(nil), in.FirewallWarn...)}
	}

	ts := in.At.UTC()
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	return Record{
		TS:      ts.Format(time.RFC3339),
		Agent:   in.Agent,
		Verbose: verbose,
		Info:    in.Info,
		Debug: DebugRecord{
			Info:       in.Info,
			Envelope:   in.Envelope,
			MirrorNote: in.MirrorNote,
			Firewall:   fw,
		},
		Suppressed: false,
	}
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…[truncated]"
}
