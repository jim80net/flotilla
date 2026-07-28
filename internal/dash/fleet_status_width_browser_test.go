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

func TestFleetStatusSemanticWidthContract839(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	js := doGet(t, srv, "/static/dash.js").Body.String()
	css := doGet(t, srv, "/static/dash.css").Body.String()
	for _, marker := range []string{`function utilizationUnits(status)`, `function utilizationReadUnits(status)`, `Almost no one is working`, `Send work or pull the next queue item`, `fleet-status-unit`, `fleet-utilization-unit`, `unit.kind`, `unit.text`} {
		if !strings.Contains(js, marker) {
			t.Errorf("fleet status renderer missing %q", marker)
		}
	}
	for _, marker := range []string{`.fleet-status-unit`, `.fleet-utilization-unit`, `white-space: nowrap`, `flex-wrap: wrap`, `.conv-rail-head #rail-meta { display: flex; grid-column: 1 / -1; }`} {
		if !strings.Contains(css, marker) {
			t.Errorf("fleet status semantic-width CSS missing %q", marker)
		}
	}
}

// TestFleetStatusSemanticWidthRendered839 locks the standing layout rule in a
// real browser: complete metric/liveness units may move between rows, but text
// inside a unit remains one line and contained at phone and desktop widths.
func TestFleetStatusSemanticWidthRendered839(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered fleet-status regression")
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
		if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	script := `
import json
import os
import sys
from playwright.sync_api import sync_playwright, expect

url, evidence_dir = sys.argv[1], sys.argv[2]
status = {
    "generated_at": "2026-07-20T12:00:00Z",
    "xo": "cos",
    "agents": [],
    "freshness": {"state": "fresh", "message": ""},
    "xo_liveness": {"acked": True, "ack_age": "11m23s", "settled_known": True, "settled": False},
    "utilization": {"working": 1, "total": 52, "blocked": 8, "awaiting_authority": 12, "utilization_wall": True}
}
expected = ["1 of 52 seats working", "8 blocked", "12 seats waiting for authority", "cos · ack 11m23s ago · active"]
footer_expected = expected[:3] + ["Almost no one is working", "Send work or pull the next queue item"]

def verify(page, width, height):
    page.set_viewport_size({"width": width, "height": height})
    page.goto(url, wait_until="domcontentloaded")
    units = page.locator("#rail-meta .fleet-status-unit")
    expect(units).to_have_count(4)
    actual = units.all_inner_texts()
    if actual != expected:
        raise AssertionError("semantic units changed: %r" % actual)
    if " ".join(page.locator("#rail-meta").inner_text().split()) != " ".join(expected):
        raise AssertionError("semantic units lost readable text boundaries at %dpx" % width)
    metrics = units.evaluate_all("""els => els.map(el => {
      const range = document.createRange();
      range.selectNodeContents(el);
      const lines = [...range.getClientRects()].map(rect => Math.round(rect.top));
      const box = el.getBoundingClientRect();
      const parent = el.parentElement.getBoundingClientRect();
      return {text: el.innerText, lines: [...new Set(lines)].length,
              left: box.left, right: box.right, parentLeft: parent.left, parentRight: parent.right,
              whiteSpace: getComputedStyle(el).whiteSpace};
    })""")
    for metric in metrics:
        if metric["lines"] != 1 or metric["whiteSpace"] != "nowrap":
            raise AssertionError("semantic unit wrapped internally at %dpx: %r" % (width, metric))
        if metric["left"] < metric["parentLeft"] - .5 or metric["right"] > metric["parentRight"] + .5:
            raise AssertionError("semantic unit escaped its column at %dpx: %r" % (width, metric))
    footer_units = page.locator("#fleet-utilization .fleet-utilization-unit")
    expect(footer_units).to_have_count(5)
    if footer_units.all_inner_texts() != footer_expected:
        raise AssertionError("footer semantic units changed at %dpx" % width)
    if " ".join(page.locator("#fleet-utilization").inner_text().split()) != " ".join(footer_expected):
        raise AssertionError("footer units lost readable text boundaries at %dpx" % width)
    footer_metrics = footer_units.evaluate_all("""els => els.map(el => {
      const range = document.createRange();
      range.selectNodeContents(el);
      const lines = [...range.getClientRects()].map(rect => Math.round(rect.top));
      const box = el.getBoundingClientRect();
      const parent = el.parentElement.getBoundingClientRect();
      return {text: el.innerText, lines: [...new Set(lines)].length,
              left: box.left, right: box.right, parentLeft: parent.left, parentRight: parent.right,
              whiteSpace: getComputedStyle(el).whiteSpace};
    })""")
    for metric in footer_metrics:
        if metric["lines"] != 1 or metric["whiteSpace"] != "nowrap":
            raise AssertionError("footer semantic unit wrapped internally at %dpx: %r" % (width, metric))
        if metric["left"] < metric["parentLeft"] - .5 or metric["right"] > metric["parentRight"] + .5:
            raise AssertionError("footer semantic unit escaped its parent at %dpx: %r" % (width, metric))
    doc = page.evaluate("() => ({scrollWidth: document.documentElement.scrollWidth, innerWidth})")
    if doc["scrollWidth"] > doc["innerWidth"]:
        raise AssertionError("fleet status causes horizontal overflow at %dpx: %r" % (width, doc))
    if evidence_dir:
        page.screenshot(path=os.path.join(evidence_dir, "fleet-status-%d.png" % width), full_page=True)
    print("FLEET_STATUS_%d=" % width + json.dumps({"header": metrics, "footer": footer_metrics}, sort_keys=True))

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        page = browser.new_page(viewport={"width": 390, "height": 844})
        page.set_default_timeout(8000)
        page.add_init_script("window.EventSource = undefined")
        page.route("**/api/status", lambda route: route.fulfill(status=200, content_type="application/json", body=json.dumps(status)))
        verify(page, 390, 844)
        verify(page, 1440, 900)
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, evidenceDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rendered fleet-status regression: %v\n%s", err, out)
	}
	if len(out) > 0 {
		t.Log(fmt.Sprintf("%s", out))
	}
	if evidenceDir != "" {
		for _, name := range []string{"fleet-status-390.png", "fleet-status-1440.png"} {
			path := filepath.Join(evidenceDir, name)
			if info, err := os.Stat(path); err != nil || info.Size() == 0 {
				t.Fatalf("rendered evidence missing at %q: %v", path, err)
			}
			t.Logf("generic rendered evidence: %s", path)
		}
	}
}
