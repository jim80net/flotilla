package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/chapterend"
)

const (
	chapterEndRecycleCooldownFile = "chapter-end-recycle-cooldown.json"
	maxRecycleStatusHistory       = 8
)

type chapterEndRecycleCooldown struct {
	Token               string    `json:"token"`
	RecycledAt          time.Time `json:"recycled_at"`
	FirstFinishObserved bool      `json:"first_finish_observed"`
}

type recycleStatusHistoryEntry struct {
	At    string `json:"at,omitempty"`
	Token string `json:"token,omitempty"`
	OK    bool   `json:"ok"`
}

// recordSuccessfulRecycleCooldown persists the lifecycle latch shared by
// commanded and watch-dispatched recycle. The successor process can therefore
// observe the success even when watch did not launch the original command.
func recordSuccessfulRecycleCooldown(statusDir, token string, at time.Time) error {
	if statusDir == "" || token == "" {
		return fmt.Errorf("record recycle cooldown: status directory and token are required")
	}
	rec := chapterEndRecycleCooldown{Token: token, RecycledAt: at.UTC()}
	return writeJSONAtomic(filepath.Join(statusDir, chapterEndRecycleCooldownFile), rec)
}

// admitChapterEndAfterRecycle applies the durable #1037 cooldown to one finish
// edge. A new unblocked backlog item proves a new chapter. Without that proof,
// the successor's first finish is always consumed. Later inferred lane/merge
// signals stay suppressed; a later explicit coordinator signal may reopen.
func admitChapterEndAfterRecycle(statusDir string, r chapterend.Result, backlogMD string) (bool, string, error) {
	path := filepath.Join(statusDir, chapterEndRecycleCooldownFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("read recycle cooldown: %w", err)
	}
	var cooldown chapterEndRecycleCooldown
	if err := json.Unmarshal(raw, &cooldown); err != nil || cooldown.Token == "" || cooldown.RecycledAt.IsZero() {
		if err == nil {
			err = fmt.Errorf("missing token or recycled_at")
		}
		return false, "", fmt.Errorf("parse recycle cooldown: %w", err)
	}

	if len(backlog.Parse(backlogMD).Unblocked) > 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, "", fmt.Errorf("clear recycle cooldown after new backlog: %w", err)
		}
		return true, "new-unblocked-backlog", nil
	}

	if !cooldown.FirstFinishObserved {
		cooldown.FirstFinishObserved = true
		if err := writeJSONAtomic(path, cooldown); err != nil {
			return false, "", fmt.Errorf("record successor first finish: %w", err)
		}
		return false, "successor-first-finish", nil
	}

	switch r.Signal {
	case chapterend.SignalCoordinatorMark,
		chapterend.SignalCoordinatorSelfHandoff,
		chapterend.SignalCoordinatorTenure:
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, "", fmt.Errorf("clear recycle cooldown after explicit chapter signal: %w", err)
		}
		return true, "explicit-new-chapter-signal", nil
	default:
		return false, "recycle-cooldown", nil
	}
}

func priorRecycleStatusHistory(path string) []recycleStatusHistoryEntry {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var previous struct {
		At      string                      `json:"at"`
		Token   string                      `json:"token"`
		OK      bool                        `json:"ok"`
		History []recycleStatusHistoryEntry `json:"history"`
	}
	if json.Unmarshal(raw, &previous) != nil {
		return nil
	}
	history := append([]recycleStatusHistoryEntry(nil), previous.History...)
	if previous.Token != "" {
		history = append(history, recycleStatusHistoryEntry{At: previous.At, Token: previous.Token, OK: previous.OK})
	}
	if len(history) > maxRecycleStatusHistory {
		history = history[len(history)-maxRecycleStatusHistory:]
	}
	return history
}

func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
