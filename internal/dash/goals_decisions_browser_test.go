package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestRDReadingRoomRendered863 exercises the combined Decisions + Research
// surface at both phone contracts and desktop. Fixtures are generic; captures,
// when requested, remain local evidence.
func TestRDReadingRoomRendered863(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Chromium regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
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
import json
import os
import sys
from urllib.parse import unquote, urlparse
from playwright.sync_api import sync_playwright, expect

url, evidence_dir = sys.argv[1], sys.argv[2]
entries = []
goals = []
for i in range(7):
    entries.append({
        "id": "decisions/generic-%d.md" % (i + 1),
        "title": "Generic decision %d" % (i + 1),
        "status": "operator-review", "decision": True,
        "publication": {"explicit": True, "classification": "decision"},
        "summary": "Recommendation %d with a reversible safe default." % (i + 1),
        "updated_at": "2026-07-%02dT12:00:00Z" % (i + 1)
    })
    if i == 0:
        brief = (
            "## Recommendation\n**Keep option 1 reversible** while the generic operating team "
            "reviews a deliberately long sequence of reversible checks, bounded observations, "
            "and routine follow-up steps without exposing raw formatting markers or turning "
            "the card into the full operational brief.\n\n## Safe default\nHold the current state."
            "\n\n[Read paper](/research/decisions/generic-1.md)"
        )
    else:
        brief = "## Recommendation\nUse \x60option %d\x60 with **bounded evidence**.\n\n## Safe default\nHold the current state." % (i + 1)
        if i > 1:
            brief += "\n\n[Read paper](/research/decisions/generic-%d.md)" % (i + 1)
    paper_id = "decisions/missing.md" if i == 1 else "decisions/generic-%d.md" % (i + 1)
    goals.append({
        "id": "generic-%d" % (i + 1), "title": "Generic decision %d" % (i + 1),
        "owner": "example-desk", "conversation_agent": "example-desk",
        "status_display": "awaiting", "state": "awaiting",
        "brief": brief if i == 2 else "",
        "work_items": [{
            "class": "awaiting", "detail": "awaiting-auth",
            "label": "authorize the bounded choice",
            "brief": brief, "paper_id": paper_id
        }]
    })
# These look superficially gated but are not real operator decisions. A stale
# blocked roll-up whose bound work is done and a seat-loop posture must never
# enter the Waiting on you shelf, even when they carry valid paper links.
goals.extend([
    {
        "id": "stale-blocked", "title": "Stale blocked roll-up",
        "owner": "example-desk", "conversation_agent": "example-desk",
        "status_display": "blocked", "state": "blocked",
        "brief": "## Recommendation\nDo not revive completed work.\n\n[Read paper](/research/decisions/generic-3.md)",
        "work_items": [
            {"class": "done", "label": "already complete"},
            {
                "class": "awaiting", "label": "current child decision",
                "detail": "awaiting-auth",
                "brief": "## 1. What it is\nYour fleet needs one current child choice.\n\n## Recommendation\nChoose the bounded child option.",
                "paper_id": "decisions/generic-2.md"
            },
            {
                "class": "blocked", "label": "ordinary product dependency",
                "detail": "blocked",
                "brief": "## 1. What it is\nA dependency is blocked, but it is not your authority gate.\n\n## Recommendation\nKeep diagnosing inside the fleet.",
                "paper_id": "library/evidence-1.md"
            }
        ]
    },
    {
        "id": "seat-posture", "title": "Seat awaiting authority",
        "status_display": "awaiting-authority", "state": "awaiting-authority",
        "brief": "## Recommendation\nAssign ordinary work.\n\n[Read paper](/research/decisions/generic-4.md)",
        "work_items": []
    }
])
for i in range(12):
    entries.append({
        "id": "library/evidence-%d.md" % (i + 1),
        "title": "Evidence note %d" % (i + 1),
        "status": "research", "decision": False,
        "learn_ready": True, "presentation_ready": True,
        "summary": "Focused evidence %d for a generic investigation." % (i + 1),
        "updated_at": "2026-06-%02dT12:00:00Z" % (i + 1)
    })

def document_for(route):
    path = unquote(urlparse(route.request.url).path)
    doc_id = path.split("/api/research/", 1)[1]
    match = next(item for item in entries if item["id"] == doc_id)
    body = dict(match)
    body.update({
        "markdown": "# %s\n\n## Recommendation\n\nKeep the change reversible.\n\n## Evidence\n\nThe paper canvas owns this decision." % match["title"],
        "digest": "sha256:generic"
    })
    route.fulfill(status=200, content_type="application/json", body=json.dumps(body))

def prepare(page):
    page.set_default_timeout(8000)
    page.add_init_script("window.EventSource = undefined")
    page.route("**/api/research", lambda route: route.fulfill(
        status=200, content_type="application/json", body=json.dumps({"research": entries})))
    page.route("**/api/goals", lambda route: route.fulfill(
        status=200, content_type="application/json",
        body=json.dumps({"found": True, "goals": goals, "counts": {"total": 7, "awaiting": 7}})))
    page.route("**/api/research/**", document_for)
    page.route("**/api/research-annotations/**", lambda route: route.fulfill(
        status=200, content_type="application/json",
        body=json.dumps({"document_id": "", "generation": 0, "annotations": []})))
    page.route("**/api/control/respond", lambda route: route.fulfill(
        status=200, content_type="application/json",
        body=json.dumps({"outcome": "queued", "target": "example-desk", "queued_id": "generic-queue"})))

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        for width, height in [(390, 844), (360, 800)]:
            page = browser.new_page(viewport={"width": width, "height": height})
            prepare(page)
            page.goto(url + "/research?focus=decisions", wait_until="domcontentloaded")
            expect(page.locator("#research-library-title")).to_have_text("R&D")
            expect(page.locator('[data-research-focus="decisions"]')).to_have_attribute("aria-pressed", "true")
            expect(page.locator("#research-decision-list .research-card")).to_have_count(3)
            expect(page.locator("#research-learn")).to_be_hidden()
            expect(page.locator("#research-filter-status")).to_have_text("7 waiting decisions")
            expect(page.locator("#gdec-detail")).to_have_count(0)
            assert page.evaluate("document.documentElement.scrollWidth === innerWidth")
            cards = page.locator("#research-decision-list .research-card")
            formatted = cards.filter(has_text="Generic decision 1")
            summary = formatted.locator(".research-card-summary")
            assert "**" not in summary.inner_text()
            assert summary.inner_text().startswith("Your next move · ")
            assert len(summary.inner_text()) <= 205
            assert summary.inner_text().endswith("…")
            expect(formatted.locator(".research-card-next")).to_have_text("Open working paper →")
            expect(formatted.locator(".research-card-blocker")).to_have_text(
                "Why you care · authorize the bounded choice")
            missing_paper = cards.filter(has_text="Generic decision 2")
            expect(missing_paper).to_have_count(0)
            page.locator("#research-decision-more").click()
            page.locator("#research-decision-more").click()
            expect(cards).to_have_count(7)
            current_child = cards.filter(has_text="Stale blocked roll-up")
            expect(current_child).to_have_count(1)
            expect(current_child).to_contain_text("Your fleet needs one current child choice.")
            expect(current_child).not_to_contain_text("Do not revive completed work")
            expect(current_child).not_to_contain_text("dependency is blocked")
            for card in cards.all():
                assert card.locator(".research-card-next").count() == 1
                assert card.get_attribute("href").startswith("/research/")
            assert cards.locator("img, script").count() == 0

            # A decision opens its paper in the one R&D canvas, never a dialog stack.
            formatted.focus()
            expect(formatted).to_be_focused()
            formatted.press("Enter")
            expect(page.locator("#research-title")).to_have_text("Generic decision 1")
            expect(page.locator("#research-body")).to_contain_text("The paper canvas owns this decision.")
            expect(page).to_have_url(url + "/research/decisions/generic-1.md")
            expect(page.locator("#research-annotation-bar")).to_be_visible()
            expect(page.locator("#research-decision-summary")).to_contain_text("What you decide ·")
            expect(page.locator("#research-decision-respond")).to_have_text("Tell your fleet")
            page.locator("#research-decision-respond").click()
            expect(page.locator('label[for="research-decision-response-input"]')).to_have_text("What you decide")
            expect(page.locator("#research-decision-response-send")).to_have_text("Send to your fleet")
            page.locator("#research-decision-response-input").fill("Approve the reversible option.")
            page.locator("#research-decision-response-send").click()
            expect(page.locator("#research-decision-response-status")).to_have_text(
                "Queued for your fleet at example-desk — not delivered yet.")
            expect(page.locator("#research-decision-response-close")).to_be_focused()
            if evidence_dir:
                page.screenshot(path=os.path.join(evidence_dir, "rd-decision-phone-%d.png" % width), full_page=False)
            page.locator("#research-decision-response-close").click()
            expect(page.locator("#research-decision-response")).to_be_hidden()
            expect(page.locator("#research-decision-respond")).to_be_focused()
            page.locator("#research-back").click()
            expect(page).to_have_url(url + "/research?focus=decisions")

            # Learn is a separate, bounded teaching lane; it never mixes raw
            # archive material into the operator's decision queue.
            page.locator('[data-research-focus="learn"]').click()
            expect(page.locator("#research-decisions")).to_be_hidden()
            expect(page.locator("#research-learn-list .research-card")).to_have_count(6)
            expect(page.locator("#research-filter-status")).to_have_text("12 educational showpieces")
            page.locator("#research-search").fill("evidence 11")
            expect(page.locator("#research-learn-list .research-card")).to_have_count(1)
            expect(page.locator("#research-learn-list")).to_contain_text("Evidence note 11")
            page.locator("#research-search").fill("")
            expect(page.locator("#research-learn-list .research-card")).to_have_count(6)
            assert page.evaluate("document.documentElement.scrollWidth === innerWidth")
            if evidence_dir:
                page.screenshot(path=os.path.join(evidence_dir, "rd-phone-%d.png" % width), full_page=False)
            page.close()

        desktop = browser.new_page(viewport={"width": 1440, "height": 900})
        prepare(desktop)
        desktop.goto(url, wait_until="domcontentloaded")
        expect(desktop.locator("#view-decisions")).to_have_count(0)
        expect(desktop.locator("#tab-decisions")).to_contain_text("R&D")
        # The dashboard badge is a broader Goals posture count; the R&D shelf
        # independently admits seven exact authority gates with explicit
        # decision-class papers; an unbound paper fails closed.
        expect(desktop.locator("#hdr-decisions-count")).to_have_text("10")
        desktop.locator("#tab-decisions").click()
        expect(desktop).to_have_url(url + "/research?focus=decisions")
        expect(desktop.locator("#research-decision-list .research-card")).to_have_count(3)
        if evidence_dir:
            desktop.screenshot(path=os.path.join(evidence_dir, "rd-desktop-1440.png"), full_page=False)
        desktop.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, evidenceDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered R&D reading-room regression: %v\n%s", err, out)
	}
	if evidenceDir != "" {
		for _, name := range []string{"rd-phone-390.png", "rd-phone-360.png", "rd-decision-phone-390.png", "rd-decision-phone-360.png", "rd-desktop-1440.png"} {
			path := filepath.Join(evidenceDir, name)
			if info, err := os.Stat(path); err != nil || info.Size() == 0 {
				t.Fatalf("rendered evidence missing at %q: %v", path, err)
			}
			t.Logf("generic rendered evidence: %s", path)
		}
	}
}
