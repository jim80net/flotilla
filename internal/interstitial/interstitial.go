// Package interstitial classifies and clears non-work product chrome that can
// strand an otherwise idle agent surface. It deliberately owns no provider-
// specific copy: callers supply a captured frame and verified surface state.
package interstitial

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/surface"
)

// Class is the safety decision for a captured frame.
type Class int

const (
	NotInterstitial Class = iota
	Clearable
	UnknownInterstitial
)

const (
	// UnknownGapName is stable product-gap vocabulary for chrome which cannot be
	// cleared safely. Watch logs/routes this name; it never asks an operator to click.
	UnknownGapName = "unknown-interstitial"
	clearFailedGap = "interstitial-clear-exhausted"
	frameTailLines = 20
)

// Observation binds one frame to state probes made at the same watch pass.
type Observation struct {
	Frame    string
	State    surface.State
	Composer surface.ComposerDisposition
}

// Assessment describes the classification and its structural reason.
type Assessment struct {
	Class  Class
	Reason string
}

var (
	bracketControlRE = regexp.MustCompile(`(?:\[[^\]\n]{1,48}\]|【[^】\n]{1,48}】)`)
	protectedGateRE  = regexp.MustCompile(`(?i)\b(?:sign[ -]?in|log[ -]?in|authentication|required payment|billing|upgrade plan|purchase|subscribe)\b`)
	dismissActionRE  = regexp.MustCompile(`(?i)(?:\bopt[ -]?out\b|\b(?:no|not|never|later|skip|dismiss|decline|disable|close|private|without|cancel)\b)`)
)

// Classify makes a fail-closed decision. Product-chrome clearing requires an
// idle surface and a positively identified empty main composer. The sole modal
// exception is a structurally proven exit confirmation, whose chrome can hide
// the composer. Usage/auth/payment/approval gates are always real gates.
func Classify(observation Observation) Assessment {
	tail := deliver.TailRegion(observation.Frame, frameTailLines)
	if observation.State == surface.StateWorking ||
		observation.State == surface.StateAwaitingApproval ||
		observation.State == surface.StateShell ||
		observation.State == surface.StateErrored {
		return Assessment{Class: NotInterstitial, Reason: "protected surface state"}
	}
	if hit, _ := deliver.SessionUncooperative(tail); hit {
		return Assessment{Class: NotInterstitial, Reason: "uncooperative session gate"}
	}
	if protectedGateRE.MatchString(tail) {
		return Assessment{Class: NotInterstitial, Reason: "auth or payment gate"}
	}

	controls := modalControls(tail)
	if observation.State == surface.StateAwaitingInput {
		if surface.InteractiveConfirmPrompt(tail) && exitConfirmation(tail) &&
			(observation.Composer == surface.ComposerCleared || observation.Composer == surface.ComposerUndetermined) {
			return Assessment{Class: Clearable, Reason: "exit confirmation chrome"}
		}
		return Assessment{Class: NotInterstitial, Reason: "real input gate"}
	}
	if observation.State != surface.StateIdle {
		return Assessment{Class: NotInterstitial, Reason: "surface is not idle"}
	}
	if len(controls) == 0 {
		if observation.Composer == surface.ComposerUndetermined {
			return Assessment{Class: UnknownInterstitial, Reason: "idle surface without a readable composer"}
		}
		return Assessment{Class: NotInterstitial, Reason: "consistent idle or ordinary output"}
	}
	if observation.Composer != surface.ComposerCleared {
		return Assessment{Class: UnknownInterstitial, Reason: "chrome without a verified empty composer"}
	}
	if len(controls) >= 2 && hasDismissAction(controls) {
		return Assessment{Class: Clearable, Reason: "dismissible multi-action product chrome"}
	}
	return Assessment{Class: UnknownInterstitial, Reason: "unrecognized interactive chrome"}
}

// modalControls recognizes rendered multi-action geometry, not bracketed words
// anywhere in scrollback. The controls must share one row and the remainder of
// that row may contain only terminal chrome/separators. This prevents quoted
// prose or unrelated checklist items from authorizing a real keypress.
func modalControls(frame string) []string {
	for _, line := range strings.Split(frame, "\n") {
		matches := bracketControlRE.FindAllString(line, -1)
		if len(matches) < 2 {
			continue
		}
		remainder := bracketControlRE.ReplaceAllString(line, "")
		if strings.Trim(remainder, " \t│─┃━┌┐└┘·•>") != "" {
			continue
		}
		controls := make([]string, 0, len(matches))
		for _, match := range matches {
			controls = append(controls, strings.TrimSpace(strings.Trim(match, "[]【】")))
		}
		return controls
	}
	return nil
}

func hasDismissAction(controls []string) bool {
	for _, control := range controls {
		if dismissActionRE.MatchString(strings.TrimSpace(control)) {
			return true
		}
	}
	return false
}

func exitConfirmation(frame string) bool {
	lower := strings.ToLower(frame)
	return strings.Contains(lower, "exit") || strings.Contains(lower, "quit") || strings.Contains(lower, "close session")
}

// Options are injected so clearing and waits are deterministic under test.
type Options struct {
	SendEscape func(pane string) error
	Wait       func(time.Duration)
	NamedGap   func(agent, gap string)
	MaxEscapes int
}

// Manager performs the bounded Escape/re-probe loop. It never sends Enter,
// Tab, Ctrl-C, or text, so action order and suggestion-chip focus are irrelevant.
type Manager struct {
	sendEscape func(string) error
	wait       func(time.Duration)
	namedGap   func(string, string)
	maxEscapes int
}

func NewManager(options Options) *Manager {
	if options.Wait == nil {
		options.Wait = time.Sleep
	}
	if options.NamedGap == nil {
		options.NamedGap = func(string, string) {}
	}
	if options.MaxEscapes <= 0 {
		options.MaxEscapes = 3
	}
	return &Manager{
		sendEscape: options.SendEscape,
		wait:       options.Wait,
		namedGap:   options.NamedGap,
		maxEscapes: options.MaxEscapes,
	}
}

// Result reports whether chrome was cleared or converted into a named gap.
type Result struct {
	Cleared  bool
	Gap      string
	Attempts int
	Err      error
}

// Reconcile observes before every decision and after every key. It stops at the
// first consistent idle frame and never clears an unknown or protected frame.
func (m *Manager) Reconcile(agent, pane string, observe func() (Observation, error)) Result {
	if observe == nil {
		return Result{Err: errors.New("interstitial observer is required")}
	}
	for attempt := 0; ; attempt++ {
		observation, err := observe()
		if err != nil {
			return Result{Attempts: attempt, Err: err}
		}
		assessment := Classify(observation)
		switch assessment.Class {
		case NotInterstitial:
			return Result{Cleared: attempt > 0 && consistentIdle(observation), Attempts: attempt}
		case UnknownInterstitial:
			m.namedGap(agent, UnknownGapName)
			return Result{Gap: UnknownGapName, Attempts: attempt}
		case Clearable:
			if attempt >= m.maxEscapes {
				m.namedGap(agent, clearFailedGap)
				return Result{Gap: clearFailedGap, Attempts: attempt}
			}
			if m.sendEscape == nil {
				return Result{Attempts: attempt, Err: errors.New("interstitial Escape sender is required")}
			}
			if err := m.sendEscape(pane); err != nil {
				return Result{Attempts: attempt, Err: err}
			}
			m.wait(75 * time.Millisecond)
		}
	}
}

func consistentIdle(observation Observation) bool {
	return observation.State == surface.StateIdle &&
		observation.Composer == surface.ComposerCleared &&
		Classify(observation).Class == NotInterstitial
}
