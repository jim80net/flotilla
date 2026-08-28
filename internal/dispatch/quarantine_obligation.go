package dispatch

import "github.com/jim80net/flotilla/internal/backlog"

// ExcludeQuarantinedInboundWork applies the recipient-level routing hold represented by any active
// quarantine. While held, none of the recipient's queue is actionable; the confirmed restore edge
// reopens every marker before normal queue evidence resumes. Source backlog and inbound rows are
// never mutated. Strict read errors let wake/status callers fail closed.
func ExcludeQuarantinedInboundWork(rosterDir, recipient string, st backlog.Status) (backlog.Status, error) {
	held, err := NewQuarantineRegistry(rosterDir).HasActiveRecipient(recipient)
	if err != nil {
		return backlog.Status{}, err
	}
	if !held || len(st.Unblocked) == 0 {
		return st, nil
	}
	st.Unblocked = nil
	return st, nil
}
