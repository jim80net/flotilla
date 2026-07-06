package watch

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
)

// shellDebounce is the number of CONSECUTIVE shell assessments required before a
// desk is treated as crashed (systems-review M2). Even though claude-code Assess
// now maps a transient pane-command read-error to StateUnknown (not StateShell),
// a desk genuinely mid-restart can momentarily present a real bare shell;
// requiring two consecutive StateShell reads suppresses that false alarm.
const shellDebounce = 2

// snapshotWriteFailThreshold is how many consecutive snapshot-write failures
// trip the LOUD persistence alert and the degrade-to-in-memory mode (H3).
const snapshotWriteFailThreshold = 3

// WakeKind labels why the detector is waking the XO, so the caller can compose
// the right prompt (the detector owns WHEN/WHY to wake; the caller owns the
// prompt text, which references deployment-specific file paths).
type WakeKind int

const (
	// WakeMaterial: an external desk transition or an external signal-file change
	// (or the cold-start reassess) — the reasons name what changed.
	WakeMaterial WakeKind = iota
	// WakeContinuation: the XO finished a turn and may have a next authorized
	// step; the prompt carries the narrow-answer discipline (advance or reply
	// idle, never manufacture work).
	WakeContinuation
	// WakePing: a liveness-only ping (ack and do nothing else) — the max-quiet
	// safety net so a healthy idle XO keeps acking.
	WakePing
	// WakeBacklog: the goal-driven loop drives the top unblocked backlog item; the
	// reasons carry that item's raw line (the caller names it in the prompt). Emitted
	// instead of settling while the backlog gate reports unblocked work remains.
	WakeBacklog
	// WakeSynthesis: the visibility-synthesis (B2) tick — a synthesizing agent is OWED a
	// curated rollup of its subordinates' latest state (a desk finished below it, and a
	// subordinate's state changed since the last synthesis). It targets an ARBITRARY
	// synthesizing agent (a project XO for Tier 2, the meta-XO for Tier 3), NOT the
	// daemon's primary clock XO, so it is delivered through the parallel WakeAgent seam,
	// never the primary-XO Wake. The reasons name the subordinate(s) that changed.
	WakeSynthesis
	// NOTE: the recursive desk-heartbeat (#183) does NOT add a WakeKind. A desk beat is delivered
	// through the dedicated DetectorConfig.WakeDeskHeartbeat func(agent) seam → the cmd-side dispatch
	// (Kind:"detector", audit-suppressed), NOT through the kind-routed Wake/WakeAgent vehicles — so no
	// kind constant is needed (a dead one would imply a Wake(WakeDeskHeartbeat,…) path that doesn't exist).
)

// DetectorConfig wires a Detector. The collaborators are injected so the whole
// state machine is unit-testable without tmux, a clock, or the filesystem.
type DetectorConfig struct {
	XOAgent  string        // the XO's roster name (its own transitions feed self-continuation only)
	Desks    []string      // monitored agent names, INCLUDING the XO
	Interval time.Duration // detector tick cadence (drives the liveness windows)

	// Assess resolves a desk's current surface state (resolve pane + Driver.Assess);
	// an unresolvable pane SHOULD return StateUnknown (anti-flap, caught by ack age).
	Assess func(agent string) surface.State
	// RateLimitMaterial probes a desk for a material provider throttle (#204). ok=false
	// when the surface lacks RateLimitProbe or the pane is unresolvable. Invoked OFF d.mu
	// from runRateLimitProbes (never under tickLocked — pane capture is blocking tmux I/O).
	// Results fold into the NEXT tick's wake decision. The probe's 2-consecutive-read
	// discipline runs inside the driver.
	RateLimitMaterial func(agent string) (limited bool, scope surface.RateLimitScope, detail string, ok bool)
	// RateLimitReset clears a desk's rate-limit read streak when it leaves the probe
	// candidate states (Idle/Errored). Invoked OFF d.mu alongside the probe batch.
	RateLimitReset func(agent string)
	// RateLimitDispatch runs the per-tick rate-limit probe batch. Production wires it to
	// `go run()` (mirrors MirrorDispatch) so slow tmux reads cannot stall the tick loop.
	// Default nil ⇒ synchronous (deterministic for tests).
	RateLimitDispatch func(run func())
	// RateLimitAutoSwitchEligible gates detector-enqueued auto-switch (GATE-4 + coordination
	// desks). Nil ⇒ no auto-switch candidates are collected.
	RateLimitAutoSwitchEligible func(agent string) bool
	// RateLimitAutoSwitch is invoked OFF d.mu with material throttle candidates. It MUST
	// exec `flotilla switch <agent> --auto` over a side-channel argv array; status goes to
	// logs only. Nil ⇒ byte-inert.
	RateLimitAutoSwitch func(candidates []RateLimitAutoSwitchCandidate)
	// RateLimitAutoSwitchDispatch runs the auto-switch callback. Production wires it to
	// `go run()` so cap/storm/recipe file I/O cannot stall the tick loop. Default nil ⇒ sync.
	RateLimitAutoSwitchDispatch func(run func())
	// SignalHash returns the OPTIONAL external signal file's content hash; ok=false
	// when no signal file is configured or it is absent/unreadable (treated as
	// unchanged — no wake-storm). This is NOT the XO's own state tracker: hashing the
	// tracker would self-wake the XO on its own writes (the heartbeat instructs the
	// XO to keep .flotilla-state.md current). External wake deltas (a desk/tool
	// dropping a signal) flow through here; unconfigured ⇒ inert (always ok=false).
	SignalHash func() (string, bool)
	// AckAge returns the wall-clock age of the XO's last liveness ack.
	AckAge func() time.Duration
	// Wake enqueues a primary-layer wake of the given kind with human-readable reasons; the
	// caller composes the prompt (and appends the ack instruction — L1).
	Wake func(kind WakeKind, reasons []string)
	// WakeLayer routes a wake to a specific coordinator layer (#438 stackable_wakes). Default
	// nil ⇒ inert; when StackableWakes is false the detector never calls this seam.
	WakeLayer func(owner string, kind WakeKind, reasons []string)
	// StackableWakes opts into per-layer material routing (#438). Default false ⇒ Wake only.
	StackableWakes bool
	// OwningXO resolves the coordinator that owns a desk for stackable routing. Default nil ⇒
	// inert even when StackableWakes is true (fails safe to primary-only Wake).
	OwningXO func(agent string) string
	// MirrorOnFinish is the per-desk visibility side-effect: invoked once for each NON-XO desk that
	// completed a unit of work this tick (a confirmed Working→Idle transition). The caller mirrors
	// that desk's turn-final output to its home Discord channel. Like the wake/rotate side-effects it
	// runs in runTail, OUTSIDE d.mu, so a slow transcript read or Discord post can never stall the
	// tick loop. It is OBSERVE-ONLY and BEST-EFFORT — the closure must never let a mirror failure
	// affect the tick or delivery. Default nil ⇒ inert (no mirror; behavior byte-identical to before
	// this change). The primary clock XO is deliberately excluded — it uses CoordinatorMirrorOnFinish.
	MirrorOnFinish func(agent string)
	// CoordinatorMirrorOnFinish is the primary-clock-XO visibility side-effect: invoked once when
	// XOAgent completes Working→Idle (the coordinator finish hook). Project-XOs and execution desks
	// use MirrorOnFinish instead. Same off-mutex / best-effort discipline. Default nil ⇒ inert.
	CoordinatorMirrorOnFinish func(agent string)
	// AdjutantFor reports the adjutant agent for a coordinator layer, or "" when none.
	// Used with AdjutantSeamOnFinish for per-owner buffer drain (#438 stackable_wakes).
	// nil ⇒ only the primary XO seam fires (legacy).
	AdjutantFor func(owner string) string
	// AdjutantSeamOnFinish fires when a coordinator leader's seam opens — drain that
	// layer's adjutant buffer (#439). owner is the coordinator layer (primary or project).
	// Default nil ⇒ inert.
	AdjutantSeamOnFinish func(owner string)
	// IdleHoldOnFinish is the idle-hold antipattern side-effect (#216): invoked once
	// for each NON-XO desk that completed a unit of work this tick (the same trigger
	// as MirrorOnFinish). The caller reads the desk's turn-final, runs the mechanical
	// detector, and injects a break prompt when consecutive strikes meet the threshold.
	// Like MirrorOnFinish it runs in runTail OUTSIDE d.mu; default nil ⇒ inert.
	IdleHoldOnFinish func(agent string)
	// IsCoordinator reports whether an agent holds a coordinator role (any XO or CoS).
	// Used by the delegation-nudge side-effect (#232). Default nil ⇒ no agent is a
	// coordinator (the nudge never fires).
	IsCoordinator func(name string) bool
	// DelegationNudgeOnFinish is the coordinator IC-ing side-effect (#232): invoked
	// once for each coordinator that completed a unit of work this tick (Working→Idle),
	// INCLUDING the primary clock XO and the CoS (unlike MirrorOnFinish, which excludes
	// only the primary XO). The caller reads the turn-final, detects inline build/ship
	// work without delegation, and injects a dispatch nudge when strikes meet threshold.
	// Like IdleHoldOnFinish it runs in runTail OUTSIDE d.mu; default nil ⇒ inert.
	DelegationNudgeOnFinish func(agent string)
	// StrandedHandoffOnFinish is the dropped gate-report side-effect (#216 extension):
	// invoked once for each NON-XO desk that completed a unit of work this tick (same
	// trigger as MirrorOnFinish). Detects turn-finals that settle gate work without
	// reporting to the gate-holder and injects a break prompt. Default nil ⇒ inert.
	StrandedHandoffOnFinish func(agent string)
	// DecisionBriefOnTick is the auto decision-brief side-effect (#349 item D): invoked
	// once per detector tick (off d.mu, optionally async via MirrorDispatch). The caller
	// scans the goals file for operator-gated items missing a brief and dispatches the
	// owning desk. Default nil ⇒ inert.
	DecisionBriefOnTick func()
	// ScheduleOnTick is the daemon-native wall-clock scheduler side-effect (#413):
	// invoked once per detector tick (off d.mu, optionally async via MirrorDispatch).
	// The caller evaluates roster schedules[] and enqueues due dispatches. Default nil ⇒
	// inert. Production also runs a standalone poll loop for sub-interval accuracy.
	ScheduleOnTick func()
	// OutboxSweepOnTick sweeps per-sender durable outboxes for pending inter-agent sends
	// (#475). Invoked once per detector tick (off d.mu). Default nil ⇒ inert.
	OutboxSweepOnTick func()
	// MirrorDispatch runs a tick's batch of per-desk mirrors. Production wires it to `go run()` so the
	// mirror I/O (a transcript read + Discord posts) is FULLY DECOUPLED from the detector loop — even
	// off-mutex, inline I/O on the tick goroutine could delay the next tick (and thus liveness eval)
	// when Discord is slow (systems-review / open-code-review / cubic all flagged this). Default nil ⇒
	// run the batch SYNCHRONOUSLY (deterministic for tests). The batch is panic-isolated per desk
	// regardless (see mirrorOne), so an async run can never crash the daemon.
	MirrorDispatch func(run func())

	// --- visibility synthesis (B2) — ALL inert by default (no synthesis wake ever fires when the
	// feature is unconfigured; behavior byte-identical to before this change). ---

	// WakeAgent is the PARALLEL agent-targeted wake seam for synthesis. The shipped primary-XO Wake
	// is left BYTE-IDENTICAL (re-trio P2-1) — widening Wake would break every existing call site;
	// a parallel seam keeps the inert-when-absent story intact. WakeSynthesis is delivered through
	// HERE to an arbitrary synthesizing agent (a project XO / the meta-XO), never through Wake.
	// Like the other side-effects it runs in runTail, OUTSIDE d.mu (its confirmed delivery acquires
	// the pane-txn lock). Default nil ⇒ inert: no synthesis wake is ever delivered.
	WakeAgent func(agent string, kind WakeKind, reasons []string)
	// SynthParents resolves the synthesizing PARENT(s) OWED a rollup when a desk finishes — the
	// roster's AgentsAbove(agent) (members of the non-fleet-command channels the agent owns, minus
	// self). A boat in two channels marks BOTH owed; a project-XO finishing marks the meta-XO owed
	// (the Tier-3 recursion). Default nil ⇒ inert: a finishing desk marks NOBODY owed, so the
	// owed-set stays empty and no synthesis wake fires (byte-identical to before).
	SynthParents func(agent string) []string
	// SynthRead resolves a subordinate agent's latest turn-final text (ok=false ⇒ unreadable: pane
	// won't resolve / surface has no ResultReader). The detector consults it for the materiality
	// gate (did a subordinate's state change since the last synthesis). An unreadable subordinate is
	// EXCLUDED from the materiality hash for that wake (never recorded as empty — no flap). Default
	// nil ⇒ inert (the materiality gate sees no readable subordinates, so it never fires a wake).
	SynthRead func(agent string) (string, bool)
	// SynthEveryTicks is the digest sub-cadence (debounce-up): WakeSynthesis fires for an owed agent
	// AT MOST once per this many ticks. It derives at the call site from heartbeat_interval (a small
	// multiple). A burst of finishes coalesces to one wake; an idle fleet (nothing owed) fires
	// nothing. NewDetector defaults it to a sensible floor when < 1.
	SynthEveryTicks int
	// SynthPersist / SynthLoad are the disk-sidecar I/O for the DURABLE last-seen materiality state,
	// injected for testability (mirroring how Persist is injected). The sidecar survives BOTH
	// context rotation AND daemon restart — an in-memory-only snapshot re-posts everything as "new"
	// on the first post-restart wake (a restart-storm). NewDetectorWithSynthSidecar defaults them to
	// SynthState.Save / LoadSynthState over the sidecar path. A missing/corrupt sidecar fails SAFE
	// toward "all changed" (synthesize once), never silent-never-fire.
	SynthPersist func(SynthState) error
	SynthLoad    func() (SynthState, bool)
	// Rotate rotates the XO context via surface.RotateContext (claude → /clear).
	Rotate func() error
	// RotatePolicy gates idle-edge rotation requests from continueXO (#467). Zero
	// value (or always) preserves legacy behavior; never/handoff suppress bare /clear.
	RotatePolicy roster.XORotatePolicy
	// Awaiting reports whether the awaiting-operator veto marker is present (gates
	// the rotate only).
	Awaiting func() bool
	// SettleConsume reports+consumes the XO's settle marker (the fast idle signal).
	SettleConsume func() bool
	// DeskSettleConsume reports+consumes a DESK's per-agent settle marker (#183 recursive
	// heartbeat): a desk the heartbeat re-engaged signals "nothing to advance" by touching its
	// own marker. nil ⇒ the per-agent fast settle is unconfigured (a desk settles only via the
	// per-agent cap backstop). Like SettleConsume, it must fail toward NOT-settled.
	DeskSettleConsume func(agent string) bool

	// --- recursive desk-heartbeat (#183) — ALL inert by default. With HeartbeatEnabled nil the
	// per-desk tickLocked block is skipped entirely, so the detector is BYTE-IDENTICAL to before
	// this change (the regression-lock invariant; G4 TDD case 11). ---

	// HeartbeatEnabled reports whether a monitored desk is eligible for the recursive heartbeat
	// (the roster opt-OUT resolver: default-ON, approval-sensitive/XO default-OFF — see
	// roster.Config.HeartbeatEnabled). nil ⇒ the WHOLE desk-heartbeat path is OFF (no beat, no cap,
	// no escalation ever fires; the detector behaves exactly as before #183). A desk for which this
	// returns false is never beaten — the primary XO (which keeps its own clock) and the
	// approval-sensitive desks are excluded here.
	HeartbeatEnabled func(agent string) bool
	// HeartbeatWarranted is the #189 per-recipient JUDGMENT: it reports whether there is OUTSTANDING
	// ACTIONABLE WORK for the agent right now. It is the cmd-wiring's FILE I/O (os.ReadFile +
	// backlog.Parse of <dir>/flotilla-<agent>-backlog.md), and is therefore invoked ONLY in the
	// PHASE-1 snapshot (deskWarrantSnapshot), OFF d.mu, BEFORE tickLocked acquires the lock. The
	// under-lock phase-2 decision (deskHeartbeatLocked) consults ONLY the resulting pure map[agent]bool
	// warrant as the LAST conjunct (after the HeartbeatEnabled HARD gate, the settle/stop checks, and
	// the cadence) — so NO backlog file I/O ever runs under d.mu (the detector's load-bearing off-mutex
	// invariant, honored by synthesis + the mirror; the same two-phase split). The judgment can ONLY
	// suppress a beat the desk would otherwise receive — it can never resurrect a beat to an ineligible/
	// settled/stopped/non-idle desk (it is the LAST gate). A not-warranted desk is treated like a
	// settled desk for that tick: no beat, no cap accrual, no cadence accrual (it is legitimately idle,
	// not wedged). nil ⇒ the phase-1 snapshot is nil ⇒ every eligible desk defaults to warranted ⇒ the
	// trigger is IDENTICAL to the pre-judgment recursive heartbeat (#183 byte-inert on this axis —
	// NewDetector defaults it to a func returning true).
	HeartbeatWarranted func(agent string) bool
	// WakeDeskHeartbeat delivers ONE desk-continuation beat to a desk that is OWED one (idle past
	// its cadence, not settled, not stopped). Called OFF d.mu in runDeskHeartbeats (its confirmed
	// delivery acquires the pane-txn lock — a bounded wait that must never be held under d.mu), like
	// the synthesis wake. Fire-and-forget: a beat to a busy/input-blocked pane is silently dropped by
	// the injector (a Kind:"detector" job never escalates), so the cap is progress-observable, never
	// keyed on a delivery outcome. nil ⇒ no beat is ever delivered (inert).
	WakeDeskHeartbeat func(agent string)
	// DeskEscalate raises the LOUD cap-escalation for a wedged desk (idle + un-progressing across
	// capN beats) to its owning XO (the channel the desk is a member of → that channel's XO, falling
	// back to the primary XO). Called OFF d.mu in runDeskHeartbeats. Fires ONCE on the ==capN edge,
	// then the desk is stopped until re-armed (AgentWake). nil ⇒ no escalation is ever delivered.
	DeskEscalate func(agent string)
	// DeskHeartbeatEveryTicks is the per-desk cadence: a desk is owed a beat after this many
	// CONSECUTIVE idle ticks (the heartbeat interval, in ticks). NewDetector defaults < 1 to 1 (every
	// idle tick), matching the design's "cadence = the heartbeat interval" (the daemon's tick IS the
	// interval).
	DeskHeartbeatEveryTicks int
	// DeskHeartbeatCap is N — the consecutive no-progress beat bound before a desk is escalated +
	// stopped (the §5.3 decision: N=3). NewDetector defaults < 1 to 3.
	DeskHeartbeatCap int

	// Alert raises a LOUD operator alert (down-alert path) — liveness + the H3
	// persistence failure.
	Alert func(string)
	// Persist writes the snapshot durably (atomic). Injected for tests; production
	// wires Snapshot.Save.
	Persist func(Snapshot) error
	// Now is the clock (tests pin it); defaults to time.Now.
	Now func() time.Time

	MaxMissedAcks       int    // K — the wedge-alert window base, in intervals
	MaxQuietIntervals   int    // N override — ping cadence; 0 ⇒ mode default
	LivenessPingMode    string // "none" (default, $0-idle) | "interval" | "consecutive"
	MaxSelfContinuation int    // H1 hard cap on consecutive no-external-change continuations

	// BacklogGate reports the fleet backlog's settle-relevant status (the goal-driven loop). A
	// non-empty Unblocked queue VETOES settle — overriding both the XO's idle self-signal and the
	// self-continuation cap — and drives the top unblocked item (WakeBacklog). NewDetector defaults
	// it to an inert closure (zero Status ⇒ no unblocked items ⇒ today's behavior unchanged), so a
	// deployment without --backlog-file is byte-identical to before this change.
	BacklogGate func() backlog.Status
	// BacklogStuckCap is the per-item drive bound: an unblocked item driven this many times without
	// leaving the queue is escalated ONCE and deprioritized (the loop drives other items rather than
	// spinning on it). NewDetector defaults it when < 1.
	BacklogStuckCap int

	// PokeDebounce coalesces event-driven TurnEndPoller pokes (default DefaultPokeDebounce).
	PokeDebounce time.Duration

	// ReferenceInterval anchors wall-time sub-cadences (roster heartbeat_interval / ceiling).
	// When zero, NewDetector defaults it to Interval.
	ReferenceInterval time.Duration
	// SynthEveryPeriod is the synthesis digest wall-time cadence (default 3 × ReferenceInterval).
	SynthEveryPeriod time.Duration
	// DeskHeartbeatPeriod is the per-desk heartbeat wall-time cadence (default ReferenceInterval).
	DeskHeartbeatPeriod time.Duration
	// RateLimitProbePeriod gates rate-limit probe batches (default ReferenceInterval).
	RateLimitProbePeriod time.Duration

	// Activity ingests tick assess snapshots and turn-end/operator signals OFF d.mu
	// for adaptive interval policy (PR 3). Nil ⇒ byte-inert.
	Activity ActivityTracker

	// AdaptiveInterval varies the live tick period from Activity output (PR 3).
	// Nil ⇒ fixed cfg.Interval (byte-inert). Ticker mutations occur only in loop().
	AdaptiveInterval AdaptiveInterval
}

// Detector is the v2 heartbeat: a deterministic, no-LLM tick that wakes the XO
// ONLY on a material change, self-continues it without a blind timer, rotates
// its context between handlings, and detects liveness on wall-clock ack age. All
// mutable state is guarded by mu so the per-interval Tick and the operator-wake
// path (relay goroutine) are race-free single-writers (systems-review M3).
type Detector struct {
	mu sync.Mutex

	cfg         DetectorConfig
	pingPeriod  time.Duration // wall-time quiet-ping period; 0 disables pings
	alertWindow time.Duration // wall-time AckAge wedge window

	snap               Snapshot
	cold               bool
	quietFSM           quietPingFSM
	selfCont           int
	lastContinuationAt time.Time
	lastDriveAt        map[string]time.Time
	driveCount         map[string]int // per-item backlog drive counts (the goal-driven loop's stuck handling)
	shellStreak        map[string]int
	writeFails         int
	degraded           bool
	wd                 *Watchdog

	// --- visibility synthesis (B2) state, guarded by the same d.mu single-writer invariant ---
	// These in-memory maps need NO prune for roster-removed agents: their keys are only ever the
	// AgentsAbove(name) parents of monitored desks, and AgentsAbove ⊆ Agents ⊆ Desks (members are
	// validated in-roster at Load), so a key for a non-roster agent can never be inserted. A roster
	// change requires a daemon restart (the roster loads once at start), which reconstructs them
	// empty — so they are bounded by roster size and self-clear on the restart any change requires.
	// (The DURABLE sidecar IS pruned at load — see NewDetector — because it persists across restarts.)
	synthOwed        map[string]bool      // synthesizing agent → has a rollup owed (a desk finished below it)
	synthSinceFireAt map[string]time.Time // synthesizing agent → wall time of last WakeSynthesis fire
	synthState       SynthState           // the DURABLE last-seen materiality snapshot (loaded from the sidecar)

	// --- recursive desk-heartbeat (#183) per-agent state, guarded by the same d.mu single-writer
	// invariant. Keyed only by monitored desks (⊆ Desks, validated in-roster), so — like the synth
	// maps — they need no prune: they're bounded by roster size and self-clear on the restart any
	// roster change requires. All in-memory (no durable snapshot): a restart cold-starts a desk's
	// heartbeat cadence, which is conservative (one fresh re-engagement, never a missed escalation).
	deskSettled          map[string]bool      // desk signaled idle (per-agent marker consumed) → suppress until re-armed
	deskBeatEligibleAt   map[string]time.Time // desk → last heartbeat fire (cadence anchor)
	rateLimitLastProbeAt map[string]time.Time
	deskNoProgress       map[string]int  // consecutive heartbeats with no intervening progress (the cap counter)
	deskStopped          map[string]bool // capped + escalated → stop heartbeating until re-armed
	deskProgressed       map[string]bool // desk went Working since its last heartbeat → resets the cap

	// rateLimitActive suppresses repeat wakes for the same throttle episode (#204).
	rateLimitActive map[string]bool
	// rateLimitPending holds the PREVIOUS tick's off-mutex probe results (folded into
	// the current tick's wake decision). Guarded by rateLimitProbeMu.
	rateLimitPending map[string]rateLimitProbeResult
	rateLimitProbeMu sync.Mutex
	// autoSwitchFlight dedupes one-in-flight auto-switch per desk (off d.mu).
	autoSwitchFlight AutoSwitchFlight

	stop         chan struct{}
	done         chan struct{}
	pokeCh       chan struct{}
	intervalCh   chan time.Duration // buffered(1); loop goroutine owns ticker resets
	pokeDebounce time.Duration
	startOnce    sync.Once
	stopOnce     sync.Once
}

// synthEveryTicksDefault is the digest sub-cadence floor (in ticks) when the caller does not set
// SynthEveryTicks. It bounds the rate of synthesis wakes (debounce-up); the skill bounds the
// content. The call site derives the deployment value from heartbeat_interval (a small multiple).
const synthEveryTicksDefault = 3

// NewDetector builds a detector from config, loading any persisted snapshot (a
// missing/corrupt one cold-starts → one conservative wake on the first tick). The synthesis
// materiality sidecar defaults to inert (no durable last-seen state, no synthesis wake);
// production uses NewDetectorWithSynthSidecar to wire a durable sidecar path.
func NewDetector(cfg DetectorConfig, snapPath string) *Detector {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Persist == nil {
		cfg.Persist = func(s Snapshot) error { return s.Save(snapPath) }
	}
	if cfg.SignalHash == nil {
		// No external signal file configured ⇒ inert (never a wake from this source).
		cfg.SignalHash = func() (string, bool) { return "", false }
	}
	if cfg.MaxSelfContinuation < 1 {
		cfg.MaxSelfContinuation = 3
	}
	if cfg.BacklogGate == nil {
		// No backlog configured ⇒ inert: the zero Status has no unblocked items, so continueXO's
		// gate is never triggered and behavior is byte-identical to before the goal-driven loop.
		cfg.BacklogGate = func() backlog.Status { return backlog.Status{} }
	}
	if cfg.BacklogStuckCap < 1 {
		cfg.BacklogStuckCap = 5
	}
	if cfg.SynthEveryTicks < 1 {
		cfg.SynthEveryTicks = synthEveryTicksDefault
	}
	if cfg.ReferenceInterval <= 0 {
		cfg.ReferenceInterval = cfg.Interval
	}
	if cfg.SynthEveryPeriod <= 0 {
		cfg.SynthEveryPeriod = time.Duration(cfg.SynthEveryTicks) * cfg.ReferenceInterval
	}
	if cfg.DeskHeartbeatPeriod <= 0 {
		ticks := cfg.DeskHeartbeatEveryTicks
		if ticks < 1 {
			ticks = 1
		}
		cfg.DeskHeartbeatPeriod = time.Duration(ticks) * cfg.ReferenceInterval
	}
	if cfg.RateLimitProbePeriod <= 0 {
		cfg.RateLimitProbePeriod = cfg.ReferenceInterval
	}
	// Recursive desk-heartbeat (#183) cadence/cap defaults. These are inert unless HeartbeatEnabled
	// is also wired (the per-desk tickLocked block is skipped when HeartbeatEnabled is nil), so
	// defaulting them here never changes behavior for a pre-#183 deployment.
	if cfg.DeskHeartbeatEveryTicks < 1 {
		cfg.DeskHeartbeatEveryTicks = 1 // cadence = the heartbeat interval (the tick IS the interval)
	}
	if cfg.DeskHeartbeatCap < 1 {
		cfg.DeskHeartbeatCap = 3 // §5.3: escalate after 3 consecutive no-progress beats
	}
	if cfg.HeartbeatWarranted == nil {
		// #189: an unwired judgment defaults to ALWAYS-WARRANTED, so the desk-heartbeat trigger is
		// byte-identical to the pre-judgment recursive heartbeat (#183). A deployment that does not
		// wire per-recipient backlogs behaves exactly as #183 (the regression-lock on this axis).
		cfg.HeartbeatWarranted = func(string) bool { return true }
	}
	if cfg.SynthParents == nil {
		// No synthesis routing configured ⇒ a finishing desk marks NOBODY owed, so the owed-set
		// stays empty and no synthesis wake ever fires (byte-identical to before this change).
		cfg.SynthParents = func(string) []string { return nil }
	}
	if cfg.SynthLoad == nil {
		// No durable sidecar wired ⇒ no last-seen state. With SynthLoad inert, the materiality gate
		// has no persisted history; combined with the default-nil SynthRead/WakeAgent the synthesis
		// path is fully inert. (Production wires a sidecar via NewDetectorWithSynthSidecar.)
		cfg.SynthLoad = func() (SynthState, bool) { return SynthState{}, false }
	}
	if cfg.SynthPersist == nil {
		cfg.SynthPersist = func(SynthState) error { return nil }
	}
	pingPeriod, alertWindow := livenessParamsWall(cfg.LivenessPingMode, cfg.MaxMissedAcks, cfg.ReferenceInterval, cfg.MaxQuietIntervals)

	snap, ok := LoadSnapshot(snapPath)
	synthState, _ := cfg.SynthLoad() // a missing/corrupt sidecar (ok=false) fails safe to empty ⇒ all-changed
	if synthState.LastSeen == nil {
		synthState.LastSeen = map[string]map[string]string{}
	}
	// Prune stale synthesizer keys from the loaded sidecar: a synthesizing agent REMOVED from the
	// roster would otherwise leave a permanent entry that accretes across roster churn (STORM P3). The
	// valid synthesizers are a subset of the monitored Desks (an XO is monitored); a key not in Desks
	// is dead. Inner subordinate keys self-prune (each fire overwrites LastSeen[agent] wholesale).
	if len(synthState.LastSeen) > 0 && len(cfg.Desks) > 0 {
		valid := make(map[string]bool, len(cfg.Desks))
		for _, name := range cfg.Desks {
			valid[name] = true
		}
		for agent := range synthState.LastSeen {
			if !valid[agent] {
				delete(synthState.LastSeen, agent)
			}
		}
	}
	d := &Detector{
		cfg:                  cfg,
		pingPeriod:           pingPeriod,
		alertWindow:          alertWindow,
		quietFSM:             quietPingFSM{pingPeriod: pingPeriod},
		snap:                 snap,
		cold:                 !ok,
		driveCount:           map[string]int{},
		lastDriveAt:          map[string]time.Time{},
		shellStreak:          map[string]int{},
		synthOwed:            map[string]bool{},
		synthSinceFireAt:     map[string]time.Time{},
		synthState:           synthState,
		deskSettled:          map[string]bool{},
		deskBeatEligibleAt:   map[string]time.Time{},
		rateLimitLastProbeAt: map[string]time.Time{},
		deskNoProgress:       map[string]int{},
		deskStopped:          map[string]bool{},
		deskProgressed:       map[string]bool{},
		rateLimitActive:      map[string]bool{},
		rateLimitPending:     map[string]rateLimitProbeResult{},
		// The detector computes the staleness threshold itself (age > alertWindow),
		// so the watchdog only needs to trip on the first stale/crash
		// signal and debounce — maxMissed=1.
		wd:         NewWatchdog(1, cfg.Alert),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		pokeCh:     make(chan struct{}, 1),
		intervalCh: make(chan time.Duration, 1),
	}
	if cfg.PokeDebounce <= 0 {
		d.pokeDebounce = DefaultPokeDebounce
	} else {
		d.pokeDebounce = cfg.PokeDebounce
	}
	return d
}

// NewDetectorWithSynthSidecar is NewDetector with the DURABLE visibility-synthesis materiality
// sidecar wired to a disk path (alongside the detector's existing snapshot). The sidecar holds the
// per-synthesizing-agent last-seen hash of each subordinate's latest turn text, so the materiality
// gate survives BOTH context rotation AND a daemon restart (no synthesis restart-storm). A
// missing/corrupt sidecar fails SAFE toward "all changed" (synthesize once). Production calls this;
// tests that exercise synthesis pass an explicit sidecar path.
func NewDetectorWithSynthSidecar(cfg DetectorConfig, snapPath, synthSidecarPath string) *Detector {
	if cfg.SynthLoad == nil {
		cfg.SynthLoad = func() (SynthState, bool) { return LoadSynthState(synthSidecarPath) }
	}
	if cfg.SynthPersist == nil {
		cfg.SynthPersist = func(s SynthState) error { return s.Save(synthSidecarPath) }
	}
	return NewDetector(cfg, snapPath)
}

// livenessParams resolves the ping cadence and the wedge-alert window (both in
// intervals) from the ping mode, K, and an optional N override. The invariant it
// preserves: the safety ping always fires at least one interval BEFORE the alert
// window, so a healthy idle XO re-acks before it is ever declared wedged. The
// three modes are the C1b tradeoff made switchable WITHOUT a rebuild:
//   - "interval": ping every K-1 intervals, alert at K — the strict window, at
//     the cost of a cheap ack-ping each idle interval.
//   - "consecutive": ping every K-1, alert after ~2 missed pings (K-1+2) — the
//     middle ground.
//   - "none" (default, the XO's option (ii)): NO per-interval ping; only a WIDE
//     safety ping at 2K, alert at 2K+1 — true $0-idle, accepting a ~2K idle-fleet
//     wedge window (a crash is still immediate; a wedged XO on an idle fleet has
//     nothing to miss).
func livenessParams(mode string, k, nOverride int) (pingEvery, alertIntervals int) {
	if k < 1 {
		k = 1
	}
	maxOf := func(a, b int) int {
		if a > b {
			return a
		}
		return b
	}
	// slack is how far the alert window sits beyond the ping cadence; the alert is
	// always at least one interval past the ping so a healthy idle XO re-acks
	// before it is ever declared wedged (even in the degenerate K=1 case).
	slack := 1
	switch mode {
	case "interval":
		pingEvery = maxOf(1, k-1)
	case "consecutive":
		pingEvery = maxOf(1, k-1)
		slack = 2
	default: // "none" / "" — the XO default: true $0-idle, wide safety ping
		pingEvery = 2 * k
	}
	if nOverride > 0 {
		pingEvery = nOverride
	}
	alertIntervals = maxOf(k, pingEvery+slack)
	return pingEvery, alertIntervals
}

// livenessParamsWall converts tick-count liveness params to wall-time periods anchored on ref.
func livenessParamsWall(mode string, k int, ref time.Duration, nOverrideTicks int) (pingPeriod, alertWindow time.Duration) {
	pingTicks, alertTicks := livenessParams(mode, k, nOverrideTicks)
	pingPeriod = time.Duration(pingTicks) * ref
	alertWindow = time.Duration(alertTicks) * ref
	return pingPeriod, alertWindow
}

func (d *Detector) now() time.Time {
	if d.cfg.Now != nil {
		return d.cfg.Now()
	}
	return time.Now()
}

// Start launches the detector loop (ticks every Interval). interval <= 0 parks
// it until Stop (disabled).
func (d *Detector) Start() { d.startOnce.Do(func() { go d.loop() }) }

// Stop ends the loop and waits for it to exit. Idempotent.
func (d *Detector) Stop() {
	d.stopOnce.Do(func() { close(d.stop) })
	<-d.done
}

// Poke requests a debounced extra Tick (event-driven desk turn-end). Coalesced: at
// most one signal is queued; repeated pokes within the debounce window reset the timer.
func (d *Detector) Poke() {
	select {
	case d.pokeCh <- struct{}{}:
	default:
	}
}

func (d *Detector) loop() {
	defer close(d.done)
	if d.cfg.Interval <= 0 {
		<-d.stop
		return
	}
	interval := d.positiveTickInterval(d.currentInterval())
	t := time.NewTicker(interval)
	defer t.Stop()
	var debounce *time.Timer
	var debounceC <-chan time.Time
	resetDebounce := func() {
		if debounce != nil {
			if !debounce.Stop() {
				select {
				case <-debounce.C:
				default:
				}
			}
		}
		debounce = time.NewTimer(d.pokeDebounce)
		debounceC = debounce.C
	}
	resetTicker := func(newInterval time.Duration) {
		old := t
		drainTicker(old)
		old.Stop()
		t = time.NewTicker(d.positiveTickInterval(newInterval))
	}
	for {
		select {
		case <-d.stop:
			if debounce != nil {
				debounce.Stop()
			}
			return
		case newInterval := <-d.intervalCh:
			resetTicker(newInterval)
		case <-t.C:
			d.Tick()
			d.maybeQueueIntervalUpdate()
		case <-d.pokeCh:
			resetDebounce()
		case <-debounceC:
			debounceC = nil
			d.Tick()
			d.maybeQueueIntervalUpdate()
		}
	}
}

func (d *Detector) currentInterval() time.Duration {
	if d.cfg.AdaptiveInterval != nil {
		return d.cfg.AdaptiveInterval.Current()
	}
	return d.cfg.Interval
}

// positiveTickInterval guards time.NewTicker from non-positive adaptive output.
func (d *Detector) positiveTickInterval(iv time.Duration) time.Duration {
	if iv > 0 {
		return iv
	}
	if d.cfg.Interval > 0 {
		return d.cfg.Interval
	}
	return time.Millisecond
}

// maybeQueueIntervalUpdate reads activity + adaptive policy OFF d.mu and coalesces
// a ticker reset request to the loop goroutine (the sole ticker owner).
func (d *Detector) maybeQueueIntervalUpdate() {
	if d.cfg.AdaptiveInterval == nil || d.cfg.Activity == nil {
		return
	}
	snap := d.cfg.Activity.Snapshot(d.now())
	interval, changed := d.cfg.AdaptiveInterval.Update(snap)
	if !changed {
		return
	}
	interval = d.positiveTickInterval(interval)
	select {
	case d.intervalCh <- interval:
	default:
		select {
		case <-d.intervalCh:
		default:
		}
		d.intervalCh <- interval
	}
}

// OperatorWake is called by the relay when an operator message is delivered to
// the XO: it clears the settled state and resets the self-continuation and quiet
// counters so a settled XO re-engages and the cap restarts (fork B #2 / H1). It
// holds the same mutex as Tick so the two never race (M3).
func (d *Detector) OperatorWake() {
	d.mu.Lock()
	d.snap.XOSettled = false
	d.selfCont = 0
	d.quietFSM.OnWake()
	d.lastContinuationAt = time.Time{}
	for item := range d.lastDriveAt {
		delete(d.lastDriveAt, item)
	}
	// Clear per-item backlog drive counts: a fresh operator directive re-engages the loop and must
	// not inherit a stale stuck-streak (which would wrongly fire a stuck alert / deprioritize an
	// item on the next wake).
	for item := range d.driveCount {
		delete(d.driveCount, item)
	}
	// Drop any settle signal the XO may have just dropped, so it cannot re-settle
	// the XO on the next tick after the operator has re-engaged it.
	if d.cfg.SettleConsume != nil {
		_ = d.cfg.SettleConsume()
	}
	d.mu.Unlock()
	d.notifyOperatorActivity()
}

// AgentWake is the per-agent analogue of OperatorWake (#183 recursive desk heartbeat): the relay
// calls it when an operator message is delivered to a DESK (or a federated sub-XO), re-arming that
// agent's heartbeat. It clears the desk's settled + stopped state and resets its cadence and
// no-progress (cap) counters so a re-engaged desk is heartbeated again from a clean slate, and drops
// any settle signal the desk may have just dropped (so it cannot re-settle on the next tick after the
// operator re-engaged it). Only the named agent's state is touched — a desk's wake never re-arms
// another desk. Holds the same mutex as Tick so the two never race (the single-writer invariant).
func (d *Detector) AgentWake(agent string) {
	if agent == "" {
		return
	}
	d.mu.Lock()
	delete(d.deskSettled, agent)
	delete(d.deskStopped, agent)
	delete(d.deskBeatEligibleAt, agent)
	delete(d.deskNoProgress, agent)
	delete(d.deskProgressed, agent)
	if d.cfg.DeskSettleConsume != nil {
		_ = d.cfg.DeskSettleConsume(agent)
	}
	d.mu.Unlock()
	d.notifyOperatorActivity()
}

func (d *Detector) notifyOperatorActivity() {
	if d.cfg.Activity == nil {
		return
	}
	d.cfg.Activity.OnOperatorActivity(d.now())
}

// ingestActivity records tick assess output and tick-diff turn-ends OFF d.mu.
func (d *Detector) ingestActivity(pendingTurnEnds []string) {
	if d.cfg.Activity == nil {
		return
	}
	d.mu.Lock()
	states := make(map[string]surface.State, len(d.snap.DeskStates))
	for k, v := range d.snap.DeskStates {
		states[k] = v
	}
	xoSettled := d.snap.XOSettled
	xoAgent := d.cfg.XOAgent
	d.mu.Unlock()

	now := d.now()
	d.cfg.Activity.OnTickIngest(now, xoAgent, states, xoSettled)
	for _, agent := range pendingTurnEnds {
		d.cfg.Activity.OnTurnEnd(agent, now)
	}
}

// deferredWake records a wake decided UNDER d.mu but DELIVERED in the post-unlock tail
// (runTail), so the wake's confirmed delivery — which acquires the cross-process pane
// transaction lock on the Injector worker — is never decided while a bounded txn-lock wait
// could be held under d.mu.
type deferredWake struct {
	kind    WakeKind
	reasons []string
	owner   string // non-empty ⇒ WakeLayer target; empty ⇒ primary Wake
}

// synthEligible is one cadence-eligible owed synthesizing agent, decided UNDER d.mu in
// synthEligibleLocked and processed OFF d.mu in runSynthesis. It carries the read set and a SNAPSHOT
// of the agent's last-seen hashes so the blocking materiality read (tmux + transcript I/O) runs in
// the tail, NEVER under the detector mutex (the P1 fix — see runSynthesis).
type synthEligible struct {
	agent    string
	readSet  []string
	lastSeen map[string]string // a snapshot, compared off-mutex; the live state is committed in runSynthesis
}

// rateLimitProbeResult is one desk's off-mutex probe outcome, folded into the NEXT tick.
type rateLimitProbeResult struct {
	limited bool
	scope   surface.RateLimitScope
	ok      bool
}

// rateLimitWork is the per-tick rate-limit side-effect plan decided UNDER d.mu: which
// desks to probe OFF mutex this tick, and which streaks to reset (left probed states).
type rateLimitWork struct {
	probe []string
	reset []string
}

// Tick runs one detector cycle. The state machine (snapshot → liveness → diff → wake-or-sleep
// → persist) runs UNDER d.mu in tickLocked; the pane-touching side effects it decides (the XO
// context rotate and the wake deliveries) run AFTER the mutex is released, in runTail. This
// split is load-bearing: both side effects acquire the cross-process pane TRANSACTION lock
// (the rotate directly; a wake via the Injector's confirmed delivery), whose acquire is a
// BOUNDED wait that an external process (the dash, a CLI send) can now make us wait out. Doing
// that wait while holding d.mu would let an external delivery stall the detector's tick loop and
// block OperatorWake; running it in the tail keeps d.mu held only for the lock-free state logic.
func (d *Detector) Tick() {
	// #189 PHASE 1 (OFF d.mu): read + parse each eligible desk's per-recipient backlog and snapshot a
	// PURE map[agent]bool warrant. The backlog read is FILE I/O and MUST NOT run under d.mu (the
	// detector's load-bearing off-mutex invariant — the same one synthesis + the mirror honor); doing it
	// here, before tickLocked acquires the lock, means the under-lock phase-2 decision (deskHeartbeatLocked)
	// consults only an already-computed boolean and never touches the filesystem. Inert (nil) when the
	// feature is off (HeartbeatEnabled nil) or the warrant seam is unwired.
	warrant := d.deskWarrantSnapshot()
	pendingRotate, pendingWakes, pendingMirrors, pendingCoordinatorMirrors, pendingDelegation, pendingAdjutantSeams, pendingSynth, pendingDeskBeats, pendingDeskEscalations, pendingRateLimit, pendingAutoSwitch, pendingTurnEnds := d.tickLocked(warrant)
	d.ingestActivity(pendingTurnEnds)
	d.runTail(pendingRotate, pendingWakes, pendingMirrors, pendingCoordinatorMirrors, pendingDelegation, pendingAdjutantSeams)
	d.runAutoSwitch(pendingAutoSwitch)
	d.runRateLimitProbes(pendingRateLimit)
	// Visibility-synthesis (B2) runs AFTER runTail and OFF d.mu: its materiality read is BLOCKING
	// tmux + transcript I/O that must NEVER execute under the detector mutex (it would stall the tick
	// loop and block OperatorWake — the relay goroutine — exactly as the mirror path is kept off-mutex).
	// It commits last-seen state under a short re-lock, so it precedes the durable persist below.
	d.runSynthesis(pendingSynth)
	// Recursive desk-heartbeat (#183) delivery runs AFTER runSynthesis and OFF d.mu: each beat's
	// confirmed delivery and each escalation's loud alert acquire the pane-txn lock / post over the
	// network — bounded waits that must never be held under the detector mutex (the same off-mutex
	// discipline runSynthesis follows). The DECISION already happened under d.mu in tickLocked; this
	// only delivers. Inert when the seams are nil.
	d.runDeskHeartbeats(pendingDeskBeats, pendingDeskEscalations)
	// Durably persist the new baseline ONLY AFTER the tail has enqueued the wakes — restoring the
	// at-least-once crash semantics the old in-line code had (enqueue-then-save). The in-memory
	// baseline is already committed under d.mu in tickLocked (so subsequent ticks don't re-wake);
	// deferring just the DURABLE write means a crash anywhere in save→tail leaves the on-disk
	// snapshot showing the PRE-tick baseline, so the restart re-detects the transition and re-wakes
	// rather than persisting "processed" while the wake was lost (cubic P1). The crash window now
	// matches main's (enqueued-but-not-yet-delivered + saved), not the full multi-second rotate.
	d.persist()
}

// runTail performs the tick's pane-touching side effects OUTSIDE d.mu. The ORDER is the
// invariant the old in-line, under-mutex call gave for free: the /clear rotate is a
// self-contained transaction that completes (acquires → clears → RELEASES the pane-txn lock)
// BEFORE the continuation wake is enqueued, so the Injector — which re-acquires the same txn
// lock for the delivery — always lands the continuation AFTER the rotate, never letting a
// trailing /clear wipe a freshly delivered continuation.
func (d *Detector) runTail(pendingRotate bool, wakes []deferredWake, mirrors, coordinatorMirrors []string, delegationNudges []string, pendingAdjutantSeams []string) {
	if pendingRotate && d.cfg.Rotate != nil {
		if err := d.cfg.Rotate(); err != nil && !errors.Is(err, surface.ErrRestartRequired) {
			log.Printf("flotilla watch: XO context rotate failed: %v (continuing without rotate)", err)
		}
	}
	// Adjutant seam enqueue runs AFTER rotate (same invariant as continuation wakes: a trailing
	// /clear must not wipe a freshly delivered brief) and BEFORE the other wake deliveries.
	if d.cfg.AdjutantSeamOnFinish != nil {
		for _, owner := range pendingAdjutantSeams {
			d.cfg.AdjutantSeamOnFinish(owner)
		}
	}
	for _, w := range wakes {
		if w.owner != "" && d.cfg.WakeLayer != nil {
			d.cfg.WakeLayer(w.owner, w.kind, w.reasons)
		} else if d.cfg.Wake != nil {
			d.cfg.Wake(w.kind, w.reasons)
		}
	}
	// Per-desk visibility mirror (a NON-XO desk finished a turn this tick). Run LAST in the tail and,
	// like the wakes, OUTSIDE d.mu — a slow transcript read or Discord post must never stall the tick
	// loop or block OperatorWake. The closure is observe-only + best-effort (it absorbs its own
	// failures); the detector only fires the trigger.
	if len(mirrors) > 0 && (d.cfg.MirrorOnFinish != nil || d.cfg.IdleHoldOnFinish != nil || d.cfg.StrandedHandoffOnFinish != nil) {
		run := func() {
			for _, agent := range mirrors {
				if d.cfg.MirrorOnFinish != nil {
					d.mirrorOne(agent)
				}
				if d.cfg.IdleHoldOnFinish != nil {
					d.idleHoldOne(agent)
				}
				if d.cfg.StrandedHandoffOnFinish != nil {
					d.strandedHandoffOne(agent)
				}
			}
		}
		if d.cfg.MirrorDispatch != nil {
			d.cfg.MirrorDispatch(run) // production: `go run()` — decouple the mirror I/O from the tick loop
		} else {
			run() // default: synchronous (deterministic for tests)
		}
	}
	if len(coordinatorMirrors) > 0 && d.cfg.CoordinatorMirrorOnFinish != nil {
		run := func() {
			for _, agent := range coordinatorMirrors {
				d.coordinatorMirrorOne(agent)
			}
		}
		if d.cfg.MirrorDispatch != nil {
			d.cfg.MirrorDispatch(run)
		} else {
			run()
		}
	}
	if len(delegationNudges) > 0 && d.cfg.DelegationNudgeOnFinish != nil {
		run := func() {
			for _, agent := range delegationNudges {
				d.delegationNudgeOne(agent)
			}
		}
		if d.cfg.MirrorDispatch != nil {
			d.cfg.MirrorDispatch(run)
		} else {
			run()
		}
	}
	if d.cfg.DecisionBriefOnTick != nil {
		run := func() { d.decisionBriefTickOne() }
		if d.cfg.MirrorDispatch != nil {
			d.cfg.MirrorDispatch(run)
		} else {
			run()
		}
	}
	if d.cfg.ScheduleOnTick != nil {
		run := func() { d.scheduleTickOne() }
		if d.cfg.MirrorDispatch != nil {
			d.cfg.MirrorDispatch(run)
		} else {
			run()
		}
	}
	if d.cfg.OutboxSweepOnTick != nil {
		run := func() { d.outboxSweepTickOne() }
		if d.cfg.MirrorDispatch != nil {
			d.cfg.MirrorDispatch(run)
		} else {
			run()
		}
	}
}

// DeskStateLabels returns the last committed per-desk state labels (lowercased agent
// name → surface.State.String). Safe to call from DecisionBriefOnTick off d.mu.
func (d *Detector) DeskStateLabels() map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]string, len(d.snap.DeskStates))
	for name, st := range d.snap.DeskStates {
		out[strings.ToLower(name)] = st.String()
	}
	return out
}

// outboxSweepTickOne invokes the durable outbox sweep with the same recover() backstop
// as mirrorOne.
func (d *Detector) outboxSweepTickOne() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("flotilla watch: outbox sweep tick panicked: %v", r)
		}
	}()
	d.cfg.OutboxSweepOnTick()
}

// scheduleTickOne invokes the wall-clock scheduler with the same recover() backstop
// as mirrorOne.
func (d *Detector) scheduleTickOne() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("flotilla watch: schedule tick panicked: %v", r)
		}
	}()
	d.cfg.ScheduleOnTick()
}

// decisionBriefTickOne invokes the decision-brief scan with the same recover()
// backstop as mirrorOne.
func (d *Detector) decisionBriefTickOne() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("flotilla watch: decision-brief tick panicked (recovered; tick unaffected): %v", r)
		}
	}()
	d.cfg.DecisionBriefOnTick()
}

// mirrorOne invokes the per-desk visibility mirror with a recover() backstop. The mirror is
// OBSERVE-ONLY, so a panic inside it (a future claudestore refactor, a nil-map deref) MUST be
// swallowed + logged, never allowed to unwind through the detector goroutine and kill the
// safety-critical clock. This is the STRUCTURAL guarantee — not merely by-inspection — that the
// mirror can never harm the tick loop. (Wake/Rotate in the tail are deliberately NOT recovered: they
// are FUNCTIONAL side-effects, and a panic there is a real bug that must surface, not a best-effort
// post to absorb.) We keep the call SYNCHRONOUS rather than `go d.cfg.MirrorOnFinish(agent)`: a bare
// goroutine's panic is UNRECOVERABLE and would crash the whole daemon (the opposite of the goal), and
// the call already runs outside d.mu so it never blocks OperatorWake — a slow mirror only delays the
// next ticker beat (which time.Ticker coalesces), negligible against the heartbeat interval.
func (d *Detector) mirrorOne(agent string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("flotilla watch: desk mirror panicked for %q (recovered; tick unaffected): %v", agent, r)
		}
	}()
	d.cfg.MirrorOnFinish(agent)
}

// coordinatorMirrorOne invokes the primary-clock-XO mirror with the same recover()
// backstop as mirrorOne.
func (d *Detector) coordinatorMirrorOne(agent string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("flotilla watch: coordinator mirror panicked for %q (recovered; tick unaffected): %v", agent, r)
		}
	}()
	d.cfg.CoordinatorMirrorOnFinish(agent)
}

// idleHoldOne invokes the idle-hold break side-effect with the same recover()
// backstop as mirrorOne — observe-only failures must never kill the clock.
func (d *Detector) idleHoldOne(agent string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("flotilla watch: idle-hold break panicked for %q (recovered; tick unaffected): %v", agent, r)
		}
	}()
	d.cfg.IdleHoldOnFinish(agent)
}

// strandedHandoffOne invokes the stranded gate-report break side-effect with the same
// recover() backstop as mirrorOne.
func (d *Detector) strandedHandoffOne(agent string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("flotilla watch: stranded-handoff break panicked for %q (recovered; tick unaffected): %v", agent, r)
		}
	}()
	d.cfg.StrandedHandoffOnFinish(agent)
}

// delegationNudgeOne invokes the coordinator delegation nudge with the same recover()
// backstop as mirrorOne — observe-only failures must never kill the clock.
func (d *Detector) delegationNudgeOne(agent string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("flotilla watch: delegation-nudge panicked for %q (recovered; tick unaffected): %v", agent, r)
		}
	}()
	d.cfg.DelegationNudgeOnFinish(agent)
}

// persist durably writes the snapshot committed in-memory by tickLocked. It re-acquires d.mu
// (the single-writer invariant — d.save touches writeFails/degraded, and OperatorWake may have
// run in the unlock window) and is called by Tick AFTER runTail, so the durable commit lands
// after the wakes are enqueued (at-least-once across a restart — see Tick).
func (d *Detector) persist() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.save()
}

// tickLocked runs the lock-free-pure state machine under d.mu and RETURNS the side effects to
// perform after unlock (a pending rotate + the ordered wakes). It is the single per-interval
// writer of detector state; OperatorWake is the only other writer and shares the mutex.
func (d *Detector) tickLocked(warrant map[string]bool) (pendingRotate bool, pendingWakes []deferredWake, pendingMirrors, pendingCoordinatorMirrors []string, pendingDelegation []string, pendingAdjutantSeams []string, pendingSynth []synthEligible, pendingDeskBeats []string, pendingDeskEscalations []string, pendingRateLimit rateLimitWork, pendingAutoSwitch []RateLimitAutoSwitchCandidate, pendingTurnEnds []string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	woke := false
	wake := func(kind WakeKind, reasons []string) {
		woke = true
		pendingWakes = append(pendingWakes, deferredWake{kind: kind, reasons: reasons})
	}
	wakeLayer := func(owner string, kind WakeKind, reasons []string) {
		// Layer wakes must not advance the primary quiet/liveness clock (#470).
		pendingWakes = append(pendingWakes, deferredWake{kind: kind, reasons: reasons, owner: owner})
	}
	deliverMaterial := func(reasons []string) {
		if !d.cfg.StackableWakes || d.cfg.OwningXO == nil || d.cfg.WakeLayer == nil {
			wake(WakeMaterial, reasons)
			return
		}
		primary, byOwner := groupMaterialByOwner(reasons, d.cfg.OwningXO)
		if len(primary) > 0 {
			wake(WakeMaterial, primary)
		}
		// Primary-owned desk material lands in byOwner[XOAgent], not the fleet-wide
		// primary slice — route it through wake() so the primary clock advances (#487 P1).
		if owned := byOwner[d.cfg.XOAgent]; len(owned) > 0 {
			wake(WakeMaterial, owned)
			delete(byOwner, d.cfg.XOAgent)
		}
		owners := make([]string, 0, len(byOwner))
		for owner := range byOwner {
			owners = append(owners, owner)
		}
		sort.Strings(owners)
		for _, owner := range owners {
			wakeLayer(owner, WakeMaterial, byOwner[owner])
		}
	}
	requestRotate := func() {
		if !d.cfg.RotatePolicy.AllowsIdleEdgeRotate() {
			return
		}
		pendingRotate = true
	}

	// 1. Gather current signals. Signal absent/unreadable carries the prior hash
	//    forward (treat-unchanged — M4); states are shell-debounced (M2).
	cur := Snapshot{
		DeskStates: make(map[string]surface.State, len(d.cfg.Desks)),
		SignalHash: d.snap.SignalHash,
		XOSettled:  d.snap.XOSettled,
	}
	for _, name := range d.cfg.Desks {
		cur.DeskStates[name] = d.debounce(name, d.cfg.Assess(name))
	}
	if h, ok := d.cfg.SignalHash(); ok {
		cur.SignalHash = h
	}

	// 2. Cold start (missing/corrupt snapshot, or first boot): seed the baseline
	//    WITHOUT emitting per-desk transitions (L3), but wake ONCE conservatively
	//    so a change that happened while the detector was down is not missed.
	if d.cold {
		d.cold = false
		d.snap = cur
		d.evalLiveness(cur) // still cover liveness from tick one
		wake(WakeMaterial, []string{"change-detector started — reassess the fleet"})
		d.quietFSM.OnColdStart()
		return // durable save happens in Tick AFTER the tail enqueues this wake (at-least-once)
	}

	prev := d.snap

	applyPrimaryMaterialClock := func(reasons []string) {
		if !d.cfg.StackableWakes || d.cfg.OwningXO == nil {
			d.selfCont = 0
			cur.XOSettled = false
			return
		}
		primary, byOwner := groupMaterialByOwner(reasons, d.cfg.OwningXO)
		if len(primary) > 0 || len(byOwner[d.cfg.XOAgent]) > 0 {
			d.selfCont = 0
			cur.XOSettled = false
		}
	}

	// 2b. Per-desk visibility mirror trigger: each NON-XO desk that completed a unit of work this
	//     tick (a confirmed Working→Idle transition) is recorded for the post-unlock tail to mirror
	//     to its home channel. Computed UNDER the lock from the same debounced states the diff uses,
	//     but emitted OUTSIDE d.mu in runTail. This is reached only past the cold-start early-return
	//     above, so the cold-start baseline emits NO mirrors (a desk that was already Idle when the
	//     detector booted has not "finished a turn"). The XO is excluded — it has its own mirror.
	for _, name := range d.cfg.Desks {
		if prev.DeskStates[name] == surface.StateWorking && cur.DeskStates[name] == surface.StateIdle {
			if name == d.cfg.XOAgent {
				pendingCoordinatorMirrors = append(pendingCoordinatorMirrors, name)
				pendingTurnEnds = append(pendingTurnEnds, name)
				continue
			}
			pendingMirrors = append(pendingMirrors, name)
			pendingTurnEnds = append(pendingTurnEnds, name)
			// 2c. Visibility-synthesis OWED marking (B2): a non-XO desk finishing a turn marks
			//     synthesis owed for each of its synthesizing parent(s) — AgentsAbove(name). A boat in
			//     two channels marks BOTH; a project-XO finishing marks the meta-XO (the Tier-3
			//     recursion, since a project-XO is itself a non-XO "desk" below the meta). Inert when
			//     SynthParents is the default (returns nil) — nobody is ever owed.
			for _, parent := range d.cfg.SynthParents(name) {
				d.synthOwed[parent] = true
			}
		}
	}

	// 2d. Coordinator delegation-nudge trigger (#232): every coordinator (any XO or CoS)
	// that finished a turn — INCLUDING the primary clock XO, which the mirror path
	// above deliberately skips. Project-XOs and a standalone CoS are covered here too.
	if d.cfg.IsCoordinator != nil && d.cfg.DelegationNudgeOnFinish != nil {
		for _, name := range d.cfg.Desks {
			if !d.cfg.IsCoordinator(name) {
				continue
			}
			if prev.DeskStates[name] == surface.StateWorking && cur.DeskStates[name] == surface.StateIdle {
				pendingDelegation = append(pendingDelegation, name)
			}
		}
	}

	// 3. Liveness — independent of the diff (H3): crash (shell-debounced) +
	//    wall-clock ack age. Kept in-memory + ack-file so a snapshot outage can
	//    never blind the watchdog.
	d.evalLiveness(cur)

	// 4. External material change (every desk EXCEPT the XO — H2). It re-engages a
	//    settled XO and resets the self-continuation cap.
	xoSeam := xoFinishedTurn(prev, cur, d.cfg.XOAgent)
	if ext, reasons := externalMaterial(prev, cur, d.cfg.XOAgent); ext {
		applyPrimaryMaterialClock(reasons)
		deliverMaterial(reasons)
	} else if rateReasons, autoCandidates := d.rateLimitMaterialFromPendingLocked(); len(rateReasons) > 0 {
		// 4b. Provider rate-limit (#204/#205): wake from the PREVIOUS tick's off-mutex probes;
		// auto-switch candidates dispatch OFF d.mu in runAutoSwitch.
		applyPrimaryMaterialClock(rateReasons)
		deliverMaterial(rateReasons)
		pendingAutoSwitch = autoCandidates
	} else if xoSeam && !cur.XOSettled {
		// 5. XO self-continuation — only when nothing external fired this tick (an
		//    external change already covers advancing the XO and resets the cap).
		d.continueXO(&cur, wake, requestRotate)
	}

	// 5b. Visibility-synthesis (B2): decide UNDER d.mu which OWED synthesizing agents are cadence-
	//     eligible this tick (PURE / cheap — NO I/O here), and return them for runSynthesis to read +
	//     wake OFF d.mu. Deliberately separate from `woke`: a synthesis wake targets an arbitrary
	//     synthesizing agent (never the primary clock XO), so it must NOT reset the primary XO's
	//     quiet/liveness counters below. Inert (nil) when nothing is owed ($0-idle preserved).
	pendingSynth = d.synthEligibleLocked()

	// 5c. Recursive desk-heartbeat (#183) — decide per-desk beats + cap-escalations UNDER d.mu; the
	//     DELIVERY (the actual beat enqueue + the loud escalation) runs OFF d.mu in runDeskHeartbeats,
	//     mirroring runSynthesis (its confirmed delivery acquires the pane-txn lock, a bounded wait
	//     that must never be held under the detector mutex). Reached only PAST the cold-start
	//     early-return above, so the cold baseline owes NO beats — exactly like the mirror/synth
	//     sections get cold-start suppression for free. Fully inert when HeartbeatEnabled is nil (the
	//     loop is skipped), so the detector is byte-identical to before #183.
	pendingDeskBeats, pendingDeskEscalations = d.deskHeartbeatLocked(cur, warrant)

	// 6. Max-quiet liveness ping (layer 3). Any wake above already refreshes
	//    liveness (L1), so only an entirely-quiet tick advances the quiet counter.
	if woke {
		d.quietFSM.OnWake()
	} else if d.pingPeriod > 0 {
		now := d.now()
		if d.quietFSM.OnQuietTick(now) {
			wake(WakePing, nil)
			d.quietFSM.OnPingFired(now)
		}
	}

	// 7. Commit the new baseline IN-MEMORY (the diff source for the next tick, so a handled
	//    transition isn't re-woken). The DURABLE write is deferred to Tick's d.persist() AFTER the
	//    tail enqueues the wakes, so a crash before the durable commit re-detects on restart (H3 /
	//    cubic-P1 at-least-once).
	d.snap = cur
	pendingRateLimit = d.rateLimitWorkLocked(cur)
	if d.cfg.AdjutantSeamOnFinish != nil {
		if d.cfg.AdjutantFor == nil {
			if xoSeam {
				pendingAdjutantSeams = append(pendingAdjutantSeams, d.cfg.XOAgent)
			}
		} else {
			for _, name := range d.cfg.Desks {
				if d.cfg.AdjutantFor(name) == "" {
					continue
				}
				if prev.DeskStates[name] == surface.StateWorking && cur.DeskStates[name] == surface.StateIdle {
					pendingAdjutantSeams = append(pendingAdjutantSeams, name)
				}
			}
			sort.Strings(pendingAdjutantSeams)
		}
	}
	return pendingRotate, pendingWakes, pendingMirrors, pendingCoordinatorMirrors, pendingDelegation, pendingAdjutantSeams, pendingSynth, pendingDeskBeats, pendingDeskEscalations, pendingRateLimit, pendingAutoSwitch, pendingTurnEnds
}

// deskWarrantSnapshot is the #189 PHASE-1 read, run OFF d.mu from Tick BEFORE tickLocked acquires the
// lock. For each eligible desk (a configured non-XO agent that HeartbeatEnabled passes — the same
// pre-filter the under-lock loop applies, so an opted-out/approval-sensitive desk's backlog is NEVER
// read) it consults the HeartbeatWarranted seam — the FILE I/O (os.ReadFile + backlog.Parse via the cmd
// wiring) — and snapshots a PURE map[agent]bool warrant. The under-lock phase-2 decision
// (deskHeartbeatLocked) then consults ONLY this map, so NO backlog file I/O ever runs under d.mu (the
// detector's load-bearing off-mutex invariant — the two-phase split mirroring synthEligibleLocked/
// runSynthesis). Returns nil when the feature is off (HeartbeatEnabled nil) or the warrant seam is
// unwired (NewDetector defaults it non-nil to always-true, so a wired-feature deployment with no
// per-recipient backlogs maps every eligible desk to true ⇒ #183 behavior). The HeartbeatEnabled +
// HeartbeatWarranted seams are read-only/pure w.r.t. detector state, so calling them without the lock is
// safe (no detector mutable state is touched here).
func (d *Detector) deskWarrantSnapshot() map[string]bool {
	if d.cfg.HeartbeatEnabled == nil || d.cfg.HeartbeatWarranted == nil {
		return nil // feature off OR warrant unwired ⇒ the under-lock decision defaults to warranted
	}
	warrant := make(map[string]bool, len(d.cfg.Desks))
	for _, name := range d.cfg.Desks {
		if name == d.cfg.XOAgent || !d.cfg.HeartbeatEnabled(name) {
			continue // never read the backlog of the XO or an opted-out/approval-sensitive desk
		}
		warrant[name] = d.cfg.HeartbeatWarranted(name) // the OFF-lock file read happens HERE
	}
	return warrant
}

// deskHeartbeatLocked is the recursive desk-heartbeat (#183) decision run UNDER d.mu from tickLocked.
// It is the careful core — every per-desk transition (design §9) is decided here, in cheap in-memory
// state, and returns the desks OWED a beat + the desks to ESCALATE; the actual delivery happens OFF
// d.mu in runDeskHeartbeats. It is PURE relative to the panes (it touches only the five per-agent maps
// + the injected DeskSettleConsume seam, which is a fail-safe file stat, not a pane read).
//
// The cap is PROGRESS-OBSERVABLE and decided HERE (NOT keyed on a delivery outcome): the beat is
// fire-and-forget (a busy/input-blocked pane silently drops a Kind:"detector" job), so runDeskHeartbeats
// never learns the per-beat outcome and the cap cannot depend on it. A desk that went Working since its
// last beat (deskProgressed) is responsive and resets the cap; a desk that stayed idle across capN beats
// is wedged and escalates ONCE (the ==capN edge) then stops.
//
// Inert when HeartbeatEnabled is nil — the whole loop is skipped, so the detector is byte-identical to
// before #183 (the regression-lock).
func (d *Detector) deskHeartbeatLocked(cur Snapshot, warrant map[string]bool) (beats, escalations []string) {
	if d.cfg.HeartbeatEnabled == nil {
		return nil, nil // feature OFF ⇒ byte-inert
	}
	period := d.cfg.DeskHeartbeatPeriod
	capN := d.cfg.DeskHeartbeatCap // defaulted >= 1 in NewDetector
	now := d.now()
	for _, name := range d.cfg.Desks {
		if name == d.cfg.XOAgent || !d.cfg.HeartbeatEnabled(name) {
			continue // the primary XO keeps its own clock; an opted-out desk never beats
		}
		switch cur.DeskStates[name] {
		case surface.StateWorking:
			// Progress: the desk re-engaged. Latch progressed (so an owed beat after this never counts
			// toward the cap), un-wedge it (progress clears a stop), and restart both the cadence and
			// the cap — a freshly-idle desk gets a full cadence before its next beat.
			d.deskProgressed[name] = true
			delete(d.deskStopped, name)
			d.deskNoProgress[name] = 0
			delete(d.deskBeatEligibleAt, name)
			delete(d.deskSettled, name)
		case surface.StateIdle:
			// Consume the per-agent settle marker (fail-safe → not-settled). A desk that touched its
			// marker is settled until re-armed.
			if d.cfg.DeskSettleConsume != nil && d.cfg.DeskSettleConsume(name) {
				d.deskSettled[name] = true
			}
			if d.deskSettled[name] || d.deskStopped[name] {
				continue // a settled/stopped desk does not beat AND does not accrue cadence
			}
			last := d.deskBeatEligibleAt[name]
			if last.IsZero() {
				d.deskBeatEligibleAt[name] = now // anchor first idle tick (no beat yet)
				continue
			}
			if now.Sub(last) < period {
				continue // cadence not yet elapsed
			}
			// #189 JUDGMENT — the LAST gate, evaluated only once the beat is otherwise owed (HARD gate
			// passed, not settled/stopped, cadence elapsed). The warrant is a PURE lookup against the
			// phase-1 snapshot read OFF d.mu in deskWarrantSnapshot (the cmd wiring did the backlog
			// ReadFile+Parse before tickLocked acquired the lock); NO file I/O runs here under the lock —
			// the detector's load-bearing off-mutex invariant. A desk ABSENT from the warrant map defaults
			// to WARRANTED (the map is nil when the warrant seam is unwired ⇒ #183 byte-identical; an
			// eligible desk always has an entry). A NOT-warranted desk is legitimately idle (no live
			// actionable work — everything done, blocked-and-tracked, or awaiting-auth): treat it like a
			// settled tick — reset the cadence counter (cadence-neutral: it does not re-trigger eligibility
			// every tick) and continue WITHOUT touching the cap (cap-neutral: a not-warranted idle desk is
			// not a wedge, so it accrues no no-progress). The judgment can ONLY suppress here; it never
			// resurrects a beat the gates above withheld — a pure narrowing of the #183 candidate set.
			if w, ok := warrant[name]; ok && !w {
				delete(d.deskBeatEligibleAt, name)
				continue
			}
			// Owed a beat.
			beats = append(beats, name)
			d.deskBeatEligibleAt[name] = now
			// Cap accounting (progress-observable, in-memory, HERE — not off-mutex): a desk that went
			// Working since its last beat is responsive (cap resets); otherwise it accrues no-progress.
			if d.deskProgressed[name] {
				d.deskNoProgress[name] = 0
			} else {
				d.deskNoProgress[name]++
			}
			d.deskProgressed[name] = false
			if d.deskNoProgress[name] >= capN {
				// Wedged: idle + un-progressing across capN beats. Escalate ONCE on the ==capN edge,
				// then stop beating until re-armed (AgentWake).
				escalations = append(escalations, name)
				d.deskStopped[name] = true
			}
		default:
			// Unknown/Shell/other (unassessable pane): no state change, no beat, NO cadence accrual —
			// an unreadable pane is not a confirmed Idle, so it must not advance toward a beat.
		}
	}
	return beats, escalations
}

// synthEligibleLocked is the visibility-synthesis (B2) decision run UNDER d.mu from tickLocked. It is
// PURE and CHEAP — NO I/O: it advances the digest cadence clock and selects the OWED synthesizing
// agents that are cadence-eligible this tick, snapshotting each one's read set + last-seen hashes for
// the OFF-mutex read in runSynthesis. The blocking transcript read + the materiality compare + the
// owed/last-seen COMMIT all happen later in runSynthesis (off d.mu), so no tmux/transcript I/O ever
// executes while d.mu is held — the detector's load-bearing off-mutex invariant (see Tick/runTail).
// Returns the eligible agents; nil when nothing is owed (the inert / $0-idle path).
//
// Cadence: synthSinceFire[agent] is the tick count since the agent last fired; it advances every tick
// for every fired agent, so a long idle gap makes the next owe immediately eligible (the cadence
// spaces consecutive WAKES, not idle gaps). A never-fired owed agent is eligible at once. A
// not-yet-eligible owe is KEPT pending (owed is NOT consumed here) so a burst coalesces into one wake
// once the window elapses. Owed is consumed — and last-seen committed — only in runSynthesis, after
// the read.
func (d *Detector) synthEligibleLocked() []synthEligible {
	if len(d.synthOwed) == 0 {
		return nil
	}

	// Deterministic order: configured desks first, then any extra owed agents, so the wake order
	// (and thus tests) is stable rather than map-iteration-random.
	order := make([]string, 0, len(d.synthOwed))
	seen := map[string]bool{}
	for _, name := range d.cfg.Desks {
		if d.synthOwed[name] && !seen[name] {
			order = append(order, name)
			seen[name] = true
		}
	}
	for agent := range d.synthOwed {
		if !seen[agent] {
			order = append(order, agent)
			seen[agent] = true
		}
	}

	now := d.now()
	var out []synthEligible
	for _, agent := range order {
		// Cadence eligibility: a never-fired agent (no synthSinceFireAt entry) is eligible at once; one
		// that fired recently waits out the digest window (its owe is KEPT — the burst coalesces).
		if ts, known := d.synthSinceFireAt[agent]; known && now.Sub(ts) < d.cfg.SynthEveryPeriod {
			continue
		}
		out = append(out, synthEligible{
			agent:    agent,
			readSet:  d.synthReadSet(agent),
			lastSeen: cloneHashes(d.synthState.LastSeen[agent]),
		})
	}
	return out
}

// runSynthesis performs the visibility-synthesis read, materiality compare, and wake delivery OUTSIDE
// d.mu (the P1 fix). For each cadence-eligible owed agent decided under the lock, it reads each
// subordinate's latest turn-final state via SynthRead — BLOCKING tmux + transcript I/O that MUST NOT
// run under d.mu (it would stall the tick loop and block OperatorWake, the relay goroutine; this is
// the exact off-mutex discipline the mirror path follows). It commits the owed / last-seen state under
// a SHORT re-lock (commitSynthesisLocked) and delivers the WakeSynthesis through the agent-targeted
// WakeAgent seam, again off-mutex (its confirmed delivery acquires the pane-txn lock).
//
// Run SYNCHRONOUSLY in the tail — NOT async like the observe-only mirror (MirrorDispatch): synthesis
// COMMITS last-seen state the next tick reads, so an async run could interleave two ticks' decisions.
// Sync-in-tail resolves the mutex stall without that ordering hazard; the cost is only a possible
// delay of the NEXT tick (which the ticker coalesces), bounded by the cadence gate.
func (d *Detector) runSynthesis(eligible []synthEligible) {
	for _, e := range eligible {
		changed, fresh := materialSubordinates(e.lastSeen, e.readSet, d.synthReadOne)
		if d.commitSynthesisLocked(e.agent, changed, fresh) && d.cfg.WakeAgent != nil {
			d.cfg.WakeAgent(e.agent, WakeSynthesis, changed)
		}
	}
}

// runDeskHeartbeats DELIVERS the recursive desk-heartbeat (#183) side effects decided UNDER d.mu in
// deskHeartbeatLocked, OFF d.mu (the same off-mutex-delivery discipline as runSynthesis/the mirror).
// Each beat is a fire-and-forget desk-continuation turn via WakeDeskHeartbeat (its confirmed delivery
// acquires the pane-txn lock; a busy/input-blocked pane silently drops it). Each escalation raises the
// LOUD cap-alert via DeskEscalate (the desk's owning XO). No detector state is touched here — the
// cadence + cap were already committed in tickLocked — so this needs no re-lock. Inert when the seams
// are nil (the decision returned nil slices anyway when HeartbeatEnabled is nil; this double-guards a
// partially-wired config).
func (d *Detector) runDeskHeartbeats(beats, escalations []string) {
	for _, name := range beats {
		if d.cfg.WakeDeskHeartbeat != nil {
			d.cfg.WakeDeskHeartbeat(name)
		}
	}
	for _, name := range escalations {
		if d.cfg.DeskEscalate != nil {
			d.cfg.DeskEscalate(name)
		}
	}
}

// commitSynthesisLocked commits one agent's synthesis decision under a SHORT re-lock of d.mu and
// reports whether to fire its wake. The owe is consumed either way (a fire records what changed; an
// immaterial owe drops the pending trigger — a later finish re-owes). On a fire it records the fresh
// last-seen hashes and resets the cadence counter. The re-lock touches only synthesis state, disjoint
// from OperatorWake's settle/quiet/drive state, so an interleaving OperatorWake is safe.
func (d *Detector) commitSynthesisLocked(agent string, changed []string, fresh map[string]string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.synthOwed, agent)
	if len(changed) == 0 {
		return false // nothing material changed since last synthesis — suppress (no re-post)
	}
	if d.synthState.LastSeen == nil {
		d.synthState.LastSeen = map[string]map[string]string{}
	}
	d.synthState.LastSeen[agent] = fresh
	d.synthSinceFireAt[agent] = d.now()
	return true
}

// cloneHashes returns a shallow copy of a subordinate→hash map so the off-mutex materiality compare in
// runSynthesis reads a stable snapshot taken under d.mu, independent of any later commit.
func cloneHashes(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// synthReadSet returns the synthesizing agent's read set — the subordinates whose latest state it
// rolls up. v1 uses the SAME relation as the owed-marking inverse: the agents whose finish would
// mark THIS agent owed. The production caller resolves it via roster.AgentsBelow(agent); the
// detector discovers it from the desks that name `agent` as a parent (SynthParents), so the
// materiality read set and the owed-marking stay derived from one source. Order-stable (desk order).
func (d *Detector) synthReadSet(agent string) []string {
	var out []string
	seen := map[string]bool{}
	for _, name := range d.cfg.Desks {
		if name == agent || seen[name] {
			continue
		}
		for _, parent := range d.cfg.SynthParents(name) {
			if parent == agent {
				out = append(out, name)
				seen[name] = true
				break
			}
		}
	}
	return out
}

// synthReadOne reads a subordinate's latest turn-final text via the injected SynthRead seam
// (ok=false ⇒ unreadable — excluded from materiality). Defaults to unreadable when SynthRead is
// unwired, keeping the synthesis path inert.
func (d *Detector) synthReadOne(agent string) (string, bool) {
	if d.cfg.SynthRead == nil {
		return "", false
	}
	return d.cfg.SynthRead(agent)
}

// continueXO handles the XO's own Working→Idle. It rotates to fresh context (unless awaiting an
// operator reply), then consults the backlog gate: while UNBLOCKED items remain it NEVER settles —
// overriding both the XO's idle self-signal and the self-continuation cap (the mechanical
// anti-passivity fix) — and drives the top non-stuck item (WakeBacklog). Only when the gate is
// satisfied (no unblocked items: empty / all-operator-blocked / awaiting / no backlog configured)
// does the existing settle/continuation/cap logic apply, byte-identical to before.
func (d *Detector) continueXO(cur *Snapshot, wake func(WakeKind, []string), requestRotate func()) {
	settleSignalled := d.cfg.SettleConsume != nil && d.cfg.SettleConsume()

	queue := d.cfg.BacklogGate().Unblocked
	if d.cfg.Awaiting != nil && d.cfg.Awaiting() {
		queue = nil
	}
	d.pruneDriveCounts(queue)

	if len(queue) == 0 {
		if settleSignalled {
			if d.cfg.Awaiting == nil || !d.cfg.Awaiting() {
				requestRotate()
			}
			cur.XOSettled = true
			return
		}
		if !d.continuationDue() {
			return
		}
		if d.cfg.Awaiting == nil || !d.cfg.Awaiting() {
			requestRotate()
		}
		d.selfCont++
		d.lastContinuationAt = d.now()
		if d.selfCont > d.cfg.MaxSelfContinuation {
			cur.XOSettled = true
			log.Printf("flotilla watch: XO self-continuation hit the cap (%d) with no external change — forcing settled", d.cfg.MaxSelfContinuation)
			return
		}
		wake(WakeContinuation, nil)
		return
	}

	d.selfCont = 0
	target := d.pickDriveTarget(queue)
	if !d.backlogDriveDue(target) {
		return
	}
	if d.cfg.Awaiting == nil || !d.cfg.Awaiting() {
		requestRotate()
	}
	d.driveCount[target]++
	d.lastDriveAt[target] = d.now()
	if d.driveCount[target] == d.cfg.BacklogStuckCap {
		// Just crossed the cap → escalate THIS item ONCE and deprioritize it (pickDriveTarget will
		// prefer lower-priority items below the cap next time). The XO durably marks it
		// [blocked]/[needs-attention] in response, removing it from the queue.
		if d.cfg.Alert != nil {
			d.cfg.Alert(fmt.Sprintf("goal-loop: backlog item not progressing after %d wakes — advance it, or mark it [blocked]/[needs-attention]: %s", d.cfg.BacklogStuckCap, target))
		}
	}
	wake(WakeBacklog, []string{target})
}

func (d *Detector) continuationDue() bool {
	return d.lastContinuationAt.IsZero() ||
		d.now().Sub(d.lastContinuationAt) >= d.cfg.ReferenceInterval
}

func (d *Detector) backlogDriveDue(item string) bool {
	t, ok := d.lastDriveAt[item]
	return !ok || d.now().Sub(t) >= d.cfg.ReferenceInterval
}

// pickDriveTarget returns the highest-priority queued item whose drive count is still below the
// stuck cap; if EVERY queued item is at/over the cap, it returns the top item anyway (keep driving
// at cadence — never spin tighter than the tick, never settle while work remains).
func (d *Detector) pickDriveTarget(queue []string) string {
	for _, item := range queue {
		if d.driveCount[item] < d.cfg.BacklogStuckCap {
			return item
		}
	}
	return queue[0]
}

// pruneDriveCounts drops per-item driveCount and lastDriveAt entries for items no longer in the
// unblocked queue (drained, or the XO marked them blocked/done), so a re-appearing item starts
// fresh and neither map grows unbounded.
func (d *Detector) pruneDriveCounts(queue []string) {
	if len(d.driveCount) == 0 && len(d.lastDriveAt) == 0 {
		return
	}
	live := make(map[string]struct{}, len(queue))
	for _, item := range queue {
		live[item] = struct{}{}
	}
	for item := range d.driveCount {
		if _, ok := live[item]; !ok {
			delete(d.driveCount, item)
		}
	}
	for item := range d.lastDriveAt {
		if _, ok := live[item]; !ok {
			delete(d.lastDriveAt, item)
		}
	}
}

// debounce returns a desk's effective state, suppressing a single transient
// shell read: a shell is only believed after shellDebounce consecutive reads;
// before that the prior known state is held so a blip is never a crash (M2).
func (d *Detector) debounce(name string, raw surface.State) surface.State {
	if raw != surface.StateShell {
		d.shellStreak[name] = 0
		return raw
	}
	d.shellStreak[name]++
	if d.shellStreak[name] >= shellDebounce {
		return surface.StateShell
	}
	if prev, ok := d.snap.DeskStates[name]; ok {
		return prev // hold the last known state through the blip
	}
	return surface.StateUnknown
}

// evalLiveness drives the watchdog from the two cadence-independent signals
// (C1): a shell-debounced crash (immediate) and a wall-clock ack age over the
// mode-derived window while the XO is not a shell. The watchdog (maxMissed=1)
// debounces the alert and clears it on recovery.
func (d *Detector) evalLiveness(cur Snapshot) {
	shellStreak := d.shellStreak[d.cfg.XOAgent]
	crashed := shellStreak >= shellDebounce
	switch {
	case crashed:
		d.wd.Observe(false, true)
	case shellStreak == 0 && d.cfg.AckAge() > d.alertWindow:
		// Wedged: alive (no shell suspicion) but not acking within the window. The
		// `shellStreak == 0` guard means a tick where a shell is suspected but not
		// yet confirmed does NOT fire the "wedged" message — the next tick confirms
		// the crash and the (debounced) alert carries the accurate crash wording.
		d.wd.Observe(false, false)
	default:
		d.wd.Observe(true, false) // healthy (or shell pending) → clear any down state
	}
}

// save persists the snapshot fail-safe (H3). The in-memory snapshot is the
// source of truth for diffs, so a write failure never causes wake-every-tick;
// after a run of failures it raises a loud alert and degrades to in-memory-only
// (failing toward not-spending). A later restart cold-starts (one wake), never
// a per-tick storm.
func (d *Detector) save() {
	if d.degraded {
		return
	}
	if err := d.cfg.Persist(d.snap); err != nil {
		d.writeFails++
		log.Printf("flotilla watch: snapshot persist failed (%d/%d): %v", d.writeFails, snapshotWriteFailThreshold, err)
		if d.writeFails >= snapshotWriteFailThreshold && d.cfg.Alert != nil {
			d.degraded = true
			d.cfg.Alert("change-detector snapshot persistence is failing — continuing in-memory only; detector state will not survive a restart, but waking is unaffected")
		}
		return
	}
	d.writeFails = 0

	// Durably persist the visibility-synthesis materiality sidecar (B2) so the last-seen state
	// survives a daemon restart (no synthesis restart-storm). Only written when synthesis has
	// recorded state — an inert deployment (no last-seen entries) writes nothing, so a fleet
	// without synthesis is byte-identical to before (no extra file). A sidecar write failure is
	// logged and dropped: it can only cost an EXTRA synthesis on the next restart (the sidecar
	// fails safe to all-changed), never a silent miss, so it must not trip the snapshot degrade
	// path or alert.
	if len(d.synthState.LastSeen) > 0 {
		if err := d.cfg.SynthPersist(d.synthState); err != nil {
			log.Printf("flotilla watch: synthesis sidecar persist failed: %v (continuing — at worst one extra synthesis after a restart)", err)
		}
	}
}

// rateLimitMaterialFromPendingLocked reads the PREVIOUS tick's off-mutex probe results and
// returns material wake reasons plus auto-switch candidates. Called under d.mu.
// "Sustained" for the switch decision = the probe driver's 2-consecutive-read debounce
// (RateLimitProbe) already applied before results land here. Edge-triggered: one wake (and
// at most one auto-switch enqueue) per throttle episode per desk — cleared when the probe
// stops reporting limited. Storm cooldown (≥2 reports / 10m) is separate: it poisons failover
// targets only, not whether a candidate is collected here.
func (d *Detector) rateLimitMaterialFromPendingLocked() (reasons []string, candidates []RateLimitAutoSwitchCandidate) {
	if d.cfg.RateLimitMaterial == nil {
		return nil, nil
	}
	d.rateLimitProbeMu.Lock()
	pending := d.rateLimitPending
	d.rateLimitProbeMu.Unlock()
	if len(pending) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(pending))
	for name := range pending {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		res := pending[name]
		if !res.ok || !res.limited {
			delete(d.rateLimitActive, name)
			continue
		}
		if d.rateLimitActive[name] {
			continue // already woke for this episode
		}
		d.rateLimitActive[name] = true
		reasons = append(reasons, name+": rate-limited ("+res.scope.String()+" — switch eligible)")
		if d.cfg.RateLimitAutoSwitch != nil {
			if d.cfg.RateLimitAutoSwitchEligible == nil || d.cfg.RateLimitAutoSwitchEligible(name) {
				candidates = append(candidates, RateLimitAutoSwitchCandidate{Agent: name, Scope: res.scope})
			}
		}
	}
	return reasons, candidates
}

// EndAutoSwitchFlight clears the per-desk in-flight marker after a side-channel auto-switch
// subprocess completes. Wired from cmd/flotilla/watch.go into the dispatch callback.
func (d *Detector) EndAutoSwitchFlight(agent string) {
	d.autoSwitchFlight.End(agent)
}

// runAutoSwitch dispatches auto-switch candidates OFF d.mu (side-channel exec). One-in-flight
// dedupe happens here via AutoSwitchFlight.TryBegin; the production callback must call End when
// the subprocess completes.
func (d *Detector) runAutoSwitch(candidates []RateLimitAutoSwitchCandidate) {
	if len(candidates) == 0 || d.cfg.RateLimitAutoSwitch == nil {
		return
	}
	dispatched := make([]RateLimitAutoSwitchCandidate, 0, len(candidates))
	for _, c := range candidates {
		if d.autoSwitchFlight.TryBegin(c.Agent) {
			dispatched = append(dispatched, c)
		}
	}
	if len(dispatched) == 0 {
		return
	}
	run := func() { d.cfg.RateLimitAutoSwitch(dispatched) }
	if d.cfg.RateLimitAutoSwitchDispatch != nil {
		d.cfg.RateLimitAutoSwitchDispatch(run)
	} else {
		run()
	}
}

func (d *Detector) rateLimitProbeDue(name string) bool {
	t, ok := d.rateLimitLastProbeAt[name]
	return !ok || d.now().Sub(t) >= d.cfg.RateLimitProbePeriod
}

// rateLimitWorkLocked decides which desks to probe OFF mutex this tick (Idle/Errored
// non-XO desks) and which streaks to reset (desks that left the candidate states).
// Pure under d.mu — NO pane I/O.
func (d *Detector) rateLimitWorkLocked(cur Snapshot) rateLimitWork {
	if d.cfg.RateLimitMaterial == nil {
		return rateLimitWork{}
	}
	var work rateLimitWork
	for _, name := range d.cfg.Desks {
		if name == d.cfg.XOAgent {
			continue
		}
		st := cur.DeskStates[name]
		if st == surface.StateIdle || st == surface.StateErrored {
			if !d.rateLimitProbeDue(name) {
				continue
			}
			work.probe = append(work.probe, name)
		} else {
			delete(d.rateLimitActive, name)
			work.reset = append(work.reset, name)
		}
	}
	sort.Strings(work.probe)
	sort.Strings(work.reset)
	now := d.now()
	for _, name := range work.probe {
		d.rateLimitLastProbeAt[name] = now
	}
	return work
}

// runRateLimitProbes executes the per-tick rate-limit probe batch OFF d.mu. Results are
// stored for the NEXT tick's rateLimitWakesFromPendingLocked (fold-back). Production
// dispatches async via RateLimitDispatch (mirrors MirrorDispatch).
func (d *Detector) runRateLimitProbes(work rateLimitWork) {
	if d.cfg.RateLimitMaterial == nil {
		return
	}
	if len(work.probe) == 0 && len(work.reset) == 0 {
		return
	}
	run := func() {
		if d.cfg.RateLimitReset != nil {
			for _, agent := range work.reset {
				d.cfg.RateLimitReset(agent)
			}
		}
		results := make(map[string]rateLimitProbeResult, len(work.probe))
		for _, agent := range work.probe {
			limited, scope, _, ok := d.cfg.RateLimitMaterial(agent)
			results[agent] = rateLimitProbeResult{limited: limited, scope: scope, ok: ok}
		}
		d.rateLimitProbeMu.Lock()
		d.rateLimitPending = results
		d.rateLimitProbeMu.Unlock()
	}
	if d.cfg.RateLimitDispatch != nil {
		d.cfg.RateLimitDispatch(run)
	} else {
		run()
	}
}

// xoFinishedTurn reports the XO's own Working→Idle transition (its self-
// continuation trigger). Kept separate from externalMaterial, which excludes the
// XO (H2).
func xoFinishedTurn(prev, cur Snapshot, xo string) bool {
	return prev.DeskStates[xo] == surface.StateWorking && cur.DeskStates[xo] == surface.StateIdle
}
