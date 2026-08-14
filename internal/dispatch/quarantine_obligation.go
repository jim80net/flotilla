package dispatch

import (
	"strings"

	"github.com/jim80net/flotilla/internal/backlog"
)

const quarantineBacklogNoncePrefix = "[dispatch-nonce:"

// QuarantineBacklogToken is the explicit cross-ledger provenance marker understood by the
// actionable-work filter. Plain prose that merely mentions a nonce is deliberately not a join.
func QuarantineBacklogToken(nonce string) string {
	return quarantineBacklogNoncePrefix + strings.TrimSpace(nonce) + "]"
}

// ExcludeQuarantinedInboundWork removes the portion of a recipient's actionable queue represented
// by active quarantined inbound rows. Quarantine is a routing hold, not work the recipient can act
// on. The source backlog and inbound rows are never mutated. A strict registry read error is returned
// so wake/status callers can fail closed rather than treating uncertain work as actionable.
func ExcludeQuarantinedInboundWork(rosterDir, recipient string, st backlog.Status) (backlog.Status, error) {
	entries, err := NewQuarantineRegistry(rosterDir).ActiveRecipientEntries("inbound-ack", recipient)
	if err != nil {
		return backlog.Status{}, err
	}
	if len(entries) == 0 || len(st.Unblocked) == 0 {
		return st, nil
	}
	used := make([]bool, len(entries))
	kept := make([]string, 0, len(st.Unblocked))
	for _, line := range st.Unblocked {
		matched := false
		for i, e := range entries {
			if !used[i] && e.Nonce != "" && strings.Contains(line, QuarantineBacklogToken(e.Nonce)) {
				used[i] = true
				matched = true
				break
			}
		}
		if !matched {
			kept = append(kept, line)
		}
	}
	st.Unblocked = kept
	return st, nil
}
