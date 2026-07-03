package goals

import (
	"fmt"
	"os"
)

// writeFilePreserveMode writes data to path, keeping the destination's existing
// permission bits when the file already exists (default 0600 for new files).
func writeFilePreserveMode(path string, data []byte) error {
	mode := os.FileMode(0o600)
	st, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("goals: stat %q: %w", path, err)
		}
	} else {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("goals: write %q: %w", path, err)
	}
	return nil
}
