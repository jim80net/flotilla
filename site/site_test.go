package site

import (
	"os"
	"regexp"
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
		`sizes="(max-width: 720px) calc(100vw - 48px), 46vw"`,
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

func TestLandingShippedClaimsAreCurrent(t *testing.T) {
	page := readSiteFile(t, "index.html")
	for _, want := range []string{
		"Claude Code, Codex, Grok, OpenCode, Pi, and aider",
		"When auto-switch is enabled, eligible non-approval-sensitive desks currently running Claude Code",
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

func TestLandingToolsDesktopUsesTwoColumnRow(t *testing.T) {
	css := readSiteFile(t, "styles.css")
	media := regexp.MustCompile(`@media\s*\(\s*min-width\s*:\s*721px\s*\)\s*\{`).FindStringIndex(css)
	if media == nil {
		t.Fatal("landing CSS missing 721px desktop media query")
	}
	desktop := css[media[1]:]
	if next := strings.Index(desktop, "@media"); next >= 0 {
		desktop = desktop[:next]
	}
	contracts := map[string][]string{
		`#yours\s+\.band-head`: {
			`grid-column\s*:\s*1\s*;`,
			`margin-bottom\s*:\s*0\s*;`,
		},
		`#yours\s+\.generated-asset--tools`: {
			`grid-column\s*:\s*2\s*;`,
			`width\s*:\s*100%\s*;`,
			`margin\s*:\s*0\s+0\s+2\.6rem\s*;`,
		},
		`#yours\s+\.diff-strip`: {
			`grid-column\s*:\s*1\s*/\s*-1\s*;`,
		},
	}
	for selector, properties := range contracts {
		match := regexp.MustCompile(`(?s)` + selector + `\s*\{([^}]*)\}`).FindStringSubmatch(desktop)
		if match == nil {
			t.Errorf("landing CSS missing tools-row selector %q", selector)
			continue
		}
		for _, property := range properties {
			if !regexp.MustCompile(property).MatchString(match[1]) {
				t.Errorf("landing CSS selector %q missing property /%s/", selector, property)
			}
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
