package adjutantbuffer

import (
	"strings"
	"time"
)

// OperatorReasonPrefix tags durable buffer items that hold operator conversation (#593).
const OperatorReasonPrefix = "operator:"

// operatorReasonSep separates message id from verbatim body in a buffer reason.
const operatorReasonSep = "|"

// FormatOperatorReason encodes an operator message for the layer buffer.
func FormatOperatorReason(messageID, body string) string {
	return OperatorReasonPrefix + messageID + operatorReasonSep + body
}

// IsOperatorReason reports whether a buffer reason holds operator conversation traffic.
func IsOperatorReason(reason string) bool {
	return strings.HasPrefix(reason, OperatorReasonPrefix)
}

// ExtractOperatorBody returns the durable message id and verbatim operator body.
func ExtractOperatorBody(reason string) (messageID, body string, ok bool) {
	if !IsOperatorReason(reason) {
		return "", "", false
	}
	rest := strings.TrimPrefix(reason, OperatorReasonPrefix)
	i := strings.Index(rest, operatorReasonSep)
	if i <= 0 {
		return "", "", false
	}
	return rest[:i], rest[i+len(operatorReasonSep):], true
}

// HasOperatorMessage reports whether messageID is already buffered for the layer.
func HasOperatorMessage(path, messageID string) bool {
	if path == "" || messageID == "" {
		return false
	}
	f, _, err := load(path)
	if err != nil {
		return false
	}
	for _, it := range f.Items {
		if id, _, ok := ExtractOperatorBody(it.Reason); ok && id == messageID {
			return true
		}
	}
	return false
}

// AppendOperator persists one operator relay with arc metadata (B1). Duplicate messageID
// is a no-op (#592/#593 busy-defer hygiene). quiet <= 0 gives each message a unique arc.
func AppendOperator(path, leader, messageID, body, channelID, operatorID string, now time.Time, quiet time.Duration) error {
	if path == "" || leader == "" || messageID == "" {
		return nil
	}
	if HasOperatorMessage(path, messageID) {
		return nil
	}
	body = strings.TrimSpace(body)
	if body == "" {
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
	quiet = ClampArcQuiet(quiet)
	arcID, openedAt := AssignArc(f.Items, leader, channelID, operatorID, now, quiet)
	reason := FormatOperatorReason(messageID, body)
	it := Item{
		At:         now,
		Reason:     reason,
		ArcID:      arcID,
		OpenedAt:   openedAt,
		MessageIDs: []string{messageID},
		ChannelID:  NormalizeChannelID(channelID),
		OperatorID: NormalizeOperatorID(operatorID),
	}
	if norm, ok := normalizeItem(it); ok {
		it = norm
	}
	f.Items = append(f.Items, it)
	return save(path, f)
}

// PartitionItems splits operator conversation items from system/detector buffer items.
func PartitionItems(items []Item) (operator, other []Item) {
	for _, it := range items {
		if IsOperatorReason(it.Reason) {
			operator = append(operator, it)
		} else {
			other = append(other, it)
		}
	}
	return operator, other
}
