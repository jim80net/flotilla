package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestResearchPublicationDiagnosticsRendered858 keeps Phase 1 honest in the
// rendered UI: invalid files remain reachable while diagnostics, explicit
// archival intent, and decision classification are visible at phone and desktop.
func TestResearchPublicationDiagnosticsRendered858(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Research regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	root := t.TempDir()
	srv.cfg.ResearchPath = root
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	writeResearchFixture(t, root, "valid.md", `<!-- flotilla-publication
classification: research
reader-action: Compare the evidence and choose the next experiment.
support: material
-->
# Valid evidence

This generic report contains substantive evidence for the operator.

[Supporting dataset](evidence.csv)
`, now)
	writeResearchFixture(t, root, "archival.md", `<!-- flotilla-publication
classification: archival
reader-action: Retain this rationale as historical context.
support: text-only
support-rationale: The contemporaneous rationale is the complete historical record.
-->
# Archival rationale

This generic note records why the reversible example was retained.
`, now.Add(-time.Minute))
	writeResearchFixture(t, root, "decision.md", `<!-- flotilla-publication
classification: decision
reader-action: Decide whether to run the reversible generic trial.
support: text-only
support-rationale: The bounded choice and rollback are fully stated here.
-->
# Reversible trial

## Recommendation

Keep the trial frozen until the operator decides.
`, now.Add(-2*time.Minute))
	writeResearchFixture(t, root, "invalid.md", "# Title-only publication\n", now.Add(-3*time.Minute))

	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(func() {
		httpServer.CloseClientConnections()
		httpServer.Close()
	})

	script := `
import sys
from playwright.sync_api import sync_playwright, expect

url = sys.argv[1]

def open_library(browser, width, height):
    page = browser.new_page(viewport={"width": width, "height": height})
    page.set_default_timeout(8000)
    page.add_init_script("window.EventSource = undefined")
    page.goto(url + "/research", wait_until="domcontentloaded")
    expect(page.locator("#research-diagnostics")).to_be_visible()
    expect(page.locator("#research-diagnostics-count")).to_have_text("1 of 4")
    expect(page.locator("#research-diagnostics-summary")).to_contain_text("all 4 remain visible")
    expect(page.locator("#research-diagnostics-breakdown")).to_contain_text("Title only")
    expect(page.locator("#research-diagnostics-breakdown")).to_contain_text("Missing reader action")
    expect(page.locator("#research-diagnostics-breakdown")).to_contain_text("Missing support")
    expect(page.locator("#research-decision-count")).to_have_text("1 waiting")
    expect(page.locator(".research-card")).to_have_count(4)
    return page

def open_card(page, title):
    card = page.locator(".research-card").filter(has_text=title)
    expect(card).to_be_visible()
    card.click()
    expect(page.locator("#research-document")).to_be_visible()
    expect(page.locator("#research-title")).to_have_text(title)
    return card

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        for width, height in [(390, 844), (1440, 900)]:
            page = open_library(browser, width, height)
            invalid = page.locator(".research-card").filter(has_text="Title-only publication")
            assert "has-diagnostics" in invalid.get_attribute("class")
            expect(invalid).to_contain_text("3 publication checks")
            invalid.click()
            expect(page.locator("#research-publication-result")).to_have_text("3 checks")
            expect(page.locator("#research-reader-action")).to_have_text("Reader action not declared.")
            expect(page.locator("#research-document-diagnostics")).to_contain_text("Title only")
            expect(page.locator("#research-document-diagnostics")).to_contain_text("Missing reader action")
            expect(page.locator("#research-document-diagnostics")).to_contain_text("Missing support")
            # Phase 1 measures; the invalid file still has a canonical reader URL.
            assert page.url.endswith("/research/invalid.md"), page.url
            page.locator("#research-back").click() if width == 390 else page.goto(url + "/research", wait_until="domcontentloaded")

            open_card(page, "Valid evidence")
            expect(page.locator("#research-publication-result")).to_have_text("Valid")
            expect(page.locator("#research-reader-action")).to_contain_text("Compare the evidence")
            expect(page.locator("#research-body")).not_to_contain_text("flotilla-publication")
            page.locator("#research-back").click() if width == 390 else page.goto(url + "/research", wait_until="domcontentloaded")

            open_card(page, "Archival rationale")
            expect(page.locator("#research-document-status")).to_have_text("Archival")
            expect(page.locator("#research-publication-result")).to_have_text("Valid")
            expect(page.locator("#research-reader-action")).to_contain_text("historical context")
            page.locator("#research-back").click() if width == 390 else page.goto(url + "/research", wait_until="domcontentloaded")

            decision = page.locator("#research-decision-list .research-card").filter(has_text="Reversible trial")
            expect(decision).to_be_visible()
            decision.click()
            expect(page.locator("#research-document-status")).to_have_text("Decision")
            expect(page.locator("#research-publication-result")).to_have_text("Valid")
            expect(page.locator("#research-decision-strip")).to_be_visible()
            expect(page.locator("#research-reader-action")).to_contain_text("Decide whether")

            metrics = page.evaluate("""() => ({
              scroll: document.documentElement.scrollWidth,
              client: document.documentElement.clientWidth,
              publication: (() => { const b = document.querySelector('#research-publication-state').getBoundingClientRect(); return {left:b.left,right:b.right,width:b.width}; })()
            })""")
            assert metrics["scroll"] == metrics["client"], (width, metrics)
            assert metrics["publication"]["left"] >= 0 and metrics["publication"]["right"] <= width, (width, metrics)
            page.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Research publication diagnostics regression: %v\n%s", err, out)
	}
}
