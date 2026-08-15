package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestIssuesExplicitTerminalStatesRendered(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Chromium regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(func() { httpServer.CloseClientConnections(); httpServer.Close() })

	script := `
import json
import sys
from playwright.sync_api import sync_playwright, expect

url = sys.argv[1]
cases = [
    ("empty", 200, {"repo":"example/product","repos":["example/product"],"flotillas":[],"coverage":{"complete":True,"expected_repos":1,"indexed_repos":["example/product"]}}, "No fleet work matches this view"),
    ("no-repo", 503, {"error":"no GitHub repo configured", "code":"no-repo"}, "Work ledger not configured"),
    ("load-error", 502, {"error":"could not reach repository service", "code":"network"}, "Work ledger unavailable"),
]
with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        for state, status, body, title in cases:
            page = browser.new_page(viewport={"width":390,"height":844})
            page.add_init_script("window.EventSource = undefined")
            def ledger(route):
                route.fulfill(status=status, content_type="application/json", body=json.dumps(body))
            page.route("**/api/work-ledger*", ledger)
            page.goto(url, wait_until="domcontentloaded")
            page.locator("#tab-issues").click()
            terminal = page.locator('[data-issue-terminal="%s"]' % state)
            expect(terminal).to_be_visible()
            expect(terminal.locator("strong")).to_have_text(title)
            expect(page.locator("#issues-list")).not_to_contain_text("Loading fleet work ledger")
            if state == "load-error":
                expect(terminal.locator("[data-issues-retry]")).to_be_visible()
            else:
                expect(terminal.locator("[data-issues-retry]")).to_have_count(0)
            page.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Issues terminal regression: %v\n%s", err, out)
	}
}
