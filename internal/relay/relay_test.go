package relay

import "testing"

// TestAccept pins the relay's operator-author filter. NOTE: the self-mirror feedback
// guard (dropping the channel's own webhook posts) NO LONGER lives here — it moved
// into the transport adapter (internal/transport's selfMirrorGuardAdapter), where it
// is pinned author-agnostically, including the adversarial sender==operator case. So
// Accept's signature folded webhookID out: Accept(authorID, operatorID).
func TestAccept(t *testing.T) {
	const op = "111111111111111111"
	if !Accept(op, op) {
		t.Error("operator message rejected; should be accepted")
	}
	if Accept("someone-else", op) {
		t.Error("non-operator message accepted; should be dropped")
	}
	if Accept("", op) {
		t.Error("empty author accepted; should be dropped")
	}
}

// resolve is a case-insensitive roster stub for routing tests.
func resolve(token string) (string, bool) {
	agents := []string{"xo", "frontend", "backend"}
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
	d := Route("status check please", "xo", resolve)
	if d.Agent != "xo" || d.Message != "status check please" || d.Notice != "" {
		t.Errorf("bare → XO: %+v", d)
	}
}

func TestRouteDirectedMultiline(t *testing.T) {
	d := Route("@frontend do X\nthen Y\nthen Z", "xo", resolve)
	if d.Agent != "frontend" {
		t.Errorf("agent = %q, want frontend", d.Agent)
	}
	if d.Message != "do X\nthen Y\nthen Z" {
		t.Errorf("multi-line body not preserved verbatim: %q", d.Message)
	}
}

func TestRouteCaseInsensitive(t *testing.T) {
	d := Route("@FrontEnd hi", "xo", resolve)
	if d.Agent != "frontend" || d.Message != "hi" {
		t.Errorf("case-insensitive route: %+v", d)
	}
}

func TestRouteUnknownAgentFallsBackWithNotice(t *testing.T) {
	d := Route("@nope do X", "xo", resolve)
	if d.Agent != "xo" {
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
	d := Route("@@here please look", "xo", resolve)
	if d.Agent != "xo" || d.Message != "@here please look" {
		t.Errorf("@@ escape: %+v", d)
	}
}

func TestRouteBareAtNoBody(t *testing.T) {
	d := Route("@frontend", "xo", resolve)
	if d.Agent != "xo" || d.Message != "@frontend" {
		t.Errorf("@name with no body should go to XO verbatim: %+v", d)
	}
}
