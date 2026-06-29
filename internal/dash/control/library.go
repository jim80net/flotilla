package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/cos"
	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/transport"
)

// dashProvenance is the CoS ledger "from" marker for a dash-issued action, so a
// dash control action is distinguishable from a Discord-originated one in the
// who-knows-what audit record (design §4 / spec "Dash control actions are
// recorded for audit").
const dashProvenance = "operator(dash)"

// LibraryController is the ONE real Controller: thin proxies over the existing
// delivery library. Notify posts through the injected Transport (the notify's
// destination is a Discord operator-note webhook, so the injected transport is the
// DISCORD transport — see NewLibrary); Route drives panes through the cross-process
// pane-transaction lock; Resume fails closed (its orchestration is being extracted
// into a reusable library, see Resume). The library calls are injected as seams so
// the policy here is unit-testable without a real Discord/tmux. The dependency on
// the concrete medium enters ONLY as an injected Transport interface value at the
// wiring boundary (cmd/flotilla/dash.go), so this package no longer imports
// internal/discord (the same decoupling PR1 established for the relay packages —
// see no_discord_import_test.go).
type LibraryController struct {
	roster      *roster.Config
	xo          string // the hub XO whose webhook a dash note posts under
	secretsPath string // for the notify webhook ("" ⇒ notify unavailable)

	// Seams (production wires the real library calls; tests inject fakes).
	// post sends an operator note to a resolved webhook; in production it is the
	// injected (discord-backed) Transport.Post, bound to a webhook Destination built
	// at the call from the resolved hook (transport.NewWebhookDestination) — NOT a
	// direct internal/discord.Post. maxContentRunes is the injected Transport's own
	// per-message content cap (transport.MaxContentRunes()), so the over-length guard
	// reads the medium's cap rather than a leaked discord constant.
	post            func(webhook, username, content string) error
	maxContentRunes int
	loadSecrets     func(path string) (*roster.Secrets, error)
	appendCos       func(path string, e cos.Entry) error
	now             func() time.Time

	// Route seams — the confirmed-delivery transaction. As of PR3 (#198) Route
	// resolves the target+pane THROUGH the injected WEB transport's ResolveDestination
	// (resolveDest), so there is ONE roster-wide-resolve + pane-resolve site, not a
	// second one re-implemented inside Route. resolveDest returns the canonical agent
	// name + the resolved pane string (webDestination.paneTarget) — the cross-process
	// AcquirePaneTxn lock key. Because the web transport wires deliver.ResolvePane (the
	// SAME function cmdSend + the watch Injector/rotate use), the key the dash route
	// computes is IDENTICAL to every other pane writer's — the contract that makes the
	// flock serialize cross-process (design §5). acquireTxn + submit terminate the same
	// lock-bracketed delivery the cmdSend path uses.
	getDriver   func(name string) (surface.Driver, bool)
	resolveDest func(originChannel, target string) (agentName, paneTarget string, err error)
	acquireTxn  func(target string) (release func(), err error)
	submit      func(drv surface.Driver, pane, text string) error
}

// NewLibrary builds the production controller. secretsPath may be "" (then notify
// returns ErrWebhookMissing); roster + xo are required for webhook + ledger
// resolution.
//
// TWO transports are injected, for the two opposite-direction seams (the direction
// asymmetry — design Decision 1):
//
//   - notifyTr backs the notify's OUTBOUND post. Because the operator-note destination
//     is a Discord webhook (secrets.Webhook(xo)), notifyTr is the DISCORD transport. The
//     notify resolves the webhook string from secrets, wraps it in a
//     transport.NewWebhookDestination, and posts via notifyTr.Post; the over-length guard
//     reads notifyTr.MaxContentRunes(). (Closes the PR1 TODO(#188/#106) seam.)
//   - webTr backs the route's INBOUND resolution. As of PR3 (#198) the dash route is the
//     LIVE web ingress: Route resolves its target+pane THROUGH webTr.ResolveDestination
//     (the ONE roster-wide resolver + the SAME deliver.ResolvePane the web transport
//     wires), and consumes the returned webDestination.paneTarget as the AcquirePaneTxn
//     lock key — NOT a second in-Route pane resolution. So the dash route + the watch
//     Injector key the per-pane flock on the IDENTICAL resolved target (cross-process
//     serialization, design §5).
//
// Both enter as interface VALUES at the wiring boundary (cmd/flotilla/dash.go), so this
// package depends on internal/transport, not on the concrete internal/discord package.
func NewLibrary(rc *roster.Config, xo, secretsPath string, notifyTr, webTr transport.Transport) *LibraryController {
	return &LibraryController{
		roster:      rc,
		xo:          xo,
		secretsPath: secretsPath,
		// The notify's outbound post goes through the injected (discord-backed)
		// Transport: the resolved webhook string is wrapped in an opaque webhook
		// Destination (the credential stays inside the transport, never a caller-visible
		// string) and posted via tr.Post — the SAME wiring-boundary pattern
		// cmd/flotilla/watch.go uses for its down-alert post (Construct +
		// NewWebhookDestination + tr.Post).
		post: func(hook, username, content string) error {
			return notifyTr.Post(transport.NewWebhookDestination(hook), username, content)
		},
		// The over-length guard reads the medium's own per-message cap from the
		// transport (discord = 2000), not a hard-coded discord constant leaking across
		// the seam.
		maxContentRunes: notifyTr.MaxContentRunes(),
		loadSecrets:     roster.LoadSecrets,
		appendCos:       cos.Append,
		now:             time.Now,
		getDriver:       surface.Get,
		// The route resolves its target+pane through the WEB transport's ResolveDestination
		// (PR3 #198): ONE roster-wide resolve (roster.ResolveTarget) + ONE pane resolve
		// (deliver.ResolvePane, wired inside the web transport). Route consumes the returned
		// webDestination.paneTarget as the lock key — the web route + every other pane writer
		// thus key the flock on the identical resolved target. The returned Destination is the
		// INBOUND target; we read its {agentName, paneTarget} via the exported InboundTarget
		// accessor (the unexported webDestination fields are not visible across the package
		// seam).
		//
		// The SPI's ResolveDestination collapses every failure into ok=false, but the HTTP
		// layer maps unknown vs ambiguous targets to distinct statuses (404 vs 400) and the
		// control tests pin those distinct sentinels. So on ok=false we re-derive the PRECISE
		// reason from the SAME shared resolver (roster.ResolveTarget) — a pure function of
		// (roster, xo, target), so calling it again yields the identical verdict with no risk
		// of drift, and it touches NO pane resolution (the lock key still comes solely from
		// the web transport's deliver.ResolvePane). If the shared resolver itself succeeds yet
		// the web transport returned ok=false, the failure was pane resolution — surfaced as a
		// pane-resolve error (the old Route's "resolve pane for %s" path), never a silent drop.
		resolveDest: func(originChannel, target string) (string, string, error) {
			dest, agentName, ok := webTr.ResolveDestination(originChannel, target)
			if ok {
				if it, isInbound := dest.(transport.InboundTarget); isInbound {
					return agentName, it.PaneTarget(), nil
				}
			}
			if name, rerr := rc.ResolveTarget(xo, target); rerr != nil {
				return "", "", rerr // ErrUnknownTarget / ErrAmbiguousTarget (the distinct sentinels)
			} else if !ok {
				return "", "", fmt.Errorf("resolve pane for %s: web transport returned no inbound target", name)
			}
			// ok==true but the destination did not satisfy InboundTarget — a wiring bug in the
			// injected transport; fail closed rather than deliver to an unknown pane.
			return "", "", fmt.Errorf("web transport resolved %q to a non-inbound destination %T", target, dest)
		},
		// Wrap AcquirePaneTxn so the seam returns a plain release func; production
		// uses the coordinated PaneTxnTimeout (identical to cmdSend + the Injector).
		acquireTxn: func(target string) (func(), error) {
			txn, err := deliver.AcquirePaneTxn(target, deliver.PaneTxnTimeout)
			if err != nil {
				return nil, err
			}
			return txn.Release, nil
		},
		// A dash control send is an operator relay → route through the self-heal-capable submit
		// (#156). Self-heal is inert unless FLOTILLA_SELF_HEAL is enabled (SendCtrlC unwired ⇒
		// SubmitWithSelfHeal == Submit). The transaction lock is held by Route around this call.
		submit: func(drv surface.Driver, pane, text string) error {
			c := surface.Confirm{SendEnter: deliver.SendEnter, Sleep: time.Sleep}
			if surface.SelfHealEnabled() {
				c.SendCtrlC = deliver.SendCtrlC
			}
			return c.SubmitWithSelfHeal(drv, pane, text)
		},
	}
}

// Notify posts an operator note to the fleet channel under the XO's webhook with
// the dash-provenance username, then mirrors it to the CoS ledger (best-effort).
func (c *LibraryController) Notify(_ context.Context, message string) error {
	if strings.TrimSpace(message) == "" {
		return ErrEmptyMessage
	}
	// This message IS operator-facing content — reject an over-length body cleanly
	// (never silently truncate the operator's note), mirroring cmdNotify. The cap is
	// the injected transport's own per-message limit (transport.MaxContentRunes()),
	// not a leaked discord constant.
	if n := len([]rune(message)); n > c.maxContentRunes {
		return fmt.Errorf("%w: %d chars (limit %d)", ErrOverLength, n, c.maxContentRunes)
	}
	if c.secretsPath == "" {
		return ErrWebhookMissing
	}
	secrets, err := c.loadSecrets(c.secretsPath)
	if err != nil {
		return fmt.Errorf("load secrets: %w", err)
	}
	hook, err := secrets.Webhook(c.xo)
	if err != nil {
		return ErrWebhookMissing
	}
	if err := c.post(hook, dashProvenance, message); err != nil {
		return err
	}
	c.mirrorToLedger(message)
	return nil
}

// Route delivers an instruction to a desk via the confirmed-delivery library,
// serialized cross-process by the per-pane TRANSACTION lock (design §5). As of PR3
// (#198) it is the LIVE web ingress: it resolves the target+pane THROUGH the injected
// web transport's ResolveDestination (resolveDest) — the ONE roster-wide resolver +
// the SAME deliver.ResolvePane every pane writer uses — and CONSUMES the returned
// webDestination.paneTarget as the AcquirePaneTxn lock key (it does NOT re-resolve the
// pane). So the dash route and the watch Injector key the flock on the IDENTICAL
// resolved target — the cross-process serialization contract; a divergent resolve
// would silently fail to serialize. After resolution it mirrors the cmdSend path:
// agent → its driver → the txn lock (keyed on the resolved pane target) → Confirm.Submit
// → Release. The typed surface outcome (delivered/busy/crashed/transient/unconfirmed)
// is returned as a RouteResult (informational outcomes the operator must see, NOT
// errors); only a hard failure (unknown/ambiguous target, unknown surface,
// pane-resolution failure) is an error. A lock-contention timeout is surfaced as a
// busy/not-delivered outcome (retryable) — never a silent partial send.
func (c *LibraryController) Route(_ context.Context, target, message string) (RouteResult, error) {
	if strings.TrimSpace(message) == "" {
		return RouteResult{}, ErrEmptyMessage
	}
	// The web transport has no channel; resolution is roster-wide (Decision 2), so the
	// originChannel is empty. resolveDest returns the canonical agent name + the resolved
	// pane target (the lock key) in one shared resolution; its error carries the distinct
	// ErrUnknownTarget / ErrAmbiguousTarget sentinel (HTTP-mapped to 404 / 400).
	agentName, pane, err := c.resolveDest("", target)
	if err != nil {
		return RouteResult{}, err
	}
	agent, err := c.roster.Agent(agentName)
	if err != nil {
		return RouteResult{}, ErrUnknownTarget
	}
	drv, ok := c.getDriver(agent.Surface)
	if !ok {
		return RouteResult{}, fmt.Errorf("agent %q: unknown surface %q", agentName, agent.Surface)
	}
	release, err := c.acquireTxn(pane)
	if err != nil {
		// The transaction lock could not be taken — typically another transaction
		// (a send/rotate/dash action) held the pane past the bound, but possibly a
		// lock-dir/fs error. Either way: not delivered, retryable — never a silent
		// partial send (flotilla-dev contract). The wording does not assert the
		// cause (contention vs infra), only the honest outcome.
		return RouteResult{Target: agentName, Outcome: OutcomeBusy, Detail: "pane unavailable (a delivery/rotate is in progress, or the pane lock could not be taken) — not delivered, retry"}, nil
	}
	defer release()

	res := RouteResult{Target: agentName}
	switch serr := c.submit(drv, pane, message); {
	case serr == nil:
		res.Outcome = OutcomeDelivered
		c.mirrorRouteToLedger(agentName, message)
	case errors.Is(serr, surface.ErrBusy):
		res.Outcome, res.Detail = OutcomeBusy, "desk is busy (mid-turn) — not delivered, retry when it is idle"
	case errors.Is(serr, surface.ErrCrashed):
		res.Outcome, res.Detail = OutcomeCrashed, "desk is at a shell (crashed) — not delivered"
	case errors.Is(serr, surface.ErrTransient):
		res.Outcome, res.Detail = OutcomeTransient, "desk state is uncertain — not delivered, retry"
	case errors.Is(serr, surface.ErrPanelBlocked):
		res.Outcome, res.Detail = OutcomeInputBlocked, "desk is input-blocked behind the agents panel — not delivered; needs a human keystroke or click into the composer at its pane"
	default: // ErrUnconfirmed, or a paste/lock error
		res.Outcome, res.Detail = OutcomeUnconfirmed, "submit could not be confirmed — escalated, not delivered"
	}
	return res, nil
}

// Resume is still gated: unlike route, its blocker is NOT the pane lock (a
// crashed/shell desk is never rotated by the detector, and resume has its own
// liveness interlock — flotilla-dev confirmed the per-call flock suffices). The
// blocker is that the resume ORCHESTRATION (runResume) currently lives in package
// main (cmd/flotilla/resume.go) and is not importable; wiring it needs that logic
// extracted into a reusable library so the dash calls the SAME tested path rather
// than a risky reimplementation. Tracked as a focused follow-on. Fails closed.
func (c *LibraryController) Resume(_ context.Context, _ string) (ResumeResult, error) {
	return ResumeResult{}, ErrResumeUnavailable
}

// mirrorRouteToLedger records a dash-routed instruction in the CoS ledger with
// dash provenance (operator(dash) → <agent>), best-effort.
func (c *LibraryController) mirrorRouteToLedger(agent, message string) {
	if c.roster == nil || c.roster.CosLedger == "" {
		return
	}
	channel, ok := c.roster.ChannelForXO(c.xo)
	if !ok && len(c.roster.Channels) > 0 {
		fmt.Fprintf(os.Stderr, "flotilla dash: XO %q has no channel binding in the federated roster — route ledger entry tagged with no channel\n", c.xo)
	}
	_ = c.appendCos(c.roster.CosLedger, cos.Entry{
		Time:    c.now(),
		Channel: channel,
		From:    dashProvenance,
		To:      agent,
		Gist:    message,
	})
}

// mirrorToLedger appends the dash note to the CoS who-knows-what ledger with dash
// provenance. Best-effort: an inert CoS, a missing channel binding, or an append
// error never fails the notify (the operator-facing post already succeeded),
// mirroring cmdNotify's mirror discipline.
func (c *LibraryController) mirrorToLedger(message string) {
	if c.roster == nil || c.roster.CosLedger == "" {
		return // CoS inert ⇒ no ledger
	}
	channel, ok := c.roster.ChannelForXO(c.xo)
	// A federated roster whose hub XO owns no channel binding is config drift —
	// the entry still records (channel ""), but surface it (parity with cmdNotify)
	// so the misconfiguration isn't masked. A legacy/clock-only XO legitimately
	// owns no channel, so warn only in the federated case.
	if !ok && len(c.roster.Channels) > 0 {
		fmt.Fprintf(os.Stderr, "flotilla dash: XO %q has no channel binding in the federated roster — ledger entry tagged with no channel\n", c.xo)
	}
	_ = c.appendCos(c.roster.CosLedger, cos.Entry{
		Time:    c.now(),
		Channel: channel,
		From:    dashProvenance,
		To:      c.xo,
		Gist:    message,
	})
}
