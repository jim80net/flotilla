package dash

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"net/http/httptest"
)

func TestMobileConversationsWindowContract689(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	html := doGet(t, srv, "/").Body.String()
	js := doGet(t, srv, "/static/dash.js").Body.String()
	css := doGet(t, srv, "/static/dash.css").Body.String()
	for _, marker := range []string{`data-conv-disclosure="nav"`, `data-conv-disclosure="context"`} {
		if !strings.Contains(html, marker) {
			t.Errorf("mobile Conversations HTML missing %q", marker)
		}
	}
	for _, marker := range []string{"MOBILE_THREAD_INITIAL", "data-thread-window-more", "mobileThreadHidden > 0"} {
		if !strings.Contains(js, marker) {
			t.Errorf("mobile Conversations renderer missing %q", marker)
		}
	}
	for _, marker := range []string{".thread-window-item:not(.is-expanded) .thread-gist", ".conv-nav:not(.mobile-expanded) .conv-rail-list"} {
		if !strings.Contains(css, marker) {
			t.Errorf("mobile Conversations CSS missing %q", marker)
		}
	}
}

// TestMobileConversationsDOMWindow689 is an opt-in rendered regression. It uses
// generic intercepted history/mirror documents so evidence never touches a live
// state store, while exercising the production renderer and CSS in Chromium.
func TestMobileConversationsDOMWindow689(t *testing.T) {
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
from datetime import datetime, timedelta, timezone
from playwright.sync_api import sync_playwright, expect

url, evidence_dir = sys.argv[1], sys.argv[2]
base = datetime(2026, 7, 15, 9, 0, tzinfo=timezone.utc)
long_text = "Generic coordination update with enough detail to exercise the mobile clamp. " * 12
mirror = {"agent": "xo", "entries": [
    {"ts": (base + timedelta(minutes=i)).isoformat().replace("+00:00", "Z"),
     "agent": "xo", "info": (long_text if i >= 195 else "Alpha synthetic session update %03d." % i),
     "debug": {}, "suppressed": False}
    for i in range(200)
]}
ledger = [{
    "parsed": True,
    "time": (base - timedelta(minutes=50-i)).isoformat().replace("+00:00", "Z"),
    "from": "operator" if i % 2 == 0 else "xo",
    "to": "xo" if i % 2 == 0 else "operator",
    "channel": "alpha-example",
    "gist": "Synthetic relay update %03d." % i,
    "body": "Synthetic relay update %03d." % i,
} for i in range(50)]
history = {"desk": "xo", "ledger": ledger, "backlog": {"found": True, "unblocked": []},
           "limit": 50, "has_more": True, "next_cursor": "100", "ledger_signature": "alpha"}

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        page = browser.new_page(viewport={"width": 390, "height": 844})
        page.set_default_timeout(8000)
        page.add_init_script("window.EventSource = undefined")
        page.route("**/api/history?*", lambda route: route.fulfill(status=200, content_type="application/json", body=json.dumps(history)))
        page.route("**/api/session-mirror?*", lambda route: route.fulfill(status=200, content_type="application/json", body=json.dumps(mirror)))
        page.goto(url, wait_until="domcontentloaded")
        more = page.locator("[data-thread-window-more]")
        expect(more).to_be_visible()
        expect(page.locator("#thread-load-earlier")).to_be_hidden()
        expect(page.locator("#conv-thread > .thread-window-item")).to_have_count(3)
        metrics = page.evaluate("""() => ({
          height: document.documentElement.scrollHeight,
          innerHeight: innerHeight,
          width: document.documentElement.scrollWidth,
          innerWidth: innerWidth,
          children: document.querySelector('#conv-thread').children.length
        })""")
        if metrics["height"] > metrics["innerHeight"] * 2:
            raise AssertionError("initial document exceeds two viewports: %r" % metrics)
        if metrics["width"] > metrics["innerWidth"]:
            raise AssertionError("initial document overflows horizontally: %r" % metrics)
        print("MOBILE_METRICS=" + json.dumps(metrics, sort_keys=True))
        if evidence_dir:
            page.screenshot(path=os.path.join(evidence_dir, "initial-390.png"), full_page=True)

        before_count = page.locator("#conv-thread > .thread-window-item").count()
        more.click()
        expect(page.locator("#conv-thread > .thread-window-item")).to_have_count(before_count + 8)
        toggle = page.locator("[data-thread-expand]").last
        expect(toggle).to_be_visible()
        toggle.click()
        expect(toggle).to_have_attribute("aria-expanded", "true")
        page.locator('[data-verbosity="debug"]').click()
        expect(page.locator('[data-thread-expand][aria-expanded="true"]')).to_be_visible()
        if evidence_dir:
            page.screenshot(path=os.path.join(evidence_dir, "expanded-390.png"), full_page=True)

        # Exhaust only the cached DOM window. The existing cursor-backed pager is
        # then exposed unchanged, proving older server history remains reachable.
        while page.locator("[data-thread-window-more]").count():
            page.locator("[data-thread-window-more]").click()
        expect(page.locator("#thread-load-earlier")).to_be_visible()

        # Desktop retains the complete bounded client timeline and its existing
        # fixed-shell thread scroll; the DOM window is intentionally phone-only.
        desktop = browser.new_page(viewport={"width": 1440, "height": 900})
        desktop.add_init_script("window.EventSource = undefined")
        desktop.route("**/api/history?*", lambda route: route.fulfill(status=200, content_type="application/json", body=json.dumps(history)))
        desktop.route("**/api/session-mirror?*", lambda route: route.fulfill(status=200, content_type="application/json", body=json.dumps(mirror)))
        desktop.goto(url, wait_until="domcontentloaded")
        expect(desktop.locator("#conv-thread > .thread-window-item")).to_have_count(250)
        expect(desktop.locator("[data-thread-window-more]")).to_have_count(0)
        expect(desktop.locator(".conv-mobile-disclosure").first).to_be_hidden()
        expect(desktop.locator("[data-thread-expand]")).to_have_count(0)
        desktop.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, evidenceDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rendered mobile Conversations regression: %v\n%s", err, out)
	}
	if len(out) > 0 {
		t.Log(fmt.Sprintf("%s", out))
	}
	if evidenceDir != "" {
		for _, name := range []string{"initial-390.png", "expanded-390.png"} {
			path := filepath.Join(evidenceDir, name)
			if info, err := os.Stat(path); err != nil || info.Size() == 0 {
				t.Fatalf("rendered evidence missing at %q: %v", path, err)
			}
			t.Logf("generic rendered evidence: %s", path)
		}
	}
}
