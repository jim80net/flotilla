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

func TestDispatchAckConvergesOnCoordinatorSendTimeConsume707(t *testing.T) {
	t.Setenv("FLOTILLA_ROSTER", "")
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"xo_agent":"xo","agents":[{"name":"xo","coordinator":true},{"name":"backend"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	msg, nonce, err := inbound.AppendDispatchNonce("coordinate the thing")
	if err != nil {
		t.Fatal(err)
	}
	// Send time: coordinator recipient is track-skipped and settles straight into
	// the consumed registry (#707) — no inbound pending row exists.
	if _, err := dispatch.ConsumeCoordinatorRecipient(dir, "backend", "xo", msg); err != nil {
		t.Fatal(err)
	}
	// The footer still instructs `flotilla dispatch-ack <nonce>`; on a coordinator
	// seat that must converge on the already-durable path, not error "not pending".
	t.Setenv("FLOTILLA_SELF", "xo")
	if err := cmdDispatchAck([]string{"--roster", rosterPath, nonce}); err != nil {
		t.Fatalf("coordinator dispatch-ack = %v, want already-durable success", err)
	}
}

// #707: a coordinator hop's send-time settlement of a nonce must not block the
// TRUE recipient's contract ack for the same dispatch text (forwarded verbatim,
// or the desk's reply-send quoting the footer landed first).
func TestDispatchAckDeskSucceedsPastCoordinatorHopEntry707(t *testing.T) {
	t.Setenv("FLOTILLA_ROSTER", "")
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"xo_agent":"xo","agents":[{"name":"xo","coordinator":true},{"name":"backend"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	msg, nonce, err := inbound.AppendDispatchNonce("forwarded work")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch.ConsumeCoordinatorRecipient(dir, "cos", "xo", msg); err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "fwd-1", Sender: "xo", Recipient: "backend", Message: msg, Nonce: nonce,
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLOTILLA_SELF", "backend")
	if err := cmdDispatchAck([]string{"--roster", rosterPath, nonce}); err != nil {
		t.Fatalf("desk dispatch-ack past coordinator hop entry = %v, want success", err)
	}
	// The desk's real settlement is recorded and takes lookup preference.
	e, ok := dispatch.NewRegistry(dir).LookupNonce(nonce)
	if !ok || e.Reason != dispatch.ReasonDurableAck || e.Recipient != "backend" {
		t.Fatalf("post-ack lookup = %+v, ok=%v, want desk durable-ack", e, ok)
	}
	path, _ := inbound.Path(dir, "backend")
	if pending := inbound.NewStore(path).Load(); len(pending) != 0 {
		t.Fatalf("pending after desk ack = %+v", pending)
	}
}

// #707 N2a: after the true recipient's real ack lands, the coordinator's own
// footer ack (its hop entry) must still converge — the registry scan is
// recipient-first, not first-entry-wins.
func TestDispatchAckCoordinatorConvergesAfterDeskRealAck707(t *testing.T) {
	t.Setenv("FLOTILLA_ROSTER", "")
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"xo_agent":"xo","agents":[{"name":"xo","coordinator":true},{"name":"backend"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	msg, nonce, err := inbound.AppendDispatchNonce("multi-hop work")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch.ConsumeCoordinatorRecipient(dir, "cos", "xo", msg); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch.NewRegistry(dir).Consume(dispatch.ConsumeFromInbound(nonce, msg, dispatch.ReasonDurableAck, "xo", "backend")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLOTILLA_SELF", "xo")
	if err := cmdDispatchAck([]string{"--roster", rosterPath, nonce}); err != nil {
		t.Fatalf("coordinator ack after desk real ack = %v, want convergence", err)
	}
	// The belongs-to-other protection still holds for a seat with NO settlement
	// and NO pending row.
	t.Setenv("FLOTILLA_SELF", "backend")
	if err := os.WriteFile(rosterPath, []byte(`{"xo_agent":"xo","agents":[{"name":"xo","coordinator":true},{"name":"backend"},{"name":"frontend"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLOTILLA_SELF", "frontend")
	if err := cmdDispatchAck([]string{"--roster", rosterPath, nonce}); err == nil {
		t.Fatal("an uninvolved seat must not converge on others' settlements")
	}
}
