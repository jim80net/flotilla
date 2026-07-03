package tracker

import "testing"

func TestParseGoalIDTrailer_Valid(t *testing.T) {
	body := "## Summary\n\nSome work.\n\ngoal-id: dash-next-gen\n"
	if got := ParseGoalIDTrailer(body); got != "dash-next-gen" {
		t.Fatalf("ParseGoalIDTrailer() = %q, want dash-next-gen", got)
	}
}

func TestParseGoalIDTrailer_Absent(t *testing.T) {
	if got := ParseGoalIDTrailer("no trailer here"); got != "" {
		t.Fatalf("ParseGoalIDTrailer() = %q, want empty", got)
	}
}

func TestParseGoalIDTrailer_Malformed(t *testing.T) {
	cases := []string{
		"goal-id:",
		"goal-id: ",
		"goal-id:bad",
		"Goal-ID: dash-next-gen",
		"goal-id: dash next gen",
		"prefix goal-id: dash-next-gen",
	}
	for _, body := range cases {
		if got := ParseGoalIDTrailer(body); got != "" {
			t.Errorf("ParseGoalIDTrailer(%q) = %q, want empty", body, got)
		}
	}
}

func TestParseGoalIDTrailer_CRLF(t *testing.T) {
	body := "details\r\n\r\ngoal-id: dash-next-gen\r\n"
	if got := ParseGoalIDTrailer(body); got != "dash-next-gen" {
		t.Fatalf("ParseGoalIDTrailer(CRLF) = %q, want dash-next-gen", got)
	}
}

func TestParseGoalIDTrailer_FirstValidLineWins(t *testing.T) {
	body := "goal-id: first\n\nfooter\n\ngoal-id: second\n"
	if got := ParseGoalIDTrailer(body); got != "first" {
		t.Fatalf("ParseGoalIDTrailer() = %q, want first", got)
	}
}

func TestParseGoalIDTrailer_CaseSensitiveSlug(t *testing.T) {
	body := "goal-id: Dash-Next-Gen\n"
	if got := ParseGoalIDTrailer(body); got != "Dash-Next-Gen" {
		t.Fatalf("ParseGoalIDTrailer() = %q, want case-preserved slug", got)
	}
}

func TestEnrichIssue_PopulatesGoalID(t *testing.T) {
	issue := Issue{Body: "details\n\ngoal-id: ship-platform\n"}
	EnrichIssue(&issue)
	if issue.GoalID != "ship-platform" {
		t.Fatalf("GoalID = %q, want ship-platform", issue.GoalID)
	}
}

func TestGet_PopulatesGoalID(t *testing.T) {
	f := &fakeRunner{stdout: []byte(`{"number":106,"title":"t","body":"work\n\ngoal-id: dash-next-gen\n","state":"OPEN","labels":[],"author":{"login":"jim80net"},"comments":[],"url":"https://github.com/jim80net/flotilla/issues/106"}`)}
	g := newFakeTracker(t, f)
	issue, err := g.Get(ctx(), 106)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.GoalID != "dash-next-gen" {
		t.Fatalf("GoalID = %q, want dash-next-gen", issue.GoalID)
	}
}

func TestList_IncludeBodyParsesGoalID(t *testing.T) {
	f := &fakeRunner{stdout: []byte(`[
		{"number":1,"title":"linked","state":"OPEN","labels":[],"author":{"login":"x"},"updatedAt":"2026-01-01T00:00:00Z","body":"goal-id: goals-map-view\n"},
		{"number":2,"title":"plain","state":"OPEN","labels":[],"author":{"login":"x"},"updatedAt":"2026-01-01T00:00:00Z","body":"no trailer"}
	]`)}
	g := newFakeTracker(t, f)
	issues, err := g.List(ctx(), ListFilter{IncludeBody: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if issues[0].GoalID != "goals-map-view" {
		t.Errorf("issue[0].GoalID = %q, want goals-map-view", issues[0].GoalID)
	}
	if issues[1].GoalID != "" {
		t.Errorf("issue[1].GoalID = %q, want empty", issues[1].GoalID)
	}
	if v, _ := f.arg("--json"); v != listFields+",body" {
		t.Errorf("--json = %q, want list fields + body", v)
	}
}
