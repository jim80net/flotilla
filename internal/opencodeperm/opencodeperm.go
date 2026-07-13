// Package opencodeperm provisions the narrow OpenCode permissions needed by
// flotilla's portable recycle bridge. It does not broaden normal edit or shell
// authority: only handoff blobs and their exact rm cleanup are allowed.
package opencodeperm

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/tailscale/hujson"
)

const handoffEditPattern = ".flotilla/handoffs/recycle-*.md"
const lockTimeout = 2 * time.Second

// ConfigPath returns OpenCode's standard user config file. OpenCode v1.3.15
// loads this before project config and accepts later, more-specific rules.
func ConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("opencode recycle permissions: resolve user config: %w", err)
	}
	return filepath.Join(dir, "opencode", "opencode.json"), nil
}

// SeedEffective installs the recycle rules in every config layer that can
// override them for cwd. OpenCode v1.3.15 loads the user config first, then
// project-root opencode.json/jsonc, then .opencode/opencode.json/jsonc. A
// project-level scalar such as edit:"ask" therefore defeats a user-only seed.
// Missing project files are not created; the user config remains the default.
func SeedEffective(userConfigPath, cwd string) ([]string, error) {
	paths := []string{userConfigPath}
	for _, relative := range []string{
		"opencode.json",
		"opencode.jsonc",
		filepath.Join(".opencode", "opencode.json"),
		filepath.Join(".opencode", "opencode.jsonc"),
	} {
		path := filepath.Join(cwd, relative)
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("opencode recycle permissions: inspect %q: %w", path, err)
		}
	}
	var changed []string
	for _, path := range paths {
		ok, err := Seed(path, cwd)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, path)
		}
	}
	return changed, nil
}

// Seed installs idempotent, specific allow rules for the portable handoff.
// Existing broad actions become the "*" fallback; all unrelated authority is
// preserved. The caller must pass an absolute worktree path.
func Seed(configPath, cwd string) (bool, error) {
	if !filepath.IsAbs(cwd) {
		return false, fmt.Errorf("opencode recycle permissions: cwd %q is not absolute", cwd)
	}
	unlock, err := acquireLock(lockPath(configPath))
	if err != nil {
		return false, err
	}
	defer unlock()
	raw, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("opencode recycle permissions: read %q: %w", configPath, err)
	}
	doc := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &doc); err != nil {
			if filepath.Ext(configPath) != ".jsonc" {
				return false, fmt.Errorf("opencode recycle permissions: parse %q: %w", configPath, err)
			}
			body, patchErr := seedJSONC(raw, cwd)
			if patchErr != nil {
				return false, fmt.Errorf("opencode recycle permissions: parse %q as JSONC: %w", configPath, patchErr)
			}
			if bytes.Equal(raw, body) {
				return false, nil
			}
			return writeConfig(configPath, body)
		}
	}
	permission, err := permissionObject(doc["permission"])
	if err != nil {
		return false, err
	}
	changed := false
	editChanged, err := setSpecific(permission, "edit", handoffEditPattern, "allow")
	if err != nil {
		return false, err
	}
	changed = editChanged || changed
	cleanupGlob := filepath.ToSlash(filepath.Join(filepath.Clean(cwd), ".flotilla", "handoffs", "recycle-*.md"))
	// Do not sweep quoted rules for other cwd values: the user config is shared
	// by every managed OpenCode worktree, and each one needs its own exact cleanup.
	// Remove the pre-#666 unquoted seed. The shared takeover turn has always
	// quoted the path, so retaining this non-matching rule adds no capability.
	if bash, ok := permission["bash"].(map[string]any); ok {
		legacy := "rm -f " + cleanupGlob
		if _, exists := bash[legacy]; exists {
			delete(bash, legacy)
			changed = true
		}
	}
	cleanup := `rm -f "` + cleanupGlob + `"`
	bashChanged, err := setSpecific(permission, "bash", cleanup, "allow")
	if err != nil {
		return false, err
	}
	changed = bashChanged || changed
	doc["permission"] = permission
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, fmt.Errorf("opencode recycle permissions: marshal: %w", err)
	}
	// setSpecific also gives the rule map a deterministic last-entry encoder.
	// Compare bytes after marshaling so an already-correct file stays untouched,
	// while a semantically identical but incorrectly ordered rule is repaired.
	if !changed && bytes.Equal(bytes.TrimSpace(raw), body) {
		return false, nil
	}
	return writeConfig(configPath, append(body, '\n'))
}

func writeConfig(configPath string, body []byte) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return false, fmt.Errorf("opencode recycle permissions: create config dir: %w", err)
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(configPath); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("opencode recycle permissions: stat %q: %w", configPath, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(configPath), "opencode-flotilla-*.tmp")
	if err != nil {
		return false, fmt.Errorf("opencode recycle permissions: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return false, fmt.Errorf("opencode recycle permissions: write temp: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return false, fmt.Errorf("opencode recycle permissions: chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("opencode recycle permissions: close temp: %w", err)
	}
	if err := os.Rename(tmpName, configPath); err != nil {
		return false, fmt.Errorf("opencode recycle permissions: finalize %q: %w", configPath, err)
	}
	return true, nil
}

// seedJSONC uses hujson's RFC 6902 patcher so comments outside the two managed
// action maps survive. Replacing those maps is deliberate: it converts scalar
// policies to an explicit "*" fallback and emits the narrow allow last.
func seedJSONC(raw []byte, cwd string) ([]byte, error) {
	ast, err := hujson.Parse(raw)
	if err != nil {
		return nil, err
	}
	standard := ast.Clone()
	standard.Standardize()
	var doc map[string]any
	if err := json.Unmarshal(standard.Pack(), &doc); err != nil {
		return nil, err
	}
	permission, err := permissionObject(doc["permission"])
	if err != nil {
		return nil, err
	}
	cleanupGlob := filepath.ToSlash(filepath.Join(filepath.Clean(cwd), ".flotilla", "handoffs", "recycle-*.md"))
	cleanup := `rm -f "` + cleanupGlob + `"`
	legacy := "rm -f " + cleanupGlob
	// A scalar permission cannot contain per-action rules, so replacing it is
	// unavoidable. For an object, patch only the two managed members below so
	// comments on unrelated permission actions and rules survive.
	if _, ok := doc["permission"].(map[string]any); !ok {
		if bash, ok := permission["bash"].(map[string]any); ok {
			delete(bash, legacy)
		}
		if _, err := setSpecific(permission, "edit", handoffEditPattern, "allow"); err != nil {
			return nil, err
		}
		if _, err := setSpecific(permission, "bash", cleanup, "allow"); err != nil {
			return nil, err
		}
		op := "add"
		if ast.Find("/permission") != nil {
			op = "replace"
		}
		if err := applyJSONPatch(&ast, op, "/permission", permission); err != nil {
			return nil, err
		}
		return ast.Pack(), nil
	}
	for _, rule := range []struct {
		kind, pattern, legacy string
	}{
		{kind: "edit", pattern: handoffEditPattern},
		{kind: "bash", pattern: cleanup, legacy: legacy},
	} {
		if err := patchJSONCAction(&ast, permission, rule.kind, rule.pattern, rule.legacy); err != nil {
			return nil, err
		}
	}
	return ast.Pack(), nil
}

func patchJSONCAction(ast *hujson.Value, permission map[string]any, kind, pattern, legacy string) error {
	current := permission[kind]
	rules := map[string]any{}
	switch value := current.(type) {
	case string:
		rules["*"] = value
	case map[string]any:
		// Preserve the existing object and its comments. Removing then adding the
		// managed member makes it LAST, matching OpenCode 1.3.15 evaluation.
		if legacy != "" {
			if _, exists := value[legacy]; exists {
				if err := applyJSONRemove(ast, "/permission/"+kind+"/"+jsonPointer(legacy)); err != nil {
					return err
				}
			}
		}
		if _, exists := value[pattern]; exists {
			if err := applyJSONRemove(ast, "/permission/"+kind+"/"+jsonPointer(pattern)); err != nil {
				return err
			}
		}
		return applyJSONPatch(ast, "add", "/permission/"+kind+"/"+jsonPointer(pattern), "allow")
	case nil:
		if fallback, ok := permission["*"].(string); ok {
			rules["*"] = fallback
		} else {
			rules["*"] = "ask"
		}
	default:
		return fmt.Errorf("opencode recycle permissions: permission.%s has unsupported type %T", kind, current)
	}
	rules[pattern] = "allow"
	op := "add"
	if ast.Find("/permission/"+kind) != nil {
		op = "replace"
	}
	return applyJSONPatch(ast, op, "/permission/"+kind, orderedRuleMap{rules: rules, last: pattern})
}

func jsonPointer(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}

func applyJSONRemove(ast *hujson.Value, path string) error {
	patch, err := json.Marshal([]map[string]any{{"op": "remove", "path": path}})
	if err != nil {
		return err
	}
	return ast.Patch(patch)
}

func applyJSONPatch(ast *hujson.Value, op, path string, value any) error {
	patch, err := json.Marshal([]map[string]any{{"op": op, "path": path, "value": value}})
	if err != nil {
		return err
	}
	return ast.Patch(patch)
}

func lockPath(configPath string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(configPath)))
	return filepath.Join(os.TempDir(), fmt.Sprintf("flotilla-opencode-permissions-%x.lock", sum[:8]))
}

func permissionObject(v any) (map[string]any, error) {
	if m, ok := v.(map[string]any); ok {
		return m, nil
	}
	if action, ok := v.(string); ok {
		return map[string]any{"*": action}, nil
	}
	if v == nil {
		return map[string]any{}, nil
	}
	return nil, fmt.Errorf("opencode recycle permissions: permission has unsupported type %T", v)
}

func setSpecific(permission map[string]any, kind, pattern, action string) (bool, error) {
	rules := map[string]any{}
	switch current := permission[kind].(type) {
	case string:
		rules["*"] = current
	case map[string]any:
		for k, v := range current {
			rules[k] = v
		}
	case nil:
		if fallback, ok := permission["*"].(string); ok {
			rules["*"] = fallback
		} else {
			rules["*"] = "ask"
		}
	default:
		return false, fmt.Errorf("opencode recycle permissions: permission.%s has unsupported type %T", kind, current)
	}
	unchanged := rules[pattern] == action
	rules[pattern] = action
	// OpenCode 1.3.15 evaluates the LAST matching rule. encoding/json sorts map
	// keys, which can otherwise place an existing broad rule after this narrow
	// allow. Marshal the managed rule last, independent of key spelling.
	permission[kind] = orderedRuleMap{rules: rules, last: pattern}
	return !unchanged, nil
}

type orderedRuleMap struct {
	rules map[string]any
	last  string
}

func (m orderedRuleMap) MarshalJSON() ([]byte, error) {
	keys := make([]string, 0, len(m.rules))
	for key := range m.rules {
		if key != m.last {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if _, ok := m.rules[m.last]; ok {
		keys = append(keys, m.last)
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			out.WriteByte(',')
		}
		encodedKey, _ := json.Marshal(key)
		encodedValue, err := json.Marshal(m.rules[key])
		if err != nil {
			return nil, err
		}
		out.Write(encodedKey)
		out.WriteByte(':')
		out.Write(encodedValue)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

func acquireLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("opencode recycle permissions: create lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opencode recycle permissions: open lock: %w", err)
	}
	deadline := time.Now().Add(lockTimeout)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
		}
		if err != syscall.EWOULDBLOCK || time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("opencode recycle permissions: lock %q: %w", path, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
