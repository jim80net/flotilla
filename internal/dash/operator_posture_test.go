package dash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	end := strings.Index(string(raw), "  function usageText(")
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
}
