package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestResearchLibraryRendered822(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Research regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, _ := newTestServer(t, singleFleetRoster, time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC))
	root := t.TempDir()
	srv.cfg.ResearchPath = root
	writeResearchFixture(t, root, "authorization-domains-design.md", `# Authorization Domains — design for operator review

**Status:** DESIGN ONLY — no implementation without operator GO

## Goal

Make the review readable on the private dash.

## Decision checklist

- Confirm the boundary.
- Give design GO or request changes.

## Threat model

| Threat | Response |
|---|---|
| Raw HTML | Escape before rendering |

<script>window.RESEARCH_INJECTED = true</script>

[unsafe](javascript:window.RESEARCH_INJECTED=true)
`, time.Now())
	writeResearchFixture(t, root, "notes/field-note.md", "# Field note\n\n## Finding\n\nAn ordinary research note.\n", time.Now().Add(-time.Hour))

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
        phone = browser.new_page(viewport={"width": 390, "height": 844})
        phone.goto(url + "/research", wait_until="domcontentloaded")
        expect(phone.locator("#research-decisions")).to_be_visible()
        decision = phone.locator("#research-decision-list .research-card")
        expect(decision).to_have_count(1)
        expect(decision).to_contain_text("Authorization Domains")
        expect(phone.locator("#research-list .research-card")).to_have_count(1)
        expect(phone.locator("#research-list")).not_to_contain_text("Authorization Domains")
        decision.click()
        expect(phone.locator("#research-document")).to_be_visible()
        expect(phone.locator("#research-title")).to_contain_text("Authorization Domains")
        expect(phone.locator("#research-decision-strip")).to_be_visible()
        expect(phone.locator("#research-toc")).to_be_visible()
        expect(phone.locator(".research-table-wrap table")).to_be_visible()
        expect(phone.locator(".research-markdown script")).to_have_count(0)
        assert phone.evaluate("window.RESEARCH_INJECTED") is None
        assert "<script>" in phone.locator("#research-body").inner_text()
        assert phone.locator('#research-body a[href^="javascript:"]').count() == 0
        assert phone.evaluate("document.documentElement.scrollWidth === document.documentElement.clientWidth")
        if evidence_dir:
            phone.screenshot(path=os.path.join(evidence_dir, "research-decision-390.png"), full_page=True)
        phone.close()

        desktop = browser.new_page(viewport={"width": 1440, "height": 900})
        desktop.goto(url + "/research/notes/field-note.md", wait_until="domcontentloaded")
        expect(desktop.locator("#research-title")).to_have_text("Field note")
        expect(desktop.locator("#research-decision-strip")).to_be_hidden()
        expect(desktop.locator("#research-library")).to_be_visible()
        expect(desktop.locator("#research-reader")).to_be_visible()
        assert desktop.evaluate("document.documentElement.scrollWidth === document.documentElement.clientWidth")
        if evidence_dir:
            desktop.screenshot(path=os.path.join(evidence_dir, "research-library-1440.png"), full_page=True)
        desktop.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, evidenceDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Research library regression: %v\n%s", err, out)
	}
	if evidenceDir != "" {
		for _, name := range []string{"research-decision-390.png", "research-library-1440.png"} {
			path := filepath.Join(evidenceDir, name)
			if info, err := os.Stat(path); err != nil || info.Size() == 0 {
				t.Fatalf("rendered Research evidence missing at %q: %v", path, err)
			}
			t.Logf("generic rendered evidence: %s", path)
		}
	}
}
