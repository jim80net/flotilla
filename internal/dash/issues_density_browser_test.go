package dash

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestIssuesLargeFanoutRendered827 uses only synthetic repositories and issue
// prose. It proves the phone's initial DOM and height stay bounded while every
// group remains reachable through the stable disclosure control.
func TestIssuesLargeFanoutRendered827(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Chromium regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	flotillas := make([]map[string]any, 0, 24)
	repos := make([]string, 0, 24)
	issueNumber := 1000
	for i := 1; i <= 24; i++ {
		repo := fmt.Sprintf("example/research-%02d", i)
		repos = append(repos, repo)
		moving := make([]map[string]any, 0, 5)
		for j := 1; j <= 5; j++ {
			issueNumber++
			moving = append(moving, map[string]any{
				"repo":  repo,
				"issue": map[string]any{"number": issueNumber, "title": fmt.Sprintf("Generic work item %02d-%02d", i, j), "state": "OPEN", "url": fmt.Sprintf("https://example.invalid/%d", issueNumber), "updatedAt": "2026-07-20T08:00:00Z"},
			})
		}
		issueNumber++
		shipped := []map[string]any{{
			"repo":  repo,
			"issue": map[string]any{"number": issueNumber, "title": fmt.Sprintf("Generic shipped item %02d", i), "state": "CLOSED", "url": fmt.Sprintf("https://example.invalid/%d", issueNumber), "closedAt": "2026-07-19T08:00:00Z"},
		}}
		flotillas = append(flotillas, map[string]any{
			"name":  fmt.Sprintf("Product %02d", i),
			"desks": []map[string]any{{"name": fmt.Sprintf("desk-%02d", i), "in_flight": moving, "shipped": shipped}},
		})
	}
	fixture, err := json.Marshal(map[string]any{
		"repo": "example/research-01", "repos": repos, "flotillas": flotillas,
		"coverage": map[string]any{
			"complete": false, "indexed_repos": repos, "expected_repos": 27,
			"failed_repos": []map[string]any{{"repo": "example/unavailable-a"}, {"repo": "example/unavailable-b"}},
			"unmapped_domains": []string{"Missing Product"},
			"domains": []map[string]any{
				{"name": "Mapped Product", "state": "mapped", "repos": []string{"example/research-01"}},
				{"name": "Advisory Product", "state": "repository-less", "repos": []string{}},
				{"name": "Missing Product", "state": "missing", "repos": []string{}},
				{"name": "Unavailable Product", "state": "failed", "repos": []string{"example/unavailable-a"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(func() { httpServer.CloseClientConnections(); httpServer.Close() })

	script := `
import json
import sys
from playwright.sync_api import sync_playwright, expect

url, body = sys.argv[1], sys.argv[2]
with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        for width, height in [(390, 844), (360, 800)]:
            page = browser.new_page(viewport={"width": width, "height": height})
            page.set_default_timeout(8000)
            page.add_init_script("window.EventSource = undefined")
            page.route("**/api/work-ledger*", lambda route: route.fulfill(status=200, content_type="application/json", body=body))
            page.route("**/api/issues/*", lambda route: route.fulfill(status=200, content_type="application/json", body=json.dumps({"number": 1001, "title": "Generic work item", "state": "OPEN", "body": "Generic detail body", "url": "https://example.invalid/1001", "labels": [], "comments": []})))
            page.goto(url, wait_until="domcontentloaded")
            page.locator("#tab-issues").click()
            expect(page.locator(".issue-mobile-window")).to_be_visible()
            rows = page.locator("#issues-list .issue-row")
            initial_rows = rows.count()
            assert initial_rows <= 10, "initial rows are not bounded: %d" % initial_rows
            assert page.locator('[data-ref="example/research-24#1144"]').count() == 0, "hidden final row entered initial DOM"
            metrics = page.evaluate("""() => ({
                height: document.documentElement.scrollHeight,
                client: document.documentElement.clientHeight,
                width: document.documentElement.scrollWidth,
                clientWidth: document.documentElement.clientWidth,
                windowText: document.querySelector('.issue-mobile-window').innerText
            })""")
            assert metrics["height"] <= height * 2, "initial Issues height exceeds two viewports: %r" % metrics
            assert metrics["width"] == metrics["clientWidth"], "Issues overflows horizontally: %r" % metrics
            assert "remaining" in metrics["windowText"] and "of 144 work items reachable" in metrics["windowText"], metrics
            coverage = page.locator(".issue-scope-incomplete")
            expect(coverage).to_contain_text("Showing 24 of 27 mapped repositories")
            expect(coverage).to_contain_text("1 mapped, 1 repository-less, 1 missing, 1 failed")
            assert coverage.evaluate("e => e.scrollHeight <= e.clientHeight + 1"), "partial coverage truth is clipped"

            first = rows.first
            first.click()
            expect(page.locator("#work-context")).to_be_visible()
            expect(page.locator("#wc-github")).to_be_visible()
            page.locator("#wc-github summary").click()
            expect(page.locator("#wc-github-body .wc-open-full-issue")).to_be_visible()
            page.locator("#wc-close").click()

            activations = 0
            jump = page.locator(".issue-ledger-jump")
            expect(jump.locator("[data-ledger-jump]")).to_have_count(24)
            jump.locator("summary").click()
            activations += 1
            expect(jump).to_have_attribute("open", "")
            jump.locator("[data-ledger-jump]").last.click()
            activations += 1
            assert activations <= 2
            expect(page.locator('[data-ref="example/research-24#1143"]')).to_be_visible()
            expect(page.locator('[data-ref="example/research-24#1144"]')).to_be_visible()
            expect(page.locator(".issue-mobile-focused")).to_contain_text("all 6 desk items visible")
            overview = page.locator("[data-ledger-overview]")
            expect(overview).to_be_visible()
            return_box = overview.evaluate("node => { const r=node.getBoundingClientRect(); return {top:r.top,bottom:r.bottom,height:r.height} }")
            assert return_box["top"] >= 0 and return_box["bottom"] <= height and return_box["height"] >= 44, return_box
            page.wait_for_function("() => document.activeElement && document.activeElement.classList.contains('issue-row')")
            overview.click()
            expect(page.locator('[data-ref="example/research-24#1143"]')).to_have_count(0)
            expect(page.locator(".issue-mobile-focused")).to_have_count(0)
            summary = page.locator(".issue-ledger-jump > summary")
            page.wait_for_function("() => document.activeElement === document.querySelector('.issue-ledger-jump > summary')")
            return_focus = summary.evaluate("node => { const r=node.getBoundingClientRect(), h=document.querySelector('#issues-listpanel > .panel-head').getBoundingClientRect(); return {top:r.top,bottom:r.bottom,height:r.height,headerBottom:h.bottom} }")
            assert return_focus["height"] >= 44 and return_focus["top"] >= return_focus["headerBottom"] + 7, return_focus
            print(json.dumps({"viewport": "%dx%d" % (width, height), "initial_rows": initial_rows, **metrics}))
            page.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, string(fixture))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered large-fanout Issues regression: %v\n%s", err, out)
	} else {
		t.Logf("generic phone metrics:\n%s", out)
	}
}
