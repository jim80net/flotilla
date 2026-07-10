package org

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultFileName is the optional org-truth document next to the roster.
const DefaultFileName = "fleet-org.yaml"

// DefaultPath returns <roster-dir>/fleet-org.yaml.
func DefaultPath(rosterPath string) string {
	if rosterPath == "" {
		return DefaultFileName
	}
	return filepath.Join(filepath.Dir(rosterPath), DefaultFileName)
}

// ResolvePath picks the org file path for a roster load.
//
//   - explicit non-empty (from --org-file / FLOTILLA_ORG_FILE): that path is
//     required to exist.
//   - explicit empty: use DefaultPath(rosterPath); if missing, ok=false (derive-only).
func ResolvePath(rosterPath, explicit string) (path string, required bool, err error) {
	if explicit != "" {
		return explicit, true, nil
	}
	return DefaultPath(rosterPath), false, nil
}

// OpenOptional loads an org file when present.
//
//   - required=true: missing file is an error.
//   - required=false: missing file returns (nil, nil) — caller uses derive path.
func OpenOptional(path string, required bool) (*File, error) {
	if path == "" {
		if required {
			return nil, fmt.Errorf("org-truth: org file path is empty")
		}
		return nil, nil
	}
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			if required {
				return nil, fmt.Errorf("org-truth: org file %q not found", path)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("org-truth: stat %q: %w", path, err)
	}
	return LoadFile(path)
}
