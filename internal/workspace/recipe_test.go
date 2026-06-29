package workspace

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jim80net/flotilla/internal/launch"
)

// writeWorkspaceRecipe sets the workspace root to a temp dir and writes a
// launch.json for the agent, returning nothing (the root is set via t.Setenv).
func writeWorkspaceRecipe(t *testing.T, agent, json string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	dir := filepath.Join(root, agent)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, LaunchFileName), []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRecipePresentAndValid(t *testing.T) {
	writeWorkspaceRecipe(t, "xo",
		`{"launch":"claude -w xo","cwd":"/abs/worktree","tmux":"flotilla:xo"}`)
	r, ok, err := LoadRecipe("xo")
	if err != nil || !ok {
		t.Fatalf("LoadRecipe = (%+v, %v, %v), want a valid recipe", r, ok, err)
	}
	if r.Launch != "claude -w xo" || r.Cwd != "/abs/worktree" {
		t.Errorf("recipe fields not parsed: %+v", r)
	}
}

func TestLoadRecipeAbsentFallsThrough(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir()) // root exists but no agent dir
	r, ok, err := LoadRecipe("nobody")
	if err != nil || ok {
		t.Fatalf("LoadRecipe(absent) = (%+v, %v, %v), want (zero, false, nil)", r, ok, err)
	}
}

func TestLoadRecipeInvalidIsError(t *testing.T) {
	writeWorkspaceRecipe(t, "a", `{"launch":"claude","cwd":"relative/path"}`) // non-absolute cwd
	if _, ok, err := LoadRecipe("a"); err == nil {
		t.Fatalf("LoadRecipe(relative cwd) = ok=%v err=nil, want a validation error", ok)
	}
	writeWorkspaceRecipe(t, "b", `{not json`)
	if _, _, err := LoadRecipe("b"); err == nil {
		t.Fatal("LoadRecipe(malformed json) = nil error, want parse error")
	}
}

func TestResolveRecipeWorkspaceWins(t *testing.T) {
	writeWorkspaceRecipe(t, "a", `{"launch":"workspace-cmd","cwd":"/abs"}`)
	flat := &launch.Config{Agents: map[string]launch.Recipe{"a": {Launch: "flat-cmd", Cwd: "/abs"}}}
	r, err := ResolveRecipe("a", flat)
	if err != nil {
		t.Fatal(err)
	}
	if r.Launch != "workspace-cmd" {
		t.Errorf("ResolveRecipe used %q, want the workspace recipe", r.Launch)
	}
}

func TestResolveRecipeFlatFallback(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir()) // no workspace recipe
	flat := &launch.Config{Agents: map[string]launch.Recipe{"a": {Launch: "flat-cmd", Cwd: "/abs"}}}
	r, err := ResolveRecipe("a", flat)
	if err != nil {
		t.Fatal(err)
	}
	if r.Launch != "flat-cmd" {
		t.Errorf("ResolveRecipe used %q, want the flat fallback", r.Launch)
	}
}

func TestResolveRecipeNeitherIsError(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir())
	if _, err := ResolveRecipe("ghost", &launch.Config{Agents: map[string]launch.Recipe{}}); err == nil {
		t.Fatal("ResolveRecipe(neither) = nil error, want a clear not-found error")
	}
	if _, err := ResolveRecipe("ghost", nil); err == nil {
		t.Fatal("ResolveRecipe(neither, nil flat) = nil error, want error")
	}
}

// writeOverlay writes an active-harness.json for an agent under the (already-set)
// workspace root. It does NOT set the root — a caller that also writes a recipe must
// set the root once (t.Setenv) so both land under the same temp tree.
func writeOverlay(t *testing.T, agent, json string) {
	t.Helper()
	dir, err := Dir(agent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ActiveHarnessFileName), []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadActiveOverlayAbsentIsNone(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir())
	ov, ok, err := ReadActiveOverlay("nobody")
	if err != nil || ok {
		t.Fatalf("ReadActiveOverlay(absent) = (%+v, %v, %v), want (zero, false, nil)", ov, ok, err)
	}
}

func TestReadActiveOverlayPresent(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir())
	writeOverlay(t, "data", `{"slot":"fallback-0","surface":"grok","provider":"xai"}`)
	ov, ok, err := ReadActiveOverlay("data")
	if err != nil || !ok {
		t.Fatalf("ReadActiveOverlay(present) = (%+v, %v, %v), want a parsed overlay", ov, ok, err)
	}
	if ov.Slot != "fallback-0" || ov.Surface != "grok" || ov.Provider != "xai" {
		t.Errorf("overlay fields not parsed: %+v", ov)
	}
}

func TestWriteActiveOverlayRoundTrips(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir())
	want := ActiveOverlay{Slot: "fallback-1", Surface: "grok", Provider: "xai", SwitchToken: "tok-123"}
	if err := WriteActiveOverlay("data", want); err != nil {
		t.Fatalf("WriteActiveOverlay = %v, want nil", err)
	}
	got, ok, err := ReadActiveOverlay("data")
	if err != nil || !ok {
		t.Fatalf("ReadActiveOverlay after write = (%+v, %v, %v)", got, ok, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, want)
	}
	// The temp file used for the atomic write must not linger.
	dir, _ := Dir("data")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" || filepath.Base(e.Name()) != ActiveHarnessFileName {
			if e.Name() != ActiveHarnessFileName {
				t.Errorf("unexpected leftover file after atomic write: %q", e.Name())
			}
		}
	}
}

// TestResolveHarnessAbsentOverlayIsPrimary: no overlay ⇒ the primary slot, which is the
// resolved (flat or workspace) recipe with slot name "primary".
func TestResolveHarnessAbsentOverlayIsPrimary(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir())
	flat := &launch.Config{Agents: map[string]launch.Recipe{"data": {Launch: "claude -w data", Cwd: "/abs"}}}
	slot, r, err := ResolveHarness("data", flat)
	if err != nil {
		t.Fatal(err)
	}
	if slot != SlotPrimary {
		t.Errorf("slot = %q, want %q (absent overlay ⇒ primary)", slot, SlotPrimary)
	}
	if r.Launch != "claude -w data" {
		t.Errorf("primary recipe launch = %q, want the resolved flat launch", r.Launch)
	}
}

// TestResolveHarnessPresentOverlayNamesSlot: a present overlay naming fallback-0
// resolves THAT slot's launch from the chain (not the primary launch), preserving the
// shared desk fields (cwd). The slot's surface is carried on the overlay.
func TestResolveHarnessPresentOverlayNamesSlot(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir())
	writeOverlay(t, "data", `{"slot":"fallback-0","surface":"grok","provider":"xai"}`)
	flat := &launch.Config{Agents: map[string]launch.Recipe{"data": {
		Launch:    "claude -w data",
		Cwd:       "/abs",
		Fallbacks: []launch.HarnessSlot{{Surface: "grok", Launch: "grok -w data", Provider: "xai"}},
	}}}
	slot, r, err := ResolveHarness("data", flat)
	if err != nil {
		t.Fatal(err)
	}
	if slot != "fallback-0" {
		t.Errorf("slot = %q, want fallback-0 (overlay names it)", slot)
	}
	if r.Launch != "grok -w data" {
		t.Errorf("resolved launch = %q, want the fallback-0 slot launch %q", r.Launch, "grok -w data")
	}
	if r.Cwd != "/abs" {
		t.Errorf("resolved cwd = %q, want the shared recipe cwd /abs", r.Cwd)
	}
}

// TestResolveHarnessOverlayNamesAbsentSlotFailsSafe: an overlay naming a slot the chain
// does NOT contain (stale chain edit) fails SAFE to primary, never erroring the desk.
func TestResolveHarnessOverlayNamesAbsentSlotFailsSafe(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir())
	writeOverlay(t, "data", `{"slot":"fallback-7","surface":"grok"}`) // chain has no fallback-7
	flat := &launch.Config{Agents: map[string]launch.Recipe{"data": {Launch: "claude -w data", Cwd: "/abs"}}}
	slot, r, err := ResolveHarness("data", flat)
	if err != nil {
		t.Fatalf("ResolveHarness(absent slot) = err %v, want fail-safe to primary", err)
	}
	if slot != SlotPrimary || r.Launch != "claude -w data" {
		t.Errorf("absent-slot resolve = (%q, %q), want (primary, the primary launch)", slot, r.Launch)
	}
}

// TestResolveHarnessTornOverlayFallsBackToPrimary: a torn/malformed overlay must NEVER
// make a desk unresolvable — it fails SAFE to the primary slot.
func TestResolveHarnessTornOverlayFallsBackToPrimary(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir())
	writeOverlay(t, "data", `{not valid json`)
	flat := &launch.Config{Agents: map[string]launch.Recipe{"data": {Launch: "claude -w data", Cwd: "/abs"}}}
	slot, r, err := ResolveHarness("data", flat)
	if err != nil {
		t.Fatalf("ResolveHarness(torn overlay) = err %v, want fail-safe to primary (nil err)", err)
	}
	if slot != SlotPrimary {
		t.Errorf("torn overlay slot = %q, want %q (fail-safe to primary)", slot, SlotPrimary)
	}
	if r.Launch != "claude -w data" {
		t.Errorf("torn-overlay recipe launch = %q, want the primary launch", r.Launch)
	}
}

// TestResolveActiveRecipeView: the recipe-shaped view returns the active slot's recipe.
func TestResolveActiveRecipeView(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir())
	flat := &launch.Config{Agents: map[string]launch.Recipe{"data": {Launch: "claude -w data", Cwd: "/abs"}}}
	r, err := ResolveActiveRecipe("data", flat)
	if err != nil {
		t.Fatal(err)
	}
	if r.Launch != "claude -w data" || r.Cwd != "/abs" {
		t.Errorf("ResolveActiveRecipe = %+v, want the resolved recipe", r)
	}
}
