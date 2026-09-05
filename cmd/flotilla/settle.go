package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/sessionmirror"
)

const defaultSettleRef = "refs/heads/backup/main-rolling"

type settlePlan struct {
	Actor      string
	Reason     string
	RosterPath string
	Remote     string
	Ref        string
	Files      []string
}

type settleAudit struct {
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Reason string    `json:"reason"`
	SHA    string    `json:"sha"`
	Remote string    `json:"remote"`
	Ref    string    `json:"ref"`
}

type settleOps struct {
	readFile func(string) ([]byte, error)
	git      func(...string) (string, error)
	now      func() time.Time
	audit    func(string, settleAudit) error
	touch    func(string) error
}

func cmdSettle(args []string) error {
	fs := flag.NewFlagSet("settle", flag.ContinueOnError)
	actor := fs.String("from", os.Getenv("FLOTILLA_SELF"), "seat recording settlement")
	reason := fs.String("reason", "backlog drained", "human-readable settlement reason")
	rosterPath := fs.String("roster", rosterDefault(), "roster path (sets the state directory)")
	remote := fs.String("remote", "origin", "backup git remote")
	ref := fs.String("ref", defaultSettleRef, "backup ref")
	var files stringListFlag
	fs.Var(&files, "file", "additional result file to stage (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: flotilla settle --from <seat> [--file <path>] [--reason <text>]")
	}
	plan := settlePlan{
		Actor: strings.TrimSpace(*actor), Reason: strings.TrimSpace(*reason),
		RosterPath: *rosterPath, Remote: strings.TrimSpace(*remote),
		Ref: strings.TrimSpace(*ref), Files: append([]string(nil), files...),
	}
	return runSettle(plan, realSettleOps(settleGitRoot(plan.RosterPath)))
}

func settleGitRoot(rosterPath string) string {
	rosterDir := filepath.Dir(rosterPath)
	if filepath.Base(rosterDir) == "state" {
		return filepath.Dir(rosterDir)
	}
	return rosterDir
}

func runSettle(plan settlePlan, ops settleOps) error {
	if err := sessionmirror.ValidateAgentName(plan.Actor); err != nil {
		return fmt.Errorf("settle: invalid --from: %w", err)
	}
	if plan.Reason == "" || plan.Remote == "" || plan.Ref == "" {
		return errors.New("settle: --reason, --remote, and --ref must be non-empty")
	}
	stateDir := filepath.Dir(plan.RosterPath)
	backlogPath := filepath.Join(stateDir, "flotilla-"+plan.Actor+"-backlog.md")
	raw, err := ops.readFile(backlogPath)
	if err != nil {
		return fmt.Errorf("settle: read required backlog %s: %w", backlogPath, err)
	}
	status := backlog.Parse(string(raw))
	if !status.Found || status.Malformed > 0 {
		return fmt.Errorf("settle: backlog %s is not structurally valid (found=%v malformed=%d)", backlogPath, status.Found, status.Malformed)
	}
	if len(status.Unblocked) != 0 {
		return fmt.Errorf("settle: backlog is not drained: %d actionable item(s)", len(status.Unblocked))
	}

	gitRoot := settleGitRoot(plan.RosterPath)
	files := append([]string{backlogPath}, plan.Files...)
	for i, path := range files {
		if strings.TrimSpace(path) == "" {
			return errors.New("settle: --file path must be non-empty")
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(gitRoot, path)
		}
		files[i] = filepath.Clean(path)
		if _, err := ops.readFile(path); err != nil {
			return fmt.Errorf("settle: read staged file %s: %w", path, err)
		}
	}
	if _, err := ops.git(append([]string{"add", "--"}, files...)...); err != nil {
		return fmt.Errorf("settle: stage: %w", err)
	}
	staged, err := ops.git("diff", "--cached", "--name-only")
	if err != nil {
		return fmt.Errorf("settle: inspect staged files: %w", err)
	}
	stagedFiles := nonEmptyLines(staged)
	intendedFiles, err := settleIndexNames(gitRoot, files)
	if err != nil {
		return err
	}
	if extras := valuesOutsideSet(stagedFiles, intendedFiles); len(extras) != 0 {
		return fmt.Errorf("settle: refusing commit: staged files %q are outside the intended settle allowlist %q", extras, intendedFiles)
	}
	committed := len(stagedFiles) != 0
	if committed {
		// Re-read the index at the commit edge. An unrelated actor can stage a
		// file after the first allowlist check; committing that file would make
		// the captured SHA wider than the declared settle set.
		staged, err = ops.git("diff", "--cached", "--name-only")
		if err != nil {
			return fmt.Errorf("settle: recheck staged files before commit: %w", err)
		}
		if extras := valuesOutsideSet(nonEmptyLines(staged), intendedFiles); len(extras) != 0 {
			return fmt.Errorf("settle: refusing commit: staged files %q appeared outside the intended settle allowlist %q", extras, intendedFiles)
		}
		if _, commitErr := ops.git("commit", "-m", "settle("+plan.Actor+"): "+plan.Reason); commitErr != nil {
			return fmt.Errorf("settle: commit: %w", commitErr)
		}
	}
	sha, err := ops.git("rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("settle: capture HEAD: %w", err)
	}
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return errors.New("settle: captured empty HEAD")
	}
	remoteLine, err := ops.git("ls-remote", "--exit-code", plan.Remote, plan.Ref)
	if err != nil {
		return fmt.Errorf("settle: read remote tip for delta proof: %w", err)
	}
	remoteFields := strings.Fields(remoteLine)
	if len(remoteFields) < 2 || remoteFields[0] == "" {
		return fmt.Errorf("settle: remote delta proof returned malformed tip %q", strings.TrimSpace(remoteLine))
	}
	remoteTip := remoteFields[0]
	if _, err := ops.git("merge-base", "--is-ancestor", remoteTip, sha); err != nil {
		return fmt.Errorf("settle: remote tip %s is not an ancestor of captured %s: %w", remoteTip, sha, err)
	}
	deltaRaw, err := ops.git("rev-list", "--count", remoteTip+".."+sha)
	if err != nil {
		return fmt.Errorf("settle: count per-push delta: %w", err)
	}
	delta, err := strconv.Atoi(strings.TrimSpace(deltaRaw))
	if err != nil || delta < 0 {
		return fmt.Errorf("settle: refusing push: captured delta %q is not a non-negative commit count", strings.TrimSpace(deltaRaw))
	}
	if (!committed && delta != 0) || (committed && delta == 0) {
		return fmt.Errorf("settle: refusing push: captured delta is %d commit(s), inconsistent with committed=%t", delta, committed)
	}
	if _, err := ops.git("push", plan.Remote, sha+":"+plan.Ref); err != nil {
		return fmt.Errorf("settle: push captured %s: %w", sha, err)
	}
	if _, err := ops.git("fetch", "--quiet", plan.Remote, plan.Ref); err != nil {
		return fmt.Errorf("settle: fetch backup ref: %w", err)
	}
	if _, err := ops.git("merge-base", "--is-ancestor", sha, "FETCH_HEAD"); err != nil {
		return fmt.Errorf("settle: backup does not contain captured %s: %w", sha, err)
	}
	if err := ops.audit(plan.Actor, settleAudit{
		At: ops.now().UTC(), Actor: plan.Actor, Reason: plan.Reason, SHA: sha,
		Remote: plan.Remote, Ref: plan.Ref,
	}); err != nil {
		return fmt.Errorf("settle: append audit: %w", err)
	}
	for _, suffix := range []string{"alive", "settled"} {
		marker := filepath.Join(stateDir, "flotilla-"+plan.Actor+"-"+suffix)
		if err := ops.touch(marker); err != nil {
			return fmt.Errorf("settle: touch %s: %w", marker, err)
		}
	}
	fmt.Printf("settled actor=%s sha=%s backup=%s/%s\n", plan.Actor, sha, plan.Remote, plan.Ref)
	return nil
}

func nonEmptyLines(raw string) []string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, filepath.ToSlash(filepath.Clean(line)))
		}
	}
	return lines
}

func settleIndexNames(gitRoot string, files []string) ([]string, error) {
	names := make([]string, 0, len(files))
	for _, path := range files {
		name := path
		if filepath.IsAbs(path) {
			var err error
			name, err = filepath.Rel(gitRoot, path)
			if err != nil {
				return nil, fmt.Errorf("settle: resolve staged file %s: %w", path, err)
			}
		}
		names = append(names, filepath.ToSlash(filepath.Clean(name)))
	}
	return names, nil
}

func valuesOutsideSet(got, allowed []string) []string {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, value := range allowed {
		allowedSet[value] = struct{}{}
	}
	var extras []string
	for _, value := range got {
		if _, ok := allowedSet[value]; !ok {
			extras = append(extras, value)
		}
	}
	return extras
}

func realSettleOps(gitRoot string) settleOps {
	return settleOps{
		readFile: os.ReadFile,
		git: func(args ...string) (string, error) {
			cmd := exec.Command("git", args...)
			cmd.Dir = gitRoot
			out, err := cmd.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
			}
			return string(out), nil
		},
		now:   time.Now,
		audit: appendSettleAudit,
		touch: func(path string) error {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			return f.Close()
		},
	}
}

func appendSettleAudit(actor string, event settleAudit) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".flotilla", actor)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "settle-log.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(event)
}

type stringListFlag []string

func (s *stringListFlag) String() string { return strings.Join(*s, ",") }
func (s *stringListFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}
