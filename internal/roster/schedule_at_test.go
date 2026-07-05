package roster

import "testing"

func TestParseDailyAtValid(t *testing.T) {
	h, m, loc, err := ParseDailyAt("12:07Z")
	if err != nil {
		t.Fatalf("ParseDailyAt: %v", err)
	}
	if h != 12 || m != 7 {
		t.Errorf("time = %d:%d, want 12:07", h, m)
	}
	if loc != nil && loc.String() != "UTC" {
		t.Errorf("loc = %v, want UTC", loc)
	}
}

func TestLoadSchedulesValidation(t *testing.T) {
	valid := `{
		"agents":[{"name":"xo"},{"name":"backend"}],
		"schedules":[
			{"name":"parade","at":"12:07Z","to":"xo","prompt":"go"},
			{"name":"walk","at":"03:07+00:00","to":"backend","prompt":"prompts/walk.md"}
		]
	}`
	if _, err := Load(writeTemp(t, valid)); err != nil {
		t.Fatalf("valid schedules rejected: %v", err)
	}
	cases := map[string]string{
		"dup name":     `{"agents":[{"name":"xo"}],"schedules":[{"name":"a","at":"12:07Z","to":"xo","prompt":"x"},{"name":"a","at":"03:07Z","to":"xo","prompt":"y"}]}`,
		"bad at":       `{"agents":[{"name":"xo"}],"schedules":[{"name":"a","at":"12:07","to":"xo","prompt":"x"}]}`,
		"bad to":       `{"agents":[{"name":"xo"}],"schedules":[{"name":"a","at":"12:07Z","to":"nope","prompt":"x"}]}`,
		"empty prompt": `{"agents":[{"name":"xo"}],"schedules":[{"name":"a","at":"12:07Z","to":"xo","prompt":""}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeTemp(t, body)); err == nil {
				t.Errorf("Load(%s) = nil, want error", name)
			}
		})
	}
}
