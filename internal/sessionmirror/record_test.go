package sessionmirror

import (
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/readermap"
)

func TestNewRecord_InfoDebugVerboseDerivation(t *testing.T) {
	env := &readermap.Envelope{
		Audience: readermap.AudienceOperator,
		Anchor:   "the cache backfill",
		Delta:    "finished",
		Decision: readermap.DecisionNone,
	}
	at := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	rec := NewRecord(Input{
		Agent:      "backend",
		At:         at,
		Verbose:    "raw turn-final with ```reader-map\n{}\n``` block",
		Info:       "the cache backfill\n\nDecision: none\n\nfinished",
		MirrorNote: "modeled",
		Envelope:   env,
	})

	if rec.Verbose != "raw turn-final with ```reader-map\n{}\n``` block" {
		t.Errorf("verbose = %q, want full turn-final", rec.Verbose)
	}
	if rec.Info != "the cache backfill\n\nDecision: none\n\nfinished" {
		t.Errorf("info = %q", rec.Info)
	}
	if rec.Debug.Info != rec.Info {
		t.Error("debug.info must match info body")
	}
	if rec.Debug.Envelope == nil || rec.Debug.Envelope.Anchor != "the cache backfill" {
		t.Errorf("debug.envelope = %+v", rec.Debug.Envelope)
	}
	if rec.Debug.MirrorNote != "modeled" {
		t.Errorf("debug.mirror_note = %q", rec.Debug.MirrorNote)
	}
	if rec.Debug.Firewall != nil {
		t.Errorf("debug.firewall = %v, want null", rec.Debug.Firewall)
	}
	if rec.Suppressed {
		t.Error("published record must not be suppressed")
	}
	if rec.TS != "2026-07-03T12:00:00Z" {
		t.Errorf("ts = %q", rec.TS)
	}
}

func TestNewRecord_DebugOmitsEnvelopeWhenAbsent(t *testing.T) {
	rec := NewRecord(Input{
		Agent:   "backend",
		Verbose: "plain prose",
		Info:    "plain prose",
	})
	if rec.Debug.Envelope != nil {
		t.Errorf("absent envelope must omit debug.envelope, got %+v", rec.Debug.Envelope)
	}
}

func TestNewRecord_FirewallWarnInDebug(t *testing.T) {
	rec := NewRecord(Input{
		Agent:        "backend",
		Verbose:      "we flattened the book",
		Info:         "we flattened the book",
		MirrorNote:   "WARN firewall-vocab flatten",
		FirewallWarn: []string{"flatten"},
	})
	if rec.Debug.Firewall == nil {
		t.Fatal("firewall diag missing")
	}
	if len(rec.Debug.Firewall.WarnTerms) != 1 || rec.Debug.Firewall.WarnTerms[0] != "flatten" {
		t.Errorf("warn terms = %v", rec.Debug.Firewall.WarnTerms)
	}
}

func TestNewRecord_FirewallRoundTripsJSON(t *testing.T) {
	rec := NewRecord(Input{
		Agent:        "backend",
		Verbose:      "x",
		Info:         "x",
		FirewallWarn: []string{"flatten"},
	})
	line := MustLine(rec)
	parsed := ParseLines(line)
	if len(parsed) != 1 {
		t.Fatalf("parsed entries = %d", len(parsed))
	}
	if parsed[0].Debug.Firewall == nil || parsed[0].Debug.Firewall.WarnTerms[0] != "flatten" {
		t.Errorf("firewall after json round-trip = %+v", parsed[0].Debug.Firewall)
	}
}

func TestNewRecord_TruncatesVerboseAtCap(t *testing.T) {
	rec := NewRecord(Input{
		Agent:      "backend",
		Verbose:    strings.Repeat("世", 20),
		Info:       "x",
		VerboseCap: 10,
	})
	if !strings.HasSuffix(rec.Verbose, "…[truncated]") {
		t.Errorf("verbose = %q, want truncation marker", rec.Verbose)
	}
	const truncMarker = "…[truncated]"
	if len([]rune(rec.Verbose)) > 10+len([]rune(truncMarker)) {
		t.Errorf("verbose runes exceed cap + marker: %d", len([]rune(rec.Verbose)))
	}
}
