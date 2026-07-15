package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/jim80net/flotilla/internal/roster"
)

// watchRosterSnapshot is published as one pointer so config and every topology
// compiled into Config (synthesis maps and the org DAG) can never come from
// different roster generations.
type watchRosterSnapshot struct {
	Config     *roster.Config
	Generation uint64
}

type watchRosterReloader struct {
	rosterPath string
	orgFile    string
	current    atomic.Pointer[watchRosterSnapshot]
	lastSeen   [32]byte
	load       func(string, roster.LoadOptions) (*roster.Config, error)
	validate   func(*roster.Config) error
}

func newWatchRosterReloader(rosterPath, orgFile string, initial *roster.Config) (*watchRosterReloader, error) {
	r := &watchRosterReloader{
		rosterPath: rosterPath,
		orgFile:    orgFile,
		load:       roster.LoadWith,
		validate:   validateAgentSurfaces,
	}
	digest, err := watchRosterDigest(rosterPath, orgFile)
	if err != nil {
		return nil, err
	}
	r.lastSeen = digest
	r.current.Store(&watchRosterSnapshot{Config: initial, Generation: 1})
	return r, nil
}

func (r *watchRosterReloader) Snapshot() *watchRosterSnapshot {
	return r.current.Load()
}

// Check builds and validates a complete candidate off-side. Only a fully clean
// candidate is published; any failure retains the entire last-good snapshot.
func (r *watchRosterReloader) Check() (bool, error) {
	digest, err := watchRosterDigest(r.rosterPath, r.orgFile)
	if err != nil {
		return false, fmt.Errorf("read roster reload inputs: %w", err)
	}
	if digest == r.lastSeen {
		return false, nil
	}
	r.lastSeen = digest // diagnose one time per distinct edit; a later edit retries
	candidate, err := r.load(r.rosterPath, roster.LoadOptions{OrgFile: r.orgFile})
	if err != nil {
		return false, fmt.Errorf("reload roster %q: %w", r.rosterPath, err)
	}
	if err := r.validate(candidate); err != nil {
		return false, fmt.Errorf("reload roster %q: %w", r.rosterPath, err)
	}
	previous := r.current.Load()
	r.current.Store(&watchRosterSnapshot{Config: candidate, Generation: previous.Generation + 1})
	return true, nil
}

func watchRosterDigest(rosterPath, orgFile string) ([32]byte, error) {
	h := sha256.New()
	paths := []string{rosterPath}
	if orgFile != "" && orgFile != "-" {
		paths = append(paths, orgFile)
	} else if orgFile == "" {
		paths = append(paths, filepath.Join(filepath.Dir(rosterPath), "fleet-org.yaml"))
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) && path != rosterPath {
				h.Write([]byte("absent:" + path))
				continue
			}
			return [32]byte{}, err
		}
		h.Write([]byte(path))
		h.Write(raw)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}
