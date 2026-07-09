package inbound

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
)

func TestTrackConfirmedSend_SkipsCoordinator(t *testing.T) {
	dir := t.TempDir()
	msg, _, err := AppendDispatchNonce("hi")
	if err != nil {
		t.Fatal(err)
	}
	isCoord := func(agent string) bool { return agent == "cos" }
	dec, err := TrackConfirmedSend(dir, "memex", "cos", msg, "1", isCoord)
	if err != nil {
		t.Fatal(err)
	}
	if dec != TrackSkipped {
		t.Fatalf("decision = %q, want skipped", dec)
	}
	path, _ := Path(dir, "cos")
	if len(NewStore(path).Load()) != 0 {
		t.Fatal("coordinator must not be tracked")
	}
	dec, err = TrackConfirmedSend(dir, "memex", "backend", msg, "2", isCoord)
	if err != nil {
		t.Fatal(err)
	}
	if dec != TrackRecorded {
		t.Fatalf("decision = %q, want recorded", dec)
	}
	path, _ = Path(dir, "backend")
	if len(NewStore(path).Load()) != 1 {
		t.Fatal("execution desk must be tracked")
	}
}

// #498 walk shape: execution desk owns its home channel with sole supervisor as member
// (xo_agent=backend, members=[meta-xo]). After #513 this desk is NOT IsCoordinator;
// a confirmed send MUST write the inbound ledger (dropped-dispatch requires it).
func TestTrackConfirmedSend_WalkDeskHomeChannel498(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	body := `{
	  "operator_user_id":"U","xo_agent":"meta-xo","cos_agent":"meta-xo",
	  "agents":[{"name":"meta-xo"},{"name":"backend"},{"name":"frontend"}],
	  "channels":[
	    {"channel_id":"C_CMD","xo_agent":"meta-xo","role":"fleet-command",
	      "members":["meta-xo","backend","frontend"]},
	    {"channel_id":"C_BE","xo_agent":"backend","members":["meta-xo"]},
	    {"channel_id":"C_FE","xo_agent":"frontend","members":["meta-xo"]}
	  ]
	}`
	if err := os.WriteFile(rosterPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsCoordinator("backend") {
		t.Fatal("walk desk-home shape: backend must NOT classify as coordinator (#513/#498)")
	}
	if !cfg.IsCoordinator("meta-xo") {
		t.Fatal("meta-xo must remain coordinator")
	}

	msg, nonce, err := AppendDispatchNonce("ORG dispatch: implement the harness fix")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	dec, err := TrackConfirmedSend(dir, "meta-xo", "backend", msg, "walk-1", cfg.IsCoordinator)
	if err != nil {
		t.Fatal(err)
	}
	if dec != TrackRecorded {
		t.Fatalf("decision = %q, want recorded", dec)
	}
	path, err := Path(dir, "backend")
	if err != nil {
		t.Fatal(err)
	}
	got := NewStore(path).Load()
	if len(got) != 1 || got[0].Nonce != nonce || got[0].Sender != "meta-xo" {
		t.Fatalf("inbound ledger = %+v, want one entry nonce=%q from meta-xo", got, nonce)
	}
	journal := buf.String()
	if !strings.Contains(journal, "inbound track backend recorded reason=ok") {
		t.Fatalf("journal missing recorded line, got: %q", journal)
	}

	// Coordinator skip still journals.
	buf.Reset()
	dec, err = TrackConfirmedSend(dir, "backend", "meta-xo", msg, "walk-2", cfg.IsCoordinator)
	if err != nil {
		t.Fatal(err)
	}
	if dec != TrackSkipped {
		t.Fatalf("decision = %q, want skipped", dec)
	}
	if !strings.Contains(buf.String(), "inbound track meta-xo skipped reason=coordinator") {
		t.Fatalf("journal missing skip line, got: %q", buf.String())
	}
	metaPath, _ := Path(dir, "meta-xo")
	if len(NewStore(metaPath).Load()) != 0 {
		t.Fatal("coordinator must not grow an inbound ledger")
	}
}

func TestTrackConfirmedSend_JournalLines498(t *testing.T) {
	dir := t.TempDir()
	msg, _, err := AppendDispatchNonce("ping")
	if err != nil {
		t.Fatal(err)
	}
	isCoord := func(a string) bool { return a == "xo" }

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	if _, err := TrackConfirmedSend(dir, "xo", "backend", msg, "a", isCoord); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "inbound track backend recorded reason=ok") {
		t.Fatalf("want recorded journal, got %q", buf.String())
	}
	buf.Reset()
	if _, err := TrackConfirmedSend(dir, "backend", "xo", msg, "b", isCoord); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "inbound track xo skipped reason=coordinator") {
		t.Fatalf("want skipped journal, got %q", buf.String())
	}
}
