package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentityHomeMalformedLaunchJSONErrors(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	agent := "infra"
	dir, err := Dir(agent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, LaunchFileName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = IdentityHome(agent, "grok")
	if err == nil || !strings.Contains(err.Error(), "parse workspace recipe") {
		t.Fatalf("IdentityHome(malformed) = %v, want parse error", err)
	}
}

func TestIdentityHomeRelativeCwdErrors(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	agent := "infra"
	dir, err := Dir(agent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"launch":"grok --model composer-2.5-fast","cwd":"relative/path","tmux":"flotilla:infra"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, LaunchFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = IdentityHome(agent, "grok")
	if err == nil || !strings.Contains(err.Error(), "not absolute") {
		t.Fatalf("IdentityHome(relative cwd) = %v, want absolute cwd error", err)
	}
}
