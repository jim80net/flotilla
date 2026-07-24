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
second = dict(document)
second["id"] = "notes/second-note.md"
second["title"] = "Second note"
second["digest"] = "sha256:" + ("c" * 64)
second["markdown"] = "# Second note\n\n## Finding\n\nBeta document remains isolated from Alpha annotation requests.\n"

def install(page, empty=False, race=None):
    writes = [0]
    page.add_init_script("window.EventSource = undefined")
    page.route("**/api/research", lambda route: route.fulfill(status=200, content_type="application/json", body=json.dumps({"research": [
        {"id": document["id"], "title": document["title"], "status": "research", "updated_at": document["updated_at"]},
        {"id": second["id"], "title": second["title"], "status": "research", "updated_at": second["updated_at"]}
    ]})))
    page.route("**/api/research/notes/field-note.md", lambda route: route.fulfill(status=200, content_type="application/json", body=document_body))
    page.route("**/api/research/notes/second-note.md", lambda route: route.fulfill(status=200, content_type="application/json", body=json.dumps(second)))
    def annotations(route):
        if route.request.method == "GET":
            if race is not None and race["hold_get"] and route.request.url.endswith("/notes/field-note.md"):
                race["pending_get"].append(route)
                return
            if route.request.url.endswith("/notes/second-note.md"):
                route.fulfill(status=200, content_type="application/json", body=json.dumps({
                    "schema": 1, "document_id": second["id"], "document_digest": second["digest"],
                    "generation": 1, "annotations": []
                }))
                return
            state = dict(initial)
            if empty:
                state["generation"] = 0
                state["annotations"] = []
            route.fulfill(status=200, content_type="application/json", body=json.dumps(state))
            return
        assert route.request.headers.get("x-flotilla-dash") == "1"
        if race is not None and race["hold_post"]:
            race["pending_post"].append(route)
            return
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

        # A stale annotation GET failure from document A cannot replace document B's
        # loaded annotation state. A late successful A save likewise cannot clear B's
        # draft, repaint A annotations, or reopen A's thread.
        race_state = {"hold_get": True, "pending_get": [], "hold_post": False, "pending_post": []}
        race = browser.new_page(viewport={"width": 390, "height": 844})
        install(race, race=race_state)
        race.goto(url + "/research/notes/field-note.md", wait_until="domcontentloaded")
        expect(race.locator("#research-title")).to_have_text("Field note")
        for _ in range(50):
            if race_state["pending_get"]: break
            race.wait_for_timeout(10)
        assert len(race_state["pending_get"]) == 1
        race.locator("#research-back").click()
        race.locator('[data-research-focus="library"]').click()
        race.locator("#research-list .research-card").filter(has_text="Second note").click()
        expect(race.locator("#research-title")).to_have_text("Second note")
        expect(race.locator("#research-annotation-count")).to_have_text("0 annotations")
        race_state["pending_get"].pop().fulfill(
            status=503, content_type="application/json", body=json.dumps({"error": "late Alpha failure"}))
        race.wait_for_timeout(50)
        expect(race.locator("#research-title")).to_have_text("Second note")
        expect(race.locator("#research-annotation-count")).to_have_text("0 annotations")
        expect(race.locator("#research-annotations-retry")).to_be_hidden()

        race_state["hold_get"] = False
        race.locator("#research-back").click()
        race.locator('[data-research-focus="library"]').click()
        race.locator("#research-list .research-card").filter(has_text="Field note").click()
        expect(race.locator("#research-annotation-count")).to_have_text("3 annotations")
        race.locator("#research-document-comment").click()
        race.locator("#research-annotation-draft").fill("Alpha save remains in flight.")
        race_state["hold_post"] = True
        race.locator("#research-annotation-save").click()
        for _ in range(50):
            if race_state["pending_post"]: break
            race.wait_for_timeout(10)
        assert len(race_state["pending_post"]) == 1
        race.locator("#research-annotation-close").click()
        race.locator("#research-back").click()
        race.locator('[data-research-focus="library"]').click()
        race.locator("#research-list .research-card").filter(has_text="Second note").click()
        expect(race.locator("#research-annotation-count")).to_have_text("0 annotations")
        race.locator("#research-document-comment").click()
        beta_draft = "Beta draft must survive Alpha completion."
        race.locator("#research-annotation-draft").fill(beta_draft)
        alpha_success = dict(initial)
        alpha_success["generation"] = 4
        alpha_success["created"] = initial["annotations"][0]
        race_state["pending_post"].pop().fulfill(
            status=201, content_type="application/json", body=json.dumps(alpha_success))
        race.wait_for_timeout(50)
        expect(race.locator("#research-title")).to_have_text("Second note")
        expect(race.locator("#research-annotation-count")).to_have_text("0 annotations")
        expect(race.locator("#research-annotation-draft")).to_have_value(beta_draft)
        expect(race.locator("#research-annotation-form")).to_be_visible()
        race.close()
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
