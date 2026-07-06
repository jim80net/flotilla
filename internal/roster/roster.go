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

	"github.com/jim80net/flotilla/internal/backlog"
)

// Agent is one coordinated coding agent — a long-lived session in a tmux pane.
type Agent struct {
	// Name is the stable identifier used on the command line and as the
	// agent's Discord identity (e.g. "backend").
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
	// Heartbeat overrides the per-agent desk-heartbeat default (#183): the detector
	// re-engages a desk that settles Idle mid-task on the clock cadence. A nil pointer
	// means the default applies (ON for a general desk); false opts a deliberately-quiet
	// desk OUT; true forces it ON. Pointer-with-omitempty so an ABSENT flag is
	// distinguishable from an explicit false.
	Heartbeat *bool `json:"heartbeat,omitempty"`
	// ApprovalSensitive marks a high-consequence desk — one that places orders or spends.
	// Such a desk defaults its heartbeat OFF (the #184 carve-out): the claude driver's
	// Idle assessment is binary and cannot yet distinguish an approval-blocked desk from
	// an idle one, so a default-on heartbeat could land text into a pending approval modal.
	// An explicit Heartbeat=true overrides this once a genuine approval classifier exists.
	ApprovalSensitive bool `json:"approval_sensitive,omitempty"`
	// AdjutantFor binds this agent as the adjutant (mechanical interrupt consumer) for
	// the named coordinator. Legacy alias assistant_for is accepted at load. An adjutant
	// receives layer interrupts (liveness pings, material edges) before the leader sees
	// them; see openspec/changes/stackable-flotillas-438/design.md.
	AdjutantFor string `json:"adjutant_for,omitempty"`
	// AssistantFor is the legacy alias for adjutant_for (same semantics).
	AssistantFor string `json:"assistant_for,omitempty"`
}

// Title returns the tmux pane title to match for this agent.
func (a Agent) Title() string {
	if a.TmuxTitle != "" {
		return a.TmuxTitle
	}
	return a.Name
}

// Channel binds one Discord channel to exactly one XO (its hub) and the member set
// addressable from that channel. It is the federation unit: a project channel binds a
// project-XO + its desks; the fleet-command channel binds the meta-XO + the project-XOs
// (a project-XO is to the meta-XO what a desk is to a project-XO). One channel routes to
// one XO, but an XO MAY hub several channels — its first-listed binding is then its
// "home"/primary channel (see ChannelForXO). The inbound relay routes a message by its
// ORIGIN channel to the matching binding. The legacy single ChannelID + XOAgent form is
// the degenerate one-binding case — see Bindings.
type Channel struct {
	// ChannelID is the Discord channel this binding owns (unique across bindings —
	// this is what guarantees "exactly one relay per channel").
	ChannelID string `json:"channel_id"`
	// XOAgent is the hub a BARE operator message in this channel routes to.
	XOAgent string `json:"xo_agent"`
	// Members are the agents addressable via "@name" in this channel (this hub's
	// desks; for the meta-XO, its project-XOs).
	Members []string `json:"members,omitempty"`
	// Role is an optional label ("fleet-command" / "project") for notices and the setup
	// helper. COMMAND routing is uniform regardless of role, but SYNTHESIS routing
	// (visibility-synthesis / B2) treats role=="fleet-command" as LOAD-BEARING: a
	// fleet-command channel is a broadcast/command channel whose members are command
	// targets, not synthesis parents, so it contributes ZERO synthesis edges (excluded from
	// AgentsBelow / AgentsAbove / the load-time DAG check — see synthesis.go). A broadcast
	// channel (members = many subordinates) that is NOT tagged fleet-command will form a
	// synthesis cycle and Load will fail-closed refuse it — by design, surfacing the
	// misconfiguration rather than silently inverting the hierarchy.
	Role string `json:"role,omitempty"`
}

// UrgentWindow matches material-change reasons that bypass the adjutant buffer (#439).
type UrgentWindow struct {
	// Match is a case-insensitive substring; any reason containing it is urgent.
	Match string `json:"match"`
}

// Schedule is one daily wall-clock dispatch the watch daemon may fire (#413).
type Schedule struct {
	// Name is a stable identifier (unique across schedules) used in logs and the
	// durable last-fired sidecar.
	Name string `json:"name"`
	// At is the daily wall-clock time with an explicit timezone, e.g. "12:07Z" or
	// "03:07+00:00". Parsed by ParseDailyAt at load.
	At string `json:"at"`
	// To names the roster agent that receives the prompt.
	To string `json:"to"`
	// Prompt is the delivery body inline, or a path to a host-local prompt file
	// (preferred for long prompts). A path that exists on disk at fire time is read
	// as file content; otherwise the string is sent verbatim.
	Prompt string `json:"prompt"`
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
	// XORotate gates idle-edge context rotation in continueXO (#467):
	// always (default — rotate before every continuation/backlog/settle wake),
	// never (preserve session; harness compaction only), or handoff (suppress bare
	// /clear until handoff-gated recycle at chapter ends — same as never until #443).
	// FLOTILLA_XO_ROTATE env overrides this field at watch startup.
	XORotate string `json:"xo_rotate,omitempty"`
	// DelegationNudge gates the coordinator IC-ing delegation nudge (#232, #481):
	// on (default) or off. FLOTILLA_DELEGATION_NUDGE env overrides at watch startup.
	DelegationNudge string `json:"delegation_nudge,omitempty"`

	// UrgentWindows declares substring matches on material-change reasons that cut through
	// the adjutant buffer to the leader immediately (#439 phase 1c). Operator relay
	// messages always bypass the buffer via the KindRelay delivery path.
	UrgentWindows []UrgentWindow `json:"urgent_windows,omitempty"`

	// StackableWakes opts into per-layer material-wake routing (#438): each material desk
	// transition is scoped to OwningXO instead of exclusively the primary xo_agent. Default
	// false (absent ⇒ legacy primary-XO-only routing). Requires adjutant_for bindings for
	// laminar flow when enabled — see stackable-flotillas-438 design.
	StackableWakes bool `json:"stackable_wakes,omitempty"`

	// VisibilitySynthesis opts into the visibility-synthesis (B2) heartbeat: when a desk finishes
	// below a synthesizing agent (a project-XO for Tier 2, the meta-XO for Tier 3), the detector
	// emits a WakeSynthesis to that agent so it curates a rollup of its subordinates' latest state
	// up into its own channel. Routing is derived from the federation membership graph (AgentsBelow
	// / AgentsAbove, fleet-command-excluded). Opt-in (default false ⇒ fully inert — no synthesis
	// wake ever fires, behavior byte-identical to before this change). Builds on the change-detector
	// (it rides the same tick + the AgentsAbove resolver) and the per-desk webhooks (the post
	// target), so it is only effective when change_detector is on and secrets supply each
	// synthesizing agent's channel webhook.
	VisibilitySynthesis bool `json:"visibility_synthesis,omitempty"`

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
	// Schedules are daemon-native daily wall-clock dispatches (#413): each entry
	// names a slot (at, with explicit timezone), a target agent (to), and a prompt
	// (inline or a host-local file path — file preferred for long prompts). Durable
	// last-fired state lives in <roster-dir>/flotilla-schedule-state.json (not in
	// the roster). Empty ⇒ the scheduler is inert.
	Schedules []Schedule `json:"schedules,omitempty"`

	// CosLedger is where the CoS context-mirror appends its deterministic
	// who-knows-what ledger (the productized state/context-ledger.md). Optional;
	// defaults at load to <roster-dir>/context-ledger.md when CosAgent is set.
	// Host-local state (the CoS's read source) like the other watch state files —
	// NOT content-hashed as a wake signal (it would self-trigger). Inert when CosAgent
	// is unset. MUST resolve to a path on a LOCAL filesystem: the lock-free concurrent
	// append (watch hook + a separate notify process) relies on O_APPEND-under-PIPE_BUF
	// atomicity, which networked mounts (NFS/overlay) may not honor.
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
		if err := validateSafeAgentName(path, a.Name, "agent name"); err != nil {
			return nil, err
		}
		if err := validateSafeAgentName(path, a.adjutantTarget(), "adjutant_for target"); err != nil {
			return nil, err
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
	if _, err := ParseXORotate(c.XORotate); err != nil {
		return nil, fmt.Errorf("roster %q: %w", path, err)
	}
	if _, err := ParseDelegationNudge(c.DelegationNudge); err != nil {
		return nil, fmt.Errorf("roster %q: %w", path, err)
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
		for _, ch := range c.Channels {
			if ch.ChannelID == "" {
				return nil, fmt.Errorf("roster %q: a channel binding has an empty channel_id", path)
			}
			// THE load-bearing invariant: a unique channel id ⇒ exactly one relay owns a
			// channel (no double-delivery). Channel→XO stays strictly one-to-one.
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
			// An agent MAY be the XO (hub) of MULTIPLE channels — XO→channels is
			// one-to-many. A flotilla XO is primary both in its C2-group channel and its own
			// command channel; the meta-XO is primary across the C2 group. The one-to-one
			// direction (each channel → exactly one XO) is preserved by seenChan above; that
			// is the routing-critical invariant. The XO's FIRST-listed binding is its
			// primary/home channel for outbound ledger tagging (see ChannelForXO). An agent
			// MAY also be a MEMBER of several channels (the recursion).
			for _, m := range ch.Members {
				if _, err := c.Agent(m); err != nil {
					return nil, fmt.Errorf("roster %q: channel %q member %q is not in agents", path, ch.ChannelID, m)
				}
			}
		}
	}
	// Synthesis routing (visibility-synthesis / B2) reads the tier below an agent and posts
	// one level up; that is acyclic IFF the synthesis-edge graph is a DAG. Assert it
	// fail-closed so a federation that would form a synthesis feedback loop refuses to start
	// (self-edges AND fleet-command channels are excluded from the edge set — see
	// assertSynthesisAcyclic). The legacy single binding is a star (no cycle) and passes
	// trivially; a clock-only daemon has no bindings and passes.
	if err := c.assertSynthesisAcyclic(); err != nil {
		return nil, fmt.Errorf("roster %q: %w", path, err)
	}
	if err := c.validateSchedules(path); err != nil {
		return nil, err
	}
	if err := c.validateAdjutantBindings(path); err != nil {
		return nil, err
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

// validateSchedules checks schedules[] at load so a misconfigured daemon refuses
// to start rather than silently skipping or double-firing.
func (c *Config) validateSchedules(path string) error {
	if len(c.Schedules) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(c.Schedules))
	for _, sch := range c.Schedules {
		if sch.Name == "" {
			return fmt.Errorf("roster %q: a schedule has an empty name", path)
		}
		if seen[sch.Name] {
			return fmt.Errorf("roster %q: duplicate schedule name %q", path, sch.Name)
		}
		seen[sch.Name] = true
		if _, _, _, err := ParseDailyAt(sch.At); err != nil {
			return fmt.Errorf("roster %q: schedule %q: %w", path, sch.Name, err)
		}
		if sch.To == "" {
			return fmt.Errorf("roster %q: schedule %q has an empty to", path, sch.Name)
		}
		if _, err := c.Agent(sch.To); err != nil {
			return fmt.Errorf("roster %q: schedule %q to %q is not in agents", path, sch.Name, sch.To)
		}
		if strings.TrimSpace(sch.Prompt) == "" {
			return fmt.Errorf("roster %q: schedule %q has an empty prompt", path, sch.Name)
		}
	}
	return nil
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

// AutoSwitchEligible reports whether the watch detector MAY enqueue an automatic
// harness switch for this agent. Coordination desks (every binding's xo_agent, the
// primary xo_agent, and cos_agent) stay on their current harness; approval_sensitive
// desks are refused at enqueue (GATE-4).
func (c *Config) AutoSwitchEligible(name string) bool {
	if name == "" {
		return false
	}
	a, err := c.Agent(name)
	if err != nil {
		return false
	}
	if a.ApprovalSensitive {
		return false
	}
	if c.IsXO(name) {
		return false
	}
	if c.CosAgent != "" && name == c.CosAgent {
		return false
	}
	return true
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

// hasSpanOfControl reports whether name coordinates subordinate agents (#460, #481).
// Two federation shapes coexist:
//   - Coordinator home: xo_agent=name lists execution desks (non-XO members) as subordinates.
//   - Desk home (supervisor-as-member): execution desk xo_agent lists coordinator XOs as
//     observers; span is detected when name appears on such a desk channel as supervisor.
//
// A coordinator listed only as supervision observer on another coordinator's home channel
// (e.g. cos on a project-XO channel) does not confer span — those members are IsXO.
func (c *Config) hasSpanOfControl(name string) bool {
	for _, ch := range c.Bindings() {
		if ch.XOAgent != name {
			continue
		}
		for _, m := range ch.Members {
			if m != name && !c.IsXO(m) {
				return true
			}
		}
	}
	for _, ch := range c.Bindings() {
		if ch.XOAgent == name || ch.XOAgent == "" {
			continue
		}
		// Primary / CoS home channels list members for fleet visibility — not span edges.
		if ch.XOAgent == c.XOAgent || ch.XOAgent == c.CosAgent {
			continue
		}
		if !c.channelIsSupervisorObserverHome(ch) {
			continue
		}
		for _, m := range ch.Members {
			if m == name {
				return true
			}
		}
	}
	return false
}

// channelIsSupervisorObserverHome reports the desk-home shape (#481): every non-self member
// is an XO supervision observer; the channel owner is the execution desk.
func (c *Config) channelIsSupervisorObserverHome(ch Channel) bool {
	hasObserver := false
	for _, m := range ch.Members {
		if m == ch.XOAgent {
			continue
		}
		hasObserver = true
		if !c.IsXO(m) {
			return false
		}
	}
	return hasObserver
}

// IsCoordinator reports whether name holds a coordinator role — the primary xo_agent,
// the chief-of-staff (cos_agent), or a binding xo_agent with span of control > 0
// (at least one channel member besides itself; #460). IsXO is broader (any channel
// owner); use IsCoordinator for delegation-nudge (#232) and coordinator doctrine.
func (c *Config) IsCoordinator(name string) bool {
	if name == "" {
		return false
	}
	if c.XOAgent != "" && name == c.XOAgent {
		return true
	}
	if c.CosAgent != "" && name == c.CosAgent {
		return true
	}
	return c.hasSpanOfControl(name)
}

// CoordinatorSet returns EVERY coordinator agent (each name for which IsCoordinator is true) —
// the primary XO, the CoS, and every binding XO with span of control — computed in a SINGLE
// pass. Callers that classify MANY agents (e.g. the dash rail's Fleet Command grouping) use
// this instead of IsCoordinator-per-agent, which re-scans the bindings on each call (O(n²)
// over a member list). The returned map is the caller's to keep.
func (c *Config) CoordinatorSet() map[string]bool {
	set := make(map[string]bool)
	if c.XOAgent != "" {
		set[c.XOAgent] = true
	}
	if c.CosAgent != "" {
		set[c.CosAgent] = true
	}
	for _, a := range c.Agents {
		if set[a.Name] {
			continue
		}
		if c.hasSpanOfControl(a.Name) {
			set[a.Name] = true
		}
	}
	return set
}

// HeartbeatEnabled reports whether the recursive desk heartbeat (#183) re-engages this agent
// when it settles Idle mid-task. The primary XO is excluded — it has its own clock (the daemon
// heartbeat), so heartbeating it would double-drive. Resolution order: an explicit per-agent
// Heartbeat flag wins; otherwise an approval-sensitive desk is OFF by default (the #184 carve-out);
// otherwise a general desk is ON by default (the directive is universal). A name that is not a
// roster agent is never heartbeated.
func (c *Config) HeartbeatEnabled(name string) bool {
	if name == "" || name == c.XOAgent {
		return false
	}
	a, err := c.Agent(name)
	if err != nil {
		return false
	}
	if a.Heartbeat != nil {
		return *a.Heartbeat
	}
	return !a.ApprovalSensitive
}

// HeartbeatWarranted refines HeartbeatEnabled (#183, the HARD eligibility gate) into the #189
// per-recipient JUDGMENT: given the recipient's already-parsed backlog Status (INJECTED — this
// function does NO file I/O, keeping the roster filesystem-free), it reports whether a desk
// heartbeat is warranted RIGHT NOW. The HARD gate is checked FIRST and can NEVER be overridden by
// the judgment: an XO / approval-sensitive / explicitly opted-out / unknown agent returns false
// regardless of how much actionable work its backlog shows. This re-check is intentional
// defense-in-depth — the detector's own HeartbeatEnabled conjunct remains the PRIMARY hard gate;
// do NOT collapse the two (they guard the same invariant at two layers on purpose).
//
// For an ELIGIBLE recipient the warrant predicate is: warranted = !Found || len(Unblocked) > 0.
//   - len(Unblocked) > 0 ⇒ there is live actionable work ([in-flight]/[next], or a malformed item
//     the parser drives via its fail-safe) ⇒ warrant a beat.
//   - !Found ⇒ a present-but-sectionless (or absent-parse) backlog CANNOT prove there is no work,
//     so it fails toward WARRANTED (keep the desk moving — the silent-stall regression #183 fixed).
//   - The ONLY path to NOT-warranted is a cleanly-parsed Found backlog whose actionable set is
//     empty — i.e. the recipient has affirmatively recorded that everything is [done], in the
//     open-questions ledger ([blocked]/[needs-attention]), or in the authorizations ledger
//     ([awaiting-auth]). Suppression requires PROOF of no work, never its absence.
//
// The caller (the cmd watch wiring) supplies the per-recipient parsed Status, read OFF the detector
// lock; a recipient with no per-recipient backlog file is handled by the caller's missing-ledger
// fallback (always-warranted), NOT here.
func (c *Config) HeartbeatWarranted(name string, st backlog.Status) bool {
	if !c.HeartbeatEnabled(name) {
		return false // the HARD eligibility gate — never overridden by the judgment
	}
	return !st.Found || len(st.Unblocked) > 0
}

// ChannelForXO returns the Discord channel an XO owns, for tagging that XO's outbound
// ledger entry. When an XO hubs MULTIPLE channels, this returns its FIRST-listed binding —
// its primary/home channel — so list an XO's home channel first among its bindings. ok=false
// when no binding is owned by name (then the caller records an empty channel — the ledger
// renders it as "-"). For the legacy single-fleet form this is the synthesized binding's
// channel.
func (c *Config) ChannelForXO(name string) (string, bool) {
	for _, ch := range c.Bindings() {
		if ch.XOAgent == name {
			return ch.ChannelID, true
		}
	}
	return "", false
}

// ChannelForAgent resolves the channel to tag an agent's ledger entry with, whether the
// agent OWNS a channel (as its xo_agent) or is only a MEMBER of one. It prefers ownership
// (its home channel, via ChannelForXO), then falls back to the first channel that lists the
// agent in members[]. A pure desk in a flat topology owns no channel but is a member of its
// parent's — resolving that membership is what lets a desk-directed relay carry a real
// channel tag (else it renders "-" and loses the side-conversation grouping). ok=false only
// when the agent is neither an owner nor a member of any binding.
func (c *Config) ChannelForAgent(name string) (string, bool) {
	if ch, ok := c.ChannelForXO(name); ok {
		return ch, true
	}
	for _, ch := range c.Bindings() {
		for _, m := range ch.Members {
			if m == name {
				return ch.ChannelID, true
			}
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
