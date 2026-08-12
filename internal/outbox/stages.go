package outbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jim80net/flotilla/internal/inbound"
)

// DeliveryStage names transport facts, not inferred task handling. Submitted is
// transport delivery: the body was pasted and Enter was accepted/confirmed.
// RecipientConsumed remains a separate optional acknowledgement.
type DeliveryStage string

const (
	StageQueued            DeliveryStage = "queued"
	StageAttemptedRefused  DeliveryStage = "attempted_refused"
	StageSubmitted         DeliveryStage = "submitted"
	StageRecipientConsumed DeliveryStage = "recipient_consumed"
	StageCanceled          DeliveryStage = "canceled"
	StageFailed            DeliveryStage = "failed"
)

type StageEvent struct {
	OutboxID    string        `json:"outbox_id"`
	Sender      string        `json:"sender"`
	Recipient   string        `json:"recipient"`
	Nonce       string        `json:"nonce,omitempty"`
	PayloadHash string        `json:"payload_hash,omitempty"`
	Stage       DeliveryStage `json:"stage"`
	At          time.Time     `json:"at"`
	LastAt      time.Time     `json:"last_at,omitempty"`
	Count       uint64        `json:"count,omitempty"`
	Evidence    string        `json:"evidence,omitempty"`
}

type stageFile struct {
	Version int          `json:"version"`
	Events  []StageEvent `json:"events"`
}

func stagesPath(rosterDir string) string {
	return filepath.Join(rosterDir, "flotilla-delivery-stages.json")
}

// RecordStage appends an immutable metadata-only stage event. No message body is
// persisted. The stage ledger is separate from the pending queue so submitted
// evidence survives removal of the outbox entry.
func RecordStage(rosterDir string, e Entry, stage DeliveryStage, evidence string, now time.Time) error {
	return recordStage(rosterDir, e, inbound.ParseOwnDispatchNonce(e.Message), stagePayloadHash(e.Message), stage, evidence, now)
}

func recordStage(rosterDir string, e Entry, nonce, payloadHash string, stage DeliveryStage, evidence string, now time.Time) error {
	if rosterDir == "" || e.ID == "" || e.Sender == "" || e.Recipient == "" {
		return fmt.Errorf("outbox stage: incomplete delivery identity")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	path := stagesPath(rosterDir)
	lock, err := acquireFileLock(path, outboxLockTimeout)
	if err != nil {
		return err
	}
	defer lock.release()
	f := stageFile{Version: 1}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &f); err != nil {
			return fmt.Errorf("outbox stage: corrupt ledger: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("outbox stage: read: %w", err)
	}
	f.Version = 1
	if stage == StageAttemptedRefused {
		for i := len(f.Events) - 1; i >= 0; i-- {
			event := &f.Events[i]
			if event.OutboxID != e.ID || event.Stage != StageAttemptedRefused {
				continue
			}
			if event.Count == 0 {
				event.Count = 1
			}
			event.Count++
			event.LastAt = now.UTC()
			event.Evidence = evidence
			return saveStageFile(rosterDir, path, f)
		}
	}
	f.Events = append(f.Events, StageEvent{OutboxID: e.ID, Sender: e.Sender,
		Recipient: e.Recipient, Nonce: nonce, PayloadHash: payloadHash,
		Stage: stage, At: now.UTC(), LastAt: now.UTC(), Count: 1, Evidence: evidence})
	return saveStageFile(rosterDir, path, f)
}

func saveStageFile(rosterDir, path string, f stageFile) error {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(rosterDir, ".delivery-stages-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func stagePayloadHash(message string) string {
	sum := sha256.Sum256([]byte(inbound.StripDispatchFooter(message)))
	return hex.EncodeToString(sum[:16])
}

// DeliveryStages returns the durable timeline for one outbox identity.
func DeliveryStages(rosterDir, id string) ([]StageEvent, error) {
	events, err := AllDeliveryStages(rosterDir)
	if err != nil {
		return nil, err
	}
	var out []StageEvent
	for _, event := range events {
		if event.OutboxID == id {
			out = append(out, event)
		}
	}
	return out, nil
}

// HasDeliveryStage reports whether a durable delivery identity has reached stage.
// Callers use this to reconcile a submitted body whose pending-row removal failed:
// submission must never be repeated merely because the queue cleanup was interrupted.
func HasDeliveryStage(rosterDir, id string, stage DeliveryStage) (bool, error) {
	events, err := DeliveryStages(rosterDir, id)
	if err != nil {
		return false, err
	}
	for _, event := range events {
		if event.Stage == stage {
			return true, nil
		}
	}
	return false, nil
}

// RecordStageByEdge links recipient handling to the exact immutable outbound
// edge. Nonce alone is intentionally insufficient because one dispatch may
// traverse multiple sender/recipient edges.
func RecordStageByEdge(rosterDir, sender, recipient, nonce, payloadHash string, stage DeliveryStage, evidence string, now time.Time) error {
	if sender == "" || recipient == "" || nonce == "" || payloadHash == "" {
		return nil
	}
	events, err := AllDeliveryStages(rosterDir)
	if err != nil {
		return err
	}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Nonce != nonce || event.Sender != sender || event.Recipient != recipient || event.PayloadHash != payloadHash {
			continue
		}
		return recordStage(rosterDir, Entry{ID: event.OutboxID, Sender: event.Sender,
			Recipient: event.Recipient}, nonce, payloadHash, stage, evidence, now)
	}
	return nil
}

// AllDeliveryStages returns the queryable metadata timeline.
func AllDeliveryStages(rosterDir string) ([]StageEvent, error) {
	raw, err := os.ReadFile(stagesPath(rosterDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f stageFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	return append([]StageEvent(nil), f.Events...), nil
}
