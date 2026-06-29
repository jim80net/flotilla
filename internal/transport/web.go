package transport

import (
	"context"
	"errors"
	"fmt"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/roster"
)

// init registers the web FACTORY keyed "web" (mirroring discord.go's init). The
// web transport is the existing dashboard surface placed behind the Transport SPI
// (design Decision 1, Option 1) — there is exactly one web coordination surface,
// not a second web application. Registration is separate from construction: the
// roster the resolver reads arrives at daemon start via Construct(Config).
func init() {
	RegisterFactory("web", newWebTransport)
}

// errWebNoRoster is returned when the web factory is constructed without a roster —
// the roster-wide resolver has nothing to resolve against, so the factory fails
// closed rather than building a transport that would nil-deref on the first resolve.
var errWebNoRoster = errors.New("web transport: a roster is required (the roster-wide resolver resolves a target against it)")

// InboundTarget is the EXPORTED accessor an inbound pane-delivery Destination
// satisfies, so the delivery leg — which lives in a DIFFERENT package
// (internal/dash/control) and cannot read the unexported webDestination fields —
// reads the two values it needs through a typed contract: PaneTarget() (the
// cross-process AcquirePaneTxn lock key) and AgentName() (the canonical roster name
// for the result/ledger). The opaque SPI Destination marker (isDestination) stays
// unexported so a caller cannot forge a Destination; InboundTarget is the narrow,
// direction-specific window the INBOUND consumer type-asserts to. The OUTBOUND
// (Post) destinations — discord's credential-bearing webhook — deliberately do NOT
// satisfy it, keeping the direction asymmetry (design Decision 1) typed: an inbound
// pane target and an outbound post target are not interchangeable.
type InboundTarget interface {
	Destination
	// AgentName is the canonical roster agent the instruction is addressed to.
	AgentName() string
	// PaneTarget is the resolved tmux pane string — the cross-process lock key the
	// delivery leg keys AcquirePaneTxn on (identical to every other pane writer's key).
	PaneTarget() string
}

// webDestination is the web transport's concrete Destination: an INBOUND
// pane-delivery target — a canonical roster agent name plus its resolved tmux pane
// string. It carries NO credential (the direction asymmetry — design Decision 1):
// discord's ResolveDestination returns an OUTBOUND post target (a channel id + a
// webhook credential, consumed by Post); web's returns an INBOUND pane-delivery
// target, consumed by the dash's delivery leg (AcquirePaneTxn → Confirm.Submit),
// NEVER by Post. A webDestination must never be handed to a Post — the web transport
// has no meaningful outbound post (the only outbound the dash does is the Discord
// notify, posted by the DISCORD transport). It satisfies InboundTarget so the
// delivery leg (another package) reads {agentName, paneTarget} through that accessor.
type webDestination struct {
	agentName  string // the canonical roster agent the instruction is addressed to
	paneTarget string // the resolved tmux pane string (the cross-process lock key)
}

func (webDestination) isDestination() {}

// AgentName / PaneTarget satisfy InboundTarget — the typed window the dash delivery
// leg reads (it cannot reach the unexported fields directly across the package seam).
func (d webDestination) AgentName() string  { return d.agentName }
func (d webDestination) PaneTarget() string { return d.paneTarget }

// webTransport is the dashboard's coordination surface behind the Transport SPI. It
// owns ONLY the INBOUND half: ResolveDestination resolves a roster-wide address (via
// the ONE shared roster.ResolveTarget) to a webDestination the dash's delivery leg
// consumes. It has no live inbound socket (Subscribe is a no-op — the only web
// ingress is the gated POST /api/control/route HTTP route) and no outbound post
// medium (Post rejects — the notify is a Discord post by the discord transport). Its
// delivery is in-process/loopback and cannot gap, so it does NOT implement CatchUp.
//
// SCOPE (PR2, #188/#106): this transport is REGISTERED (init → RegisterFactory) but NOT YET
// CONSTRUCTED in the dash runtime — the live ingress today is still POST /api/control/route →
// LibraryController.Route → roster.ResolveTarget (which PR2 unified). ResolveDestination /
// webDestination / Post-reject are test-covered scaffolding; PR3 (#198) constructs + wires this
// transport as the actual ingress and pins the single-lock-key invariant (the route consumes
// webDestination.paneTarget rather than re-resolving the pane).
type webTransport struct {
	roster *roster.Config
	xo     string // the hub XO an empty target resolves to (XOAgent, else Agents[0])

	// resolvePane resolves an agent's Title() to its tmux pane string — the
	// cross-process lock key. Production wires deliver.ResolvePane (the SAME function
	// the dash control Route + cmdSend + the watch Injector use, so every writer keys
	// the per-pane lock on one identical resolved target); tests inject a fake.
	resolvePane func(title string) (string, error)
}

// newWebTransport is the registered Factory: it builds the web transport from the
// runtime Config. It reads the roster (the resolver's source) and derives the hub XO
// the same way the dash does (XOAgent, else the first agent). The resolvePane seam is
// wired to deliver.ResolvePane — the shared lock-key source. A missing roster fails
// closed (errWebNoRoster).
func newWebTransport(cfg Config) (Transport, error) {
	if cfg.Roster == nil {
		return nil, errWebNoRoster
	}
	xo := cfg.Roster.XOAgent
	if xo == "" && len(cfg.Roster.Agents) > 0 {
		xo = cfg.Roster.Agents[0].Name
	}
	return &webTransport{
		roster:      cfg.Roster,
		xo:          xo,
		resolvePane: deliver.ResolvePane,
	}, nil
}

// Name is the registry key.
func (t *webTransport) Name() string { return "web" }

// Subscribe is a deliberate NO-OP (design Decision 3). The web medium has no live
// inbound socket to subscribe to (unlike the discord gateway); the ONLY web ingress
// for an operator instruction is the EXISTING requireWrite/Host/Origin-gated
// POST /api/control/route HTTP route. Pinning Subscribe as a no-op forecloses a
// SECOND, ungated inbound feed that would bypass the reused dash CSRF/Host/Origin
// defenses. It opens no feed, never invokes the handler, and never fires onReconnect.
func (t *webTransport) Subscribe(_ context.Context, _ []Destination, _ MessageHandler, _ func()) error {
	return nil
}

// Destinations is a no-op set for the web transport: it has no Subscribe feed to
// target, so it builds no inbound destinations. Returning an empty slice keeps the
// no-op Subscribe honest (there is nothing to subscribe to).
func (t *webTransport) Destinations(_ []string) []Destination { return nil }

// Post rejects every destination: the web transport owns NO outbound post medium
// (the direction asymmetry — design Decision 1). The only outbound the dashboard
// does is the Discord operator-note, which is posted by the DISCORD transport
// (constructed at the wiring boundary), never by the web transport. A webDestination
// is an INBOUND pane-delivery target consumed by the delivery leg, not a Post target;
// rejecting here forecloses wiring an outbound post through the wrong transport.
func (t *webTransport) Post(dest Destination, _, _ string) error {
	return fmt.Errorf("web transport: Post is unsupported — the web medium owns only inbound resolution (its only outbound, the notify, is a Discord post by the discord transport); got %T", dest)
}

// ResolveDestination maps a roster-wide address to an INBOUND pane-delivery target.
// It resolves the agent name through the ONE shared roster.ResolveTarget (so the
// dash control library and the web transport cannot drift — transport spec "The
// roster-wide resolver is shared, not forked"), IGNORING originChannel (the web
// medium has no channel; resolution is roster-wide, design Decision 2). It then
// resolves the agent's tmux pane (the cross-process lock key) so the returned
// webDestination carries {agentName, paneTarget}. ok=false ⇒ the target resolves to
// no roster agent, is ambiguous, or its pane cannot be resolved (the caller ignores
// it / surfaces the failure) — never a silent wrong delivery.
func (t *webTransport) ResolveDestination(_, bareOrMention string) (Destination, string, bool) {
	if t.roster == nil {
		return nil, "", false
	}
	agentName, err := t.roster.ResolveTarget(t.xo, bareOrMention)
	if err != nil {
		return nil, "", false
	}
	agent, err := t.roster.Agent(agentName)
	if err != nil {
		return nil, "", false
	}
	pane, err := t.resolvePane(agent.Title())
	if err != nil {
		return nil, "", false
	}
	return webDestination{agentName: agentName, paneTarget: pane}, agentName, true
}

// MaxContentRunes is the web medium's per-message content cap. The dash composer is
// not subject to Discord's 2000-rune cap, but a delivered instruction is a pane
// paste; the cap matches the dash's existing posture (no separate web cap was ever
// configured), so it reports the same 2000 the discord medium does — a conservative,
// non-regressing default for the inbound delivery path.
func (t *webTransport) MaxContentRunes() int { return 2000 }

// Chunk splits text at the web medium's cap. The web inbound delivers an instruction
// to a pane as one paste (it is not posted to a chunked channel), so chunking is a
// pass-through within the cap; the package-level Chunk helper is used for a caller
// that genuinely needs splitting.
func (t *webTransport) Chunk(text string) []string { return Chunk(text, t.MaxContentRunes()) }

// Close releases the web transport's resources. The web transport owns no live
// session (no gateway, no REST) — its inbound is the dash's HTTP route and its
// delivery is the in-process lock-bracketed pane write — so Close is a no-op.
func (t *webTransport) Close() error { return nil }
