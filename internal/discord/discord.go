// Package discord mirrors coordination traffic to a Discord channel so there
// is a durable, readable-back audit trail.
//
// v0 is send-only: each agent posts under its own webhook identity, which needs
// no bot gateway. v0.1 will ADD inbound reading (the operator typing in the
// channel) via the bot gateway; that path MUST filter — act only on the
// configured operator user id (roster.Config.OperatorUserID) and ignore the
// channel's own webhook posts, so the channel cannot feed back on itself. The
// bot token for that reader is carried in roster.Secrets; this file stays
// send-only until then.
package discord

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// maxContentRunes is Discord's hard limit on webhook message content length.
const maxContentRunes = 2000

// webhookPayload is the subset of Discord's execute-webhook body flotilla sends.
type webhookPayload struct {
	Username string `json:"username,omitempty"`
	Content  string `json:"content"`
}

// httpClient is the shared client for webhook posts.
var httpClient = &http.Client{Timeout: 10 * time.Second}

// Post sends content to a Discord webhook, displayed under username. This is the
// v0 audit-mirror primitive. Discord returns 204 No Content on success.
//
// The webhook URL is itself a credential (anyone holding it can post as that
// identity), so it is never allowed to appear in a returned error: the URL is
// validated up front (a malformed URL yields a content-free error), and any
// transport error is reduced to its URL-free underlying cause.
func Post(webhookURL, username, content string) error {
	parsed, err := url.Parse(webhookURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		// Deliberately generic — echoing the URL here would leak the credential.
		return errors.New("invalid webhook URL")
	}

	body, err := json.Marshal(webhookPayload{Username: username, Content: clampContent(content)})
	if err != nil {
		return fmt.Errorf("encode webhook payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request for host %s: %w", parsed.Host, urlFreeCause(err))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post to webhook host %s: %w", parsed.Host, urlFreeCause(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("webhook returned %s: %s", resp.Status, snippet)
	}
	return nil
}

// clampContent trims content to Discord's 2000-character limit on a rune
// boundary, appending an ellipsis so truncation is visible in the audit trail.
func clampContent(s string) string {
	r := []rune(s)
	if len(r) <= maxContentRunes {
		return s
	}
	return string(r[:maxContentRunes-1]) + "…"
}

// urlFreeCause unwraps a *url.Error to its underlying cause, which does not carry
// the (secret) URL. The wrapper's Error() string embeds the full URL — escaped
// via strconv.Quote, so a literal string-scrub is unreliable — so we drop the
// wrapper entirely and keep only the cause (a dial/timeout error, etc.).
func urlFreeCause(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
