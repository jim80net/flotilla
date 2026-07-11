package dispatch

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/jim80net/flotilla/internal/inbound"
)

// PayloadHash fingerprints a dispatch body for the consumed registry (#614).
// Uses the nonce-stripped form so the same work identity matches across
// reinject stamps that re-append the #472 footer.
func PayloadHash(message string) string {
	sum := sha256.Sum256([]byte(inbound.StripDispatchFooter(message)))
	return hex.EncodeToString(sum[:16])
}
