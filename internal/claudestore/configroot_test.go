package claudestore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withConfigRootFakes(t *testing.T) {
	t.Helper()
	oldPID, oldStart, oldRead := panePID, paneStartCommand, readProcFile
	t.Cleanup(func() { panePID, paneStartCommand, readProcFile = oldPID, oldStart, oldRead })
}

func TestProjectsRootForPanePrecedence(t *testing.T) {
	withConfigRootFakes(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	panePID = func(string) (int, error) { return 101, nil }
	paneStartCommand = func(string) (string, error) { return "export CLAUDE_CONFIG_DIR='/intent/account'; claude", nil }
	readProcFile = func(path string) ([]byte, error) {
		switch path {
		case "/proc/101/environ":
			return []byte("PATH=/bin\x00"), nil
		case "/proc/101/task/101/children":
			return []byte("202"), nil
		case "/proc/202/environ":
			return []byte("CLAUDE_CONFIG_DIR=/observed/account\x00"), nil
		default:
			return nil, os.ErrNotExist
		}
	}
	root, ok := projectsRootForPane("pane")
	if !ok || root != "/observed/account/projects" {
		t.Fatalf("observed root=(%q,%t)", root, ok)
	}
	// A successful process-environment observation that lacks the variable is
	// evidence for Claude's default root. Stale recipe intent must not override it.
	readProcFile = func(path string) ([]byte, error) {
		switch path {
		case "/proc/101/environ":
			return []byte("PATH=/bin\x00"), nil
		case "/proc/101/task/101/children":
			return []byte(""), nil
		default:
			return nil, os.ErrNotExist
		}
	}
	root, ok = projectsRootForPane("pane")
	if !ok || root != filepath.Join(home, ".claude", "projects") {
		t.Fatalf("observed-absent root=(%q,%t), want default despite recipe intent", root, ok)
	}

	// Only an unavailable live observation permits launch-recipe fallback.
	panePID = func(string) (int, error) { return 0, fmt.Errorf("unavailable") }
	root, ok = projectsRootForPane("pane")
	if !ok || root != "/intent/account/projects" {
		t.Fatalf("recipe root=(%q,%t)", root, ok)
	}
	paneStartCommand = func(string) (string, error) { return "claude", nil }
	root, ok = projectsRootForPane("pane")
	if !ok || root != filepath.Join(home, ".claude", "projects") {
		t.Fatalf("default root=(%q,%t)", root, ok)
	}
}

func TestObservedConfigRootLocatesSession(t *testing.T) {
	withConfigRootFakes(t)
	config := t.TempDir()
	cwd := "/workspace/alpha"
	project := filepath.Join(config, "projects", encodeProjectDir(cwd))
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"type":"assistant","cwd":%q,"message":{"role":"assistant","content":"completed from account root"}}`, cwd)
	if err := os.WriteFile(filepath.Join(project, "session.jsonl"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	panePID = func(string) (int, error) { return 303, nil }
	paneStartCommand = func(string) (string, error) { return "claude", nil }
	readProcFile = func(path string) ([]byte, error) {
		if path == "/proc/303/environ" {
			return []byte("CLAUDE_CONFIG_DIR=" + config + "\x00"), nil
		}
		return nil, os.ErrNotExist
	}
	root, ok := projectsRootForPane("pane")
	if !ok {
		t.Fatal("observed root not resolved")
	}
	text, ok := latestTurnTextForCwdRoot(root, cwd)
	if !ok || text != "completed from account root" {
		t.Fatalf("latest=(%q,%t), root=%q", text, ok, root)
	}
}

func TestProjectsRootForPaneNoSourcesFailsSoft(t *testing.T) {
	withConfigRootFakes(t)
	t.Setenv("HOME", "")
	panePID = func(string) (int, error) { return 0, fmt.Errorf("unavailable") }
	paneStartCommand = func(string) (string, error) { return "", fmt.Errorf("unavailable") }
	root, ok := projectsRootForPane("pane")
	if ok || root != "" {
		t.Fatalf("root=(%q,%t), want fail-soft", root, ok)
	}
}

func TestConfigDirFromLaunch(t *testing.T) {
	for _, tc := range []struct{ launch, want string }{
		{"export CLAUDE_CONFIG_DIR='/accounts/one'; claude", "/accounts/one"},
		{"CLAUDE_CONFIG_DIR=/accounts/two claude", "/accounts/two"},
		{"claude", ""},
	} {
		if got := configDirFromLaunch(tc.launch); got != tc.want {
			t.Errorf("configDirFromLaunch(%q)=%q want %q", tc.launch, got, tc.want)
		}
	}
	if got := envValue([]byte(strings.Join([]string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/account"}, "\x00")), claudeConfigDir); got != "/account" {
		t.Fatalf("envValue=%q", got)
	}
}
