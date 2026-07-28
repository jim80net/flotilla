package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestConversationsQueuedRendered856 exercises the real Conversations composer in
// Chromium. A typed queued result is an accepted durable message: the draft clears,
// the queue id is visible, and no optimistic "delivered" thread row is invented.
func TestConversationsQueuedRendered856(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Chromium regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(func() {
		httpServer.CloseClientConnections()
		httpServer.Close()
	})

	script := `
import json
import sys
from playwright.sync_api import sync_playwright, expect

url = sys.argv[1]
with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        page = browser.new_page(viewport={"width": 390, "height": 844})
        page.set_default_timeout(8000)
        page.add_init_script("window.EventSource = undefined")
        page.route("**/api/control/route", lambda route: route.fulfill(
            status=200, content_type="application/json",
            body=json.dumps({
                "outcome": "queued", "target": "xo",
                "queued_id": "queue-generic-1", "detail": "desk is busy"
            })))
        page.goto(url, wait_until="domcontentloaded")

        composer = page.locator("#thread-composer-input")
        expect(composer).to_be_visible()
        queued_text = "Queue this generic operator note."
        composer.fill(queued_text)
        page.locator("#thread-composer .thread-composer-send").click()

        expect(page.locator("#thread-composer-msg")).to_contain_text(
            "Queued durably for xo (id queue-generic-1)")
        expect(composer).to_have_value("")
        expect(page.locator("#conv-thread")).not_to_contain_text(queued_text)
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered queued Conversations regression: %v\n%s", err, out)
	}
}
