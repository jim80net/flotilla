package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readSiteFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func TestLandingHeroShowsProductProof(t *testing.T) {
	page := readSiteFile(t, "index.html")
	for _, asset := range []string{
		"assets/dashboard-product-proof-mobile-780.webp",
		"assets/dashboard-product-proof-720.webp",
		"assets/dashboard-product-proof-1440.webp",
	} {
		if _, err := os.Stat(asset); err != nil {
			t.Errorf("product-proof asset %s is not readable: %v", asset, err)
		}
	}
	for _, want := range []string{
		`class="product-proof product-proof--hero"`,
		`dashboard-product-proof-720.webp 720w`,
		`dashboard-product-proof-1440.webp 1440w`,
		`dashboard-product-proof-mobile-780.webp 780w`,
		`alt="Flotilla dashboard showing a fleet map, active coding desks, coordination history, and a scoped work queue"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("landing page missing product-proof marker %q", want)
		}
	}
	for _, stale := range []string{"AI-GENERATED ATMOSPHERE", "atmosphere", "generated-asset"} {
		if strings.Contains(page, stale) {
			t.Errorf("landing page retains rejected decorative-media marker %q", stale)
		}
	}
	if strings.Contains(page, `.png`) {
		t.Error("landing page references a PNG source master as a public payload")
	}
}

func TestLandingRemovesTemporaryBannerAndDeadStatusWidget(t *testing.T) {
	page := readSiteFile(t, "index.html")
	css := readSiteFile(t, "styles.css")
	js := readSiteFile(t, "app.js")
	for _, stale := range []string{
		`class="id-banner"`,
		"32 days, 239 merged pull requests, one fleet",
		"Independence Day 2026 · open source · v0",
		`id="fleet-status"`,
	} {
		if strings.Contains(page, stale) {
			t.Errorf("landing page retains removed surface %q", stale)
		}
	}
	for name, content := range map[string]string{"styles.css": css, "app.js": js} {
		if strings.Contains(content, "fleet-status") || strings.Contains(content, "status.json") {
			t.Errorf("%s retains dead fleet-status widget code", name)
		}
	}
}

func TestLandingShippedClaimsAreCurrent(t *testing.T) {
	page := readSiteFile(t, "index.html")
	for _, want := range []string{
		"Claude Code, Codex, Grok, OpenCode, Pi, and aider",
		"When auto-switch is enabled, eligible non-approval-sensitive sessions currently running Claude Code",
		"Five dashboard destinations:",
		"Conversations, Goals, Issues, Parade, and combined R&amp;D",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("landing page missing current shipped claim %q", want)
		}
	}
	for _, stale := range []string{"Cursor", "on the roadmap", "in supervised trial", "in trial · on the roadmap", "eligible non-approval-sensitive desks can switch automatically"} {
		if strings.Contains(page, stale) {
			t.Errorf("landing page retains stale claim %q", stale)
		}
	}
}

func TestLandingDocsHierarchyAndMemexBoundary(t *testing.T) {
	page := readSiteFile(t, "index.html")
	for _, path := range []string{"docs/index.html", "docs/understand.html", "docs/quickstart.html", "docs/commands.html", "docs/architecture.html"} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("public docs page %s is not readable: %v", path, err)
		}
	}
	for _, want := range []string{
		"Two parts do the whole job.",
		`class="cards two"`,
		`href="./docs/understand.html"`,
		`href="./docs/quickstart.html"`,
		`href="./docs/commands.html"`,
		`href="./docs/architecture.html"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("landing page missing docs hierarchy marker %q", want)
		}
	}
	for _, stale := range []string{"Three ideas do the whole job.", "memex-core", ">memex<"} {
		if strings.Contains(strings.ToLower(page), strings.ToLower(stale)) {
			t.Errorf("landing page retains rejected component hierarchy %q", stale)
		}
	}
	heroAt := strings.Index(page, `class="product-proof product-proof--hero"`)
	docsAt := strings.Index(page, `id="docs"`)
	problemAt := strings.Index(page, `id="feel"`)
	if heroAt < 0 || docsAt < heroAt || problemAt < docsAt {
		t.Errorf("landing information order = hero:%d docs:%d problem:%d, want hero product proof then docs path then narrative", heroAt, docsAt, problemAt)
	}
	pages, err := filepath.Glob("docs/*.html")
	if err != nil {
		t.Fatalf("glob public docs: %v", err)
	}
	for _, path := range pages {
		if filepath.Base(path) == "architecture.html" {
			continue
		}
		if strings.Contains(strings.ToLower(readSiteFile(t, path)), "memex") {
			t.Errorf("supporting component promoted outside architecture page: %s", path)
		}
	}
	architecture := readSiteFile(t, "docs/architecture.html")
	for _, want := range []string{"Memex", "External and optional", "not required for Flotilla to deliver work"} {
		if !strings.Contains(architecture, want) {
			t.Errorf("architecture page missing component boundary %q", want)
		}
	}
}

func TestLandingRejectsDecorativeAtmosphereAssets(t *testing.T) {
	page := readSiteFile(t, "index.html")
	css := readSiteFile(t, "styles.css")
	for _, stale := range []string{"AI-GENERATED ATMOSPHERE", "atmosphere", "generated-asset"} {
		if strings.Contains(page, stale) || strings.Contains(css, stale) {
			t.Errorf("landing retains rejected decorative-media concept %q", stale)
		}
	}
	for _, asset := range []string{
		"assets/hero-atmosphere-800.webp",
		"assets/hero-atmosphere-1600.webp",
		"assets/tools-atmosphere-800.webp",
		"assets/tools-atmosphere-1600.webp",
	} {
		if _, err := os.Stat(asset); !os.IsNotExist(err) {
			t.Errorf("rejected decorative asset %s still exists", asset)
		}
	}
}

func TestParadeHasVisibleMobilePagerWithoutFallbackGlyph(t *testing.T) {
	page := readSiteFile(t, "parade/index.html")
	for _, want := range []string{
		`aria-label", "Parade slide navigation`,
		`← Previous`,
		`Next →`,
		`min-height:44px`,
		`scrollIntoView`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("parade page missing pager marker %q", want)
		}
	}
	if strings.Contains(page, "🎆") {
		t.Error("parade lead still depends on the platform fireworks glyph")
	}
	if got := strings.Count(page, `<section id="s`); got != 15 {
		t.Errorf("parade slide count = %d, want 15", got)
	}
}
