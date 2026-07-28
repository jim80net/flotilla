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

func TestLandingParadePromotionIsEvergreen(t *testing.T) {
	page := readSiteFile(t, "index.html")
	for _, stale := range []string{
		"on America's 250th birthday",
		"Watch the Independence Day parade →",
	} {
		if strings.Contains(page, stale) {
			t.Errorf("landing page still contains stale hero copy %q", stale)
		}
	}
	for _, want := range []string{
		"See the inaugural parade",
		"32 days, 239 merged pull requests, one fleet",
		"Watch the story →",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("landing page missing evergreen parade copy %q", want)
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
