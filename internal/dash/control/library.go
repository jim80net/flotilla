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

	// Route seams — the confirmed-delivery transaction. resolvePane + acquireTxn +
	// submit mirror the cmdSend path (cmd/flotilla/main.go) EXACTLY so the dash
	// keys the cross-process lock on the SAME resolved pane target as `flotilla
	// send` and the watch Injector/rotate (the contract that makes the lock
	// serialize cross-process — design §5).
	getDriver   func(name string) (surface.Driver, bool)
	resolvePane func(title string) (string, error)
	acquireTxn  func(target string) (release func(), err error)
	submit      func(drv surface.Driver, pane, text string) error
}

// NewLibrary builds the production controller. secretsPath may be "" (then notify
// returns ErrWebhookMissing); roster + xo are required for webhook + ledger
// resolution. tr is the coordination Transport that backs the notify's outbound
// post: because the operator-note destination is a Discord webhook
// (secrets.Webhook(xo)), tr is the DISCORD transport, constructed at the wiring
// boundary (cmd/flotilla/dash.go) and injected here as an interface VALUE — so this
// package depends on internal/transport, not on the concrete internal/discord
// package. The notify resolves the webhook string from secrets, wraps it in a
// transport.NewWebhookDestination, and posts via tr.Post; the over-length guard
// reads tr.MaxContentRunes(). This closes the PR1 TODO(#188/#106) seam.
func NewLibrary(rc *roster.Config, xo, secretsPath string, tr transport.Transport) *LibraryController {
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
			return tr.Post(transport.NewWebhookDestination(hook), username, content)
		},
		// The over-length guard reads the medium's own per-message cap from the
		// transport (discord = 2000), not a hard-coded discord constant leaking across
		// the seam.
		maxContentRunes: tr.MaxContentRunes(),
		loadSecrets:     roster.LoadSecrets,
		appendCos:       cos.Append,
		now:             time.Now,
		getDriver:       surface.Get,
		resolvePane:     deliver.ResolvePane,
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
// serialized cross-process by the per-pane TRANSACTION lock (design §5). It
// mirrors the cmdSend path EXACTLY: resolve the agent → its driver → its pane
// (deliver.ResolvePane(agent.Title())) → acquire the txn lock keyed on that
// resolved pane target → Confirm.Submit → Release. The typed surface outcome
// (delivered/busy/crashed/transient/unconfirmed) is returned as a RouteResult
// (these are informational outcomes the operator must see, NOT errors); only a
// hard failure (unknown target, unknown surface, pane-resolution failure) is an
// error. A lock-contention timeout is surfaced as a busy/not-delivered outcome
// (retryable) — never a silent partial send.
func (c *LibraryController) Route(_ context.Context, target, message string) (RouteResult, error) {
	if strings.TrimSpace(message) == "" {
		return RouteResult{}, ErrEmptyMessage
	}
	agentName, err := c.resolveTarget(target)
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
	// Resolve the pane the SAME way cmdSend + the watch Injector/rotate do
	// (deliver.ResolvePane(agent.Title())) — the lock keys on this exact target,
	// so every transaction writer computes one identical key per pane (the
	// cross-process serialization contract; a divergent resolve would silently
	// fail to serialize).
	pane, err := c.resolvePane(agent.Title())
	if err != nil {
		return RouteResult{}, fmt.Errorf("resolve pane for %s: %w", agentName, err)
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

// resolveTarget maps a route target to a canonical roster agent name by delegating
// to the ONE shared roster-wide resolver (roster.ResolveTarget) — the SAME function
// the web transport's ResolveDestination calls, so the empty→XO default and the
// exact-wins-else-ambiguous case-collision rule cannot drift between the dash control
// surface and the web transport (transport spec "The roster-wide resolver is shared,
// not forked"). The dash resolves ROSTER-WIDE (a host-local operator console with no
// Discord channel context can address any desk), which is preserved verbatim inside
// the shared resolver — see roster.ResolveTarget's doc for the boundary-transcending
// rationale and the case-collision rule.
func (c *LibraryController) resolveTarget(target string) (string, error) {
	return c.roster.ResolveTarget(c.xo, target)
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
