package inbound

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ConsumedRecord is one durable disposition for a dispatch nonce (#614 / CNS Stratum A).
type ConsumedRecord struct {
	Nonce      string    `json:"nonce"`
	PayloadSHA string    `json:"payload_sha256,omitempty"`
	Reason     string    `json:"reason"`
	ConsumedAt time.Time `json:"consumed_at"`
}

type consumedFile struct {
	Records map[string]ConsumedRecord `json:"records"` // nonce → record
}

// ConsumedPath returns <roster-dir>/flotilla-dispatch-consumed.json.
func ConsumedPath(rosterDir string) string {
	return filepath.Join(rosterDir, "flotilla-dispatch-consumed.json")
}

// PayloadHash returns hex sha256 of the dispatch message body.
func PayloadHash(message string) string {
	sum := sha256.Sum256([]byte(message))
	return hex.EncodeToString(sum[:])
}

// LoadConsumed reads the consumed registry fail-safe (empty map if missing/corrupt).
func LoadConsumed(rosterDir string) map[string]ConsumedRecord {
	path := ConsumedPath(rosterDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		return map[string]ConsumedRecord{}
	}
	var f consumedFile
	if err := json.Unmarshal(raw, &f); err != nil || f.Records == nil {
		return map[string]ConsumedRecord{}
	}
	return f.Records
}

// IsConsumed reports whether nonce is durably consumed (reinject must not re-task).
func IsConsumed(rosterDir, nonce string) bool {
	if nonce == "" || rosterDir == "" {
		return false
	}
	_, ok := LoadConsumed(rosterDir)[nonce]
	return ok
}

// MarkConsumed records a durable consumed disposition for nonce (idempotent).
func MarkConsumed(rosterDir, nonce, message, reason string) error {
	if rosterDir == "" || nonce == "" {
		return fmt.Errorf("inbound consumed: roster dir and nonce required")
	}
	path := ConsumedPath(rosterDir)
	return withConsumedLock(path, func(f *consumedFile) error {
		if f.Records == nil {
			f.Records = make(map[string]ConsumedRecord)
		}
		if _, exists := f.Records[nonce]; exists {
			return nil
		}
		f.Records[nonce] = ConsumedRecord{
			Nonce:      nonce,
			PayloadSHA: PayloadHash(message),
			Reason:     reason,
			ConsumedAt: time.Now().UTC(),
		}
		return nil
	})
}

func withConsumedLock(path string, fn func(*consumedFile) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("inbound consumed: mkdir: %w", err)
	}
	f, err := readConsumedFile(path)
	if err != nil {
		return err
	}
	if err := fn(&f); err != nil {
		return err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("inbound consumed: marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("inbound consumed: write temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("inbound consumed: rename: %w", err)
	}
	return nil
}

func readConsumedFile(path string) (consumedFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return consumedFile{Records: map[string]ConsumedRecord{}}, nil
		}
		return consumedFile{}, fmt.Errorf("inbound consumed: read: %w", err)
	}
	var f consumedFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return consumedFile{}, fmt.Errorf("inbound consumed: corrupt: %w", err)
	}
	if f.Records == nil {
		f.Records = map[string]ConsumedRecord{}
	}
	return f, nil
}
