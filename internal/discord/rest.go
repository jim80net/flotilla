package discord

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Message is the relay-relevant projection of a Discord message — the fields the
// catch-up reconciler and the `inbox` command need, decoupled from discordgo's
// full type. SnowID is the message id parsed as a snowflake (a time-ordered
// uint64) so callers compare/advance a cursor numerically.
type Message struct {
	ID        string
	SnowID    uint64
	AuthorID  string
	WebhookID string
	Content   string
	Timestamp time.Time
}

// channelMessagesFunc matches discordgo's Session.ChannelMessages — the only REST
// call this helper needs. It is a seam so the projection + ordering logic is unit
// testable with a fake, without a live Discord session.
type channelMessagesFunc func(channelID string, limit int, beforeID, afterID, aroundID string) ([]*discordgo.Message, error)

// REST is a READ-ONLY Discord REST client: it builds a discordgo session for its
// auth + rate-limiter but NEVER opens the gateway websocket. It exists so the
// catch-up reconciler and `flotilla inbox` can read channel history independent of
// gateway-websocket health — which is precisely the state in which messages get
// lost (a reconnect/resume-failure gap), so the recovery path must not depend on
// the same websocket that just failed.
type REST struct {
	fetch channelMessagesFunc
	sess  *discordgo.Session // retained only so Close() can release transport resources
}

// NewREST builds a REST client from the bot token. It constructs a discordgo
// session (for the authenticated, rate-limited REST transport) but does not call
// Open() — no websocket, no intents, no gateway lifecycle, and therefore NO
// background goroutines (those start only on Open). One session is built per process
// (daemon-lifetime for the poller; one-shot for the `inbox` CLI), not per request.
func NewREST(botToken string) (*REST, error) {
	s, err := discordgo.New("Bot " + botToken)
	if err != nil {
		return nil, fmt.Errorf("discord rest session: %w", err)
	}
	return &REST{
		fetch: func(ch string, limit int, before, after, around string) ([]*discordgo.Message, error) {
			return s.ChannelMessages(ch, limit, before, after, around)
		},
		sess: s,
	}, nil
}

// Close releases the underlying session's transport. Safe on a never-Open()'d
// session (discordgo's CloseWithCode is a no-op when no websocket is connected) and
// safe to call on a nil/zero REST. The daemon's poller keeps its session for the
// process lifetime (no Close needed — it dies with the process); the short-lived
// `inbox` CLI defers it for hygiene.
func (r *REST) Close() error {
	if r == nil || r.sess == nil {
		return nil
	}
	return r.sess.Close()
}

// MessagesAfter returns up to limit messages with id > afterID, in ASCENDING id
// order. Discord's `after` returns the OLDEST block above afterID (the contiguous
// messages nearest the cursor), newest-first within the batch (verified by live
// probe 2026-06-23, channel 1511357941893304462); we sort to ascending so a caller
// can walk forward and advance a cursor monotonically without leapfrogging.
func (r *REST) MessagesAfter(channelID, afterID string, limit int) ([]Message, error) {
	raw, err := r.fetch(channelID, limit, "", afterID, "")
	if err != nil {
		return nil, err
	}
	return project(raw), nil
}

// MessagesAfterPaged walks the CONTIGUOUS run of messages above afterID, ascending,
// page by page (each page's max id becomes the next `after`). It is the catch-up
// reconciler's fetch: the returned batch is always a contiguous run from afterID
// upward, so a caller that commits its cursor to the batch's max can never leapfrog
// an unfetched older message (F1 / Invariant 3). It stops when a page is NOT full
// (the channel is drained above the cursor) OR the page cap is hit; capped=true
// means the cap stopped it and more messages remain ABOVE the returned batch (the
// caller alerts a bulk backlog and the next sweep continues from the advanced
// cursor).
//
// The page-fullness check keys on the RAW discordgo page length (pre-projection),
// never on the projected/filtered count — otherwise a full page that happens to
// contain only dropped entries (a non-operator message, an unparseable id) would
// look "short" and stop the walk early, stranding operator messages above it
// (systems-review round 2). `after` advances by the raw max id for the same reason.
func (r *REST) MessagesAfterPaged(channelID, afterID string, pageLimit, maxPages int) (out []Message, capped bool, err error) {
	after := afterID
	for page := 0; page < maxPages; page++ {
		raw, err := r.fetch(channelID, pageLimit, "", after, "")
		if err != nil {
			return out, false, err
		}
		out = append(out, project(raw)...)
		if len(raw) < pageLimit { // RAW count — the true page-fullness signal
			return sortedUniqueAscending(out), false, nil
		}
		maxID, ok := maxRawSnowflake(raw)
		if !ok { // no parseable id to continue from (cannot happen for real Discord data)
			return sortedUniqueAscending(out), false, nil
		}
		after = maxID
	}
	return sortedUniqueAscending(out), true, nil
}

// Latest returns the channel's single most recent message; ok=false if the
// channel is empty. Used to tail-initialize a cursor on first boot (so prior
// history is never replayed).
func (r *REST) Latest(channelID string) (Message, bool, error) {
	raw, err := r.fetch(channelID, 1, "", "", "")
	if err != nil {
		return Message{}, false, err
	}
	out := project(raw)
	if len(out) == 0 {
		return Message{}, false, nil
	}
	return out[len(out)-1], true, nil
}

// ChannelMessagesMax is discordgo's per-request cap on ChannelMessages. Requests
// above this are silently truncated — pagination must use non-overlapping before
// pages rather than re-fetching an ever-wider tail.
const ChannelMessagesMax = 100

// DefaultRecentSinceMaxPages caps backward walks for time-bounded history (50 pages
// × 100 msgs = 5000 messages worst-case per channel per sweep).
const DefaultRecentSinceMaxPages = 50

// Recent returns up to limit of the channel's most recent messages, ASCENDING.
// Limits above ChannelMessagesMax are satisfied via non-overlapping before-pages.
func (r *REST) Recent(channelID string, limit int) ([]Message, error) {
	if limit <= 0 {
		return nil, nil
	}
	got, _, err := r.recentWalkBackward(channelID, recentWalkOpts{maxMessages: limit})
	return got, err
}

// RecentSince returns ascending messages with Timestamp >= since, walking backward
// from the channel head in non-overlapping before-pages until the cutoff is
// reached or history is exhausted.
func (r *REST) RecentSince(channelID string, since time.Time, maxPages int) ([]Message, error) {
	if maxPages < 1 {
		maxPages = DefaultRecentSinceMaxPages
	}
	got, _, err := r.recentWalkBackward(channelID, recentWalkOpts{since: since, maxPages: maxPages})
	return got, err
}

type recentWalkOpts struct {
	maxMessages int       // Recent: return the N newest messages
	since       time.Time // RecentSince: stop when oldest page edge reaches since
	maxPages    int       // page-budget cap (RecentSince); derived for Recent
}

// recentWalkBackward fetches channel history newest→oldest in before-pages, merges
// ascending, and stops on count/cutoff/exhaustion. capped=true when maxPages stopped
// the walk before the stop condition was met.
func (r *REST) recentWalkBackward(channelID string, opts recentWalkOpts) (out []Message, capped bool, err error) {
	pageLimit := ChannelMessagesMax
	maxPages := opts.maxPages
	if maxPages < 1 {
		if opts.maxMessages > 0 {
			maxPages = (opts.maxMessages + pageLimit - 1) / pageLimit
		}
		if maxPages < 1 {
			maxPages = 1
		}
	}
	haveSince := !opts.since.IsZero()
	before := ""
	for page := 0; page < maxPages; page++ {
		reqLimit := pageLimit
		if opts.maxMessages > 0 {
			remaining := opts.maxMessages - len(out)
			if remaining <= 0 {
				break
			}
			if remaining < reqLimit {
				reqLimit = remaining
			}
		}
		raw, err := r.fetch(channelID, reqLimit, before, "", "")
		if err != nil {
			return nil, false, err
		}
		if len(raw) == 0 {
			return trimSince(out, opts.since), false, nil
		}
		batch := project(raw)
		if len(batch) == 0 {
			return trimSince(out, opts.since), false, nil
		}
		if before == "" {
			out = batch
		} else {
			out = append(batch, out...)
		}
		out = sortedUniqueAscending(out)
		if opts.maxMessages > 0 && len(out) > opts.maxMessages {
			out = out[len(out)-opts.maxMessages:]
		}
		oldest := out[0]
		if haveSince && (oldest.Timestamp.IsZero() || !oldest.Timestamp.After(opts.since)) {
			return trimSince(out, opts.since), false, nil
		}
		if len(raw) < reqLimit {
			return trimSince(out, opts.since), false, nil
		}
		before = batch[0].ID
	}
	return trimSince(out, opts.since), true, nil
}

func trimSince(in []Message, since time.Time) []Message {
	if since.IsZero() || len(in) == 0 {
		return in
	}
	out := in[:0]
	for _, m := range in {
		if m.Timestamp.IsZero() || !m.Timestamp.Before(since) {
			out = append(out, m)
		}
	}
	return out
}

// ParseSnowflake parses a Discord snowflake id into a uint64. Empty or
// non-numeric input yields ok=false (the caller skips it — never a panic).
func ParseSnowflake(id string) (uint64, bool) {
	if id == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// project maps discordgo messages to the relay projection and sorts ASCENDING by
// snowflake id. Sorting (rather than reversing) makes the order independent of
// Discord's return ordering — robust to any API ordering change. Messages whose id
// does not parse as a snowflake are dropped: they cannot be positioned in the
// cursor space, and a real Discord id is always a valid snowflake.
func project(raw []*discordgo.Message) []Message {
	out := make([]Message, 0, len(raw))
	for _, m := range raw {
		if m == nil {
			continue
		}
		snow, ok := ParseSnowflake(m.ID)
		if !ok {
			// A real Discord message id is always a valid snowflake; a parse failure
			// means an API-contract change or corruption. Such a message cannot be
			// positioned in the cursor space, so it is dropped — but logged, so a
			// mysterious missing message in the catch-up reconciler has an audit trail
			// rather than vanishing silently.
			log.Printf("flotilla discord: dropping message with unparseable id %q (not a snowflake)", m.ID)
			continue
		}
		authorID := ""
		if m.Author != nil {
			authorID = m.Author.ID
		}
		out = append(out, Message{
			ID:        m.ID,
			SnowID:    snow,
			AuthorID:  authorID,
			WebhookID: m.WebhookID,
			Content:   m.Content,
			Timestamp: m.Timestamp,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SnowID < out[j].SnowID })
	return out
}

// maxRawSnowflake returns the largest parseable snowflake id (as a string) in a raw
// discordgo page, for advancing the `after` cursor. ok=false if none parse.
func maxRawSnowflake(raw []*discordgo.Message) (string, bool) {
	var max uint64
	var maxStr string
	for _, m := range raw {
		if m == nil {
			continue
		}
		if n, ok := ParseSnowflake(m.ID); ok && (maxStr == "" || n > max) {
			max, maxStr = n, m.ID
		}
	}
	return maxStr, maxStr != ""
}

// sortedUniqueAscending sorts messages ascending by snowflake and drops duplicate
// ids (a paginated walk can in principle re-see a boundary message). The dedup keeps
// the contiguous-ascending contract clean for the reconciler's classify.
func sortedUniqueAscending(in []Message) []Message {
	if len(in) <= 1 {
		return in
	}
	sort.Slice(in, func(i, j int) bool { return in[i].SnowID < in[j].SnowID })
	out := in[:0]
	var last uint64
	for i, m := range in {
		if i == 0 || m.SnowID != last {
			out = append(out, m)
			last = m.SnowID
		}
	}
	return out
}
