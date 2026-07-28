package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestWorkTimelineRendered891 proves the shared Work Context timeline at the
// phone and desktop contracts. Fixtures are generic and optional captures stay
// in the caller-supplied private evidence directory.
func TestWorkTimelineRendered891(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Chromium regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
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
from urllib.parse import parse_qs, urlparse
from playwright.sync_api import sync_playwright, expect

url, evidence_dir = sys.argv[1], sys.argv[2]

def event(i):
    states = ["assigned", "queued", "delivered", "acknowledged", "gated", "merged", "superseded"]
    kinds = ["assignment", "dispatch", "dispatch", "dispatch", "gate", "github_pr", "outcome"]
    state = states[i % len(states)]
    kind = kinds[i % len(kinds)]
    return {
        "id": "event-%02d" % i, "kind": kind, "state": state,
        "at": "2026-07-28T10:%02d:00Z" % (i % 60), "actor": "generic-desk",
        "title": "Stable timeline fact %02d" % i,
        "detail": "A bounded source-native fact with deterministic ordering.",
        "source": "dispatch" if kind == "dispatch" else "github",
        "source_id": "native-%02d" % i,
        "source_url": "https://github.com/example/product/issues/%d" % (100 + i)
    }

long_events = [event(i) for i in range(28)]
earlier_attempts = {"count": 0}

def timeline(route):
    query = parse_qs(urlparse(route.request.url).query)
    if query.get("goal") == ["goal-long"]:
        before = query.get("before", [""])[0]
        if before and earlier_attempts["count"] == 0:
            earlier_attempts["count"] += 1
            route.fulfill(
                status=503, content_type="application/json",
                body=json.dumps({"error":"earlier history source temporarily unavailable"})
            )
            return
        body = {
            "subject": {"kind":"goal","id":"goal-long","title":"Long generic goal","state":"in-flight","source_id":"goal-long"},
            "events": long_events[:8] if before else long_events[8:],
            "sources": [
                {"id":"goals","label":"Goals","status":"available","detail":"Goal identity available."},
                {"id":"dispatch","label":"Dispatch ledger","status":"partial","detail":"Compacted history remains outside the retained window."},
                {"id":"github","label":"GitHub","status":"stale","detail":"Last successful source read is stale."}
            ],
            "generated_at":"2026-07-28T11:00:00Z","partial":True,
            "total":8 if before else 28,"next_cursor":"" if before else "older"
        }
    elif query.get("issue") == ["44"]:
        body = {
            "subject":{"kind":"issue","id":"example/product#44","title":"Partial generic issue","state":"open","source_id":"example/product#44"},
            "events":[event(2)],
            "sources":[
                {"id":"identity","label":"Work item","status":"available","detail":"Identity available."},
                {"id":"github","label":"GitHub","status":"unavailable","detail":"Source request failed loudly."},
                {"id":"dispatch","label":"Dispatch ledger","status":"partial","detail":"Retained exact matches only."}
            ],
            "generated_at":"2026-07-28T11:00:00Z","partial":True,"total":1
        }
    else:
        body = {
            "subject":{"kind":"issue","id":"example/product#45","title":"Empty generic issue","state":"open","source_id":"example/product#45"},
            "events":[],
            "sources":[
                {"id":"identity","label":"Work item","status":"available","detail":"Identity available."},
                {"id":"github","label":"GitHub","status":"available","detail":"No history exists."}
            ],
            "generated_at":"2026-07-28T11:00:00Z","partial":False,"total":0
        }
    route.fulfill(status=200, content_type="application/json", body=json.dumps(body))

def prepare(page):
    page.set_default_timeout(8000)
    page.add_init_script("window.EventSource = undefined")
    page.route("**/api/work-timeline?**", timeline)
    page.route("**/api/goals", lambda route: route.fulfill(
        status=200, content_type="application/json", body=json.dumps({
          "found":True,
          "goals":[{
            "id":"goal-long","title":"Long generic goal","owner":"alpha",
            "conversation_agent":"alpha","status_display":"in-flight",
            "state":"in-flight","work_items":[]
          }]
        })))
    page.route("**/api/status", lambda route: route.fulfill(
        status=200, content_type="application/json",
        body=json.dumps({"agents":[{"name":"alpha","state":"idle"}]})))
    page.route("**/api/topology", lambda route: route.fulfill(
        status=200, content_type="application/json", body=json.dumps({"org_nodes":[]})))
    page.route("**/api/session-mirror?**", lambda route: route.fulfill(
        status=200, content_type="application/json",
        body=json.dumps({"agent":"alpha","entries":[],"limit":500})))
    page.route("**/api/issues/**", lambda route: route.fulfill(
        status=200, content_type="application/json",
        body=json.dumps({"number":44,"title":"Generic issue","state":"OPEN","comments":[]})))

def open_goal(page):
    page.evaluate("""() => {
      history.replaceState({view:'goals'}, '', '#goals');
      window.flotillaDash.showView('goals');
    }""")
    page.evaluate("""() => window.flotillaWorkContext.open({
      item:{goal:{id:'goal-long',title:'Long generic goal',owner:'alpha',status_display:'in-flight'}},
      posture:'in-flight',flotilla:'Example',desk:'alpha',seats:['alpha']
    }, document.body)""")

def open_issue(page, number):
    page.evaluate("() => window.flotillaWorkContext.close()")
    page.evaluate("""() => {
      history.replaceState({view:'issues'}, '', '#issues');
      window.flotillaDash.showView('issues');
    }""")
    page.evaluate("""number => window.flotillaWorkContext.open({
      item:{repo:'example/product',issue:{number:number,title:number===44?'Partial generic issue':'Empty generic issue',state:'OPEN'}},
      posture:'in-flight',flotilla:'Example',desk:'alpha',seats:['alpha']
    }, document.body)""", number)

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        for width, height in [(390, 844), (1440, 900)]:
            earlier_attempts["count"] = 0
            page = browser.new_page(viewport={"width":width,"height":height})
            prepare(page)
            page.goto(url, wait_until="domcontentloaded")

            open_goal(page)
            expect(page.locator("#wc-timeline")).to_be_visible()
            expect(page.locator("#wc-timeline-events .wc-event")).to_have_count(20)
            expect(page.locator(".wc-source-partial")).to_contain_text("Dispatch ledger")
            expect(page.locator(".wc-source-stale")).to_contain_text("GitHub")
            assert page.locator(".wc-source").evaluate_all("""nodes => {
              const parent = nodes[0].parentElement.getBoundingClientRect();
              return nodes.every(node => {
                const box = node.getBoundingClientRect();
                return box.left >= parent.left && box.right <= parent.right;
              });
            }""")
            for state in ["queued", "delivered", "acknowledged", "gated", "merged", "superseded"]:
                expect(page.locator("#wc-timeline-events")).to_contain_text(state)
            page.locator("#wc-timeline-earlier").click()
            expect(page.locator("#wc-timeline-events .wc-event")).to_have_count(20)
            expect(page.locator(".wc-source-unavailable")).to_contain_text("Earlier history")
            expect(page.locator("#wc-timeline-summary")).to_contain_text("partial coverage")
            expect(page.locator("#wc-timeline-earlier")).to_have_text("Retry earlier history")
            expect(page.locator("#wc-timeline-earlier")).to_be_enabled()
            page.locator("#wc-timeline-earlier").click()
            expect(page.locator("#wc-timeline-events .wc-event")).to_have_count(28)
            expect(page.locator("#wc-timeline-earlier")).to_be_hidden()
            box = page.locator("#wc-timeline").bounding_box()
            assert box and box["x"] >= 0 and box["x"] + box["width"] <= width + 0.5
            assert page.evaluate("document.documentElement.scrollWidth === innerWidth")
            if evidence_dir:
                page.screenshot(path=os.path.join(evidence_dir, "work-timeline-long-%d.png" % width), full_page=False)

            open_issue(page, 44)
            expect(page.locator(".wc-source-unavailable")).to_contain_text("GitHub")
            expect(page.locator("#wc-timeline-summary")).to_contain_text("partial coverage")
            expect(page.locator("#wc-timeline-events .wc-event")).to_have_count(1)

            open_issue(page, 45)
            expect(page.locator("#wc-timeline-summary")).to_have_text("No recorded facts")
            expect(page.locator(".wc-timeline-empty")).to_contain_text("No timeline facts")
            expect(page.locator("#wc-timeline-events .wc-event")).to_have_count(0)
            assert page.evaluate("document.documentElement.scrollWidth === innerWidth")
            if evidence_dir:
                page.screenshot(path=os.path.join(evidence_dir, "work-timeline-empty-%d.png" % width), full_page=False)
            page.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, evidenceDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Work Context timeline regression: %v\n%s", err, out)
	}
	if evidenceDir != "" {
		for _, name := range []string{
			"work-timeline-long-390.png", "work-timeline-empty-390.png",
			"work-timeline-long-1440.png", "work-timeline-empty-1440.png",
		} {
			path := filepath.Join(evidenceDir, name)
			if info, err := os.Stat(path); err != nil || info.Size() == 0 {
				t.Fatalf("generic rendered evidence missing at %q: %v", path, err)
			}
			t.Logf("private generic timeline evidence: %s", path)
		}
	}
}
