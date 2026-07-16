package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestStartupFetchCoalescing748 runs the production assets with a deliberately
// slow full Goals response. Landing must use fast metadata, while every full
// Goals consumer shares one in-flight request.
func TestStartupFetchCoalescing748(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Chromium regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatal(err)
	}
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(func() { httpServer.CloseClientConnections(); httpServer.Close() })

	script := `
import sys
from playwright.sync_api import sync_playwright

url = sys.argv[1]
with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        page = browser.new_page(viewport={"width": 390, "height": 844})
        page.add_init_script(r'''
          window.EventSource = undefined;
          window.__apiReads = {};
          var nativeFetch = window.fetch.bind(window);
          window.fetch = function(input, init) {
            var raw = typeof input === "string" ? input : input.url;
            var path = new URL(raw, location.href).pathname + new URL(raw, location.href).search;
            if (path.indexOf("/api/") !== 0) return nativeFetch(input, init);
            window.__apiReads[path] = (window.__apiReads[path] || 0) + 1;
            var delay = path === "/api/goals" ? 900 : (path === "/api/goals/meta" ? 5 : 0);
            var data = {};
            if (path === "/api/goals/meta") data = {found:true, default_view:false};
            if (path === "/api/goals") data = {found:true, version:1, default_view:false, goals:[], counts:{}};
            if (path === "/api/status") data = {agents:[]};
            if (path === "/api/topology") data = {channels:[]};
            if (path.indexOf("/api/history") === 0) data = {ledger:[], backlog:{found:false,unblocked:[]}};
            return new Promise(function(resolve) { setTimeout(function() {
              resolve({ok:true, status:200, json:function(){return Promise.resolve(data);}, text:function(){return Promise.resolve(JSON.stringify(data));}});
            }, delay); });
          };
        ''')
        page.goto(url, wait_until="domcontentloaded")
        # A full Goals read takes 900ms; metadata must select Conversations first.
        page.wait_for_function("location.hash === '#conv'", timeout=500)
        page.wait_for_timeout(1100)
        reads = page.evaluate("window.__apiReads")
        assert reads.get("/api/goals/meta") == 1, reads
        assert reads.get("/api/goals") == 1, reads
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("startup fetch regression: %v\n%s", err, out)
	}
}
