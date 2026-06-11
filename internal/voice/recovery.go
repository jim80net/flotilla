package voice

import (
	"context"
	"time"
)

// recovery.go is the §3.4 voice-session recovery POLICY, deliberately isolated from the
// (untestable, discordgo-WIP) voice transport behind the Connector seam so it can be unit
// tested in full. discordgo's voice support is self-described work-in-progress, so a
// mid-session gateway drop is expected; Supervise re-establishes the connection, and on a
// drop it tears the session down — discarding any in-flight utterance, because a fresh
// session/pipeline starts with an empty buffer, so a half-captured command is never injected
// late (the design's "no stale-audio replay" invariant).

// Connector establishes one voice session. It returns the Session, a `lost` channel that is
// closed when the connection drops, and a cleanup func to tear the session down. The real
// connector (the `flotilla voice` command) does a discordgo ChannelVoiceJoin; tests fake it.
type Connector func(ctx context.Context) (sess Session, lost <-chan struct{}, cleanup func(), err error)

// SessionRunner runs the pipelines over a session and BLOCKS until ctx is cancelled (it
// owns the inbound + outbound pipelines for that session). The command supplies it closing
// over the codec/provider/meter/gate/injector. Because each session gets a fresh runner,
// no audio state crosses a reconnect.
type SessionRunner func(ctx context.Context, sess Session)

// SuperviseConfig bounds reconnection. Zero values get the documented defaults.
type SuperviseConfig struct {
	// ReconnectDelay is the wait between connection attempts. Default 2s.
	ReconnectDelay time.Duration
	// MaxAttempts is the number of CONSECUTIVE failed connects tolerated before giving up
	// (reset to zero by any successful connect). Default 5.
	MaxAttempts int
}

func (c SuperviseConfig) withDefaults() SuperviseConfig {
	if c.ReconnectDelay <= 0 {
		c.ReconnectDelay = 2 * time.Second
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 5
	}
	return c
}

// Supervise keeps a voice session alive until ctx is cancelled: connect → run the pipelines →
// on a DROP, stop the pipelines, tear the session down, and reconnect (up to MaxAttempts
// consecutive connect failures, then emit a one-line operator notice and return the last
// error). A ctx cancellation (operator shutdown) returns ctx.Err() cleanly, with no notice.
func Supervise(ctx context.Context, connect Connector, run SessionRunner, notice func(string), cfg SuperviseConfig) error {
	cfg = cfg.withDefaults()
	if notice == nil {
		notice = func(string) {}
	}
	fails := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		sess, lost, cleanup, err := connect(ctx)
		if err != nil {
			fails++
			if fails >= cfg.MaxAttempts {
				notice("voice: could not establish the voice connection — giving up; restart `flotilla voice` to retry.")
				return err
			}
			if !sleep(ctx, cfg.ReconnectDelay) {
				return ctx.Err()
			}
			continue
		}
		fails = 0

		// Run the pipelines for this session in the background; block until they finish.
		rctx, rcancel := context.WithCancel(ctx)
		runDone := make(chan struct{})
		go func() { run(rctx, sess); close(runDone) }()

		select {
		case <-ctx.Done():
			// Operator shutdown: stop the pipelines, wait for them, tear down, return clean.
			rcancel()
			<-runDone
			cleanup()
			return ctx.Err()
		case <-lost:
			// Gateway drop: stop the pipelines (no late inject), wait, tear down, reconnect.
			rcancel()
			<-runDone
			cleanup()
			notice("voice: connection dropped — reconnecting.")
			if !sleep(ctx, cfg.ReconnectDelay) {
				return ctx.Err()
			}
		}
	}
}

// sleep waits d or until ctx is cancelled; it reports whether the wait completed (true) or
// ctx fired (false).
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
