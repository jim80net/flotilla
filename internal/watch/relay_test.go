package watch

import (
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
)

func testRelay() (*Relay, *collector, *[]string) {
	cfg := &roster.Config{
		OperatorUserID: "op",
		Agents:         []roster.Agent{{Name: "hydra-ops"}, {Name: "v12-dev"}},
	}
	c := &collector{}
	inj := NewInjector(func(agent, msg string) error { c.enqueue(Job{Agent: agent, Message: msg}); return nil }, 8)
	inj.Start()
	var notices []string
	r := NewRelay(cfg, "hydra-ops", inj, nil, func(s string) { notices = append(notices, s) })
	return r, c, &notices
}

func TestRelayDropsWebhookAndNonOperator(t *testing.T) {
	r, c, _ := testRelay()
	r.Handle("webhook-1", "op", "→ v12-dev: mirror echo") // our own mirror → dropped
	r.Handle("", "intruder", "do evil")                   // non-operator → dropped
	r.injector.Stop()                                     // drain
	if c.count() != 0 {
		t.Errorf("delivered %d, want 0 (webhook + non-operator must be dropped)", c.count())
	}
}

func TestRelayRoutesOperatorMessage(t *testing.T) {
	r, c, notices := testRelay()
	r.Handle("", "op", "@v12-dev ship it")
	r.Handle("", "op", "status please") // bare → XO
	r.Handle("", "op", "@nope hello")   // unknown → XO + notice
	r.injector.Stop()                   // drain

	if len(c.jobs) != 3 {
		t.Fatalf("delivered %d, want 3", len(c.jobs))
	}
	if c.jobs[0].Agent != "v12-dev" || c.jobs[0].Message != "ship it" {
		t.Errorf("directed route wrong: %+v", c.jobs[0])
	}
	if c.jobs[1].Agent != "hydra-ops" || c.jobs[1].Message != "status please" {
		t.Errorf("bare route wrong: %+v", c.jobs[1])
	}
	if c.jobs[2].Agent != "hydra-ops" {
		t.Errorf("unknown-agent should fall back to XO: %+v", c.jobs[2])
	}
	if len(*notices) != 1 {
		t.Errorf("notices = %d, want 1 (unknown agent)", len(*notices))
	}
}

func TestRelayOnAcceptedReceivesRoutedTarget(t *testing.T) {
	cfg := &roster.Config{
		OperatorUserID: "op",
		Agents:         []roster.Agent{{Name: "hydra-ops"}, {Name: "v12-dev"}},
	}
	c := &collector{}
	inj := NewInjector(func(agent, msg string) error { c.enqueue(Job{Agent: agent, Message: msg}); return nil }, 8)
	inj.Start()
	var targets []string
	r := NewRelay(cfg, "hydra-ops", inj, func(target string) { targets = append(targets, target) }, nil)

	r.Handle("", "op", "status please")     // bare → XO
	r.Handle("", "op", "@v12-dev ship it")  // directed → desk
	r.Handle("webhook-1", "op", "→ mirror") // dropped → no onAccepted
	r.Handle("", "intruder", "evil")        // dropped → no onAccepted
	inj.Stop()

	if len(targets) != 2 || targets[0] != "hydra-ops" || targets[1] != "v12-dev" {
		t.Errorf("onAccepted targets = %v, want [hydra-ops v12-dev] (dropped messages must not fire)", targets)
	}
}
