package dash

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResearchLibraryRendered822(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Research regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, _ := newTestServer(t, singleFleetRoster, time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC))
	root := t.TempDir()
	srv.cfg.ResearchPath = root
	var design strings.Builder
	design.WriteString(`# Authorization Domains — design for operator review

**Status:** DESIGN ONLY — no implementation without operator GO

## Goal

Make the review readable on the private dash.

![Video: Five-minute briefing](media/briefing.mp4)

## Decision checklist

- Confirm the boundary.
- Give design GO or request changes.

## Threat model

| Threat | Response |
|---|---|
| Raw HTML | Escape before rendering |
`)
	design.WriteString("```shell\nresearch-library-command-with-a-deliberately-long-unbroken-value-abcdefghijklmnopqrstuvwxyz-0123456789-abcdefghijklmnopqrstuvwxyz-0123456789\n```\n\n")
	design.WriteString(`
<script>window.RESEARCH_INJECTED = true</script>

[unsafe](javascript:window.RESEARCH_INJECTED=true)
`)
	for i := 4; i <= 33; i++ {
		design.WriteString("\n## Review section " + fmt.Sprintf("%02d", i) + "\n\nMeasured review content.\n")
	}
	now := time.Now()
	writeResearchFixture(t, root, "authorization-domains-design.md", design.String(), now)
	if err := os.MkdirAll(filepath.Join(root, "media"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "media", "briefing.mp4"), []byte("generic-video-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeResearchFixture(t, root, "notes/field-note.md", "# Field note\n\n## Finding\n\nAn ordinary research note.\n", time.Now().Add(-time.Hour))
	for i := 1; i <= 7; i++ {
		writeResearchFixture(t, root, fmt.Sprintf("decisions/design-%02d.md", i), fmt.Sprintf("# Design %02d\n\n**Status:** operator-review\n\n## Checklist\n\nReview this design.\n", i), now.Add(-time.Duration(i)*time.Minute))
		writeResearchFixture(t, root, fmt.Sprintf("notes/field-note-%02d.md", i), fmt.Sprintf("# Field note %02d\n\n## Finding\n\nReference material.\n", i), now.Add(-time.Duration(i+60)*time.Minute))
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
import json
import os
import sys
from playwright.sync_api import sync_playwright, expect

url, evidence_dir = sys.argv[1], sys.argv[2]
goals = []
for i in range(8):
    paper = "authorization-domains-design.md" if i == 0 else "decisions/design-%02d.md" % i
    goals.append({
        "id": "design-%02d" % (i + 1), "title": "Design %02d" % (i + 1),
        "owner": "example-desk", "conversation_agent": "example-desk",
        "status_display": "awaiting", "state": "awaiting",
        "brief": "## Recommendation\nReview this design.\n\n[Read paper](/research/%s)" % paper,
        "work_items": []
    })
def install_goals(page):
    page.route("**/api/goals", lambda route: route.fulfill(
        status=200, content_type="application/json",
        body=json.dumps({"found": True, "goals": goals, "counts": {"total": 8, "awaiting": 8}})))
with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        phone = browser.new_page(viewport={"width": 390, "height": 844})
        install_goals(phone)
        phone.goto(url + "/research", wait_until="domcontentloaded")
        expect(phone.locator("#research-decisions")).to_be_visible()
        decision = phone.locator("#research-decision-list .research-card")
        expect(decision).to_have_count(3)
        expect(phone.locator("#research-decision-count")).to_have_text("8 waiting")
        expect(phone.locator("#research-all")).to_be_hidden()
        phone.locator('[data-research-focus="all"]').click()
        expect(decision).to_have_count(3)
        expect(phone.locator("#research-list .research-card")).to_have_count(6)
        expect(phone.locator("#research-count")).to_have_text("16 documents")
        expect(phone.locator("#research-decision-more")).to_contain_text("5 remaining")
        expect(phone.locator("#research-library-more")).to_contain_text("10 remaining")
        initial_metrics = phone.evaluate("({height:document.documentElement.scrollHeight,width:document.documentElement.scrollWidth,clientWidth:document.documentElement.clientWidth})")
        if evidence_dir:
            phone.screenshot(path=os.path.join(evidence_dir, "research-library-initial-390.png"), full_page=True)
        phone.locator("#research-decision-more").click()
        expect(phone.locator("#research-decision-list .research-card")).to_have_count(6)
        phone.locator("#research-decision-more").click()
        phone.locator("#research-library-more").click()
        phone.locator("#research-library-more").click()
        expect(phone.locator("#research-decision-list .research-card")).to_have_count(8)
        expect(phone.locator("#research-list .research-card")).to_have_count(16)
        expect(phone.locator("#research-decision-more")).to_be_hidden()
        expect(phone.locator("#research-library-more")).to_be_hidden()
        phone.locator("#research-decision-list .research-card").filter(has_text="Design 01").click()
        expect(phone.locator("#research-document")).to_be_visible()
        expect(phone.locator("#research-title")).to_contain_text("Authorization Domains")
        expect(phone.locator("#research-decision-strip")).to_be_visible()
        expect(phone.locator("#research-toc")).to_be_visible()
        assert phone.locator("#research-toc").get_attribute("open") is None
        expect(phone.locator("#research-toc-count")).to_have_text("33 sections")
        closed_height = phone.locator("#research-toc").evaluate("node => node.getBoundingClientRect().height")
        assert closed_height <= 48, closed_height
        closed_page_height = phone.evaluate("document.documentElement.scrollHeight")
        expect(phone.locator(".research-table-wrap table")).to_be_visible()
        video = phone.locator(".research-video video")
        expect(video).to_be_visible()
        assert video.get_attribute("controls") is not None
        assert video.locator("source").get_attribute("src") == "/research-assets/media/briefing.mp4"
        full_screen = phone.locator("[data-research-video-fullscreen]")
        expect(full_screen).to_have_text("Full screen")
        full_metrics = full_screen.evaluate("node => ({width:node.getBoundingClientRect().width,height:node.getBoundingClientRect().height,right:node.getBoundingClientRect().right,viewport:document.documentElement.clientWidth})")
        assert full_metrics["width"] >= 44 and full_metrics["height"] >= 44 and full_metrics["right"] <= full_metrics["viewport"], full_metrics
        video.evaluate("node => { node.requestFullscreen = function () { window.RESEARCH_FULLSCREEN_REQUESTED = true; return Promise.resolve(); }; }")
        full_screen.click()
        assert phone.evaluate("window.RESEARCH_FULLSCREEN_REQUESTED === true")
        phone.evaluate("window.scrollTo(0, 0)")
        expect(phone.locator(".research-markdown script")).to_have_count(0)
        assert phone.evaluate("window.RESEARCH_INJECTED") is None
        assert "<script>" in phone.locator("#research-body").inner_text()
        assert phone.locator('#research-body a[href^="javascript:"]').count() == 0
        assert phone.evaluate("document.documentElement.scrollWidth === document.documentElement.clientWidth")
        table_metrics = phone.locator(".research-table-wrap").evaluate("node => ({scrollWidth:node.scrollWidth,clientWidth:node.clientWidth})")
        code_metrics = phone.locator(".research-markdown pre").evaluate("node => ({scrollWidth:node.scrollWidth,clientWidth:node.clientWidth})")
        assert table_metrics["scrollWidth"] <= table_metrics["clientWidth"]
        assert code_metrics["scrollWidth"] > code_metrics["clientWidth"]
        if evidence_dir:
            phone.screenshot(path=os.path.join(evidence_dir, "research-document-top-390.png"), full_page=True)
        phone.locator("#research-toc > summary").click()
        expect(phone.locator("#research-toc")).to_have_attribute("open", "")
        expect(phone.locator("#research-toc-list li")).to_have_count(33)
        open_metrics = phone.locator("#research-toc").evaluate("node => { const box=node.getBoundingClientRect(), summary=node.querySelector('summary').getBoundingClientRect(); return {top:box.top,bottom:box.bottom,height:box.height,summaryTop:summary.top,listClient:node.querySelector('ol').clientHeight,listScroll:node.querySelector('ol').scrollHeight} }")
        assert open_metrics["top"] >= 0 and open_metrics["bottom"] <= 844 and open_metrics["height"] > 48, open_metrics
        assert open_metrics["summaryTop"] >= open_metrics["top"], open_metrics
        assert open_metrics["listScroll"] > open_metrics["listClient"], open_metrics
        assert phone.evaluate("document.documentElement.scrollHeight") <= closed_page_height + 2
        assert phone.locator("body").evaluate("node => node.classList.contains('research-toc-open')")
        if evidence_dir:
            phone.screenshot(path=os.path.join(evidence_dir, "research-toc-expanded-390.png"), full_page=False)
        phone.keyboard.press("Escape")
        phone.wait_for_timeout(50)
        assert phone.locator("#research-toc").get_attribute("open") is None
        assert phone.locator("#research-toc > summary").evaluate("node => document.activeElement === node")
        assert phone.evaluate("window.scrollY") < 20
        phone.locator("#research-toc > summary").click()
        target_link = phone.locator("#research-toc-list a").nth(9)
        target_id = target_link.get_attribute("href")[1:]
        target_link.click()
        expect(phone.locator("#research-toc")).not_to_have_attribute("open", "")
        phone.wait_for_function("id => document.activeElement && document.activeElement.id === id", arg=target_id)
        phone.evaluate("window.scrollTo(0, 900)")
        phone.wait_for_timeout(50)
        sticky = phone.locator("#research-decision-strip").evaluate("node => ({position:getComputedStyle(node).position, top:node.getBoundingClientRect().top, bottom:node.getBoundingClientRect().bottom})")
        assert sticky["position"] == "sticky", sticky
        assert sticky["top"] >= 0 and sticky["bottom"] <= 844, sticky
        assert phone.evaluate("document.documentElement.scrollWidth === document.documentElement.clientWidth")
        if evidence_dir:
            phone.screenshot(path=os.path.join(evidence_dir, "research-document-scrolled-390.png"), full_page=False)
        print(json.dumps({
            "initial": initial_metrics,
            "collections": {"decision_initial": 3, "decision_total": 8, "library_initial": 6, "library_total": 16},
            "toc": {"closed_height": closed_height, "open": open_metrics, "sections": 33},
            "sticky_after_900": sticky,
            "table": table_metrics,
            "code": code_metrics
        }, sort_keys=True))
        phone.close()

        compact = browser.new_page(viewport={"width": 360, "height": 800})
        install_goals(compact)
        compact.goto(url + "/research/authorization-domains-design.md", wait_until="domcontentloaded")
        expect(compact.locator("#research-document")).to_be_visible()
        compact.locator("#research-toc > summary").click()
        compact_toc = compact.locator("#research-toc").evaluate("node => { const box=node.getBoundingClientRect(); return {top:box.top,bottom:box.bottom,height:box.height,listClient:node.querySelector('ol').clientHeight,listScroll:node.querySelector('ol').scrollHeight} }")
        assert compact_toc["top"] >= 0 and compact_toc["bottom"] <= 800 and compact_toc["listScroll"] > compact_toc["listClient"], compact_toc
        assert compact.evaluate("document.documentElement.scrollWidth === document.documentElement.clientWidth")
        compact.keyboard.press("Escape")
        assert compact.locator("#research-toc > summary").evaluate("node => document.activeElement === node")
        compact.close()

        collection_attempts = {"count": 0}
        unavailable = browser.new_page(viewport={"width": 390, "height": 844})
        install_goals(unavailable)
        def collection_route(route):
            collection_attempts["count"] += 1
            if collection_attempts["count"] == 1:
                route.fulfill(status=503, content_type="application/json", body='{"error":"temporarily unavailable"}')
            else:
                route.continue_()
        unavailable.route("**/api/research", collection_route)
        unavailable.goto(url + "/research", wait_until="domcontentloaded")
        expect(unavailable.locator("#research-status-title")).to_have_text("R&D library unavailable")
        expect(unavailable.locator("#research-index-retry")).to_be_visible()
        unavailable.locator("#research-index-retry").click()
        expect(unavailable.locator("#research-decisions")).to_be_visible()
        assert collection_attempts["count"] == 2
        unavailable.close()

        document_attempts = {"count": 0}
        document_error = browser.new_page(viewport={"width": 390, "height": 844})
        install_goals(document_error)
        def document_route(route):
            document_attempts["count"] += 1
            if document_attempts["count"] == 1:
                route.fulfill(status=503, content_type="application/json", body='{"error":"temporarily unavailable"}')
            else:
                route.continue_()
        document_error.route("**/api/research/authorization-domains-design.md", document_route)
        document_error.goto(url + "/research/authorization-domains-design.md", wait_until="domcontentloaded")
        expect(document_error.locator("#research-reader-state-title")).to_have_text("Document unavailable")
        expect(document_error.locator("#research-document-retry")).to_be_visible()
        document_error.locator("#research-document-retry").click()
        expect(document_error.locator("#research-document")).to_be_visible()
        assert document_attempts["count"] == 2
        document_error.close()

        card_attempts = {"count": 0}
        card_error = browser.new_page(viewport={"width": 390, "height": 844})
        install_goals(card_error)
        def card_route(route):
            card_attempts["count"] += 1
            if card_attempts["count"] == 1:
                route.fulfill(status=503, content_type="application/json", body='{"error":"temporarily unavailable"}')
            else:
                route.continue_()
        card_error.route("**/api/research/authorization-domains-design.md", card_route)
        card_error.goto(url + "/research", wait_until="domcontentloaded")
        card_error.locator("#research-decision-list .research-card").filter(has_text="Design 01").click()
        expect(card_error.locator("#research-reader-state-title")).to_have_text("Document unavailable")
        assert card_error.url.endswith("/research"), card_error.url
        card_error.locator("#research-document-retry").click()
        expect(card_error.locator("#research-document")).to_be_visible()
        expect(card_error).to_have_url(url + "/research/authorization-domains-design.md")
        assert card_attempts["count"] == 2
        card_error.go_back()
        expect(card_error).to_have_url(url + "/research")
        expect(card_error.locator("#research-library")).to_be_visible()
        expect(card_error.locator("#research-reader")).to_be_hidden()
        card_error.close()

        desktop = browser.new_page(viewport={"width": 1440, "height": 900})
        install_goals(desktop)
        desktop.goto(url + "/research/notes/field-note.md", wait_until="domcontentloaded")
        expect(desktop.locator("#research-title")).to_have_text("Field note")
        expect(desktop.locator("#research-decision-strip")).to_be_hidden()
        expect(desktop.locator("#research-library")).to_be_visible()
        expect(desktop.locator("#research-reader")).to_be_visible()
        assert desktop.evaluate("document.documentElement.scrollWidth === document.documentElement.clientWidth")
        if evidence_dir:
            desktop.screenshot(path=os.path.join(evidence_dir, "research-library-1440.png"), full_page=True)
        desktop.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, evidenceDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rendered Research library regression: %v\n%s", err, out)
	}
	t.Logf("rendered Research metrics: %s", strings.TrimSpace(string(out)))
	if evidenceDir != "" {
		for _, name := range []string{"research-library-initial-390.png", "research-document-top-390.png", "research-toc-expanded-390.png", "research-document-scrolled-390.png", "research-library-1440.png"} {
			path := filepath.Join(evidenceDir, name)
			if info, err := os.Stat(path); err != nil || info.Size() == 0 {
				t.Fatalf("rendered Research evidence missing at %q: %v", path, err)
			}
			t.Logf("generic rendered evidence: %s", path)
		}
	}
}

func TestResearchShowpieceRendered873(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Research showpiece regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	root := t.TempDir()
	srv.cfg.ResearchPath = root
	writeResearchFixture(t, root, "buzz/SOURCE.md", "# Buzz market research\n\nA fully evidenced pilot source.\n", time.Now())
	writeResearchFixture(t, root, "draft/SOURCE.md", "# Draft evidence\n\nSource awaiting its presentation.\n", time.Now().Add(time.Hour))
	navigation, err := os.ReadFile(filepath.Join("..", "..", "docs", "examples", "research-showpiece-navigation.js"))
	if err != nil {
		t.Fatalf("read showpiece navigation reference: %v", err)
	}
	for name, body := range map[string]string{
		"buzz/presentation/index.html":                   `<!doctype html><html data-showpiece-title="Generic showpiece"><head><meta name="viewport" content="width=device-width"><link rel="stylesheet" href="assets/showpiece.css"></head><body><main data-showpiece-deck><section class="slide" id="slide-1" data-label="Section one"><p>HTML5 showpiece</p><h1>Market event flow</h1><img src="assets/event-flow-poster.png" alt="Generic event flow"><video controls poster="assets/event-flow-poster.png" src="media/event-flow.mp4"></video><a href="../SOURCE.md">Read source</a></section><section class="slide tall" id="slide-2" data-label="Section two"><h2>Two</h2></section><section class="slide" id="slide-3" data-label="Section three"><h2>Three</h2></section><section class="slide tall" id="slide-4" data-label="Section four"><h2>Four</h2></section><section class="slide" id="slide-5" data-label="Section five"><h2>Five</h2></section><section class="slide tall" id="slide-6" data-label="Section six"><h2>Six</h2></section><section class="slide" id="slide-7" data-label="Section seven"><h2>Seven</h2></section><section class="slide tall" id="slide-8" data-label="Section eight"><h2>Eight</h2></section><section class="slide" id="slide-9" data-label="Section nine"><h2>Nine</h2></section></main><div><span data-current></span> / <span data-total></span><span data-progress></span><button data-prev aria-label="Previous slide">Previous</button><button data-next aria-label="Next slide">Next</button></div><script src="assets/showpiece.js"></script></body></html>`,
		"buzz/presentation/assets/showpiece.css":         `html,body{margin:0;background:#08111f;color:#f3f7fb;font:16px system-ui;overflow:hidden}main{height:620px;overflow-y:auto;scroll-snap-type:none}.slide{box-sizing:border-box;min-height:620px;padding:24px}.slide.tall{min-height:940px}img,video{display:block;max-width:100%;margin:16px 0}`,
		"buzz/presentation/assets/showpiece.js":          string(navigation),
		"buzz/presentation/assets/event-flow-poster.png": "generic-poster",
		"buzz/presentation/media/event-flow.mp4":         "generic-video",
	} {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(httpServer.Close)

	script := `
import sys
from playwright.sync_api import sync_playwright, expect

url = sys.argv[1]
with sync_playwright() as p:
    browser = p.chromium.launch()
    page = browser.new_page(viewport={"width": 390, "height": 844}, reduced_motion="reduce")
    page.goto(url + "/research?focus=library", wait_until="domcontentloaded")
    cards = page.locator("#research-list .research-card")
    expect(cards).to_have_count(2)
    expect(cards.nth(0)).to_contain_text("HTML5 showpiece")
    expect(cards.nth(1)).to_contain_text("Source only · not ready")
    cards.nth(0).click()
    frame = page.locator("#research-presentation")
    expect(frame).to_be_visible()
    assert frame.get_attribute("sandbox") == "allow-scripts"
    expect(page.locator("#research-body")).to_be_hidden()
    expect(page.frame_locator("#research-presentation").locator("h1")).to_have_text("Market event flow")
    expect(page.frame_locator("#research-presentation").locator("img")).to_be_visible()
    expect(page.frame_locator("#research-presentation").locator("video")).to_be_visible()
    expect(page.frame_locator("#research-presentation").locator('a[href="../SOURCE.md"]')).to_be_visible()
    frame = page.frame_locator("#research-presentation")
    expect(frame.locator("body")).to_have_attribute("data-showpiece-navigation-ready", "true")
    current = frame.locator("[data-current]")
    previous = frame.locator("[data-prev]")
    next_button = frame.locator("[data-next]")
    expect(current).to_have_text("01")
    for section in range(2, 10):
        next_button.evaluate("node => node.click()")
        expect(current).to_have_text(f"{section:02d}")
        expect(frame.locator(f"#slide-{section}")).to_be_visible()
        assert frame.locator("main").evaluate(
            f"node => node.scrollTop === node.querySelector('#slide-{section}').offsetTop"
        )
        assert frame.locator("html").evaluate(
            "node => document.title",
        ).startswith(f"Section {['zero','one','two','three','four','five','six','seven','eight','nine'][section]}")
    for section in range(8, 0, -1):
        previous.evaluate("node => node.click()")
        expect(current).to_have_text(f"{section:02d}")
    frame.locator("main").evaluate(
        "node => node.scrollTo({top: node.querySelector('#slide-5').offsetTop, behavior: 'auto'})"
    )
    expect(current).to_have_text("05")
    assert frame.locator("main").evaluate(
        "node => getComputedStyle(node).scrollSnapType"
    ) == "none"
    expect(page.locator("#research-document-comment")).to_be_visible()
    assert page.evaluate("document.documentElement.scrollWidth === document.documentElement.clientWidth")
    page.locator("#research-back").click()
    cards = page.locator("#research-list .research-card")
    cards.nth(1).click()
    expect(page.locator("#research-body")).to_be_visible()
    expect(page.locator("#research-presentation")).to_be_hidden()
    browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Research showpiece regression: %v\n%s", err, out)
	}
}
