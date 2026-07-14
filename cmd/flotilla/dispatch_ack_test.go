package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jim80net/flotilla/internal/dispatch"
	"github.com/jim80net/flotilla/internal/inbound"
)

func TestDispatchAckWritesDurableRecordAndClearsInbound683(t *testing.T) {
	t.Setenv("FLOTILLA_ROSTER", "")
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"xo_agent":"xo","agents":[{"name":"xo"},{"name":"backend"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e := inbound.Entry{
		ID: "in-1", Sender: "xo", Recipient: "backend", Message: "generic dispatch",
		Nonce: "flotilla-dispatch-cafebabe",
	}
	if err := inbound.Record(dir, e); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLOTILLA_SELF", "backend")
	if err := cmdDispatchAck([]string{"--roster", rosterPath, e.Nonce}); err != nil {
		t.Fatal(err)
	}
	consumed, ok := dispatch.NewRegistry(dir).LookupNonce(e.Nonce)
	if !ok || consumed.Reason != dispatch.ReasonDurableAck || consumed.Recipient != "backend" {
		t.Fatalf("durable ack = %+v, ok=%v", consumed, ok)
	}
	path, err := inbound.Path(dir, "backend")
	if err != nil {
		t.Fatal(err)
	}
	if pending := inbound.NewStore(path).Load(); len(pending) != 0 {
		t.Fatalf("pending after durable ack = %+v", pending)
	}
}

func TestDispatchAckRefusesAnotherSeatsNonce683(t *testing.T) {
	t.Setenv("FLOTILLA_ROSTER", "")
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"xo_agent":"xo","agents":[{"name":"xo"},{"name":"backend"},{"name":"frontend"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "in-2", Sender: "xo", Recipient: "frontend", Message: "generic dispatch",
		Nonce: "flotilla-dispatch-deadbeef",
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLOTILLA_SELF", "backend")
	if err := cmdDispatchAck([]string{"--roster", rosterPath, "flotilla-dispatch-deadbeef"}); err == nil {
		t.Fatal("a desk must not acknowledge another seat's dispatch")
	}
}

func TestDispatchAckRerunClearsRegistryFirstPartialSuccess683(t *testing.T) {
	t.Setenv("FLOTILLA_ROSTER", "")
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"agents":[{"name":"backend"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e := inbound.Entry{ID: "in-3", Sender: "xo", Recipient: "backend", Message: "generic dispatch", Nonce: "flotilla-dispatch-acde1234"}
	if err := inbound.Record(dir, e); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch.NewRegistry(dir).Consume(dispatch.ConsumeFromInbound(e.Nonce, e.Message, dispatch.ReasonDurableAck, e.Sender, e.Recipient)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLOTILLA_SELF", "backend")
	if err := cmdDispatchAck([]string{"--roster", rosterPath, e.Nonce}); err != nil {
		t.Fatal(err)
	}
	path, _ := inbound.Path(dir, "backend")
	if pending := inbound.NewStore(path).Load(); len(pending) != 0 {
		t.Fatalf("idempotent rerun left inbound rows: %+v", pending)
	}
}
