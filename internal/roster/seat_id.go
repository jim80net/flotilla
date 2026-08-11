package roster

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const seatIDBytes = 8

// HasStructuredHierarchy reports whether the roster has crossed the crown
// boundary: any explicit parent edge, or seat IDs on every seat (a valid flat
// tree). Assigning one backward-compatible ID during provisioning must not by
// itself discard the still-interim channel/file view before migration lands.
func (c *Config) HasStructuredHierarchy() bool {
	if c == nil || len(c.Agents) == 0 {
		return false
	}
	allIDs := true
	for _, agent := range c.Agents {
		if agent.Parent != "" {
			return true
		}
		if agent.SeatID == "" {
			allIDs = false
		}
	}
	return allIDs
}

func validateSeatID(id string) error {
	if len(id) < 8 || len(id) > 32 || len(id)%2 != 0 {
		return fmt.Errorf("must be 8-32 lowercase hexadecimal characters with an even length")
	}
	if id != strings.ToLower(id) {
		return fmt.Errorf("must use lowercase hexadecimal characters")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return fmt.Errorf("must be hexadecimal")
	}
	return nil
}

func validateRosterParentCycles(agents []Agent, seatByID map[string]string) error {
	parentByName := make(map[string]string, len(agents))
	for _, agent := range agents {
		if agent.Parent != "" {
			parentByName[agent.Name] = seatByID[agent.Parent]
		}
	}
	const white, gray, black = 0, 1, 2
	colors := make(map[string]int, len(agents))
	var visit func(string) error
	visit = func(name string) error {
		colors[name] = gray
		if parent := parentByName[name]; parent != "" {
			switch colors[parent] {
			case gray:
				return fmt.Errorf("agent %q parent edge forms a cycle through %q", name, parent)
			case white:
				if err := visit(parent); err != nil {
					return err
				}
			}
		}
		colors[name] = black
		return nil
	}
	for _, agent := range agents {
		if colors[agent.Name] == white {
			if err := visit(agent.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// EnsureSeatID assigns an opaque seat id to an existing roster agent when it is
// absent. It is the provisioning write seam used by workspace init and register.
// Existing IDs are returned unchanged. The update is a same-directory atomic
// replacement of a regular, non-symlink roster file.
func EnsureSeatID(path, agentName string) (id string, assigned bool, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", false, fmt.Errorf("stat roster %q: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", false, fmt.Errorf("roster %q must be a regular non-symlink file", path)
	}
	cfg, err := Load(path)
	if err != nil {
		return "", false, err
	}
	index := -1
	used := make(map[string]bool, len(cfg.Agents))
	for i, agent := range cfg.Agents {
		if agent.SeatID != "" {
			used[agent.SeatID] = true
		}
		if agent.Name == agentName {
			index = i
		}
	}
	if index < 0 {
		return "", false, fmt.Errorf("no agent named %q in roster", agentName)
	}
	if cfg.Agents[index].SeatID != "" {
		return cfg.Agents[index].SeatID, false, nil
	}
	for attempts := 0; attempts < 32; attempts++ {
		candidate, genErr := randomSeatID(rand.Reader)
		if genErr != nil {
			return "", false, genErr
		}
		if !used[candidate] {
			id = candidate
			break
		}
	}
	if id == "" {
		return "", false, fmt.Errorf("could not allocate a unique seat_id")
	}
	cfg.Agents[index].SeatID = id
	body, err := rosterWithSeatID(path, agentName, id)
	if err != nil {
		return "", false, err
	}
	if err := atomicReplaceRegular(path, body, info.Mode().Perm()); err != nil {
		return "", false, err
	}
	return id, true, nil
}

// rosterWithSeatID patches only the agents array in the authored JSON. It does
// not marshal Config because Load intentionally resolves some runtime defaults
// (including host-local paths) that must never be written back to the roster.
func rosterWithSeatID(path, agentName, id string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read roster %q: %w", path, err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("parse roster %q: %w", path, err)
	}
	var agents []map[string]json.RawMessage
	if err := json.Unmarshal(document["agents"], &agents); err != nil {
		return nil, fmt.Errorf("parse roster %q agents: %w", path, err)
	}
	patched := false
	for _, agent := range agents {
		var name string
		if err := json.Unmarshal(agent["name"], &name); err != nil {
			return nil, fmt.Errorf("parse roster %q agent name: %w", path, err)
		}
		if name != agentName {
			continue
		}
		encodedID, _ := json.Marshal(id)
		agent["seat_id"] = encodedID
		patched = true
		break
	}
	if !patched {
		return nil, fmt.Errorf("no agent named %q in roster", agentName)
	}
	document["agents"], err = json.Marshal(agents)
	if err != nil {
		return nil, fmt.Errorf("encode roster %q agents: %w", path, err)
	}
	body, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode roster %q: %w", path, err)
	}
	return append(body, '\n'), nil
}

func randomSeatID(source io.Reader) (string, error) {
	var raw [seatIDBytes]byte
	if _, err := io.ReadFull(source, raw[:]); err != nil {
		return "", fmt.Errorf("generate seat_id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func atomicReplaceRegular(path string, body []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".flotilla-roster-*")
	if err != nil {
		return fmt.Errorf("create roster replacement: %w", err)
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("chmod roster replacement: %w", err)
	}
	if _, err := tmp.Write(body); err != nil {
		return fmt.Errorf("write roster replacement: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync roster replacement: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close roster replacement: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace roster %q: %w", path, err)
	}
	d, err := os.Open(dir)
	if err == nil {
		err = d.Sync()
		_ = d.Close()
	}
	if err != nil && !errors.Is(err, os.ErrInvalid) {
		return fmt.Errorf("sync roster directory %q: %w", dir, err)
	}
	ok = true
	return nil
}
