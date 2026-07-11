package inbound

import (
	"log"
	"path/filepath"
	"strings"
)

// ClearAcknowledged removes pending entries that the turn-final acknowledges
// (#472 matcher) and returns the cleared copies. Used by the undelivered sweep
// to heal ledgers when a finish-edge miss left acked work pending (#628 false-positive).
func (s Store) ClearAcknowledged(turnFinal string) []Entry {
	if s.path == "" || strings.TrimSpace(turnFinal) == "" {
		return nil
	}
	var cleared []Entry
	if err := s.withLock(func() error {
		f, err := s.readFileForUpdate()
		if err != nil {
			return err
		}
		if len(f.Pending) == 0 {
			return nil
		}
		next := f.Pending[:0]
		for _, e := range f.Pending {
			if Acknowledged(turnFinal, e) {
				cleared = append(cleared, e)
				continue
			}
			next = append(next, e)
		}
		if len(cleared) == 0 {
			return nil
		}
		f.Pending = next
		return s.save(f)
	}); err != nil {
		log.Printf("flotilla inbound: clear-acknowledged failed: %v", err)
		return nil
	}
	return cleared
}

// ClearConsumed removes pending entries whose nonce is already in the durable
// consumed registry (isConsumed). Returns cleared entries.
func (s Store) ClearConsumed(isConsumed func(nonce, message string) bool) []Entry {
	if s.path == "" || isConsumed == nil {
		return nil
	}
	var cleared []Entry
	if err := s.withLock(func() error {
		f, err := s.readFileForUpdate()
		if err != nil {
			return err
		}
		if len(f.Pending) == 0 {
			return nil
		}
		next := f.Pending[:0]
		for _, e := range f.Pending {
			if e.Nonce != "" && isConsumed(e.Nonce, e.Message) {
				cleared = append(cleared, e)
				continue
			}
			next = append(next, e)
		}
		if len(cleared) == 0 {
			return nil
		}
		f.Pending = next
		return s.save(f)
	}); err != nil {
		log.Printf("flotilla inbound: clear-consumed failed: %v", err)
		return nil
	}
	return cleared
}

// RecipientFromInboundPath extracts the agent slug from a ledger path basename.
func RecipientFromInboundPath(path string) string {
	return RecipientFromPath(path)
}

// ListInboundPaths returns flotilla-*-inbound.json paths under rosterDir.
func ListInboundPaths(rosterDir string) []string {
	if rosterDir == "" {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(rosterDir, "flotilla-*-inbound.json"))
	if err != nil {
		log.Printf("flotilla inbound: glob failed: %v", err)
		return nil
	}
	return matches
}
