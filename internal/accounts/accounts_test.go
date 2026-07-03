package accounts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateID(t *testing.T) {
	for _, id := range []string{"anthropic-work", "anthropic-personal", "a", "a1_b2-c3"} {
		if err := ValidateID(id); err != nil {
			t.Errorf("ValidateID(%q) = %v, want nil", id, err)
		}
	}
	for _, id := range []string{"", "My-Account", "../escape", "UPPER", "has space"} {
		if err := ValidateID(id); err == nil {
			t.Errorf("ValidateID(%q) = nil, want error", id)
		}
	}
}

func TestConfigDirAndInit(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	dir, err := Init("anthropic-work")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "anthropic-work", ClaudeConfigSubdir)
	if dir != want {
		t.Errorf("Init dir = %q, want %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("mode = %o, want 0700", info.Mode().Perm())
	}
	// idempotent
	if _, err := Init("anthropic-work"); err != nil {
		t.Fatalf("second Init: %v", err)
	}
}

func TestWrapClaudeLaunch(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	got, err := WrapClaudeLaunch("claude-code", "anthropic-work", "claude -w xo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "export CLAUDE_CONFIG_DIR=") {
		t.Fatalf("got %q, want CLAUDE_CONFIG_DIR prefix", got)
	}
	if !strings.HasSuffix(got, "claude -w xo") {
		t.Fatalf("got %q, want original launch suffix", got)
	}
	// non-claude: no wrap
	got, err = WrapClaudeLaunch("grok", "xai-personal", "grok")
	if err != nil || got != "grok" {
		t.Errorf("grok wrap = %q err=%v, want grok", got, err)
	}
	// empty subscription: no wrap
	got, err = WrapClaudeLaunch("claude-code", "", "claude -w xo")
	if err != nil || got != "claude -w xo" {
		t.Errorf("empty sub = %q err=%v", got, err)
	}
	// idempotent
	already := "export CLAUDE_CONFIG_DIR='/tmp/x'; claude -w xo"
	got, err = WrapClaudeLaunch("claude-code", "anthropic-work", already)
	if err != nil || got != already {
		t.Errorf("idempotent = %q err=%v, want unchanged", got, err)
	}
}

func TestProbeHealth(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)

	if _, err := Init("anthropic-work"); err != nil {
		t.Fatal(err)
	}
	h, err := ProbeHealth("anthropic-work", now)
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != StatusNoCredsFile {
		t.Errorf("status = %q, want %q", h.Status, StatusNoCredsFile)
	}

	dir, err := ConfigDir("anthropic-work")
	if err != nil {
		t.Fatal(err)
	}
	cred := filepath.Join(dir, CredentialsFile)
	body := fmt.Sprintf(`{"claudeAiOauth":{"expiresAt":%d,"subscriptionType":"max"}}`, now.Add(48*time.Hour).UnixMilli())
	if err := os.WriteFile(cred, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err = ProbeHealth("anthropic-work", now)
	if err != nil {
		t.Fatal(err)
	}
	if !h.CredFileExists || h.SubscriptionType != "max" || h.Status != StatusOK {
		t.Errorf("probe = %+v, want cred exists type=max status=ok", h)
	}

	// expired
	body = fmt.Sprintf(`{"claudeAiOauth":{"expiresAt":%d}}`, now.Add(-time.Hour).UnixMilli())
	if err := os.WriteFile(cred, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err = ProbeHealth("anthropic-work", now)
	if err != nil || h.Status != StatusExpired {
		t.Errorf("expired status = %q err=%v", h.Status, err)
	}

	// expires soon
	body = fmt.Sprintf(`{"claudeAiOauth":{"expiresAt":%d}}`, now.Add(2*time.Hour).UnixMilli())
	if err := os.WriteFile(cred, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err = ProbeHealth("anthropic-work", now)
	if err != nil || h.Status != StatusExpiresSoon {
		t.Errorf("expires-soon status = %q err=%v", h.Status, err)
	}
}

func TestProbeHealthNoSecretsInOutput(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	dir, _ := Init("anthropic-work")
	body := `{"claudeAiOauth":{"accessToken":"SECRET","refreshToken":"SECRET2","expiresAt":9999999999999,"subscriptionType":"max"}}`
	if err := os.WriteFile(filepath.Join(dir, CredentialsFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := ProbeHealth("anthropic-work", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw := h.SubscriptionID + h.ConfigDir + h.Status + h.SubscriptionType
	if strings.Contains(raw, "SECRET") {
		t.Error("probe output must not contain token material")
	}
}

func TestList(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	if _, err := Init("anthropic-work"); err != nil {
		t.Fatal(err)
	}
	if _, err := Init("anthropic-personal"); err != nil {
		t.Fatal(err)
	}
	list, err := List(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
}
