package relay

import "testing"

func TestAccept(t *testing.T) {
	const op = "111111111111111111"
	// The single most important test: a webhook post (our own mirror) is dropped
	// even if it somehow carried the operator's id — no feedback loop.
	if Accept("webhook-123", op, op) {
		t.Error("webhook message accepted; must be dropped (feedback guard)")
	}
	if !Accept("", op, op) {
		t.Error("operator message rejected; should be accepted")
	}
	if Accept("", "someone-else", op) {
		t.Error("non-operator message accepted; should be dropped")
	}
	if Accept("", "", op) {
		t.Error("empty author accepted; should be dropped")
	}
}

// resolve is a case-insensitive roster stub for routing tests.
func resolve(token string) (string, bool) {
	agents := []string{"alpha-xo", "desk-a", "desk-g"}
	for _, a := range agents {
		if equalFold(token, a) {
			return a, true
		}
	}
	return "", false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func TestRouteBareToXO(t *testing.T) {
	d := Route("status check please", "alpha-xo", resolve)
	if d.Agent != "alpha-xo" || d.Message != "status check please" || d.Notice != "" {
		t.Errorf("bare → XO: %+v", d)
	}
}

func TestRouteDirectedMultiline(t *testing.T) {
	d := Route("@desk-a do X\nthen Y\nthen Z", "alpha-xo", resolve)
	if d.Agent != "desk-a" {
		t.Errorf("agent = %q, want desk-a", d.Agent)
	}
	if d.Message != "do X\nthen Y\nthen Z" {
		t.Errorf("multi-line body not preserved verbatim: %q", d.Message)
	}
}

func TestRouteCaseInsensitive(t *testing.T) {
	d := Route("@Desk-A hi", "alpha-xo", resolve)
	if d.Agent != "desk-a" || d.Message != "hi" {
		t.Errorf("case-insensitive route: %+v", d)
	}
}

func TestRouteUnknownAgentFallsBackWithNotice(t *testing.T) {
	d := Route("@nope do X", "alpha-xo", resolve)
	if d.Agent != "alpha-xo" {
		t.Errorf("unknown agent should route to XO, got %q", d.Agent)
	}
	if d.Message != "@nope do X" {
		t.Errorf("unknown agent should deliver whole body to XO, got %q", d.Message)
	}
	if d.Notice == "" {
		t.Error("unknown agent should produce a notice")
	}
}

func TestRouteEscapeToXO(t *testing.T) {
	d := Route("@@here please look", "alpha-xo", resolve)
	if d.Agent != "alpha-xo" || d.Message != "@here please look" {
		t.Errorf("@@ escape: %+v", d)
	}
}

func TestRouteBareAtNoBody(t *testing.T) {
	d := Route("@desk-a", "alpha-xo", resolve)
	if d.Agent != "alpha-xo" || d.Message != "@desk-a" {
		t.Errorf("@name with no body should go to XO verbatim: %+v", d)
	}
}
