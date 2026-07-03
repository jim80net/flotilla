package goals

import (
	"encoding/json"
	"fmt"
	"os"
)

// CompileJSON validates and serializes a File to fleet-goals.json bytes.
func CompileJSON(f File) ([]byte, error) {
	NormalizeFileScopes(&f)
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

// MaybeCompileYAMLToJSON refreshes the compiled json cache when the yaml source is
// newer than the json (or json is absent). A missing yaml is not an error.
func MaybeCompileYAMLToJSON(yamlPath, jsonPath string) error {
	if yamlPath == "" || jsonPath == "" {
		return nil
	}
	yst, err := os.Stat(yamlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("goals: stat yaml %q: %w", yamlPath, err)
	}
	jst, jerr := os.Stat(jsonPath)
	if jerr != nil && !os.IsNotExist(jerr) {
		return fmt.Errorf("goals: stat json %q: %w", jsonPath, jerr)
	}
	needCompile := os.IsNotExist(jerr) || yst.ModTime().After(jst.ModTime())
	if !needCompile {
		return nil
	}
	f, err := LoadYAML(yamlPath)
	if err != nil {
		return err
	}
	return WriteJSON(jsonPath, f)
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
