package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/discord"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
)

// The c2-hotline reply-watcher (#175): when an operator message is confirmed-delivered to a channel's
// XO, watch that XO's session store for the resulting turn-final and route it back to the ORIGIN
// channel, attributed — for EVERY turn, NEVER silently. It detects the reply from the harness session
// store (the ground truth of completed turns, via the ReplyMarkReader assistant-turn COUNT marker),
// NOT from pane-rendered state (racy) nor the heartbeat-cadence detector (drops sub-interval turns).
//
// Snapshot timing: the watcher is armed in the post-confirmed-delivery SetMirror hook. For a
// substantive reply the working spinner renders, so confirm.Submit returns at turn-START (the
// Idle→Working edge), making the snapshot count the pre-reply baseline. A degenerate sub-second turn
// (no spinner, already finished by snapshot) is NOT mis-routed — its count is already advanced, the
// watcher sees no further increase, and it ESCALATES on the TTL (never silent). Verbatim-content
// correlation (match the user turn, take the following assistant turn) is a possible robustness
// follow-up for that degenerate case.

const (
	replyPollInterval = 1 * time.Second
	replyTTL          = 5 * time.Minute
)

// replyDeps are the injected collaborators the reply-watch decision logic needs, so the
// snapshot→poll→route/escalate flow is unit-testable without tmux, a real session store, or Discord.
type replyDeps struct {
	// mark reads the XO's latest turn-final + the assistant-turn count marker (ReplyMarkReader via the
	// agent's driver+pane). ok=false ⇒ no substantive turn yet; err ⇒ a session/pane read failure.
	mark func(agent string) (text string, count int, ok bool, err error)
	// dest resolves the ORIGIN channel's webhook (BindingForChannel→XOAgent→Webhook). ok=false ⇒ no
	// route target — the watcher ESCALATES rather than silently dropping.
	dest func(originChannel string) (url string, ok bool)
	// post sends one chunk under the XO's identity to the origin-channel webhook.
	post func(url, username, content string) error
	// escalate raises a LOUD operator alert for the given origin channel (the never-silent backstop for
	// every non-route outcome) — routed to the channel the operator messaged from, with a primary-channel
	// fallback when the origin webhook is itself unresolvable, so the operator always sees it.
	escalate func(originChannel, msg string)
	sleep    func(time.Duration)
	logf  func(format string, args ...any)
	ttl   time.Duration
	interval time.Duration
}

// runReplyWatch watches xo's store for the reply to a just-delivered operator hotline message and
// routes it to originChannel. NEVER SILENT: every non-route outcome (unreadable session, no reply
// within the TTL, no-substantive-text reply, unresolved webhook, post failure) escalates via alert.
func runReplyWatch(ctx context.Context, d replyDeps, xo, originChannel string) {
	_, baseN, _, err := d.mark(xo)
	if err != nil {
		d.escalate(originChannel, fmt.Sprintf("hotline: can't watch %s's reply to your message (session unreadable: %v) — read its pane", xo, err))
		return
	}
	attempts := int(d.ttl / d.interval)
	if attempts < 1 {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return // superseded by a newer hotline message to this XO
		}
		d.sleep(d.interval)
		text, count, ok, err := d.mark(xo)
		if err != nil || count <= baseN {
			continue // no new assistant turn yet (or a transient store read) — keep polling until the TTL
		}
		if !ok || text == "" {
			d.escalate(originChannel, fmt.Sprintf("hotline: %s answered your message but produced no substantive text — read its pane", xo))
			return
		}
		d.route(ctx, xo, originChannel, text)
		return
	}
	d.escalate(originChannel, fmt.Sprintf("hotline: %s has not answered your message within %s — read its pane", xo, d.ttl))
}

// route posts the XO's verbatim reply to the origin channel, attributed + chunked. It re-checks ctx
// before each chunk so a watcher superseded mid-route never emits a stale reply to the old channel.
func (d replyDeps) route(ctx context.Context, xo, originChannel, text string) {
	url, ok := d.dest(originChannel)
	if !ok {
		d.escalate(originChannel, fmt.Sprintf("hotline: %s replied to you but I can't route it (no webhook for the origin channel) — read its pane", xo))
		return
	}
	chunks := discord.ChunkContent(text, mirrorChunkLimit)
	n := len(chunks)
	runes := utf8.RuneCountInString(text)
	for i, chunk := range chunks {
		if ctx.Err() != nil {
			return // superseded mid-route — do not emit a stale reply
		}
		body := "↩ " + xo + " (reply to your message):\n" + chunk
		if n > 1 {
			body = fmt.Sprintf("↩ %s (reply %d/%d):\n%s", xo, i+1, n, chunk)
		}
		if err := d.post(url, xo, body); err != nil {
			d.escalate(originChannel, fmt.Sprintf("hotline: %s replied but I couldn't post it (chunk %d/%d failed) — read its pane", xo, i+1, n))
			return
		}
	}
	d.logf("flotilla watch: hotline reply routed %s → origin channel %s (%d chunks resplen=%d)", xo, originChannel, n, runes)
}

// replyRouter manages per-XO hotline reply watchers. A newer hotline message to the same XO supersedes
// (cancels) the prior watcher and re-anchors to the new origin channel — so the reply always goes to
// the channel of the message the XO is currently answering.
type replyRouter struct {
	mu       sync.Mutex
	gen      map[string]uint64
	cancels  map[string]context.CancelFunc
	deps     replyDeps
	dispatch func(func()) // runs the watcher goroutine; injectable so tests run it synchronously
}

func newReplyRouter(deps replyDeps) *replyRouter {
	return &replyRouter{
		gen:      map[string]uint64{},
		cancels:  map[string]context.CancelFunc{},
		deps:     deps,
		dispatch: func(f func()) { go f() },
	}
}

// arm launches (or supersedes) the reply-watcher for an operator hotline message to xo from
// originChannel.
func (r *replyRouter) arm(xo, originChannel string) {
	r.mu.Lock()
	if cancel, ok := r.cancels[xo]; ok {
		cancel() // supersede the prior watcher for this XO
	}
	r.gen[xo]++
	g := r.gen[xo]
	ctx, cancel := context.WithCancel(context.Background())
	r.cancels[xo] = cancel
	r.mu.Unlock()

	r.dispatch(func() {
		runReplyWatch(ctx, r.deps, xo, originChannel)
		r.mu.Lock()
		if r.gen[xo] == g { // still the active watcher → clean up; a superseder owns its own entry
			delete(r.cancels, xo)
		}
		r.mu.Unlock()
	})
}

// --- wiring helpers (the real surface/store/Discord/alert collaborators) ---

// replyDest resolves the ORIGIN channel's return-leg webhook: BindingForChannel→XOAgent→Webhook (the
// same per-channel-XO-webhook convention the daemon's alertHook uses). ok=false ⇒ the channel has no
// binding or its XO has no webhook — the watcher ESCALATES rather than silently dropping.
func replyDest(cfg *roster.Config, secrets *roster.Secrets, originChannel string) (string, bool) {
	if secrets == nil || originChannel == "" {
		return "", false
	}
	binding, ok := cfg.BindingForChannel(originChannel)
	if !ok || binding.XOAgent == "" {
		return "", false
	}
	url, err := secrets.Webhook(binding.XOAgent)
	if err != nil || url == "" {
		return "", false
	}
	return url, true
}

// isHotlineToChannelXO reports whether j is an operator hotline message that NEEDS the watcher return
// leg: a relay delivery whose target IS the origin channel's xo_agent, AND is NOT the primary XO (which
// already has its host-local Stop-hook return leg — #175 scope is the FEDERATED c2-channel XOs).
func isHotlineToChannelXO(cfg *roster.Config, j watch.Job) bool {
	if j.Kind != "relay" || j.OriginChannel == "" || j.Agent == cfg.XOAgent {
		return false
	}
	binding, ok := cfg.BindingForChannel(j.OriginChannel)
	return ok && binding.XOAgent == j.Agent
}

// newHotlineReplyRouter builds the replyRouter wired to the real surface/store/Discord, or nil when
// secrets are absent (no return-leg webhooks to resolve). primaryAlert is the daemon's loud alert (the
// fallback when an escalation's own origin-channel webhook is unresolvable, so the operator always sees it).
func newHotlineReplyRouter(cfg *roster.Config, secrets *roster.Secrets, primaryAlert func(string)) *replyRouter {
	if secrets == nil {
		return nil
	}
	deps := replyDeps{
		mark: func(agent string) (string, int, bool, error) {
			drv, ok := surface.Get(agentSurface(cfg, agent))
			if !ok {
				return "", 0, false, fmt.Errorf("unknown surface for agent %q", agent)
			}
			rr, ok := drv.(surface.ReplyMarkReader)
			if !ok {
				return "", 0, false, fmt.Errorf("surface %q cannot read replies (no ReplyMarkReader)", drv.Name())
			}
			pane, err := deliver.ResolvePane(agentTitle(cfg, agent))
			if err != nil {
				return "", 0, false, err
			}
			return rr.LatestTurnMark(pane)
		},
		dest: func(originChannel string) (string, bool) { return replyDest(cfg, secrets, originChannel) },
		post: discord.Post,
		escalate: func(originChannel, msg string) {
			// Route the escalation to the channel the operator messaged from; fall back to the primary
			// operator channel if the origin webhook is unresolvable — so the operator ALWAYS sees it.
			if url, ok := replyDest(cfg, secrets, originChannel); ok {
				_ = discord.Post(url, "flotilla-watch", "⚠️ "+msg)
				return
			}
			primaryAlert(msg)
		},
		sleep:    time.Sleep,
		logf:     log.Printf,
		ttl:      replyTTL,
		interval: replyPollInterval,
	}
	return newReplyRouter(deps)
}

// logReplyLegCoverage prints, at startup, which c2-channel (federated) XOs have a resolvable
// return-leg webhook — so a mis-provisioned channel is visible at boot, not at the first dropped
// reply. The primary XO is excluded (it uses its host-local Stop-hook).
func logReplyLegCoverage(cfg *roster.Config, secrets *roster.Secrets) {
	if secrets == nil {
		return
	}
	var withLeg, without []string
	for _, ch := range cfg.Bindings() {
		if ch.XOAgent == "" || ch.XOAgent == cfg.XOAgent {
			continue // unbound, or the primary XO (Stop-hook return leg)
		}
		if url, err := secrets.Webhook(ch.XOAgent); err == nil && url != "" {
			withLeg = append(withLeg, ch.XOAgent)
		} else {
			without = append(without, ch.XOAgent)
		}
	}
	fmt.Printf("flotilla watch: c2 hotline return leg — %d XO(s) routable %v; %d with no webhook (replies will escalate) %v\n",
		len(withLeg), withLeg, len(without), without)
}
