package dash

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	var design strings.Builder
	design.WriteString(`# Authorization Domains — design for operator review

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
`)
	design.WriteString("```shell\nresearch-library-command-with-a-deliberately-long-unbroken-value-abcdefghijklmnopqrstuvwxyz-0123456789-abcdefghijklmnopqrstuvwxyz-0123456789\n```\n\n")
	design.WriteString(`
<script>window.RESEARCH_INJECTED = true</script>

[unsafe](javascript:window.RESEARCH_INJECTED=true)
`)
	for i := 4; i <= 33; i++ {
		design.WriteString("\n## Review section " + fmt.Sprintf("%02d", i) + "\n\nMeasured review content.\n")
	}
	now := time.Now()
	writeResearchFixture(t, root, "authorization-domains-design.md", design.String(), now)
	writeResearchFixture(t, root, "notes/field-note.md", "# Field note\n\n## Finding\n\nAn ordinary research note.\n", time.Now().Add(-time.Hour))
	for i := 1; i <= 7; i++ {
		writeResearchFixture(t, root, fmt.Sprintf("decisions/design-%02d.md", i), fmt.Sprintf("# Design %02d\n\n**Status:** operator-review\n\n## Checklist\n\nReview this design.\n", i), now.Add(-time.Duration(i)*time.Minute))
		writeResearchFixture(t, root, fmt.Sprintf("notes/field-note-%02d.md", i), fmt.Sprintf("# Field note %02d\n\n## Finding\n\nReference material.\n", i), now.Add(-time.Duration(i+60)*time.Minute))
	}

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
        phone = browser.new_page(viewport={"width": 390, "height": 844})
        phone.goto(url + "/research", wait_until="domcontentloaded")
        expect(phone.locator("#research-decisions")).to_be_visible()
        decision = phone.locator("#research-decision-list .research-card")
        expect(decision).to_have_count(6)
        expect(phone.locator("#research-decision-count")).to_have_text("8 waiting")
        expect(phone.locator("#research-list .research-card")).to_have_count(6)
        expect(phone.locator("#research-count")).to_have_text("8 documents")
        expect(phone.locator("#research-list")).not_to_contain_text("Authorization Domains")
        expect(phone.locator("#research-decision-more")).to_contain_text("2 remaining")
        expect(phone.locator("#research-library-more")).to_contain_text("2 remaining")
        initial_metrics = phone.evaluate("({height:document.documentElement.scrollHeight,width:document.documentElement.scrollWidth,clientWidth:document.documentElement.clientWidth})")
        if evidence_dir:
            phone.screenshot(path=os.path.join(evidence_dir, "research-library-initial-390.png"), full_page=True)
        phone.locator("#research-decision-more").click()
        phone.locator("#research-library-more").click()
        expect(phone.locator("#research-decision-list .research-card")).to_have_count(8)
        expect(phone.locator("#research-list .research-card")).to_have_count(8)
        expect(phone.locator("#research-decision-more")).to_be_hidden()
        expect(phone.locator("#research-library-more")).to_be_hidden()
        phone.locator("#research-decision-list .research-card").filter(has_text="Authorization Domains").click()
        expect(phone.locator("#research-document")).to_be_visible()
        expect(phone.locator("#research-title")).to_contain_text("Authorization Domains")
        expect(phone.locator("#research-decision-strip")).to_be_visible()
        expect(phone.locator("#research-toc")).to_be_visible()
        assert phone.locator("#research-toc").get_attribute("open") is None
        expect(phone.locator("#research-toc-count")).to_have_text("33 sections")
        closed_height = phone.locator("#research-toc").evaluate("node => node.getBoundingClientRect().height")
        assert closed_height <= 48, closed_height
        expect(phone.locator(".research-table-wrap table")).to_be_visible()
        expect(phone.locator(".research-markdown script")).to_have_count(0)
        assert phone.evaluate("window.RESEARCH_INJECTED") is None
        assert "<script>" in phone.locator("#research-body").inner_text()
        assert phone.locator('#research-body a[href^="javascript:"]').count() == 0
        assert phone.evaluate("document.documentElement.scrollWidth === document.documentElement.clientWidth")
        table_metrics = phone.locator(".research-table-wrap").evaluate("node => ({scrollWidth:node.scrollWidth,clientWidth:node.clientWidth})")
        code_metrics = phone.locator(".research-markdown pre").evaluate("node => ({scrollWidth:node.scrollWidth,clientWidth:node.clientWidth})")
        assert table_metrics["scrollWidth"] <= table_metrics["clientWidth"]
        assert code_metrics["scrollWidth"] > code_metrics["clientWidth"]
        if evidence_dir:
            phone.screenshot(path=os.path.join(evidence_dir, "research-document-top-390.png"), full_page=True)
        phone.locator("#research-toc > summary").click()
        expect(phone.locator("#research-toc")).to_have_attribute("open", "")
        expect(phone.locator("#research-toc-list li")).to_have_count(33)
        open_height = phone.locator("#research-toc").evaluate("node => node.getBoundingClientRect().height")
        assert open_height > 48
        if evidence_dir:
            phone.screenshot(path=os.path.join(evidence_dir, "research-toc-expanded-390.png"), full_page=False)
        phone.locator("#research-toc > summary").click()
        phone.wait_for_timeout(50)
        assert phone.evaluate("window.scrollY") < 20
        phone.evaluate("window.scrollTo(0, 900)")
        phone.wait_for_timeout(50)
        sticky = phone.locator("#research-decision-strip").evaluate("node => ({position:getComputedStyle(node).position, top:node.getBoundingClientRect().top, bottom:node.getBoundingClientRect().bottom})")
        assert sticky["position"] == "sticky", sticky
        assert sticky["top"] >= 0 and sticky["bottom"] <= 844, sticky
        assert phone.evaluate("document.documentElement.scrollWidth === document.documentElement.clientWidth")
        if evidence_dir:
            phone.screenshot(path=os.path.join(evidence_dir, "research-document-scrolled-390.png"), full_page=False)
        print(json.dumps({
            "initial": initial_metrics,
            "collections": {"decision_visible": 6, "decision_total": 8, "library_visible": 6, "library_total": 8},
            "toc": {"closed_height": closed_height, "open_height": open_height, "sections": 33},
            "sticky_after_900": sticky,
            "table": table_metrics,
            "code": code_metrics
        }, sort_keys=True))
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
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rendered Research library regression: %v\n%s", err, out)
	}
	t.Logf("rendered Research metrics: %s", strings.TrimSpace(string(out)))
	if evidenceDir != "" {
		for _, name := range []string{"research-library-initial-390.png", "research-document-top-390.png", "research-toc-expanded-390.png", "research-document-scrolled-390.png", "research-library-1440.png"} {
			path := filepath.Join(evidenceDir, name)
			if info, err := os.Stat(path); err != nil || info.Size() == 0 {
				t.Fatalf("rendered Research evidence missing at %q: %v", path, err)
			}
			t.Logf("generic rendered evidence: %s", path)
		}
	}
}
