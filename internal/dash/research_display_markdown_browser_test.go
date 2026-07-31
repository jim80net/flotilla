package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestResearchDisplayMarkdownRendered905 protects the plain-text projection at
// both display boundaries: Learn cards and the opened reader title. The API
// continues to return the exact author-owned Markdown and its source digest.
func TestResearchDisplayMarkdownRendered905(t *testing.T) {
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
	markdown := `<!-- flotilla-publication
classification: research
reader-action: Compare the evidence before choosing the next reversible step.
support: material
-->
# **Readable** R&D ` + "`showpiece`" + ` with [one action](https://private.example/action)

Your **fleet** gets ` + "`plain evidence`" + ` and [one next action](javascript:alert(1)) without source syntax <script>window.privateToken = "nope"</script>.
`
	writeResearchFixture(t, root, "display-safe/SOURCE.md", markdown, time.Now())
	presentation := filepath.Join(root, "display-safe", "presentation", "index.html")
	if err := os.MkdirAll(filepath.Dir(presentation), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(presentation, []byte("<!doctype html><title>Display-safe showpiece</title><p>Generic evidence.</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

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
expected_title = "Readable R&D showpiece with one action"
expected_summary = "Your fleet gets plain evidence and one next action without source syntax."
raw_markers = ["**", chr(96), "](", "javascript:", "<script", "privateToken"]

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        for width, height in [(390, 844), (360, 800), (1440, 900)]:
            page = browser.new_page(viewport={"width": width, "height": height})
            page.set_default_timeout(8000)
            page.add_init_script("window.EventSource = undefined")
            page.route("**/api/goals", lambda route: route.fulfill(
                status=200, content_type="application/json",
                body=json.dumps({"found": True, "goals": []})))
            page.goto(url + "/research?focus=learn", wait_until="domcontentloaded")

            card = page.locator("#research-learn-list .research-card")
            expect(card).to_have_count(1)
            expect(card.locator("strong")).to_have_text(expected_title)
            expect(card.locator(".research-card-summary")).to_have_text(expected_summary)
            card_copy = card.inner_text()
            for marker in raw_markers:
                assert marker not in card_copy, (width, marker, card_copy)

            card.click()
            expect(page.locator("#research-title")).to_have_text(expected_title)
            for marker in raw_markers:
                assert marker not in page.locator("#research-title").inner_text(), (width, marker)

            source = page.request.get(url + "/api/research/display-safe/SOURCE.md")
            assert source.ok, source.status
            payload = source.json()
            assert "**Readable**" in payload["markdown"], payload["markdown"]
            assert "[one action](https://private.example/action)" in payload["markdown"]
            assert payload["digest"].startswith("sha256:"), payload["digest"]

            metrics = page.evaluate("""() => {
              const card = document.querySelector('#research-learn-list .research-card');
              const title = document.querySelector('#research-title').getBoundingClientRect();
              const body = document.documentElement;
              return {
                scroll: body.scrollWidth, client: body.clientWidth,
                title: {left: title.left, right: title.right},
                scriptCount: document.querySelectorAll('script[src*="private"], #research-title script, .research-card script').length,
                cardRight: card ? card.getBoundingClientRect().right : 0
              };
            }""")
            assert metrics["scroll"] == metrics["client"], (width, metrics)
            assert metrics["title"]["left"] >= 0 and metrics["title"]["right"] <= width, (width, metrics)
            assert metrics["scriptCount"] == 0, metrics
            page.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Research Markdown display regression: %v\n%s", err, out)
	}
}
