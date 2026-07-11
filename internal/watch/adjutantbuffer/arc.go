package adjutantbuffer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// DefaultArcQuiet is the default quiet window for mechanical coalesce (B1).
const DefaultArcQuiet = 60 * time.Second

// ArcQuietFloor / ArcQuietCeil clamp configured quiet durations.
const (
	ArcQuietFloor = 45 * time.Second
	ArcQuietCeil  = 90 * time.Second
)

// BodyDelimiter separates verbatim operator bodies in a coalesced seam payload.
const BodyDelimiter = "\n\n---\n\n"

// ParseArcQuiet parses a duration string for FLOTILLA_ADJUTANT_ARC_QUIET.
// Empty → DefaultArcQuiet. "0" / "0s" → 0 (disable coalesce). Other values are
// clamped to [ArcQuietFloor, ArcQuietCeil].
func ParseArcQuiet(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultArcQuiet
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return DefaultArcQuiet
	}
	if d == 0 {
		return 0
	}
	if d < ArcQuietFloor {
		return ArcQuietFloor
	}
	if d > ArcQuietCeil {
		return ArcQuietCeil
	}
	return d
}

// ArcKey builds the mechanical coalesce key (leader + channel + operator).
func ArcKey(leader, channelID, operatorID string) string {
	return leader + "\x00" + channelID + "\x00" + operatorID
}

// NewArcID returns a durable opaque arc id from key + time.
func NewArcID(arcKey string, at time.Time) string {
	sum := sha256.Sum256([]byte(arcKey + "\x00" + at.UTC().Format(time.RFC3339Nano)))
	return "arc_" + hex.EncodeToString(sum[:10])
}

// AssignArc sets arc metadata on a new operator item given the current buffer
// contents and quiet window. quiet==0 → always a new singleton arc.
func AssignArc(f File, leader, channelID, operatorID, messageID string, at time.Time, quiet time.Duration) (arcID string, openedAt time.Time) {
	at = at.UTC()
	if quiet <= 0 {
		id := NewArcID(ArcKey(leader, channelID, operatorID)+messageID, at)
		return id, at
	}
	key := ArcKey(leader, channelID, operatorID)
	// Find open arc: same key, last message within quiet.
	type cand struct {
		arcID    string
		openedAt time.Time
		lastAt   time.Time
	}
	var best *cand
	for _, it := range f.Items {
		if !IsOperatorReason(it.Reason) || it.ArcID == "" {
			continue
		}
		if ArcKey(leader, it.ChannelID, it.OperatorID) != key {
			continue
		}
		last := it.At.UTC()
		if best == nil || last.After(best.lastAt) {
			c := cand{arcID: it.ArcID, openedAt: it.OpenedAt.UTC(), lastAt: last}
			if c.openedAt.IsZero() {
				c.openedAt = last
			}
			best = &c
		}
	}
	if best != nil && at.Sub(best.lastAt) <= quiet {
		return best.arcID, best.openedAt
	}
	return NewArcID(key, at), at
}

// ArcGroup is one coalesce unit for seam forward.
type ArcGroup struct {
	ArcID string
	Items []Item // sorted by At ascending
}

// GroupByArc groups operator items by arc_id (legacy empty → per-message singleton).
// Groups are ordered by the earliest At in each group.
func GroupByArc(items []Item) []ArcGroup {
	type acc struct {
		items []Item
		first time.Time
	}
	m := map[string]*acc{}
	order := []string{}
	for _, it := range items {
		id := it.ArcID
		if id == "" {
			// singleton synthetic id from message id or state hash
			if mid, _, ok := ExtractOperatorBody(it.Reason); ok {
				id = "singleton_" + mid
			} else {
				id = "singleton_" + it.StateHash
			}
		}
		a := m[id]
		if a == nil {
			a = &acc{first: it.At}
			m[id] = a
			order = append(order, id)
		}
		a.items = append(a.items, it)
		if it.At.Before(a.first) {
			a.first = it.At
		}
	}
	sort.Slice(order, func(i, j int) bool {
		return m[order[i]].first.Before(m[order[j]].first)
	})
	out := make([]ArcGroup, 0, len(order))
	for _, id := range order {
		g := m[id].items
		sort.Slice(g, func(i, j int) bool { return g[i].At.Before(g[j].At) })
		out = append(out, ArcGroup{ArcID: id, Items: g})
	}
	return out
}

// ArcQuietClosed reports whether the arc has no messages newer than quiet before now.
// quiet<=0 means always closed (each message already its own arc).
func ArcQuietClosed(items []Item, now time.Time, quiet time.Duration) bool {
	if quiet <= 0 || len(items) == 0 {
		return true
	}
	last := items[0].At
	for _, it := range items[1:] {
		if it.At.After(last) {
			last = it.At
		}
	}
	return !now.Before(last.Add(quiet))
}

// FormatArcBodies joins ordered verbatim operator bodies with BodyDelimiter.
func FormatArcBodies(items []Item) string {
	var parts []string
	for _, it := range items {
		_, body, ok := ExtractOperatorBody(it.Reason)
		if !ok || body == "" {
			continue
		}
		parts = append(parts, body)
	}
	return strings.Join(parts, BodyDelimiter)
}

// FilterArcReady returns only arc groups eligible for seam forward (quiet closed).
// When quiet is 0, all groups are ready.
func FilterArcReady(groups []ArcGroup, now time.Time, quiet time.Duration) []ArcGroup {
	if quiet <= 0 {
		return groups
	}
	var out []ArcGroup
	for _, g := range groups {
		if ArcQuietClosed(g.Items, now, quiet) {
			out = append(out, g)
		}
	}
	return out
}

// EnsureMessageID on arc metadata after assign.
func ensureMessageIDs(it *Item, messageID string) {
	if messageID == "" {
		return
	}
	for _, id := range it.MessageIDs {
		if id == messageID {
			return
		}
	}
	it.MessageIDs = append(it.MessageIDs, messageID)
}

// AppendOperator appends one operator message with arc assignment.
func AppendOperator(path, leader, messageID, body, channelID, operatorID string, now time.Time, quiet time.Duration) error {
	if path == "" || leader == "" || messageID == "" {
		return nil
	}
	if HasOperatorMessage(path, messageID) {
		return nil
	}
	f, _, err := load(path)
	if err != nil {
		return err
	}
	if f.Leader == "" {
		f.Leader = leader
	}
	now = now.UTC()
	reason := FormatOperatorReason(messageID, body)
	arcID, openedAt := AssignArc(f, leader, channelID, operatorID, messageID, now, quiet)
	it := Item{
		At:         now,
		Reason:     reason,
		Key:        itemKey(reason),
		StateHash:  itemStateHash(reason, now),
		ArcID:      arcID,
		OpenedAt:   openedAt,
		MessageIDs: []string{messageID},
		ChannelID:  channelID,
		OperatorID: operatorID,
	}
	f.Items = append(f.Items, it)
	return save(path, f)
}

// Debug helper for tests.
func formatArcKey(leader, ch, op string) string {
	return fmt.Sprintf("%s|%s|%s", leader, ch, op)
}
