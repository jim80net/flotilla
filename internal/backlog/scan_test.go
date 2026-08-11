package backlog

import (
	"strings"
	"testing"
)

func TestScanIsParseAuthorityForBoundariesAndClassifications(t *testing.T) {
	md := "# Plan\n\n## Backlog\n" +
		"- [in-flight] primary item\n" +
		"  continuation with [detail: notes/primary.md]\n" +
		"  - nested markerless child\n" +
		"- [blocked] operator question\n" +
		"## Other\n- [next] ignored\n"
	scan := Scan(md)
	if !scan.Found || len(scan.Items) != 3 {
		t.Fatalf("Scan = %+v, want three Backlog items", scan)
	}
	want := []struct {
		line, class int
	}{
		{4, int(clsUnblocked)}, {6, int(clsMalformed)}, {7, int(clsBlocked)},
	}
	for i, item := range scan.Items {
		if item.StartLine != want[i].line || int(classOf(item.Classification)) != want[i].class {
			t.Errorf("item %d = %+v", i, item)
		}
	}
	if !strings.Contains(scan.Items[0].Raw, "continuation") || strings.Contains(scan.Items[0].Raw, "nested") {
		t.Errorf("primary Raw boundary = %q", scan.Items[0].Raw)
	}
	status := Parse(md)
	if status.Items != len(scan.Items) || status.Malformed != 1 || status.Blocked != 1 || len(status.Unblocked) != 2 {
		t.Fatalf("Parse parity = %+v scan=%+v", status, scan)
	}
	for _, item := range scan.Items {
		if got := ClassifyLine(item.Head); got != item.Classification {
			t.Errorf("ClassifyLine(%q)=%q, scan=%q", item.Head, got, item.Classification)
		}
	}
}
