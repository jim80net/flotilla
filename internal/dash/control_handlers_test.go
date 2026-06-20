package dash

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/dash/control"
)

// fakeController implements control.Controller, recording inputs and returning
// canned results/errors so handler tests don't touch the delivery library.
type fakeController struct {
	routeRes  control.RouteResult
	routeErr  error
	notifyErr error
	resumeRes control.ResumeResult
	resumeErr error

	lastRouteTarget, lastRouteMsg string
	lastNotifyMsg                 string
	lastResumeAgent               string
	calls                         int
}

func (f *fakeController) Route(_ context.Context, target, message string) (control.RouteResult, error) {
	f.calls++
	f.lastRouteTarget, f.lastRouteMsg = target, message
	return f.routeRes, f.routeErr
}

func (f *fakeController) Notify(_ context.Context, message string) error {
	f.calls++
	f.lastNotifyMsg = message
	return f.notifyErr
}

func (f *fakeController) Resume(_ context.Context, agent string) (control.ResumeResult, error) {
	f.calls++
	f.lastResumeAgent = agent
	return f.resumeRes, f.resumeErr
}

func controlServer(t *testing.T, f *fakeController) *Server {
	t.Helper()
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	srv.control = f
	return srv
}

// --- notify (live) ---

func TestControlNotify_HappyPath(t *testing.T) {
	f := &fakeController{}
	srv := controlServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/control/notify", `{"message":"stand by"}`)
	if rec.Code != 200 {
		t.Fatalf("code %d; body=%s", rec.Code, rec.Body.String())
	}
	if f.lastNotifyMsg != "stand by" {
		t.Errorf("notify message not forwarded: %q", f.lastNotifyMsg)
	}
}

func TestControlNotify_EmptyRejected(t *testing.T) {
	f := &fakeController{notifyErr: control.ErrEmptyMessage}
	srv := controlServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/control/notify", `{"message":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code %d, want 400", rec.Code)
	}
}

func TestControlNotify_WebhookMissing(t *testing.T) {
	f := &fakeController{notifyErr: control.ErrWebhookMissing}
	srv := controlServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/control/notify", `{"message":"x"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code %d, want 503", rec.Code)
	}
}

// --- route / resume (gated on the pane lock → 503) ---

func TestControlRoute_ForwardsInput(t *testing.T) {
	f := &fakeController{routeRes: control.RouteResult{Target: "alpha", Outcome: control.OutcomeDelivered}}
	srv := controlServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/control/route", `{"target":"alpha","message":"do X"}`)
	if rec.Code != 200 {
		t.Fatalf("code %d; body=%s", rec.Code, rec.Body.String())
	}
	if f.lastRouteTarget != "alpha" || f.lastRouteMsg != "do X" {
		t.Errorf("route input not forwarded: %q / %q", f.lastRouteTarget, f.lastRouteMsg)
	}
}

func TestControlRoute_HappyOutcome(t *testing.T) {
	// When the lock lands and Route succeeds, the typed outcome is returned 200.
	f := &fakeController{routeRes: control.RouteResult{Target: "alpha", Outcome: control.OutcomeDelivered}}
	srv := controlServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/control/route", `{"target":"alpha","message":"do X"}`)
	if rec.Code != 200 {
		t.Fatalf("code %d; body=%s", rec.Code, rec.Body.String())
	}
	var res control.RouteResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Outcome != control.OutcomeDelivered {
		t.Errorf("outcome = %q", res.Outcome)
	}
}

func TestControlRoute_BusyOutcomeIs200NotError(t *testing.T) {
	// busy/crashed/unconfirmed are informational outcomes (the operator must see
	// them), surfaced as 200 with the outcome — never a bare failure.
	f := &fakeController{routeRes: control.RouteResult{Target: "alpha", Outcome: control.OutcomeBusy, Detail: "desk is busy — retry"}}
	srv := controlServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/control/route", `{"target":"alpha","message":"do X"}`)
	if rec.Code != 200 {
		t.Fatalf("code %d, want 200 (busy is an outcome, not an error)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "busy") {
		t.Errorf("busy outcome not surfaced: %s", rec.Body.String())
	}
}

func TestControlResume_GatedReturns503(t *testing.T) {
	f := &fakeController{resumeErr: control.ErrResumeUnavailable}
	srv := controlServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/control/resume", `{"agent":"alpha"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code %d, want 503", rec.Code)
	}
	if f.lastResumeAgent != "alpha" {
		t.Errorf("resume agent not forwarded: %q", f.lastResumeAgent)
	}
}

// --- CSRF gate applies to control writes too ---

func TestControl_MissingCustomHeaderRejected(t *testing.T) {
	f := &fakeController{}
	srv := controlServer(t, f)
	for _, path := range []string{"/api/control/route", "/api/control/notify", "/api/control/resume"} {
		req := httptest.NewRequest("POST", "http://127.0.0.1:8787"+path, strings.NewReader(`{"message":"x","target":"a","agent":"a"}`))
		req.Host = "127.0.0.1:8787"
		req.Header.Set("Origin", "http://127.0.0.1:8787")
		// No X-Flotilla-Dash header.
		rec := httptest.NewRecorder()
		srv.handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s without custom header → %d, want 403", path, rec.Code)
		}
	}
	if f.calls != 0 {
		t.Error("a CSRF-rejected control request must not reach the controller")
	}
}

func TestControl_GETOnControlRouteRejected(t *testing.T) {
	f := &fakeController{}
	srv := controlServer(t, f)
	rec := doGet(t, srv, "/api/control/notify")
	if rec.Code == 200 {
		t.Fatalf("a GET on a control write route must not succeed (code %d)", rec.Code)
	}
	if f.calls != 0 {
		t.Error("a GET must never reach a control handler")
	}
}
