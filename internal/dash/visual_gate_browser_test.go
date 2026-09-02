package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestSevenCVisualGateRendered is one generic end-to-end fixture for the four
// W39 seams: embedded showpiece execution, dark selected-card contrast,
// presenter continuity/missing portrait fallback, and annotation modal focus.
func TestSevenCVisualGateRendered(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Seven-C visual gate")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, dir := newTestServer(t, singleFleetRoster, time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC))
	research := filepath.Join(dir, "research")
	srv.cfg.ResearchPath = research
	presentation := filepath.Join(research, "generic-decision", "presentation")
	if err := os.MkdirAll(presentation, 0o700); err != nil {
		t.Fatal(err)
	}
	writeResearchFixture(t, research, "generic-decision/SOURCE.md", `<!-- flotilla-publication
classification: decision
reader-action: Review the generic shape.
support: material
-->
# Generic shape decision

**Status:** operator-review

## Recommendation

Approve the reversible shape.
`, time.Now())
	showpiece := `<!doctype html><html><body><main id="showpiece"></main><script>
localStorage.setItem("embedded-showpiece-ready", "yes");
document.getElementById("showpiece").textContent = "Embedded showpiece is ready";
</script></body></html>`
	if err := os.WriteFile(filepath.Join(presentation, "index.html"), []byte(showpiece), 0o600); err != nil {
		t.Fatal(err)
	}

	paradeDir := filepath.Join(dir, "parades", "2026-08-09")
	if err := os.MkdirAll(filepath.Join(paradeDir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paradeDir, "slides.md"), []byte("Chief of Staff · Opening claim\n\nFirst slide.\n\n---\n\nAdjacent claim\n\nSecond slide."), 0o600); err != nil {
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
import json, os, sys
from playwright.sync_api import sync_playwright, expect

url, evidence_dir = sys.argv[1], sys.argv[2]

def luminance(rgb):
    parts = [int(x) / 255 for x in rgb.removeprefix("rgb(").removesuffix(")").split(",")]
    vals = [x / 12.92 if x <= .04045 else ((x + .055) / 1.055) ** 2.4 for x in parts]
    return .2126 * vals[0] + .7152 * vals[1] + .0722 * vals[2]
def contrast(a, b):
    hi, lo = sorted([luminance(a), luminance(b)], reverse=True)
    return (hi + .05) / (lo + .05)

status = {"xo":"fleet-lead", "freshness":{"state":"fresh","message":"fresh"}, "agents":[
  {"name":"fleet-lead","state":"idle","loop_posture":"available","surface":"codex","queue_state":"empty"},
  {"name":"alpha-desk","state":"idle","loop_posture":"available","surface":"codex","queue_state":"empty"}]}
topology = {"roster_hierarchy":True,"root_seat_id":"0102030405060708","seats":[
  {"seat_id":"0102030405060708","name":"fleet-lead","coordinator":True},
  {"seat_id":"1112131415161718","name":"alpha-desk","parent":"0102030405060708"}]}

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        phone = browser.new_page(viewport={"width":390,"height":844})
        phone.add_init_script("window.EventSource = undefined")
        phone.goto(url + "/research/generic-decision/SOURCE.md", wait_until="domcontentloaded")
        frame = phone.frame_locator("#research-presentation")
        expect(frame.locator("#showpiece")).to_have_text("Embedded showpiece is ready")
        phone.locator("#research-presentation").scroll_into_view_if_needed()
        if evidence_dir: phone.screenshot(path=os.path.join(evidence_dir,"visual-gate-showpiece-390.png"), full_page=False)
        phone.locator("#research-document-comment").scroll_into_view_if_needed()
        phone.locator("#research-document-comment").click()
        backdrop = phone.locator("#research-annotation-backdrop")
        panel = phone.locator("#research-annotation-panel")
        expect(backdrop).to_be_visible(); expect(panel).to_be_visible()
        assert panel.evaluate("node => node.contains(document.activeElement)")
        assert backdrop.evaluate("node => getComputedStyle(node).backgroundColor") != "rgba(0, 0, 0, 0)"
        if evidence_dir: phone.screenshot(path=os.path.join(evidence_dir,"visual-gate-research-390.png"), full_page=False)
        phone.keyboard.press("Escape")
        expect(backdrop).to_be_hidden()

        phone.goto(url + "/parade", wait_until="domcontentloaded")
        expect(phone.locator(".pd-presenter-name")).to_have_text("Chief of Staff")
        expect(phone.locator(".pd-presenter-fallback")).to_have_text("CS")
        phone.locator("#pd-next").click()
        expect(phone.locator(".pd-presenter-name")).to_have_text("Chief of Staff")
        expect(phone.locator(".pd-presenter-fallback")).to_have_text("CS")
        if evidence_dir: phone.screenshot(path=os.path.join(evidence_dir,"visual-gate-parade-390.png"), full_page=False)

        desktop = browser.new_page(viewport={"width":1440,"height":900}, color_scheme="dark")
        desktop.add_init_script("window.EventSource = undefined")
        desktop.route("**/api/status", lambda route: route.fulfill(status=200, content_type="application/json", body=json.dumps(status)))
        desktop.route("**/api/topology", lambda route: route.fulfill(status=200, content_type="application/json", body=json.dumps(topology)))
        desktop.goto(url, wait_until="domcontentloaded")
        expect(desktop.locator("html")).to_have_attribute("data-theme", "dark")
        selected = desktop.locator('[data-desk="alpha-desk"]')
        selected.click(); assert "selected" in (selected.get_attribute("class") or "")
        colors = selected.evaluate("node => ({fg:getComputedStyle(node.querySelector('.conv-item-name')).color,bg:getComputedStyle(node).backgroundColor})")
        assert contrast(colors["fg"], colors["bg"]) >= 4.5, colors
        card = desktop.locator("#conv-desk-card .desk")
        expect(card).to_be_visible()
        card_colors = card.evaluate("node => ({fg:getComputedStyle(node.querySelector('.desk-name')).color,bg:getComputedStyle(node).backgroundColor})")
        assert contrast(card_colors["fg"], card_colors["bg"]) >= 4.5, card_colors
        if evidence_dir: desktop.screenshot(path=os.path.join(evidence_dir,"visual-gate-dashboard-1440.png"), full_page=False)

        desktop.goto(url + "/research/generic-decision/SOURCE.md", wait_until="domcontentloaded")
        expect(desktop.frame_locator("#research-presentation").locator("#showpiece")).to_have_text("Embedded showpiece is ready")
        if evidence_dir: desktop.screenshot(path=os.path.join(evidence_dir,"visual-gate-showpiece-1440.png"), full_page=False)
        desktop.locator("#research-document-comment").click()
        expect(desktop.locator("#research-annotation-backdrop")).to_be_visible()
        if evidence_dir: desktop.screenshot(path=os.path.join(evidence_dir,"visual-gate-research-1440.png"), full_page=False)
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, evidenceDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Seven-C visual gate: %v\n%s", err, out)
	}
	if evidenceDir != "" {
		for _, name := range []string{"visual-gate-showpiece-390.png", "visual-gate-research-390.png", "visual-gate-parade-390.png", "visual-gate-dashboard-1440.png", "visual-gate-showpiece-1440.png", "visual-gate-research-1440.png"} {
			if info, err := os.Stat(filepath.Join(evidenceDir, name)); err != nil || info.Size() == 0 {
				t.Fatalf("rendered evidence %s: %v", name, err)
			}
		}
	}
}
