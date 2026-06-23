package watch

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/discord"
	"github.com/jim80net/flotilla/internal/relay"
	"github.com/jim80net/flotilla/internal/roster"
)

// Catch-up reconciler defaults. The common case (a reconnect gap of 1-2 messages
// recovered within a poll interval) auto-relays; a bulk/ancient backlog alerts.
const (
	defaultPollInterval = 30 * time.Second
	defaultPageLimit    = 100            // Discord's max messages per REST page
	defaultMaxPages     = 5              // ≤500 messages drained per sweep; beyond → bulk-alert + next sweep
	defaultBulkCap      = 5              // > this many recovered ⇒ alert, don't auto-relay
	defaultStaleCeiling = 24 * time.Hour // oldest recovered older than this ⇒ alert (don't replay ancient directives)
	defaultFailThresh   = 5              // consecutive failed sweeps ⇒ escalate the backstop-down alert once
)

// MessageReader is the subset of *discord.REST the poller needs — a seam so the
// reconcile/disposition logic is unit-testable with a fake (no live Discord).
type MessageReader interface {
	MessagesAfterPaged(channelID, afterID string, pageLimit, maxPages int) ([]discord.Message, bool, error)
	Latest(channelID string) (discord.Message, bool, error)
}

// Catchup is the REST-based at-least-once ingestion backstop: a single goroutine
// that periodically (and on every gateway reconnect) reconciles each bound channel
// against the durable cursor, recovering operator messages the live gateway path
// missed. It is independent of the gateway websocket — REST works precisely when the
// websocket is unhealthy, which is when messages are lost.
type Catchup struct {
	cfg    *roster.Config
	gate   *dedup
	reader MessageReader
	relay  *Relay       // reuse route() — the same delivery seam as the live path
	notify func(string) // one-line catch-up notice (recovered N)
	alert  func(string) // LOUD: bulk/stale backlog + backstop-down liveness

	pollInterval time.Duration
	pageLimit    int
	maxPages     int
	bulkCap      int
	staleCeiling time.Duration
	failThresh   int

	now  func() time.Time
	kick chan struct{}

	// liveness (touched only from the single run goroutine)
	consecFails int
	escalated   bool
}

// NewCatchup builds the reconciler: it constructs the shared dedup gate (cursor at
// cursorPath), WIRES it into rel (so the live gateway path dedups against the poller
// — SetGate), and returns a *Catchup the daemon starts with Run(ctx) and kicks via
// Kick() on a gateway reconnect. reader is *discord.REST in production. The kick
// channel is buffered (size 1) so a reconnect during an in-flight sweep coalesces to
// exactly one follow-up sweep.
func NewCatchup(cfg *roster.Config, rel *Relay, reader MessageReader, cursorPath string, notify, alert func(string)) *Catchup {
	gate := newDedup(cursorStore{path: cursorPath}, defaultSeenCap)
	rel.SetGate(gate)
	return &Catchup{
		cfg:          cfg,
		gate:         gate,
		reader:       reader,
		relay:        rel,
		notify:       notify,
		alert:        alert,
		pollInterval: defaultPollInterval,
		pageLimit:    defaultPageLimit,
		maxPages:     defaultMaxPages,
		bulkCap:      defaultBulkCap,
		staleCeiling: defaultStaleCeiling,
		failThresh:   defaultFailThresh,
		now:          time.Now,
		kick:         make(chan struct{}, 1),
	}
}

// Kick requests an immediate sweep (non-blocking — the gateway's reconnect handler
// calls this; it must never block the gateway goroutine). A kick arriving during a
// sweep is buffered and runs one follow-up sweep; a second is dropped (coalesced).
func (c *Catchup) Kick() {
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

// Run is the single reconcile goroutine: an initial sweep on start (tail-init on
// first boot; catch-up after a restart), then a sweep on each tick OR reconnect
// kick. Synchronous in this one goroutine, so there is never more than one in-flight
// sweep (single-flight by construction — F3).
func (c *Catchup) Run(ctx context.Context) {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	c.sweep()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sweep()
		case <-c.kick:
			c.sweep()
		}
	}
}

// sweep reconciles every bound channel once and records backstop liveness.
func (c *Catchup) sweep() {
	bindings := c.cfg.Bindings()
	failures := 0
	for _, b := range bindings {
		if err := c.sweepChannel(b); err != nil {
			failures++
			log.Printf("flotilla watch: relay catch-up sweep failed for channel %q: %v", channelLabel(b), err)
		}
	}
	// A sweep is "failed" only when EVERY channel errored (a total REST outage —
	// the backstop is down). A single channel's error is a per-channel degrade,
	// logged above, not a backstop-down escalation.
	c.recordHealth(len(bindings) > 0 && failures == len(bindings))
}

// sweepChannel reconciles one channel: tail-init on first boot, else fetch the
// contiguous run above the cursor, relay/alert the recovered operator messages, and
// commit the cursor AFTER (enqueue-then-commit — Invariant 2 / F7).
func (c *Catchup) sweepChannel(b roster.Channel) error {
	cur, ok := c.gate.cursorOf(b.ChannelID)
	if !ok {
		// First boot: tail-init the cursor to the channel's latest id WITHOUT relaying
		// history (never replay the backlog on a fresh deploy).
		latest, has, err := c.reader.Latest(b.ChannelID)
		if err != nil {
			return err
		}
		var id uint64
		if has {
			id = latest.SnowID
		}
		return c.gate.initCursor(b.ChannelID, id)
	}

	batch, capped, err := c.reader.MessagesAfterPaged(b.ChannelID, strconv.FormatUint(cur, 10), c.pageLimit, c.maxPages)
	if err != nil {
		return err
	}
	if len(batch) == 0 {
		return nil // nothing above the cursor; cursor unchanged
	}

	// Accept + empty-guard FIRST (so the gate's seen-set holds only relayed ids — F4),
	// then classify (dedup vs the live path). The cursor advances over the FULL batch
	// (incl. non-operator messages) so the non-operator tail is not re-fetched forever.
	candidates := c.accepted(batch)
	toRelay := c.gate.classify(b.ChannelID, candidates)
	c.disposition(b, toRelay, capped) // enqueue or alert — BEFORE commit
	return c.gate.commit(b.ChannelID, MaxSnowflake(batch))
}

// accepted filters a fetched batch to the operator-authored, non-empty, non-webhook
// messages — the same Accept + empty-content policy the live path applies.
func (c *Catchup) accepted(batch []discord.Message) []discord.Message {
	out := make([]discord.Message, 0, len(batch))
	for _, m := range batch {
		if !relay.Accept(m.WebhookID, m.AuthorID, c.cfg.OperatorUserID) {
			continue
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		out = append(out, m)
	}
	return out
}

// disposition decides the fate of the recovered operator messages: auto-relay the
// few-and-recent (count-primary, the common reconnect/restart case), alert the
// bulk-or-ancient (don't blind-inject a long-outage backlog out of context). The
// cursor advances in BOTH cases (the alerted backlog has been surfaced; re-alerting
// it every sweep would be an alert storm — the operator recovers it via `inbox`).
func (c *Catchup) disposition(b roster.Channel, toRelay []discord.Message, capped bool) {
	if len(toRelay) == 0 {
		// No OPERATOR messages recovered this sweep (the run may have been all
		// non-operator chatter, even a full/capped page of it). Intentionally no alert
		// here — there is nothing the operator needs to act on. The cursor still
		// advances (caller), and any operator messages above a cap are surfaced by a
		// later sweep continuing from the advanced cursor. Never a drop, only deferred.
		return
	}
	oldest := toRelay[0].Timestamp // ascending — [0] is the oldest (classify preserves batch order)
	tooOld := !oldest.IsZero() && c.now().Sub(oldest) > c.staleCeiling
	if capped || len(toRelay) > c.bulkCap || tooOld {
		if c.alert != nil {
			c.alert(fmt.Sprintf("%d operator message(s) on %s were NOT auto-delivered (bulk/stale backlog from a gateway/outage gap) — run `flotilla inbox %s` to view and re-send the still-relevant ones",
				len(toRelay), channelLabel(b), inboxArg(b)))
		}
		return
	}
	for _, m := range toRelay {
		c.relay.route(b.ChannelID, b, m.Content)
	}
	if c.notify != nil {
		c.notify(fmt.Sprintf("recovered %d operator message(s) on %s the live gateway missed — delivered via catch-up",
			len(toRelay), channelLabel(b)))
	}
}

// recordHealth tracks consecutive backstop-down sweeps and escalates ONCE past the
// threshold (the at-least-once guarantee is down while live delivery continues),
// re-arming on the next healthy sweep. The meta-#161 guard: a silently-dead poller
// would re-create the very failure this backstop exists to prevent.
func (c *Catchup) recordHealth(sweepFailed bool) {
	if sweepFailed {
		c.consecFails++
		if c.consecFails >= c.failThresh && !c.escalated {
			c.escalated = true
			if c.alert != nil {
				c.alert(fmt.Sprintf("relay catch-up has failed %d consecutive sweeps — the at-least-once ingestion backstop is DOWN (live gateway delivery continues). Check the bot token / network; retries continue.", c.consecFails))
			}
		}
		return
	}
	c.consecFails = 0
	c.escalated = false
}

// channelLabel is a human label for a binding (its role, else its id).
func channelLabel(b roster.Channel) string {
	if b.Role != "" {
		return b.Role
	}
	return b.ChannelID
}

// inboxArg is what to pass `flotilla inbox` to read this channel (its role if set,
// else its raw id — both are resolvable by the inbox command).
func inboxArg(b roster.Channel) string {
	if b.Role != "" {
		return b.Role
	}
	return b.ChannelID
}
