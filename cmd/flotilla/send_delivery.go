package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/dispatch"
	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/outbox"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
)

// Sender-side retry policy for a bounced `flotilla send` (#475). Inline retries cover short
// busy windows; exhausted attempts fall through to the durable per-sender outbox.
const (
	sendRetryInitial = 5 * time.Second
	sendRetryMax     = 60 * time.Second
	// Three quick attempts (~5s + ~10s sleeps) then queue — the sending desk must not
	// block for minutes; the durable outbox + watch sweep carry the long busy window.
	sendRetryMaxAttempts = 3
)

func deliverSendOnce(drv surface.Driver, pane, message string) error {
	confirm := surface.Confirm{SendEnter: deliver.SendEnter, Sleep: time.Sleep}
	if surface.SelfHealEnabled() {
		confirm.SendCtrlC = deliver.SendCtrlC
	}
	return confirm.SubmitWithSelfHeal(drv, pane, message)
}

func deliverSendWithRetry(drv surface.Driver, pane, agentName, message string) error {
	wait := sendRetryInitial
	for attempt := 1; attempt <= sendRetryMaxAttempts; attempt++ {
		txn, err := deliver.AcquirePaneTxn(pane, deliver.PaneTxnTimeout)
		if err != nil {
			if attempt < sendRetryMaxAttempts {
				time.Sleep(wait)
				wait = nextSendRetryWait(wait)
				continue
			}
			return fmt.Errorf("%w: %v", errRetryableBusy{agent: agentName}, err)
		}
		err = deliverSendOnce(drv, pane, message)
		txn.Release()
		if err == nil {
			return nil
		}
		if !errors.Is(err, surface.ErrBusy) && !errors.Is(err, surface.ErrTransient) {
			return mapSendDeliveryError(agentName, err)
		}
		if attempt >= sendRetryMaxAttempts {
			break
		}
		time.Sleep(wait)
		wait = nextSendRetryWait(wait)
	}
	return fmt.Errorf("%w", errRetryableBusy{agent: agentName})
}

type errRetryableBusy struct{ agent string }

func (e errRetryableBusy) Error() string {
	return fmt.Sprintf("%s is busy (mid-turn) — NOT delivered after %d retries", e.agent, sendRetryMaxAttempts)
}

func (e errRetryableBusy) Unwrap() error { return surface.ErrBusy }

func mapSendDeliveryError(agentName string, err error) error {
	switch {
	case errors.Is(err, surface.ErrBusy):
		return fmt.Errorf("%s is busy (mid-turn) — NOT delivered; retry when it is idle", agentName)
	case errors.Is(err, surface.ErrTransient):
		return fmt.Errorf("%s pane state is uncertain — NOT delivered; retry", agentName)
	case errors.Is(err, surface.ErrCrashed):
		return fmt.Errorf("%s is at a shell (crashed) — NOT delivered", agentName)
	case errors.Is(err, surface.ErrPanelBlocked):
		return fmt.Errorf("%s is input-blocked behind the Claude Code agents panel — NOT delivered; it needs a human keystroke or click into the composer at its pane, then retry", agentName)
	default:
		return fmt.Errorf("delivery to %s could not be confirmed: %w", agentName, err)
	}
}

func nextSendRetryWait(cur time.Duration) time.Duration {
	next := cur * 2
	if next > sendRetryMax {
		return sendRetryMax
	}
	return next
}

// recordDirectInboundTrack writes the recipient inbound ledger on CLI direct-delivery success
// (#494). Daemon-swept sends use the same primitive via InboundTrackHook on confirm.
// TrackConfirmedSend journals skipped|recorded reason=… (#498). A coordinator recipient keeps
// no pending row; its dispatch settles straight into the consumed registry instead (#707), so
// the footer's dispatch-ack and dispatch-status stay answerable.
func recordDirectInboundTrack(cfg *roster.Config, rosterPath, sender, recipient, message string) {
	rosterDir := filepath.Dir(rosterPath)
	decision, err := inbound.TrackConfirmedSend(rosterDir, sender, recipient, message, "", cfg.IsCoordinator)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flotilla: inbound track %q from %q failed: %v\n", recipient, sender, err)
	}
	if decision == inbound.TrackSkipped {
		if _, err := dispatch.ConsumeCoordinatorRecipient(rosterDir, sender, recipient, message); err != nil {
			fmt.Fprintf(os.Stderr, "flotilla: consume coordinator dispatch for %q failed: %v\n", recipient, err)
		}
	}
}

func enqueueOrFailSend(rosterPath, sender, recipient, message string, deliveryErr error) error {
	rosterDir := filepath.Dir(rosterPath)
	id, deduped, err := outbox.Enqueue(rosterDir, sender, recipient, message)
	if err != nil {
		return fmt.Errorf("%v; durable outbox enqueue also failed: %w", deliveryErr, err)
	}
	// Desk-visible machine-readable ack first (#475 / #614) so monitors can grep QUEUED.
	fmt.Println(dispatch.FormatQueuedAck(id, sender, recipient, deduped))
	if deduped {
		fmt.Fprintf(os.Stderr, "flotilla: %v — send already queued as %s; no duplicate added\n", deliveryErr, id)
		fmt.Printf("send already queued as %s — will deliver when %s is idle\n", id, recipient)
		return nil
	}
	fmt.Fprintf(os.Stderr, "flotilla: %v — queued to durable outbox (id=%s); watch will deliver when %s is idle\n", deliveryErr, id, recipient)
	fmt.Printf("queued to %s outbox (id %s) — will deliver when recipient is idle\n", sender, id)
	return nil
}

// deliverOrQueueSend attempts confirmed delivery with inline retry; on sustained busy/transient
// failure it enqueues to the sender's durable outbox and returns queued=true (not delivered).
func deliverOrQueueSend(cfg *roster.Config, rosterPath, sender, recipient string, drv surface.Driver, pane, message string) (queued bool, err error) {
	err = deliverSendWithRetry(drv, pane, recipient, message)
	if err == nil {
		fmt.Printf("delivered to %s (pane %s) — turn confirmed\n", recipient, pane)
		mirrorSendToLedger(cfg, sender, recipient, message)
		recordDirectInboundTrack(cfg, rosterPath, sender, recipient, message)
		return false, nil
	}
	var busy errRetryableBusy
	if errors.As(err, &busy) || errors.Is(err, surface.ErrBusy) || errors.Is(err, surface.ErrTransient) {
		return true, enqueueOrFailSend(rosterPath, sender, recipient, message, err)
	}
	return false, err
}
