package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/sessionmirror"
)

func TestDeskMirror_AppendsSessionLedgerOnPost(t *testing.T) {
	dir := t.TempDir()
	var posted string
	var lines []string
	turn := "```reader-map\n" +
		`{"audience":"operator","anchor":"anchor text","delta":"delta text","decision":"none"}` +
		"\n```"

	m := deskMirror{
		rosterDir: dir,
		now:       func() time.Time { return time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC) },
		webhook:   func(string) (string, bool) { return "https://wh", true },
		turnFinal: func(string) (string, bool, error) { return turn, true, nil },
		post:      func(_, _, body string) error { posted = body; return nil },
		logf:      recordLogf(&lines),
	}
	m.run("backend")

	if !strings.HasPrefix(posted, "anchor text") {
		t.Errorf("discord body = %q, want modeled info body unchanged", posted)
	}

	path, err := sessionmirror.LedgerPath(dir, "backend")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := sessionmirror.BuildHistory("backend", raw, 0)
	if len(doc.Entries) != 1 {
		t.Fatalf("ledger entries = %d, want 1", len(doc.Entries))
	}
	e := doc.Entries[0]
	if e.Verbose != turn {
		t.Errorf("verbose = %q, want full turn-final", e.Verbose)
	}
	if e.Info != posted {
		t.Errorf("info = %q, want discord body %q", e.Info, posted)
	}
	if e.Debug.Envelope == nil || e.Debug.Envelope.Anchor != "anchor text" {
		t.Errorf("debug envelope = %+v", e.Debug.Envelope)
	}
	if e.Debug.MirrorNote != "modeled" {
		t.Errorf("debug mirror_note = %q", e.Debug.MirrorNote)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "POST backend") {
		t.Errorf("decision lines = %v, want exactly one POST line", lines)
	}
}

// TestDeskMirror_SuppressKeepsPrivateLedgerSkipsPost locks #405 Inc 1: a firewall REFUSE gags
// the PUBLIC Discord post, but the session-mirror ledger is a PRIVATE, loopback-only operator
// surface — so the refused turn-final is STILL written to the private ledger (raw text + refuse
// marker) while the public post is skipped. This is the fix for the "CoS thread empty" bug: the
// coordinator's fleet-vocabulary-dense turn-finals were tripping REFUSE and vanishing from the
// operator's own dash.
func TestDeskMirror_SuppressKeepsPrivateLedgerSkipsPost(t *testing.T) {
	dir := t.TempDir()
	appended := false
	var rec sessionmirror.Record
	m := deskMirror{
		rosterDir: dir,
		webhook:   func(string) (string, bool) { return "https://wh", true },
		turnFinal: func(string) (string, bool, error) { return "leak acme-desk token", true, nil },
		post: func(_, _, _ string) error {
			t.Fatal("firewall refuse must NOT post to the public Discord egress")
			return nil
		},
		logf:     func(string, ...any) {},
		firewall: firewallTermSet(t, []string{"acme-desk"}, nil),
		alert:    func(string) {},
		ledgerAppend: func(_, _ string, r sessionmirror.Record) error {
			appended = true
			rec = r
			return nil
		},
	}
	m.run("backend")
	if !appended {
		t.Fatal("firewall refuse must STILL append the PRIVATE (loopback-only) session-mirror ledger")
	}
	if !strings.Contains(rec.Verbose, "leak acme-desk token") {
		t.Errorf("the private ledger record must carry the RAW withheld turn-final so the operator sees it; Verbose=%q", rec.Verbose)
	}
	if !strings.Contains(rec.Debug.MirrorNote, "firewall refuse") {
		t.Errorf("the private ledger record must mark WHY it was withheld from Discord; MirrorNote=%q", rec.Debug.MirrorNote)
	}
}

// TestDeskMirror_PrimaryXOLedgerOnlyInvariant documents the P1 gate: CoordinatorMirrorOnFinish
// (primary clock XO) appends session-mirror and runs readerModelInternal but never posts to Discord.
// The XO Stop hook already publishes via flotilla notify — a second post would double-publish.
func TestDeskMirror_LedgerFailOmitsLedgerSuccess(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	turn := "coordinator turn-final body"

	m := deskMirror{
		ledgerOnly: true,
		rosterDir:  dir,
		turnFinal:  func(string) (string, bool, error) { return turn, true, nil },
		post:       func(_, _, _ string) error { t.Fatal("must not post"); return nil },
		logf:       recordLogf(&lines),
		ledgerAppend: func(string, string, sessionmirror.Record) error {
			return fmt.Errorf("disk full")
		},
	}
	m.run("xo")

	for _, line := range lines {
		if strings.Contains(line, "mirror LEDGER xo") && !strings.Contains(line, "LEDGER-FAIL") {
			t.Fatalf("false success audit line on append failure: %q", line)
		}
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "LEDGER-FAIL") {
		t.Fatalf("decision lines = %v, want exactly one LEDGER-FAIL", lines)
	}
}

func TestDeskMirror_PrimaryXOLedgerOnlyInvariant(t *testing.T) {
	dir := t.TempDir()
	postCalls := 0
	var lines []string
	turn := "```reader-map\n" +
		`{"audience":"operator","anchor":"xo anchor","delta":"xo delta","decision":"none"}` +
		"\n```"

	m := deskMirror{
		ledgerOnly: true,
		rosterDir:  dir,
		now:        func() time.Time { return time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC) },
		turnFinal:  func(string) (string, bool, error) { return turn, true, nil },
		post: func(_, _, _ string) error {
			postCalls++
			return nil
		},
		logf: recordLogf(&lines),
	}
	m.run("xo")

	if postCalls != 0 {
		t.Fatalf("primary XO ledger-only mirror posted %d times, want 0 (Stop hook owns Discord)", postCalls)
	}

	path, err := sessionmirror.LedgerPath(dir, "xo")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := sessionmirror.BuildHistory("xo", raw, 0)
	if len(doc.Entries) != 1 {
		t.Fatalf("ledger entries = %d, want 1", len(doc.Entries))
	}
	if !strings.HasPrefix(doc.Entries[0].Info, "xo anchor") {
		t.Errorf("info = %q, want modeled anchor body", doc.Entries[0].Info)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "LEDGER xo") {
		t.Errorf("decision lines = %v, want exactly one LEDGER line", lines)
	}
}

func TestDeskMirror_LedgerOnlySkipsWithoutWebhook(t *testing.T) {
	dir := t.TempDir()
	postCalls := 0
	m := deskMirror{
		ledgerOnly: true,
		rosterDir:  dir,
		turnFinal:  func(string) (string, bool, error) { return "coordinator turn", true, nil },
		post: func(_, _, _ string) error {
			postCalls++
			return nil
		},
		logf: func(string, ...any) {},
	}
	m.run("xo")

	if postCalls != 0 {
		t.Errorf("ledger-only mirror posted %d times, want 0", postCalls)
	}
	path, err := sessionmirror.LedgerPath(dir, "xo")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := sessionmirror.BuildHistory("xo", raw, 0)
	if len(doc.Entries) != 1 {
		t.Fatalf("ledger entries = %d, want 1 without webhook", len(doc.Entries))
	}
}

func TestDeskMirror_DeskPathStillPosts(t *testing.T) {
	var posted string
	m := deskMirror{
		webhook:   func(string) (string, bool) { return "https://wh", true },
		turnFinal: func(string) (string, bool, error) { return "desk report", true, nil },
		post:      func(_, _, body string) error { posted = body; return nil },
		logf:      func(string, ...any) {},
	}
	m.run("backend")
	if posted != "desk report" {
		t.Errorf("desk mirror body = %q, want discord post", posted)
	}
}

func TestReaderModelInternal_DerivationForSessionMirror(t *testing.T) {
	turn := "prose\n\n```reader-map\n" +
		`{"audience":"operator","anchor":"A","delta":"D","decision":"ship it"}` +
		"\n```"
	d := readerModelInternal(turn, nil)
	if d.body == turn {
		t.Fatal("enveloped brief must render modeled info body")
	}
	if d.envelope == nil || d.envelope.Anchor != "A" {
		t.Fatalf("envelope = %+v", d.envelope)
	}
	rec := sessionmirror.NewRecord(sessionmirror.Input{
		Agent:      "backend",
		Verbose:    turn,
		Info:       d.body,
		MirrorNote: d.note,
		Envelope:   d.envelope,
	})
	if rec.Verbose != turn {
		t.Error("verbose must retain full turn-final")
	}
	if rec.Info != d.body {
		t.Error("info must be modeled body")
	}
	if rec.Debug.Envelope.Anchor != "A" {
		t.Errorf("debug envelope anchor = %q", rec.Debug.Envelope.Anchor)
	}
}
