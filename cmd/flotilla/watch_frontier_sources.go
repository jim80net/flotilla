package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// frontierSourceFlag publishes the exact coordinator→tracker mapping used by the return-to guard.
// It is repeatable: --frontier-source alpha-xo=/path/to/alpha-backlog.md.
type frontierSourceFlag map[string]string

func (f *frontierSourceFlag) Set(value string) error {
	owner, path, ok := strings.Cut(value, "=")
	owner, path = strings.TrimSpace(owner), strings.TrimSpace(path)
	if !ok || owner == "" || path == "" {
		return fmt.Errorf("frontier source must be coordinator=path")
	}
	if *f == nil {
		*f = make(map[string]string)
	}
	if _, exists := (*f)[owner]; exists {
		return fmt.Errorf("frontier source for %q is published more than once", owner)
	}
	(*f)[owner] = filepath.Clean(path)
	return nil
}

func (f *frontierSourceFlag) String() string {
	if f == nil || len(*f) == 0 {
		return ""
	}
	keys := make([]string, 0, len(*f))
	for owner := range *f {
		keys = append(keys, owner)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, owner := range keys {
		parts = append(parts, owner+"="+(*f)[owner])
	}
	return strings.Join(parts, ",")
}

// pathFor returns only an explicitly owner-bound source. --backlog-file remains the primary XO's
// backwards-compatible publication; it is never shared with another coordinator.
func (f frontierSourceFlag) pathFor(owner, primaryXO, primaryBacklog string) string {
	if path := f[owner]; path != "" {
		return path
	}
	if owner == primaryXO && primaryBacklog != "" {
		return filepath.Clean(primaryBacklog)
	}
	return ""
}
