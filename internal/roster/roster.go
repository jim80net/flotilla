// Package roster describes the fleet: which agents exist and how to reach each
// one's terminal pane. Secrets (the Discord bot token and per-agent webhook
// urls) live in a separate, never-committed env file loaded by LoadSecrets —
// the roster config itself is safe to commit.
package roster

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// Channel binds one Discord channel to exactly one XO (its "home" hub) and the
// member set addressable from that channel. It is the federation unit: a project
// channel binds a project-XO + its desks; the fleet-command channel binds the
// meta-XO + the project-XOs (a project-XO is to the meta-XO what a desk is to a
// project-XO). The inbound relay routes a message by its ORIGIN channel to the
// matching binding. The legacy single ChannelID + XOAgent form is the degenerate
// one-binding case — see Bindings.
type Channel struct {
	// ChannelID is the Discord channel this binding owns (unique across bindings —
	// this is what guarantees "exactly one relay per channel").
	ChannelID string `json:"channel_id"`
	// XOAgent is the hub a BARE operator message in this channel routes to.
	XOAgent string `json:"xo_agent"`
	// Members are the agents addressable via "@name" in this channel (this hub's
	// desks; for the meta-XO, its project-XOs).
	Members []string `json:"members,omitempty"`
	// Role is an optional human label ("fleet-command" / "project") for notices and
	// the setup helper; routing is uniform regardless of role.
	Role string `json:"role,omitempty"`
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
	// overrides this setting (precedence: explicit flag > roster setting > default
	// off). `notify` is unaffected.
	MirrorInterAgent bool `json:"mirror_inter_agent,omitempty"`

	// --- `watch` capability (flotilla watch); validated at load ---

	// XOAgent is this daemon's PRIMARY XO: the heartbeat/clock target, the status
	// default, and the voice/push target. In the legacy single-channel form it is
	// ALSO the bare-message delivery target (the one binding's XO). It is ORTHOGONAL
	// to the binding form, so it MAY be set alongside channels[] to pick which XO a
	// federated relay daemon clocks (typically the meta-XO) — without it, the clock
	// falls back to Agents[0]. If set, it MUST name an agent in Agents.
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

	// --- federation (`federation-channels`); validated at load ---

	// Channels binds Discord channels to XOs for federation (per-XO + fleet-command
	// channels). Each binds one channel to one XO + its member scope; the inbound
	// relay routes a message by its ORIGIN channel to that binding. MUTUALLY
	// EXCLUSIVE with the legacy top-level channel_id (the other binding form — use
	// one), but NOT with xo_agent, which remains valid as this daemon's primary/clock
	// XO. When empty, the single channel_id/xo_agent is the one effective binding (see
	// Bindings) — backward compatible.
	Channels []Channel `json:"channels,omitempty"`

	// CosAgent names the chief-of-staff agent the CoS context-mirror (#108) mirrors
	// operator↔XO traffic to. Validated (must name an agent in Agents) when set. A
	// generalizable role, not a deployment desk name. Empty ⇒ the CoS mirror is inert
	// (no mirror, no ledger — fully backward compatible).
	CosAgent string `json:"cos_agent,omitempty"`
	// CosLedger is where the CoS context-mirror appends its deterministic
	// who-knows-what ledger (the productized state/context-ledger.md). Optional;
	// defaults at load to <roster-dir>/context-ledger.md when CosAgent is set.
	// Host-local state (the CoS's read source) like the other watch state files —
	// NOT content-hashed as a wake signal (it would self-trigger). Inert when CosAgent
	// is unset.
	CosLedger string `json:"cos_ledger,omitempty"`

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
	// federation: channel↔XO bindings. The legacy channel_id is one IMPLICIT binding
	// (see Bindings); an explicit channels[] is the federated set. The two BINDING
	// forms are mutually exclusive (you cannot declare both a legacy binding and a
	// federated set). xo_agent is ORTHOGONAL to the binding form — it is this daemon's
	// primary/clock XO (also the heartbeat/status/voice target), and with channels[]
	// it picks WHICH XO this relay daemon clocks (typically the meta-XO) instead of
	// silently defaulting to Agents[0]. Fail-closed so a misconfigured federation
	// refuses to start.
	if len(c.Channels) > 0 {
		if c.ChannelID != "" {
			return nil, fmt.Errorf("roster %q: channels[] and the legacy channel_id are mutually exclusive binding forms — use one (xo_agent may accompany channels[] as this daemon's primary/clock XO)", path)
		}
		seenChan := make(map[string]bool, len(c.Channels))
		seenXO := make(map[string]bool, len(c.Channels))
		for _, ch := range c.Channels {
			if ch.ChannelID == "" {
				return nil, fmt.Errorf("roster %q: a channel binding has an empty channel_id", path)
			}
			// Unique channel id ⇒ exactly one relay owns a channel (no double-delivery).
			if seenChan[ch.ChannelID] {
				return nil, fmt.Errorf("roster %q: channel %q is bound more than once (exactly one relay per channel)", path, ch.ChannelID)
			}
			seenChan[ch.ChannelID] = true
			if ch.XOAgent == "" {
				return nil, fmt.Errorf("roster %q: channel %q has no xo_agent", path, ch.ChannelID)
			}
			if _, err := c.Agent(ch.XOAgent); err != nil {
				return nil, fmt.Errorf("roster %q: channel %q xo_agent %q is not in agents", path, ch.ChannelID, ch.XOAgent)
			}
			// A channel has one hub: an agent is the xo_agent of at most one binding.
			// (An agent MAY still be a MEMBER of several channels — that's the
			// recursion, e.g. a project-XO is a member of fleet-command.)
			if seenXO[ch.XOAgent] {
				return nil, fmt.Errorf("roster %q: agent %q is the xo_agent of more than one channel binding", path, ch.XOAgent)
			}
			seenXO[ch.XOAgent] = true
			for _, m := range ch.Members {
				if _, err := c.Agent(m); err != nil {
					return nil, fmt.Errorf("roster %q: channel %q member %q is not in agents", path, ch.ChannelID, m)
				}
			}
		}
	}
	// cos_agent (the CoS context-mirror #108): validated fail-closed when set. CosLedger
	// is resolved here to be non-empty IFF the mirror is active, so a single check
	// (cfg.CosLedger != "") is the correct gate for every consumer: when cos_agent is
	// set, default the ledger path (explicit cos_ledger, else <roster-dir>/context-ledger.md,
	// host-local beside the roster); when cos_agent is UNSET, force CosLedger empty so a
	// stray cos_ledger can never activate the (inert) feature.
	if c.CosAgent != "" {
		if _, err := c.Agent(c.CosAgent); err != nil {
			return nil, fmt.Errorf("roster %q: cos_agent %q is not in agents", path, c.CosAgent)
		}
		if c.CosLedger == "" {
			c.CosLedger = filepath.Join(filepath.Dir(path), "context-ledger.md")
		}
	} else {
		c.CosLedger = "" // inert: cos_ledger without cos_agent is ignored (the feature is gated on cos_agent)
	}
	return &c, nil
}

// HeartbeatDur returns the parsed heartbeat interval (0 when disabled).
func (c *Config) HeartbeatDur() time.Duration { return c.heartbeatDur }

// Bindings returns the effective channel→XO bindings the relay routes on. With an
// explicit Channels list it returns that; otherwise it synthesizes the single
// legacy binding (channel_id + xo_agent, with EVERY agent as a member — preserving
// the pre-federation behavior where "@name" resolved against all agents, and
// defaulting the XO to the first agent when xo_agent is unset, matching watch). When
// neither channel_id nor channels[] is set (a clock-only daemon), it returns nil.
//
// READ-ONLY CONTRACT: the returned bindings (and each binding's Members slice) are
// shared with the Config in the federation path (it returns the config's own
// Channels slice header, not a copy) — callers MUST treat the result as immutable
// and MUST NOT append to or reassign any Members slice. Config is read-only after
// Load, and every consumer (BindingForChannel, the relay's memberResolver) only
// reads, so no copy is made on this per-message path.
func (c *Config) Bindings() []Channel {
	if len(c.Channels) > 0 {
		return c.Channels
	}
	if c.ChannelID == "" {
		return nil
	}
	members := make([]string, 0, len(c.Agents))
	for _, a := range c.Agents {
		members = append(members, a.Name)
	}
	xo := c.XOAgent
	if xo == "" && len(c.Agents) > 0 {
		xo = c.Agents[0].Name
	}
	return []Channel{{ChannelID: c.ChannelID, XOAgent: xo, Members: members}}
}

// BindingForChannel returns the binding that owns a Discord channel id (ok=false
// when no binding owns it — the relay ignores such a channel).
func (c *Config) BindingForChannel(channelID string) (Channel, bool) {
	for _, ch := range c.Bindings() {
		if ch.ChannelID == channelID {
			return ch, true
		}
	}
	return Channel{}, false
}

// IsXO reports whether name is an XO in this roster — the top-level primary
// xo_agent OR the xo_agent of any channel binding (federation). The CoS outbound
// mirror uses it to scope the ledger to XO→operator replies (a desk `notify` is
// not operator↔XO traffic in v1).
func (c *Config) IsXO(name string) bool {
	if name == "" {
		return false
	}
	if c.XOAgent == name {
		return true
	}
	for _, ch := range c.Bindings() {
		if ch.XOAgent == name {
			return true
		}
	}
	return false
}

// ChannelForXO returns the Discord channel an XO owns (the binding whose xo_agent is
// name), for tagging that XO's outbound ledger entry. ok=false when no binding is
// owned by name (then the caller records an empty channel — the ledger renders it as
// "-"). For the legacy single-fleet form this is the synthesized binding's channel.
func (c *Config) ChannelForXO(name string) (string, bool) {
	for _, ch := range c.Bindings() {
		if ch.XOAgent == name {
			return ch.ChannelID, true
		}
	}
	return "", false
}

// Agent looks up an agent by name.
func (c *Config) Agent(name string) (Agent, error) {
	for _, a := range c.Agents {
		if a.Name == name {
			return a, nil
		}
	}
	return Agent{}, fmt.Errorf("no agent named %q in roster", name)
}
