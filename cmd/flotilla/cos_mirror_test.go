package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
)

// loadCosRoster writes a roster JSON to a temp dir, loads it, and returns the resolved
// config (CosLedger defaulted beside the roster when cos_agent is set) for the relay
// mirror tests. Recording is gated only on cos_agent (CosLedger != "") since #349 E11
// realized the desk-relay scope; the IsXO gate is gone.
func loadCosRoster(t *testing.T, body string) *roster.Config {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(p)
	if err != nil {
		t.Fatalf("load roster: %v", err)
	}
	return cfg
}

// cosFederatedRoster is the shared federated fixture: meta-xo owns #fleet-command,
// alpha-xo owns #fleet-alpha, alpha-be is alpha's desk. cos_agent=meta-xo.
const cosFederatedRoster = `{
	  "operator_user_id":"U","cos_agent":"meta-xo",
	  "agents":[{"name":"meta-xo"},{"name":"alpha-xo"},{"name":"alpha-be"}],
	  "channels":[
	    {"channel_id":"C_CMD","xo_agent":"meta-xo","members":["alpha-xo"]},
	    {"channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["alpha-be"]}]}`

// writeCosRoster writes a roster JSON to a temp dir and returns (rosterPath, ledgerPath).
func writeCosRoster(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p, filepath.Join(dir, "context-ledger.md")
}

func readLedger(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "" // absent ⇒ nothing was mirrored
	}
	return string(b)
}

func TestMirrorRelay_InboundLedgeredWithOriginChannel(t *testing.T) {
	cfg := loadCosRoster(t, cosFederatedRoster)
	// An operator message routed to alpha-xo on #fleet-alpha (the #105 OriginChannel
	// seam) is recorded operator → alpha-xo, tagged with the origin channel.
	mirrorRelayToLedger(cfg, watch.Job{Agent: "alpha-xo", Message: "ship the cache PR", Kind: "relay", OriginChannel: "C_ALPHA"})
	got := readLedger(t, cfg.CosLedger)
	if !strings.Contains(got, "operator → alpha-xo") || !strings.Contains(got, "C_ALPHA") {
		t.Errorf("inbound entry wrong:\n%s", got)
	}
	if !strings.Contains(got, `"ship the cache PR"`) {
		t.Errorf("inbound entry missing gist:\n%s", got)
	}
}

func TestMirrorRelay_DeskTargetLedgered(t *testing.T) {
	// #349 E11 source: an operator message addressed to a DESK (@alpha-be) is now recorded
	// (the v1 IsXO gate / §6.3 deferral is realized), so it lands in alpha-be's dash thread.
	cfg := loadCosRoster(t, cosFederatedRoster)
	mirrorRelayToLedger(cfg, watch.Job{Agent: "alpha-be", Message: "do the thing", Kind: "relay", OriginChannel: "C_ALPHA"})
	got := readLedger(t, cfg.CosLedger)
	if !strings.Contains(got, "operator → alpha-be") || !strings.Contains(got, "C_ALPHA") {
		t.Errorf("operator→desk relay must now be ledgered, got:\n%s", got)
	}
	if !strings.Contains(got, `"do the thing"`) {
		t.Errorf("desk relay entry missing gist:\n%s", got)
	}
}

func TestMirrorRelay_InertWhenCosLedgerEmpty(t *testing.T) {
	// cos_agent unset ⇒ cfg.CosLedger == "" ⇒ no write (and no panic on the empty path).
	cfg := loadCosRoster(t, `{
	  "operator_user_id":"U","channel_id":"C","xo_agent":"alpha-xo",
	  "agents":[{"name":"alpha-xo"}]}`)
	if cfg.CosLedger != "" {
		t.Fatalf("expected inert CosLedger, got %q", cfg.CosLedger)
	}
	mirrorRelayToLedger(cfg, watch.Job{Agent: "alpha-xo", Message: "x", OriginChannel: "C"})
}

func TestMirrorNotify_XOReplyIsLedgered(t *testing.T) {
	rosterPath, ledger := writeCosRoster(t, cosFederatedRoster)

	mirrorNotifyToLedger(rosterPath, "alpha-xo", "merged; deploying")

	got := readLedger(t, ledger)
	if !strings.Contains(got, "alpha-xo → operator") {
		t.Errorf("ledger missing the XO→operator entry:\n%s", got)
	}
	if !strings.Contains(got, "C_ALPHA") {
		t.Errorf("entry should be tagged with the XO's channel C_ALPHA:\n%s", got)
	}
	if !strings.Contains(got, `"merged; deploying"`) {
		t.Errorf("entry should carry the gist:\n%s", got)
	}
}

func TestMirrorNotify_DeskSenderLedgered(t *testing.T) {
	// #349 E11 source: a desk's notify to the operator is now recorded (v1 IsXO gate / §6.3
	// deferral realized), so it lands in the desk's own dash thread. alpha-be owns no channel
	// binding, so the entry tags no channel ("-") — the from→to line is what matters.
	rosterPath, ledger := writeCosRoster(t, cosFederatedRoster)

	mirrorNotifyToLedger(rosterPath, "alpha-be", "status update")

	got := readLedger(t, ledger)
	if !strings.Contains(got, "alpha-be → operator") {
		t.Errorf("a desk notify must now be ledgered, got:\n%s", got)
	}
	if !strings.Contains(got, `"status update"`) {
		t.Errorf("desk notify entry missing gist:\n%s", got)
	}
}

func TestMirrorSend_LedgersRelay(t *testing.T) {
	// #349 E11 source: `flotilla send` now records the confirmed relay (from → to) to the CoS
	// ledger, tagged with the recipient's home channel — this is what populates a desk's dash
	// conversation thread with CoS↔desk / desk↔desk traffic.
	cfg := loadCosRoster(t, cosFederatedRoster)
	mirrorSendToLedger(cfg, "meta-xo", "alpha-xo", "rebase and re-run CI")
	got := readLedger(t, cfg.CosLedger)
	if !strings.Contains(got, "meta-xo → alpha-xo") || !strings.Contains(got, "C_ALPHA") {
		t.Errorf("send must be ledgered from→to with the recipient's channel, got:\n%s", got)
	}
	if !strings.Contains(got, `"rebase and re-run CI"`) {
		t.Errorf("send entry missing gist:\n%s", got)
	}
}

func TestMirrorSend_InertWhenCosLedgerEmpty(t *testing.T) {
	// cos_agent unset ⇒ CosLedger == "" ⇒ no write, no panic.
	cfg := loadCosRoster(t, `{
	  "operator_user_id":"U","channel_id":"C","xo_agent":"alpha-xo",
	  "agents":[{"name":"alpha-xo"}]}`)
	if cfg.CosLedger != "" {
		t.Fatalf("expected inert CosLedger, got %q", cfg.CosLedger)
	}
	mirrorSendToLedger(cfg, "alpha-xo", "alpha-be", "x") // must be a no-op
}

func TestMirrorNotify_InertWithoutCosAgent(t *testing.T) {
	rosterPath, ledger := writeCosRoster(t, `{
	  "operator_user_id":"U","channel_id":"C","xo_agent":"xo",
	  "agents":[{"name":"xo"}]}`)

	mirrorNotifyToLedger(rosterPath, "xo", "hi")

	if got := readLedger(t, ledger); got != "" {
		t.Errorf("no cos_agent ⇒ inert, but ledger was written:\n%s", got)
	}
}

func TestMirrorNotify_MissingRosterIsBestEffort(t *testing.T) {
	// An unreadable roster path must not panic or error — the helper is best-effort
	// (notify already succeeded). It simply does nothing.
	mirrorNotifyToLedger(filepath.Join(t.TempDir(), "does-not-exist.json"), "alpha-xo", "x")
}
