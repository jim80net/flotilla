package authshadow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type HealthState string

const (
	HealthHealthy         HealthState = "healthy"
	HealthMissing         HealthState = "missing"
	HealthEmpty           HealthState = "empty"
	HealthDiskUnavailable HealthState = "disk_unavailable"
	HealthCorrupt         HealthState = "corrupt"
	HealthTruncated       HealthState = "truncated"
	HealthSequenceGap     HealthState = "sequence_gap"
	HealthChainMismatch   HealthState = "chain_mismatch"
	HealthDomainMismatch  HealthState = "domain_mismatch"
)

type Health struct {
	State        HealthState `json:"state"`
	ReasonCode   string      `json:"reason_code"`
	LastSequence uint64      `json:"last_sequence"`
	LastHash     string      `json:"last_hash"`
	Records      int         `json:"records"`
	Enforcing    bool        `json:"enforcing"`
}

func (h Health) Healthy() bool { return h.State == HealthHealthy }

// Writer owns one per-domain WAL. Domain IDs are hashed into filenames so a
// caller cannot turn a logical identity into a path selector.
type Writer struct {
	root     string
	domainID string
	path     string
	lockPath string
}

func NewWriter(root, domainID string) (*Writer, error) {
	if root == "" {
		return nil, errors.New("shadow audit root is required")
	}
	if err := validateStableID("domain_id", domainID); err != nil {
		return nil, err
	}
	key := sha256String(domainID)
	return &Writer{
		root: root, domainID: domainID,
		path:     filepath.Join(root, "domain-"+key+".wal"),
		lockPath: filepath.Join(root, "domain-"+key+".lock"),
	}, nil
}

func (w *Writer) Append(ctx context.Context, input EventInput) (AuditRecord, error) {
	if err := input.DomainContext.Validate(); err != nil {
		return AuditRecord{}, err
	}
	if input.DomainContext.DomainID != w.domainID {
		return AuditRecord{}, errors.New("shadow audit domain does not match writer")
	}
	if err := os.MkdirAll(w.root, 0o700); err != nil {
		return AuditRecord{}, fmt.Errorf("shadow audit disk unavailable: %w", err)
	}
	lock, err := acquireAuditLock(ctx, w.lockPath)
	if err != nil {
		return AuditRecord{}, err
	}
	defer lock.Close()

	health, records := w.verifyUnlocked()
	if health.State != HealthHealthy && health.State != HealthMissing && health.State != HealthEmpty {
		return AuditRecord{}, fmt.Errorf("shadow audit fail closed: %s (%s)", health.State, health.ReasonCode)
	}
	sequence, predecessor := uint64(1), ""
	if len(records) > 0 {
		last := records[len(records)-1]
		sequence, predecessor = last.Sequence+1, last.RecordHash
	}
	record, err := newRecord(input, sequence, predecessor)
	if err != nil {
		return AuditRecord{}, err
	}
	body, err := json.Marshal(record)
	if err != nil {
		return AuditRecord{}, err
	}
	body = append(body, '\n')
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return AuditRecord{}, fmt.Errorf("shadow audit disk unavailable: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return AuditRecord{}, fmt.Errorf("shadow audit chmod: %w", err)
	}
	if n, writeErr := f.Write(body); writeErr != nil || n != len(body) {
		_ = f.Close()
		if writeErr == nil {
			writeErr = io.ErrShortWrite
		}
		return AuditRecord{}, fmt.Errorf("shadow audit append failed: %w", writeErr)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return AuditRecord{}, fmt.Errorf("shadow audit sync failed: %w", err)
	}
	if err := f.Close(); err != nil {
		return AuditRecord{}, fmt.Errorf("shadow audit close failed: %w", err)
	}
	post, _ := w.verifyUnlocked()
	if !post.Healthy() || post.LastSequence != record.Sequence || post.LastHash != record.RecordHash {
		return AuditRecord{}, fmt.Errorf("shadow audit post-append verification failed: %s", post.State)
	}
	return record, nil
}

func (w *Writer) Verify(ctx context.Context) Health {
	if err := os.MkdirAll(w.root, 0o700); err != nil {
		return Health{State: HealthDiskUnavailable, ReasonCode: "mkdir_failed", Enforcing: false}
	}
	lock, err := acquireAuditLock(ctx, w.lockPath)
	if err != nil {
		return Health{State: HealthDiskUnavailable, ReasonCode: "lock_failed", Enforcing: false}
	}
	defer lock.Close()
	health, _ := w.verifyUnlocked()
	return health
}

func (w *Writer) verifyUnlocked() (Health, []AuditRecord) {
	body, err := os.ReadFile(w.path)
	if errors.Is(err, os.ErrNotExist) {
		return Health{State: HealthMissing, ReasonCode: "wal_missing", Enforcing: false}, nil
	}
	if err != nil {
		return Health{State: HealthDiskUnavailable, ReasonCode: "read_failed", Enforcing: false}, nil
	}
	if len(body) == 0 {
		return Health{State: HealthEmpty, ReasonCode: "wal_empty", Enforcing: false}, nil
	}
	if body[len(body)-1] != '\n' {
		return Health{State: HealthTruncated, ReasonCode: "missing_record_terminator", Enforcing: false}, nil
	}
	lines := bytes.Split(body[:len(body)-1], []byte{'\n'})
	records := make([]AuditRecord, 0, len(lines))
	previous := ""
	seenEvents := make(map[string]bool, len(lines))
	seenRequests := make(map[string]bool, len(lines))
	seenDecisions := make(map[string]string, len(lines))
	seenOutcomes := make(map[string]bool, len(lines))
	for i, line := range lines {
		var record AuditRecord
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&record); err != nil {
			return unhealthy(HealthCorrupt, "invalid_json", records), records
		}
		if err := ensureDecoderEOF(dec); err != nil {
			return unhealthy(HealthCorrupt, "trailing_json", records), records
		}
		if err := record.validate(); err != nil {
			return unhealthy(HealthCorrupt, "invalid_record", records), records
		}
		if seenEvents[record.EventID] {
			return unhealthy(HealthCorrupt, "duplicate_event_id", records), records
		}
		seenEvents[record.EventID] = true
		switch record.Kind {
		case EventRequest:
			if seenRequests[record.RequestID] {
				return unhealthy(HealthCorrupt, "duplicate_request_id", records), records
			}
			seenRequests[record.RequestID] = true
		case EventDecision:
			if !seenRequests[record.RequestID] {
				return unhealthy(HealthCorrupt, "decision_without_request", records), records
			}
			if _, exists := seenDecisions[record.DecisionID]; exists {
				return unhealthy(HealthCorrupt, "duplicate_decision_id", records), records
			}
			seenDecisions[record.DecisionID] = record.RequestID
		case EventSimulatedOutcome:
			if !seenRequests[record.RequestID] {
				return unhealthy(HealthCorrupt, "outcome_without_request", records), records
			}
			requestID, exists := seenDecisions[record.DecisionID]
			if !exists || requestID != record.RequestID {
				return unhealthy(HealthCorrupt, "outcome_without_matching_decision", records), records
			}
			if seenOutcomes[record.OutcomeID] {
				return unhealthy(HealthCorrupt, "duplicate_outcome_id", records), records
			}
			seenOutcomes[record.OutcomeID] = true
		}
		wantSequence := uint64(i + 1)
		if record.Sequence != wantSequence {
			return unhealthy(HealthSequenceGap, "non_monotonic_sequence", records), records
		}
		if record.DomainContext.DomainID != w.domainID {
			return unhealthy(HealthDomainMismatch, "record_domain_mismatch", records), records
		}
		if record.PredecessorHash != previous {
			return unhealthy(HealthChainMismatch, "predecessor_mismatch", records), records
		}
		computed, err := record.computedHash()
		if err != nil || computed != record.RecordHash {
			return unhealthy(HealthCorrupt, "record_hash_mismatch", records), records
		}
		previous = record.RecordHash
		records = append(records, record)
	}
	return Health{State: HealthHealthy, ReasonCode: "verified", LastSequence: uint64(len(records)), LastHash: previous, Records: len(records), Enforcing: false}, records
}

func unhealthy(state HealthState, reason string, records []AuditRecord) Health {
	h := Health{State: state, ReasonCode: reason, Records: len(records), Enforcing: false}
	if len(records) > 0 {
		h.LastSequence = records[len(records)-1].Sequence
		h.LastHash = records[len(records)-1].RecordHash
	}
	return h
}

func ensureDecoderEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else {
		return err
	}
}

type auditLock struct{ file *os.File }

func acquireAuditLock(ctx context.Context, path string) (*auditLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("shadow audit lock unavailable: %w", err)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &auditLock{file: f}, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			_ = f.Close()
			return nil, fmt.Errorf("shadow audit lock failed: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("shadow audit lock timeout: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (l *auditLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	return l.file.Close()
}

func sha256String(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}
