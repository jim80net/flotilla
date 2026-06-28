package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/transport"
	"github.com/jim80net/flotilla/internal/watch"
)

// The c2-hotline reply-watcher (#175): when an operator message is confirmed-delivered to a channel's
// XO, watch that XO's session store for the VERBATIM reply and route it back to the ORIGIN channel,
// attributed — for EVERY turn, NEVER silently.
//
// CORRELATION (the load-bearing property): the reply is the assistant turn that FOLLOWS the operator
// message's recorded USER turn in the session store (claudestore/grokstore ReplyAfter) — NOT a bare
// turn-count delta. Anchoring to the user turn makes the reply the answer to THIS message, immune to a
// QUEUED message (where the XO's current, unrelated turn would otherwise be mis-routed) or an
// interleaved turn. It is also timing-independent — whether the reply already completed (a fast turn)
// or lands later, ReplyAfter finds it — so there is no pane-state race and no heartbeat-tick dependency.
//
// TTL: a soft bound escalates ONCE ("still working, I'll route it when it lands") but KEEPS watching to
// a hard bound, so a long XO answer is still routed rather than lost. Every non-route outcome is loud.

const (
	replyPollInterval = 2 * time.Second
	replySoftTTL      = 5 * time.Minute  // no reply yet by here ⇒ escalate ONCE, keep watching
	replyHardTTL      = 30 * time.Minute // give up watching (a federated XO answering a hotline can do real work)
)

// replyDeps are the injected collaborators the reply-watch decision logic needs, so the
// poll→route/escalate flow is unit-testable without tmux, a real session store, or Discord.
type replyDeps struct {
	// reply reads the XO's VERBATIM reply to operatorMsg (ReplyReader.ReplyAfter via the agent's
	// driver+pane): the assistant turn following operatorMsg's recorded user turn. found=false ⇒ the
	// reply has not landed yet (keep polling); err ⇒ a session/pane read failure (transient — poll on,
	// the hard TTL is the backstop).
	reply func(agent, operatorMsg string) (text string, found bool, err error)
	// dest resolves the ORIGIN channel's webhook (BindingForChannel→XOAgent→Webhook). ok=false ⇒ no
	// route target — the watcher ESCALATES rather than silently dropping.
	dest func(originChannel string) (url string, ok bool)
	// post sends one chunk under the XO's identity to the origin-channel webhook.
	post func(url, username, content string) error
	// escalate raises a LOUD operator alert for the given origin channel (the never-silent backstop) —
	// routed to the channel the operator messaged from, with a primary-channel fallback when the origin
	// webhook is itself unresolvable, so the operator always sees it.
	escalate func(originChannel, msg string)
	sleep    func(time.Duration)
	logf     func(format string, args ...any)
	softTTL  time.Duration
	hardTTL  time.Duration
	interval time.Duration
}

// runReplyWatch polls xo's store for its reply to operatorMsg and routes it to originChannel. NEVER
// SILENT: a found reply routes; otherwise the soft TTL escalates once ("still working") and the hard
// TTL escalates a final "hasn't answered" — and route() escalates on a webhook miss / post failure.
func runReplyWatch(ctx context.Context, d replyDeps, xo, originChannel, operatorMsg string) {
	attempts := int(d.hardTTL / d.interval)
	if attempts < 1 {
		attempts = 1
	}
	softAt := int(d.softTTL / d.interval)
	notified := false
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return // superseded by a newer hotline message to this XO, or the daemon is shutting down
		}
		d.sleep(d.interval)
		text, found, err := d.reply(xo, operatorMsg)
		if err == nil && found && text != "" {
			d.route(ctx, xo, originChannel, text)
			return
		}
		if !notified && i >= softAt {
			d.escalate(originChannel, fmt.Sprintf("hotline: %s is working on your message — I'll route the reply here when it lands (or read its pane)", xo))
			notified = true
		}
	}
	d.escalate(originChannel, fmt.Sprintf("hotline: %s has not answered your message within %s — read its pane", xo, d.hardTTL))
}

// route posts the XO's verbatim reply to the origin channel, attributed + chunked. It re-checks ctx
// before each chunk so a watcher superseded mid-route never emits a stale reply to the old channel. A
// failed chunk escalates and names the partial delivery so the operator knows to read the pane for the
// remainder (the one place a long reply cannot be fully self-delivered).
func (d replyDeps) route(ctx context.Context, xo, originChannel, text string) {
	url, ok := d.dest(originChannel)
	if !ok {
		d.escalate(originChannel, fmt.Sprintf("hotline: %s replied to you but I can't route it (no webhook for the origin channel) — read its pane", xo))
		return
	}
	chunks := transport.Chunk(text, mirrorChunkLimit)
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
			d.escalate(originChannel, fmt.Sprintf("hotline: %s replied but I could only post %d of %d parts (part %d failed) — read its pane for the rest", xo, i, n, i+1))
			return
		}
	}
	d.logf("flotilla watch: hotline reply routed %s → origin channel %s (%d chunks resplen=%d)", xo, originChannel, n, runes)
}

// replyRouter manages per-XO hotline reply watchers. A newer hotline message to the same XO supersedes
// (cancels) the prior watcher and re-anchors to the new origin channel — so the reply always goes to
// the channel of the message the XO is currently answering. All watchers are children of the router's
// parent context, so Stop() (wired to the daemon's shutdown) cancels every in-flight watcher.
type replyRouter struct {
	mu       sync.Mutex
	parent   context.Context
	gen      map[string]uint64
	cancels  map[string]context.CancelFunc
	deps     replyDeps
	dispatch func(func()) // runs the watcher goroutine; injectable so tests run it synchronously
}

func newReplyRouter(parent context.Context, deps replyDeps) *replyRouter {
	return &replyRouter{
		parent:   parent,
		gen:      map[string]uint64{},
		cancels:  map[string]context.CancelFunc{},
		deps:     deps,
		dispatch: func(f func()) { go f() },
	}
}

// arm launches (or supersedes) the reply-watcher for an operator hotline message (operatorMsg) to xo
// from originChannel.
func (r *replyRouter) arm(xo, originChannel, operatorMsg string) {
	r.mu.Lock()
	if cancel, ok := r.cancels[xo]; ok {
		cancel() // supersede the prior watcher for this XO
	}
	r.gen[xo]++
	g := r.gen[xo]
	ctx, cancel := context.WithCancel(r.parent)
	r.cancels[xo] = cancel
	r.mu.Unlock()

	r.dispatch(func() {
		runReplyWatch(ctx, r.deps, xo, originChannel, operatorMsg)
		r.mu.Lock()
		if r.gen[xo] == g { // still the active watcher → clean up; a superseder owns its own entry
			delete(r.cancels, xo)
		}
		r.mu.Unlock()
	})
}

// Stop cancels every in-flight watcher (wired to the daemon shutdown), so no watcher keeps polling or
// posts a reply after the daemon is told to stop.
func (r *replyRouter) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, cancel := range r.cancels {
		cancel()
	}
	r.cancels = map[string]context.CancelFunc{}
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
// leg: a relay delivery whose target IS the origin channel's xo_agent. This covers EVERY channel's XO,
// INCLUDING the primary (#177 unified the primary XO into the flotilla-native watcher, retiring its
// host-local Stop-hook — the watcher has the same replies-only semantics, more robustly). Heartbeat /
// detector ticks (j.Kind != "relay") never arm it; a channel MEMBER (not the channel's XO) does not.
func isHotlineToChannelXO(cfg *roster.Config, j watch.Job) bool {
	if j.Kind != "relay" || j.OriginChannel == "" {
		return false
	}
	binding, ok := cfg.BindingForChannel(j.OriginChannel)
	return ok && binding.XOAgent == j.Agent
}

// newHotlineReplyRouter builds the replyRouter wired to the real surface/store/Discord, or nil when
// secrets are absent (no return-leg webhooks to resolve). parent is the daemon's shutdown context (so
// Stop cancels in-flight watchers); primaryAlert is the daemon's loud alert (the fallback when an
// escalation's own origin-channel webhook is unresolvable, so the operator always sees it).
func newHotlineReplyRouter(parent context.Context, cfg *roster.Config, secrets *roster.Secrets, tr transport.Transport, primaryAlert func(string)) *replyRouter {
	if secrets == nil || tr == nil {
		return nil
	}
	// post sends one chunk to a resolved webhook URL through the transport seam (the
	// credential stays inside the transport's Destination). The replyDeps.dest/post
	// signatures stay url-string-based so the reply-watch decision logic — and its
	// suite — are unchanged; only the wiring routes the send through the transport.
	post := func(url, username, content string) error {
		return tr.Post(transport.NewWebhookDestination(url), username, content)
	}
	deps := replyDeps{
		reply: func(agent, operatorMsg string) (string, bool, error) {
			drv, ok := surface.Get(agentSurface(cfg, agent))
			if !ok {
				return "", false, fmt.Errorf("unknown surface for agent %q", agent)
			}
			rr, ok := drv.(surface.ReplyReader)
			if !ok {
				return "", false, fmt.Errorf("surface %q cannot read replies (no ReplyReader)", drv.Name())
			}
			pane, err := deliver.ResolvePane(agentTitle(cfg, agent))
			if err != nil {
				return "", false, err
			}
			return rr.ReplyAfter(pane, operatorMsg)
		},
		dest: func(originChannel string) (string, bool) { return replyDest(cfg, secrets, originChannel) },
		post: post,
		escalate: func(originChannel, msg string) {
			// Route the escalation to the channel the operator messaged from; fall back to the primary
			// operator channel if the origin webhook is unresolvable OR the post FAILS — so the operator
			// ALWAYS sees it (an ignored post error here would be a hole in the never-silent guarantee).
			if url, ok := replyDest(cfg, secrets, originChannel); ok {
				if err := post(url, "flotilla-watch", "⚠️ "+msg); err == nil {
					return
				}
				// the origin-channel post failed — fall through to the primary alert below
			}
			primaryAlert(msg)
		},
		sleep:    time.Sleep,
		logf:     log.Printf,
		softTTL:  replySoftTTL,
		hardTTL:  replyHardTTL,
		interval: replyPollInterval,
	}
	return newReplyRouter(parent, deps)
}

// logReplyLegCoverage prints, at startup, which channel XOs have a resolvable return-leg webhook — so a
// mis-provisioned channel is visible at boot, not at the first dropped reply. Covers EVERY channel's XO
// including the primary (#177: the watcher is the unified return leg for all XOs).
func logReplyLegCoverage(cfg *roster.Config, secrets *roster.Secrets) {
	if secrets == nil {
		return
	}
	var withLeg, without []string
	for _, ch := range cfg.Bindings() {
		if ch.XOAgent == "" {
			continue // unbound channel
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
