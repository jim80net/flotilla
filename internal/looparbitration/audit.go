package looparbitration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AuditEntry records one arbitration decision. Evaluate appends on leader bypass
// (urgent no-adjutant or explicit BypassClass); Audited is set only when Record succeeds.
type AuditEntry struct {
	At          time.Time  `json:"at"`
	Coordinator string     `json:"coordinator"`
	Target      string     `json:"target"`
	Kind        InjectKind `json:"kind"`
	Priority    Priority   `json:"priority,omitempty"`
	Source      string     `json:"source,omitempty"`
	Decision    Decision   `json:"decision"`
	Bypass      string     `json:"bypass,omitempty"`
	Reason      string     `json:"reason,omitempty"`
}

// AuditLog appends durable JSONL audit records (one line per entry).
type AuditLog struct {
	path string
}

// NewAuditLog opens or creates the audit trail at path.
func NewAuditLog(path string) *AuditLog {
	return &AuditLog{path: path}
}

// Path returns the backing file path.
func (l *AuditLog) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Record appends one audit entry atomically (line-delimited JSON).
func (l *AuditLog) Record(e AuditEntry) error {
	if l == nil || l.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return fmt.Errorf("mkdir audit dir: %w", err)
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open audit %q: %w", l.path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("append audit: %w", err)
	}
	return f.Sync()
}

// Load reads all audit entries from path (missing file → empty slice).
func LoadAudit(path string) ([]AuditEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []AuditEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e AuditEntry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("parse audit line: %w", err)
		}
		out = append(out, e)
	}
	return out, sc.Err()
}
