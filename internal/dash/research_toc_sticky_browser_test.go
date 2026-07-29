package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestResearchTOCFinalEntryDismissRendered877 pins the mobile failure where
// bringing the final entry into view scrolls the details container itself and
// strands the summary/dismiss affordance above the bounded overlay.
func TestResearchTOCFinalEntryDismissRendered877(t *testing.T) {
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
	writeResearchFixture(t, root, "six-section-note.md", `# Six-section field note

## First section
Bounded generic evidence.

## Second section
Bounded generic evidence.

## Third section
Bounded generic evidence.

## Fourth section
Bounded generic evidence.

## Fifth section
Bounded generic evidence.

## Sixth section
Bounded generic evidence.
`, time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC))

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

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        for width, height in [(390, 844), (360, 800)]:
            page = browser.new_page(viewport={"width": width, "height": height})
            page.set_default_timeout(8000)
            page.add_init_script("window.EventSource = undefined")
            page.route("**/api/goals", lambda route: route.fulfill(
                status=200, content_type="application/json",
                body=json.dumps({"found": True, "goals": []})))
            page.goto(url + "/research/six-section-note.md", wait_until="domcontentloaded")

            expect(page.locator("#research-document")).to_be_visible()
            toc = page.locator("#research-toc")
            summary = page.locator("#research-toc > summary")
            links = page.locator("#research-toc-list a")
            expect(page.locator("#research-toc-count")).to_have_text("6 sections")
            expect(links).to_have_count(6)
            summary.click()
            expect(toc).to_have_attribute("open", "")

            # Move the bounded list to its final entry. The summary remains a
            # sticky sibling even when a browser also reveals the focused link
            # against the outer details scrollport.
            links.last.evaluate("""node => {
              node.focus();
              const list = node.closest('ol');
              list.scrollTop = list.scrollHeight;
              node.scrollIntoView({block:'nearest'});
            }""")
            expect(links.last).to_be_focused()
            metrics = toc.evaluate("""node => {
              const box = node.getBoundingClientRect();
              const control = node.querySelector('summary').getBoundingClientRect();
              const list = node.querySelector('ol');
              return {
                top: box.top, right: box.right, bottom: box.bottom, left: box.left,
                controlTop: control.top, controlBottom: control.bottom,
                controlPosition: getComputedStyle(node.querySelector('summary')).position,
                listOverflow: getComputedStyle(list).overflowY
              };
            }""")
            assert metrics["controlPosition"] == "sticky", metrics
            assert metrics["controlTop"] >= metrics["top"], metrics
            assert metrics["controlBottom"] <= metrics["bottom"], metrics
            assert metrics["left"] >= 0 and metrics["right"] <= width, metrics
            assert metrics["top"] >= 0 and metrics["bottom"] <= height, metrics
            assert metrics["listOverflow"] == "auto", metrics
            expect(summary).to_contain_text("Close")
            assert page.evaluate("document.documentElement.scrollWidth === document.documentElement.clientWidth")

            if evidence_dir:
                page.screenshot(path=os.path.join(evidence_dir, "research-toc-final-%d.png" % width))

            # The visible control closes immediately without resetting either
            # scrollport first, and native summary activation retains focus.
            summary.click()
            expect(toc).not_to_have_attribute("open", "")
            expect(summary).to_be_focused()

            # The keyboard contract remains available and restores the same
            # summary control after a final-entry reveal.
            summary.click()
            links.last.evaluate("""node => {
              node.focus();
              const list = node.closest('ol');
              list.scrollTop = list.scrollHeight;
            }""")
            page.keyboard.press("Escape")
            expect(toc).not_to_have_attribute("open", "")
            expect(summary).to_be_focused()
            page.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, evidenceDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered sticky Research TOC regression: %v\n%s", err, out)
	}
	for _, width := range []string{"390", "360"} {
		if evidenceDir == "" {
			break
		}
		path := filepath.Join(evidenceDir, "research-toc-final-"+width+".png")
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Fatalf("private TOC evidence missing: %s (%v)", path, err)
		}
		t.Logf("private generic sticky-TOC evidence: %s", path)
	}
}
