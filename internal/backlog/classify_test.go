package backlog

import "testing"

func TestClassifyLine(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{"in-flight", "- [in-flight] ship the thing", "in-flight"},
		{"next", "- [next] then this", "next"},
		{"blocked", "- [blocked] waiting on the operator", "blocked"},
		{"needs-attention", "- [needs-attention] stuck", "needs-attention"},
		{"awaiting-auth exact", "- [awaiting-auth] pending go/no-go", "awaiting-auth"},
		{"done marker", "- [done] complete", "done"},
		{"x checkbox", "- [x] complete", "done"},
		{"strike is done", "- ~~old idea~~", "done"},
		{"checkmark is done", "- ✅ shipped", "done"},
		{"numbered list item", "1. [in-flight] numbered still an item", "in-flight"},
		{"star bullet", "* [blocked] star bullet", "blocked"},
		{"unrecognized marker is malformed", "- [wip] not a known marker", "malformed"},
		{"awaiting-authorization near-miss is malformed", "- [awaiting-authorization] near miss", "malformed"},
		{"markerless is malformed", "- just some text", "malformed"},
		{"leading link never misclassifies", "- [in-flight] see [done] elsewhere", "in-flight"},
		{"not a list line", "## Backlog", ""},
		{"prose line", "some prose, not a list item", ""},
		{"blank", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyLine(c.line); got != c.want {
				t.Errorf("ClassifyLine(%q) = %q, want %q", c.line, got, c.want)
			}
		})
	}
}

func TestClassifyLineMatchesParse(t *testing.T) {
	// ClassifyLine and Parse must agree on marker semantics (they share markerOf).
	md := "## Backlog\n" +
		"- [in-flight] a\n" +
		"- [blocked] b\n" +
		"- [awaiting-auth] c\n" +
		"- [done] d\n" +
		"- [wip] e\n"
	st := Parse(md)
	if st.Blocked != 1 || st.AwaitingAuth != 1 || st.Done != 1 {
		t.Fatalf("Parse baseline unexpected: %+v", st)
	}
	// The malformed [wip] line is counted Malformed AND Unblocked by Parse; ClassifyLine calls it malformed.
	if got := ClassifyLine("- [wip] e"); got != "malformed" {
		t.Errorf("ClassifyLine malformed disagreement: %q", got)
	}
}

func TestMatchInBacklog(t *testing.T) {
	md := "## Goals\n" +
		"- [in-flight] a decoy outside the Backlog section\n" +
		"## Backlog\n" +
		"- [in-flight] wire the goals dashboard view\n" +
		"- [blocked] operator sign-off on the loss cap\n" +
		"- [done] branch protection applied\n" +
		"## Other\n" +
		"- [next] a decoy AFTER the Backlog section\n"

	cases := []struct {
		name      string
		substr    string
		wantMark  string
		wantMatch bool
	}{
		{"matches in-flight in section", "goals dashboard", "in-flight", true},
		{"matches blocked in section", "loss cap", "blocked", true},
		{"case-insensitive", "BRANCH PROTECTION", "done", true},
		{"decoy before section is not matched", "decoy outside", "", false},
		{"decoy after section is not matched", "decoy AFTER", "", false},
		{"no match", "nonexistent phrase", "", false},
		{"empty substr never matches", "", "", false},
		{"whitespace substr never matches", "   ", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mark, ok := MatchInBacklog(md, c.substr)
			if ok != c.wantMatch || mark != c.wantMark {
				t.Errorf("MatchInBacklog(%q) = (%q,%v), want (%q,%v)", c.substr, mark, ok, c.wantMark, c.wantMatch)
			}
		})
	}
}
