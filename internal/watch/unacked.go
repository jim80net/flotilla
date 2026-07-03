package watch

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/transport"
	"github.com/jim80net/flotilla/internal/unacked"
)

const (
	defaultUnackedRetention = 7 * 24 * time.Hour
)

// RecentHistoryReader fetches channel history for the un-acked backstop (ascending).
type RecentHistoryReader interface {
	Recent(channelID string, limit int) ([]transport.Message, error)
	// RecentSince returns messages with Timestamp >= since using non-overlapping
	// backward pagination (Discord before-pages).
	RecentSince(channelID string, since time.Time) ([]transport.Message, error)
}

// CoordinatorWake attempts a confirmed delivery to the coordinator pane. It
// returns nil on success, surface.ErrBusy when mid-turn (retry next sweep), or
// another error on failure. Same signature as SendFunc — alias so mkSend wiring
// composes without a cast at the call site.
type CoordinatorWake = SendFunc

// UnackedBackstop periodically scans bound channels for operator messages with
// no fleet acknowledgment and surfaces a digest (issue #234).
type UnackedBackstop struct {
	cfg          *roster.Config
	reader       RecentHistoryReader
	alert        func(string)
	wake         CoordinatorWake
	coordinator  func(*roster.Config) string
	store        unackedStateStore
	scanCfg      unacked.Config
	lookback     int
	pollInterval time.Duration
	now          func() time.Time
}

// NewUnackedBackstop builds the standing un-acked detector. coordinator resolves
// the wake target (cos_agent, else primary XO). wake may be nil when no injection
// is wired (alert-only mode).
func NewUnackedBackstop(cfg *roster.Config, reader RecentHistoryReader, statePath string, alert func(string), wake CoordinatorWake, coordinator func(*roster.Config) string) *UnackedBackstop {
	if coordinator == nil {
		coordinator = defaultCoordinator
	}
	return &UnackedBackstop{
		cfg:          cfg,
		reader:       reader,
		alert:        alert,
		wake:         wake,
		coordinator:  coordinator,
		store:        newUnackedStateStore(statePath, defaultUnackedRetention),
		scanCfg:      unacked.DefaultConfig(cfg.OperatorUserID),
		lookback:     unacked.DefaultLookback,
		pollInterval: unacked.DefaultScanInterval,
		now:          time.Now,
	}
}

func defaultCoordinator(cfg *roster.Config) string {
	if cfg.CosAgent != "" {
		return cfg.CosAgent
	}
	xo := cfg.XOAgent
	if xo == "" && len(cfg.Agents) > 0 {
		xo = cfg.Agents[0].Name
	}
	return xo
}

// Run is the single scan goroutine (one sweep on start, then each tick).
func (u *UnackedBackstop) Run(ctx context.Context) {
	ticker := time.NewTicker(u.pollInterval)
	defer ticker.Stop()
	u.sweep()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			u.sweep()
		}
	}
}

func (u *UnackedBackstop) sweep() {
	now := u.now()
	st, pruned := u.store.load(now)
	changed := pruned
	for _, b := range u.cfg.Bindings() {
		if c := u.sweepChannel(b, &st, now); c {
			changed = true
		}
	}
	if changed {
		if err := u.store.save(st, now); err != nil {
			log.Printf("flotilla watch: unacked state save failed: %v", err)
		}
	}
}

func (u *UnackedBackstop) sweepChannel(b roster.Channel, st *unackedState, now time.Time) bool {
	if u.reader == nil || u.cfg.OperatorUserID == "" {
		return false
	}
	ack := u.scanCfg.AckWindow
	if ack <= 0 {
		ack = unacked.DefaultAckWindow
	}
	cutoff := now.Add(-ack)
	raw, err := u.reader.RecentSince(b.ChannelID, cutoff)
	if err != nil {
		log.Printf("flotilla watch: unacked scan failed for %s: %v", channelLabel(b), err)
		return false
	}
	msgs := make([]unacked.Message, len(raw))
	for i, m := range raw {
		msgs[i] = unacked.FromTransport(m)
	}
	findings := unacked.Scan(msgs, b.ChannelID, now, u.scanCfg)
	if len(findings) == 0 {
		return false
	}
	var changed bool
	var newAlerts []unacked.Finding
	for _, f := range findings {
		idx, ok := st.index(f.ChannelID, f.MessageID)
		if !ok {
			newAlerts = append(newAlerts, f)
			st.Records = append(st.Records, alertedRecord{
				MessageID: f.MessageID,
				ChannelID: f.ChannelID,
				AlertedAt: now,
				WakeDone:  false,
			})
			changed = true
			idx = len(st.Records) - 1
		}
		if u.wake != nil && !st.Records[idx].WakeDone {
			if err := u.tryCoordinatorWake(b, f); err != nil {
				if errors.Is(err, surface.ErrBusy) {
					log.Printf("flotilla watch: unacked coordinator wake skipped for %s (busy mid-turn) — will retry next sweep; channel alert is the backstop", u.coordinator(u.cfg))
				} else {
					log.Printf("flotilla watch: unacked coordinator wake failed for %s: %v", u.coordinator(u.cfg), err)
				}
			} else {
				st.Records[idx].WakeDone = true
				changed = true
				log.Printf("flotilla watch: unacked coordinator wake delivered to %q", u.coordinator(u.cfg))
			}
		}
	}
	if len(newAlerts) > 0 && u.alert != nil {
		u.alert(formatUnackedDigest(b, newAlerts))
	}
	return changed
}

func (u *UnackedBackstop) tryCoordinatorWake(b roster.Channel, f unacked.Finding) error {
	agent := u.coordinator(u.cfg)
	if agent == "" {
		return fmt.Errorf("no coordinator agent configured")
	}
	body := fmt.Sprintf("[flotilla unacked-backstop] Operator message on %s (%s) has no fleet acknowledgment (%s, age %s):\n  id=%s\n  %q\nReview channel history and act — the alert above is the persistent backstop.",
		channelLabel(b), b.ChannelID, f.Reason, f.Age.Round(time.Minute), f.MessageID, f.Snippet)
	return u.wake(agent, body)
}

func formatUnackedDigest(b roster.Channel, findings []unacked.Finding) string {
	var bldr strings.Builder
	fmt.Fprintf(&bldr, "%d un-acked operator message(s) on %s — no fleet reply in channel:\n", len(findings), channelLabel(b))
	for _, f := range findings {
		fmt.Fprintf(&bldr, "  • [%s] id=%s age=%s — %q\n", f.Reason, f.MessageID, f.Age.Round(time.Minute), f.Snippet)
	}
	return bldr.String()
}
