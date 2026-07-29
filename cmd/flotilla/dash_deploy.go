package main

import (
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const dashDeployManifestSchema = "flotilla.dash_deploy/v1"

type dashDeployOptions struct {
	Repo       string
	StageDir   string
	UnitFile   string
	InstallBin string
	Service    string
	LiveURL    string
	Apply      bool
	Timeout    time.Duration
	// UnitState is test-only input for exercising staging without a user
	// systemd manager. Production callers always resolve the effective unit.
	UnitState *dashEffectiveUnit
}

type dashDeployBinary struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Revision string `json:"revision"`
	Modified *bool  `json:"modified"`
}

type dashDeployUnitFile struct {
	Path     string `json:"path"`
	Snapshot string `json:"snapshot"`
	SHA256   string `json:"sha256"`
}

type dashDeployUnit struct {
	SHA256             string               `json:"sha256"`
	EffectiveExecStart string               `json:"effective_exec_start"`
	TrackerFile        string               `json:"tracker_file"`
	BacklogFile        string               `json:"backlog_file"`
	Files              []dashDeployUnitFile `json:"files"`
}

type dashEffectiveUnit struct {
	FragmentPath string
	DropInPaths  []string
	ExecStart    string
}

type dashDeployManifest struct {
	Schema      string           `json:"schema"`
	Status      string           `json:"status"`
	CreatedAt   string           `json:"created_at"`
	TipRevision string           `json:"tip_revision"`
	Candidate   dashDeployBinary `json:"candidate"`
	Previous    dashDeployBinary `json:"previous"`
	Unit        dashDeployUnit   `json:"unit"`
	FeatureGate []string         `json:"feature_gate"`
}

type dashResearchIndex struct {
	Research []struct {
		ID             string `json:"id"`
		Decision       bool   `json:"decision"`
		LearnReady     bool   `json:"learn_ready"`
		Classification struct {
			Classification string `json:"classification"`
		} `json:"publication"`
	} `json:"research"`
}

func cmdDashDeploy(args []string) error {
	fs := flag.NewFlagSet("dash deploy", flag.ContinueOnError)
	repo := fs.String("repo", ".", "clean checkout whose HEAD must equal freshly fetched origin/main")
	stageDir := fs.String("stage-dir", "", "new directory outside the repository for candidate and rollback evidence (required)")
	unitFile := fs.String("unit-file", "", "loaded base flotilla-dash.service fragment; effective base+drop-ins are validated and snapshotted (required)")
	installBin := fs.String("install-bin", "", "installed flotilla binary to snapshot and optionally replace (required)")
	service := fs.String("service", "flotilla-dash.service", "systemd user service restarted only with --apply")
	liveURL := fs.String("live-url", "http://127.0.0.1:8787", "installed dash URL verified only with --apply")
	apply := fs.Bool("apply", false, "atomically replace the installed binary and restart dash after every preflight passes")
	timeout := fs.Duration("timeout", 2*time.Minute, "overall build and smoke timeout")
	if err := fs.Parse(args); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("dash deploy: unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	opts := dashDeployOptions{
		Repo:       *repo,
		StageDir:   *stageDir,
		UnitFile:   *unitFile,
		InstallBin: *installBin,
		Service:    *service,
		LiveURL:    *liveURL,
		Apply:      *apply,
		Timeout:    *timeout,
	}
	manifest, err := runDashDeploy(opts)
	if err != nil {
		return err
	}
	fmt.Printf("dash deploy: %s candidate %s\n", manifest.Status, manifest.TipRevision)
	fmt.Printf("candidate: %s\n", manifest.Candidate.Path)
	fmt.Printf("rollback manifest: %s\n", filepath.Join(opts.StageDir, "manifest.json"))
	if !opts.Apply {
		fmt.Println("stage-only: installed binary and service were not changed (pass --apply only after review clearance)")
	}
	return nil
}

func runDashDeploy(opts dashDeployOptions) (dashDeployManifest, error) {
	if strings.TrimSpace(opts.StageDir) == "" || strings.TrimSpace(opts.UnitFile) == "" || strings.TrimSpace(opts.InstallBin) == "" {
		return dashDeployManifest{}, errors.New("dash deploy: --stage-dir, --unit-file, and --install-bin are required")
	}
	if opts.Timeout <= 0 {
		return dashDeployManifest{}, errors.New("dash deploy: --timeout must be positive")
	}
	repo, err := filepath.Abs(opts.Repo)
	if err != nil {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: resolve repo: %w", err)
	}
	stageDir, err := filepath.Abs(opts.StageDir)
	if err != nil {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: resolve stage dir: %w", err)
	}
	opts.Repo, opts.StageDir = repo, stageDir
	if pathWithin(stageDir, repo) {
		return dashDeployManifest{}, errors.New("dash deploy: stage directory must be outside the source repository")
	}
	if entries, readErr := os.ReadDir(stageDir); readErr == nil && len(entries) > 0 {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: stage directory is not empty: %s", stageDir)
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: inspect stage directory: %w", readErr)
	}
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: create stage directory: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	tip, err := cleanOriginMainTip(ctx, repo)
	if err != nil {
		return dashDeployManifest{}, err
	}

	sourceRoot, err := os.MkdirTemp("", "flotilla-dash-source-")
	if err != nil {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: create temporary source root: %w", err)
	}
	defer os.RemoveAll(sourceRoot)
	source := filepath.Join(sourceRoot, "source")
	originURL, err := commandOutput(ctx, repo, "git", "remote", "get-url", "origin")
	if err != nil {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: resolve origin URL: %w", err)
	}
	originURL = strings.TrimSpace(originURL)
	if _, err := commandOutput(ctx, "", "git", "clone", "--quiet", "--single-branch", "--branch", "main", "--no-tags", originURL, source); err != nil {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: clone fresh origin/main: %w", err)
	}
	clonedHead, err := commandOutput(ctx, source, "git", "rev-parse", "HEAD")
	if err != nil {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: resolve cloned HEAD: %w", err)
	}
	if strings.TrimSpace(clonedHead) != tip {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: cloned origin/main %s does not match fetched tip %s", strings.TrimSpace(clonedHead), tip)
	}
	if dirty, err := commandOutput(ctx, source, "git", "status", "--porcelain", "--untracked-files=all"); err != nil {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: inspect fresh clone: %w", err)
	} else if strings.TrimSpace(dirty) != "" {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: fresh source clone is dirty: %s", strings.TrimSpace(dirty))
	}

	candidatePath := filepath.Join(stageDir, "flotilla-candidate")
	if _, err := commandOutput(ctx, source, "go", "build", "-buildvcs=true", "-trimpath", "-o", candidatePath, "./cmd/flotilla"); err != nil {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: build candidate: %w", err)
	}
	candidate, err := inspectDeployBinary(candidatePath)
	if err != nil {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: inspect candidate: %w", err)
	}
	if err := validateCandidateProvenance(candidate, tip); err != nil {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: %w", err)
	}

	unitState := opts.UnitState
	if unitState == nil {
		resolved, resolveErr := effectiveDashUnit(ctx, opts.Service, opts.UnitFile)
		if resolveErr != nil {
			return dashDeployManifest{}, resolveErr
		}
		unitState = &resolved
	}
	unit, err := snapshotDashUnit(*unitState, stageDir)
	if err != nil {
		return dashDeployManifest{}, err
	}
	previous, err := snapshotInstalledBinary(opts.InstallBin, stageDir)
	if err != nil {
		return dashDeployManifest{}, err
	}

	if err := smokeStagedDash(ctx, candidatePath, source, stageDir, tip); err != nil {
		return dashDeployManifest{}, fmt.Errorf("dash deploy: staged HTTP/R&D smoke: %w", err)
	}
	manifest := dashDeployManifest{
		Schema:      dashDeployManifestSchema,
		Status:      "staged",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		TipRevision: tip,
		Candidate:   candidate,
		Previous:    previous,
		Unit:        unit,
		FeatureGate: []string{"root_revision", "research_page", "decide_entry", "learn_showpiece"},
	}
	if err := writeDashDeployManifest(stageDir, manifest); err != nil {
		return dashDeployManifest{}, err
	}
	if !opts.Apply {
		return manifest, nil
	}
	if err := applyDashCandidate(ctx, opts, &manifest); err != nil {
		return dashDeployManifest{}, err
	}
	if err := writeDashDeployManifest(stageDir, manifest); err != nil {
		return dashDeployManifest{}, err
	}
	return manifest, nil
}

func cleanOriginMainTip(ctx context.Context, repo string) (string, error) {
	if dirty, err := commandOutput(ctx, repo, "git", "status", "--porcelain", "--untracked-files=all"); err != nil {
		return "", fmt.Errorf("dash deploy: inspect source checkout: %w", err)
	} else if strings.TrimSpace(dirty) != "" {
		return "", fmt.Errorf("dash deploy: source checkout is dirty; use a clean origin/main worktree: %s", strings.TrimSpace(dirty))
	}
	if _, err := commandOutput(ctx, repo, "git", "fetch", "--quiet", "origin", "main"); err != nil {
		return "", fmt.Errorf("dash deploy: fetch origin/main: %w", err)
	}
	tip, err := commandOutput(ctx, repo, "git", "rev-parse", "refs/remotes/origin/main")
	if err != nil {
		return "", fmt.Errorf("dash deploy: resolve origin/main: %w", err)
	}
	head, err := commandOutput(ctx, repo, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("dash deploy: resolve HEAD: %w", err)
	}
	tip, head = strings.TrimSpace(tip), strings.TrimSpace(head)
	if head != tip {
		return "", fmt.Errorf("dash deploy: checkout HEAD %s is not current origin/main %s", head, tip)
	}
	return tip, nil
}

func inspectDeployBinary(path string) (dashDeployBinary, error) {
	sum, err := fileSHA256(path)
	if err != nil {
		return dashDeployBinary{}, err
	}
	result := dashDeployBinary{Path: path, SHA256: sum}
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return result, err
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			result.Revision = setting.Value
		case "vcs.modified":
			modified := setting.Value == "true"
			result.Modified = &modified
		}
	}
	if result.Revision == "" || result.Modified == nil {
		return result, errors.New("binary has no complete Go VCS metadata")
	}
	return result, nil
}

func validateCandidateProvenance(candidate dashDeployBinary, tip string) error {
	if candidate.Revision != tip || candidate.Modified == nil || *candidate.Modified {
		return fmt.Errorf("candidate provenance mismatch: revision=%q modified=%v want revision=%q modified=false", candidate.Revision, boolString(candidate.Modified), tip)
	}
	return nil
}

func validateApprovedCandidate(candidate, approved dashDeployBinary, tip string) error {
	if err := validateCandidateProvenance(candidate, tip); err != nil {
		return err
	}
	if candidate.SHA256 == "" || candidate.SHA256 != approved.SHA256 {
		return fmt.Errorf("staged candidate changed after approval: sha256=%s want=%s", candidate.SHA256, approved.SHA256)
	}
	return nil
}

func effectiveDashUnit(ctx context.Context, service, wantFragment string) (dashEffectiveUnit, error) {
	fragment, err := commandOutput(ctx, "", "systemctl", "--user", "show", service, "--property=FragmentPath", "--value")
	if err != nil {
		return dashEffectiveUnit{}, fmt.Errorf("dash deploy: resolve effective unit fragment: %w", err)
	}
	fragment = strings.TrimSpace(fragment)
	wantFragment, err = filepath.Abs(wantFragment)
	if err != nil {
		return dashEffectiveUnit{}, fmt.Errorf("dash deploy: resolve --unit-file: %w", err)
	}
	if fragment == "" {
		return dashEffectiveUnit{}, fmt.Errorf("dash deploy: service %s has no loaded FragmentPath", service)
	}
	fragment, err = filepath.Abs(fragment)
	if err != nil {
		return dashEffectiveUnit{}, fmt.Errorf("dash deploy: resolve loaded unit fragment: %w", err)
	}
	if fragment != wantFragment {
		return dashEffectiveUnit{}, fmt.Errorf("dash deploy: --unit-file %s is not the loaded fragment for %s (%s)", wantFragment, service, fragment)
	}
	dropIns, err := commandOutput(ctx, "", "systemctl", "--user", "show", service, "--property=DropInPaths", "--value")
	if err != nil {
		return dashEffectiveUnit{}, fmt.Errorf("dash deploy: resolve effective unit drop-ins: %w", err)
	}
	execStart, err := commandOutput(ctx, "", "systemctl", "--user", "show", service, "--property=ExecStart", "--value")
	if err != nil {
		return dashEffectiveUnit{}, fmt.Errorf("dash deploy: resolve effective unit ExecStart: %w", err)
	}
	execStart = strings.TrimSpace(execStart)
	if execStart == "" {
		return dashEffectiveUnit{}, fmt.Errorf("dash deploy: service %s has no effective ExecStart", service)
	}
	return dashEffectiveUnit{
		FragmentPath: fragment,
		DropInPaths:  strings.Fields(strings.TrimSpace(dropIns)),
		ExecStart:    execStart,
	}, nil
}

func snapshotDashUnit(state dashEffectiveUnit, stageDir string) (dashDeployUnit, error) {
	tracker, backlog, err := dashUnitDataPaths(state.ExecStart)
	if err != nil {
		return dashDeployUnit{}, fmt.Errorf("dash deploy: validate unit flags: %w", err)
	}
	paths := append([]string{state.FragmentPath}, state.DropInPaths...)
	if len(paths) == 0 || strings.TrimSpace(state.FragmentPath) == "" {
		return dashDeployUnit{}, errors.New("dash deploy: effective unit has no base fragment")
	}
	rollbackDir := filepath.Join(stageDir, "rollback", "unit")
	if err := os.MkdirAll(rollbackDir, 0o700); err != nil {
		return dashDeployUnit{}, fmt.Errorf("dash deploy: create rollback directory: %w", err)
	}
	files := make([]dashDeployUnitFile, 0, len(paths))
	for i, path := range paths {
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return dashDeployUnit{}, fmt.Errorf("dash deploy: read effective unit file %s: %w", path, readErr)
		}
		snapshot := filepath.Join(rollbackDir, fmt.Sprintf("%02d-%s", i, filepath.Base(path)))
		if writeErr := os.WriteFile(snapshot, body, 0o600); writeErr != nil {
			return dashDeployUnit{}, fmt.Errorf("dash deploy: snapshot unit file %s: %w", path, writeErr)
		}
		sum := sha256.Sum256(body)
		files = append(files, dashDeployUnitFile{
			Path:     path,
			Snapshot: snapshot,
			SHA256:   hex.EncodeToString(sum[:]),
		})
	}
	unit := dashDeployUnit{
		EffectiveExecStart: stableEffectiveExecStart(state.ExecStart),
		TrackerFile:        tracker,
		BacklogFile:        backlog,
		Files:              files,
	}
	unit.SHA256 = effectiveUnitSHA256(unit)
	return unit, nil
}

func snapshotInstalledBinary(installPath, stageDir string) (dashDeployBinary, error) {
	rollbackPath := filepath.Join(stageDir, "rollback", "flotilla")
	if err := copyFile(installPath, rollbackPath, 0o700); err != nil {
		return dashDeployBinary{}, fmt.Errorf("dash deploy: snapshot installed binary: %w", err)
	}
	result, err := inspectDeployBinary(rollbackPath)
	if err == nil {
		result.Path = rollbackPath
		return result, nil
	}
	sum, sumErr := fileSHA256(rollbackPath)
	if sumErr != nil {
		return dashDeployBinary{}, fmt.Errorf("dash deploy: hash installed binary snapshot: %w", sumErr)
	}
	return dashDeployBinary{Path: rollbackPath, SHA256: sum, Revision: "unavailable"}, nil
}

var dashUnitFlagPattern = regexp.MustCompile(`(?:^|[[:space:]])--([a-z-]+)(?:=|[[:space:]]+)([^[:space:]\\]+)`)

func dashUnitDataPaths(unit string) (string, string, error) {
	var execStart string
	for _, line := range strings.Split(unit, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ExecStart=") {
			execStart = strings.TrimPrefix(line, "ExecStart=")
		}
	}
	if execStart == "" && strings.Contains(unit, "--") {
		// systemctl show --property=ExecStart --value returns the effective
		// command structure rather than an ExecStart= line.
		execStart = strings.TrimSpace(unit)
	}
	if execStart == "" {
		return "", "", errors.New("missing ExecStart")
	}
	values := map[string]string{}
	for _, match := range dashUnitFlagPattern.FindAllStringSubmatch(execStart, -1) {
		values[match[1]] = match[2]
	}
	tracker, backlog := values["tracker-file"], values["backlog-file"]
	if tracker == "" || backlog == "" {
		return "", "", errors.New("ExecStart must retain explicit --tracker-file and --backlog-file flags")
	}
	if tracker == backlog {
		return "", "", errors.New("--tracker-file and --backlog-file must remain distinct")
	}
	return tracker, backlog, nil
}

func smokeStagedDash(ctx context.Context, candidate, source, stageDir, revision string) error {
	smokeDir := filepath.Join(stageDir, "smoke")
	researchDir := filepath.Join(smokeDir, "research")
	if err := writeDashSmokeFixtures(smokeDir, researchDir); err != nil {
		return err
	}
	addr, err := freeLoopbackAddress()
	if err != nil {
		return err
	}
	args := []string{
		"dash",
		"--roster", filepath.Join(source, "flotilla.example.json"),
		"--bind", addr,
		"--repo", "example/flotilla",
		"--tracker-file", filepath.Join(smokeDir, "history.md"),
		"--backlog-file", filepath.Join(smokeDir, "backlog.md"),
		"--goals-file", filepath.Join(smokeDir, "goals.json"),
		"--research-dir", researchDir,
	}
	cmd := exec.CommandContext(ctx, candidate, args...)
	logPath := filepath.Join(smokeDir, "candidate.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		return err
	}
	defer stopDashSmokeProcess(cmd)
	baseURL := "http://" + addr
	if err := waitForDashHTTP(ctx, baseURL); err != nil {
		return fmt.Errorf("%w (log: %s)", err, logPath)
	}
	return smokeDashHTTP(ctx, baseURL, revision, true)
}

func writeDashSmokeFixtures(root, research string) error {
	for _, dir := range []string{root, filepath.Join(research, "decisions"), filepath.Join(research, "learn", "presentation")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	files := map[string]string{
		filepath.Join(root, "history.md"): "## Backlog\n- [done] generic history\n",
		filepath.Join(root, "backlog.md"): "## Backlog\n- [done] generic drive item\n",
		filepath.Join(root, "goals.json"): `{"schema":"flotilla.goals/v1","goals":[]}`,
		filepath.Join(research, "decisions", "design.md"): `<!-- flotilla-publication
classification: decision
reader-action: Decide whether to run the reversible generic trial.
support: text-only
support-rationale: The bounded decision and rollback are fully stated.
-->
# Reversible generic trial

**Status:** awaiting operator decision

The trial has a bounded rollback.
`,
		filepath.Join(research, "learn", "SOURCE.md"): `<!-- flotilla-publication
classification: research
reader-action: Learn how the measured generic result changes the next experiment.
support: material
-->
# Measured generic result

This report explains the measured outcome and the next reversible experiment.

[Evidence](evidence.csv)
`,
		filepath.Join(research, "learn", "evidence.csv"):               "metric,value\nlatency_ms,5\n",
		filepath.Join(research, "learn", "presentation", "index.html"): "<!doctype html><title>Measured generic result</title><main>Evidence and next experiment.</main>",
	}
	for path, body := range files {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func smokeDashHTTP(ctx context.Context, baseURL, revision string, requireEntries bool) error {
	root, err := getDashSmoke(ctx, baseURL+"/")
	if err != nil {
		return err
	}
	if !strings.Contains(root, `data-build-revision="`+revision+`"`) {
		return fmt.Errorf("root page does not report candidate revision %s", revision)
	}
	researchPage, err := getDashSmoke(ctx, baseURL+"/research?focus=decisions")
	if err != nil {
		return err
	}
	for _, marker := range []string{`id="research-library-title"`, `data-research-focus="decisions"`, `data-research-focus="learn"`, `/static/research.js`} {
		if !strings.Contains(researchPage, marker) {
			return fmt.Errorf("R&D page missing marker %q", marker)
		}
	}
	indexBody, err := getDashSmoke(ctx, baseURL+"/api/research")
	if err != nil {
		return err
	}
	var index dashResearchIndex
	if err := json.Unmarshal([]byte(indexBody), &index); err != nil {
		return fmt.Errorf("decode R&D index: %w", err)
	}
	if !requireEntries {
		return nil
	}
	var decision, learn bool
	for _, entry := range index.Research {
		decision = decision || entry.Decision || entry.Classification.Classification == "decision"
		learn = learn || entry.LearnReady
	}
	if !decision || !learn {
		return fmt.Errorf("R&D smoke missing Decide/Learn evidence: decision=%t learn=%t", decision, learn)
	}
	return nil
}

func applyDashCandidate(ctx context.Context, opts dashDeployOptions, manifest *dashDeployManifest) error {
	tip, err := cleanOriginMainTip(ctx, opts.Repo)
	if err != nil {
		return fmt.Errorf("dash deploy: pre-swap source recheck: %w", err)
	}
	if tip != manifest.TipRevision {
		return fmt.Errorf("dash deploy: origin/main advanced after staging: staged=%s current=%s", manifest.TipRevision, tip)
	}
	candidate, err := inspectDeployBinary(manifest.Candidate.Path)
	if err != nil {
		return fmt.Errorf("dash deploy: pre-swap candidate inspection failed: %w", err)
	}
	if err := validateApprovedCandidate(candidate, manifest.Candidate, tip); err != nil {
		return fmt.Errorf("dash deploy: pre-swap %w", err)
	}
	unitState, err := effectiveDashUnit(ctx, opts.Service, opts.UnitFile)
	if err != nil {
		return fmt.Errorf("dash deploy: pre-swap effective unit: %w", err)
	}
	unitNow, err := inspectDashUnit(unitState)
	if err != nil {
		return fmt.Errorf("dash deploy: pre-swap unit inspection: %w", err)
	}
	if unitNow.SHA256 != manifest.Unit.SHA256 {
		return errors.New("dash deploy: effective unit changed after staging; refusing swap")
	}

	tmpInstall := opts.InstallBin + ".flotilla-deploy-new"
	if err := copyFile(manifest.Candidate.Path, tmpInstall, 0o755); err != nil {
		return fmt.Errorf("dash deploy: stage install sibling: %w", err)
	}
	defer os.Remove(tmpInstall)
	if err := os.Rename(tmpInstall, opts.InstallBin); err != nil {
		return fmt.Errorf("dash deploy: atomic binary swap: %w", err)
	}
	rollback := func(cause error) error {
		var failures []string
		if err := copyFile(manifest.Previous.Path, tmpInstall, 0o755); err != nil {
			failures = append(failures, "copy previous binary: "+err.Error())
		} else if err := os.Rename(tmpInstall, opts.InstallBin); err != nil {
			failures = append(failures, "restore previous binary: "+err.Error())
		}
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer rollbackCancel()
		if _, err := commandOutput(rollbackCtx, "", "systemctl", "--user", "restart", opts.Service); err != nil {
			failures = append(failures, "restart previous service: "+err.Error())
		}
		if len(failures) > 0 {
			return fmt.Errorf("%w; ROLLBACK FAILED: %s (artifacts: %s)", cause, strings.Join(failures, "; "), manifest.Previous.Path)
		}
		return fmt.Errorf("%w (previous binary restored from %s)", cause, manifest.Previous.Path)
	}
	installed, err := inspectDeployBinary(opts.InstallBin)
	if err != nil {
		return rollback(fmt.Errorf("dash deploy: installed provenance check failed: %v", err))
	}
	if err := validateApprovedCandidate(installed, manifest.Candidate, tip); err != nil {
		return rollback(fmt.Errorf("dash deploy: installed candidate check failed: %w", err))
	}
	if _, err := commandOutput(ctx, "", "systemctl", "--user", "restart", opts.Service); err != nil {
		return rollback(fmt.Errorf("dash deploy: restart %s: %w", opts.Service, err))
	}
	if err := waitForDashHTTP(ctx, strings.TrimRight(opts.LiveURL, "/")); err != nil {
		return rollback(fmt.Errorf("dash deploy: installed HTTP readiness: %w", err))
	}
	if err := smokeDashHTTP(ctx, strings.TrimRight(opts.LiveURL, "/"), tip, false); err != nil {
		return rollback(fmt.Errorf("dash deploy: installed HTTP/R&D smoke: %w", err))
	}
	unitStateAfter, err := effectiveDashUnit(ctx, opts.Service, opts.UnitFile)
	if err != nil {
		return rollback(fmt.Errorf("dash deploy: inspect effective unit after deploy: %w", err))
	}
	unitAfter, err := inspectDashUnit(unitStateAfter)
	if err != nil || unitAfter.SHA256 != manifest.Unit.SHA256 {
		return rollback(errors.New("dash deploy: unit flags changed during deploy"))
	}
	manifest.Status = "deployed"
	manifest.Candidate = installed
	return nil
}

func waitForDashHTTP(ctx context.Context, baseURL string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := getDashSmoke(ctx, strings.TrimRight(baseURL, "/")+"/"); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func getDashSmoke(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s = %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return string(body), nil
}

func freeLoopbackAddress() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	return listener.Addr().String(), nil
}

func stopDashSmokeProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

func writeDashDeployManifest(stageDir string, manifest dashDeployManifest) error {
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("dash deploy: encode manifest: %w", err)
	}
	body = append(body, '\n')
	if err := os.WriteFile(filepath.Join(stageDir, "manifest.json"), body, 0o600); err != nil {
		return fmt.Errorf("dash deploy: write manifest: %w", err)
	}
	return nil
}

func commandOutput(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func inspectDashUnit(state dashEffectiveUnit) (dashDeployUnit, error) {
	tracker, backlog, err := dashUnitDataPaths(state.ExecStart)
	if err != nil {
		return dashDeployUnit{}, err
	}
	paths := append([]string{state.FragmentPath}, state.DropInPaths...)
	files := make([]dashDeployUnitFile, 0, len(paths))
	for _, path := range paths {
		sum, sumErr := fileSHA256(path)
		if sumErr != nil {
			return dashDeployUnit{}, sumErr
		}
		files = append(files, dashDeployUnitFile{Path: path, SHA256: sum})
	}
	unit := dashDeployUnit{
		EffectiveExecStart: stableEffectiveExecStart(state.ExecStart),
		TrackerFile:        tracker,
		BacklogFile:        backlog,
		Files:              files,
	}
	unit.SHA256 = effectiveUnitSHA256(unit)
	return unit, nil
}

func stableEffectiveExecStart(execStart string) string {
	execStart = strings.TrimSpace(execStart)
	if !strings.Contains(execStart, "argv[]=") {
		return execStart
	}

	// systemctl show serializes runtime process state into ExecStart alongside
	// the configured command. Fields such as pid and start_time necessarily
	// change after the deploy restarts the service, so they are not unit
	// configuration and must not participate in the configuration CAS.
	var stable []string
	for _, part := range strings.Split(execStart, ";") {
		field := strings.Trim(strings.TrimSpace(part), "{} \t\r\n")
		if strings.HasPrefix(field, "path=") || strings.HasPrefix(field, "argv[]=") {
			stable = append(stable, field)
		}
	}
	if len(stable) == 0 {
		return execStart
	}
	return strings.Join(stable, " ; ")
}

func effectiveUnitSHA256(unit dashDeployUnit) string {
	hash := sha256.New()
	_, _ = io.WriteString(hash, unit.EffectiveExecStart)
	_, _ = io.WriteString(hash, "\x00"+unit.TrackerFile+"\x00"+unit.BacklogFile)
	for _, file := range unit.Files {
		_, _ = io.WriteString(hash, "\x00"+file.Path+"\x00"+file.SHA256)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func pathWithin(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func boolString(value *bool) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprint(*value)
}
