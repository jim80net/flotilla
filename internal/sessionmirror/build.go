package sessionmirror

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
)

// HistoryDoc is the dash read-model for session-mirror history.
type HistoryDoc struct {
	Agent   string   `json:"agent"`
	Entries []Record `json:"entries"`
	Limit   int      `json:"limit"`
}

// BuildHistory parses ledger bytes and returns the last limit entries (newest last).
// Malformed lines are skipped. limit <= 0 means no limit.
func BuildHistory(agent string, data []byte, limit int) HistoryDoc {
	entries := ParseLines(data)
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return HistoryDoc{
		Agent:   agent,
		Entries: entries,
		Limit:   limit,
	}
}

// ParseLines decodes all valid JSON lines from a ledger file.
func ParseLines(data []byte) []Record {
	if len(data) == 0 {
		return nil
	}
	var out []Record
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// MustLine is a test helper that JSON-encodes a record as one line.
func MustLine(rec Record) []byte {
	b, err := json.Marshal(rec)
	if err != nil {
		panic(fmt.Sprintf("sessionmirror: marshal: %v", err))
	}
	return append(b, '\n')
}
