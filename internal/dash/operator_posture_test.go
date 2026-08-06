package dash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
)

func TestOperatorVisualState744(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("assets", "dash.js"))
	if err != nil {
		raw, err = os.ReadFile(filepath.Join("internal", "dash", "assets", "dash.js"))
	}
	if err != nil {
		t.Fatalf("read dash.js: %v", err)
	}
	start := strings.Index(string(raw), "  function operatorVisualState(")
	end := strings.Index(string(raw), "  function renderFreshness(")
	if start < 0 || end <= start {
		t.Fatal("could not extract operator visual-state projection")
	}
	vm := goja.New()
	if _, err := vm.RunString(string(raw[start:end])); err != nil {
		t.Fatalf("load operator visual-state projection: %v", err)
	}
	project, ok := goja.AssertFunction(vm.Get("operatorVisualState"))
	if !ok {
		t.Fatal("operatorVisualState not callable")
	}
	cases := []struct{ state, posture, want string }{
		{"idle", "available", "idle"},
		{"working", "composing", "working"},
		{"idle", "blocked", "blocked"},
	}
	for _, tc := range cases {
		got, err := project(goja.Undefined(), vm.ToValue(tc.state), vm.ToValue(tc.posture))
		if err != nil || got.String() != tc.want {
			t.Errorf("operatorVisualState(%q, %q) = %q, %v; want %q", tc.state, tc.posture, got, err, tc.want)
		}
	}
	evidenceText, ok := goja.AssertFunction(vm.Get("postureEvidenceText"))
	if !ok {
		t.Fatal("postureEvidenceText not callable")
	}
	agent := map[string]any{
		"loop_posture_reason":      "backlog:blocked=1,unblocked=0",
		"loop_posture_observed_at": "2026-08-06T09:10:00Z",
		"blocked_items":            []string{"waiting on external grant"},
	}
	now := time.Date(2026, 8, 6, 9, 15, 0, 0, time.UTC).UnixMilli()
	got, err := evidenceText(goja.Undefined(), vm.ToValue(agent), vm.ToValue(now))
	if err != nil {
		t.Fatal(err)
	}
	want := "backlog:blocked=1,unblocked=0 · 5m old · waiting on external grant"
	if got.String() != want {
		t.Errorf("postureEvidenceText = %q, want %q", got.String(), want)
	}
}
