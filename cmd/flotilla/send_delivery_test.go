package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/dispatch"
	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/outbox"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
)

func TestNextSendRetryWait(t *testing.T) {
	if got := nextSendRetryWait(sendRetryInitial); got != 10*time.Second {
		t.Fatalf("got %v", got)
	}
	if got := nextSendRetryWait(40 * time.Second); got != sendRetryMax {
		t.Fatalf("cap got %v", got)
	}
}

func TestReopenRecipientQuarantineRequiresOpenRosterAndPreservesConsumed(t *testing.T) {
	dir := t.TempDir()
	q := dispatch.NewQuarantineRegistry(dir)
	now := time.Date(2026, 8, 13, 20, 0, 0, 0, time.UTC)
	entry := dispatch.QuarantineEntry{Kind: "inbound-ack", RowID: "synthetic-row", Recipient: "desk", QuarantinedAt: now}
	if inserted, err := q.Quarantine(entry); err != nil || !inserted {
		t.Fatalf("quarantine = (%v,%v)", inserted, err)
	}
	closed := &roster.Config{Agents: []roster.Agent{{Name: "desk", ClosedOut: true}}}
	reopenRecipientQuarantine(dir, closed, "desk", now.Add(time.Minute))
	if active, err := q.IsQuarantined(entry.Kind, entry.RowID, entry.Recipient); err != nil || !active {
		t.Fatalf("closed recipient reopened: active=%v err=%v", active, err)
	}
	open := &roster.Config{Agents: []roster.Agent{{Name: "desk"}}}
	reopenRecipientQuarantine(dir, open, "desk", now.Add(2*time.Minute))
	if active, err := q.IsQuarantined(entry.Kind, entry.RowID, entry.Recipient); err != nil || active {
		t.Fatalf("open confirmed recipient stayed quarantined: active=%v err=%v", active, err)
	}
	if consumed := dispatch.NewRegistry(dir).Load(); len(consumed) != 0 {
		t.Fatalf("reopen fabricated acknowledgement: %+v", consumed)
	}
}

func TestRecipientClosedOutUsesDurableDocumentUntilConfirmedRestoration(t *testing.T) {
	dir := t.TempDir()
	deskDir := filepath.Join(dir, "desks", "desk")
	if err := os.MkdirAll(deskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	closedAt := time.Date(2026, 8, 13, 20, 3, 0, 0, time.UTC)
	// Match the contract-of-record format exactly: UTC with minute precision.
	doc := "# Close-out\n\n**When:** 2026-08-13T20:03Z\n"
	if err := os.WriteFile(filepath.Join(deskDir, "CLOSE-OUT-20260813.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	openRoster := &roster.Config{Agents: []roster.Agent{{Name: "desk"}}}
	if !recipientClosedOut(dir, openRoster, "desk") {
		t.Fatal("durable close-out document did not close recipient")
	}

	// A real confirmed turn is restoration proof. It tombstones any row markers and records a
	// durable recipient transition, while the close-out audit document remains untouched.
	reopenRecipientQuarantine(dir, openRoster, "desk", closedAt.Add(time.Minute))
	if recipientClosedOut(dir, openRoster, "desk") {
		t.Fatal("confirmed restoration did not override the older audit close-out event")
	}
	if _, err := os.Stat(filepath.Join(deskDir, "CLOSE-OUT-20260813.md")); err != nil {
		t.Fatalf("restoration removed close-out provenance: %v", err)
	}
	restoredAt, err := dispatch.NewQuarantineRegistry(dir).RecipientRestoredAt("desk")
	if err != nil || !restoredAt.Equal(closedAt.Add(time.Minute)) {
		t.Fatalf("durable restoration = %v err=%v", restoredAt, err)
	}
	generation, err := dispatch.NewQuarantineRegistry(dir).Generation("desk")
	if err != nil {
		t.Fatal(err)
	}
	// Routine later confirmed turns are not new restoration transitions: they must not rewrite
	// the registry or advance Fired generation, which would recreate restart-style recurrence.
	reopenRecipientQuarantine(dir, openRoster, "desk", closedAt.Add(2*time.Minute))
	restoredAgain, err := dispatch.NewQuarantineRegistry(dir).RecipientRestoredAt("desk")
	if err != nil || !restoredAgain.Equal(restoredAt) {
		t.Fatalf("routine confirmed turn changed restoration: before=%v after=%v err=%v", restoredAt, restoredAgain, err)
	}
	if got, err := dispatch.NewQuarantineRegistry(dir).Generation("desk"); err != nil || got != generation {
		t.Fatalf("routine confirmed turn changed generation: before=%d after=%d err=%v", generation, got, err)
	}
	if consumed := dispatch.NewRegistry(dir).Load(); len(consumed) != 0 {
		t.Fatalf("restoration fabricated acknowledgement: %+v", consumed)
	}

	// A later explicit administrative close-out is a new event and wins again.
	newClosedAt := closedAt.Add(3 * time.Minute)
	doc = "# Close-out again\n\n**When:** " + newClosedAt.Format(time.RFC3339) + "\n"
	if err := os.WriteFile(filepath.Join(deskDir, "CLOSE-OUT-20260813T2005.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if !recipientClosedOut(dir, openRoster, "desk") {
		t.Fatal("newer close-out event did not supersede restoration")
	}
}

func TestRecipientClosedOutExplicitRosterFlagOverridesConfirmedRestoration(t *testing.T) {
	dir := t.TempDir()
	q := dispatch.NewQuarantineRegistry(dir)
	if _, err := q.ReopenRecipient("desk", time.Date(2026, 8, 13, 21, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	closedRoster := &roster.Config{Agents: []roster.Agent{{Name: "desk", ClosedOut: true}}}
	if !recipientClosedOut(dir, closedRoster, "desk") {
		t.Fatal("explicit roster close-out must remain authoritative")
	}
}

func TestErrRetryableBusyUnwrap(t *testing.T) {
	err := fmt.Errorf("%w", errRetryableBusy{agent: "cos"})
	if !errors.Is(err, surface.ErrBusy) {
		t.Fatal("should unwrap to ErrBusy")
	}
}

// Acceptance (#484): repeated bounce of the same send dedups to the existing outbox id.
// Drives the production path: stamp → bounce → enqueue (cmdSend stamps before enqueue).
func TestBouncedSendDedupesIdenticalPending(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"agents":[{"name":"xo"},{"name":"alpha"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	base := "deploy complete"
	busy := errRetryableBusy{agent: "xo"}
	msg1, _, err := inbound.AppendDispatchNonce(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := enqueueOrFailSend(rosterPath, "alpha", "xo", msg1, busy); err != nil {
		t.Fatal(err)
	}
	msg2, _, err := inbound.AppendDispatchNonce(base)
	if err != nil {
		t.Fatal(err)
	}
	if msg1 == msg2 {
		t.Fatal("probe requires distinct stamps")
	}
	if err := enqueueOrFailSend(rosterPath, "alpha", "xo", msg2, busy); err != nil {
		t.Fatal(err)
	}
	path, err := outbox.Path(dir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	got := outbox.NewStore(path).Load()
	if len(got) != 1 {
		t.Fatal("duplicate bounce must not append a second pending entry")
	}
	if got[0].Message != msg1 {
		t.Fatalf("surviving queued send keeps first stamp, got %q", got[0].Message)
	}
}

// Acceptance (#475): a bounced send lands in the sender's durable outbox instead of failing the turn.
func TestBouncedSendLandsInOutbox(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"agents":[{"name":"xo"},{"name":"alpha"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	busy := errRetryableBusy{agent: "xo"}
	if err := enqueueOrFailSend(rosterPath, "alpha", "xo", "deploy complete", busy); err != nil {
		t.Fatalf("enqueueOrFailSend = %v, want success (queued)", err)
	}
	path, err := outbox.Path(dir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	got := outbox.NewStore(path).Load()
	if len(got) != 1 || got[0].Recipient != "xo" || got[0].Message != "deploy complete" {
		t.Fatalf("outbox = %+v, want one pending send to xo", got)
	}
	if got[0].EnqueuedAt.IsZero() {
		t.Fatal("enqueued_at must be set")
	}
}

func TestDirectSendAfterDurableJoinsTailWithoutQueueJump(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if _, _, err := outbox.Enqueue(dir, "alpha", "xo", "first durable order"); err != nil {
		t.Fatal(err)
	}
	queued, err := deliverOrQueueSend(&roster.Config{}, rosterPath, "beta", "xo", nil, "unused", "later direct order")
	if err != nil || !queued {
		t.Fatalf("queued=%v err=%v, want durable tail join", queued, err)
	}
	got := outbox.ListAll(dir)
	if len(got) != 2 || got[0].Message != "first durable order" || got[1].Message != "later direct order" {
		t.Fatalf("recipient order=%+v", got)
	}
}

// #475 desk-visible queued ack: machine-readable QUEUED line for monitors.
func TestFormatQueuedAck_Visible(t *testing.T) {
	line := dispatch.FormatQueuedAck("deadbeef", "alpha", "xo", false)
	if !strings.Contains(line, "QUEUED") || !strings.Contains(line, "status=busy_outbox") {
		t.Fatalf("queued ack not desk-visible: %q", line)
	}
	if !strings.Contains(dispatch.FormatQueuedAck("x", "a", "b", true), "already_queued") {
		t.Fatal("deduped ack missing already_queued")
	}
}

// Acceptance (#494): CLI direct-delivery success writes the inbound ledger (not injector-only).
func TestCLIDirectDeliveryTracksInboundE2E(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{
		"xo_agent":"cos",
		"agents":[
			{"name":"cos"},
			{"name":"memex"},
			{"name":"codex-harness-dev"}
		],
		"channels":[{"channel_id":"1","xo_agent":"cos","members":["codex-harness-dev"]}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}

	msg, nonce, err := inbound.AppendDispatchNonce("continue dispatch")
	if err != nil {
		t.Fatal(err)
	}
	recordDirectInboundTrack(cfg, rosterPath, "memex", "codex-harness-dev", msg)

	path, err := inbound.Path(dir, "codex-harness-dev")
	if err != nil {
		t.Fatal(err)
	}
	got := inbound.NewStore(path).Load()
	if len(got) != 1 || got[0].Nonce != nonce || got[0].Sender != "memex" {
		t.Fatalf("inbound ledger = %+v, want memex dispatch with nonce %q", got, nonce)
	}

	var reinjected []watch.Job
	hook := watch.DroppedDispatchFinishHook(dir, func(string) (string, bool, error) {
		return "done without nonce echo", true, nil
	}, func(j watch.Job) { reinjected = append(reinjected, j) }, nil)
	hook("codex-harness-dev")

	if len(reinjected) != 1 {
		t.Fatalf("miss without nonce echo: want 1 reinject, got %d", len(reinjected))
	}
	if !strings.Contains(reinjected[0].Message, "dropped-dispatch resume") {
		t.Fatalf("reinject message = %q", reinjected[0].Message)
	}
}

// #498 walk acceptance: desk-home channel (xo_agent=desk, members=[coordinator]) must
// write inbound ledger after confirmed CLI track with real IsCoordinator.
func TestCLIDirectDeliveryTracksWalkDeskHomeChannel498(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	body := `{
		"operator_user_id":"U","xo_agent":"meta-xo","cos_agent":"meta-xo",
		"agents":[{"name":"meta-xo"},{"name":"backend"}],
		"channels":[
			{"channel_id":"C_CMD","xo_agent":"meta-xo","role":"fleet-command","members":["meta-xo","backend"]},
			{"channel_id":"C_BE","xo_agent":"backend","members":["meta-xo"]}
		]}`
	if err := os.WriteFile(rosterPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsCoordinator("backend") {
		t.Fatal("backend must not classify as coordinator on walk desk-home shape")
	}
	msg, nonce, err := inbound.AppendDispatchNonce("ORG dispatch: harness work")
	if err != nil {
		t.Fatal(err)
	}
	recordDirectInboundTrack(cfg, rosterPath, "meta-xo", "backend", msg)
	path, err := inbound.Path(dir, "backend")
	if err != nil {
		t.Fatal(err)
	}
	got := inbound.NewStore(path).Load()
	if len(got) != 1 || got[0].Nonce != nonce || got[0].Sender != "meta-xo" {
		t.Fatalf("inbound ledger = %+v, want nonce %q from meta-xo", got, nonce)
	}
	// #707 negative: a DESK recipient records the pending row only — a send-time
	// consumed entry here would instantly suppress the desk's reinject supervision.
	if entries := dispatch.NewRegistry(dir).Load(); len(entries) != 0 {
		t.Fatalf("desk-recipient send wrote consumed entries = %+v, want none", entries)
	}
}

// Acceptance (#491): execution desk with supervisor-as-member residue still records inbound.
func TestCLIDirectDeliveryTracksDeclassifiedExecutionDesk491(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	body := `{
		"operator_user_id":"U","xo_agent":"cos","cos_agent":"cos",
		"agents":[{"name":"cos"},{"name":"product-skill-dev","coordinator":false},{"name":"dash-desk"}],
		"channels":[
			{"channel_id":"C_CMD","xo_agent":"cos","role":"fleet-command","members":["product-skill-dev","dash-desk"]},
			{"channel_id":"C_PSKILL","xo_agent":"product-skill-dev","members":["cos"]},
			{"channel_id":"C_DASH","xo_agent":"dash-desk","members":["product-skill-dev"]}
		]}`
	if err := os.WriteFile(rosterPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsCoordinator("product-skill-dev") {
		t.Fatal("product-skill-dev must not classify as coordinator")
	}
	msg, nonce, err := inbound.AppendDispatchNonce("build the classifier fix")
	if err != nil {
		t.Fatal(err)
	}
	recordDirectInboundTrack(cfg, rosterPath, "cos", "product-skill-dev", msg)
	path, err := inbound.Path(dir, "product-skill-dev")
	if err != nil {
		t.Fatal(err)
	}
	got := inbound.NewStore(path).Load()
	if len(got) != 1 || got[0].Nonce != nonce {
		t.Fatalf("inbound ledger = %+v, want one entry with nonce %q", got, nonce)
	}
}

func TestCLIDirectDeliverySkipsCoordinatorInbound(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"xo_agent":"cos","agents":[{"name":"cos"},{"name":"memex"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	msg, _, err := inbound.AppendDispatchNonce("nudge")
	if err != nil {
		t.Fatal(err)
	}
	recordDirectInboundTrack(cfg, rosterPath, "memex", "cos", msg)
	path, _ := inbound.Path(dir, "cos")
	if len(inbound.NewStore(path).Load()) != 0 {
		t.Fatal("coordinator inbound must not be tracked on CLI path")
	}
	// #707: the skipped coordinator dispatch settles into the consumed registry
	// through THIS wiring, so its footer's dispatch-ack / dispatch-status work.
	nonce := inbound.ParseOwnDispatchNonce(msg)
	e, ok := dispatch.NewRegistry(dir).LookupNonce(nonce)
	if !ok || e.Reason != dispatch.ReasonCoordinatorRecipient || e.Recipient != "cos" || e.Sender != "memex" {
		t.Fatalf("coordinator consumed entry via CLI track = %+v, ok=%v", e, ok)
	}
}
