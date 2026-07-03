package sessionmirror

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var fileLocks sync.Map // ledger path → *sync.Mutex

func withFileLock(path string, fn func() error) error {
	v, _ := fileLocks.LoadOrStore(path, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	return fn()
}

// AppendOptions configures ledger append retention.
type AppendOptions struct {
	MaxEntries int // 0 ⇒ DefaultMaxEntries
}

// LedgerPath returns the per-agent jsonl path under rosterDir.
func LedgerPath(rosterDir, agent string) string {
	return filepath.Join(rosterDir, "session-mirror", agent+".jsonl")
}

// Append appends one record to the agent ledger, enforcing a ring buffer of at most
// MaxEntries lines. The write is atomic on compaction (temp + rename); a simple
// append uses O_APPEND when under the cap.
func Append(rosterDir, agent string, rec Record, opts AppendOptions) error {
	if rosterDir == "" || agent == "" {
		return fmt.Errorf("sessionmirror: roster dir and agent are required")
	}
	max := opts.MaxEntries
	if max <= 0 {
		max = DefaultMaxEntries
	}

	line, err := marshalLedgerLine(rec)
	if err != nil {
		return err
	}

	dir := filepath.Join(rosterDir, "session-mirror")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("sessionmirror: mkdir %q: %w", dir, err)
	}
	path := filepath.Join(dir, agent+".jsonl")

	return withFileLock(path, func() error {
		entries, err := readLines(path)
		if err != nil {
			return err
		}
		entries = append(entries, append([]byte(nil), line...))
		if len(entries) > max {
			entries = entries[len(entries)-max:]
			return writeLinesAtomic(path, entries)
		}

		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("sessionmirror: open %q: %w", path, err)
		}
		if _, err := f.Write(line); err != nil {
			f.Close()
			return fmt.Errorf("sessionmirror: append %q: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("sessionmirror: close %q: %w", path, err)
		}
		return nil
	})
}

func readLines(path string) ([][]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("sessionmirror: read %q: %w", path, err)
	}
	var out [][]byte
	sc := newLineScanner(raw)
	for sc.Scan() {
		b := append([]byte(nil), sc.Bytes()...)
		if len(b) == 0 {
			continue
		}
		out = append(out, b)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("sessionmirror: scan %q: %w", path, err)
	}
	return out, nil
}

func writeLinesAtomic(path string, lines [][]byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("sessionmirror: create temp in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmpPath)
		}
	}()

	for _, line := range lines {
		if _, err := tmp.Write(line); err != nil {
			tmp.Close()
			return fmt.Errorf("sessionmirror: write temp: %w", err)
		}
		if line[len(line)-1] != '\n' {
			if _, err := tmp.Write([]byte{'\n'}); err != nil {
				tmp.Close()
				return fmt.Errorf("sessionmirror: write temp newline: %w", err)
			}
		}
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("sessionmirror: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("sessionmirror: rename %q → %q: %w", tmpPath, path, err)
	}
	ok = true
	return nil
}
