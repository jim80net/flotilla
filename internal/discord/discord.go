// Package discord mirrors coordination traffic to a Discord channel so there
// is a durable, readable-back audit trail.
//
// v0 is send-only: each agent posts under its own webhook identity, which needs
// no bot gateway. v0.1 will ADD inbound reading (the operator typing in the
// channel) via the bot gateway; that path MUST filter — act only on the
// configured operator user id (roster.Config.OperatorUserID) and ignore the
// channel's own webhook posts, so the bus cannot feed back on itself. The bot
// token for that reader is carried in roster.Secrets; this file stays send-only
// until then.
package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// webhookPayload is the subset of Discord's execute-webhook body flotilla sends.
type webhookPayload struct {
	Username string `json:"username,omitempty"`
	Content  string `json:"content"`
}

// httpClient is the shared client for webhook posts.
var httpClient = &http.Client{Timeout: 10 * time.Second}

// Post sends content to a Discord webhook, displayed under username. This is
// the v0 audit-mirror primitive. Discord returns 204 No Content on success.
func Post(webhookURL, username, content string) error {
	body, err := json.Marshal(webhookPayload{Username: username, Content: content})
	if err != nil {
		return fmt.Errorf("encode webhook payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post to webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("webhook returned %s: %s", resp.Status, snippet)
	}
	return nil
}
