package dash

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestResearchAnnotationsRendered833 is a wholly generic browser fixture. It
// covers the selection action, attached and stale passage states, document
// comments, the phone sheet, the desktop drawer, and loud draft retention.
func TestResearchAnnotationsRendered833(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Research annotation regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, _ := newTestServer(t, singleFleetRoster, time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC))
	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(func() {
		httpServer.CloseClientConnections()
		httpServer.Close()
	})

	digest := "sha256:" + strings.Repeat("a", 64)
	markdown := "# Field note\n\n## Finding\n\nAlpha target sentence for the attached note. A second passage waits for a new comment.\n"
	document, err := json.Marshal(map[string]any{
		"id": "notes/field-note.md", "title": "Field note", "status": "research",
		"updated_at": "2026-07-23T12:00:00Z", "markdown": markdown, "digest": digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	annotations, err := json.Marshal(map[string]any{
		"schema": 1, "document_id": "notes/field-note.md", "document_digest": digest, "generation": 3,
		"annotations": []map[string]any{
			{
				"id": "ra_attached", "document_id": "notes/field-note.md", "document_digest": digest,
				"anchor":            map[string]any{"quote": "target sentence", "prefix": "Alpha ", "suffix": " for the", "start": 28, "end": 43},
				"anchor_resolution": map[string]any{"state": "attached", "start": 28, "end": 43},
				"author":            "operator", "created_at": "2026-07-23T10:00:00Z", "updated_at": "2026-07-23T10:00:00Z",
				"comments": []map[string]any{{"id": "rc_1", "author": "operator", "text": "<img src=x onerror=alert(1)> stays text", "created_at": "2026-07-23T10:00:00Z"}},
				"resolved": false,
			},
			{
				"id": "ra_stale", "document_id": "notes/field-note.md", "document_digest": "sha256:" + strings.Repeat("b", 64),
				"anchor":            map[string]any{"quote": "Removed finding", "prefix": "", "suffix": "", "start": 4, "end": 19},
				"anchor_resolution": map[string]any{"state": "needs_review", "start": 0, "end": 0},
				"author":            "operator", "created_at": "2026-07-22T10:00:00Z", "updated_at": "2026-07-22T10:00:00Z",
				"comments": []map[string]any{{"id": "rc_2", "author": "operator", "text": "Keep this stale note visible.", "created_at": "2026-07-22T10:00:00Z"}},
				"resolved": false,
			},
			{
				"id": "ra_document", "document_id": "notes/field-note.md", "document_digest": digest,
				"author": "operator", "created_at": "2026-07-21T10:00:00Z", "updated_at": "2026-07-21T10:00:00Z",
				"comments": []map[string]any{{"id": "rc_3", "author": "operator", "text": "Whole-document context.", "created_at": "2026-07-21T10:00:00Z"}},
				"resolved": false,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	script := `
import json
import sys
from playwright.sync_api import sync_playwright, expect

url, document_body, annotation_body = sys.argv[1], sys.argv[2], sys.argv[3]
document = json.loads(document_body)
initial = json.loads(annotation_body)

def install(page, empty=False):
    writes = [0]
    page.add_init_script("window.EventSource = undefined")
    page.route("**/api/research", lambda route: route.fulfill(status=200, content_type="application/json", body=json.dumps({"research": [{"id": document["id"], "title": document["title"], "status": "research", "updated_at": document["updated_at"]}]})))
    page.route("**/api/research/notes/field-note.md", lambda route: route.fulfill(status=200, content_type="application/json", body=document_body))
    def annotations(route):
        if route.request.method == "GET":
            state = dict(initial)
            if empty:
                state["generation"] = 0
                state["annotations"] = []
            route.fulfill(status=200, content_type="application/json", body=json.dumps(state))
            return
        assert route.request.headers.get("x-flotilla-dash") == "1"
        writes[0] += 1
        payload = route.request.post_data_json
        if writes[0] == 1:
            route.fulfill(status=503, content_type="application/json", body=json.dumps({"error": "generic write service unavailable"}))
            return
        created = {
            "id": "ra_created", "document_id": document["id"], "document_digest": document["digest"],
            "author": "operator", "created_at": "2026-07-23T12:00:00Z", "updated_at": "2026-07-23T12:00:00Z",
            "comments": [{"id": "rc_created", "author": "operator", "text": payload["comment"], "created_at": "2026-07-23T12:00:00Z"}],
            "resolved": False,
        }
        if payload.get("anchor"):
            created["anchor"] = payload["anchor"]
            created["anchor_resolution"] = {"state": "attached", "start": payload["anchor"]["start"], "end": payload["anchor"]["end"]}
        else:
            created["id"] = "ra_document_created"
        state = dict(initial)
        state["generation"] = 4
        state["annotations"] = initial["annotations"] + [created]
        state["created"] = created
        route.fulfill(status=201, content_type="application/json", body=json.dumps(state))
    page.route("**/api/research-annotations/**", annotations)

def select_text(page, quote):
    page.locator("#research-body").evaluate("""(root, quote) => {
      const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
      let node;
      while ((node = walker.nextNode())) {
        const start = node.nodeValue.indexOf(quote);
        if (start >= 0) {
          const range = document.createRange();
          range.setStart(node, start); range.setEnd(node, start + quote.length);
          const selection = window.getSelection(); selection.removeAllRanges(); selection.addRange(range);
          root.dispatchEvent(new MouseEvent("mouseup", {bubbles:true}));
          return;
        }
      }
      throw new Error("quote not found");
    }""", quote)

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        phone = browser.new_page(viewport={"width": 390, "height": 844})
        install(phone)
        phone.goto(url + "/research/notes/field-note.md", wait_until="domcontentloaded")
        expect(phone.locator("#research-annotation-count")).to_have_text("3 annotations")
        expect(phone.locator("#research-annotation-summary")).to_contain_text("1 passage needs review")
        expect(phone.locator(".research-highlight")).to_have_count(1)
        phone.locator(".research-highlight").click()
        panel = phone.locator("#research-annotation-panel")
        expect(panel).to_be_visible()
        expect(phone.locator("#research-annotation-comments")).to_contain_text("<img src=x onerror=alert(1)> stays text")
        expect(phone.locator("#research-annotation-comments img")).to_have_count(0)
        expect(phone.locator(".research-annotation-card.is-stale")).to_contain_text("Needs review")
        phone.keyboard.press("Shift+Tab")
        assert panel.evaluate("node => node.contains(document.activeElement)")
        sheet = panel.evaluate("node => { const r=node.getBoundingClientRect(); return {left:r.left,right:r.right,top:r.top,bottom:r.bottom,width:r.width} }")
        assert sheet["left"] >= 0 and sheet["right"] <= 390 and sheet["top"] >= 0 and sheet["bottom"] <= 844, sheet
        phone.keyboard.press("Escape")
        expect(panel).to_be_hidden()
        assert phone.locator(".research-highlight").evaluate("node => document.activeElement === node")

        quote = "A second passage waits for a new comment."
        select_text(phone, quote)
        action = phone.locator("#research-selection-action")
        expect(action).to_be_visible()
        action_box = action.evaluate("node => { const r=node.getBoundingClientRect(), b=node.querySelector('button').getBoundingClientRect(); return {left:r.left,right:r.right,top:r.top,bottom:r.bottom,buttonHeight:b.height} }")
        assert action_box["left"] >= 0 and action_box["right"] <= 390 and action_box["top"] >= 0 and action_box["bottom"] <= 844 and action_box["buttonHeight"] >= 44, action_box
        action.locator("button").focus()
        action.locator("button").press("Enter")
        expect(phone.locator("#research-annotation-form-title")).to_have_text("Comment on passage")
        expect(phone.locator("#research-annotation-draft-quote")).to_have_text(quote)
        draft = "Long retained draft — " + ("generic context " * 28)
        phone.locator("#research-annotation-draft").fill(draft)
        phone.locator("#research-annotation-save").click()
        expect(phone.locator("#research-annotation-save-status")).to_contain_text("Not saved")
        expect(phone.locator("#research-annotation-save-status")).to_contain_text("draft is still here")
        assert phone.locator("#research-annotation-draft").input_value() == draft
        phone.locator("#research-annotation-save").click()
        expect(phone.locator("#research-annotation-thread-title")).to_have_text("Passage thread")
        expect(phone.locator("#research-annotation-count")).to_have_text("4 annotations")
        expect(phone.locator(".research-highlight")).to_have_count(2)
        phone.keyboard.press("Escape")
        phone.locator("#research-document-comment").click()
        expect(phone.locator("#research-annotation-form-title")).to_have_text("Comment on this document")
        expect(phone.locator("#research-annotation-draft-quote")).to_be_hidden()
        phone.locator("#research-annotation-draft").fill("New whole-document context.")
        phone.locator("#research-annotation-save").click()
        expect(phone.locator("#research-annotation-thread-title")).to_have_text("Document comment")
        expect(phone.locator("#research-annotation-quote")).to_be_hidden()
        phone.keyboard.press("Escape")
        phone_metrics = phone.evaluate("() => ({width:document.documentElement.scrollWidth, client:document.documentElement.clientWidth})")
        assert phone_metrics["width"] == phone_metrics["client"], phone_metrics

        desktop = browser.new_page(viewport={"width": 1440, "height": 900})
        install(desktop, empty=True)
        desktop.goto(url + "/research/notes/field-note.md", wait_until="domcontentloaded")
        expect(desktop.locator("#research-annotation-count")).to_have_text("0 annotations")
        desktop.locator("#research-document-comment").click()
        expect(desktop.locator("#research-annotation-empty")).to_be_visible()
        expect(desktop.locator("#research-annotation-form")).to_be_visible()
        geometry = desktop.evaluate("""() => {
          const panel=document.querySelector('#research-annotation-panel').getBoundingClientRect();
          const body=document.querySelector('#research-body').getBoundingClientRect();
          return {panelLeft:panel.left,panelRight:panel.right,bodyRight:body.right,width:document.documentElement.scrollWidth,client:document.documentElement.clientWidth};
        }""")
        assert geometry["panelLeft"] >= geometry["bodyRight"] - 1, geometry
        assert geometry["panelRight"] <= 1440 and geometry["width"] == geometry["client"], geometry
        print(json.dumps({"phone_sheet": sheet, "selection_action": action_box, "phone": phone_metrics, "desktop": geometry}))
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, string(document), string(annotations))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Research annotations regression: %v\n%s", err, out)
	} else {
		t.Logf("generic annotation metrics:\n%s", out)
	}
}
