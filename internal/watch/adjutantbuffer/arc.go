package adjutantbuffer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// DefaultArcQuiet is the mechanical coalesce window when unset (B1).
const DefaultArcQuiet = 60 * time.Second

const (
	minArcQuiet = 45 * time.Second
	maxArcQuiet = 90 * time.Second

	channelUnknown = "unknown"
	operatorUnknown = "unknown"
)

// ClampArcQuiet bounds configured quiet duration to the B1 policy window.
// quiet <= 0 disables coalesce (every message is its own arc).
func ClampArcQuiet(quiet time.Duration) time.Duration {
	if quiet <= 0 {
		return 0
	}
	if quiet < minArcQuiet {
		return minArcQuiet
	}
	if quiet > maxArcQuiet {
		return maxArcQuiet
	}
	return quiet
}

// NormalizeChannelID maps relay channel identity to a stable arc-key component.
func NormalizeChannelID(channelID string) string {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return channelUnknown
	}
	return channelID
}

// NormalizeOperatorID maps relay operator identity to a stable arc-key component.
func NormalizeOperatorID(operatorID string) string {
	operatorID = strings.TrimSpace(operatorID)
	if operatorID == "" {
		return operatorUnknown
	}
	return operatorID
}

func arcKey(leader, channelID, operatorID string) string {
	return leader + "\x00" + NormalizeChannelID(channelID) + "\x00" + NormalizeOperatorID(operatorID)
}

func newArcID(key string, now time.Time) string {
	sum := sha256.Sum256([]byte(key + "\x00" + now.UTC().Format(time.RFC3339Nano)))
	return fmt.Sprintf("arc_%d_%s", now.UnixNano(), hex.EncodeToString(sum[:4]))
}

// EffectiveArcID returns the durable arc id for an item. Legacy operator items without
// arc metadata are treated as singleton arcs keyed by message id.
func EffectiveArcID(it Item) string {
	if it.ArcID != "" {
		return it.ArcID
	}
	if id, _, ok := ExtractOperatorBody(it.Reason); ok && id != "" {
		return "legacy:" + id
	}
	return ""
}

// AssignArc picks or opens an arc for a new operator message. When quiet is zero each
// call returns a fresh arc. Otherwise an open arc with the same leader/channel/operator
// key whose last message is within quiet of now is reused.
func AssignArc(items []Item, leader, channelID, operatorID string, now time.Time, quiet time.Duration) (arcID string, openedAt time.Time) {
	now = now.UTC()
	key := arcKey(leader, channelID, operatorID)
	if quiet <= 0 {
		return newArcID(key, now), now
	}

	type arcState struct {
		id        string
		openedAt  time.Time
		lastAt    time.Time
		channelID string
		operator  string
	}

	states := make(map[string]*arcState)
	for _, it := range items {
		if !IsOperatorReason(it.Reason) {
			continue
		}
		id := EffectiveArcID(it)
		if id == "" {
			continue
		}
		st := states[id]
		if st == nil {
			st = &arcState{id: id}
			states[id] = st
		}
		if !it.OpenedAt.IsZero() && (st.openedAt.IsZero() || it.OpenedAt.Before(st.openedAt)) {
			st.openedAt = it.OpenedAt.UTC()
		}
		if it.At.After(st.lastAt) {
			st.lastAt = it.At.UTC()
		}
		if st.channelID == "" && it.ChannelID != "" {
			st.channelID = it.ChannelID
		}
		if st.operator == "" && it.OperatorID != "" {
			st.operator = it.OperatorID
		}
	}

	normChannel := NormalizeChannelID(channelID)
	normOperator := NormalizeOperatorID(operatorID)
	var best *arcState
	for _, st := range states {
		ch := st.channelID
		if ch == "" {
			ch = channelUnknown
		}
		op := st.operator
		if op == "" {
			op = operatorUnknown
		}
		if ch != normChannel || op != normOperator {
			continue
		}
		if now.Sub(st.lastAt) > quiet {
			continue
		}
		if best == nil || st.lastAt.After(best.lastAt) {
			best = st
		}
	}
	if best != nil {
		opened := best.openedAt
		if opened.IsZero() {
			opened = best.lastAt
		}
		return best.id, opened
	}
	return newArcID(key, now), now
}