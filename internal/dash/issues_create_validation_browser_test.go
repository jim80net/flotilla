package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestIssuesCreateValidationRendered860 pins the operator-visible boundary for
// an incomplete New Issue attempt. The fixture is generic and never permits a
// write to leave the browser.
func TestIssuesCreateValidationRendered860(t *testing.T) {
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
import sys
from playwright.sync_api import sync_playwright, expect

url = sys.argv[1]
with sync_playwright() as p:
    browser = p.chromium.launch()
    page = browser.new_page(viewport={"width": 390, "height": 844})
    writes = []

    def issues(route):
        if route.request.method == "POST":
            writes.append(route.request.post_data)
            route.fulfill(status=500, content_type="application/json", body='{"error":"unexpected write"}')
        else:
            route.continue_()

    page.route("**/api/issues", issues)
    page.route("**/api/work-ledger*", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body='{"repo":"example/product","repos":[],"flotillas":[],"coverage":{"complete":true}}',
    ))
    page.add_init_script("window.EventSource = undefined")
    page.goto(url, wait_until="domcontentloaded")
    page.locator("#tab-issues").click()
    page.locator("#issues-new").evaluate("node => node.click()")

    title = page.locator("#create-title")
    error = page.locator("#create-title-error")
    submit = page.locator("#create-form button[type=submit]")

    expect(page.locator("#issues-create")).to_be_visible()
    expect(error).to_be_hidden()
    assert title.get_attribute("aria-invalid") is None
    assert page.evaluate("document.activeElement === document.querySelector('#create-title')")

    submit.click()
    expect(error).to_be_visible()
    expect(error).to_have_text("Enter a title before creating the issue.")
    expect(title).to_have_attribute("aria-invalid", "true")
    assert "create-title-error" in title.get_attribute("aria-describedby").split()
    assert page.evaluate("document.activeElement === document.querySelector('#create-title')")
    assert writes == [], "blank form attempted an issue write"

    boxes = page.evaluate("""() => {
      const field = document.querySelector('#create-title').getBoundingClientRect();
      const error = document.querySelector('#create-title-error').getBoundingClientRect();
      return {
        fieldBottom: field.bottom,
        errorTop: error.top,
        errorRight: error.right,
        viewportWidth: innerWidth,
        documentWidth: document.documentElement.scrollWidth
      };
    }""")
    assert boxes["errorTop"] >= boxes["fieldBottom"], boxes
    assert boxes["errorRight"] <= boxes["viewportWidth"], boxes
    assert boxes["documentWidth"] == boxes["viewportWidth"], boxes

    title.fill("Generic follow-up")
    expect(error).to_be_hidden()
    assert title.get_attribute("aria-invalid") is None
    assert writes == [], "correcting the field submitted without an operator action"
    browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered New Issue validation regression: %v\n%s", err, out)
	}
}
