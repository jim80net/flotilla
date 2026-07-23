package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestDecisionsBoundedRendered848 exercises the real dashboard at both phone contracts.
// Fixtures are deliberately generic; screenshots are never written by this test.
func TestDecisionsBoundedRendered848(t *testing.T) {
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
long_tail = "\n\n## Mechanics\n" + ("Reversible generic detail with no production identifiers. " * 32)
goals = []
for i in range(7):
    goals.append({
        "id": "generic-%d" % (i + 1),
        "title": "Generic decision %d" % (i + 1),
        "owner": "example-desk",
        "conversation_agent": "example-desk",
        "status_display": "awaiting",
        "brief": "## What it is\nA generic choice.\n\n## Recommendation\nChoose reversible option %d.\n\n## Safe default\nHold the current state.%s" % (i + 1, long_tail),
        "work_items": [], "children": [], "parent": None
    })
doc = {"found": True, "goals": goals, "counts": {
    "fleet": 7, "total": 7, "in_flight": 0, "awaiting": 7,
    "pending": 0, "realized": 0, "aspirational": 0
}}

def open_decisions(browser, width, height, body, status=200):
    page = browser.new_page(viewport={"width": width, "height": height})
    page.set_default_timeout(8000)
    page.add_init_script("window.EventSource = undefined")
    page.route("**/api/goals", lambda route: route.fulfill(
        status=status, content_type="application/json", body=json.dumps(body)))
    page.goto(url, wait_until="domcontentloaded")
    page.locator("#tab-decisions").click()
    return page

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        for width, height in [(390, 844), (360, 800)]:
            page = open_decisions(browser, width, height, doc)
            expect(page.locator("#gdec-title")).to_have_text("Decisions awaiting you · 7")
            expect(page.locator(".gdec-summary")).to_have_count(3)
            expect(page.locator(".gdec-brief")).to_have_count(0)
            expect(page.locator(".gdec-summary").first.locator("dt")).to_have_count(4)
            expect(page.locator(".gdec-summary").first).to_contain_text("Choose reversible option 1")
            expect(page.locator(".gdec-summary").first).to_contain_text("Hold the current state")
            page_bottom = page.locator("#gdec-list").bounding_box()["y"] + page.locator("#gdec-list").bounding_box()["height"]
            assert page_bottom <= height * 2, (width, height, page_bottom)

            # Activation 1 opens the complete index; activation 2 reaches the terminal brief.
            page.locator("[data-gdec-jump]").click()
            final_jump = page.locator(".gdec-jump").last
            expect(final_jump).to_be_visible()
            final_jump.click()
            before_y = page.evaluate("window.scrollY")
            expect(page.locator("#gdec-detail")).to_be_visible()
            expect(page.locator("#gdec-detail-count")).to_have_text("Decision 7 of 7")
            expect(page.locator("#gdec-detail-body .gdec-brief")).to_contain_text("Reversible generic detail")
            expect(page.locator("#gdec-detail-body .gdec-resp-input")).to_be_visible()
            box = page.locator("#gdec-detail").bounding_box()
            assert box["x"] >= 0 and box["x"] + box["width"] <= width, ("dialog x", width, box)
            assert box["y"] >= 0 and box["y"] + box["height"] <= height, ("dialog y", height, box)
            page.locator("[data-gdec-detail-close]").click()
            expect(page.locator("#gdec-detail")).not_to_be_visible()
            expect(final_jump).to_be_focused()
            page.wait_for_timeout(50)
            assert page.evaluate("window.scrollY") == before_y, ("return scroll", before_y, page.evaluate("window.scrollY"))
            page.close()

        unavailable = open_decisions(browser, 390, 844, {"error": "generic unavailable"}, 503)
        expect(unavailable.locator("[data-gdec-retry]")).to_be_visible()
        unavailable.unroute("**/api/goals")
        unavailable.route("**/api/goals", lambda route: route.fulfill(
            status=200, content_type="application/json", body=json.dumps(doc)))
        unavailable.locator("[data-gdec-retry]").click()
        expect(unavailable.locator(".gdec-summary")).to_have_count(3)
        unavailable.close()

        empty = open_decisions(browser, 390, 844, {
            "found": True, "goals": [], "counts": {
                "fleet": 0, "total": 0, "in_flight": 0, "awaiting": 0,
                "pending": 0, "realized": 0, "aspirational": 0
            }})
        expect(empty.locator("#gdec-list")).to_contain_text("Nothing is awaiting your decision")
        empty.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered bounded Decisions regression: %v\n%s", err, out)
	}
}
