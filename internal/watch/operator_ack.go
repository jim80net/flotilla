package watch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/jim80net/flotilla/internal/sessionmirror"
)

const operatorAckDir = "flotilla-operator-acks"

var operatorAckComponent = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// OperatorAckRoot returns the durable marker root shared by confirmed relay,
// finish-edge mirror, and unacked-backstop paths.
func OperatorAckRoot(rosterDir string) string {
	if rosterDir == "" {
		return ""
	}
	return filepath.Join(rosterDir, operatorAckDir)
}

type operatorAckRecord struct {
	Agent          string    `json:"agent"`
	ChannelID      string    `json:"channel_id"`
	MessageID      string    `json:"message_id"`
	DeliveredAt    time.Time `json:"delivered_at"`
	AcknowledgedAt time.Time `json:"acknowledged_at,omitempty"`
}

// TrackOperatorRelayAck records the exact origin message only after a relay has
// been confirmed in a seat. KindSend is deliberately inert: outbox epoch/cancel
// traffic cannot create an operator-channel acknowledgement.
func TrackOperatorRelayAck(root string, j Job, at time.Time) error {
	if root == "" || !isRelay(j.Kind) {
		return nil
	}
	// Legacy KindDefault jobs without Discord origin metadata are relays for
	// delivery purposes, but cannot participate in this exact-ID contract.
	if j.OriginChannel == "" || j.MessageID == "" {
		return nil
	}
	if err := sessionmirror.ValidateAgentName(j.Agent); err != nil {
		return fmt.Errorf("operator ack agent: %w", err)
	}
	if err := validateOperatorAckComponent("channel", j.OriginChannel); err != nil {
		return err
	}
	if err := validateOperatorAckComponent("message", j.MessageID); err != nil {
		return err
	}
	rec := operatorAckRecord{
		Agent: j.Agent, ChannelID: j.OriginChannel, MessageID: j.MessageID, DeliveredAt: at.UTC(),
	}
	return writeOperatorAckRecord(operatorAckPendingPath(root, rec), rec)
}

// AcknowledgeOperatorTurnFinal consumes the oldest confirmed operator relay
// pending for agent. One confirmed relay starts one turn, so one finish must
// never acknowledge a later relay that queued while the mirror was running.
func AcknowledgeOperatorTurnFinal(root, agent string, at time.Time) (int, error) {
	if root == "" {
		return 0, nil
	}
	if err := sessionmirror.ValidateAgentName(agent); err != nil {
		return 0, fmt.Errorf("operator ack agent: %w", err)
	}
	pendingRoot := filepath.Join(root, "pending", agent)
	var paths []string
	err := filepath.WalkDir(pendingRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if !d.IsDir() && filepath.Ext(path) == ".json" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("scan pending operator acks: %w", err)
	}
	type pendingRecord struct {
		path string
		rec  operatorAckRecord
	}
	pending := make([]pendingRecord, 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return 0, fmt.Errorf("read pending operator ack %q: %w", path, err)
		}
		var rec operatorAckRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return 0, fmt.Errorf("decode pending operator ack %q: %w", path, err)
		}
		if rec.Agent != agent {
			return 0, fmt.Errorf("pending operator ack %q belongs to %q, want %q", path, rec.Agent, agent)
		}
		pending = append(pending, pendingRecord{path: path, rec: rec})
	}
	if len(pending) == 0 {
		return 0, nil
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].rec.DeliveredAt.Equal(pending[j].rec.DeliveredAt) {
			return pending[i].path < pending[j].path
		}
		return pending[i].rec.DeliveredAt.Before(pending[j].rec.DeliveredAt)
	})
	oldest := pending[0]
	oldest.rec.AcknowledgedAt = at.UTC()
	if err := writeOperatorAckRecord(operatorAckMarkerPath(root, oldest.rec.ChannelID, oldest.rec.MessageID), oldest.rec); err != nil {
		return 0, err
	}
	if err := os.Remove(oldest.path); err != nil && !os.IsNotExist(err) {
		return 0, fmt.Errorf("consume pending operator ack %q: %w", oldest.path, err)
	}
	return 1, nil
}

// OperatorMessageAcknowledged reports whether the mirror path recorded a
// substantive turn-final for this exact origin channel and message.
func OperatorMessageAcknowledged(root, channelID, messageID string) bool {
	if root == "" {
		return false
	}
	if validateOperatorAckComponent("channel", channelID) != nil || validateOperatorAckComponent("message", messageID) != nil {
		return false
	}
	raw, err := os.ReadFile(operatorAckMarkerPath(root, channelID, messageID))
	if err != nil {
		return false
	}
	var rec operatorAckRecord
	if json.Unmarshal(raw, &rec) != nil {
		return false
	}
	return rec.ChannelID == channelID && rec.MessageID == messageID && !rec.AcknowledgedAt.IsZero()
}

// PruneOperatorAckMarkers removes acknowledged records older than before. Pending
// records are retained: losing one would turn a later legitimate turn-final into a
// false wake, while a recovered seat will consume its pending set naturally.
func PruneOperatorAckMarkers(root string, before time.Time) error {
	if root == "" || before.IsZero() {
		return nil
	}
	ackedRoot := filepath.Join(root, "acked")
	return filepath.WalkDir(ackedRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var rec operatorAckRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return fmt.Errorf("decode operator ack marker %q: %w", path, err)
		}
		at := rec.AcknowledgedAt
		if at.IsZero() {
			at = rec.DeliveredAt
		}
		if !at.IsZero() && at.Before(before) {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	})
}

func validateOperatorAckComponent(label, value string) error {
	if !operatorAckComponent.MatchString(value) || value == "." || value == ".." {
		return fmt.Errorf("operator ack %s %q is not a safe identifier", label, value)
	}
	return nil
}

func operatorAckPendingPath(root string, rec operatorAckRecord) string {
	return filepath.Join(root, "pending", rec.Agent, rec.ChannelID, rec.MessageID+".json")
}

func operatorAckMarkerPath(root, channelID, messageID string) string {
	return filepath.Join(root, "acked", channelID, messageID+".json")
}

func writeOperatorAckRecord(path string, rec operatorAckRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir operator ack dir: %w", err)
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal operator ack: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create operator ack temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write operator ack temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync operator ack temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close operator ack temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("commit operator ack: %w", err)
	}
	return nil
}
