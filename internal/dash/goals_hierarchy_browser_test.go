package dash

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func goalsHierarchyFixture766() GoalsDoc {
	return BuildGoals(GoalsInputs{
		FileOK: true,
		File: GoalsFile{Version: 1, Goals: []Goal{
			// Finance is an authored owner root whose declared task scope reproduces
			// the collision that previously created a parallel synthetic owner hub.
			{ID: "finance", Title: "Finance", Description: "Finance example hub", Scope: ScopeTask, Owner: "finance-xo"},
			{ID: "venture", Title: "Venture", Scope: ScopeProject, Parent: "finance", Owner: "finance-xo"},
			{ID: "trading", Title: "Trading", Scope: ScopeProject, Parent: "finance", Owner: "finance-xo"},
			{ID: "alpha", Title: "Alpha Product", Description: "Alpha example hub", Scope: ScopeFlotilla, Owner: "alpha-xo"},
		}},
		MetaXO: "coord",
		Channels: []DeskChannel{
			{ChannelID: "C_FIN", XOAgent: "finance-xo", Members: []string{"coord", "finance-desk", "alpha-desk", "beta-desk"}},
			{ChannelID: "C_ALPHA", XOAgent: "alpha-xo", Members: []string{"coord", "alpha-desk"}},
			{ChannelID: "C_BETA", XOAgent: "beta-xo", Members: []string{"coord", "beta-desk"}},
		},
		OrgParents: map[string]string{
			"finance-xo":   "coord",
			"alpha-xo":     "coord",
			"beta-xo":      "coord",
			"finance-desk": "finance-xo",
			"alpha-desk":   "alpha-xo",
			"beta-desk":    "beta-xo",
		},
		OrgSource: "derived",
	})
}

// TestGoalsHierarchyRendered766 proves that both Goals renderers consume the
// corrected parent graph. Its fixture is fully generic and never reads live state.
func TestGoalsHierarchyRendered766(t *testing.T) {
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
	doc, err := json.Marshal(goalsHierarchyFixture766())
	if err != nil {
		t.Fatal(err)
	}
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

url, body, evidence_dir = sys.argv[1], sys.argv[2], sys.argv[3]
with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        desktop = browser.new_page(viewport={"width": 1440, "height": 900})
        desktop.set_default_timeout(8000)
        desktop.add_init_script("window.EventSource = undefined")
        desktop.route("**/api/goals", lambda route: route.fulfill(status=200, content_type="application/json", body=body))
        desktop.goto(url, wait_until="domcontentloaded")
        desktop.locator("#tab-goals").click()
        expect(desktop.locator('.gnode[data-id="hub:coord"]')).to_be_visible()
        expect(desktop.locator('.gnode[data-id="hub:finance-xo"]')).to_have_count(0)
        for child in ["finance", "alpha", "hub:beta-xo"]:
            expect(desktop.locator('.gnode[data-id="%s"]' % child)).to_have_attribute("data-parent", "hub:coord")
        expect(desktop.locator('.gnode[data-id="desk:finance-desk"]')).to_have_attribute("data-parent", "finance")
        expect(desktop.locator('.gnode[data-id="venture"]')).to_have_attribute("data-parent", "finance")
        expect(desktop.locator('.gnode[data-id="trading"]')).to_have_attribute("data-parent", "finance")
        expect(desktop.locator('.gnode[data-id="desk:alpha-desk"]')).to_have_attribute("data-parent", "alpha")
        expect(desktop.locator('.gnode[data-id="desk:beta-desk"]')).to_have_attribute("data-parent", "hub:beta-xo")
        if evidence_dir:
            desktop.screenshot(path=os.path.join(evidence_dir, "desktop-1440.png"), full_page=True)
        desktop.close()

        phone = browser.new_page(viewport={"width": 390, "height": 844})
        phone.set_default_timeout(8000)
        phone.add_init_script("window.EventSource = undefined")
        phone.route("**/api/goals", lambda route: route.fulfill(status=200, content_type="application/json", body=body))
        phone.goto(url, wait_until="domcontentloaded")
        phone.locator("#tab-goals").click()
        root = phone.locator('[data-outline-root="hub:coord"]')
        expect(root).to_be_visible()
        expect(root.locator('[data-outline-desk="finance"]')).to_be_visible()
        expect(root.locator('[data-outline-desk="alpha"]')).to_be_visible()
        expect(root.locator('[data-outline-desk="hub:beta-xo"]')).to_be_visible()
        root.locator('[data-outline-desk-toggle="finance"]').click()
        expect(root.locator('[data-outline-id="desk:finance-desk"]')).to_be_visible()
        expect(root.locator('[data-outline-id="desk:alpha-desk"]')).to_have_count(0)
        if evidence_dir:
            phone.screenshot(path=os.path.join(evidence_dir, "phone-390.png"), full_page=True)
        phone.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, string(doc), evidenceDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Goals hierarchy regression: %v\n%s", err, out)
	}
	if evidenceDir != "" {
		for _, name := range []string{"desktop-1440.png", "phone-390.png"} {
			path := filepath.Join(evidenceDir, name)
			if info, err := os.Stat(path); err != nil || info.Size() == 0 {
				t.Fatalf("rendered evidence missing at %q: %v", path, err)
			}
			t.Logf("generic rendered evidence: %s", path)
		}
	}
}
