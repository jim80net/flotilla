package transport

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/roster"
)

// webTestRoster builds a Config with an XO + the given extra agents (no file I/O).
func webTestRoster(xo string, agents ...string) *roster.Config {
	rc := &roster.Config{XOAgent: xo}
	rc.Agents = append(rc.Agents, roster.Agent{Name: xo})
	for _, a := range agents {
		rc.Agents = append(rc.Agents, roster.Agent{Name: a})
	}
	return rc
}

// fakeResolvePane stands in for deliver.ResolvePane (which needs a live tmux fleet):
// it maps an agent Title() to a deterministic pane target, recording the title asked.
func fakeResolvePane(target string) func(string) (string, error) {
	return func(title string) (string, error) {
		return target, nil
	}
}

// --- Section 2: registration ---

// TestWebTransport_RegistersAndResolvesByName pins task 2.1/2.2: Construct("web", …)
// builds a web transport that Get("web") resolves; an unknown name still errors; an
// empty name still resolves discord (no default regression).
func TestWebTransport_RegistersAndResolvesByName(t *testing.T) {
	rc := webTestRoster("xo", "alpha")
	tr, err := Construct("web", Config{Roster: rc})
	if err != nil {
		t.Fatalf("Construct web: %v", err)
	}
	if tr.Name() != "web" {
		t.Errorf("Name() = %q, want web", tr.Name())
	}
	got, ok := Get("web")
	if !ok || got.Name() != "web" {
		t.Errorf("Get(web) = (%v, %v), want the web transport", got, ok)
	}

	// An unknown name still errors (registry.go:99-122).
	if _, err := Construct("nope", Config{Roster: rc}); err == nil {
		t.Error("Construct(nope) must error for an unregistered transport")
	}

	// An empty name still resolves the discord default — no default regression.
	if d, derr := Construct("", Config{}); derr != nil || d.Name() != DefaultTransport {
		t.Errorf("Construct(\"\") = (%v, %v), want the discord default", d, derr)
	}
}

// --- Section 2: the webDestination direction asymmetry ---

// TestWebDestination_IsInboundCarriesNoCredential pins task 2.3/2.4: a webDestination
// carries {agentName, paneTarget} and NO credential, and is the INBOUND delivery
// target — it must NEVER be a valid argument to a Post (the direction asymmetry:
// discord's ResolveDestination yields an OUTBOUND post target; web's yields an INBOUND
// pane-delivery target consumed by the delivery leg, never by Post).
func TestWebDestination_IsInboundCarriesNoCredential(t *testing.T) {
	rc := webTestRoster("xo", "alpha")
	wt := &webTransport{roster: rc, xo: "xo", resolvePane: fakeResolvePane("spark:3.1")}

	dest, agent, ok := wt.ResolveDestination("", "alpha")
	if !ok {
		t.Fatal("ResolveDestination(alpha) must resolve")
	}
	if agent != "alpha" {
		t.Errorf("agent = %q, want alpha", agent)
	}
	wd, isWeb := dest.(webDestination)
	if !isWeb {
		t.Fatalf("destination type = %T, want webDestination", dest)
	}
	// It carries the resolved {agentName, paneTarget} — and no credential field exists
	// on the type at all (the direction asymmetry: an inbound pane target, not an
	// outbound webhook).
	if wd.agentName != "alpha" || wd.paneTarget != "spark:3.1" {
		t.Errorf("webDestination = %+v, want {alpha, spark:3.1}", wd)
	}
	// A webDestination must NEVER be a valid Post target — Post is the OUTBOUND seam
	// the web transport does not own (its only outbound, the notify, is a Discord post
	// by the discord transport). Post must reject it rather than silently deliver.
	if err := wt.Post(wd, "user", "content"); err == nil {
		t.Error("web Post(webDestination) must error — web owns no outbound post; the inbound destination must not flow to Post")
	}
}

// --- Section 3: inbound resolution (Decision 1 + 2) ---

// TestWebResolveDestination_RosterWideIgnoresOriginChannel pins task 3.1: the web
// transport resolves ROSTER-WIDE (empty → XO; @name/name → any roster agent,
// case-insensitive, exact-wins, ambiguity rejected) and IGNORES originChannel.
func TestWebResolveDestination_RosterWideIgnoresOriginChannel(t *testing.T) {
	rc := webTestRoster("xo", "alpha", "Alpha")
	wt := &webTransport{roster: rc, xo: "xo", resolvePane: fakeResolvePane("%pane")}

	// Empty → the XO, and the originChannel is ignored (a non-empty channel must not
	// change the result — web has no channel multiplexing).
	for _, originChannel := range []string{"", "C1", "anything"} {
		_, agent, ok := wt.ResolveDestination(originChannel, "")
		if !ok || agent != "xo" {
			t.Errorf("empty target on origin %q → (%q, %v), want the XO (originChannel ignored)", originChannel, agent, ok)
		}
	}

	// @name / case-insensitive, exact-wins.
	_, agent, ok := wt.ResolveDestination("C1", "@Alpha")
	if !ok || agent != "Alpha" {
		t.Errorf("@Alpha → (%q, %v), want exact Alpha", agent, ok)
	}

	// Ambiguous case-collision with no exact match → not ok (the bus ignores it).
	if _, _, ok := wt.ResolveDestination("", "ALPHA"); ok {
		t.Error("ambiguous 'ALPHA' must resolve ok=false (rejected, not guessed)")
	}

	// Unknown target → not ok.
	if _, _, ok := wt.ResolveDestination("", "ghost"); ok {
		t.Error("unknown 'ghost' must resolve ok=false")
	}
}

// TestWebResolveDestination_SharesRosterResolver pins task 3.2's spec requirement
// ("The roster-wide resolver is shared, not forked"): the web transport resolves
// through the SAME roster.ResolveTarget the dash control library uses, so an
// identical (roster, xo, target) resolves identically across both call sites — the
// exact-wins-else-ambiguous rule cannot drift. Here we assert the web transport's
// result matches roster.ResolveTarget directly (the one shared function).
func TestWebResolveDestination_SharesRosterResolver(t *testing.T) {
	rc := webTestRoster("xo", "alpha", "Alpha")
	wt := &webTransport{roster: rc, xo: "xo", resolvePane: fakeResolvePane("%pane")}

	for _, target := range []string{"", "@alpha", "Alpha", "ALPHA", "ghost"} {
		wantAgent, wantErr := rc.ResolveTarget("xo", target)
		_, gotAgent, gotOK := wt.ResolveDestination("ignored-channel", target)
		// ok mirrors (wantErr == nil); on success the agent must match the shared resolver.
		if (wantErr == nil) != gotOK {
			t.Errorf("target %q: web ok=%v, shared resolver err=%v (must agree)", target, gotOK, wantErr)
		}
		if wantErr == nil && gotAgent != wantAgent {
			t.Errorf("target %q: web agent=%q, shared resolver agent=%q (must match — one resolver)", target, gotAgent, wantAgent)
		}
	}
}

// TestWebTransport_DoesNotImplementCatchUp pins task 3.3: the web transport's
// delivery is in-process (loopback) and cannot gap, so it does NOT implement the
// optional CatchUp capability — the type-assertion fails, the clean demonstration
// that the optional capability is optional.
func TestWebTransport_DoesNotImplementCatchUp(t *testing.T) {
	var tr Transport = &webTransport{}
	if _, ok := tr.(CatchUp); ok {
		t.Error("the web transport must NOT implement CatchUp (loopback in-process delivery cannot gap)")
	}
}

// TestWebTransport_SubscribeIsNoOp pins task 3.4: Subscribe is a deliberate no-op —
// the ONLY web ingress is the gated POST /api/control/route HTTP route, so Subscribe
// opens NO second inbound feed (which would bypass the reused requireWrite/Host/Origin
// defenses). It returns nil and never invokes the handler.
func TestWebTransport_SubscribeIsNoOp(t *testing.T) {
	wt := &webTransport{roster: webTestRoster("xo"), xo: "xo", resolvePane: fakeResolvePane("%p")}
	handlerCalled := false
	handler := func(string, string, string, string) { handlerCalled = true }

	err := wt.Subscribe(context.Background(), wt.Destinations([]string{"C1"}), handler, func() {
		t.Error("web Subscribe must not fire onReconnect — it opens no live feed")
	})
	if err != nil {
		t.Errorf("web Subscribe must return nil (no-op), got %v", err)
	}
	if handlerCalled {
		t.Error("web Subscribe must NOT invoke the handler — the only ingress is the gated HTTP route")
	}
}

// TestWebTransport_PostRejectsWebhookDestination guards the direction asymmetry from
// the other side: even a discord webhook destination must not post through the web
// transport (web owns no outbound). This forecloses wiring an outbound webhook post
// through the wrong transport.
func TestWebTransport_PostRejectsWebhookDestination(t *testing.T) {
	wt := &webTransport{roster: webTestRoster("xo"), xo: "xo"}
	if err := wt.Post(NewWebhookDestination("https://x/webhook"), "u", "c"); err == nil {
		t.Error("web Post must reject ANY destination — it has no outbound post medium")
	}
}

// TestWebTransport_ConstructRequiresRoster pins that the factory fails closed without
// a roster (the resolver has nothing to resolve against) rather than building a
// transport that would nil-deref on the first resolve.
func TestWebTransport_ConstructRequiresRoster(t *testing.T) {
	if _, err := newWebTransport(Config{}); !errors.Is(err, errWebNoRoster) {
		t.Errorf("newWebTransport(no roster) = %v, want errWebNoRoster", err)
	}
}

// --- Section 6.3: discord + web coexistence ---

// TestWebDiscordCoexist pins task 6.3 / spec "The discord and web transports run
// simultaneously without interference": both transports construct + resolve from the
// SAME per-process registry concurrently (the mutex-guarded maps are the only shared
// mutable state), and a delivery to one pane from either path keys the cross-process
// lock on the IDENTICAL resolved pane target — the convergence guarantee. Here the web
// transport resolves a pane via its resolvePane seam; production wires
// deliver.ResolvePane, the SAME function the dash control Route + cmdSend + the watch
// Injector use, so the lock keys cannot diverge (asserted by identity in
// TestWebTransport_WiresRealResolvePane below).
func TestWebDiscordCoexist(t *testing.T) {
	rc := webTestRoster("xo", "alpha")

	// Construct both transports from the same registry — they register independently
	// and resolve by name with no interference.
	web, err := Construct("web", Config{Roster: rc})
	if err != nil {
		t.Fatalf("Construct web: %v", err)
	}
	disc, err := Construct("", Config{Roster: rc}) // the discord default
	if err != nil {
		t.Fatalf("Construct discord: %v", err)
	}
	if web.Name() != "web" || disc.Name() != DefaultTransport {
		t.Fatalf("names = (%q, %q), want (web, discord)", web.Name(), disc.Name())
	}

	// Both resolve concurrently from the registry — race the lookups to exercise the
	// mutex-guarded maps (run under -race to catch a missing lock).
	const goroutines = 16
	done := make(chan struct{}, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			if w, ok := Get("web"); !ok || w.Name() != "web" {
				t.Errorf("concurrent Get(web) = (%v, %v)", w, ok)
			}
			if d, ok := Get(""); !ok || d.Name() != DefaultTransport {
				t.Errorf("concurrent Get(discord) = (%v, %v)", d, ok)
			}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// The web transport's resolved pane target is the cross-process lock key. With the
	// fake resolver returning a fixed pane, the webDestination carries that exact key —
	// the SAME string a watch rotate / dash route to the same desk would key on (the
	// per-pane flock convergence point — internal/deliver owns the flock itself).
	wt := web.(*webTransport)
	wt.resolvePane = fakeResolvePane("spark:3.1")
	dest, _, ok := wt.ResolveDestination("ignored", "alpha")
	if !ok {
		t.Fatal("web ResolveDestination(alpha) must resolve")
	}
	if got := dest.(webDestination).paneTarget; got != "spark:3.1" {
		t.Errorf("web lock key = %q, want the resolved pane target spark:3.1", got)
	}
}

// TestWebTransport_WiresRealResolvePane is the lock-key DRIFT guard (mirroring the
// dash's TestNewLibrary_WiresRealResolvePane): newWebTransport MUST wire resolvePane =
// deliver.ResolvePane by function identity — the SAME function the dash control Route +
// cmdSend + the watch Injector use — so the cross-process per-pane lock keys cannot
// silently diverge between the web inbound and every other pane writer. A real
// ResolvePane(...) needs the live tmux fleet, so identity is the runnable proxy.
func TestWebTransport_WiresRealResolvePane(t *testing.T) {
	tr, err := newWebTransport(Config{Roster: webTestRoster("xo", "alpha")})
	if err != nil {
		t.Fatalf("newWebTransport: %v", err)
	}
	wt := tr.(*webTransport)
	if reflect.ValueOf(wt.resolvePane).Pointer() != reflect.ValueOf(deliver.ResolvePane).Pointer() {
		t.Error("newWebTransport must wire resolvePane = deliver.ResolvePane (the shared lock-key source; a divergent resolver silently breaks cross-process serialization)")
	}
}
