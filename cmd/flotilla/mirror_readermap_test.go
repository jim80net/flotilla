package main

import (
	"strings"
	"testing"
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
		allowDiscord: true,
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return turn, true, nil },
		post:         func(_, _, b string) error { body = b; return nil },
		logf:         recordLogf(&lines),
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
		allowDiscord: true,
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return turn, true, nil },
		post:         func(_, _, b string) error { body = b; return nil },
		logf:         recordLogf(&lines),
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
		allowDiscord: true,
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return turn, true, nil },
		post:         func(_, _, b string) error { body = b; return nil },
		logf:         recordLogf(&lines),
	}
	m.run("backend")
	if body != turn {
		t.Errorf("a malformed envelope must publish the RAW turn-final (never lost); got %q", body)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "WARN malformed") {
		t.Errorf("decision lines = %v, want exactly one POST ... WARN malformed", lines)
	}
}

// An ordinary un-enveloped turn-final publishes raw with no flag (back-compat).
func TestReaderModelInternal_AbsentIsBackCompat(t *testing.T) {
	d := readerModelInternal("just an ordinary turn-final")
	if d.body != "just an ordinary turn-final" {
		t.Errorf("absent envelope must pass the raw body through; got %q", d.body)
	}
	if d.note != "" {
		t.Errorf("an ordinary turn-final carries no flag; got note %q", d.note)
	}
}
