package site

import (
	"os"
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

func TestLandingGeneratedAtmosphereContract(t *testing.T) {
	page := readSiteFile(t, "index.html")
	for _, want := range []string{
		`class="generated-asset generated-asset--hero"`,
		`class="generated-asset generated-asset--tools"`,
		`hero-atmosphere-800.webp 800w`,
		`hero-atmosphere-1600.webp 1600w`,
		`tools-atmosphere-800.webp 800w`,
		`tools-atmosphere-1600.webp 1600w`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("landing page missing generated atmosphere marker %q", want)
		}
	}
	if got := strings.Count(page, `class="generated-asset__disclosure"`); got != 2 {
		t.Errorf("generated disclosure count = %d, want 2", got)
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
