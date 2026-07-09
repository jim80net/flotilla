package watch

import (
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
)

// newRelayHarness wires a Relay over an Injector whose CONFIRMED-delivery mirror
// records the FULL Job (so assertions can see OriginChannel, not just agent+body).
// send always succeeds, so every routed job is delivered and recorded in order.
func newRelayHarness(cfg *roster.Config) (*Relay, *collector, *[]string) {
	c := &collector{}
	inj := NewInjector(func(agent, msg string) error { return nil }, 8)
	inj.SetMirror(c.enqueue) // capture the whole delivered Job (incl. OriginChannel)
	inj.Start()
	var notices []string
	r := NewRelay(cfg, inj, nil, func(s string) { notices = append(notices, s) })
	return r, c, &notices
}

// legacyCfg is the pre-federation single-fleet roster: one channel_id + xo_agent.
// Bindings() synthesizes the one binding with EVERY agent as a member, so "@name"
// resolves against all agents exactly as before federation.
func legacyCfg() *roster.Config {
	return &roster.Config{
		OperatorUserID: "op",
		ChannelID:      "C1",
		XOAgent:        "xo",
		Agents:         []roster.Agent{{Name: "xo"}, {Name: "backend"}},
	}
}

// fedCfg is a federated roster: #fleet-command bound to the meta-XO (members = the
// project-XOs) plus a per-project channel for each project-XO (members = its desks).
// A project-XO is a member of fleet-command AND the xo of its own channel — the
// recursion the design relies on.
func fedCfg() *roster.Config {
	return &roster.Config{
		OperatorUserID: "op",
		Agents: []roster.Agent{
			{Name: "meta-xo"},
			{Name: "alpha-xo"}, {Name: "alpha-be"},
			{Name: "beta-xo"}, {Name: "beta-be"},
		},
		Channels: []roster.Channel{
			{Role: "fleet-command", ChannelID: "C_CMD", XOAgent: "meta-xo", Members: []string{"alpha-xo", "beta-xo"}},
			{Role: "project", ChannelID: "C_ALPHA", XOAgent: "alpha-xo", Members: []string{"alpha-be"}},
			{Role: "project", ChannelID: "C_BETA", XOAgent: "beta-xo", Members: []string{"beta-be"}},
		},
	}
}

// Handle drops a non-operator message. NOTE: the self-mirror (webhook) drop NO
// LONGER lives in Handle — it moved into the transport adapter (the author-agnostic
// selfMirrorGuardAdapter), so a self-post never reaches Handle at all and webhookID
// is no longer a Handle argument. The webhook-drop property is pinned at the adapter
// level (internal/transport's selfmirror tests, including the sender==operator case).
// Operator relay routes to the adjutant when adjutant_for is configured (#533).
func TestRelayOperatorToCoordinatorRoutesAdjutant(t *testing.T) {
	cfg := &roster.Config{
		OperatorUserID: "op",
		ChannelID:      "C1",
		XOAgent:        "alpha-xo",
		Agents: []roster.Agent{
			{Name: "alpha-xo"},
			{Name: "alpha-adj", AdjutantFor: "alpha-xo"},
		},
	}
	delivered := make(chan string, 1)
	inj := NewInjector(func(agent, msg string) error {
		delivered <- agent
		return nil
	}, 8)
	inj.SetCoordinatorIngress(NewCoordinatorIngress(cfg))
	inj.Start()
	defer inj.Stop()
	r := NewRelay(cfg, inj, nil, nil)
	r.Handle("C1", "200", "op", "status?")
	select {
	case gotAgent := <-delivered:
		if gotAgent != "alpha-adj" {
			t.Errorf("relay target = %q, want alpha-adj (#533 adjutant routing)", gotAgent)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("relay delivery timed out")
	}
}

func TestRelayDropsNonOperator(t *testing.T) {
	r, c, _ := newRelayHarness(legacyCfg())
	r.Handle("C1", "1000002", "intruder", "do evil") // non-operator → dropped
	r.injector.Stop()                                // drain
	if c.count() != 0 {
		t.Errorf("delivered %d, want 0 (non-operator must be dropped)", c.count())
	}
}

// With a dedup gate wired, the live path relays a message once and records its id;
// a re-delivery of the SAME id (e.g. the poller also fetched it, or a duplicate
// gateway event) is dropped — and the live path NEVER advances the cursor.
func TestRelayWithGate_DedupsAndDoesNotAdvanceCursor(t *testing.T) {
	r, c, _ := newRelayHarness(legacyCfg())
	gate := newDedup(cursorStore{}, defaultSeenCap)
	r.SetGate(gate)

	r.Handle("C1", "100", "op", "ship it") // new id → relayed
	r.Handle("C1", "100", "op", "ship it") // same id → deduped
	waitForDelivered(t, c, 1)
	time.Sleep(20 * time.Millisecond)
	if c.count() != 1 {
		t.Fatalf("delivered %d, want 1 (the second is a duplicate id)", c.count())
	}
	if cur, _ := gate.cursorOf("C1"); cur != 0 {
		t.Fatalf("live path advanced cursor to %d, want 0 (leapfrog guard)", cur)
	}
}

// An unparseable message id (never for real Discord data) bypasses the gate rather
// than being silently dropped — fail-open on a malformed id.
func TestRelayWithGate_UnparseableIDBypassesGate(t *testing.T) {
	r, c, _ := newRelayHarness(legacyCfg())
	r.SetGate(newDedup(cursorStore{}, defaultSeenCap))
	r.Handle("C1", "not-a-snowflake", "op", "still deliver me")
	waitForDelivered(t, c, 1)
}

// A message on a channel no binding owns is ignored even from the operator — the
// relay's defense-in-depth drop (the gateway already filters, but the relay must
// not route an unbound channel either).
func TestRelayDropsUnboundChannel(t *testing.T) {
	r, c, _ := newRelayHarness(legacyCfg())
	r.Handle("C_OTHER", "1000003", "op", "status please") // operator, but unbound channel → dropped
	r.injector.Stop()
	if c.count() != 0 {
		t.Errorf("delivered %d, want 0 (a message on an unbound channel must be dropped)", c.count())
	}
}

// An empty / whitespace-only operator message is dropped — there is nothing to
// deliver, and an empty body is the signature of a bound channel where the bot lacks
// the Message Content intent (partial-intent coverage is possible once federation
// spans several channels). It must never inject a blank turn into the XO pane.
func TestRelayDropsEmptyContent(t *testing.T) {
	r, c, _ := newRelayHarness(legacyCfg())
	r.Handle("C1", "1000004", "op", "")    // bot-without-intent shape → dropped
	r.Handle("C1", "1000005", "op", "   ") // whitespace-only → dropped
	r.injector.Stop()
	if c.count() != 0 {
		t.Errorf("delivered %d, want 0 (empty/whitespace operator message must be dropped)", c.count())
	}
}

func TestRelayRoutesOperatorMessage(t *testing.T) {
	r, c, notices := newRelayHarness(legacyCfg())
	r.Handle("C1", "1000006", "op", "@backend ship it")
	r.Handle("C1", "1000007", "op", "status please") // bare → XO
	r.Handle("C1", "1000008", "op", "@nope hello")   // unknown → XO + notice
	r.injector.Stop()                                // drain

	if len(c.jobs) != 3 {
		t.Fatalf("delivered %d, want 3", len(c.jobs))
	}
	if c.jobs[0].Agent != "backend" || c.jobs[0].Message != "ship it" {
		t.Errorf("directed route wrong: %+v", c.jobs[0])
	}
	if c.jobs[1].Agent != "xo" || c.jobs[1].Message != "status please" {
		t.Errorf("bare route wrong: %+v", c.jobs[1])
	}
	if c.jobs[2].Agent != "xo" {
		t.Errorf("unknown-agent should fall back to XO: %+v", c.jobs[2])
	}
	if len(*notices) != 1 {
		t.Errorf("notices = %d, want 1 (unknown agent)", len(*notices))
	}
}

// The relay tags every routed delivery with the origin channel (the CoS-mirror seam
// for #108). The mirror hook receives the whole Job, so OriginChannel must ride it.
func TestRelaySetsOriginChannelOnJob(t *testing.T) {
	r, c, _ := newRelayHarness(legacyCfg())
	r.Handle("C1", "1000009", "op", "status please")
	r.injector.Stop()
	if len(c.jobs) != 1 {
		t.Fatalf("delivered %d, want 1", len(c.jobs))
	}
	if c.jobs[0].OriginChannel != "C1" {
		t.Errorf("OriginChannel = %q, want C1 (the CoS-mirror seam must carry the origin channel)", c.jobs[0].OriginChannel)
	}
	if c.jobs[0].Kind != "relay" {
		t.Errorf("Kind = %q, want relay", c.jobs[0].Kind)
	}
}

// Routing is by ORIGIN channel: a bare message hits that channel's bound XO; an
// "@name" resolves only within that channel's member scope. A project channel
// resolves its desks; #fleet-command resolves the project-XOs (the same primitive
// one tier up).
func TestRelayRoutesByOriginChannel(t *testing.T) {
	r, c, _ := newRelayHarness(fedCfg())
	r.Handle("C_ALPHA", "1000010", "op", "@alpha-be do x")   // project desk, in its project channel
	r.Handle("C_ALPHA", "1000011", "op", "status")           // bare → project-XO
	r.Handle("C_CMD", "1000012", "op", "@alpha-xo delegate") // project-XO, addressed from fleet-command
	r.Handle("C_CMD", "1000013", "op", "status")             // bare → meta-XO
	r.injector.Stop()

	if len(c.jobs) != 4 {
		t.Fatalf("delivered %d, want 4: %+v", len(c.jobs), c.jobs)
	}
	if c.jobs[0].Agent != "alpha-be" || c.jobs[0].Message != "do x" || c.jobs[0].OriginChannel != "C_ALPHA" {
		t.Errorf("alpha desk route wrong: %+v", c.jobs[0])
	}
	if c.jobs[1].Agent != "alpha-xo" || c.jobs[1].OriginChannel != "C_ALPHA" {
		t.Errorf("alpha bare→project-XO wrong: %+v", c.jobs[1])
	}
	if c.jobs[2].Agent != "alpha-xo" || c.jobs[2].Message != "delegate" || c.jobs[2].OriginChannel != "C_CMD" {
		t.Errorf("fleet-command @project-XO route wrong: %+v", c.jobs[2])
	}
	if c.jobs[3].Agent != "meta-xo" || c.jobs[3].OriginChannel != "C_CMD" {
		t.Errorf("fleet-command bare→meta-XO wrong: %+v", c.jobs[3])
	}
}

// Member scope is ISOLATED per channel: a desk addressable in its own project
// channel is NOT addressable from another channel (it falls back to that channel's
// bound XO + a notice), and a desk is NOT addressable from fleet-command (whose
// members are the project-XOs). This is the security-relevant containment — an
// "@name" never reaches outside the channel it was typed in.
func TestRelayMemberScopeIsolation(t *testing.T) {
	r, c, notices := newRelayHarness(fedCfg())
	// beta-be is a member of #beta, NOT of #alpha → unknown in #alpha → alpha-xo + notice.
	r.Handle("C_ALPHA", "1000014", "op", "@beta-be sneak in")
	// alpha-be is a desk, NOT a member of #fleet-command → unknown there → meta-xo + notice.
	r.Handle("C_CMD", "1000015", "op", "@alpha-be reach down")
	r.injector.Stop()

	if len(c.jobs) != 2 {
		t.Fatalf("delivered %d, want 2: %+v", len(c.jobs), c.jobs)
	}
	if c.jobs[0].Agent != "alpha-xo" {
		t.Errorf("cross-project @name should fall back to the channel's XO, got %+v", c.jobs[0])
	}
	if c.jobs[1].Agent != "meta-xo" {
		t.Errorf("a desk @name in fleet-command should fall back to the meta-XO, got %+v", c.jobs[1])
	}
	if len(*notices) != 2 {
		t.Errorf("notices = %d, want 2 (both out-of-scope @names)", len(*notices))
	}
}

// Operator-only auth holds PER channel in a federation, not just for the single-fleet
// case. (The webhook self-mirror drop moved to the transport adapter — see the note on
// TestRelayDropsNonOperator — so it is no longer exercised through Handle here.)
func TestRelayPerChannelAuth(t *testing.T) {
	r, c, _ := newRelayHarness(fedCfg())
	r.Handle("C_CMD", "1000017", "intruder", "@alpha-xo evil") // non-operator in fleet-command → dropped
	r.injector.Stop()
	if c.count() != 0 {
		t.Errorf("delivered %d, want 0 (per-channel non-operator must drop)", c.count())
	}
}

func TestRelayOnAcceptedReceivesRoutedTarget(t *testing.T) {
	cfg := legacyCfg()
	c := &collector{}
	inj := NewInjector(func(agent, msg string) error { c.enqueue(Job{Agent: agent, Message: msg}); return nil }, 8)
	inj.Start()
	var targets []string
	r := NewRelay(cfg, inj, func(target string) { targets = append(targets, target) }, nil)

	r.Handle("C1", "1000018", "op", "status please")    // bare → XO
	r.Handle("C1", "1000019", "op", "@backend ship it") // directed → desk
	r.Handle("C1", "1000021", "intruder", "evil")       // dropped (non-operator) → no onAccepted
	inj.Stop()

	if len(targets) != 2 || targets[0] != "xo" || targets[1] != "backend" {
		t.Errorf("onAccepted targets = %v, want [xo backend] (dropped messages must not fire)", targets)
	}
}
