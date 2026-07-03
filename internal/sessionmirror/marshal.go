package sessionmirror

import (
	"encoding/json"
	"fmt"
)

// marshalLedgerLine JSON-encodes rec as one jsonl line bounded to maxLineBytes.
// Verbose is shrunk (re-truncated) until the marshaled line fits, so a written line
// never exceeds what readLines/ParseLines accept — JSON escaping and ANSI bytes
// from tmux turn-finals cannot wedge the ledger.
func marshalLedgerLine(rec Record) ([]byte, error) {
	runes := []rune(rec.Verbose)
	lo, hi := 0, len(runes)
	var best []byte
	for lo <= hi {
		mid := (lo + hi) / 2
		trial := rec
		trial.Verbose = truncateRunes(string(runes), mid)
		line, err := json.Marshal(trial)
		if err != nil {
			return nil, fmt.Errorf("sessionmirror: marshal record: %w", err)
		}
		if len(line) <= maxLineBytes {
			best = line
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if best == nil {
		return nil, fmt.Errorf("sessionmirror: record exceeds max line %d bytes even with empty verbose", maxLineBytes)
	}
	return append(best, '\n'), nil
}
