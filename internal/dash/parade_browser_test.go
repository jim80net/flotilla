package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestParadeZeroAndOneSlideRendered772 exercises the honest collection boundary
// in Chromium. An empty parade stays collection-scoped; a real one-slide parade
// retains its numbered deck and per-slide conversation affordance.
func TestParadeZeroAndOneSlideRendered772(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Parade regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, dir := newTestServer(t, singleFleetRoster, time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC))
	paradesDir := filepath.Join(dir, "parades")
	emptyDir := filepath.Join(paradesDir, "2026-07-17")
	oneDir := filepath.Join(paradesDir, "2026-07-16")
	for _, path := range []string{emptyDir, oneDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(emptyDir, "slides.md"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oneDir, "slides.md"), []byte("One honest slide\n\nA real authored claim."), 0o600); err != nil {
		t.Fatal(err)
	}

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
import os
import sys
from playwright.sync_api import sync_playwright, expect

url, evidence_dir = sys.argv[1], sys.argv[2]
with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        page = browser.new_page(viewport={"width": 390, "height": 844})
        page.set_default_timeout(8000)
        conversation_requests = []
        page.on("request", lambda request: conversation_requests.append(request.url) if "/conversations" in request.url else None)
        page.goto(url + "/parade", wait_until="domcontentloaded")

        empty = page.locator(".pd-empty-state")
        expect(empty).to_be_visible()
        expect(empty).to_contain_text("No slides in this parade")
        expect(empty).to_contain_text("no authored slides yet")
        expect(page.locator("#pd-deck-date")).to_have_text("2026-07-17")
        expect(page.locator("#pd-counter")).to_be_hidden()
        expect(page.locator("#pd-prev")).to_be_hidden()
        expect(page.locator("#pd-next")).to_be_hidden()
        expect(page.locator("#pd-conversation")).to_have_count(0)
        assert "1 / 1" not in page.locator("body").inner_text()
        assert not conversation_requests, "empty parade requested slide conversations: %r" % conversation_requests
        if evidence_dir:
            page.screenshot(path=os.path.join(evidence_dir, "zero-slide-390.png"), full_page=True)

        empty.get_by_role("button", name="Back to all parades").click()
        expect(page.locator("#pd-list-view")).to_be_visible()
        expect(page.locator(".pd-listcard")).to_have_count(2)
        expect(page.locator(".pd-listcard").filter(has_text="2026-07-17")).to_contain_text("0 slides · (empty)")
        one = page.locator(".pd-listcard").filter(has_text="2026-07-16")
        expect(one).to_contain_text("1 slide · One honest slide")

        with page.expect_response(lambda response: "/api/parades/2026-07-16/conversations" in response.url):
            one.click()
        expect(page.locator("#pd-counter")).to_be_visible()
        expect(page.locator("#pd-counter")).to_have_text("1 / 1")
        expect(page.locator("#pd-prev")).to_be_visible()
        expect(page.locator("#pd-prev")).to_be_disabled()
        expect(page.locator("#pd-next")).to_be_visible()
        expect(page.locator("#pd-next")).to_be_disabled()
        conversation = page.locator("#pd-conversation")
        expect(conversation).to_be_visible()
        conversation.locator("summary").click()
        expect(conversation.locator("textarea[name=text]")).to_be_visible()
        expect(conversation.get_by_role("button", name="Send to CoS")).to_be_visible()
        if evidence_dir:
            page.screenshot(path=os.path.join(evidence_dir, "one-slide-390.png"), full_page=True)
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, evidenceDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Parade zero/one-slide regression: %v\n%s", err, out)
	}
	if evidenceDir != "" {
		for _, name := range []string{"zero-slide-390.png", "one-slide-390.png"} {
			path := filepath.Join(evidenceDir, name)
			if info, err := os.Stat(path); err != nil || info.Size() == 0 {
				t.Fatalf("rendered Parade evidence missing at %q: %v", path, err)
			}
			t.Logf("generic rendered evidence: %s", path)
		}
	}
}
