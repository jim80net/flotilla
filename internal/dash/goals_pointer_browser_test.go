package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestGoalsActionMenuPointerRendered exercises Chromium hit-testing against the
// real dashboard assets. Set FLOTILLA_PLAYWRIGHT_PYTHON to a Python executable
// with playwright installed; ordinary Go-only environments skip this rendered
// layer while retaining the server/unit suite.
func TestGoalsActionMenuPointerRendered(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Chromium regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, dir := newTestServer(t, singleFleetRoster, time.Now())
	goals := `{
  "version": 1,
  "goals": [{
    "id": "alpha-goal",
    "title": "Alpha launch decision",
    "description": "Choose the safe launch window for the example service.",
    "scope": "project",
    "work_items": [{
      "kind": "backlog",
      "match": "alpha launch approval",
      "label": "Approve the launch window",
      "brief": "What it is — a generic launch-window choice.\n\nValue — protects the example rollout.\n\nRecommendation — approve the reversible staged option."
    }]
  }]
}`
	if err := os.WriteFile(filepath.Join(dir, "fleet-goals.json"), []byte(goals), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".flotilla-state.md"), []byte("## Backlog\n- [awaiting-auth] alpha launch approval\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// httptest chooses a random port outside the production Host allowlist; the
	// browser regression targets the already-wired mux while hostAllow remains
	// covered by its dedicated server tests.
	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(func() {
		httpServer.CloseClientConnections()
		httpServer.Close()
	})

	screenshot := os.Getenv("FLOTILLA_BROWSER_SCREENSHOT")
	script := `
import sys
from playwright.sync_api import sync_playwright, expect

url, screenshot = sys.argv[1], sys.argv[2]
with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        page = browser.new_page(viewport={"width": 1440, "height": 900})
        page.set_default_timeout(8000)
        # Keep the opt-in test server finite: production SSE behavior has its own
        # tests and would intentionally hold httptest.Server open during cleanup.
        page.add_init_script("window.EventSource = undefined")
        # The dashboard intentionally keeps /events open; DOM readiness, followed by
        # a real node selector, is the deterministic rendered-page boundary.
        page.goto(url, wait_until="domcontentloaded")
        page.locator("#tab-goals").click()
        card = page.locator('.gnode[data-id="alpha-goal"]')
        expect(card).to_be_visible()
        kebab = card.locator(".gnode-kebab")
        kebab.click()
        menu = card.locator(".gnode-pop")
        expect(menu).to_be_visible()
        menu.locator('[data-gnode-action="respond"]').click()
        modal = page.locator("#goals-modal")
        expect(modal).to_have_class("goals-modal open")
        expect(page.locator("#goals-modal-input")).to_be_focused()
        if screenshot:
            page.screenshot(path=screenshot, full_page=True)
        page.keyboard.press("Escape")
        expect(modal).not_to_have_class("goals-modal open")
        expect(kebab).to_be_focused()
        card.locator(".gnode-title").click()
        expect(page.locator('.gnode[data-id="alpha-goal"].gnode-selected')).to_be_visible()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, screenshot)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered pointer regression: %v\n%s", err, out)
	}
	if screenshot != "" {
		if info, err := os.Stat(screenshot); err != nil || info.Size() == 0 {
			t.Fatalf("rendered evidence missing at %q: %v", screenshot, err)
		}
		t.Logf("generic rendered evidence: %s", screenshot)
	}
}
