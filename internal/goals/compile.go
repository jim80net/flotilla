package goals

import (
	"encoding/json"
	"fmt"
	"os"
)

// CompileJSON validates and serializes a File to fleet-goals.json bytes.
func CompileJSON(f File) ([]byte, error) {
	if err := f.validate(); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("goals: compile json: %w", err)
	}
	return append(b, '\n'), nil
}

// CompileYAML parses YAML and returns compiled JSON bytes (validate + normalize + serialize).
func CompileYAML(raw []byte) ([]byte, error) {
	f, err := ParseYAML(raw)
	if err != nil {
		return nil, err
	}
	return CompileJSON(f)
}

// LoadYAML reads fleet-goals.yaml from path. A missing file yields an empty File (not an error).
func LoadYAML(path string) (File, error) {
	if path == "" {
		return File{Goals: []Goal{}}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return File{Goals: []Goal{}}, nil
		}
		return File{}, fmt.Errorf("goals: read %q: %w", path, err)
	}
	return ParseYAML(raw)
}

// WriteJSON writes a compiled File to path (mode 0600).
func WriteJSON(path string, f File) error {
	b, err := CompileJSON(f)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("goals: write %q: %w", path, err)
	}
	return nil
}
