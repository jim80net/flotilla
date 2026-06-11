// Package roster describes the fleet: which agents exist and how to reach each
// one's terminal pane. Secrets (the Discord bot token and per-agent webhook
// urls) live in a separate, never-committed env file loaded by LoadSecrets —
// the roster config itself is safe to commit.
package roster

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Agent is one coordinated coding agent — a long-lived session in a tmux pane.
type Agent struct {
	// Name is the stable identifier used on the command line and as the
	// agent's Discord identity (e.g. "v12-dev").
	Name string `json:"name"`
	// TmuxTitle is the title of the tmux pane the agent runs in. Delivery
	// resolves the pane by matching this title, so it survives pane
	// reordering. Defaults to Name when empty.
	TmuxTitle string `json:"tmux_title,omitempty"`
	// Surface names the agent's terminal-surface driver (e.g. "claude-code",
	// "grok", "cursor") — how flotilla submits turns, assesses pane state, and
	// rotates context for this agent. Empty defaults to the reference driver
	// ("claude-code"). Validated against the registered drivers at command
	// startup (in cmd, to keep roster free of an internal/surface import).
	Surface string `json:"surface,omitempty"`
}

// Title returns the tmux pane title to match for this agent.
func (a Agent) Title() string {
	if a.TmuxTitle != "" {
		return a.TmuxTitle
	}
	return a.Name
}

// Config is the committable, secret-free fleet description.
type Config struct {
	// GuildID and ChannelID identify the Discord coordination channel. Reserved
	// for the inbound reader (v0.1) and the setup bootstrap; not used by v0 send.
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	// OperatorUserID is the Discord user id whose messages flotilla will act
	// on once inbound Discord reading lands (v0.1). It is the filter allow-list
	// — flotilla ignores everyone else, and ignores the channel's own webhook
	// posts, so the bus cannot feed back on itself. Stored now so the design is
	// ready; not a secret.
	OperatorUserID string  `json:"operator_user_id,omitempty"`
	Agents         []Agent `json:"agents"`

	// --- `send` capability ---

	// MirrorInterAgent gates whether `flotilla send` mirrors the delivered message
	// to the Discord audit channel. DEFAULT FALSE (off): inter-agent (XO↔desk)
	// coordination stays in the tmux panes and does not clutter the operator's
	// Discord — only the operator-facing `flotilla notify` posts. Set true to
	// restore the v0 always-mirror audit trail. A per-call `--mirror` / `--no-mirror`
	// overrides it (precedence: flag → this setting → off). `notify` is unaffected.
	MirrorInterAgent bool `json:"mirror_inter_agent,omitempty"`

	// --- `watch` capability (flotilla watch); validated at load ---

	// XOAgent is the delivery target for a bare operator message and the target
	// of the heartbeat. If set, it MUST name an agent in Agents.
	XOAgent string `json:"xo_agent,omitempty"`
	// HeartbeatInterval is a Go duration (e.g. "20m"); empty or "0" disables the
	// heartbeat. Parsed (validated) at load.
	HeartbeatInterval string `json:"heartbeat_interval,omitempty"`
	// HeartbeatMessage is the idempotent tick prompt; watch supplies a default
	// when empty.
	HeartbeatMessage string `json:"heartbeat_message,omitempty"`

	// ChangeDetector opts into heartbeat v2: instead of waking the XO every
	// interval with a generic prompt, the detector wakes it ONLY on a material
	// change (a desk transition or a tracker change) and rotates its context
	// after each settled handling. An idle fleet costs nothing. Opt-in (default
	// false → the legacy always-wake heartbeat). Requires heartbeat_interval > 0.
	ChangeDetector bool `json:"change_detector,omitempty"`
	// LivenessPingMode tunes the v2 liveness safety ping WITHOUT a rebuild
	// (the C1b tradeoff): "none" (default — true $0-idle, a wide safety ping at
	// ~2K×interval, accepting a ~2K idle-fleet wedge window), "interval" (a cheap
	// ack-ping every K-1 intervals — the strict K×interval window), or
	// "consecutive" (ping every K-1, alert after ~2 missed pings — the middle
	// ground). Empty ⇒ "none". Only consulted when ChangeDetector is on.
	LivenessPingMode string `json:"liveness_ping_mode,omitempty"`

	// heartbeatDur is HeartbeatInterval parsed once at load (0 = disabled), so
	// consumers get a typed value instead of re-parsing the string.
	heartbeatDur time.Duration
}

// Load reads and validates a roster config file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read roster %q: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse roster %q: %w", path, err)
	}
	if len(c.Agents) == 0 {
		return nil, fmt.Errorf("roster %q lists no agents", path)
	}
	seenName := make(map[string]bool, len(c.Agents))
	seenTitle := make(map[string]bool, len(c.Agents))
	for _, a := range c.Agents {
		if a.Name == "" {
			return nil, fmt.Errorf("roster %q has an agent with an empty name", path)
		}
		if seenName[a.Name] {
			return nil, fmt.Errorf("roster %q has duplicate agent %q", path, a.Name)
		}
		seenName[a.Name] = true
		// The resolution key travels on a TAB-delimited, NEWLINE-separated tmux
		// list-panes line and is stored verbatim as the @flotilla_agent marker; a
		// tab or newline in it would corrupt that wire format — splitting the
		// marker (so a tagged pane never resolves) or injecting a phantom record.
		// Reject it at load so the corruption is impossible by construction.
		if strings.ContainsAny(a.Title(), "\t\n\r") {
			return nil, fmt.Errorf("roster %q: agent %q resolution key %q contains a tab/newline (would corrupt tmux pane resolution)", path, a.Name, a.Title())
		}
		// Two agents resolving to the same tmux pane title would misroute
		// (delivery resolves by Title()); reject it at load time.
		if seenTitle[a.Title()] {
			return nil, fmt.Errorf("roster %q: agents share tmux title %q (would misroute delivery)", path, a.Title())
		}
		seenTitle[a.Title()] = true
	}
	// watch-capability fields: validate at load so a misconfigured daemon
	// refuses to start rather than failing silently at the first tick.
	if c.XOAgent != "" {
		if _, err := c.Agent(c.XOAgent); err != nil {
			return nil, fmt.Errorf("roster %q: xo_agent %q is not in agents", path, c.XOAgent)
		}
	}
	if c.HeartbeatInterval != "" && c.HeartbeatInterval != "0" {
		d, err := time.ParseDuration(c.HeartbeatInterval)
		if err != nil {
			return nil, fmt.Errorf("roster %q: invalid heartbeat_interval %q: %w", path, c.HeartbeatInterval, err)
		}
		c.heartbeatDur = d
	}
	switch c.LivenessPingMode {
	case "", "none", "interval", "consecutive":
	default:
		return nil, fmt.Errorf("roster %q: invalid liveness_ping_mode %q (want none|interval|consecutive)", path, c.LivenessPingMode)
	}
	// The change-detector ticks on heartbeat_interval; without one it would never
	// run (and never check liveness). Refuse to start rather than silently no-op.
	if c.ChangeDetector && c.heartbeatDur <= 0 {
		return nil, fmt.Errorf("roster %q: change_detector requires a positive heartbeat_interval", path)
	}
	return &c, nil
}

// HeartbeatDur returns the parsed heartbeat interval (0 when disabled).
func (c *Config) HeartbeatDur() time.Duration { return c.heartbeatDur }

// Agent looks up an agent by name.
func (c *Config) Agent(name string) (Agent, error) {
	for _, a := range c.Agents {
		if a.Name == name {
			return a, nil
		}
	}
	return Agent{}, fmt.Errorf("no agent named %q in roster", name)
}
