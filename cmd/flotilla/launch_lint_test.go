package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReportLaunchChainLintWarnsSortedAndHonorsAcknowledgement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flotilla-launch.json")
	body := `{"agents":{
		"alpha":{"launch":"alpha","cwd":"/srv/alpha"},
		"beta":{"launch":"beta","cwd":"/srv/beta","fallbacks":[{"surface":"grok","launch":"grok"}]},
		"gamma":{"launch":"gamma","cwd":"/srv/gamma","single_harness":true}
	}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var messages []string
	err := reportLaunchChainLint("test", path,
		map[string]bool{"alpha": true, "beta": true, "gamma": true, "delta": true},
		func(message string) { messages = append(messages, message) })
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %v, want one warning", messages)
	}
	for _, want := range []string{"alpha, delta", "single_harness=true", "docs/harness-subscription-switching.md"} {
		if !strings.Contains(messages[0], want) {
			t.Errorf("warning %q missing %q", messages[0], want)
		}
	}
}

func TestLoadLaunchForChainLintMissingFileFlagsWholeRoster(t *testing.T) {
	roster := map[string]bool{"beta": true, "alpha": true}
	cfg, err := loadLaunchForChainLint(filepath.Join(t.TempDir(), "missing.json"), roster)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.UnprotectedAgents(roster), []string{"alpha", "beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unprotected = %v, want %v", got, want)
	}
}

func TestDoctorRunsSharedLaunchLint(t *testing.T) {
	body, err := os.ReadFile("../../deploy/flotilla-doctor.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{`"$BIN" launch lint`, `--roster "$WORKDIR/flotilla.json"`, "launch chain lint could not inspect recipes"} {
		if !strings.Contains(script, want) {
			t.Errorf("doctor script missing %q", want)
		}
	}
}

func TestWatchLaunchLintWiring(t *testing.T) {
	body, err := os.ReadFile("watch.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	for _, want := range []string{"logLaunchChainLint(launchPath, rosterAgents)", "time.NewTicker(launchChainNoticeInterval)"} {
		if !strings.Contains(source, want) {
			t.Errorf("watch wiring missing %q", want)
		}
	}
}
