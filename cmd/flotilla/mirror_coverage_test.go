package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
)

// captureStdoutStderr runs f while capturing os.Stdout and os.Stderr.
func captureStdoutStderr(t *testing.T, f func()) (stdout, stderr string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	f()
	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	var bufOut, bufErr bytes.Buffer
	_, _ = io.Copy(&bufOut, rOut)
	_, _ = io.Copy(&bufErr, rErr)
	_ = rOut.Close()
	_ = rErr.Close()
	return bufOut.String(), bufErr.String()
}

func secretsFromString(t *testing.T, body string) *roster.Secrets {
	t.Helper()
	p := filepath.Join(t.TempDir(), "secrets.env")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := roster.LoadSecrets(p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestUniqueBindingXOAgents_Dedupes506(t *testing.T) {
	// Same XO on three channel bindings (live hotline triple-list defect).
	cfg := &roster.Config{
		XOAgent: "meta-xo",
		Channels: []roster.Channel{
			{ChannelID: "C1", XOAgent: "meta-xo", Members: []string{"backend"}},
			{ChannelID: "C2", XOAgent: "meta-xo", Members: []string{"frontend"}},
			{ChannelID: "C3", XOAgent: "meta-xo", Members: []string{"data"}},
			{ChannelID: "C4", XOAgent: "alpha-xo", Members: []string{"alpha-be"}},
		},
		Agents: []roster.Agent{
			{Name: "meta-xo"}, {Name: "alpha-xo"},
			{Name: "backend"}, {Name: "frontend"}, {Name: "data"}, {Name: "alpha-be"},
		},
	}
	got := uniqueBindingXOAgents(cfg)
	if len(got) != 2 || got[0] != "meta-xo" || got[1] != "alpha-xo" {
		t.Fatalf("uniqueBindingXOAgents = %v, want [meta-xo alpha-xo] once each", got)
	}
}

func TestLogReplyLegCoverage_DedupesAndWarns506(t *testing.T) {
	cfg := &roster.Config{
		XOAgent: "meta-xo",
		Channels: []roster.Channel{
			{ChannelID: "C1", XOAgent: "meta-xo"},
			{ChannelID: "C2", XOAgent: "meta-xo"},
			{ChannelID: "C3", XOAgent: "meta-xo"},
			{ChannelID: "C4", XOAgent: "alpha-xo"},
		},
		Agents: []roster.Agent{{Name: "meta-xo"}, {Name: "alpha-xo"}},
	}
	// Only meta-xo has a webhook.
	sec := secretsFromString(t, "FLOTILLA_WEBHOOK_META_XO=https://example.invalid/wh/meta\n")
	out, errOut := captureStdoutStderr(t, func() { logReplyLegCoverage(cfg, sec) })
	if strings.Count(out, "meta-xo") != 1 {
		t.Fatalf("hotline banner must list meta-xo once, got: %q", out)
	}
	if !strings.Contains(out, "1 XO(s) routable") || !strings.Contains(out, "1 with no webhook") {
		t.Fatalf("coverage counts wrong: %q", out)
	}
	if !strings.Contains(out, "alpha-xo") {
		t.Fatalf("alpha-xo without webhook must appear in without list: %q", out)
	}
	if !strings.Contains(errOut, "WARNING") || !strings.Contains(errOut, "alpha-xo") {
		t.Fatalf("stderr must LOUD-warn missing hotline webhook: %q", errOut)
	}
}

func TestFinishEdgeMirrorAgents_IncludesCoordinators506(t *testing.T) {
	cfg := &roster.Config{
		XOAgent:  "meta-xo",
		CosAgent: "cos",
		Agents: []roster.Agent{
			{Name: "meta-xo"},
			{Name: "cos"},
			{Name: "alpha-xo"},
			{Name: "backend"},
		},
	}
	got := finishEdgeMirrorAgents(cfg)
	// Primary XO, cos, project-XO, and desk — all monitored by finish-edge mirrors.
	want := []string{"meta-xo", "cos", "alpha-xo", "backend"}
	if len(got) != len(want) {
		t.Fatalf("finishEdgeMirrorAgents = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("finishEdgeMirrorAgents[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLogMirrorCoverage_IncludesCoordinatorsAndLoudMissing506(t *testing.T) {
	cfg := &roster.Config{
		XOAgent:  "meta-xo",
		CosAgent: "cos",
		Agents: []roster.Agent{
			{Name: "meta-xo"},
			{Name: "cos"},
			{Name: "backend"},
		},
	}
	sec := secretsFromString(t,
		"FLOTILLA_WEBHOOK_BACKEND=https://example.invalid/wh/backend\n"+
			"FLOTILLA_WEBHOOK_COS=https://example.invalid/wh/cos\n",
	)
	out, errOut := captureStdoutStderr(t, func() { logMirrorCoverage(cfg, sec, "meta-xo") })
	// Banner must name the primary coordinator (was omitted pre-#506).
	if !strings.Contains(out, "meta-xo") {
		t.Fatalf("finish-edge banner must include primary XO meta-xo: %q", out)
	}
	if !strings.Contains(out, "finish-edge mirror") {
		t.Fatalf("banner must say finish-edge mirror (not desk-only): %q", out)
	}
	if !strings.Contains(out, "cos") || !strings.Contains(out, "backend") {
		t.Fatalf("banner must list cos + backend: %q", out)
	}
	// meta-xo lacks webhook → without list + LOUD stderr.
	if !strings.Contains(out, "1 have no webhook") && !strings.Contains(out, "have no webhook") {
		t.Fatalf("want missing-webhook count in stdout: %q", out)
	}
	if !strings.Contains(errOut, "WARNING") || !strings.Contains(errOut, "meta-xo") {
		t.Fatalf("stderr must LOUD-warn missing coordinator webhook: %q", errOut)
	}
	if !strings.Contains(errOut, "FLOTILLA_WEBHOOK") {
		t.Fatalf("stderr must hint secrets key: %q", errOut)
	}
}

func TestPartitionWebhookCoverage506(t *testing.T) {
	sec := secretsFromString(t, "FLOTILLA_WEBHOOK_BACKEND=https://example.invalid/wh\n")
	with, without := partitionWebhookCoverage([]string{"backend", "cos", "alpha-xo"}, sec)
	if len(with) != 1 || with[0] != "backend" {
		t.Fatalf("with = %v, want [backend]", with)
	}
	if len(without) != 2 {
		t.Fatalf("without = %v, want cos + alpha-xo", without)
	}
}
