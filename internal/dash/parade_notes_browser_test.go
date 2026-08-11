package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestParadeSpeakerNotesRendered907 pins the three presentation depths:
// outline, a separately controlled narrative, and safe evidence links.
func TestParadeSpeakerNotesRendered907(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Parade regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, dir := newTestServer(t, singleFleetRoster, time.Date(2026, 7, 30, 16, 0, 0, 0, time.UTC))
	paradeDir := filepath.Join(dir, "parades", "2026-07-30")
	if err := os.MkdirAll(paradeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	deck := `# Alpha XO · Bounded release

- clean candidate
- rollback manifest

:::notes
The narrative explains why the outline matters.

---

[Open the evidence](/research/evidence.md)
:::
:::

---

# Legacy slide

This slide has no authored narrative and must still work.

---

:::notes
# This narrative must never become the outline title
:::`
	if err := os.WriteFile(filepath.Join(paradeDir, "slides.md"), []byte(deck), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.cfg.ParadesPath = filepath.Join(dir, "parades")

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
import os
import sys
from playwright.sync_api import sync_playwright, expect

url, evidence_dir = sys.argv[1], sys.argv[2]

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        desktop = browser.new_page(viewport={"width": 1440, "height": 900})
        desktop.add_init_script("window.EventSource = undefined")
        desktop.goto(url + "/parade", wait_until="domcontentloaded")

        expect(desktop.locator("#pd-counter")).to_have_text("1 / 3")
        expect(desktop.locator(".pd-slide-title")).to_have_text("Bounded release")
        expect(desktop.locator(".pd-slide-body")).to_contain_text("clean candidate")
        expect(desktop.locator(".pd-slide-body")).not_to_contain_text("narrative explains")
        expect(desktop.locator("#pd-notes")).to_be_visible()
        expect(desktop.locator("#pd-notes-body")).to_contain_text("narrative explains")
        expect(desktop.locator("#pd-notes-body a")).to_have_attribute("href", "/research/evidence.md")
        expect(desktop.locator("#pd-notes-toggle")).to_have_attribute("aria-expanded", "true")
        desktop.locator("#pd-conversation").evaluate("node => node.open = true")
        expect(desktop.locator("#pd-conversation")).to_be_visible()
        rendered_copy = desktop.locator(".pd-slide-title, .pd-slide-body, #pd-notes-body").all_inner_texts()
        assert all(":::notes" not in text and text.strip() != ":::" for text in rendered_copy), rendered_copy
        assert desktop.locator("#pd-notes").evaluate(
            "node => getComputedStyle(node).backgroundColor") == "rgb(255, 248, 236)"
        assert desktop.evaluate("document.documentElement.scrollWidth === document.documentElement.clientWidth")
        if evidence_dir:
            desktop.screenshot(path=os.path.join(evidence_dir, "parade-notes-desktop.png"))

        desktop.locator("#pd-notes-close").click()
        expect(desktop.locator("#pd-notes")).to_be_hidden()
        expect(desktop.locator("#pd-notes-toggle")).to_be_focused()
        desktop.locator("#pd-next").click()
        expect(desktop.locator("#pd-counter")).to_have_text("2 / 3")
        expect(desktop.locator(".pd-slide-title")).to_have_text("Legacy slide")
        desktop.locator("#pd-notes-toggle").click()
        expect(desktop.locator("#pd-notes-body")).to_contain_text("No speaker notes")

        desktop.locator("#pd-notes-close").click()
        desktop.locator("#pd-next").click()
        expect(desktop.locator("#pd-counter")).to_have_text("3 / 3")
        expect(desktop.locator(".pd-slide-title")).to_have_text("Untitled slide")
        expect(desktop.locator(".pd-slide-title")).not_to_contain_text("narrative")
        expect(desktop.locator(".pd-slide-body")).not_to_contain_text("narrative")
        expect(desktop.locator(".pd-slide-body")).not_to_contain_text(":::")
        desktop.locator("#pd-notes-toggle").click()
        expect(desktop.locator("#pd-notes-body")).to_contain_text("Notes unavailable")
        expect(desktop.locator("#pd-notes-body")).not_to_contain_text("never become")
        desktop.close()

        mobile = browser.new_page(viewport={"width": 390, "height": 844})
        mobile.add_init_script("window.EventSource = undefined")
        mobile.goto(url + "/parade", wait_until="domcontentloaded")
        expect(mobile.locator("#pd-counter")).to_have_text("1 / 3")
        expect(mobile.locator("#pd-notes")).to_be_hidden()
        expect(mobile.locator("#pd-slide")).to_be_visible()
        mobile.locator("#pd-notes-toggle").click()
        expect(mobile.locator("#pd-notes")).to_be_visible()
        expect(mobile.locator("#pd-slide")).to_be_hidden()
        expect(mobile.locator("#pd-notes-body")).to_contain_text("narrative explains")
        expect(mobile.locator("#pd-prev")).to_be_visible()
        expect(mobile.locator("#pd-next")).to_be_visible()
        assert mobile.evaluate("document.documentElement.scrollWidth === document.documentElement.clientWidth")
        if evidence_dir:
            mobile.screenshot(path=os.path.join(evidence_dir, "parade-notes-390.png"))
        mobile.locator("#pd-notes-close").click()
        expect(mobile.locator("#pd-slide")).to_be_visible()
        expect(mobile.locator("#pd-notes-toggle")).to_be_focused()
        mobile.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, evidenceDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Parade speaker notes regression: %v\n%s", err, out)
	}
}
