package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestParadeEmptyArchiveRenderedCard(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered parade-empty regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(httpServer.Close)

	script := `
import sys
from playwright.sync_api import sync_playwright, expect

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        page = browser.new_page(viewport={"width": 390, "height": 844})
        page.goto(sys.argv[1] + "/parade", wait_until="networkidle")
        card = page.locator(".pd-list-empty")
        expect(card).to_be_visible()
        expect(card.locator("h1")).to_have_text("No parades yet")
        metric = card.evaluate("""el => {
          const r=el.getBoundingClientRect(), p=el.parentElement.getBoundingClientRect();
          return {left:r.left, right:r.right, top:r.top, bottom:r.bottom,
                  center:r.left+r.width/2, parentCenter:p.left+p.width/2,
                  border:getComputedStyle(el).borderStyle, background:getComputedStyle(el).backgroundColor};
        }""")
        if abs(metric["center"] - metric["parentCenter"]) > 1:
            raise AssertionError("empty card is not centered: %r" % metric)
        if metric["left"] < 0 or metric["right"] > 390 or metric["bottom"] > 844:
            raise AssertionError("empty card escapes phone viewport: %r" % metric)
        if metric["border"] == "none" or metric["background"] == "rgba(0, 0, 0, 0)":
            raise AssertionError("empty state regressed to loose text: %r" % metric)
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered parade-empty regression: %v\n%s", err, out)
	}
}
