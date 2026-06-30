package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jim80net/flotilla/internal/workspace"
)

const autoSwitchCapWindow = time.Hour

type autoSwitchCapFile struct {
	Times []time.Time `json:"times"`
}

func autoSwitchCapPath(agent string) (string, error) {
	dir, err := workspace.Dir(agent)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "auto-switch-cap.json"), nil
}

func loadAutoSwitchCapTimes(agent string) ([]time.Time, error) {
	path, err := autoSwitchCapPath(agent)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	var f autoSwitchCapFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return f.Times, nil
}

func recordAutoSwitchCap(agent string, now time.Time) error {
	path, err := autoSwitchCapPath(agent)
	if err != nil {
		return err
	}
	times, err := loadAutoSwitchCapTimes(agent)
	if err != nil {
		return err
	}
	times = append(times, now)
	raw, err := json.MarshalIndent(autoSwitchCapFile{Times: times}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
