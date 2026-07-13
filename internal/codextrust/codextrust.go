// Package codextrust pre-seeds launch-gate configuration for OpenAI's Codex CLI
// so centrally managed desks do not wedge on interactive trust or update menus.
//
// Codex records trust per absolute path in $CODEX_HOME/config.toml (default
// ~/.codex/config.toml):
//
//	[projects."/abs/path"]
//	trust_level = "trusted"
//
// PROVENANCE (SOURCE-VERIFIED openai/codex rust-v0.144.1): the trust check
// (config/src/loader/mod.rs, ProjectTrustContext.decision_for_dir) looks up the
// working directory's own key FIRST, before the checkout root and the main-repo
// root — so seeding the desk cwd itself satisfies the check for plain checkouts
// AND for linked git worktrees (the class that wedged: a worktree whose main
// checkout was trusted still prompted, because neither the worktree path nor its
// resolution chain was covered). Codex's own "Yes, continue" would trust the
// MAIN repo root (git-utils/src/info.rs resolve_root_git_project_for_trust);
// seeding the narrower desk cwd is deliberate least-privilege.
package codextrust

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// lock timing: mirrors internal/dispatch's file-lock posture (bounded acquire,
// never wedge a resume on a stuck lock).
const (
	lockTimeout    = 15 * time.Second
	lockPoll       = 25 * time.Millisecond
	updateCheckKey = "check_for_update_on_startup"
)

// ConfigPath returns the codex user config path: $CODEX_HOME/config.toml when
// CODEX_HOME is set (codex-cli honors it), else ~/.codex/config.toml.
func ConfigPath() (string, error) {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return filepath.Join(h, "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("codextrust: resolve home for ~/.codex: %w", err)
	}
	return filepath.Join(home, ".codex", "config.toml"), nil
}

// SuppressStartupUpdateCheck ensures Codex does not surface its interactive
// self-update prompt. Version upgrades remain centrally managed by fleet ops.
// The managed key is top-level TOML, so a missing key is PREPENDED before any
// project table; appending it after [projects."..."] would silently put it in
// that table and leave the global default enabled.
//
// PROVENANCE (SOURCE-VERIFIED official openai/codex rust-v0.144.1, peeled
// commit 44918ea10c0f99151c6710411b4322c2f5c96bea):
//   - codex-rs/config/src/config_toml.rs declares the optional top-level
//     check_for_update_on_startup boolean and documents false for centrally
//     managed installations.
//   - codex-rs/core/src/config/mod.rs defaults the resolved value to true.
//   - codex-rs/tui/src/updates.rs returns before lookup/popup construction when
//     the resolved value is false.
//
// LIVE-VERIFIED 2026-07-13 against the installed codex-cli 0.144.1 binary:
// CODEX_HOME with this seed made `codex doctor --json` report
// "check for update on startup": "false" while a newer version was available.
//
// Writes share Seed's bounded flock and atomic rename. An explicit true is
// changed to false because this key is fleet-managed policy, unlike per-project
// trust where an explicit untrusted choice remains operator-owned. A malformed
// managed value fails loudly and is never guessed or rewritten.
func SuppressStartupUpdateCheck(configPath string) (bool, error) {
	unlock, err := acquireLock(configPath + ".flotilla-lock")
	if err != nil {
		return false, err
	}
	defer unlock()

	raw, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("codextrust: read %q: %w", configPath, err)
	}
	updated, changed, err := suppressStartupUpdateCheck(string(raw))
	if err != nil {
		return false, fmt.Errorf("codextrust: %s in %q: %w", updateCheckKey, configPath, err)
	}
	if !changed {
		return false, nil
	}
	if err := writeAtomic(configPath, []byte(updated)); err != nil {
		return false, err
	}
	return true, nil
}

func suppressStartupUpdateCheck(content string) (string, bool, error) {
	lines := strings.SplitAfter(content, "\n")
	match := -1
	value := ""
	for i, rawLine := range lines {
		line := strings.TrimSuffix(strings.TrimSuffix(rawLine, "\n"), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			break
		}
		lhs, rhs, ok := strings.Cut(trimmed, "=")
		if !ok || tomlKey(strings.TrimSpace(lhs)) != updateCheckKey {
			continue
		}
		if match >= 0 {
			return "", false, fmt.Errorf("duplicate top-level key")
		}
		match = i
		value, _, _ = strings.Cut(rhs, "#")
		value = strings.TrimSpace(value)
		if value != "true" && value != "false" {
			return "", false, fmt.Errorf("must be a boolean, got %q", value)
		}
	}

	if match < 0 {
		prefix := updateCheckKey + " = false\n"
		if content != "" {
			prefix += "\n"
		}
		return prefix + content, true, nil
	}
	if value == "false" {
		return content, false, nil
	}

	original := lines[match]
	newline := ""
	if strings.HasSuffix(original, "\n") {
		newline = "\n"
		original = strings.TrimSuffix(original, "\n")
	}
	comment := ""
	if pos := strings.IndexByte(original, '#'); pos >= 0 {
		comment = strings.TrimSpace(original[pos:])
	}
	replacement := updateCheckKey + " = false"
	if comment != "" {
		replacement += " " + comment
	}
	lines[match] = replacement + newline
	return strings.Join(lines, ""), true, nil
}

func tomlKey(raw string) string {
	if raw == updateCheckKey {
		return raw
	}
	key, remainder, ok := cutQuoted(raw)
	if ok && strings.TrimSpace(remainder) == "" {
		return key
	}
	return raw
}

// Seed ensures configPath carries a [projects."<cwd>"] section, appending
// `trust_level = "trusted"` when the path has no section yet. It returns
// seeded=true only when it wrote the new section.
//
// A section that ALREADY exists for the path — whatever its trust_level — is
// left untouched: an explicit `untrusted` is the operator's call and is never
// flipped (seeding must not escalate past a human decision). The file (and its
// parent directory) is created when missing: a host where codex has never run
// still gets the seed, so the trust menu is skipped once login completes.
//
// Writes are append-only (never rewrites existing content), atomic
// (temp+rename), and serialized against concurrent seeders via a sibling
// .lock flock — a duplicate [projects."…"] table would be a TOML redefinition
// error that breaks codex config loading for EVERY desk, so the race is closed,
// not tolerated. Known bound: codex's OWN config writes (a human answering a
// trust prompt elsewhere) do not take this lock, so a last-rename-wins overlap
// can drop one side's entry — idempotent re-seed at the next launch recovers it.
func Seed(configPath, cwd string) (bool, error) {
	if !filepath.IsAbs(cwd) {
		return false, fmt.Errorf("codextrust: cwd %q is not absolute", cwd)
	}
	if strings.ContainsAny(cwd, "\x00\n\r\t") {
		return false, fmt.Errorf("codextrust: cwd %q contains control characters", cwd)
	}
	want := filepath.Clean(cwd)

	unlock, err := acquireLock(configPath + ".flotilla-lock")
	if err != nil {
		return false, err
	}
	defer unlock()

	raw, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("codextrust: read %q: %w", configPath, err)
	}
	if hasProjectSection(string(raw), want) {
		return false, nil
	}

	section := fmt.Sprintf("[projects.%s]\ntrust_level = \"trusted\"\n", quoteTOML(want))
	var b strings.Builder
	b.Write(raw)
	if len(raw) > 0 {
		if raw[len(raw)-1] != '\n' {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	b.WriteString(section)

	if err := writeAtomic(configPath, []byte(b.String())); err != nil {
		return false, err
	}
	return true, nil
}

// hasProjectSection reports whether the TOML content already declares a
// [projects."<path>"] table (basic- or literal-quoted, tolerant of interior
// whitespace) whose path equals want after cleaning. It is a targeted scan, not
// a full TOML parser: flotilla only ever APPENDS canonical sections, and an
// existing section in any quoting style must be recognized so it is never
// duplicated (a redefined table is a codex config load error).
func hasProjectSection(content, want string) bool {
	for _, line := range strings.Split(content, "\n") {
		path, ok := projectSectionPath(line)
		if ok && filepath.Clean(path) == want {
			return true
		}
	}
	return false
}

// projectSectionPath extracts the quoted path from a `[projects."<path>"]`
// header line, tolerating interior whitespace and a trailing `# comment` (TOML
// allows one after a table header — and it must be handled STRUCTURALLY, after
// the quoted string is consumed, because '#' is also legal inside a quoted
// path). Lines that are not project headers (other tables, key/values,
// comments, deeper dotted keys like [projects."x".y]) return ok=false.
func projectSectionPath(line string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "[")
	if !ok {
		return "", false
	}
	rest, ok = strings.CutPrefix(strings.TrimSpace(rest), "projects")
	if !ok {
		return "", false
	}
	rest, ok = strings.CutPrefix(strings.TrimSpace(rest), ".")
	if !ok {
		return "", false
	}
	path, remainder, ok := cutQuoted(strings.TrimSpace(rest))
	if !ok {
		return "", false
	}
	remainder, ok = strings.CutPrefix(strings.TrimSpace(remainder), "]")
	if !ok {
		return "", false
	}
	remainder = strings.TrimSpace(remainder)
	if remainder != "" && !strings.HasPrefix(remainder, "#") {
		return "", false
	}
	return path, true
}

// cutQuoted consumes one leading TOML string (basic "…" with \\ and \" escapes,
// or literal '…') and returns its unescaped value plus the remainder.
func cutQuoted(s string) (value, remainder string, ok bool) {
	if s == "" {
		return "", "", false
	}
	switch s[0] {
	case '\'':
		end := strings.IndexByte(s[1:], '\'')
		if end < 0 {
			return "", "", false
		}
		return s[1 : 1+end], s[2+end:], true
	case '"':
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			switch s[i] {
			case '\\':
				if i+1 >= len(s) {
					return "", "", false
				}
				i++
				switch s[i] {
				case '\\':
					b.WriteByte('\\')
				case '"':
					b.WriteByte('"')
				default:
					// Other escapes (\t, \uXXXX, …) cannot appear in the path keys
					// flotilla manages (control characters are rejected at Seed);
					// treat an unrecognized escape as a non-matching header rather
					// than mis-decoding it.
					return "", "", false
				}
			case '"':
				return b.String(), s[i+1:], true
			default:
				b.WriteByte(s[i])
			}
		}
		return "", "", false
	default:
		return "", "", false
	}
}

// quoteTOML renders a path as a TOML basic string (the form codex itself
// writes), escaping backslashes and double quotes.
func quoteTOML(path string) string {
	escaped := strings.ReplaceAll(path, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// writeAtomic writes data to path via a same-directory temp file + rename,
// creating the parent directory when missing (0700 — the codex home holds
// auth material; never widen it).
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("codextrust: create %q: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("codextrust: create temp for %q: %w", path, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("codextrust: write %q: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("codextrust: close temp for %q: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("codextrust: finalize %q: %w", path, err)
	}
	return nil
}

// acquireLock takes a bounded exclusive flock on lockPath and returns the
// release func. Mirrors internal/dispatch's acquireFileLock (kept local: that
// helper is package-private and dispatch-scoped).
func acquireLock(lockPath string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, fmt.Errorf("codextrust: create lock dir for %q: %w", lockPath, err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("codextrust: open lock %q: %w", lockPath, err)
	}
	deadline := time.Now().Add(lockTimeout)
	for {
		switch err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err {
		case nil:
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		case syscall.EWOULDBLOCK:
			if time.Now().After(deadline) {
				f.Close()
				return nil, fmt.Errorf("codextrust: lock %q busy: timed out after %s", lockPath, lockTimeout)
			}
			time.Sleep(lockPoll)
		default:
			f.Close()
			return nil, fmt.Errorf("codextrust: flock %q: %w", lockPath, err)
		}
	}
}
