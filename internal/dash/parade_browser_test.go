package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestParadeUnreadReplyRendered793 proves the operator-visible lifecycle: an
// agent reply arriving after page load glows without a hard refresh, and opening
// the thread clears both thread and navigation indicators.
func TestParadeUnreadReplyRendered793(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(paradeDir, "slides.md"), []byte("Alpha · A visible outcome\n\nThe result is now clear."), 0o600); err != nil {
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
            "kind": "note", "text": "The reply is visible on this page."
        })
        tmp = conversation_path + ".browser.tmp"
        with open(tmp, "w", encoding="utf-8") as handle:
            json.dump(doc, handle)
        os.replace(tmp, conversation_path)

        expect(conversation.locator(".pd-unread-dot")).to_be_visible(timeout=5000)
        expect(page.locator("#pd-global-unread")).to_be_visible()
        expect(conversation.locator(".pd-convo-count")).to_have_text("2")
        if evidence_dir:
            page.screenshot(path=os.path.join(evidence_dir, "parade-unread-390.png"), full_page=True)

        conversation.locator("summary").click()
        expect(conversation.locator(".pd-convo-thread")).to_be_visible()
        expect(conversation.locator(".pd-unread-dot")).to_have_count(0)
        expect(page.locator("#pd-global-unread")).to_be_hidden()
        page.locator("#pd-close").click()
        expect(page.locator(".pd-listcard .pd-unread-dot")).to_have_count(0)
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

// TestParadeDragScrollDoesNotAdvanceRendered locks the operator's reading
// contract: mouse drag-release and document scrolling stay on the current
// slide; only the visible navigation controls page the deck.
func TestParadeDragScrollDoesNotAdvanceRendered(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Parade regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, dir := newTestServer(t, singleFleetRoster, time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	paradeDir := filepath.Join(dir, "parades", "2026-07-22")
	if err := os.MkdirAll(paradeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	longBody := "First readable slide\n\n" + strings.Repeat("A paragraph that should scroll without changing slides.\n\n", 36) + "---\n\nSecond slide\n\nOnly explicit navigation reaches this slide."
	if err := os.WriteFile(filepath.Join(paradeDir, "slides.md"), []byte(longBody), 0o600); err != nil {
		t.Fatal(err)
	}

	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(func() { httpServer.CloseClientConnections(); httpServer.Close() })
	script := `
import sys
from playwright.sync_api import sync_playwright, expect

url = sys.argv[1]
with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        page = browser.new_page(viewport={"width": 390, "height": 844})
        page.set_default_timeout(8000)
        page.goto(url + "/parade", wait_until="domcontentloaded")
        counter = page.locator("#pd-counter")
        expect(counter).to_have_text("1 / 2")

        stage = page.locator("#pd-stage")
        box = stage.bounding_box()
        page.mouse.move(box["x"] + box["width"] - 30, box["y"] + 250)
        page.mouse.down()
        page.mouse.move(box["x"] + 30, box["y"] + 120, steps=8)
        page.mouse.up()
        expect(counter).to_have_text("1 / 2")

        stage.click(position={"x": box["width"] - 24, "y": 180})
        expect(counter).to_have_text("1 / 2")
        page.mouse.wheel(0, 500)
        expect(counter).to_have_text("1 / 2")

        page.locator("#pd-next").click()
        expect(counter).to_have_text("2 / 2")
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Parade drag-scroll regression: %v\n%s", err, out)
	}
}

// TestParadeMobileControlsClearContent849 proves the phone control row is a
// reserved layout region, not an overlay over the scrollable slide.
func TestParadeMobileControlsClearContent849(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Parade regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, dir := newTestServer(t, singleFleetRoster, time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC))
	paradeDir := filepath.Join(dir, "parades", "2026-07-23")
	if err := os.MkdirAll(filepath.Join(paradeDir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	longBody := "# Alpha XO · Read every line\n\n" +
		"[Open the generic source](https://example.invalid/source)\n\n" +
		strings.Repeat("Long generic narrative remains clear of explicit navigation controls.\n\n", 30) +
		"![Generic diagram](diagram.png)\n\nFinal readable line before the conversation.\n\n---\n\n# Second slide\n\nExplicit navigation arrives here."
	if err := os.WriteFile(filepath.Join(paradeDir, "slides.md"), []byte(longBody), 0o600); err != nil {
		t.Fatal(err)
	}
	// A tiny generic PNG; rendered evidence never uses production parade media.
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde}
	if err := os.WriteFile(filepath.Join(paradeDir, "assets", "diagram.png"), png, 0o600); err != nil {
		t.Fatal(err)
	}

	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(func() { httpServer.CloseClientConnections(); httpServer.Close() })
	script := `
import json
import sys
from playwright.sync_api import sync_playwright, expect

url = sys.argv[1]
def metrics(page):
    return page.evaluate("""() => {
      const slide=document.querySelector('#pd-slide').getBoundingClientRect();
      const controls=[...document.querySelectorAll('.pd-nav')].map(node => {
        const r=node.getBoundingClientRect();
        return {left:r.left,right:r.right,top:r.top,bottom:r.bottom,width:r.width,height:r.height,disabled:node.disabled};
      });
      const readable=[...document.querySelectorAll('.pd-slide-title,.pd-slide-body p,.pd-slide-body a,.pd-slide-img,.pd-conversation')].map(node => {
        const r=node.getBoundingClientRect();
        return {left:Math.max(r.left,slide.left),right:Math.min(r.right,slide.right),top:Math.max(r.top,slide.top),bottom:Math.min(r.bottom,slide.bottom)};
      }).filter(r => r.right > r.left && r.bottom > r.top);
      return {slide:{left:slide.left,right:slide.right,top:slide.top,bottom:slide.bottom},controls,readable,
        width:document.documentElement.scrollWidth,client:document.documentElement.clientWidth};
    }""")

def assert_clear(result, width, height):
    assert result["width"] == result["client"] == width, result
    for control in result["controls"]:
        assert control["left"] >= 0 and control["right"] <= width and control["top"] >= 0 and control["bottom"] <= height, result
        assert control["width"] >= 44 and control["height"] >= 44, result
        assert control["top"] >= result["slide"]["bottom"], result
        for content in result["readable"]:
            intersects = control["left"] < content["right"] and control["right"] > content["left"] and control["top"] < content["bottom"] and control["bottom"] > content["top"]
            assert not intersects, {"control":control,"content":content,"all":result}

with sync_playwright() as p:
    browser = p.chromium.launch()
    seen = []
    try:
        for width, height in [(390,844),(360,800)]:
            for theme in ["light","dark"]:
                page = browser.new_page(viewport={"width":width,"height":height})
                page.add_init_script("localStorage.setItem('flotilla-theme-v1', %s)" % json.dumps(theme))
                page.goto(url + "/parade", wait_until="domcontentloaded")
                expect(page.locator("html")).to_have_attribute("data-theme", theme)
                expect(page.locator("#pd-counter")).to_have_text("1 / 2")
                expect(page.locator("#pd-prev")).to_be_disabled()
                expect(page.locator("#pd-next")).to_be_enabled()
                expect(page.locator(".pd-nav-label")).to_have_count(2)
                before = metrics(page); assert_clear(before, width, height)
                slide = page.locator("#pd-slide")
                slide.evaluate("node => node.scrollTop = Math.floor(node.scrollHeight / 2)")
                middle = metrics(page); assert_clear(middle, width, height)
                slide.evaluate("node => node.scrollTop = node.scrollHeight")
                expect(page.locator("#pd-conversation")).to_be_visible()
                page.locator("#pd-conversation > summary").click()
                expect(page.locator("#pd-conversation")).to_have_attribute("open", "")
                after = metrics(page); assert_clear(after, width, height)
                expect(page.locator("#pd-next")).to_be_visible()
                page.locator("#pd-next").click()
                expect(page.locator("#pd-counter")).to_have_text("2 / 2")
                seen.append({"viewport":"%dx%d"%(width,height),"theme":theme,"before":before["controls"],"after":after["controls"]})
                page.close()
        print(json.dumps(seen))
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Parade mobile-control regression: %v\n%s", err, out)
	} else {
		t.Logf("generic mobile-control metrics:\n%s", out)
	}
}
