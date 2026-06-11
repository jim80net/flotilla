package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// voiceConfig is the `flotilla voice` runtime configuration, loaded from the voice env file
// (state/voice.env — chmod 600, never committed; the operator copies XAI_API_KEY there from
// ~/.hermes/.env, decoupling voice from Hermes). It is parsed here (pure-Go, no build tag) so
// the loader is unit-tested in the core build; the voiceopus command consumes it.
//
// XAIKey is a secret and MUST never be logged or echoed (loadVoiceConfig never prints values,
// only missing KEY NAMES).
type voiceConfig struct {
	XAIKey         string  // XAI_API_KEY (Grok STT/TTS)
	GuildID        string  // VOICE_GUILD_ID
	ChannelID      string  // VOICE_CHANNEL_ID (the voice channel to join)
	OperatorUserID string  // VOICE_OPERATOR_USER_ID (the only voice trusted for injection)
	XOAgent        string  // VOICE_XO_AGENT (roster name whose pane transcripts inject into)
	CapUSD         float64 // VOICE_COST_CAP_USD (session spend cap)
}

// loadVoiceConfig parses a KEY=VALUE env file into a voiceConfig, validating that every
// required key is present and the cap parses as a number. On a missing/invalid field it
// returns an error naming the KEY (never the value).
func loadVoiceConfig(path string) (*voiceConfig, error) {
	kv, err := parseEnvFile(path)
	if err != nil {
		return nil, err
	}
	var missing []string
	get := func(key string) string {
		v, ok := kv[key]
		if !ok || strings.TrimSpace(v) == "" {
			missing = append(missing, key)
		}
		return strings.TrimSpace(v)
	}
	cfg := &voiceConfig{
		XAIKey:         get("XAI_API_KEY"),
		GuildID:        get("VOICE_GUILD_ID"),
		ChannelID:      get("VOICE_CHANNEL_ID"),
		OperatorUserID: get("VOICE_OPERATOR_USER_ID"),
		XOAgent:        get("VOICE_XO_AGENT"),
	}
	capRaw := get("VOICE_COST_CAP_USD")
	if len(missing) > 0 {
		return nil, fmt.Errorf("voice config %s missing required keys: %s", path, strings.Join(missing, ", "))
	}
	cap64, err := strconv.ParseFloat(capRaw, 64)
	if err != nil || cap64 <= 0 {
		return nil, fmt.Errorf("voice config %s: VOICE_COST_CAP_USD must be a positive number", path)
	}
	cfg.CapUSD = cap64
	return cfg, nil
}

// parseEnvFile reads a KEY=VALUE file, honoring `export ` prefixes, surrounding quotes, blank
// lines, and `#` comments. Values may contain `=`.
func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open voice config: %w", err)
	}
	defer f.Close()
	kv := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		val = strings.Trim(val, `"'`)
		kv[key] = val
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read voice config: %w", err)
	}
	return kv, nil
}
