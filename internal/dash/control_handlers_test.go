package dash

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/dash/control"
	"github.com/jim80net/flotilla/internal/outbox"
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

// TestControlRoute_DisableAuthentication allows writes when the operator enables
// insecure mode (env DISABLE_AUTHENTICATION) until bearer auth (#208) lands.
func TestControlRoute_DisableAuthentication(t *testing.T) {
	f := &fakeController{routeRes: control.RouteResult{Target: "alpha", Outcome: control.OutcomeDelivered}}
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	srv.cfg.DisableAuthentication = true
	srv.control = f
	req := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/control/route", strings.NewReader(`{"target":"alpha","message":"hi"}`))
	req.Host = "127.0.0.1:8787"
	// Deliberately omit X-Flotilla-Dash and Origin.
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DISABLE_AUTHENTICATION route → %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if f.lastRouteTarget != "alpha" {
		t.Errorf("route target = %q, want alpha", f.lastRouteTarget)
	}
}

// lanServer builds a non-loopback (LAN) bound server with the given configured write-gate
// origins — the fixture for the DNS-rebinding contract tests below.
func lanServer(t *testing.T, allowedOrigins []string) *Server {
	t.Helper()
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(singleFleetRoster), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(Config{RosterPath: rosterPath, Bind: "0.0.0.0:8787", AllowedOrigins: allowedOrigins, Transport: stubTransport{}, WebTransport: stubTransport{}})
	if err != nil {
		t.Fatal(err)
	}
	srv.control = &fakeController{routeRes: control.RouteResult{Target: "alpha", Outcome: control.OutcomeDelivered}}
	return srv
}

func lanRouteReq(originHost string) *http.Request {
	req := httptest.NewRequest("POST", "http://"+originHost+"/api/control/route", strings.NewReader(`{"target":"alpha","message":"hi"}`))
	req.Host = originHost
	req.Header.Set("X-Flotilla-Dash", "1")
	req.Header.Set("Origin", "http://"+originHost)
	return req
}

// TestControlRoute_LANRebindingOriginRejected: the DNS-rebinding attack — a page whose DNS
// resolves the attacker's own domain to the dash's LAN IP sends a matching Origin AND Host.
// A Host-relative check would pass; validating against the CONFIGURED allowlist rejects it.
// (Regression for #421 cubic P1 — the write gate must NOT trust the request Host header.)
func TestControlRoute_LANRebindingOriginRejected(t *testing.T) {
	srv := lanServer(t, []string{"http://192.168.1.5:8787"}) // operator declared their LAN origin
	req := lanRouteReq("attacker.example.com")               // but the forged request carries the attacker's origin+host
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("rebinding Origin matching request Host → %d, want 403 (DNS-rebinding must be rejected); body=%s", rec.Code, rec.Body.String())
	}
	if srv.control.(*fakeController).calls != 0 {
		t.Error("a rebinding-rejected write must never reach the controller")
	}
}

// TestControlRoute_LANConfiguredOriginAccepted: the operator's DECLARED LAN origin
// (FLOTILLA_DASH_ALLOWED_ORIGINS) is accepted — legitimate LAN access still works.
func TestControlRoute_LANConfiguredOriginAccepted(t *testing.T) {
	srv := lanServer(t, []string{"http://192.168.1.5:8787"})
	req := lanRouteReq("192.168.1.5:8787")
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("configured LAN Origin → %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestControlRoute_LANUnconfiguredOriginRejected: a LAN bind with NO declared origins fails
// closed for writes — the operator must declare their access origin (or DISABLE_AUTHENTICATION).
func TestControlRoute_LANUnconfiguredOriginRejected(t *testing.T) {
	srv := lanServer(t, nil)
	req := lanRouteReq("192.168.1.5:8787")
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unconfigured LAN Origin → %d, want 403 (fail-closed); body=%s", rec.Code, rec.Body.String())
	}
}

// --- respond (#501: the decision-response loop's delivery leg) ---

func respondOutbox(t *testing.T, srv *Server) []outbox.Entry {
	t.Helper()
	p, err := outbox.Path(filepath.Dir(srv.cfg.RosterPath), "operator")
	if err != nil {
		t.Fatal(err)
	}
	return outbox.NewStore(p).Load()
}

func TestControlRespond_DeliveredLive(t *testing.T) {
	f := &fakeController{routeRes: control.RouteResult{Target: "alpha", Outcome: control.OutcomeDelivered}}
	srv := controlServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/control/respond",
		`{"target":"alpha","goal_id":"g1","item":"approve budget","message":"Approved — take option A."}`)
	if rec.Code != 200 {
		t.Fatalf("code %d; body=%s", rec.Code, rec.Body.String())
	}
	var res respondDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Outcome != "delivered" || res.Target != "alpha" || res.QueuedID != "" {
		t.Errorf("delivered outcome wrong: %+v", res)
	}
	// The delivered body is self-describing: WHICH decision, then the operator's words.
	if want := "[operator decision response — g1 / approve budget] Approved — take option A."; f.lastRouteMsg != want {
		t.Errorf("composed message = %q, want %q", f.lastRouteMsg, want)
	}
	if got := respondOutbox(t, srv); len(got) != 0 {
		t.Errorf("a live delivery must not enqueue; outbox has %d entries", len(got))
	}
}

func TestControlRespond_QueuesDurablyOnBusy(t *testing.T) {
	f := &fakeController{routeRes: control.RouteResult{Target: "alpha", Outcome: control.OutcomeBusy, Detail: "desk is busy — retry"}}
	srv := controlServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/control/respond",
		`{"target":"alpha","goal_id":"g1","message":"Approved."}`)
	if rec.Code != 200 {
		t.Fatalf("code %d; body=%s", rec.Code, rec.Body.String())
	}
	var res respondDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Outcome != "queued" || res.QueuedID == "" {
		t.Fatalf("busy must queue durably, got %+v", res)
	}
	got := respondOutbox(t, srv)
	if len(got) != 1 {
		t.Fatalf("outbox entries = %d, want 1", len(got))
	}
	e := got[0]
	if e.Sender != "operator" || e.Recipient != "alpha" || !strings.Contains(e.Message, "Approved.") ||
		!strings.Contains(e.Message, "g1") || e.ID != res.QueuedID {
		t.Errorf("outbox entry wrong: %+v (res %+v)", e, res)
	}
}

func TestControlRespond_CrashedQueuesToo(t *testing.T) {
	// At-least-once means EVERY not-delivered outcome queues — including a crashed desk
	// (the sweep delivers after it is resumed), never a dead-end error.
	f := &fakeController{routeRes: control.RouteResult{Target: "alpha", Outcome: control.OutcomeCrashed, Detail: "desk is at a shell"}}
	srv := controlServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/control/respond", `{"target":"alpha","goal_id":"g1","message":"Go."}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"queued"`) {
		t.Fatalf("crashed must queue durably; code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestControlRespond_RepeatQueueDedupes(t *testing.T) {
	f := &fakeController{routeRes: control.RouteResult{Target: "alpha", Outcome: control.OutcomeBusy}}
	srv := controlServer(t, f)
	body := `{"target":"alpha","goal_id":"g1","message":"Approved."}`
	doWrite(t, srv, "POST", "/api/control/respond", body)
	rec := doWrite(t, srv, "POST", "/api/control/respond", body)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "already queued") {
		t.Fatalf("identical repeat must dedupe with an honest detail; body=%s", rec.Body.String())
	}
	if got := respondOutbox(t, srv); len(got) != 1 {
		t.Errorf("outbox entries = %d, want 1 (deduped)", len(got))
	}
}

func TestControlRespond_EmptyMessageRejected(t *testing.T) {
	// The OPERATOR's text is the guard target — the composed wrapper must not make an
	// empty response look non-empty.
	f := &fakeController{}
	srv := controlServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/control/respond", `{"target":"alpha","goal_id":"g1","message":"   "}`)
	if rec.Code != 400 {
		t.Fatalf("code %d, want 400", rec.Code)
	}
	if f.calls != 0 {
		t.Errorf("an empty response must never reach Route (calls=%d)", f.calls)
	}
}

func TestControlRespond_UnknownTarget404(t *testing.T) {
	f := &fakeController{routeErr: control.ErrUnknownTarget}
	srv := controlServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/control/respond", `{"target":"nobody","goal_id":"g1","message":"x"}`)
	if rec.Code != 404 {
		t.Fatalf("code %d, want 404", rec.Code)
	}
	if got := respondOutbox(t, srv); len(got) != 0 {
		t.Errorf("a hard route error must not enqueue; outbox has %d entries", len(got))
	}
}

func TestControlRespond_ItemOnlyRefAndEmptyTarget(t *testing.T) {
	// OCR #505 round: an item-only reference must not render a dangling " / ", and an
	// empty target fast-fails 404 without reaching Route.
	f := &fakeController{routeRes: control.RouteResult{Target: "alpha", Outcome: control.OutcomeDelivered}}
	srv := controlServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/control/respond", `{"target":"alpha","item":"approve budget","message":"Yes."}`)
	if rec.Code != 200 {
		t.Fatalf("code %d; body=%s", rec.Code, rec.Body.String())
	}
	if want := "[operator decision response — approve budget] Yes."; f.lastRouteMsg != want {
		t.Errorf("item-only ref composed %q, want %q", f.lastRouteMsg, want)
	}
	calls := f.calls
	rec = doWrite(t, srv, "POST", "/api/control/respond", `{"target":"  ","goal_id":"g1","message":"Yes."}`)
	if rec.Code != 404 {
		t.Fatalf("empty target: code %d, want 404", rec.Code)
	}
	if f.calls != calls {
		t.Errorf("an empty target must never reach Route")
	}
}
