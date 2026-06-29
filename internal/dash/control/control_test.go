package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/cos"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/transport"
)

// fixedTime keeps ledger entries deterministic.
var fixedTime = time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)

// newTestController builds a LibraryController over an in-memory roster + a temp
// secrets file, with the discord/cos seams captured for assertions.
func newTestController(t *testing.T, rosterBody, secretsBody string) (*LibraryController, *capture) {
	t.Helper()
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(rosterBody), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatalf("roster.Load: %v", err)
	}
	secretsPath := ""
	if secretsBody != "" {
		secretsPath = filepath.Join(dir, "secrets.env")
		if err := os.WriteFile(secretsPath, []byte(secretsBody), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	xo := rc.XOAgent
	if xo == "" {
		xo = rc.Agents[0].Name
	}
	// Inject a discord-backed NOTIFY transport stand-in reporting the Discord 2000-rune
	// cap (so the over-length guard byte-pins to the previous discord.MaxContentRunes
	// behavior), plus a WEB transport stand-in for the route's inbound resolution. The
	// post seam is overridden below with cap.post, so these tests pin notify/route behavior
	// independent of the transport's Post — the constructor CALL updates for the new params;
	// the seam-override ASSERTIONS stay byte-pinned. As of PR3 (#198) Route resolves through
	// the web transport's ResolveDestination; the capture's resolveDest seam (wired below)
	// replicates exactly what the real web transport does — resolve the target through the
	// SHARED roster.ResolveTarget, then resolvePane(agent.Title()) — so the byte-pinned Route
	// assertions (resolvePane asked for the Title, lock keyed on the resolved pane, distinct
	// unknown/ambiguous errors) hold UNCHANGED against the new seam.
	c := NewLibrary(rc, xo, secretsPath, &fakeNotifyTransport{maxRunes: 2000}, &fakeNotifyTransport{maxRunes: 2000})
	cap := &capture{paneTarget: "%5", rc: rc, xo: xo}
	c.post = cap.post
	c.appendCos = cap.append
	c.now = func() time.Time { return fixedTime }
	// Route seams: record the resolve→acquire→submit→release sequence so tests can
	// assert the lock keys on the RESOLVED pane target and brackets the submit.
	c.resolveDest = cap.resolveDest
	c.acquireTxn = cap.acquireTxn
	c.submit = cap.submit
	return c, cap
}

type capture struct {
	postWebhook, postUser, postContent string
	postErr                            error
	postCalls                          int
	ledger                             []cos.Entry
	ledgerErr                          error

	// Route seam recording. rc + xo let resolveDest replicate the real web transport's
	// resolution (the shared roster.ResolveTarget) so the distinct unknown/ambiguous
	// sentinels and the Title()-keyed pane resolution are exercised exactly as in prod.
	rc             *roster.Config
	xo             string
	paneTarget     string // what resolvePane returns
	resolvedTitle  string // the title resolvePane was asked for
	resolvePaneErr error
	txnTarget      string // the target acquireTxn was keyed on
	acquireErr     error  // simulate lock contention
	submitDrv      surface.Driver
	submitPane     string
	submitText     string
	submitErr      error
	events         []string // ordered: "resolve","acquire","submit","release"
}

// resolveDest stands in for the web transport's ResolveDestination wired into the
// controller's resolveDest seam. It replicates the real web transport EXACTLY: resolve
// the target through the SHARED roster.ResolveTarget (so the distinct ErrUnknownTarget /
// ErrAmbiguousTarget sentinels propagate), then resolvePane(agent.Title()) for the pane
// target (the cross-process lock key). originChannel is ignored (roster-wide, Decision 2).
func (c *capture) resolveDest(_, target string) (string, string, error) {
	agentName, err := c.rc.ResolveTarget(c.xo, target)
	if err != nil {
		return "", "", err
	}
	agent, err := c.rc.Agent(agentName)
	if err != nil {
		return "", "", ErrUnknownTarget
	}
	pane, err := c.resolvePane(agent.Title())
	if err != nil {
		return "", "", err
	}
	return agentName, pane, nil
}

func (c *capture) resolvePane(title string) (string, error) {
	c.resolvedTitle = title
	c.events = append(c.events, "resolve")
	if c.resolvePaneErr != nil {
		return "", c.resolvePaneErr
	}
	return c.paneTarget, nil
}

func (c *capture) acquireTxn(target string) (func(), error) {
	c.txnTarget = target
	if c.acquireErr != nil {
		return nil, c.acquireErr
	}
	c.events = append(c.events, "acquire")
	return func() { c.events = append(c.events, "release") }, nil
}

func (c *capture) submit(drv surface.Driver, pane, text string) error {
	c.submitDrv, c.submitPane, c.submitText = drv, pane, text
	c.events = append(c.events, "submit")
	return c.submitErr
}

func (c *capture) post(webhook, username, content string) error {
	c.postCalls++
	c.postWebhook, c.postUser, c.postContent = webhook, username, content
	return c.postErr
}

func (c *capture) append(_ string, e cos.Entry) error {
	c.ledger = append(c.ledger, e)
	return c.ledgerErr
}

const rosterCos = `{
	"channel_id": "C1",
	"xo_agent": "xo",
	"cos_agent": "xo",
	"heartbeat_interval": "20m",
	"agents": [{"name": "xo"}, {"name": "alpha"}]
}`

const secretsXO = "FLOTILLA_WEBHOOK_XO=https://discord.example/webhook/xo\n"

func TestNotify_HappyPath_PostsAndMirrors(t *testing.T) {
	c, cap := newTestController(t, rosterCos, secretsXO)
	if err := c.Notify(context.Background(), "fleet, stand by for a deploy"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if cap.postCalls != 1 || cap.postWebhook != "https://discord.example/webhook/xo" {
		t.Errorf("post = %d calls, webhook %q", cap.postCalls, cap.postWebhook)
	}
	if cap.postUser != dashProvenance {
		t.Errorf("post username = %q, want %q", cap.postUser, dashProvenance)
	}
	if cap.postContent != "fleet, stand by for a deploy" {
		t.Errorf("post content = %q", cap.postContent)
	}
	// CoS mirror with dash provenance.
	if len(cap.ledger) != 1 {
		t.Fatalf("ledger entries = %d, want 1", len(cap.ledger))
	}
	e := cap.ledger[0]
	if e.From != dashProvenance || e.To != "xo" || e.Gist != "fleet, stand by for a deploy" {
		t.Errorf("ledger entry = %+v", e)
	}
}

func TestNotify_EmptyMessage(t *testing.T) {
	c, cap := newTestController(t, rosterCos, secretsXO)
	if err := c.Notify(context.Background(), "   "); !errors.Is(err, ErrEmptyMessage) {
		t.Errorf("err = %v, want ErrEmptyMessage", err)
	}
	if cap.postCalls != 0 {
		t.Error("empty message must not post")
	}
}

func TestNotify_OverLength(t *testing.T) {
	c, _ := newTestController(t, rosterCos, secretsXO)
	long := make([]byte, 2001)
	for i := range long {
		long[i] = 'x'
	}
	if err := c.Notify(context.Background(), string(long)); !errors.Is(err, ErrOverLength) {
		t.Errorf("err = %v, want ErrOverLength", err)
	}
}

func TestNotify_NoSecretsConfigured(t *testing.T) {
	c, _ := newTestController(t, rosterCos, "") // no secrets file
	if err := c.Notify(context.Background(), "hi"); !errors.Is(err, ErrWebhookMissing) {
		t.Errorf("err = %v, want ErrWebhookMissing", err)
	}
}

func TestNotify_WebhookMissingForXO(t *testing.T) {
	// Secrets file exists but has no webhook for the XO.
	c, _ := newTestController(t, rosterCos, "FLOTILLA_WEBHOOK_OTHER=https://x\n")
	if err := c.Notify(context.Background(), "hi"); !errors.Is(err, ErrWebhookMissing) {
		t.Errorf("err = %v, want ErrWebhookMissing", err)
	}
}

func TestNotify_PostFailureSurfaced(t *testing.T) {
	c, cap := newTestController(t, rosterCos, secretsXO)
	cap.postErr = errors.New("discord 500")
	if err := c.Notify(context.Background(), "hi"); err == nil {
		t.Fatal("a post failure must be surfaced")
	}
	// A failed post must NOT mirror to the ledger (the post didn't happen).
	if len(cap.ledger) != 0 {
		t.Error("no ledger entry on post failure")
	}
}

func TestNotify_LedgerFailureIsBestEffort(t *testing.T) {
	c, cap := newTestController(t, rosterCos, secretsXO)
	cap.ledgerErr = errors.New("ledger write failed")
	// The post succeeded; a ledger failure must NOT fail the notify.
	if err := c.Notify(context.Background(), "hi"); err != nil {
		t.Errorf("ledger failure must not fail notify, got %v", err)
	}
}

func TestNotify_NoCosLedgerSkipsMirror(t *testing.T) {
	// Roster without cos_agent ⇒ CosLedger inert ⇒ no mirror, notify still posts.
	rosterNoCos := `{"channel_id":"C1","xo_agent":"xo","heartbeat_interval":"20m","agents":[{"name":"xo"}]}`
	c, cap := newTestController(t, rosterNoCos, secretsXO)
	if err := c.Notify(context.Background(), "hi"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if cap.postCalls != 1 {
		t.Error("notify should still post with an inert CoS")
	}
	if len(cap.ledger) != 0 {
		t.Error("inert CoS ⇒ no ledger mirror")
	}
}

// --- Route (confirmed delivery, txn-lock serialized) ---

// TestRoute_KeysLockOnResolvedPaneTarget is THE cross-process-correctness guard
// flotilla-dev asked for: the dash MUST resolve the pane via
// deliver.ResolvePane(agent.Title()) and key the transaction lock on that
// resolved target — IDENTICAL to cmdSend (cmd/flotilla/main.go:322,332) and the
// watch Injector — or the lock keys diverge and silently fail to serialize. This
// asserts (a) resolvePane is asked for the agent's TITLE, (b) the lock is keyed
// on resolvePane's OUTPUT (not the agent name), (c) submit runs on that same pane.
func TestRoute_KeysLockOnResolvedPaneTarget(t *testing.T) {
	c, cap := newTestController(t, rosterCos, secretsXO)
	cap.paneTarget = "flotilla:3.1" // what deliver.ResolvePane would return
	res, err := c.Route(context.Background(), "alpha", "do the thing")
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if cap.resolvedTitle != agentTitle(t, c, "alpha") {
		t.Errorf("resolvePane asked for %q, want the agent's Title()", cap.resolvedTitle)
	}
	if cap.txnTarget != "flotilla:3.1" {
		t.Errorf("txn lock keyed on %q, want the resolved pane target %q (NOT the agent name)", cap.txnTarget, "flotilla:3.1")
	}
	if cap.submitPane != "flotilla:3.1" {
		t.Errorf("submit ran on pane %q, want the resolved target", cap.submitPane)
	}
	if res.Outcome != OutcomeDelivered {
		t.Errorf("outcome = %q, want delivered", res.Outcome)
	}
}

// TestRoute_LockBracketsSubmit asserts the txn lock is acquired BEFORE the submit
// and released AFTER — the ordering that prevents a dash route from interleaving
// with a watch rotate (design §5). (Replaces the Phase-3a import-guard test, whose
// "control links no pane-driving code" invariant is intentionally retired now that
// route legitimately drives a pane through the lock.)
func TestRoute_LockBracketsSubmit(t *testing.T) {
	c, cap := newTestController(t, rosterCos, secretsXO)
	if _, err := c.Route(context.Background(), "alpha", "x"); err != nil {
		t.Fatal(err)
	}
	want := []string{"resolve", "acquire", "submit", "release"}
	if strings.Join(cap.events, ",") != strings.Join(want, ",") {
		t.Errorf("call order = %v, want %v (lock must bracket submit)", cap.events, want)
	}
}

func TestRoute_DeliveredMirrorsToLedger(t *testing.T) {
	c, cap := newTestController(t, rosterCos, secretsXO)
	if _, err := c.Route(context.Background(), "alpha", "ship it"); err != nil {
		t.Fatal(err)
	}
	if len(cap.ledger) != 1 || cap.ledger[0].From != dashProvenance || cap.ledger[0].To != "alpha" || cap.ledger[0].Gist != "ship it" {
		t.Errorf("ledger = %+v", cap.ledger)
	}
}

func TestRoute_EmptyTargetGoesToXO(t *testing.T) {
	c, cap := newTestController(t, rosterCos, secretsXO)
	res, err := c.Route(context.Background(), "", "x")
	if err != nil {
		t.Fatal(err)
	}
	if res.Target != "xo" {
		t.Errorf("empty target → %q, want the XO", res.Target)
	}
	_ = cap
}

func TestRoute_ResolvesCaseInsensitiveAndAtPrefix(t *testing.T) {
	c, _ := newTestController(t, rosterCos, secretsXO)
	for _, target := range []string{"ALPHA", "@alpha", "@ALPHA"} {
		res, err := c.Route(context.Background(), target, "x")
		if err != nil {
			t.Fatalf("Route(%q): %v", target, err)
		}
		if res.Target != "alpha" {
			t.Errorf("target %q → %q, want alpha", target, res.Target)
		}
	}
}

func TestRoute_CaseCollisionExactWinsElseAmbiguous(t *testing.T) {
	// Roster names are unique only case-sensitively, so "alpha" + "Alpha" coexist.
	rosterDup := `{"channel_id":"C1","xo_agent":"xo","cos_agent":"xo","heartbeat_interval":"20m","agents":[{"name":"xo"},{"name":"alpha"},{"name":"Alpha"}]}`
	// An EXACT match is unambiguous and delivers.
	c, _ := newTestController(t, rosterDup, secretsXO)
	res, err := c.Route(context.Background(), "Alpha", "x")
	if err != nil || res.Target != "Alpha" {
		t.Fatalf("exact 'Alpha' → (%q, %v), want delivered to Alpha", res.Target, err)
	}
	// A case-insensitive collision with NO exact match is rejected, not guessed.
	c2, cap2 := newTestController(t, rosterDup, secretsXO)
	if _, err := c2.Route(context.Background(), "ALPHA", "x"); !errors.Is(err, ErrAmbiguousTarget) {
		t.Errorf("ambiguous 'ALPHA' → %v, want ErrAmbiguousTarget", err)
	}
	if len(cap2.events) != 0 {
		t.Error("an ambiguous target must not reach the pane")
	}
}

func TestRoute_UnknownTarget(t *testing.T) {
	c, cap := newTestController(t, rosterCos, secretsXO)
	if _, err := c.Route(context.Background(), "ghost", "x"); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("err = %v, want ErrUnknownTarget", err)
	}
	if len(cap.events) != 0 {
		t.Error("an unknown target must not reach the pane (no resolve/acquire/submit)")
	}
}

func TestRoute_EmptyMessage(t *testing.T) {
	c, _ := newTestController(t, rosterCos, secretsXO)
	if _, err := c.Route(context.Background(), "alpha", "  "); !errors.Is(err, ErrEmptyMessage) {
		t.Errorf("err = %v, want ErrEmptyMessage", err)
	}
}

func TestRoute_LockContentionIsBusyNotError(t *testing.T) {
	c, cap := newTestController(t, rosterCos, secretsXO)
	cap.acquireErr = errors.New("pane txn lock busy: timed out")
	res, err := c.Route(context.Background(), "alpha", "x")
	if err != nil {
		t.Fatalf("contention must be an outcome, not an error: %v", err)
	}
	if res.Outcome != OutcomeBusy {
		t.Errorf("contention outcome = %q, want busy", res.Outcome)
	}
	// A contended lock must NOT submit (no silent partial) and must NOT mirror.
	for _, e := range cap.events {
		if e == "submit" {
			t.Error("must not submit when the lock is contended")
		}
	}
	if len(cap.ledger) != 0 {
		t.Error("contended route must not mirror to the ledger")
	}
}

func TestRoute_SubmitOutcomesMapped(t *testing.T) {
	cases := []struct {
		err  error
		want RouteOutcome
	}{
		{surface.ErrBusy, OutcomeBusy},
		{surface.ErrCrashed, OutcomeCrashed},
		{surface.ErrTransient, OutcomeTransient},
		{surface.ErrUnconfirmed, OutcomeUnconfirmed},
	}
	for _, tc := range cases {
		c, cap := newTestController(t, rosterCos, secretsXO)
		cap.submitErr = tc.err
		res, err := c.Route(context.Background(), "alpha", "x")
		if err != nil {
			t.Fatalf("%v: unexpected error %v", tc.err, err)
		}
		if res.Outcome != tc.want {
			t.Errorf("%v → outcome %q, want %q", tc.err, res.Outcome, tc.want)
		}
		// A non-delivered outcome must NOT mirror, but the lock MUST be released.
		if len(cap.ledger) != 0 {
			t.Errorf("%v: non-delivered must not mirror", tc.err)
		}
		if cap.events[len(cap.events)-1] != "release" {
			t.Errorf("%v: lock must be released after a failed submit (events=%v)", tc.err, cap.events)
		}
	}
}

// recordingWebTransport is a web-transport stand-in for the PR3 drift guard: it
// records the (originChannel, target) ResolveDestination was called with and returns a
// fixed InboundTarget, so the test can prove the production controller's Route keys the
// lock on the pane the WEB TRANSPORT resolved — not on a forked in-Route resolution.
type recordingWebTransport struct {
	transport.Transport  // embed so the unused methods are inherited (nil-safe: only ResolveDestination is called)
	gotOrigin, gotTarget string
	dest                 transport.Destination
	agent                string
	ok                   bool
}

func (r *recordingWebTransport) ResolveDestination(origin, target string) (transport.Destination, string, bool) {
	r.gotOrigin, r.gotTarget = origin, target
	return r.dest, r.agent, r.ok
}

// TestNewLibrary_RoutesThroughWebTransport is the PR3 (#198) DRIFT guard: the production
// controller MUST resolve its route target+pane THROUGH the injected web transport's
// ResolveDestination and key the lock on the pane THAT resolver returned — never a forked
// in-Route resolution. The seam-stubbed Route tests above prove the plumbing; this proves
// NewLibrary actually wires the seam to the injected webTr (a future refactor that
// re-introduced an in-Route resolvePane would diverge the lock key and silently break
// cross-process serialization). The companion fact — the web transport wires
// deliver.ResolvePane (the SAME function cmdSend + the watch Injector use) — is pinned in
// internal/transport (TestWebTransport_WiresRealResolvePane); together they prove the dash
// route's lock key == deliver.ResolvePane(agent.Title()) == the discord writer's key.
func TestNewLibrary_RoutesThroughWebTransport(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(rosterCos), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	webTr := &recordingWebTransport{
		dest:  transport.NewInboundTarget("alpha", "flotilla:9.2"),
		agent: "alpha",
		ok:    true,
	}
	c := NewLibrary(rc, "xo", "", &fakeNotifyTransport{maxRunes: 2000}, webTr)
	// Drive Route with the real resolveDest seam, capturing only the lock + submit.
	cap := &capture{}
	c.acquireTxn = cap.acquireTxn
	c.submit = cap.submit
	res, err := c.Route(context.Background(), "@alpha", "go")
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if webTr.gotTarget != "@alpha" {
		t.Errorf("web ResolveDestination target = %q, want the route target @alpha (Route must resolve THROUGH the web transport)", webTr.gotTarget)
	}
	if webTr.gotOrigin != "" {
		t.Errorf("web ResolveDestination originChannel = %q, want empty (roster-wide, Decision 2)", webTr.gotOrigin)
	}
	if cap.txnTarget != "flotilla:9.2" {
		t.Errorf("txn lock keyed on %q, want the pane the WEB TRANSPORT resolved (flotilla:9.2) — not a forked in-Route resolution", cap.txnTarget)
	}
	if cap.submitPane != "flotilla:9.2" {
		t.Errorf("submit ran on pane %q, want the web-transport-resolved pane", cap.submitPane)
	}
	if res.Target != "alpha" || res.Outcome != OutcomeDelivered {
		t.Errorf("result = %+v, want delivered to alpha", res)
	}
}

// newRouteErrorController builds the production controller (real NewLibrary-built
// resolveDest closure) over an in-memory roster, injecting the given web transport.
// It captures the lock/submit seams so a test can prove an error arm NEVER reaches
// the pane (no acquire/submit), and drives Route through the REAL production closure
// — NOT the capture's fake resolveDest. This is the lever the seam-stubbed Route
// tests cannot pull: those override c.resolveDest, so the production closure's three
// ok=false error arms + its non-inbound fail-closed arm are otherwise unexercised.
func newRouteErrorController(t *testing.T, rosterBody string, webTr transport.Transport) (*LibraryController, *capture) {
	t.Helper()
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(rosterBody), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatalf("roster.Load: %v", err)
	}
	xo := rc.XOAgent
	if xo == "" {
		xo = rc.Agents[0].Name
	}
	c := NewLibrary(rc, xo, "", &fakeNotifyTransport{maxRunes: 2000}, webTr)
	cap := &capture{}
	// Leave c.resolveDest as the REAL production closure; capture only the lock/submit
	// so we can assert an error arm short-circuits before the pane.
	c.acquireTxn = cap.acquireTxn
	c.submit = cap.submit
	return c, cap
}

// TestRoute_ProductionResolveDestErrorArms drives the REAL NewLibrary-built resolveDest
// closure (library.go) through every failure arm the SPI's ok=false collapse forces it to
// re-derive — the LIVE operator-facing error path the seam-stubbed Route tests never reach
// (they override c.resolveDest). The web transport is a recordingWebTransport returning a
// controlled (ok, dest); the re-derivation runs against the SAME shared roster.ResolveTarget,
// so the closure must surface the PRECISE failure the HTTP layer maps (404 vs 400) — never a
// wrong sentinel, never a silent drop. All four arms must short-circuit BEFORE the pane (no
// acquire/submit) — a failed resolution must not reach a lock or a paste.
func TestRoute_ProductionResolveDestErrorArms(t *testing.T) {
	// rosterDup lets a case-insensitive collision (alpha + Alpha) produce ErrAmbiguousTarget
	// from the shared resolver when the target is given in a non-exact case ("ALPHA").
	const rosterDup = `{"channel_id":"C1","xo_agent":"xo","cos_agent":"xo","heartbeat_interval":"20m","agents":[{"name":"xo"},{"name":"alpha"},{"name":"Alpha"}]}`

	cases := []struct {
		name       string
		rosterBody string
		target     string
		webTr      transport.Transport
		// wantErrIs, if non-nil, is the sentinel the surfaced error must match
		// (errors.Is). The HTTP layer (control_handlers.go:82-88) maps ErrUnknownTarget →
		// 404 and ErrAmbiguousTarget → 400.
		wantErrIs error
		// wantNotErrIs are sentinels the error must NOT match — pinning that the
		// pane-resolve / non-inbound arms are NOT mis-mapped to a 404/400 target error.
		wantNotErrIs []error
		// wantDetail, if non-empty, must appear in the error string (the non-silent
		// pane-resolve / fail-closed wording).
		wantDetail string
	}{
		{
			// (a) webTr ok=false AND the shared resolver yields ErrUnknownTarget → the
			// closure surfaces the unknown-target sentinel (HTTP 404).
			name:       "ok_false_unknown_target",
			rosterBody: rosterCos,
			target:     "ghost", // no such agent in the roster
			webTr:      &recordingWebTransport{ok: false},
			wantErrIs:  ErrUnknownTarget,
		},
		{
			// (b) webTr ok=false AND the shared resolver yields ErrAmbiguousTarget → the
			// closure surfaces the ambiguous sentinel (HTTP 400).
			name:       "ok_false_ambiguous_target",
			rosterBody: rosterDup,
			target:     "ALPHA", // case collision with no exact match
			webTr:      &recordingWebTransport{ok: false},
			wantErrIs:  ErrAmbiguousTarget,
		},
		{
			// (c) webTr ok=false BUT the shared resolver SUCCEEDS — the resolver-ok-but-no-
			// inbound-target edge. The failure was pane resolution; the closure surfaces a
			// NON-SILENT pane-resolve error, NOT a wrong 404/400 target sentinel.
			name:         "ok_false_but_resolver_succeeds_is_pane_resolve_error",
			rosterBody:   rosterCos,
			target:       "alpha", // resolves cleanly in the roster
			webTr:        &recordingWebTransport{ok: false},
			wantNotErrIs: []error{ErrUnknownTarget, ErrAmbiguousTarget},
			wantDetail:   "resolve pane for",
		},
		{
			// (d) webTr ok=true but the returned dest does NOT satisfy InboundTarget (a
			// wiring bug — a webhook/outbound destination handed to the inbound seam). The
			// closure FAILS CLOSED rather than delivering to an unknown pane — never a
			// silent drop, never a wrong target sentinel.
			name:       "ok_true_non_inbound_dest_fails_closed",
			rosterBody: rosterCos,
			target:     "alpha",
			// A discord webhook Destination satisfies Destination but NOT InboundTarget.
			webTr:        &recordingWebTransport{ok: true, dest: transport.NewWebhookDestination("https://discord.example/webhook/x"), agent: "alpha"},
			wantNotErrIs: []error{ErrUnknownTarget, ErrAmbiguousTarget},
			wantDetail:   "non-inbound destination",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, cap := newRouteErrorController(t, tc.rosterBody, tc.webTr)
			res, err := c.Route(context.Background(), tc.target, "do the thing")
			if err == nil {
				t.Fatalf("Route → (%+v, nil), want a hard error from the production resolveDest closure", res)
			}
			if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
				t.Errorf("err = %v, want errors.Is(%v)", err, tc.wantErrIs)
			}
			for _, not := range tc.wantNotErrIs {
				if errors.Is(err, not) {
					t.Errorf("err = %v, must NOT be %v (the pane-resolve / fail-closed arm must not be mis-mapped to a target sentinel)", err, not)
				}
			}
			if tc.wantDetail != "" && !strings.Contains(err.Error(), tc.wantDetail) {
				t.Errorf("err = %q, want it to contain %q (non-silent failure wording)", err.Error(), tc.wantDetail)
			}
			// Every error arm must short-circuit BEFORE the pane: no lock, no submit.
			if len(cap.events) != 0 {
				t.Errorf("a resolve failure reached the pane (events=%v), want none — no acquire/submit on a failed resolution", cap.events)
			}
			if cap.txnTarget != "" {
				t.Errorf("a resolve failure keyed the lock on %q, want no lock at all", cap.txnTarget)
			}
		})
	}
}

// TestRoute_ProductionResolveDestErrorArms_FailsIfArmBroken is the prove-it-fails
// companion for arm (a): it documents the assertion that breaks if the production
// closure stops surfacing the unknown-target sentinel. (Manual confirmation: temporarily
// rewriting the closure's `return "", "", rerr` to swallow the error makes the (a) subtest
// FAIL with "want errors.Is(unknown route target …)". Restored after confirming.) Kept as a
// named anchor so a future reader knows the error arms are exercised against the real closure.
func TestRoute_ProductionResolveDestErrorArms_FailsIfArmBroken(t *testing.T) {
	c, cap := newRouteErrorController(t, rosterCos, &recordingWebTransport{ok: false})
	_, err := c.Route(context.Background(), "ghost", "x")
	if !errors.Is(err, ErrUnknownTarget) {
		t.Fatalf("production resolveDest closure must surface ErrUnknownTarget for an unknown target; got %v", err)
	}
	if len(cap.events) != 0 {
		t.Errorf("unknown target reached the pane (events=%v)", cap.events)
	}
}

// TestWebAndControlXODerivationsConverge pins FOLD-2's load-bearing dependency: the
// re-derivation in NewLibrary's resolveDest closure (library.go) is only correct because
// the web transport's xo and the control library's xo are derived IDENTICALLY — both
// "XOAgent else Agents[0]" from the SAME roster. This asserts the two derivation RULES
// agree across rosters (one with an explicit xo_agent, one relying on the Agents[0]
// fallback). If a future refactor let the web transport's xo diverge, the re-derivation
// would mis-map the failure; this test is the canary. (#200 removes the re-derivation and
// this dependency entirely by having the SPI return the precise sentinel.)
func TestWebAndControlXODerivationsConverge(t *testing.T) {
	cases := []struct {
		name       string
		rosterBody string
		wantXO     string
	}{
		// xo_agent set explicitly → both derivations pick it.
		{"explicit_xo_agent", rosterCos, "xo"},
		// no xo_agent → both fall back to Agents[0].
		{"agents_zero_fallback", `{"channel_id":"C1","heartbeat_interval":"20m","agents":[{"name":"backend"},{"name":"alpha"}]}`, "backend"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			rosterPath := filepath.Join(dir, "flotilla.json")
			if err := os.WriteFile(rosterPath, []byte(tc.rosterBody), 0o600); err != nil {
				t.Fatal(err)
			}
			rc, err := roster.Load(rosterPath)
			if err != nil {
				t.Fatalf("roster.Load: %v", err)
			}
			// The control library's rule (internal/dash/server.go:93-96): xo_agent else
			// Agents[0]. NewLibrary receives THIS xo and the resolveDest closure re-derives
			// the failure against (rc, this xo).
			controlXO := rc.XOAgent
			if controlXO == "" {
				controlXO = rc.Agents[0].Name
			}
			// The web transport's rule (internal/transport/web.go newWebTransport): the SAME
			// xo_agent-else-Agents[0] derivation, applied to the SAME roster. transport
			// exposes it via XOForRoster so the convergence is asserted against the transport
			// package's OWN derivation function — not a copy that could silently drift.
			webXO := transport.XOForRoster(rc)
			if controlXO != tc.wantXO {
				t.Errorf("control xo = %q, want %q", controlXO, tc.wantXO)
			}
			if webXO != controlXO {
				t.Errorf("web transport xo = %q, control library xo = %q — the two derivations DIVERGED (the resolveDest re-derivation would mis-map; see library.go FOLD-2 comment / #200)", webXO, controlXO)
			}
		})
	}
}

// --- Resume stays gated (its blocker is the package-main orchestration, not the lock) ---

func TestResume_GatedPendingLibraryExtraction(t *testing.T) {
	c, _ := newTestController(t, rosterCos, secretsXO)
	if _, err := c.Resume(context.Background(), "alpha"); !errors.Is(err, ErrResumeUnavailable) {
		t.Errorf("Resume err = %v, want ErrResumeUnavailable", err)
	}
}

// agentTitle returns the roster Title() for an agent, to assert resolvePane is
// asked for the Title (not the bare name) — the cmdSend-identical resolution.
func agentTitle(t *testing.T, c *LibraryController, name string) string {
	t.Helper()
	a, err := c.roster.Agent(name)
	if err != nil {
		t.Fatal(err)
	}
	return a.Title()
}
