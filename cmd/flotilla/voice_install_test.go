package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These tests lock deploy/flotilla-voice-install.sh — the generator that produces
// ~/.config/systemd/user/flotilla-voice.service from the repo template + a host-path env.
// The installer is the anti-drift mechanism (mirrors watch_install_test.go).
const (
	voiceInstallerSh = "../../deploy/flotilla-voice-install.sh"
	voiceExampleEnv  = "../../deploy/flotilla-voice.env.example"
	voiceFixtureEnv  = "../../deploy/testdata/flotilla-voice.fixture.env"
)

var voicePlaceholderRe = regexp.MustCompile(`@FLOTILLA_[A-Z_]+@`)

var voiceFuncLineRe = regexp.MustCompile(`(?m)^(\[(?:Unit|Service|Install)\]|(?:Type|WorkingDirectory|ExecStartPre|ExecStart|RestartSec|Restart|StartLimitIntervalSec|StartLimitBurst|KillSignal|TimeoutStopSec|After|Wants|WantedBy)=.*)$`)

func renderVoiceUnit(t *testing.T, envPath string) string {
	t.Helper()
	out, err := exec.Command("bash", voiceInstallerSh, "--print", envPath).CombinedOutput()
	if err != nil {
		t.Fatalf("installer --print %s failed: %v\n%s", envPath, err, out)
	}
	return string(out)
}

func TestVoiceInstallerGeneratesExpectedFunctionalUnit(t *testing.T) {
	unit := renderVoiceUnit(t, voiceFixtureEnv)
	if m := voicePlaceholderRe.FindAllString(unit, -1); len(m) > 0 {
		t.Fatalf("unsubstituted placeholders remain: %v", m)
	}
	want := []string{
		"[Unit]",
		"After=network-online.target",
		"Wants=network-online.target",
		"StartLimitIntervalSec=600",
		"StartLimitBurst=5",
		"[Service]",
		"Type=simple",
		"WorkingDirectory=/srv/fleet",
		`ExecStartPre=/bin/sh -c 'for i in $(seq 1 30); do getent hosts discord.com >/dev/null 2>&1 && exit 0; sleep 2; done; exit 0'`,
		"ExecStart=%h/go/bin/flotilla-voice voice --config /srv/fleet/voice.env --roster /srv/fleet/flotilla.json --secrets /srv/fleet/secrets.env",
		"Restart=on-failure",
		"RestartSec=15",
		"KillSignal=SIGTERM",
		"TimeoutStopSec=15",
		"[Install]",
		"WantedBy=default.target",
	}
	got := voiceFuncLineRe.FindAllString(unit, -1)
	if len(got) != len(want) {
		t.Fatalf("functional line count = %d, want %d\ngot:  %#v\nwant: %#v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("functional line %d:\n got:  %q\n want: %q", i, got[i], want[i])
		}
	}
}

// The committed example env must keep the template fully substitutable (catches template /
// example drift before it ships).
func TestVoiceInstallerExampleEnvSubstitutesFully(t *testing.T) {
	unit := renderVoiceUnit(t, voiceExampleEnv)
	if m := voicePlaceholderRe.FindAllString(unit, -1); len(m) > 0 {
		t.Fatalf("example env leaves placeholders (template/example out of sync): %v", m)
	}
}

func TestVoiceInstallerRejectsIncompleteEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), "incomplete.env")
	if err := os.WriteFile(p, []byte("FLOTILLA_WORKDIR=/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", voiceInstallerSh, "--print", p).CombinedOutput(); err == nil {
		t.Fatalf("expected failure on incomplete env, got success:\n%s", out)
	}
}

func TestVoiceInstallerRejectsPlaceholderInValue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "evil.env")
	body := "FLOTILLA_WORKDIR=/srv/fleet\n" +
		"FLOTILLA_BIN=@FLOTILLA_ROSTER@\n" +
		"FLOTILLA_VOICE_CONFIG=/srv/fleet/voice.env\n" +
		"FLOTILLA_ROSTER=/srv/fleet/flotilla.json\n" +
		"FLOTILLA_SECRETS=/srv/fleet/secrets.env\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", voiceInstallerSh, "--print", p).CombinedOutput(); err == nil {
		t.Fatalf("expected failure on placeholder-in-value, got success:\n%s", out)
	}
}

func TestVoiceInstallerTrimsWhitespaceAroundEquals(t *testing.T) {
	p := filepath.Join(t.TempDir(), "spaced.env")
	body := "FLOTILLA_WORKDIR = /srv/fleet\n" +
		"\tFLOTILLA_BIN = %h/go/bin/flotilla-voice\n" +
		"FLOTILLA_VOICE_CONFIG=/srv/fleet/voice.env\n" +
		"FLOTILLA_ROSTER=/srv/fleet/flotilla.json\n" +
		"FLOTILLA_SECRETS=/srv/fleet/secrets.env\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	unit := renderVoiceUnit(t, p)
	if !strings.Contains(unit, "ExecStart=%h/go/bin/flotilla-voice voice ") {
		t.Errorf("ExecStart not clean after a spaced env:\n%s", voiceFuncLineRe.FindString(unit))
	}
	if strings.Contains(unit, "ExecStart= ") || strings.Contains(unit, "WorkingDirectory= ") {
		t.Errorf("a value retained leading whitespace:\n%s", unit)
	}
}
