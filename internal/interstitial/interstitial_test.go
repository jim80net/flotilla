package interstitial

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestClassifyProductInterstitialByStructure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		fixture string
	}{
		{name: "specimen", fixture: "product_banner.txt"},
		{name: "mutated copy swapped action order", fixture: "product_banner_mutated.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(Observation{
				Frame:    fixture(t, tc.fixture),
				State:    surface.StateIdle,
				Composer: surface.ComposerCleared,
			})
			if got.Class != Clearable {
				t.Fatalf("Classify() class = %v, want Clearable (assessment: %+v)", got.Class, got)
			}
		})
	}
}

func TestQuotedControlsDoNotAuthorizeKeypress(t *testing.T) {
	t.Parallel()
	frame := "The guide compares [Opt out] and [Opt in] in prose.\n│ ❯ │\n"
	got := Classify(Observation{Frame: frame, State: surface.StateIdle, Composer: surface.ComposerCleared})
	if got.Class != NotInterstitial {
		t.Fatalf("Classify(quoted controls) = %+v, want NotInterstitial", got)
	}
}

func TestExitProseDoesNotConvertRealInputGateToClearable(t *testing.T) {
	t.Parallel()
	observation := Observation{
		Frame: "The guide says type exit when the session is done.\n\n" +
			"Do you want to deploy?\n1. Allow\n2. Deny\nEnter to confirm\n",
		State:    surface.StateAwaitingInput,
		Composer: surface.ComposerUndetermined,
	}
	assertNoEscape(t, observation)
}

func TestCopiedControlRowInScrollbackDoesNotAuthorizeEscape(t *testing.T) {
	t.Parallel()
	observation := Observation{
		Frame: "The prior screen rendered this exact row:\n[Opt out]  [Opt in]\n\n" +
			"Turn complete.\n│ ❯ │\n",
		State:    surface.StateIdle,
		Composer: surface.ComposerCleared,
	}
	assertNoEscape(t, observation)
}

func TestQuotedBlockProseDoesNotBridgeControlsToComposer(t *testing.T) {
	t.Parallel()
	observation := Observation{
		Frame: "[Opt out]  [Opt in]\n" +
			"> The report quotes a suggestion rather than rendering a chip\n" +
			"│ ❯ │\n",
		State:    surface.StateIdle,
		Composer: surface.ComposerCleared,
	}
	assertNoEscape(t, observation)
}

func assertNoEscape(t *testing.T, observation Observation) {
	t.Helper()
	if got := Classify(observation); got.Class != NotInterstitial {
		t.Fatalf("Classify() = %+v, want NotInterstitial", got)
	}
	keys := 0
	manager := NewManager(Options{SendEscape: func(string) error { keys++; return nil }})
	result := manager.Reconcile("backend", "%1", func() (Observation, error) { return observation, nil })
	if keys != 0 || result.Cleared || result.Gap != "" || result.Err != nil {
		t.Fatalf("Reconcile()=%+v keys=%d, want untouched real input/idle", result, keys)
	}
}

func TestManagerReprobesAfterEscapeAndStopsAtConsistentIdle(t *testing.T) {
	t.Parallel()
	frames := []Observation{
		{Frame: fixture(t, "product_banner_mutated.txt"), State: surface.StateIdle, Composer: surface.ComposerCleared},
		{Frame: fixture(t, "product_banner.txt"), State: surface.StateIdle, Composer: surface.ComposerCleared},
		{Frame: "Turn complete.\n\u2502 \u276f \u2502\n", State: surface.StateIdle, Composer: surface.ComposerCleared},
	}
	observed := 0
	var keys []string
	m := NewManager(Options{
		SendEscape: func(pane string) error {
			keys = append(keys, pane)
			return nil
		},
		Wait: func(time.Duration) {},
	})
	result := m.Reconcile("backend", "%1", func() (Observation, error) {
		if observed >= len(frames) {
			return Observation{}, errors.New("unexpected extra probe")
		}
		got := frames[observed]
		observed++
		return got, nil
	})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if !result.Cleared || result.Gap != "" {
		t.Fatalf("Reconcile() = %+v, want cleared without gap", result)
	}
	if !reflect.DeepEqual(keys, []string{"%1", "%1"}) {
		t.Fatalf("escape targets = %v, want two Escape attempts", keys)
	}
	if observed != 3 {
		t.Fatalf("probes = %d, want initial plus one after each key", observed)
	}
}

func TestExitConfirmationIsClearableWithoutReachableComposer(t *testing.T) {
	t.Parallel()
	got := Classify(Observation{
		Frame:    fixture(t, "exit_confirmation.txt"),
		State:    surface.StateAwaitingInput,
		Composer: surface.ComposerUndetermined,
	})
	if got.Class != Clearable {
		t.Fatalf("Classify(exit confirmation) = %+v, want Clearable", got)
	}
}

func TestManagerClearsExitConfirmationToIdle(t *testing.T) {
	t.Parallel()
	observations := []Observation{
		{Frame: fixture(t, "exit_confirmation.txt"), State: surface.StateAwaitingInput, Composer: surface.ComposerUndetermined},
		{Frame: "Turn complete.\n│ ❯ │\n", State: surface.StateIdle, Composer: surface.ComposerCleared},
	}
	index := 0
	escapes := 0
	m := NewManager(Options{
		SendEscape: func(string) error { escapes++; return nil },
		Wait:       func(time.Duration) {},
	})
	result := m.Reconcile("backend", "%1", func() (Observation, error) {
		observation := observations[index]
		index++
		return observation, nil
	})
	if !result.Cleared || result.Attempts != 1 || escapes != 1 || index != 2 {
		t.Fatalf("result=%+v escapes=%d probes=%d, want one Escape then confirmed idle", result, escapes, index)
	}
}

func TestUnknownInterstitialNamesGapWithoutKeypress(t *testing.T) {
	t.Parallel()
	var keys, gaps int
	m := NewManager(Options{
		SendEscape: func(string) error { keys++; return nil },
		NamedGap: func(agent, gap string) {
			gaps++
			if agent != "frontend" || gap != UnknownGapName {
				t.Fatalf("NamedGap(%q, %q)", agent, gap)
			}
		},
	})
	result := m.Reconcile("frontend", "%2", func() (Observation, error) {
		return Observation{Frame: fixture(t, "unknown_chrome.txt"), State: surface.StateIdle, Composer: surface.ComposerCleared}, nil
	})
	if result.Gap != UnknownGapName || keys != 0 || gaps != 1 {
		t.Fatalf("result=%+v keys=%d gaps=%d", result, keys, gaps)
	}
}

func TestManagerNamesGapAfterBoundedEscapeExhaustion(t *testing.T) {
	t.Parallel()
	keys := 0
	var gap string
	manager := NewManager(Options{
		SendEscape: func(string) error { keys++; return nil },
		Wait:       func(time.Duration) {},
		NamedGap:   func(_, name string) { gap = name },
		MaxEscapes: 2,
	})
	observation := Observation{
		Frame:    fixture(t, "product_banner.txt"),
		State:    surface.StateIdle,
		Composer: surface.ComposerCleared,
	}
	result := manager.Reconcile("backend", "%1", func() (Observation, error) { return observation, nil })
	if keys != 2 || result.Attempts != 2 || result.Gap != clearFailedGap || gap != clearFailedGap {
		t.Fatalf("result=%+v keys=%d callbackGap=%q, want two Escapes then %q", result, keys, gap, clearFailedGap)
	}
}

func TestProtectedStatesAreNeverDismissed(t *testing.T) {
	t.Parallel()
	cases := []Observation{
		{Frame: fixture(t, "product_banner.txt"), State: surface.StateWorking, Composer: surface.ComposerCleared},
		{Frame: fixture(t, "product_banner.txt"), State: surface.StateAwaitingApproval, Composer: surface.ComposerCleared},
		{Frame: "You're out of usage credits\n[Upgrade]", State: surface.StateIdle, Composer: surface.ComposerCleared},
		{Frame: "Authentication required\n[Sign in]", State: surface.StateIdle, Composer: surface.ComposerCleared},
		{Frame: "Do you want to allow this action?\n1. Allow\n2. Deny", State: surface.StateAwaitingInput, Composer: surface.ComposerCleared},
	}
	for _, observation := range cases {
		observation := observation
		t.Run(observation.Frame, func(t *testing.T) {
			t.Parallel()
			if got := Classify(observation); got.Class != NotInterstitial {
				t.Fatalf("Classify() = %+v, want NotInterstitial", got)
			}
			keys := 0
			manager := NewManager(Options{SendEscape: func(string) error { keys++; return nil }})
			result := manager.Reconcile("backend", "%1", func() (Observation, error) { return observation, nil })
			if keys != 0 || result.Cleared || result.Gap != "" || result.Err != nil {
				t.Fatalf("protected reconcile result=%+v keys=%d", result, keys)
			}
		})
	}
}

func TestClearableRequiresKnownEmptyComposer(t *testing.T) {
	t.Parallel()
	for _, disposition := range []surface.ComposerDisposition{
		surface.ComposerUndetermined,
		surface.ComposerPending,
		surface.ComposerQueued,
		surface.ComposerSubAgent,
		surface.ComposerListNav,
	} {
		got := Classify(Observation{Frame: fixture(t, "product_banner.txt"), State: surface.StateIdle, Composer: disposition})
		if got.Class == Clearable {
			t.Fatalf("composer %v classified Clearable: %+v", disposition, got)
		}
	}
}
