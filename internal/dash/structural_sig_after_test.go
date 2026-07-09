package dash

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/dop251/goja"
)

// TestStructuralSigAfter461Goja locks #461: structuralSig must change when ONLY
// the authored `after` list changes, so an after-only roadmap resequence takes the
// full re-layout path instead of the in-place fast path with stale geometry.
func TestStructuralSigAfter461Goja(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("assets", "goals.js"))
	if err != nil {
		raw, err = os.ReadFile(filepath.Join("internal", "dash", "assets", "goals.js"))
	}
	if err != nil {
		t.Fatalf("read goals.js: %v", err)
	}
	// Extract structuralSig as authored (not a reimplementation).
	re := regexp.MustCompile(`(?s)function structuralSig\(goals\) \{.*?\n  \}`)
	m := re.Find(raw)
	if m == nil {
		t.Fatal("could not extract function structuralSig from goals.js — #461")
	}
	vm := goja.New()
	// structuralSig closes over `collaborations` — seed an empty array.
	if err := vm.Set("collaborations", vm.NewArray()); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.RunString(string(m)); err != nil {
		t.Fatalf("load structuralSig: %v", err)
	}
	fn, ok := goja.AssertFunction(vm.Get("structuralSig"))
	if !ok {
		t.Fatal("structuralSig not a function in goja")
	}
	// Two docs that differ ONLY in after.
	base := []map[string]interface{}{
		{"id": "hub", "title": "Hub", "depth": 0, "scope": "fleet"},
		{"id": "a", "parent": "hub", "title": "A", "depth": 1, "scope": "project", "after": []interface{}{}},
		{"id": "b", "parent": "hub", "title": "B", "depth": 1, "scope": "project", "after": []interface{}{"a"}},
	}
	reseq := []map[string]interface{}{
		{"id": "hub", "title": "Hub", "depth": 0, "scope": "fleet"},
		{"id": "a", "parent": "hub", "title": "A", "depth": 1, "scope": "project", "after": []interface{}{"b"}},
		{"id": "b", "parent": "hub", "title": "B", "depth": 1, "scope": "project", "after": []interface{}{}},
	}
	sig := func(goals []map[string]interface{}) string {
		v, err := fn(goja.Undefined(), vm.ToValue(goals))
		if err != nil {
			t.Fatalf("structuralSig: %v", err)
		}
		return v.String()
	}
	s1, s2 := sig(base), sig(reseq)
	if s1 == s2 {
		t.Fatalf("structuralSig must differ on after-only resequence — #461\nbase=%s\nreseq=%s", s1, s2)
	}
	// Status-only change must NOT alter the structural signature (in-place path).
	live := []map[string]interface{}{
		{"id": "hub", "title": "Hub", "depth": 0, "scope": "fleet", "status_display": "in-flight"},
		{"id": "a", "parent": "hub", "title": "A", "depth": 1, "scope": "project", "after": []interface{}{}, "status_display": "awaiting"},
		{"id": "b", "parent": "hub", "title": "B", "depth": 1, "scope": "project", "after": []interface{}{"a"}, "status_display": "blocked"},
	}
	if sig(live) != s1 {
		t.Error("status_display-only change must not alter structuralSig — #461 / #283 contract")
	}
}
