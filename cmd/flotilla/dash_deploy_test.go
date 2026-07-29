package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDashUnitDataPathsRequiresDistinctExplicitFlags(t *testing.T) {
	unit := `[Service]
ExecStart=/opt/flotilla dash --tracker-file /var/lib/flotilla/history.md --backlog-file=/var/lib/flotilla/drive.md
`
	tracker, backlog, err := dashUnitDataPaths(unit)
	if err != nil {
		t.Fatal(err)
	}
	if tracker != "/var/lib/flotilla/history.md" || backlog != "/var/lib/flotilla/drive.md" {
		t.Fatalf("paths = %q %q", tracker, backlog)
	}
	for name, body := range map[string]string{
		"missing tracker": "ExecStart=/opt/flotilla dash --backlog-file /tmp/drive.md",
		"missing backlog": "ExecStart=/opt/flotilla dash --tracker-file /tmp/history.md",
		"conflated":       "ExecStart=/opt/flotilla dash --tracker-file /tmp/state.md --backlog-file /tmp/state.md",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := dashUnitDataPaths(body); err == nil {
				t.Fatal("expected fail-closed unit validation")
			}
		})
	}
}

func TestValidateCandidateProvenanceRefusesDirtyOrWrongRevision(t *testing.T) {
	const tip = "0123456789abcdef0123456789abcdef01234567"
	clean, dirty := false, true
	tests := []struct {
		name      string
		candidate dashDeployBinary
		wantErr   bool
	}{
		{"clean exact tip", dashDeployBinary{Revision: tip, Modified: &clean}, false},
		{"dirty exact tip", dashDeployBinary{Revision: tip, Modified: &dirty}, true},
		{"missing dirty stamp", dashDeployBinary{Revision: tip}, true},
		{"clean wrong revision", dashDeployBinary{Revision: strings.Repeat("a", 40), Modified: &clean}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCandidateProvenance(tt.candidate, tip)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateCandidateProvenance() error = %v, wantErr=%t", err, tt.wantErr)
			}
		})
	}
}

func TestValidateApprovedCandidateRefusesSameRevisionDifferentBytes(t *testing.T) {
	const tip = "0123456789abcdef0123456789abcdef01234567"
	clean := false
	approved := dashDeployBinary{Revision: tip, Modified: &clean, SHA256: "approved"}
	for name, candidate := range map[string]dashDeployBinary{
		"same bytes":              {Revision: tip, Modified: &clean, SHA256: "approved"},
		"same revision new bytes": {Revision: tip, Modified: &clean, SHA256: "mutated"},
		"missing hash":            {Revision: tip, Modified: &clean},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateApprovedCandidate(candidate, approved, tip)
			if (err != nil) != (name != "same bytes") {
				t.Fatalf("validateApprovedCandidate() error = %v", err)
			}
		})
	}
}

func TestEffectiveUnitCASIncludesBaseDropInsAndExecStart(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "flotilla-dash.service")
	dropIn := filepath.Join(root, "flotilla-dash.service.d", "70-tracker-file.conf")
	writeTestFile(t, base, "[Service]\nExecStart=/opt/flotilla dash --bind 127.0.0.1:8787\n")
	writeTestFile(t, dropIn, "[Service]\nExecStart=\nExecStart=/opt/flotilla dash --tracker-file /state/history.md --backlog-file /state/drive.md\n")
	const configured = `path=/opt/flotilla ; argv[]=/opt/flotilla dash --tracker-file /state/history.md --backlog-file /state/drive.md`
	state := dashEffectiveUnit{
		FragmentPath: base,
		DropInPaths:  []string{dropIn},
		ExecStart:    `{ ` + configured + ` ; ignore_errors=no ; start_time=[Wed 2026-07-29 03:27:22 UTC] ; stop_time=[n/a] ; pid=1705733 ; code=(null) ; status=0/0 }`,
	}
	stage := t.TempDir()
	snapshot, err := snapshotDashUnit(state, stage)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TrackerFile != "/state/history.md" || snapshot.BacklogFile != "/state/drive.md" {
		t.Fatalf("effective flags = %q %q", snapshot.TrackerFile, snapshot.BacklogFile)
	}
	if len(snapshot.Files) != 2 {
		t.Fatalf("snapshotted files = %d, want base + drop-in", len(snapshot.Files))
	}
	if snapshot.EffectiveExecStart != configured {
		t.Fatalf("stable effective ExecStart = %q, want %q", snapshot.EffectiveExecStart, configured)
	}
	for _, file := range snapshot.Files {
		if file.Snapshot == "" {
			t.Fatalf("missing rollback snapshot: %+v", file)
		}
		if _, err := os.Stat(file.Snapshot); err != nil {
			t.Fatalf("rollback snapshot %s: %v", file.Snapshot, err)
		}
	}
	inspected, err := inspectDashUnit(state)
	if err != nil {
		t.Fatal(err)
	}
	if inspected.SHA256 != snapshot.SHA256 {
		t.Fatalf("effective unit CAS = %s, snapshot = %s", inspected.SHA256, snapshot.SHA256)
	}
	restarted := state
	restarted.ExecStart = `{ ` + configured + ` ; ignore_errors=no ; start_time=[Wed 2026-07-29 04:01:03 UTC] ; stop_time=[n/a] ; pid=1800123 ; code=(null) ; status=0/0 }`
	afterRestart, err := inspectDashUnit(restarted)
	if err != nil {
		t.Fatal(err)
	}
	if afterRestart.SHA256 != snapshot.SHA256 {
		t.Fatalf("restart-only runtime fields changed unit CAS: after=%s before=%s", afterRestart.SHA256, snapshot.SHA256)
	}

	flagDrift := restarted
	flagDrift.ExecStart = strings.Replace(flagDrift.ExecStart, "/state/drive.md", "/state/other-drive.md", 1)
	changedFlags, err := inspectDashUnit(flagDrift)
	if err != nil {
		t.Fatal(err)
	}
	if changedFlags.SHA256 == snapshot.SHA256 {
		t.Fatal("effective argv flag change did not invalidate unit CAS")
	}

	writeTestFile(t, dropIn, "[Service]\nExecStart=\nExecStart=/opt/flotilla dash --tracker-file /state/history.md --backlog-file /state/other-drive.md\n")
	changed, err := inspectDashUnit(state)
	if err != nil {
		t.Fatal(err)
	}
	if changed.SHA256 == snapshot.SHA256 {
		t.Fatal("drop-in change did not invalidate effective unit CAS")
	}
}

func TestDashDeployHTTPRnDSmoke(t *testing.T) {
	const revision = "0123456789abcdef0123456789abcdef01234567"
	index := `{"research":[{"id":"decisions/design.md","decision":true,"learn_ready":false,"publication":{"classification":"decision"}},{"id":"learn/SOURCE.md","decision":false,"learn_ready":true,"publication":{"classification":"research"}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprintf(w, `<body data-build-revision="%s">`, revision)
		case "/research":
			fmt.Fprint(w, `<h1 id="research-library-title">R&amp;D</h1><button data-research-focus="decisions"></button><button data-research-focus="learn"></button><script src="/static/research.js"></script>`)
		case "/api/research":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, index)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if err := smokeDashHTTP(context.Background(), server.URL, revision, true); err != nil {
		t.Fatal(err)
	}

	missingLearn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprintf(w, `<body data-build-revision="%s">`, revision)
		case "/research":
			fmt.Fprint(w, `<h1 id="research-library-title"></h1><i data-research-focus="decisions"></i><i data-research-focus="learn"></i><script src="/static/research.js"></script>`)
		case "/api/research":
			fmt.Fprint(w, `{"research":[{"id":"decisions/design.md","decision":true,"learn_ready":false,"publication":{"classification":"decision"}}]}`)
		}
	}))
	defer missingLearn.Close()
	if err := smokeDashHTTP(context.Background(), missingLearn.URL, revision, true); err == nil || !strings.Contains(err.Error(), "missing Decide/Learn") {
		t.Fatalf("missing Learn showpiece must fail, got %v", err)
	}
}

func TestSmokeStagedDashExercisesCurrentProductRnD(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test builds and starts the current flotilla candidate")
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	stage := t.TempDir()
	candidate := filepath.Join(stage, "flotilla-candidate")
	runTestCommand(t, repo, "go", "build", "-buildvcs=true", "-trimpath", "-o", candidate, "./cmd/flotilla")
	revision := "unavailable"
	if info, inspectErr := inspectDeployBinary(candidate); inspectErr == nil && info.Modified != nil && !*info.Modified {
		revision = info.Revision
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := smokeStagedDash(ctx, candidate, repo, stage, revision); err != nil {
		t.Fatal(err)
	}
}

func TestDashDeployStagesOnlyCleanOriginMainWithProvenanceAndRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test builds a tiny Go fixture")
	}
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	repo := filepath.Join(root, "repo")
	runTestCommand(t, root, "git", "init", "--bare", origin)
	runTestCommand(t, root, "git", "init", "-b", "main", repo)
	runTestCommand(t, repo, "git", "config", "user.name", "Flotilla Test")
	runTestCommand(t, repo, "git", "config", "user.email", "flotilla@example.invalid")
	writeTestFile(t, filepath.Join(repo, "go.mod"), "module example.invalid/flotilla\n\ngo 1.23\n")
	writeTestFile(t, filepath.Join(repo, "flotilla.example.json"), `{"xo_agent":"xo","agents":[{"name":"xo"}]}`)
	writeTestFile(t, filepath.Join(repo, "cmd", "flotilla", "main.go"), dashDeployFixtureProgram)
	runTestCommand(t, repo, "git", "add", "go.mod", "flotilla.example.json", "cmd/flotilla/main.go")
	runTestCommand(t, repo, "git", "commit", "-m", "fixture")
	runTestCommand(t, repo, "git", "remote", "add", "origin", origin)
	runTestCommand(t, repo, "git", "push", "-u", "origin", "main")
	tip := strings.TrimSpace(runTestCommand(t, repo, "git", "rev-parse", "HEAD"))

	unitPath := filepath.Join(root, "flotilla-dash.service")
	unitBody := "[Service]\nExecStart=/opt/flotilla dash --bind 127.0.0.1:8787\n"
	writeTestFile(t, unitPath, unitBody)
	dropInPath := filepath.Join(root, "flotilla-dash.service.d", "70-tracker-file.conf")
	dropInBody := "[Service]\nExecStart=\nExecStart=/opt/flotilla dash --tracker-file /var/lib/flotilla/history.md --backlog-file /var/lib/flotilla/drive.md\n"
	writeTestFile(t, dropInPath, dropInBody)
	unitState := &dashEffectiveUnit{
		FragmentPath: unitPath,
		DropInPaths:  []string{dropInPath},
		ExecStart:    `{ path=/opt/flotilla ; argv[]=/opt/flotilla dash --tracker-file /var/lib/flotilla/history.md --backlog-file /var/lib/flotilla/drive.md ; }`,
	}
	installBin := filepath.Join(root, "installed-flotilla")
	writeTestFile(t, installBin, "previous-binary")
	if err := os.Chmod(installBin, 0o755); err != nil {
		t.Fatal(err)
	}
	stageDir := filepath.Join(root, "stage")
	manifest, err := runDashDeploy(dashDeployOptions{
		Repo:       repo,
		StageDir:   stageDir,
		UnitFile:   unitPath,
		InstallBin: installBin,
		Service:    "must-not-run.service",
		Apply:      false,
		Timeout:    time.Minute,
		UnitState:  unitState,
	})
	if err != nil {
		if out, inspectErr := exec.Command("go", "version", "-m", filepath.Join(stageDir, "flotilla-candidate")).CombinedOutput(); inspectErr == nil {
			t.Logf("candidate build info:\n%s", out)
		}
		t.Fatal(err)
	}
	if manifest.Status != "staged" || manifest.TipRevision != tip {
		t.Fatalf("manifest status/tip = %q %q, want staged %q", manifest.Status, manifest.TipRevision, tip)
	}
	if manifest.Candidate.Modified == nil || *manifest.Candidate.Modified || manifest.Candidate.Revision != tip {
		t.Fatalf("candidate provenance = %+v", manifest.Candidate)
	}
	if pathWithin(manifest.Candidate.Path, repo) {
		t.Fatalf("candidate was written inside source checkout: %s", manifest.Candidate.Path)
	}
	if manifest.Unit.TrackerFile == manifest.Unit.BacklogFile ||
		manifest.Unit.TrackerFile != "/var/lib/flotilla/history.md" ||
		manifest.Unit.BacklogFile != "/var/lib/flotilla/drive.md" {
		t.Fatalf("unit flags = %+v", manifest.Unit)
	}
	if len(manifest.Unit.Files) != 2 {
		t.Fatalf("unit rollback files = %+v", manifest.Unit.Files)
	}
	for i, want := range []string{unitBody, dropInBody} {
		unitSnapshot, err := os.ReadFile(manifest.Unit.Files[i].Snapshot)
		if err != nil || string(unitSnapshot) != want {
			t.Fatalf("unit rollback snapshot %d = %q err=%v", i, unitSnapshot, err)
		}
	}
	onDisk, err := os.ReadFile(filepath.Join(stageDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded dashDeployManifest
	if err := json.Unmarshal(onDisk, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Status != "staged" || decoded.Previous.SHA256 == "" {
		t.Fatalf("durable manifest = %+v", decoded)
	}
	if got, err := os.ReadFile(installBin); err != nil || string(got) != "previous-binary" {
		t.Fatalf("stage-only changed installed binary: %q err=%v", got, err)
	}

	writeTestFile(t, filepath.Join(repo, "dirty-untracked"), "must refuse")
	_, err = runDashDeploy(dashDeployOptions{
		Repo:       repo,
		StageDir:   filepath.Join(root, "dirty-stage"),
		UnitFile:   unitPath,
		InstallBin: installBin,
		Apply:      false,
		Timeout:    time.Minute,
		UnitState:  unitState,
	})
	if err == nil || !strings.Contains(err.Error(), "source checkout is dirty") {
		t.Fatalf("dirty checkout must fail before build, got %v", err)
	}
}

func runTestCommand(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const dashDeployFixtureProgram = `package main

import (
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
)

func main() {
	bind := "127.0.0.1:0"
	for i := 0; i+1 < len(os.Args); i++ {
		if os.Args[i] == "--bind" {
			bind = os.Args[i+1]
		}
	}
	revision := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				revision = setting.Value
			}
		}
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, "<body data-build-revision=%q>", revision)
	})
	http.HandleFunc("/research", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "<h1 id=\"research-library-title\">R&amp;D</h1><button data-research-focus=\"decisions\"></button><button data-research-focus=\"learn\"></button><script src=\"/static/research.js\"></script>")
	})
	http.HandleFunc("/api/research", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "{\"research\":[{\"id\":\"decisions/design.md\",\"decision\":true,\"learn_ready\":false,\"publication\":{\"classification\":\"decision\"}},{\"id\":\"learn/SOURCE.md\",\"decision\":false,\"learn_ready\":true,\"publication\":{\"classification\":\"research\"}}]}")
	})
	if err := http.ListenAndServe(bind, nil); err != nil {
		panic(err)
	}
}
`
