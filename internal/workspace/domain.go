package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GatekeeperDomainRel is the worktree-relative path of the authority-domain file
// consumed by the merge-domain gatekeeper precondition (Track A hook contract).
// flotilla materializes this file; it does NOT implement the hook itself.
const GatekeeperDomainRel = ".gatekeeper/domain"

// ParseGitRemoteOwnerName extracts owner/name from a git remote URL.
// Supports https://host/owner/name(.git), git@host:owner/name(.git), and
// ssh://git@host/owner/name(.git). Returns an error when the URL cannot be
// reduced to exactly two path segments.
func ParseGitRemoteOwnerName(remoteURL string) (string, error) {
	s := strings.TrimSpace(remoteURL)
	if s == "" {
		return "", fmt.Errorf("empty git remote URL")
	}
	// git@host:owner/name(.git)
	if strings.HasPrefix(s, "git@") {
		// git@github.com:acme/flotilla.git
		colon := strings.Index(s, ":")
		if colon < 0 || colon == len(s)-1 {
			return "", fmt.Errorf("ssh remote %q missing path after host", s)
		}
		return normalizeOwnerNamePath(s[colon+1:])
	}
	// strip scheme for https://, http://, ssh://, git://
	rest := s
	if i := strings.Index(s, "://"); i >= 0 {
		rest = s[i+3:]
	}
	// drop userinfo (git@host/...)
	if at := strings.Index(rest, "@"); at >= 0 {
		rest = rest[at+1:]
	}
	// host/owner/name — drop host
	slash := strings.Index(rest, "/")
	if slash < 0 || slash == len(rest)-1 {
		return "", fmt.Errorf("remote URL %q has no owner/name path", s)
	}
	return normalizeOwnerNamePath(rest[slash+1:])
}

func normalizeOwnerNamePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")
	if path == "" {
		return "", fmt.Errorf("empty owner/name path")
	}
	// Drop query/fragment if any leaked through.
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("remote path %q is not exactly owner/name", path)
	}
	owner, name := parts[0], parts[1]
	if strings.Contains(owner, "..") || strings.Contains(name, "..") {
		return "", fmt.Errorf("remote path %q contains path traversal", path)
	}
	return owner + "/" + name, nil
}

// DomainFileContent builds the .gatekeeper/domain body: primary on line 1,
// then each secondary on its own line. primary must be non-empty.
func DomainFileContent(primary string, secondaries []string) string {
	var b strings.Builder
	b.WriteString(primary)
	b.WriteByte('\n')
	for _, s := range secondaries {
		s = strings.TrimSpace(s)
		if s == "" || s == primary {
			continue
		}
		b.WriteString(s)
		b.WriteByte('\n')
	}
	return b.String()
}

// ResolveDomainPrimary picks line-1 owner/name: roster primary_repo when set,
// otherwise origin remote URL parsed to owner/name. ok=false when neither yields
// a domain (caller may soft-skip materialization).
func ResolveDomainPrimary(primaryRepo, originURL string) (primary string, ok bool) {
	if p := strings.TrimSpace(primaryRepo); p != "" {
		return p, true
	}
	if originURL == "" {
		return "", false
	}
	p, err := ParseGitRemoteOwnerName(originURL)
	if err != nil {
		return "", false
	}
	return p, true
}

// MaterializeGatekeeperDomain writes worktreeAbs/.gatekeeper/domain (mode 0644)
// from roster primary_repo (preferred) or git origin, plus secondary_repos lines.
// Idempotent: leaves an identical file untouched. When no primary can be resolved
// (empty primary_repo and unparseable/missing origin), it is a no-op (nil error)
// so desks without remotes still init; the hook treats missing domain as NODOMAIN.
//
// This consumes the Track A hook contract — it does not implement or duplicate
// the merge-domain precondition hook.
func MaterializeGatekeeperDomain(worktreeAbs, primaryRepo string, secondaryRepos []string) error {
	if worktreeAbs == "" {
		return fmt.Errorf("materialize .gatekeeper/domain: empty worktree path")
	}
	if !filepath.IsAbs(worktreeAbs) {
		return fmt.Errorf("materialize .gatekeeper/domain: worktree %q is not absolute", worktreeAbs)
	}
	originURL, _ := originRemoteURL(worktreeAbs)
	primary, ok := ResolveDomainPrimary(primaryRepo, originURL)
	if !ok {
		return nil
	}
	content := DomainFileContent(primary, secondaryRepos)
	dir := filepath.Join(worktreeAbs, ".gatekeeper")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %q: %w", dir, err)
	}
	path := filepath.Join(worktreeAbs, GatekeeperDomainRel)
	if prev, err := os.ReadFile(path); err == nil && string(prev) == content {
		return nil // idempotent: unchanged
	}
	// Write via temp + rename so concurrent readers never see a partial file.
	tmp, err := os.CreateTemp(dir, "domain-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp domain file: %w", err)
	}
	tmpName := tmp.Name()
	// Ensure cleanup on any failure path before rename.
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp domain file: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp domain file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp domain file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install %q: %w", path, err)
	}
	return nil
}

func originRemoteURL(worktreeAbs string) (string, error) {
	out, err := runGitOutput(worktreeAbs, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
