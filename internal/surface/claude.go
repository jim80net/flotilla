package surface

import (
	"log"

	"github.com/jim80net/flotilla/internal/deliver"
)

func init() { Register(newClaudeCode()) }

// claudeCode is the reference driver: it wraps the existing internal/deliver
// primitives so behavior is byte-identical to flotilla's prior hard-coded Claude
// Code handling. The deliver calls are injectable (fields) so the state-mapping is
// unit-testable without a live tmux server.
type claudeCode struct {
	paneCommand func(string) (string, error)
	isShell     func(string) bool
	capturePane func(string) (string, error)
	parseBusy   func(string) bool
	send        func(string, string) error
	clear       func(string) error
}

func newClaudeCode() claudeCode {
	return claudeCode{
		paneCommand: deliver.PaneCommand,
		isShell:     deliver.IsShell,
		capturePane: deliver.CapturePane,
		parseBusy:   deliver.ParseBusy,
		send:        deliver.Send,
		clear:       deliver.ClearContext,
	}
}

func (claudeCode) Name() string { return "claude-code" }

// Submit delivers a turn exactly as the prior code did: bracketed paste + Enter.
func (c claudeCode) Submit(pane, text string) error { return c.send(pane, text) }

// Assess replicates the prior watch-gate classification EXACTLY:
//   - pane_current_command errors OR is a shell  → Shell (the crash fast-path)
//   - else capture fails                          → Idle  (fail-open: matches the
//     prior "busy-check error ⇒ treat as not busy")
//   - else the working-spinner is present         → Working, else Idle
func (c claudeCode) Assess(pane string) State {
	cmd, err := c.paneCommand(pane)
	if err != nil || c.isShell(cmd) {
		return StateShell
	}
	captured, err := c.capturePane(pane)
	if err != nil {
		log.Printf("flotilla: surface(claude-code): pane capture failed for %q: %v (treating as idle)", pane, err)
		return StateIdle
	}
	if c.parseBusy(captured) {
		return StateWorking
	}
	return StateIdle
}

// Rotate resets context by injecting Claude Code's /clear (verified literal
// keystrokes). RotateStrategy is SlashCommand, so RotateContext routes here.
func (c claudeCode) Rotate(pane string) error { return c.clear(pane) }

func (claudeCode) RotateStrategy() Strategy { return SlashCommand }
