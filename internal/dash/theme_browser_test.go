package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestThemeAssets834(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	srv.cfg.ResearchPath = t.TempDir()
	for _, path := range []string{"/", "/research", "/parade"} {
		page := doGet(t, srv, path)
		if page.Code != 200 {
			t.Fatalf("GET %s = %d", path, page.Code)
		}
		body := page.Body.String()
		if !strings.Contains(body, `/static/theme.js`) || !strings.Contains(body, `data-theme-toggle`) {
			t.Errorf("GET %s lacks shared theme bootstrap/control", path)
		}
		if strings.Index(body, `/static/theme.js`) > strings.Index(body, `/static/dash.css`) {
			t.Errorf("GET %s loads theme after stylesheet (would flash light)", path)
		}
	}

	theme := doGet(t, srv, "/static/theme.js")
	if theme.Code != 200 {
		t.Fatalf("GET /static/theme.js = %d", theme.Code)
	}
	for _, marker := range []string{"prefers-color-scheme: dark", "flotilla-theme-v1", "localStorage", "data-theme-toggle"} {
		if !strings.Contains(theme.Body.String(), marker) {
			t.Errorf("theme bootstrap lacks %q", marker)
		}
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	for _, marker := range []string{`:root[data-theme="dark"]`, `--ground:`, `.theme-toggle`} {
		if !strings.Contains(css, marker) {
			t.Errorf("theme CSS lacks %q", marker)
		}
	}
}

// TestThemeRendered834 proves the theme at rendered boundaries: first-paint
// system preference, an explicit durable override, readable semantic colors,
// shared standalone pages, and phone-width chrome containment.
func TestThemeRendered834(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered theme regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, dir := newTestServer(t, singleFleetRoster, time.Now())
	srv.cfg.ResearchPath = t.TempDir()
	srv.cfg.ParadesPath = filepath.Join(dir, "parades")
	paradeDir := filepath.Join(srv.cfg.ParadesPath, "2026-07-23")
	if err := os.MkdirAll(paradeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paradeDir, "slides.md"), []byte("Generic theme fixture\n\nNo private content."), 0o600); err != nil {
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
import json
import os
import sys
from playwright.sync_api import sync_playwright, expect

url, evidence_dir = sys.argv[1], sys.argv[2]

def rgb(value):
    return [int(piece) for piece in value.removeprefix("rgb(").removesuffix(")").split(",")]

def luminance(value):
    channels = []
    for channel in rgb(value):
        channel = channel / 255
        channels.append(channel / 12.92 if channel <= .04045 else ((channel + .055) / 1.055) ** 2.4)
    return .2126 * channels[0] + .7152 * channels[1] + .0722 * channels[2]

def contrast(a, b):
    light, dark = sorted([luminance(a), luminance(b)], reverse=True)
    return (light + .05) / (dark + .05)

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        desktop = browser.new_context(viewport={"width": 1440, "height": 900}, color_scheme="dark")
        page = desktop.new_page()
        page.add_init_script("window.EventSource = undefined")
        page.goto(url, wait_until="domcontentloaded")
        expect(page.locator("html")).to_have_attribute("data-theme", "dark")
        expect(page.locator(".theme-toggle")).to_have_attribute("aria-pressed", "true")
        expect(page.locator(".theme-toggle [data-theme-label]")).to_have_text("Dark → Light")
        expect(page.locator(".theme-toggle [data-theme-label]")).to_have_attribute("data-theme-compact", "D→L")
        desktop_toggle = page.locator(".theme-toggle").bounding_box()
        assert desktop_toggle and desktop_toggle["y"] >= 0 and desktop_toggle["height"] >= 36, desktop_toggle
        colors = page.evaluate("""() => ({
            ground: getComputedStyle(document.body).backgroundColor,
            ink: getComputedStyle(document.querySelector('.brand-name')).color,
            surface: getComputedStyle(document.querySelector('.bar')).backgroundColor
        })""")
        assert luminance(colors["ground"]) < .02, colors
        assert contrast(colors["ink"], colors["ground"]) >= 7, colors

        for tab in ["#tab-goals", "#tab-issues"]:
            page.locator(tab).click()
            expect(page.locator("html")).to_have_attribute("data-theme", "dark")
        if evidence_dir:
            page.locator("#tab-conversations").click()
            page.screenshot(path=os.path.join(evidence_dir, "theme-dark-dashboard-1440.png"), full_page=False)

        page.locator(".theme-toggle").click()
        expect(page.locator("html")).to_have_attribute("data-theme", "light")
        assert page.evaluate("localStorage.getItem('flotilla-theme-v1')") == "light"
        page.reload(wait_until="domcontentloaded")
        expect(page.locator("html")).to_have_attribute("data-theme", "light")
        desktop.close()

        phone = browser.new_context(viewport={"width": 390, "height": 844}, color_scheme="light")
        mobile = phone.new_page()
        mobile.add_init_script("window.EventSource = undefined")
        mobile.goto(url, wait_until="domcontentloaded")
        mobile.evaluate("localStorage.setItem('flotilla-theme-v1', 'dark')")
        phone_metrics = []
        for width, height in [(390, 844), (360, 800)]:
            mobile.set_viewport_size({"width": width, "height": height})
            for path in ["/", "/research", "/parade"]:
                mobile.goto(url + path, wait_until="domcontentloaded")
                expect(mobile.locator("html")).to_have_attribute("data-theme", "dark")
                if path == "/parade" and mobile.locator("#pd-deck").is_visible():
                    mobile.locator("#pd-close").click()
                    expect(mobile.locator("#pd-list-view")).to_be_visible()
                toggle = mobile.locator(".theme-toggle:visible").first
                expect(toggle).to_be_visible()
                expect(toggle).to_have_attribute("aria-pressed", "true")
                expect(toggle).to_have_attribute("aria-label", "Dark theme active. Switch to light theme")
                label = toggle.locator("[data-theme-label]")
                expect(label).to_have_text("Dark → Light")
                expect(label).to_have_attribute("data-theme-compact", "D→L")
                compact = label.evaluate("node => getComputedStyle(node, '::after').content")
                assert "D→L" in compact, (width, path, compact)
                box = toggle.bounding_box()
                assert box and box["height"] >= 44 and box["width"] >= 44, (width, path, box)
                assert box["y"] >= 0 and box["y"] + box["height"] <= height, (width, path, box)
                route_metrics = {"viewport": "%dx%d" % (width, height), "path": path, "toggle": box}
                if path in ["/research", "/parade"]:
                    expect(mobile.locator(".pd-topback")).to_be_visible()
                    expect(mobile.locator(".brand-name")).to_have_text("flotilla")
                    brand = mobile.locator(".bar .brand")
                    nav = mobile.locator(".bar .parade-topnav")
                    brand_box = brand.bounding_box()
                    nav_box = nav.bounding_box()
                    brand_metrics = brand.evaluate("node => ({scroll: node.scrollWidth, client: node.clientWidth})")
                    name_box = mobile.locator(".bar .brand-name").bounding_box()
                    assert brand_box and nav_box and name_box, (width, path)
                    assert brand_metrics["scroll"] <= brand_metrics["client"], (width, path, brand_metrics)
                    assert name_box["x"] >= 0 and name_box["x"] + name_box["width"] <= width, (width, path, name_box)
                    assert brand_box["y"] + brand_box["height"] <= nav_box["y"], (width, path, brand_box, nav_box)
                    route_metrics["brand"] = brand_box
                    route_metrics["nav"] = nav_box
                metrics = mobile.evaluate("({scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth})")
                assert metrics["scroll"] == metrics["client"], (width, path, metrics)
                if path == "/parade":
                    mobile.locator(".pd-listcard").click()
                    deck_toggle = mobile.locator("#pd-deck .theme-toggle")
                    expect(deck_toggle).to_be_visible()
                    expect(deck_toggle).to_have_attribute("aria-label", "Dark theme active. Switch to light theme")
                    expect(deck_toggle.locator("[data-theme-label]")).to_have_text("Dark → Light")
                    deck_box = deck_toggle.bounding_box()
                    assert deck_box and deck_box["width"] >= 44 and deck_box["height"] >= 44, (width, deck_box)
                    assert deck_box["x"] >= 0 and deck_box["x"] + deck_box["width"] <= width, (width, deck_box)
                    assert deck_box["y"] >= 0 and deck_box["y"] + deck_box["height"] <= height, (width, deck_box)
                phone_metrics.append(route_metrics)
                if evidence_dir:
                    name = "dashboard" if path == "/" else path[1:]
                    mobile.screenshot(path=os.path.join(evidence_dir, "theme-dark-%s-%d.png" % (name, width)), full_page=False)

        mobile.locator(".theme-toggle:visible").first.click()
        expect(mobile.locator("html")).to_have_attribute("data-theme", "light")
        expect(mobile.locator(".theme-toggle:visible").first).to_have_attribute("aria-pressed", "false")
        expect(mobile.locator(".theme-toggle:visible").first.locator("[data-theme-label]")).to_have_text("Light → Dark")
        mobile.goto(url, wait_until="domcontentloaded")
        expect(mobile.locator("html")).to_have_attribute("data-theme", "light")
        print("THEME_PHONE_METRICS=" + json.dumps(phone_metrics, sort_keys=True))
        phone.close()
    finally:
        browser.close()
`

	cmd := exec.Command(python, "-c", script, httpServer.URL, evidenceDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rendered theme regression: %v\n%s", err, out)
	}
	if len(out) > 0 {
		t.Logf("rendered theme metrics:\n%s", out)
	}
}
