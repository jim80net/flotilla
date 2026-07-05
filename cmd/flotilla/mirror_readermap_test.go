package main

import (
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/readermap"
)

// An enveloped brief that passes tier-1 is published as the RENDERED body (modeled
// from the fields), NOT the raw turn-final with its JSON block.
func TestDeskMirrorRendersEnvelopedBrief(t *testing.T) {
	var lines []string
	var body string
	turn := "Here is my brief.\n\n```reader-map\n" +
		`{"audience":"operator","anchor":"the backfill you track","delta":"it finished","decision":"none"}` +
		"\n```"
	m := deskMirror{
		webhook:   func(string) (string, bool) { return "https://wh", true },
		turnFinal: func(string) (string, bool, error) { return turn, true, nil },
		post:      func(_, _, b string) error { body = b; return nil },
		logf:      recordLogf(&lines),
	}
	m.run("backend")
	if strings.Contains(body, "reader-map") || strings.Contains(body, "{") {
		t.Errorf("published body should be the RENDERED brief, not the raw JSON block; got %q", body)
	}
	if !strings.HasPrefix(body, "the backfill you track") {
		t.Errorf("rendered body must open with the anchor; got %q", body)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "POST backend") || !strings.Contains(lines[0], "modeled") {
		t.Errorf("decision lines = %v, want exactly one POST ... modeled", lines)
	}
}

// A deficient envelope (present, tier-1 fail) warns-and-publishes the RAW turn-final
// (never lost) and flags the gap — the mirror has no public egress, so it never
// blocks an internal post on a lint.
func TestDeskMirrorWarnsAndPublishesDeficientEnvelope(t *testing.T) {
	var lines []string
	var body string
	turn := "```reader-map\n" + `{"audience":"operator","anchor":"","delta":"x","decision":"none"}` + "\n```"
	m := deskMirror{
		webhook:   func(string) (string, bool) { return "https://wh", true },
		turnFinal: func(string) (string, bool, error) { return turn, true, nil },
		post:      func(_, _, b string) error { body = b; return nil },
		logf:      recordLogf(&lines),
	}
	m.run("backend")
	if body != turn {
		t.Errorf("a deficient envelope must publish the RAW turn-final (never lost); got %q", body)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "WARN tier1") {
		t.Errorf("decision lines = %v, want exactly one POST ... WARN tier1", lines)
	}
}

// A malformed (unparseable) reader-map block warns-and-publishes raw + flags.
func TestDeskMirrorWarnsAndPublishesMalformedEnvelope(t *testing.T) {
	var lines []string
	var body string
	turn := "```reader-map\n{not json]\n```"
	m := deskMirror{
		webhook:   func(string) (string, bool) { return "https://wh", true },
		turnFinal: func(string) (string, bool, error) { return turn, true, nil },
		post:      func(_, _, b string) error { body = b; return nil },
		logf:      recordLogf(&lines),
	}
	m.run("backend")
	if body != turn {
		t.Errorf("a malformed envelope must publish the RAW turn-final (never lost); got %q", body)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "WARN malformed") {
		t.Errorf("decision lines = %v, want exactly one POST ... WARN malformed", lines)
	}
}

// An ordinary un-enveloped turn-final publishes raw with no flag (back-compat). With
// a nil firewall the firewall stage is skipped entirely (the P0 behavior).
func TestReaderModelInternal_AbsentIsBackCompat(t *testing.T) {
	d := readerModelInternal("just an ordinary turn-final", nil)
	if d.suppress {
		t.Fatal("a nil firewall must never suppress on the internal mirror")
	}
	if d.body != "just an ordinary turn-final" {
		t.Errorf("absent envelope must pass the raw body through; got %q", d.body)
	}
	if d.note != "" {
		t.Errorf("an ordinary turn-final carries no flag; got note %q", d.note)
	}
	if d.alert {
		t.Errorf("an ordinary turn-final raises no alert; got alert=%v", d.alert)
	}
}

// firewallTermSet builds a TermSet for the mirror-wiring tests.
func firewallTermSet(t *testing.T, deny, warn []string) *readermap.TermSet {
	t.Helper()
	ts, err := readermap.NewTermSet(deny, warn)
	if err != nil {
		t.Fatalf("NewTermSet: %v", err)
	}
	return ts
}

// STAGE 1 firewall: a leaking turn-final is withheld from the PUBLIC post (never posted),
// logged on the one decision line as SUPPRESS-POST, and raises the operator-visible alert.
// (The private loopback ledger is still kept — #405 Inc 1 — but this wiring has no rosterDir,
// so the ledger append is inert here; the ledger-kept behavior is covered in mirror_session_test.)
func TestDeskMirrorSuppressesAndAlertsOnFirewallRefuse(t *testing.T) {
	var lines []string
	var posted bool
	var alerts []string
	m := deskMirror{
		webhook:   func(string) (string, bool) { return "https://wh", true },
		turnFinal: func(string) (string, bool, error) { return "the acme-desk finished its run", true, nil },
		post:      func(_, _, _ string) error { posted = true; return nil },
		logf:      recordLogf(&lines),
		firewall:  firewallTermSet(t, []string{"acme-desk"}, nil),
		alert:     func(s string) { alerts = append(alerts, s) },
	}
	m.run("backend")
	if posted {
		t.Fatal("a firewall REFUSE must SUPPRESS the post (nothing published)")
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "SUPPRESS-POST backend") || !strings.Contains(lines[0], "firewall refuse") {
		t.Fatalf("decision lines = %v, want exactly one SUPPRESS-POST ... firewall refuse", lines)
	}
	if len(alerts) != 1 || !strings.Contains(alerts[0], "WITHHELD") || !strings.Contains(alerts[0], "backend") {
		t.Fatalf("alerts = %v, want exactly one operator alert naming the withheld desk", alerts)
	}
}

// STAGE 1 firewall WARN tier: a domain-vocabulary hit (no denylist) still PUBLISHES
// (never lost), flags the decision line, and raises an advisory alert.
func TestDeskMirrorWarnTierPublishesAndAlerts(t *testing.T) {
	var lines []string
	var body string
	var alerts []string
	m := deskMirror{
		webhook:   func(string) (string, bool) { return "https://wh", true },
		turnFinal: func(string) (string, bool, error) { return "we flattened the book", true, nil },
		post:      func(_, _, b string) error { body = b; return nil },
		logf:      recordLogf(&lines),
		firewall:  firewallTermSet(t, nil, []string{"flatten(ed|s|ing)?"}),
		alert:     func(s string) { alerts = append(alerts, s) },
	}
	m.run("backend")
	if body != "we flattened the book" {
		t.Fatalf("a firewall WARN must still PUBLISH the turn-final (never lost); got %q", body)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "POST backend") || !strings.Contains(lines[0], "WARN firewall-vocab") {
		t.Fatalf("decision lines = %v, want one POST ... WARN firewall-vocab", lines)
	}
	if len(alerts) != 1 || !strings.Contains(alerts[0], "advisory") {
		t.Fatalf("alerts = %v, want one advisory alert", alerts)
	}
}

// The firewall precedes the envelope stages: a leak inside an otherwise-valid brief is
// SUPPRESSED before any modeling work (no post, no render).
func TestDeskMirrorFirewallPrecedesEnvelope(t *testing.T) {
	var lines []string
	var posted bool
	turn := "```reader-map\n" +
		`{"audience":"operator","anchor":"the acme-desk run","delta":"done","decision":"none"}` +
		"\n```"
	m := deskMirror{
		webhook:   func(string) (string, bool) { return "https://wh", true },
		turnFinal: func(string) (string, bool, error) { return turn, true, nil },
		post:      func(_, _, _ string) error { posted = true; return nil },
		logf:      recordLogf(&lines),
		firewall:  firewallTermSet(t, []string{"acme-desk"}, nil),
		alert:     func(string) {},
	}
	m.run("backend")
	if posted {
		t.Fatal("a leak in an enveloped brief must SUPPRESS before the envelope render")
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "SUPPRESS") {
		t.Fatalf("decision lines = %v, want one SUPPRESS", lines)
	}
}
