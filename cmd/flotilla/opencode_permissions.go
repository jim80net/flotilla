package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jim80net/flotilla/internal/opencodeperm"
)

func seedOpenCodeRecyclePermissions(cwd string) error {
	if !filepath.IsAbs(cwd) {
		return fmt.Errorf("OpenCode recycle permissions: cwd %q is not absolute", cwd)
	}
	path, err := opencodeperm.ConfigPath()
	if err != nil {
		return err
	}
	changed, err := opencodeperm.SeedEffective(path, cwd)
	if err != nil {
		return err
	}
	for _, changedPath := range changed {
		fmt.Fprintf(os.Stderr, "flotilla: seeded narrow OpenCode recycle permissions in %s\n", changedPath)
	}
	return nil
}
