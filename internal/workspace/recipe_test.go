package workspace

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/launch"
)

func TestStaleWorkspaceLaunchWarningAbsent(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir())
	if warn, err := StaleWorkspaceLaunchWarning("nobody"); err != nil || warn != "" {
		t.Fatalf("StaleWorkspaceLaunchWarning(absent) = (%q, %v), want (\"\", nil)", warn, err)
	}
}

func TestStaleWorkspaceLaunchWarningPresent(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	dir := filepath.Join(root, "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, LaunchFileName), []byte(`{"launch":"old","cwd":"/abs"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	warn, err := StaleWorkspaceLaunchWarning("data")
	if err != nil || warn == "" || !strings.Contains(warn, "deprecated") {
		t.Fatalf("StaleWorkspaceLaunchWarning(present) = (%q, %v), want deprecation warning", warn, err)
	}
}

func TestResolveRecipeFlatOnly(t *testing.T) {
	flat := &launch.Config{Agents: map[string]launch.Recipe{"data": {
		Launch: "grok --model composer-2.5-fast -w data",
		Cwd:    "/abs/worktree",
		Tmux:   "flotilla-data:desk",
	}}}
	r, err := ResolveRecipe("data", flat)
	if err != nil {
		t.Fatal(err)
	}
	if r.Launch != "grok --model composer-2.5-fast -w data" {
		t.Errorf("launch = %q, want flat harness command", r.Launch)
	}
	if r.Cwd != "/abs/worktree" || r.Tmux != "flotilla-data:desk" {
		t.Errorf("desk fields = %+v, want flat recipe cwd/tmux", r)
	}
}

func TestResolveRecipeIgnoresStaleWorkspaceLaunch(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	dir := filepath.Join(root, "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := `{"launch":"grok --model composer-2 -w data","cwd":"/stale","tmux":"flotilla:stale"}`
	if err := os.WriteFile(filepath.Join(dir, LaunchFileName), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	flat := &launch.Config{Agents: map[string]launch.Recipe{"data": {
		Launch: "grok --model composer-2.5-fast -w data",
		Cwd:    "/abs/worktree",
		Tmux:   "flotilla-data:desk",
	}}}
	r, err := ResolveRecipe("data", flat)
	if err != nil {
		t.Fatal(err)
	}
	if r.Launch != "grok --model composer-2.5-fast -w data" {
		t.Errorf("launch = %q, want flat file (stale workspace ignored)", r.Launch)
	}
}

func TestResolveRecipeMissingFlatEntryIsError(t *testing.T) {
	flat := &launch.Config{Agents: map[string]launch.Recipe{}}
	if _, err := ResolveRecipe("ghost", flat); err == nil {
		t.Fatal("ResolveRecipe(missing entry) = nil error, want error")
	}
	if _, err := ResolveRecipe("ghost", nil); err == nil {
		t.Fatal("ResolveRecipe(nil flat) = nil error, want error")
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
// resolved flat recipe with slot name "primary".
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

func TestResolveResumeSelectionAbsentOverlayUsesPrimary(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir()) // a clean workspace: no active-harness.json, no bundle
	flat := &launch.Config{Agents: map[string]launch.Recipe{"data": {
		Launch:    "claude -w data",
		Cwd:       "/abs",
		Fallbacks: []launch.HarnessSlot{{Surface: "grok", Launch: "grok -w data", Provider: "xai"}},
	}}}
	selection, err := ResolveResumeSelection("data", flat, "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if selection.Slot != SlotPrimary || selection.Surface != "claude-code" || selection.Source != "launch primary" {
		t.Fatalf("selection = %+v, want primary claude-code from launch primary", selection)
	}
	if selection.Recipe.Launch != "claude -w data" || selection.Recipe.Cwd != "/abs" {
		t.Errorf("recipe = %+v, want launchable primary", selection.Recipe)
	}
	if _, ok, readErr := ReadActiveOverlay("data"); ok || readErr != nil {
		t.Errorf("ReadActiveOverlay = (ok=%v, err=%v), want absent", ok, readErr)
	}
}

func TestResolveResumeSelectionValidOverlayUsesFallback(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir())
	writeOverlay(t, "data", `{"slot":"fallback-0","surface":"grok","provider":"xai"}`)
	flat := &launch.Config{Agents: map[string]launch.Recipe{"data": {
		Launch: "pi --model primary", Cwd: "/abs",
		Fallbacks: []launch.HarnessSlot{{Surface: "grok", Launch: "grok --model fallback", Provider: "xai"}},
	}}}
	selection, err := ResolveResumeSelection("data", flat, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if selection.Slot != "fallback-0" || selection.Surface != "grok" || selection.Source != "active-harness overlay" {
		t.Fatalf("selection = %+v, want fallback-0 grok from overlay", selection)
	}
	if selection.Recipe.Launch != "grok --model fallback" {
		t.Fatalf("launch = %q, want fallback launch", selection.Recipe.Launch)
	}
}

func TestResolveResumeSelectionInvalidOverlayStopsLaunch(t *testing.T) {
	flat := &launch.Config{Agents: map[string]launch.Recipe{"data": {
		Launch: "pi --model primary", Cwd: "/abs",
		Fallbacks: []launch.HarnessSlot{{Surface: "grok", Launch: "grok --model fallback"}},
	}}}
	for _, tc := range []struct {
		name, overlay string
	}{
		{name: "torn", overlay: `{not-json`},
		{name: "missing slot", overlay: `{"slot":"fallback-7","surface":"grok"}`},
		{name: "surface mismatch", overlay: `{"slot":"fallback-0","surface":"pi"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(rootEnv, t.TempDir())
			writeOverlay(t, "data", tc.overlay)
			if _, err := ResolveResumeSelection("data", flat, "pi"); err == nil {
				t.Fatal("invalid active-harness overlay must stop resume")
			}
		})
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

func TestSlotRecipeWrapsClaudeConfigDirFromSubscriptionID(t *testing.T) {
	accountsRoot := t.TempDir()
	t.Setenv("FLOTILLA_ACCOUNTS_ROOT", accountsRoot)
	chain := launch.Recipe{
		Cwd: "/abs",
		Primary: &launch.HarnessSlot{
			Surface:        "claude-code",
			Launch:         "claude -w xo",
			Provider:       "anthropic",
			SubscriptionID: "anthropic-work",
		},
	}
	r, ok, err := slotRecipeByName(chain, SlotPrimary)
	if err != nil || !ok {
		t.Fatalf("slotRecipeByName: ok=%v err=%v", ok, err)
	}
	if r.Launch == "claude -w xo" {
		t.Fatalf("launch unchanged %q, want CLAUDE_CONFIG_DIR wrap", r.Launch)
	}
	if !strings.Contains(r.Launch, "CLAUDE_CONFIG_DIR=") || !strings.Contains(r.Launch, "anthropic-work") {
		t.Fatalf("launch = %q, want config dir wrap for anthropic-work", r.Launch)
	}
	// stored chain launch string is unchanged
	if chain.Primary.Launch != "claude -w xo" {
		t.Errorf("stored primary launch mutated: %q", chain.Primary.Launch)
	}
}
