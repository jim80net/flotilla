package dash

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// TestParseHash579Goja runs the live dash.js parseHash under goja so a refactor
// cannot keep the seedLanding markers while breaking deep-link parsing (#579).
func TestParseHash579Goja(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("assets", "dash.js"))
	if err != nil {
		raw, err = os.ReadFile(filepath.Join("internal", "dash", "assets", "dash.js"))
	}
	if err != nil {
		t.Fatalf("read dash.js: %v", err)
	}
	// Extract the parseHash function body as authored (not a reimplementation).
	re := regexp.MustCompile(`(?s)function parseHash\(raw\) \{.*?\n  \}`)
	m := re.Find(raw)
	if m == nil {
		t.Fatal("could not extract function parseHash from dash.js — #579")
	}
	vm := goja.New()
	if _, err := vm.RunString(string(m)); err != nil {
		t.Fatalf("load parseHash: %v", err)
	}
	parse, ok := goja.AssertFunction(vm.Get("parseHash"))
	if !ok {
		t.Fatal("parseHash not a function in goja")
	}
	call := func(h string) goja.Value {
		v, err := parse(goja.Undefined(), vm.ToValue(h))
		if err != nil {
			t.Fatalf("parseHash(%q): %v", h, err)
		}
		return v
	}
	// Empty / unknown → null (default_view may still choose).
	for _, h := range []string{"", "#", "#nope", "random"} {
		if !goja.IsNull(call(h)) {
			t.Errorf("parseHash(%q) = %v, want null", h, call(h))
		}
	}
	// Conversations deep links.
	for _, h := range []string{"#conv", "conv", "#conv/alpha"} {
		v := call(h)
		if goja.IsNull(v) {
			t.Fatalf("parseHash(%q) = null, want conversations", h)
		}
		o := v.ToObject(vm)
		if o.Get("view").String() != "conversations" {
			t.Errorf("parseHash(%q).view = %q", h, o.Get("view"))
		}
	}
	desk := call("#conv/alpha-xo").ToObject(vm).Get("desk").String()
	if desk != "alpha-xo" {
		t.Errorf("parseHash(#conv/alpha-xo).desk = %q, want alpha-xo", desk)
	}
	// Goals deep links.
	g := call("#goals").ToObject(vm)
	if g.Get("view").String() != "goals" {
		t.Errorf("parseHash(#goals).view = %q", g.Get("view"))
	}
	node := call("#goals/ship-platform").ToObject(vm).Get("node").String()
	if node != "ship-platform" {
		t.Errorf("parseHash(#goals/ship-platform).node = %q", node)
	}
	// Other SPA tabs.
	for _, tab := range []string{"issues", "decisions"} {
		v := call("#" + tab).ToObject(vm)
		if v.Get("view").String() != tab {
			t.Errorf("parseHash(#%s).view = %q", tab, v.Get("view"))
		}
	}
	// Guard: the extracted source must still document the default_view contract,
	// using metadata rather than blocking landing on the full Goals document.
	if !strings.Contains(string(m), "default_view") && !strings.Contains(string(raw), "g.default_view") {
		t.Error("dash.js must still reference g.default_view for landing — #579")
	}
	if !strings.Contains(string(raw), `getJSON("/api/goals/meta")`) {
		t.Error("seedLanding must use the lightweight Goals metadata endpoint")
	}
}
