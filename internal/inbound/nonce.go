package inbound

import (
	"regexp"
	"strings"
)

var dispatchNonceRE = regexp.MustCompile(`flotilla-dispatch-[0-9a-f]{8,16}`)

// AppendDispatchNonce appends a machine-readable nonce footer for turn-final ack (#472).
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
