package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestFleetMapStructuredRosterRendered942(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered fleet-map regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatal(err)
	}
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(func() { httpServer.CloseClientConnections(); httpServer.Close() })

	script := `
import json
import sys
from playwright.sync_api import sync_playwright, expect

url = sys.argv[1]
status = {
  "xo":"fleet-lead", "freshness":{"state":"fresh","message":"fresh"},
  "agents":[
    {"name":"fleet-lead","state":"idle"},
    {"name":"alpha-xo","state":"idle","loop_posture":"awaiting-authority"},
    {"name":"alpha-build","state":"crashed"},
    {"name":"empty-xo","state":"idle"},
    {"name":"direct-desk","state":"working"},
    {"name":"orphan-xo","state":"idle"}
  ]
}
topology = {
  "roster_hierarchy":True, "root_seat_id":"0102030405060708",
  "seats":[
    {"seat_id":"0102030405060708","name":"fleet-lead","coordinator":True,"channel_id":"C_ROOT"},
    {"seat_id":"1112131415161718","name":"alpha-xo","parent":"0102030405060708","coordinator":True,"channel_id":"C_ALPHA"},
    {"seat_id":"2122232425262728","name":"alpha-build","parent":"1112131415161718","channel_id":"C_BUILD"},
    {"seat_id":"3132333435363738","name":"empty-xo","parent":"0102030405060708","coordinator":True},
    {"seat_id":"4142434445464748","name":"direct-desk","parent":"0102030405060708"},
    {"seat_id":"5152535455565758","name":"orphan-xo","coordinator":True}
  ],
  "channels":[{"channel_id":"C_WRONG","xo_agent":"orphan-xo","members":["alpha-build"]}]
}

with sync_playwright() as p:
  browser = p.chromium.launch()
  try:
    for width, height in [(390,844),(1440,900)]:
      page = browser.new_page(viewport={"width":width,"height":height})
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
      expect(page.locator("#conv-rail .conv-item")).to_have_count(6)
      headings = page.locator("#conv-rail .chan-id").all_text_contents()
      assert headings == ["Fleet Command", "Alpha", "Unassigned"], headings
      expect(page.locator(".conv-group-unassigned")).to_be_visible()
      expect(page.locator('.conv-item[data-desk="empty-xo"]')).to_have_count(1)
      assert "Empty" not in headings
      alpha = page.locator(".conv-group", has=page.locator(".chan-id", has_text="Alpha"))
      expect(alpha.locator(".conv-group-rollup.blocked")).to_have_text("1 blocked")
      expect(alpha.locator(".conv-group-rollup.waiting")).to_have_text("1 waiting")
      assert page.locator('.conv-item[data-desk="alpha-build"]').get_attribute("data-channel") == "C_BUILD"
      for group in page.locator("#conv-rail .conv-group").all():
        box = group.bounding_box()
        assert box and box["x"] >= -0.5 and box["x"] + box["width"] <= width + 0.5, box
      page.close()
  finally:
    browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered structured roster fleet map: %v\n%s", err, out)
	}
}
