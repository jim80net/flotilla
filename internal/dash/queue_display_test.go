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

func TestBuildQueueItemsPreservesOrder(t *testing.T) {
	items := BuildQueueItems([]string{
		"- [next] First item",
		"- [in-flight] Second :: A short summary.",
	})
	if len(items) != 2 || items[0].Title == "" || items[1].Summary == "" {
		t.Fatalf("items = %+v", items)
	}
}
