package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestBindAddressDemotedToStartupDiagnosticsRendered(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered bind-diagnostics regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatal(err)
	}
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(func() { httpServer.CloseClientConnections(); httpServer.Close() })

	script := `
import sys
from playwright.sync_api import sync_playwright, expect

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        page = browser.new_page(viewport={"width":1280,"height":800})
        page.add_init_script("window.EventSource = undefined")
        page.goto(sys.argv[1], wait_until="domcontentloaded")
        expect(page.locator(".bar-meta .diagnostic-bind")).to_have_count(0)
        diagnostic = page.locator(".diagnostic-bind")
        expect(diagnostic).to_be_hidden()
        page.locator(".perf-diagnostics > summary").click()
        expect(diagnostic).to_be_visible()
        expect(diagnostic).to_contain_text("Listening on")
        assert ":" in diagnostic.locator("code").inner_text()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered bind-diagnostics regression: %v\n%s", err, out)
	}
}
