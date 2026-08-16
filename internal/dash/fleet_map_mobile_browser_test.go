package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

const mobileFleetMapRoster = `{
	"channel_id": "fleet",
	"xo_agent": "xo",
	"heartbeat_interval": "20m",
	"agents": [{"name": "xo"}, {"name": "backend"}, {"name": "frontend"}]
}`

// TestMobileFleetMapNamesVisibleW9 locks the first-glance mobile contract: a
// populated Fleet Map exposes desk identities before the operator touches the
// disclosure. The intercepted snapshot is intentionally generic and never
// reads a live dashboard.
func TestMobileFleetMapNamesVisibleW9(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered mobile Fleet Map regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, _ := newTestServer(t, mobileFleetMapRoster, time.Now())
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
status = {
  "xo":"xo", "freshness":{"state":"fresh","message":"fresh"},
  "agents":[
    {"name":"xo","state":"idle"},
    {"name":"backend","state":"working"},
    {"name":"frontend","state":"idle"}
  ]
}
topology = {
  "roster_hierarchy":True, "root_seat_id":"0102030405060708",
  "seats":[
    {"seat_id":"0102030405060708","name":"xo","coordinator":True,"channel_id":"fleet"},
    {"seat_id":"1112131415161718","name":"backend","parent":"0102030405060708","channel_id":"fleet"},
    {"seat_id":"2122232425262728","name":"frontend","parent":"0102030405060708","channel_id":"fleet"}
  ],
  "channels":[]
}

with sync_playwright() as p:
  browser = p.chromium.launch()
  try:
    page = browser.new_page(viewport={"width":390,"height":844})
    page.add_init_script("window.EventSource = undefined")
    def route_api(route):
      path = route.request.url.split("?",1)[0]
      if path.endswith("/api/status"): data = status
      elif path.endswith("/api/topology"): data = topology
      elif path.endswith("/api/goals/meta"): data = {"found":True,"default_view":False}
      elif path.endswith("/api/goals"): data = {"found":True,"version":1,"default_view":False,"goals":[],"counts":{}}
      elif "/api/history" in path: data = {"ledger":[],"backlog":{"found":False,"unblocked":[]}}
      elif "/api/session-mirror" in path: data = {"entries":[]}
      else: data = {}
      route.fulfill(status=200, content_type="application/json", body=json.dumps(data))
    page.route("**/api/**", route_api)
    page.goto(url + "/#conv", wait_until="domcontentloaded")

    disclosure = page.locator('[data-conv-disclosure="nav"]')
    expect(disclosure).to_have_attribute("aria-expanded", "true")
    expect(disclosure).to_have_text("Hide desks")
    expect(page.locator('.conv-item[data-desk="xo"] .conv-item-name')).to_be_visible()
    expect(page.locator('.conv-item[data-desk="backend"] .conv-item-name')).to_be_visible()
    expect(page.locator('.conv-item[data-desk="frontend"] .conv-item-name')).to_be_visible()
    assert page.evaluate("document.documentElement.scrollWidth === document.documentElement.clientWidth")
  finally:
    browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered mobile Fleet Map first-glance regression: %v\n%s", err, out)
	}
}
