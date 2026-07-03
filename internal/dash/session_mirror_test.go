package dash

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/sessionmirror"
)

func TestHandleSessionMirror_ReturnsLedgerEntries(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	srv, dir := newTestServer(t, singleFleetRoster, now)

	rec1 := sessionmirror.NewRecord(sessionmirror.Input{
		Agent:   "alpha",
		At:      now,
		Verbose: "full turn-final",
		Info:    "modeled brief",
	})
	rec2 := sessionmirror.NewRecord(sessionmirror.Input{
		Agent:   "alpha",
		At:      now.Add(time.Minute),
		Verbose: "second turn",
		Info:    "second brief",
	})
	for _, rec := range []sessionmirror.Record{rec1, rec2} {
		if err := sessionmirror.Append(dir, "alpha", rec, sessionmirror.AppendOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	rec := doGet(t, srv, "/api/session-mirror?agent=alpha&limit=1")
	if rec.Code != 200 {
		t.Fatalf("status code %d body %s", rec.Code, rec.Body.String())
	}
	var doc sessionmirror.HistoryDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Agent != "alpha" {
		t.Errorf("agent = %q, want alpha", doc.Agent)
	}
	if doc.Limit != 1 {
		t.Errorf("limit = %d, want 1", doc.Limit)
	}
	if len(doc.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(doc.Entries))
	}
	if doc.Entries[0].Info != "second brief" {
		t.Errorf("entry info = %q, want second brief (newest last)", doc.Entries[0].Info)
	}
}

func TestHandleSessionMirror_MissingAgent(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)

	rec := doGet(t, srv, "/api/session-mirror")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status code %d, want 400", rec.Code)
	}
}

func TestHandleSessionMirror_UnknownAgent(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)

	rec := doGet(t, srv, "/api/session-mirror?agent=not-in-roster")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status code %d, want 400", rec.Code)
	}
}

func TestHandleSessionMirror_EmptyLedger(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)

	rec := doGet(t, srv, "/api/session-mirror?agent=alpha")
	if rec.Code != 200 {
		t.Fatalf("status code %d", rec.Code)
	}
	var doc sessionmirror.HistoryDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Agent != "alpha" {
		t.Errorf("agent = %q", doc.Agent)
	}
	if len(doc.Entries) != 0 {
		t.Errorf("entries = %v, want empty for missing ledger", doc.Entries)
	}
}

