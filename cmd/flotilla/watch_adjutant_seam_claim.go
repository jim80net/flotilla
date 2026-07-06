package main

import (
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jim80net/flotilla/internal/watch/adjutantbuffer"
)

const adjutantSeamClaimPrefix = "adjutant-seam:"

type adjutantSeamClaim struct {
	owner         string
	bufferPath    string
	deliveredPath string
	recordItems   []adjutantbuffer.Item
}

// adjutantSeamClaims holds in-flight seam briefs until injector confirm/abort (#488 P2).
type adjutantSeamClaims struct {
	mu      sync.Mutex
	pending map[string]adjutantSeamClaim
}

func newAdjutantSeamClaims() *adjutantSeamClaims {
	return &adjutantSeamClaims{pending: make(map[string]adjutantSeamClaim)}
}

func adjutantSeamClaimKey(owner string) string {
	// Unique per drain so overlapping seams do not replace in-flight claims (#488 P1).
	return adjutantSeamClaimPrefix + owner + ":" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
}

func (c *adjutantSeamClaims) register(key string, claim adjutantSeamClaim) {
	c.mu.Lock()
	c.pending[key] = claim
	c.mu.Unlock()
}

func (c *adjutantSeamClaims) confirm(key string) {
	c.mu.Lock()
	claim, ok := c.pending[key]
	delete(c.pending, key)
	c.mu.Unlock()
	if !ok {
		return
	}
	if err := adjutantbuffer.RecordDelivered(claim.deliveredPath, claim.owner, claim.recordItems); err != nil {
		log.Printf("flotilla watch: adjutant delivered ledger record failed for %q: %v", claim.owner, err)
	}
	if err := adjutantbuffer.RemoveConfirmedItems(claim.bufferPath, claim.owner, claim.recordItems); err != nil {
		log.Printf("flotilla watch: adjutant buffer remove-after-confirmed-seam failed for %q: %v", claim.owner, err)
	}
}

func (c *adjutantSeamClaims) abort(key string) {
	c.mu.Lock()
	delete(c.pending, key)
	c.mu.Unlock()
	// Buffer retained — next seam drain retries (#488 P2).
}

func isAdjutantSeamClaimKey(key string) bool {
	return strings.HasPrefix(key, adjutantSeamClaimPrefix)
}
