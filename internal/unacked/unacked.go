// Package unacked implements the pure scan logic for the standing detector that
// surfaces operator messages in bound channels with no fleet acknowledgment
// (issue #234 — the after-the-fact backstop to confirmed-delivery).
package unacked

import (
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/relay"
	"github.com/jim80net/flotilla/internal/transport"
)

// Defaults align scan cadence with the minimum age threshold
// (monitoring-cadence-equals-alert-threshold: never alert on a message the fleet
// may still be mid-answering within one scan cycle).
const (
	DefaultScanInterval    = 30 * time.Minute
	DefaultMinAge          = 30 * time.Minute // MUST be >= DefaultScanInterval
	DefaultAckWindow       = 2 * time.Hour
	DefaultWorkingFollowUp = 30 * time.Minute
	DefaultLookback        = 50
)

// Message is the scan input (transport.Message field-for-field at the seam).
type Message struct {
	ID        string
	SnowID    uint64
	AuthorID  string
	WebhookID string
	Content   string
	Timestamp time.Time
}

// FromTransport projects a bus message into the scan type.
func FromTransport(m transport.Message) Message {
	return Message{
		ID:        m.ID,
		SnowID:    m.SnowID,
		AuthorID:  m.AuthorID,
		WebhookID: m.WebhookID,
		Content:   m.Content,
		Timestamp: m.Timestamp,
	}
}

// Config tunes the mechanical classifier. MinAge MUST be >= the poller's scan
// interval so a message seen on one sweep is never flagged while still inside
// the fleet's in-flight answer window for that cadence.
type Config struct {
	MinAge          time.Duration
	AckWindow       time.Duration
	WorkingFollowUp time.Duration
	OperatorUserID  string
}

// DefaultConfig returns production defaults (MinAge == DefaultScanInterval).
func DefaultConfig(operatorUserID string) Config {
	return Config{
		MinAge:          DefaultMinAge,
		AckWindow:       DefaultAckWindow,
		WorkingFollowUp: DefaultWorkingFollowUp,
		OperatorUserID:  operatorUserID,
	}
}

// Finding is one operator message that passed the age gate and lacks a fleet ack.
type Finding struct {
	ChannelID string
	MessageID string
	Snippet   string
	Age       time.Duration
	Reason    string // "no-reply" | "working-only"
}

// Scan inspects ascending channel history and returns un-acked operator requests.
// Messages younger than MinAge are excluded. A fleet webhook reply after the
// operator message counts as ack unless it is only a "working on it" with no
// substantive follow-up within WorkingFollowUp.
func Scan(msgs []Message, channelID string, now time.Time, cfg Config) []Finding {
	if cfg.OperatorUserID == "" || len(msgs) == 0 {
		return nil
	}
	var out []Finding
	for i, m := range msgs {
		if !isOperatorMessage(m, cfg.OperatorUserID) {
			continue
		}
		if !looksLikeRequest(m.Content) {
			continue
		}
		age := ageOf(m, now)
		if age < cfg.MinAge {
			continue
		}
		if age > cfg.AckWindow {
			// Still report — the operator may have been waiting beyond the ack window.
		}
		reason, unacked := lacksFleetAck(msgs, i, now, cfg)
		if unacked {
			out = append(out, Finding{
				ChannelID: channelID,
				MessageID: m.ID,
				Snippet:   snippet(m.Content),
				Age:       age,
				Reason:    reason,
			})
		}
	}
	return out
}

func isOperatorMessage(m Message, operatorID string) bool {
	if m.WebhookID != "" {
		return false
	}
	if !relay.Accept(m.AuthorID, operatorID) {
		return false
	}
	return strings.TrimSpace(m.Content) != ""
}

func ageOf(m Message, now time.Time) time.Duration {
	if m.Timestamp.IsZero() {
		return 0
	}
	return now.Sub(m.Timestamp)
}

func lacksFleetAck(msgs []Message, opIdx int, now time.Time, cfg Config) (reason string, unacked bool) {
	var lastWorkingAt time.Time
	for j := opIdx + 1; j < len(msgs); j++ {
		f := msgs[j]
		if !isFleetReply(f) {
			continue
		}
		if isWorkingOnIt(f.Content) {
			lastWorkingAt = f.Timestamp
			continue
		}
		// Substantive fleet reply after the operator message — acked.
		return "", false
	}
	if !lastWorkingAt.IsZero() {
		sinceWorking := now.Sub(lastWorkingAt)
		if sinceWorking >= cfg.WorkingFollowUp {
			return "working-only", true
		}
		return "", false // still inside working follow-up window
	}
	// No fleet reply at all; operator message is old enough (caller checked MinAge).
	return "no-reply", true
}

func isFleetReply(m Message) bool {
	return m.WebhookID != "" && strings.TrimSpace(m.Content) != ""
}

func snippet(content string) string {
	const max = 120
	flat := strings.Join(strings.Fields(content), " ")
	r := []rune(flat)
	if len(r) <= max {
		return flat
	}
	return string(r[:max]) + "…"
}
