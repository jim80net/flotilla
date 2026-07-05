package dash

import "testing"

func TestParseQueueItemDisplayExplicitDelimiter(t *testing.T) {
	raw := "- [in-flight] Watch scheduler deploy :: Ready to merge tonight; verify at day boundary."
	item := ParseQueueItemDisplay(raw)
	if item.Status != "in-flight" {
		t.Fatalf("status = %q", item.Status)
	}
	if item.Title != "Watch scheduler deploy" {
		t.Fatalf("title = %q", item.Title)
	}
	if item.Summary == "" {
		t.Fatal("want summary")
	}
	if !TitleIsOperatorFacing(item.Title) {
		t.Fatal("title must be operator-facing")
	}
}

func TestParseQueueItemDisplayDerivesTitleBeforeJargon(t *testing.T) {
	raw := "- [in-flight] Gate the watch scheduler PR #414 SHA c1d47a5837106354bf2654cccc7d03b473ffd1de for merge"
	item := ParseQueueItemDisplay(raw)
	if !TitleIsOperatorFacing(item.Title) {
		t.Fatalf("title not operator-facing: %q", item.Title)
	}
	if item.Internal == "" {
		t.Fatal("want internal body")
	}
}

func TestParseQueueItemDisplayScope(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
	}{
		{"at-desk", "- [in-flight] Ship dash :: Summary. @flotilla-dash", "flotilla-dash"},
		{"arrow-desk", "- [in-flight] Deploy canary → flotilla-dash", "flotilla-dash"}, // scopeArrowAgent path
		{"arrow-desk-ascii", "- [next] Hand off -> macro-desk", "macro-desk"},          // -> variant
		{"unscoped-coordinator", "- [next] Fleet parade prep", ""},                     // no scope token
		{"at-operator-skipped", "- [blocked] @operator please review", ""},             // @operator is coordinator-level
		{"arrow-operator-skipped", "- [awaiting-auth] Approve spend → operator", ""},   // → operator likewise (cubic #421 P3)
	} {
		if got := ParseQueueItemDisplay(tc.raw).Scope; got != tc.want {
			t.Errorf("%s: scope = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestBuildQueueItemsPreservesOrder(t *testing.T) {
	items := BuildQueueItems([]string{
		"- [next] First item",
		"- [in-flight] Second :: A short summary.",
	})
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d: %+v", len(items), items)
	}
	// Positional assertions — the projection preserves input order (not merely non-empty).
	if items[0].Title != "First item" {
		t.Errorf("items[0].Title = %q, want %q", items[0].Title, "First item")
	}
	if items[1].Title != "Second" {
		t.Errorf("items[1].Title = %q, want %q", items[1].Title, "Second")
	}
	if items[1].Summary != "A short summary." {
		t.Errorf("items[1].Summary = %q, want %q", items[1].Summary, "A short summary.")
	}
}
