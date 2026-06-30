package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/readermap"
)

func TestParseBriefArgs_DeskRequired(t *testing.T) {
	if _, err := parseBriefArgs([]string{"--from", "xo"}); err == nil {
		t.Fatal("brief with no desk must error")
	}
}

func TestParseBriefArgs_AcceptsDeskAndFlags(t *testing.T) {
	a, err := parseBriefArgs([]string{"--from", "xo", "--audience", "newcomer", "backend"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.desk != "backend" {
		t.Errorf("desk = %q, want backend", a.desk)
	}
	if a.from != "xo" {
		t.Errorf("from = %q, want xo", a.from)
	}
	if a.audience != "newcomer" {
		t.Errorf("audience = %q, want newcomer", a.audience)
	}
}

func TestParseBriefArgs_FlagAfterDeskIsCaught(t *testing.T) {
	if _, err := parseBriefArgs([]string{"backend", "--audience", "operator"}); err == nil {
		t.Fatal("a flag placed AFTER the desk must be caught, not silently swallowed")
	}
}

func TestParseBriefArgs_DefaultAudienceIsOperator(t *testing.T) {
	a, err := parseBriefArgs([]string{"--from", "xo", "backend"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.audience != string(readermap.AudienceOperator) {
		t.Errorf("default audience = %q, want operator", a.audience)
	}
}

func TestBuildBriefRequest_InstructsEnvelopeAndForbidsNotify(t *testing.T) {
	req := buildBriefRequest(string(readermap.AudienceOperator))
	if !strings.Contains(req, readermap.FenceTag) {
		t.Errorf("brief request must tell the desk the %q fence tag; got %q", readermap.FenceTag, req)
	}
	for _, field := range []string{"audience", "anchor", "delta", "decision"} {
		if !strings.Contains(req, field) {
			t.Errorf("brief request must name the envelope field %q", field)
		}
	}
	if !strings.Contains(req, "notify") || !strings.Contains(strings.ToLower(req), "do not run") {
		t.Errorf("brief request must carry the secret-free invariant (do NOT run notify); got %q", req)
	}
	if !strings.Contains(strings.ToUpper(req), "DECISION") {
		t.Errorf("brief request must instruct lead-with-the-decision; got %q", req)
	}
}

func TestBuildBriefRequest_DeskAudienceHumanized(t *testing.T) {
	req := buildBriefRequest("desk:flotilla")
	if !strings.Contains(req, "desk flotilla") {
		t.Errorf("a desk:<name> audience should humanize to 'desk <name>' in the prose; got %q", req)
	}
	if !strings.Contains(req, `"audience": "desk:flotilla"`) {
		t.Errorf("the JSON audience field must carry the raw desk:<name> value; got %q", req)
	}
}

func TestBuildBriefRequest_EmptyAudienceDefaultsOperator(t *testing.T) {
	req := buildBriefRequest("")
	if !strings.Contains(req, `"audience": "operator"`) {
		t.Errorf("empty audience must default to operator; got %q", req)
	}
}

func TestDeskIsDark(t *testing.T) {
	if !deskIsDark("", nil) {
		t.Error("an empty webhook URL is dark")
	}
	if !deskIsDark("   ", nil) {
		t.Error("a whitespace webhook URL is dark")
	}
	if !deskIsDark("https://wh", errors.New("no such agent")) {
		t.Error("a webhook resolution error is dark")
	}
	if deskIsDark("https://wh", nil) {
		t.Error("a resolved webhook is NOT dark")
	}
}
