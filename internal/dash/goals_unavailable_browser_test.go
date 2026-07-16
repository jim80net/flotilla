package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGoalsUnavailableRenderContract761(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	js := doGet(t, srv, "/static/goals.js").Body.String()
	css := doGet(t, srv, "/static/dash.css").Body.String()
	for _, marker := range []string{
		"function setGoalsAvailability",
		"Goals are unavailable right now. Refresh this page or wait for the next live update to retry.",
		`compact.textContent = "unavailable"`,
		`hdrBtn.setAttribute("aria-label", "Decisions unavailable until goals data reloads")`,
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js missing unavailable-state contract %q", marker)
		}
	}
	for _, marker := range []string{
		".goals-done.is-unavailable > summary",
		".goals-mobile-summary.is-unavailable .goals-situation",
	} {
		if !strings.Contains(css, marker) {
			t.Errorf("dash.css missing unavailable-state contract %q", marker)
		}
	}
}

// TestGoalsUnavailableRendered761 exercises both sides of the truth boundary:
// an HTTP 5xx must never look empty, while a loaded empty document may show real
// zero counts. All browser data is generic and synthetic.
func TestGoalsUnavailableRendered761(t *testing.T) {
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
	evidenceDir := os.Getenv("FLOTILLA_BROWSER_EVIDENCE_DIR")
	if evidenceDir != "" {
		if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	script := `
import json
import os
import sys
from playwright.sync_api import sync_playwright, expect

url, evidence_dir = sys.argv[1], sys.argv[2]
empty_doc = {"found": True, "goals": [], "counts": {
    "fleet": 0, "total": 0, "in_flight": 0, "awaiting": 0,
    "pending": 0, "realized": 0, "aspirational": 0
}}

def open_goals(browser, response_status, response_body):
    page = browser.new_page(viewport={"width": 390, "height": 844})
    page.set_default_timeout(8000)
    page.add_init_script("window.EventSource = undefined")
    page.route("**/api/goals", lambda route: route.fulfill(
        status=response_status, content_type="application/json", body=json.dumps(response_body)))
    page.goto(url, wait_until="domcontentloaded")
    page.locator("#tab-goals").click()
    return page

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        unavailable = open_goals(browser, 503, {"error": "synthetic unavailable"})
        error = unavailable.locator("#goals-empty")
        expect(error).to_be_visible()
        expect(error).to_have_text("Goals are unavailable right now. Refresh this page or wait for the next live update to retry.")
        expect(unavailable.locator(".gtile")).to_have_count(0)
        expect(unavailable.locator("#goals-mobile-summary-count")).to_have_text("unavailable")
        expect(unavailable.locator("#goals-done-count")).to_have_text("unavailable")
        expect(unavailable.locator("#goals-done")).to_have_attribute("aria-disabled", "true")
        expect(unavailable.locator("#goals-done-list")).to_be_empty()
        expect(unavailable.locator("#tab-decisions")).to_have_attribute("aria-label", "Decisions unavailable until goals data reloads")
        if evidence_dir:
            unavailable.screenshot(path=os.path.join(evidence_dir, "unavailable-390.png"))
        unavailable.locator("#tab-decisions").click()
        expect(unavailable.locator("#gdec-list")).to_contain_text("goals data is unavailable")
        unavailable.unroute("**/api/goals")
        unavailable.route("**/api/goals", lambda route: route.fulfill(
            status=200, content_type="application/json", body=json.dumps(empty_doc)))
        unavailable.evaluate("() => window.flotillaGoals.refresh()")
        expect(unavailable.locator("#gdec-list")).to_contain_text("Nothing is awaiting your decision")
        expect(unavailable.locator("#tab-decisions")).not_to_have_attribute("aria-label", "Decisions unavailable until goals data reloads")
        unavailable.close()

        honest_empty = open_goals(browser, 200, empty_doc)
        expect(honest_empty.locator("#goals-empty")).to_have_text("No goals defined yet.")
        expect(honest_empty.locator(".gtile")).to_have_count(6)
        expect(honest_empty.locator("#goals-mobile-summary-count")).to_have_text("0 goals · 0 in flight · 0 awaiting")
        expect(honest_empty.locator("#goals-done-count")).to_be_empty()
        expect(honest_empty.locator("#goals-done-list")).to_contain_text("No realized goals yet.")
        expect(honest_empty.locator("#goals-done")).not_to_have_attribute("aria-disabled", "true")
        expect(honest_empty.locator("#tab-decisions")).not_to_have_attribute("aria-label", "Decisions unavailable until goals data reloads")
        if evidence_dir:
            honest_empty.screenshot(path=os.path.join(evidence_dir, "honest-empty-390.png"))
        honest_empty.locator("#tab-decisions").click()
        expect(honest_empty.locator("#gdec-list")).to_contain_text("Nothing is awaiting your decision")
        honest_empty.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, evidenceDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Goals unavailable regression: %v\n%s", err, out)
	}
	if evidenceDir != "" {
		for _, name := range []string{"unavailable-390.png", "honest-empty-390.png"} {
			path := filepath.Join(evidenceDir, name)
			if info, err := os.Stat(path); err != nil || info.Size() == 0 {
				t.Fatalf("rendered evidence missing at %q: %v", path, err)
			}
			t.Logf("generic rendered evidence: %s", path)
		}
	}
}
