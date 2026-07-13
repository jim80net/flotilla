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
		allowDiscord: true,
		rosterDir:    dir,
		now:          func() time.Time { return time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC) },
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return turn, true, nil },
		post:         func(_, _, body string) error { posted = body; return nil },
		logf:         recordLogf(&lines),
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

// TestDeskMirror_FleetInternalVocabPostsAndLedgers locks #465: deployment vocabulary on a
// fleet-internal mirror path posts to the operator channel AND appends the dash ledger —
// the partition firewall does not run on operator surfaces.
func TestDeskMirror_FleetInternalVocabPostsAndLedgers(t *testing.T) {
	dir := t.TempDir()
	appended := false
	var rec sessionmirror.Record
	var posted string
	m := deskMirror{
		allowDiscord: true,
		rosterDir:    dir,
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return "leak acme-desk token", true, nil },
		post:         func(_, _, body string) error { posted = body; return nil },
		logf:         func(string, ...any) {},
		ledgerAppend: func(_, _ string, r sessionmirror.Record) error {
			appended = true
			rec = r
			return nil
		},
	}
	m.run("backend")
	if posted != "leak acme-desk token" {
		t.Fatalf("fleet-internal mirror must post deployment vocabulary; got %q", posted)
	}
	if !appended {
		t.Fatal("fleet-internal mirror must append the session-mirror ledger")
	}
	if !strings.Contains(rec.Verbose, "leak acme-desk token") {
		t.Errorf("ledger must carry the raw turn-final; Verbose=%q", rec.Verbose)
	}
	if rec.Suppressed {
		t.Error("fleet-internal mirror must not mark Suppressed — firewall does not run here (#465)")
	}
}

func TestDeskMirror_LedgerFailOmitsLedgerSuccess(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	turn := "coordinator turn-final body"

	m := deskMirror{
		rosterDir: dir,
		turnFinal: func(string) (string, bool, error) { return turn, true, nil },
		post:      func(_, _, _ string) error { t.Fatal("must not post"); return nil },
		logf:      recordLogf(&lines),
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

func TestDeskMirror_CoordinatorDefaultsToLedgerWithoutDiscord683(t *testing.T) {
	dir := t.TempDir()
	postCalls := 0
	var postedBody string
	var lines []string
	turn := "```reader-map\n" +
		`{"audience":"operator","anchor":"xo anchor","delta":"xo delta","decision":"none"}` +
		"\n```"

	m := deskMirror{
		rosterDir: dir,
		now:       func() time.Time { return time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC) },
		webhook:   func(string) (string, bool) { return "https://wh", true },
		turnFinal: func(string) (string, bool, error) { return turn, true, nil },
		post: func(_, _, body string) error {
			postCalls++
			postedBody = body
			return nil
		},
		logf: recordLogf(&lines),
	}
	m.run("xo")

	if postCalls != 0 {
		t.Fatalf("coordinator turn-final posted %d times, want 0 (operator channel is fail-quiet)", postCalls)
	}
	if postedBody != "" {
		t.Errorf("posted body = %q, want no operator-channel turn-final", postedBody)
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

func TestDeskMirror_ExplicitParadeStillPostsAndLedgers683(t *testing.T) {
	dir := t.TempDir()
	if err := markParadePending(dir, "backend"); err != nil {
		t.Fatal(err)
	}
	posted := 0
	m := deskMirror{
		rosterDir:    dir,
		claimDiscord: func(agent string) bool { return claimParadePending(dir, agent) },
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return "parade answer", true, nil },
		post:         func(_, _, _ string) error { posted++; return nil },
		logf:         func(string, ...any) {},
	}
	m.run("backend")
	if posted != 1 {
		t.Fatalf("explicit parade posts = %d, want 1", posted)
	}
	path, err := sessionmirror.LedgerPath(dir, "backend")
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(path); err != nil || !strings.Contains(string(raw), "parade answer") {
		t.Fatalf("parade ledger = %q, err=%v", raw, err)
	}
}

func TestDeskMirror_LedgerOnlySkipsWithoutWebhook(t *testing.T) {
	dir := t.TempDir()
	postCalls := 0
	m := deskMirror{
		rosterDir: dir,
		turnFinal: func(string) (string, bool, error) { return "coordinator turn", true, nil },
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
		allowDiscord: true,
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return "desk report", true, nil },
		post:         func(_, _, body string) error { posted = body; return nil },
		logf:         func(string, ...any) {},
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
	d := readerModelInternal(turn)
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
