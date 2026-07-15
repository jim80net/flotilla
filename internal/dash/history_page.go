package dash

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jim80net/flotilla/internal/backlog"
)

const (
	defaultHistoryLimit = 50
	maxHistoryLimit     = 200
)

// HistoryQuery is the bounded coordination-ledger read contract. Cursor is an
// opaque byte position into the append-only ledger; an empty cursor starts at
// the current end. Desk and Channel are optional compatibility filters, though
// the dash startup path always supplies Desk.
type HistoryQuery struct {
	Desk    string
	Channel string
	Limit   int
	Cursor  string
	Meta    bool
}

// HistoryPage is a bounded coordination-history page. Ledger remains newest
// first; NextCursor retrieves the immediately-older matching window.
type HistoryPage struct {
	Ledger           []LedgerEntry `json:"ledger"`
	Backlog          BacklogInfo   `json:"backlog"`
	Desk             string        `json:"desk,omitempty"`
	Channel          string        `json:"channel,omitempty"`
	Limit            int           `json:"limit"`
	HasMore          bool          `json:"has_more"`
	NextCursor       string        `json:"next_cursor,omitempty"`
	LedgerSignature  string        `json:"ledger_signature"`
	BacklogSignature string        `json:"backlog_signature"`
}

func parseHistoryQuery(values map[string][]string) (HistoryQuery, error) {
	q := HistoryQuery{
		Desk:    strings.TrimSpace(firstQuery(values, "desk")),
		Channel: strings.TrimSpace(firstQuery(values, "channel")),
		Limit:   defaultHistoryLimit,
		Cursor:  strings.TrimSpace(firstQuery(values, "cursor")),
		Meta:    firstQuery(values, "meta") == "1",
	}
	if raw := strings.TrimSpace(firstQuery(values, "limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > maxHistoryLimit {
			return HistoryQuery{}, fmt.Errorf("limit must be between 1 and %d", maxHistoryLimit)
		}
		q.Limit = limit
	}
	if q.Cursor != "" {
		cursor, err := strconv.ParseInt(q.Cursor, 10, 64)
		if err != nil || cursor < 0 {
			return HistoryQuery{}, fmt.Errorf("cursor is invalid")
		}
	}
	return q, nil
}

func firstQuery(values map[string][]string, key string) string {
	if items := values[key]; len(items) > 0 {
		return items[0]
	}
	return ""
}

// BuildHistoryPage scans backward from cursor and parses only until the bounded
// matching window plus one look-ahead match is found. It never constructs the
// former all-ledger slice. Cursor byte positions remain stable when the
// append-only ledger grows between requests.
func BuildHistoryPage(ledgerRaw, backlogRaw string, q HistoryQuery) (HistoryPage, error) {
	if q.Limit == 0 {
		q.Limit = defaultHistoryLimit
	}
	end := len(ledgerRaw)
	if q.Cursor != "" {
		cursor, err := strconv.ParseInt(q.Cursor, 10, 64)
		if err != nil || cursor < 0 || cursor > int64(len(ledgerRaw)) {
			return HistoryPage{}, fmt.Errorf("cursor is outside the current ledger")
		}
		end = int(cursor)
	}
	page := HistoryPage{
		Ledger:  []LedgerEntry{},
		Backlog: buildBacklogInfo(backlogRaw),
		Desk:    q.Desk,
		Channel: q.Channel,
		Limit:   q.Limit,
	}
	if q.Meta {
		return page, nil
	}

	for end > 0 {
		// A cursor may point immediately after a newline. Exclude delimiters from
		// the line span without skipping any content byte.
		for end > 0 && (ledgerRaw[end-1] == '\n' || ledgerRaw[end-1] == '\r') {
			end--
		}
		if end == 0 {
			break
		}
		lineEnd := end
		start := strings.LastIndexByte(ledgerRaw[:end], '\n') + 1
		line := strings.TrimRight(ledgerRaw[start:lineEnd], "\r")
		end = start
		if strings.TrimSpace(line) == "" {
			continue
		}
		entry := ParseLedgerLine(line)
		if !historyEntryMatches(entry, q.Desk, q.Channel) {
			continue
		}
		if len(page.Ledger) == q.Limit {
			page.HasMore = true
			page.NextCursor = strconv.Itoa(lineEnd)
			break
		}
		page.Ledger = append(page.Ledger, entry)
	}
	return page, nil
}

func historyEntryMatches(entry LedgerEntry, desk, channel string) bool {
	if channel != "" && (!entry.Parsed || !strings.EqualFold(entry.Channel, channel)) {
		return false
	}
	if desk == "" {
		return true
	}
	if !entry.Parsed {
		return false
	}
	want := normalizeLedgerParticipant(desk)
	return normalizeLedgerParticipant(entry.From) == want || normalizeLedgerParticipant(entry.To) == want
}

func normalizeLedgerParticipant(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "@")
}

func buildBacklogInfo(raw string) BacklogInfo {
	st := backlog.Parse(raw)
	info := BacklogInfo{
		Found:        st.Found,
		Unblocked:    BuildQueueItems(st.Unblocked),
		Blocked:      st.Blocked,
		AwaitingAuth: st.AwaitingAuth,
		Done:         st.Done,
		Malformed:    st.Malformed,
		Items:        st.Items,
	}
	if info.Unblocked == nil {
		info.Unblocked = []QueueItem{}
	}
	return info
}

func fileSignature(path string) string {
	if path == "" {
		return "absent"
	}
	info, err := os.Stat(path)
	if err != nil {
		return "absent"
	}
	return strconv.FormatInt(info.Size(), 36) + "-" + strconv.FormatInt(info.ModTime().UnixNano(), 36)
}
