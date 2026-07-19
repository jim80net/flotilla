package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/paradeconversation"
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
        page.on("request", lambda request: conversation_requests.append(request.url) if "/conversations" in request.url and not request.url.endswith("/meta") else None)
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

// TestParadeUnreadNavigationRendered812 proves the operator-visible lifecycle:
// an agent reply arriving after page load glows without a hard refresh, either
// nav bar jumps to the deterministic unread thread, and only a successfully
// opened thread clears its indicators.
func TestParadeUnreadNavigationRendered812(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Parade regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}
	srv, dir := newTestServer(t, singleFleetRoster, time.Date(2026, 7, 18, 14, 0, 0, 0, time.UTC))
	date := "2026-07-18"
	paradeDir := filepath.Join(dir, "parades", date)
	if err := os.MkdirAll(paradeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paradeDir, "slides.md"), []byte("Alpha · A visible outcome\n\nThe result is now clear.\n\n---\n\nBeta · A second outcome\n\nThe follow-up is also clear."), 0o600); err != nil {
		t.Fatal(err)
	}
	conversationPath := filepath.Join(paradeDir, "conversations.json")
	if _, err := paradeconversation.Append(conversationPath, 0, "Alpha · A visible outcome", paradeconversation.Message{
		ID: "operator-1", TS: "2026-07-18T14:00:00Z", Author: "operator", Kind: "note", Text: "What changed?",
	}); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(func() { httpServer.CloseClientConnections(); httpServer.Close() })
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
from playwright.sync_api import sync_playwright, expect

url, conversation_path, evidence_dir = sys.argv[1], sys.argv[2], sys.argv[3]
with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        page = browser.new_page(viewport={"width": 390, "height": 844})
        page.set_default_timeout(8000)
        page.add_init_script("window.__FLOTILLA_PARADE_POLL_MS = 100")
        page.goto(url + "/parade", wait_until="domcontentloaded")
        conversation = page.locator("#pd-conversation")
        expect(conversation).to_be_visible()
        expect(conversation.locator(".pd-convo-count")).to_have_text("1")
        expect(conversation.locator(".pd-unread-dot")).to_have_count(0)

        with open(conversation_path, "r", encoding="utf-8") as handle:
            doc = json.load(handle)
        doc["slides"]["0"]["messages"].append({
            "id": "agent-1", "ts": "2026-07-18T14:01:00Z", "author": "alpha-desk",
            "kind": "note", "text": "The first unread reply is visible on this page."
        })
        doc["slides"]["1"] = {
            "title": "Beta · A second outcome",
            "messages": [{
                "id": "agent-2", "ts": "2026-07-18T14:02:00Z", "author": "beta-desk",
                "kind": "note", "text": "The second unread reply is visible on this page."
            }]
        }
        tmp = conversation_path + ".browser.tmp"
        with open(tmp, "w", encoding="utf-8") as handle:
            json.dump(doc, handle)
        os.replace(tmp, conversation_path)

        expect(conversation.locator(".pd-unread-dot")).to_be_visible(timeout=5000)
        expect(page.locator("#pd-global-unread")).to_be_visible()
        expect(conversation.locator(".pd-convo-count")).to_have_text("2")
        deck_jump = page.locator("#pd-unread-jump")
        expect(deck_jump).to_be_visible()
        expect(deck_jump).to_have_attribute("aria-label", "Open unread parade reply from 2026-07-18, slide 1")

        # Move away from the deterministic first target, then enter the archive
        # chrome. Both unread threads must remain reachable from either nav bar.
        page.locator("#pd-next").click()
        expect(page.locator("#pd-counter")).to_have_text("2 / 2")
        expect(page.locator("#pd-conversation .pd-unread-dot")).to_be_visible()
        page.locator("#pd-close").click()
        list_jump = page.locator("#pd-list-unread-jump")
        expect(list_jump).to_be_visible()
        expect(list_jump).to_have_attribute("aria-label", "Open unread parade reply from 2026-07-18, slide 1")
        expect(page.locator(".pd-listcard .pd-unread-dot")).to_be_visible()
        if evidence_dir:
            page.screenshot(path=os.path.join(evidence_dir, "parade-unread-390.png"), full_page=True)

        # Newest parade + lowest slide index wins. The jump opens the actual
        # conversation before clearing that thread's read marker.
        list_jump.click()
        expect(page.locator("#pd-counter")).to_have_text("1 / 2")
        conversation = page.locator("#pd-conversation")
        expect(conversation).to_have_attribute("open", "")
        expect(conversation.locator(".pd-convo-thread")).to_contain_text("first unread reply")
        expect(conversation.locator(".pd-unread-dot")).to_have_count(0)
        expect(deck_jump).to_be_visible()
        expect(deck_jump).to_have_attribute("aria-label", "Open unread parade reply from 2026-07-18, slide 2")

        # The second activation advances to the remaining thread. Only after it
        # is open do all global/list/deck glows clear.
        deck_jump.click()
        expect(page.locator("#pd-counter")).to_have_text("2 / 2")
        conversation = page.locator("#pd-conversation")
        expect(conversation).to_have_attribute("open", "")
        expect(conversation.locator(".pd-convo-thread")).to_contain_text("second unread reply")
        expect(deck_jump).to_be_hidden()
        expect(page.locator("#pd-global-unread")).to_be_hidden()
        page.locator("#pd-close").click()
        expect(page.locator("#pd-list-unread-jump")).to_be_hidden()
        expect(page.locator(".pd-listcard .pd-unread-dot")).to_have_count(0)

        # A thread-body failure may open honest error chrome, but it must not
        # advance the local read cursor or clear either navigation glow.
        failed = browser.new_page(viewport={"width": 390, "height": 844})
        failed.set_default_timeout(8000)
        failed.add_init_script("window.__FLOTILLA_PARADE_POLL_MS = 100")
        failed.route("**/api/parades/2026-07-18/conversations", lambda route: route.fulfill(
            status=503, content_type="application/json", body='{"error":"synthetic unavailable"}'))
        failed.goto(url + "/parade", wait_until="domcontentloaded")
        failed_jump = failed.locator("#pd-unread-jump")
        expect(failed_jump).to_be_visible(timeout=5000)
        failed_jump.click()
        expect(failed.locator("#pd-conversation")).to_have_attribute("open", "")
        expect(failed.locator(".pd-convo-error")).to_contain_text("unavailable")
        expect(failed_jump).to_be_visible()
        expect(failed.locator("#pd-global-unread")).to_be_visible()
        failed.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, conversationPath, evidenceDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Parade unread regression: %v\n%s", err, out)
	}
	if evidenceDir != "" {
		path := filepath.Join(evidenceDir, "parade-unread-390.png")
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Fatalf("rendered evidence missing at %q: %v", path, err)
		}
		t.Logf("generic rendered evidence: %s", path)
	}
}
