package dash

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/dash/control"
)

// TestWebControlIngress_IsGated is the graft-level security test (web-transport task
// 5.2): the web transport's ONLY inbound ingress is the EXISTING
// requireWrite + Host-allowlist-gated POST /api/control/route route — no new ungated
// path was introduced. It pins three facets of that one gated ingress:
//
//  1. a foreign Host is rejected by the anti-DNS-rebinding Host allowlist (the
//     controller is never reached);
//  2. a missing browser-CSRF custom header is rejected by requireWrite (the
//     controller is never reached);
//  3. a properly-gated request DOES reach the controller (the route works through
//     the existing gates — the web inbound rides the dash's tested defenses, adding
//     no new listener or auth surface).
//
// Together these prove the web coordination ingress is the SAME gated HTTP route the
// dash already exposes (design Decision 3 / spec "Web inbound is the ONE gated HTTP
// route; Subscribe is a no-op"), reusing requireWrite + Host + Origin unchanged.
func TestWebControlIngress_IsGated(t *testing.T) {
	f := &fakeController{routeRes: control.RouteResult{Target: "alpha", Outcome: control.OutcomeDelivered}}
	srv := controlServer(t, f)
	const path = "/api/control/route"
	const body = `{"target":"alpha","message":"do the thing"}`

	// (1) Foreign Host → 403, controller untouched (anti-DNS-rebinding).
	t.Run("foreign Host is rejected", func(t *testing.T) {
		f.calls = 0
		req := httptest.NewRequest("POST", "http://evil.example.com"+path, strings.NewReader(body))
		req.Host = "evil.example.com"
		req.Header.Set("X-Flotilla-Dash", "1")
		req.Header.Set("Origin", "http://evil.example.com")
		rec := httptest.NewRecorder()
		srv.handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("foreign Host on the web ingress → %d, want 403", rec.Code)
		}
		if f.calls != 0 {
			t.Error("a Host-rejected web instruction must not reach the controller")
		}
	})

	// (2) Missing custom header → 403, controller untouched (browser-CSRF).
	t.Run("missing custom header is rejected", func(t *testing.T) {
		f.calls = 0
		req := httptest.NewRequest("POST", "http://127.0.0.1:8787"+path, strings.NewReader(body))
		req.Host = "127.0.0.1:8787"
		req.Header.Set("Origin", "http://127.0.0.1:8787")
		// No X-Flotilla-Dash header.
		rec := httptest.NewRecorder()
		srv.handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("missing custom header on the web ingress → %d, want 403", rec.Code)
		}
		if f.calls != 0 {
			t.Error("a CSRF-rejected web instruction must not reach the controller")
		}
	})

	// (3) Properly gated → reaches the controller (the route works through the gates).
	t.Run("gated request reaches the controller", func(t *testing.T) {
		f.calls = 0
		rec := doWrite(t, srv, "POST", path, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("gated web instruction → %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if f.lastRouteTarget != "alpha" {
			t.Errorf("gated web instruction not forwarded to the controller: target=%q", f.lastRouteTarget)
		}
	})
}
