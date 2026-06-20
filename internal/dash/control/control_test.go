package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/cos"
	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
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
	c := NewLibrary(rc, xo, secretsPath)
	cap := &capture{paneTarget: "%5"}
	c.post = cap.post
	c.appendCos = cap.append
	c.now = func() time.Time { return fixedTime }
	// Route seams: record the resolve→acquire→submit→release sequence so tests can
	// assert the lock keys on the RESOLVED pane target and brackets the submit.
	c.resolvePane = cap.resolvePane
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

	// Route seam recording.
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
	cap.paneTarget = "spark:3.1" // what deliver.ResolvePane would return
	res, err := c.Route(context.Background(), "alpha", "do the thing")
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if cap.resolvedTitle != agentTitle(t, c, "alpha") {
		t.Errorf("resolvePane asked for %q, want the agent's Title()", cap.resolvedTitle)
	}
	if cap.txnTarget != "spark:3.1" {
		t.Errorf("txn lock keyed on %q, want the resolved pane target %q (NOT the agent name)", cap.txnTarget, "spark:3.1")
	}
	if cap.submitPane != "spark:3.1" {
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

// TestNewLibrary_WiresRealResolvePane is the DRIFT guard: the seam-stubbed tests
// above prove the plumbing (Route keys the lock on resolvePane's output), but they
// cannot catch a production controller that wires a DIFFERENT resolver. This
// asserts NewLibrary wires resolvePane = deliver.ResolvePane by function identity —
// the SAME function cmdSend + the watch Injector use — so the cross-process lock
// keys cannot silently diverge in a future refactor (a real ResolvePane(...) call
// needs the live tmux fleet, so identity is the runnable proxy).
func TestNewLibrary_WiresRealResolvePane(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(rosterCos), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	c := NewLibrary(rc, "xo", "")
	if reflect.ValueOf(c.resolvePane).Pointer() != reflect.ValueOf(deliver.ResolvePane).Pointer() {
		t.Error("NewLibrary must wire resolvePane = deliver.ResolvePane (the shared lock-key source; a divergent resolver silently breaks cross-process serialization)")
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
