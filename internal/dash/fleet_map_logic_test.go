package dash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

type railGroupFixture struct {
	ChannelID string `json:"channel_id"`
	Role      string `json:"role"`
	Label     string `json:"label"`
	Depth     int    `json:"depth"`
	Desks     []struct {
		Name      string `json:"name"`
		Role      string `json:"role"`
		ChannelID string `json:"channel_id"`
	} `json:"desks"`
}

// TestFleetMapCanonicalHierarchy745 executes the authored rail projection against a
// channel-dump-shaped fixture: routing edges repeat every seat, while org_nodes supplies
// the one canonical hierarchy. Generic identities stand in for the private deployment.
func TestFleetMapCanonicalHierarchy745(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("assets", "dash.js"))
	if err != nil {
		raw, err = os.ReadFile(filepath.Join("internal", "dash", "assets", "dash.js"))
	}
	if err != nil {
		t.Fatalf("read dash.js: %v", err)
	}
	start := strings.Index(string(raw), "  function coordinatorNames(")
	end := strings.Index(string(raw), "  function groupForDesk(")
	if start < 0 || end <= start {
		t.Fatal("could not extract fleet-map projection from dash.js")
	}
	source := `var cache = {status:{}};
function escapeHtml(s) { return String(s == null ? "" : s)
  .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
  .replace(/"/g, "&quot;").replace(/'/g, "&#39;"); }
` + string(raw[start:end])
	vm := goja.New()
	if _, err := vm.RunString(source); err != nil {
		t.Fatalf("load fleet-map projection: %v", err)
	}

	fixture := map[string]any{
		"coordinators": []string{"meta-xo", "alpha-xo", "beta-xo", "dev-xo"},
		"org_root":     "meta-xo",
		"org_nodes": []map[string]any{
			{"id": "meta-xo", "kind": "coordinator", "children": []string{"portfolio-a", "portfolio-b", "engineering"}},
			{"id": "portfolio-a", "kind": "container", "parent": "meta-xo", "children": []string{"alpha-program"}},
			{"id": "alpha-program", "kind": "container", "parent": "portfolio-a", "children": []string{"alpha-xo"}},
			{"id": "alpha-xo", "kind": "coordinator", "parent": "alpha-program", "home_channel_id": "C_ALPHA", "children": []string{"alpha-build"}},
			{"id": "alpha-build", "kind": "desk", "parent": "alpha-xo", "home_channel_id": "C_ALPHA_BUILD"},
			{"id": "portfolio-b", "kind": "container", "parent": "meta-xo", "children": []string{"beta-xo"}},
			{"id": "beta-xo", "kind": "coordinator", "parent": "portfolio-b", "home_channel_id": "C_BETA", "children": []string{"beta-research"}},
			{"id": "beta-research", "kind": "desk", "parent": "beta-xo", "home_channel_id": "C_BETA_RESEARCH"},
			{"id": "engineering", "kind": "container", "parent": "meta-xo", "children": []string{"dev-xo"}},
			{"id": "dev-xo", "kind": "coordinator", "parent": "engineering", "home_channel_id": "C_DEV", "children": []string{"dev-build"}},
			{"id": "dev-build", "kind": "desk", "parent": "dev-xo", "home_channel_id": "C_DEV_BUILD"},
		},
		"channels": []map[string]any{
			{"channel_id": "900000000000000001", "xo_agent": "meta-xo", "role": "fleet-command", "members": []string{"alpha-xo", "beta-xo", "dev-xo", "alpha-build", "beta-research", "dev-build"}},
			{"channel_id": "900000000000000002", "xo_agent": "alpha-xo", "role": "project", "members": []string{"alpha-build"}},
			{"channel_id": "900000000000000003", "xo_agent": "alpha-xo", "members": []string{"alpha-build"}},
			{"channel_id": "900000000000000004", "xo_agent": "beta-xo", "role": "project", "members": []string{"beta-research"}},
			{"channel_id": "900000000000000005", "xo_agent": "dev-xo", "role": "project", "members": []string{"dev-build"}},
		},
	}
	status := map[string]any{
		"xo": "meta-xo",
		"agents": []map[string]any{
			{"name": "meta-xo"}, {"name": "alpha-xo"}, {"name": "beta-xo"}, {"name": "dev-xo"},
			{"name": "alpha-build"}, {"name": "beta-research"}, {"name": "dev-build"},
		},
	}
	build, ok := goja.AssertFunction(vm.Get("buildRailGroups"))
	if !ok {
		t.Fatal("buildRailGroups not callable")
	}
	value, err := build(goja.Undefined(), vm.ToValue(fixture), vm.ToValue(status))
	if err != nil {
		t.Fatalf("buildRailGroups: %v", err)
	}
	encoded, err := json.Marshal(value.Export())
	if err != nil {
		t.Fatalf("marshal groups: %v", err)
	}
	var groups []railGroupFixture
	if err := json.Unmarshal(encoded, &groups); err != nil {
		t.Fatalf("decode groups: %v", err)
	}

	wantLabels := []string{"Fleet Command", "Portfolio A", "Alpha Program", "Portfolio B", "Engineering"}
	labelCount := map[string]int{}
	labelDepth := map[string]int{}
	seatCount := map[string]int{}
	seatChannel := map[string]string{}
	for _, group := range groups {
		labelCount[group.Label]++
		labelDepth[group.Label] = group.Depth
		if strings.HasPrefix(group.Label, "#") || strings.Contains(group.Label, "900000000000000") {
			t.Errorf("raw channel id leaked into group label %q", group.Label)
		}
		for _, desk := range group.Desks {
			seatCount[desk.Name]++
			seatChannel[desk.Name] = desk.ChannelID
		}
	}
	for _, label := range wantLabels {
		if labelCount[label] != 1 {
			t.Errorf("group %q count = %d, want 1 (groups=%+v)", label, labelCount[label], groups)
		}
	}
	if labelDepth["Alpha Program"] != 1 {
		t.Errorf("nested container depth = %d, want 1", labelDepth["Alpha Program"])
	}
	for _, seat := range []string{"meta-xo", "alpha-xo", "beta-xo", "dev-xo", "alpha-build", "beta-research", "dev-build"} {
		if seatCount[seat] != 1 {
			t.Errorf("seat %q count = %d, want 1", seat, seatCount[seat])
		}
	}
	if seatChannel["alpha-build"] != "C_ALPHA_BUILD" {
		t.Errorf("alpha-build channel = %q, want its org home", seatChannel["alpha-build"])
	}

	roleTag, ok := goja.AssertFunction(vm.Get("railRoleTag"))
	if !ok {
		t.Fatal("railRoleTag not callable")
	}
	redundant, err := roleTag(goja.Undefined(), vm.ToValue("alpha-xo"), vm.ToValue("xo"))
	if err != nil || redundant.String() != "" {
		t.Errorf("redundant role tag = %q, err=%v; want suppressed", redundant, err)
	}
	separated, err := roleTag(goja.Undefined(), vm.ToValue("lead"), vm.ToValue("xo"))
	if err != nil || !strings.Contains(separated.String(), "· xo") {
		t.Errorf("separated role tag = %q, err=%v; want visible separator", separated, err)
	}
}
