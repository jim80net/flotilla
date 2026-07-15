package dash

import (
	"fmt"
	"os"
	"strings"

	"github.com/jim80net/flotilla/internal/cos"
)

const (
	defaultHistoryPageLimit = 80
	maxHistoryPageLimit     = 200
)

type indexedLedgerEntry struct {
	entry LedgerEntry
	pos   int // append-stable chronological ordinal: oldest=0
}

// loadHistoryPage returns only one desk's bounded relay history. The cursor is
// the chronological ordinal of the oldest returned row; because the ledger is
// append-only, later appends do not move that boundary and show-more cannot
// duplicate or skip older rows.
func (s *Server) loadHistoryPage(desk string, limit int, before *int) HistoryDoc {
	full := BuildHistory(readFileOrEmpty(s.cfg.LedgerPath), readFileOrEmpty(s.cfg.BacklogPath))
	matched := make([]indexedLedgerEntry, 0, limit+1)
	for i, entry := range full.Ledger {
		if !historyEntryMatchesDesk(entry, desk) {
			continue
		}
		matched = append(matched, indexedLedgerEntry{entry: entry, pos: len(full.Ledger) - 1 - i})
	}

	doc := HistoryDoc{
		Ledger:    []LedgerEntry{},
		Backlog:   full.Backlog,
		Limit:     limit,
		Total:     len(matched),
		Signature: historyFileSignature(s.cfg.LedgerPath),
	}
	page := make([]indexedLedgerEntry, 0, limit+1)
	for _, item := range matched {
		if before != nil && item.pos >= *before {
			continue
		}
		page = append(page, item)
		if len(page) > limit {
			break
		}
	}
	if len(page) > limit {
		doc.HasMore = true
		page = page[:limit]
	}
	for _, item := range page {
		doc.Ledger = append(doc.Ledger, item.entry)
	}
	if doc.HasMore && len(page) > 0 {
		doc.NextCursor = fmt.Sprintf("%d", page[len(page)-1].pos)
	}
	if s.cfg.LedgerPath != "" {
		HydrateLedgerBodies(doc.Ledger, func(nonce string) (string, bool) {
			return cos.LookupBody(s.cfg.LedgerPath, nonce)
		})
	}
	return doc
}

func historyEntryMatchesDesk(entry LedgerEntry, desk string) bool {
	want := historyParticipant(desk)
	if want == "" {
		return false
	}
	if entry.Parsed {
		return historyParticipant(entry.From) == want || historyParticipant(entry.To) == want
	}
	return historyRawHasParticipant(entry.Raw, want)
}

func historyParticipant(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "@")
}

func historyRawHasParticipant(raw, desk string) bool {
	s := strings.ToLower(raw)
	for offset := 0; offset < len(s); {
		i := strings.Index(s[offset:], desk)
		if i < 0 {
			return false
		}
		i += offset
		beforeOK := i == 0 || !historyTokenByte(s[i-1])
		after := i + len(desk)
		afterOK := after == len(s) || !historyTokenByte(s[after])
		if beforeOK && afterOK {
			return true
		}
		offset = i + len(desk)
	}
	return false
}

func historyTokenByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_' || b == '-'
}

func historyFileSignature(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "absent"
	}
	return fmt.Sprintf("%x-%x", info.Size(), info.ModTime().UnixNano())
}
