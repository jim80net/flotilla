package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnv(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "voice.env")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadVoiceConfigComplete(t *testing.T) {
	p := writeEnv(t, `
# voice config
export XAI_API_KEY="xai-secret-value"
VOICE_GUILD_ID=111
VOICE_CHANNEL_ID = 222
VOICE_OPERATOR_USER_ID=333
VOICE_XO_AGENT=alpha-xo
VOICE_COST_CAP_USD=2.50
`)
	cfg, err := loadVoiceConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.XAIKey != "xai-secret-value" || cfg.GuildID != "111" || cfg.ChannelID != "222" ||
		cfg.OperatorUserID != "333" || cfg.XOAgent != "alpha-xo" || cfg.CapUSD != 2.50 {
		t.Errorf("parsed config wrong: %+v", *cfg)
	}
}

func TestLoadVoiceConfigMissingKeysNamed(t *testing.T) {
	p := writeEnv(t, "XAI_API_KEY=k\nVOICE_GUILD_ID=1\n") // missing channel/operator/xo/cap
	_, err := loadVoiceConfig(p)
	if err == nil {
		t.Fatal("expected an error for missing keys")
	}
	for _, want := range []string{"VOICE_CHANNEL_ID", "VOICE_OPERATOR_USER_ID", "VOICE_XO_AGENT", "VOICE_COST_CAP_USD"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name missing key %q: %v", want, err)
		}
	}
	// The error must NOT leak the secret value.
	if strings.Contains(err.Error(), "=k") || strings.Contains(err.Error(), "\bk\b") {
		t.Errorf("error leaked a value: %v", err)
	}
}

func TestLoadVoiceConfigBadCap(t *testing.T) {
	for _, bad := range []string{"abc", "0", "-1"} {
		p := writeEnv(t, "XAI_API_KEY=k\nVOICE_GUILD_ID=1\nVOICE_CHANNEL_ID=2\nVOICE_OPERATOR_USER_ID=3\nVOICE_XO_AGENT=x\nVOICE_COST_CAP_USD="+bad+"\n")
		if _, err := loadVoiceConfig(p); err == nil {
			t.Errorf("cap %q should be rejected", bad)
		}
	}
}

func TestLoadVoiceConfigMissingFile(t *testing.T) {
	if _, err := loadVoiceConfig(filepath.Join(t.TempDir(), "nope.env")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

// Cold-test the committed runtime template: deploy/voice.env.example must PARSE via the real
// loader (all required keys present, cap numeric) so the operator's copy-fill-run path can't
// silently rot. (go test CWD = cmd/flotilla, so deploy/ is two up.)
func TestVoiceEnvExampleParses(t *testing.T) {
	cfg, err := loadVoiceConfig("../../deploy/voice.env.example")
	if err != nil {
		t.Fatalf("deploy/voice.env.example does not parse via loadVoiceConfig: %v", err)
	}
	if cfg.XOAgent == "" || cfg.CapUSD <= 0 || cfg.XAIKey == "" {
		t.Errorf("template parsed but with empty/invalid fields: %+v", *cfg)
	}
}
