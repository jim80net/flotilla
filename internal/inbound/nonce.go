package inbound

import (
	"regexp"
	"strings"
)

var dispatchNonceRE = regexp.MustCompile(`flotilla-dispatch-[0-9a-f]{8,16}`)

// AppendDispatchNonce appends a machine-readable nonce footer for durable ack (#472).
func AppendDispatchNonce(message string) (string, string, error) {
	if existing := ParseDispatchNonce(message); existing != "" {
		return message, existing, nil
	}
	nonce, err := NewNonce()
	if err != nil {
		return "", "", err
	}
	augmented := strings.TrimRight(message, "\n") + FormatDispatchFooter(nonce)
	return augmented, nonce, nil
}

// ParseDispatchNonce extracts the dispatch nonce from a message body, if present.
func ParseDispatchNonce(message string) string {
	return dispatchNonceRE.FindString(message)
}

// ParseOwnDispatchNonce returns the nonce this message is itself STAMPED with —
// the one inside its trailing #472 footer — as distinct from a nonce merely
// QUOTED in the body (an upward report naming a dispatch, a status line). A
// message with no footer returns "" even when its prose contains nonces:
// AppendDispatchNonce reuses a quoted nonce for outbox dedup, but that reuse
// does not make the message a dispatch under the ack contract, and settlement
// paths (#707) must not treat it as one.
func ParseOwnDispatchNonce(message string) string {
	i := strings.LastIndex(message, dispatchFooterMarker)
	if i < 0 {
		return ""
	}
	return dispatchNonceRE.FindString(message[i:])
}

const dispatchFooterMarker = "\n\n---\nflotilla dispatch ack (#472)\n"

// StripDispatchFooter returns the operator-facing body without the #472 dispatch footer.
// Outbox dedup hashes this stripped form so per-send nonce stamps do not defeat collapse (#484).
func StripDispatchFooter(message string) string {
	if i := strings.Index(message, dispatchFooterMarker); i >= 0 {
		return strings.TrimRight(message[:i], "\n")
	}
	return message
}
