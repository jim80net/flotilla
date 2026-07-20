package harnessquality

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var ledgerLocks sync.Map

func LedgerPath(rosterDir string) string { return filepath.Join(rosterDir, LedgerName) }

func Append(rosterDir string, event Event) (Event, error) {
	if strings.TrimSpace(rosterDir) == "" {
		return Event{}, fmt.Errorf("harnessquality: roster dir is required")
	}
	if event.Schema == "" {
		event.Schema = EventSchema
	}
	if event.ID == "" {
		id, err := newID()
		if err != nil {
			return Event{}, err
		}
		event.ID = id
	}
	if event.TS == "" {
		event.TS = time.Now().UTC().Format(time.RFC3339)
	}
	if event.WorkClass == "" {
		event.WorkClass = WorkUnclassified
	}
	if strings.TrimSpace(event.Model) == "" {
		event.Model = "unknown"
	}
	if err := event.Validate(); err != nil {
		return Event{}, fmt.Errorf("harnessquality: invalid event: %w", err)
	}
	line, err := json.Marshal(event)
	if err != nil {
		return Event{}, fmt.Errorf("harnessquality: marshal event: %w", err)
	}
	line = append(line, '\n')
	path := LedgerPath(rosterDir)
	v, _ := ledgerLocks.LoadOrStore(path, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(rosterDir, 0o700); err != nil {
		return Event{}, fmt.Errorf("harnessquality: create roster dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return Event{}, fmt.Errorf("harnessquality: open ledger: %w", err)
	}
	if _, err := f.Write(line); err != nil {
		f.Close()
		return Event{}, fmt.Errorf("harnessquality: append ledger: %w", err)
	}
	if err := f.Close(); err != nil {
		return Event{}, fmt.Errorf("harnessquality: close ledger: %w", err)
	}
	return event, nil
}

func Load(rosterDir string) ([]Event, error) {
	path := LedgerPath(rosterDir)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("harnessquality: open ledger: %w", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var events []Event
	line := 0
	for scanner.Scan() {
		line++
		if len(strings.TrimSpace(scanner.Text())) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("harnessquality: parse %s line %d: %w", path, line, err)
		}
		if err := event.Validate(); err != nil {
			return nil, fmt.Errorf("harnessquality: validate %s line %d: %w", path, line, err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("harnessquality: read ledger: %w", err)
	}
	return events, nil
}

func WriteContext(rosterDir string, context Context) error {
	if context.Schema == "" {
		context.Schema = ContextSchema
	}
	if context.UpdatedAt == "" {
		context.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := context.Validate(); err != nil {
		return fmt.Errorf("harnessquality: invalid context: %w", err)
	}
	dir := filepath.Join(rosterDir, ContextDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("harnessquality: create context dir: %w", err)
	}
	data, err := json.MarshalIndent(context, "", "  ")
	if err != nil {
		return fmt.Errorf("harnessquality: marshal context: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".context-*")
	if err != nil {
		return fmt.Errorf("harnessquality: create context temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
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
	return os.Rename(tmpName, filepath.Join(dir, context.Seat+".json"))
}

func ReadContext(rosterDir, seat string) (Context, bool, error) {
	path := filepath.Join(rosterDir, ContextDir, seat+".json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Context{}, false, nil
	}
	if err != nil {
		return Context{}, false, fmt.Errorf("harnessquality: read context: %w", err)
	}
	var context Context
	if err := json.Unmarshal(raw, &context); err != nil {
		return Context{}, false, fmt.Errorf("harnessquality: parse context %q: %w", path, err)
	}
	if context.Seat != seat {
		return Context{}, false, fmt.Errorf("harnessquality: context %q names seat %q", path, context.Seat)
	}
	if err := context.Validate(); err != nil {
		return Context{}, false, fmt.Errorf("harnessquality: validate context %q: %w", path, err)
	}
	return context, true, nil
}

func newID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("harnessquality: generate id: %w", err)
	}
	return "hq-" + hex.EncodeToString(raw[:]), nil
}
