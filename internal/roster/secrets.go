package roster

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Secrets holds credentials loaded from an env-style file that must never be
// committed (chmod 600). Recognised keys:
//
//	FLOTILLA_BOT_TOKEN=...                  the Discord bot token (setup + the
//	                                        future inbound reader)
//	FLOTILLA_WEBHOOK_<AGENT>=https://...    per-agent webhook url; <AGENT> is the
//	                                        agent name upper-cased with '-' -> '_'
type Secrets struct {
	vals map[string]string
}

// LoadSecrets parses an env-style KEY=VALUE file. Blank lines and lines
// beginning with '#' are ignored.
func LoadSecrets(path string) (*Secrets, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open secrets %q: %w", path, err)
	}
	defer f.Close()

	vals := make(map[string]string)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			// A non-blank, non-comment line with no '=' is malformed. Reject it
			// rather than silently dropping what may be a credential.
			return nil, fmt.Errorf("secrets %q: malformed line (expected KEY=VALUE): %q", path, line)
		}
		vals[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read secrets %q: %w", path, err)
	}
	return &Secrets{vals: vals}, nil
}

// BotToken returns the Discord bot token, or "" if absent.
func (s *Secrets) BotToken() string {
	return s.vals["FLOTILLA_BOT_TOKEN"]
}

// WebhookKey derives the env key holding an agent's webhook url.
func WebhookKey(agent string) string {
	return "FLOTILLA_WEBHOOK_" + strings.ToUpper(strings.ReplaceAll(agent, "-", "_"))
}

// Webhook returns the webhook url for an agent.
func (s *Secrets) Webhook(agent string) (string, error) {
	key := WebhookKey(agent)
	url, ok := s.vals[key]
	if !ok || url == "" {
		return "", fmt.Errorf("no webhook for %q (expected %s in secrets file)", agent, key)
	}
	return url, nil
}
