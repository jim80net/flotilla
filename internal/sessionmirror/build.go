package sessionmirror

import (
	"bytes"
	"crypto/sha256"
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
	ensureRecordIDs(entries)
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return HistoryDoc{
		Agent:   agent,
		Entries: entries,
		Limit:   limit,
	}
}

// ensureRecordIDs gives pre-ID ledger records a stable read identity. New entries
// persist a random ID at append time. For legacy entries, a full SHA-256 digest of
// the canonical JSON plus a newest-relative duplicate ordinal is collision-safe
// within the ledger and remains stable when retention drops the oldest lines.
func ensureRecordIDs(entries []Record) {
	seen := make(map[[32]byte]int)
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].ID != "" {
			continue
		}
		encoded, err := json.Marshal(entries[i])
		if err != nil {
			continue
		}
		digest := sha256.Sum256(encoded)
		ordinal := seen[digest]
		seen[digest] = ordinal + 1
		entries[i].ID = fmt.Sprintf("legacy-sm-%x-%d", digest, ordinal)
	}
}

// ParseLines decodes all valid JSON lines from a ledger file.
func ParseLines(data []byte) []Record {
	if len(data) == 0 {
		return nil
	}
	var out []Record
	sc := newLineScanner(data)
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
