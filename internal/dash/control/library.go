package control

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/cos"
	"github.com/jim80net/flotilla/internal/discord"
	"github.com/jim80net/flotilla/internal/roster"
)

// dashProvenance is the CoS ledger "from" marker for a dash-issued action, so a
// dash control action is distinguishable from a Discord-originated one in the
// who-knows-what audit record (design §4 / spec "Dash control actions are
// recorded for audit").
const dashProvenance = "operator(dash)"

// LibraryController is the ONE real Controller: thin proxies over the existing
// delivery library. Notify (discord.Post) is live; Route and Resume drive panes
// and FAIL CLOSED until the cross-process pane-transaction lock lands (design §5)
// — they are wired to surface.Confirm.Submit / the resume recipe path in the
// follow-up that integrates flotilla-dev's lock. The library calls are injected
// as seams so the policy here is unit-testable without a real Discord/tmux.
type LibraryController struct {
	roster      *roster.Config
	xo          string // the hub XO whose webhook a dash note posts under
	secretsPath string // for the notify webhook ("" ⇒ notify unavailable)

	// Seams (production wires the real library calls; tests inject fakes).
	post        func(webhook, username, content string) error
	loadSecrets func(path string) (*roster.Secrets, error)
	appendCos   func(path string, e cos.Entry) error
	now         func() time.Time
}

// NewLibrary builds the production controller. secretsPath may be "" (then notify
// returns ErrWebhookMissing); roster + xo are required for webhook + ledger
// resolution.
func NewLibrary(rc *roster.Config, xo, secretsPath string) *LibraryController {
	return &LibraryController{
		roster:      rc,
		xo:          xo,
		secretsPath: secretsPath,
		post:        discord.Post,
		loadSecrets: roster.LoadSecrets,
		appendCos:   cos.Append,
		now:         time.Now,
	}
}

// Notify posts an operator note to the fleet channel under the XO's webhook with
// the dash-provenance username, then mirrors it to the CoS ledger (best-effort).
func (c *LibraryController) Notify(_ context.Context, message string) error {
	if strings.TrimSpace(message) == "" {
		return ErrEmptyMessage
	}
	// This message IS operator-facing content — reject an over-length body cleanly
	// (never silently truncate the operator's note), mirroring cmdNotify.
	if len([]rune(message)) > discord.MaxContentRunes {
		return fmt.Errorf("%w: %d chars (limit %d)", ErrOverLength, len([]rune(message)), discord.MaxContentRunes)
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

// Route is GATED until the cross-process pane lock lands (design §5): it drives a
// pane and must serialize against watch's detector rotate, which requires the
// lock. Fails closed.
func (c *LibraryController) Route(_ context.Context, _, _ string) (RouteResult, error) {
	return RouteResult{}, ErrControlUnavailable
}

// Resume is GATED until the cross-process pane lock lands (design §5). Fails closed.
func (c *LibraryController) Resume(_ context.Context, _ string) (ResumeResult, error) {
	return ResumeResult{}, ErrControlUnavailable
}

// mirrorToLedger appends the dash note to the CoS who-knows-what ledger with dash
// provenance. Best-effort: an inert CoS, a missing channel binding, or an append
// error never fails the notify (the operator-facing post already succeeded),
// mirroring cmdNotify's mirror discipline.
func (c *LibraryController) mirrorToLedger(message string) {
	if c.roster == nil || c.roster.CosLedger == "" {
		return // CoS inert ⇒ no ledger
	}
	channel, _ := c.roster.ChannelForXO(c.xo)
	_ = c.appendCos(c.roster.CosLedger, cos.Entry{
		Time:    c.now(),
		Channel: channel,
		From:    dashProvenance,
		To:      c.xo,
		Gist:    message,
	})
}
