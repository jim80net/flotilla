//go:build voiceopus

package voice

import (
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// JoinVoice joins a voice channel and adapts it to a Session — the real Connector for the
// recovery Supervisor. It joins with mute=false, deaf=FALSE (deaf would stop OpusRecv, so we
// could not hear the operator). Returns the Session, its Lost channel (closed on a drop), and
// a cleanup func. Call only after the owning discordgo session's gateway is Open.
func JoinVoice(dg *discordgo.Session, guildID, channelID string) (Session, <-chan struct{}, func(), error) {
	vc, err := dg.ChannelVoiceJoin(guildID, channelID, false, false)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("voice: join channel %s/%s: %w", guildID, channelID, err)
	}
	ds := newDiscordSession(vc)
	return ds, ds.lost, func() { _ = ds.Close() }, nil
}

// discord_session.go adapts a discordgo voice connection to the Session seam. It is
// voiceopus-tagged (voice-process-only) so the core CGO_ENABLED=0 binary never compiles the
// voice transport. discordgo's voice support is self-described WIP, so this layer is kept
// THIN — it only translates channels/events — and the genuinely untestable live behavior
// (does a join succeed; does OpusRecv close on a drop) is isolated here behind the Session /
// Connector seams, which the pipelines and the recovery Supervise are tested against.
const (
	recvBuffer   = 256                    // re-buffer above discordgo's buffer-2 OpusRecv so a brief consumer stall (STT) doesn't drop frames
	speakBuffer  = 64                     // buffered speaking events
	sendBuffer   = 256                    // outbound frame buffer
	speakingIdle = 300 * time.Millisecond // stop the Discord "speaking" indicator after this gap with no frames
)

// discordSession implements Session over a *discordgo.VoiceConnection.
type discordSession struct {
	vc        *discordgo.VoiceConnection
	recv      chan Packet
	speak     chan SpeakingEvent
	send      chan []byte
	done      chan struct{}
	lost      chan struct{}
	closeOnce sync.Once
	lostOnce  sync.Once
}

// newDiscordSession wraps a READY voice connection (call only after a successful
// ChannelVoiceJoin, when discordgo has created vc.OpusSend/OpusRecv). It starts the receive
// and send pumps and registers the speaking-event handler.
func newDiscordSession(vc *discordgo.VoiceConnection) *discordSession {
	ds := &discordSession{
		vc:    vc,
		recv:  make(chan Packet, recvBuffer),
		speak: make(chan SpeakingEvent, speakBuffer),
		send:  make(chan []byte, sendBuffer),
		done:  make(chan struct{}),
		lost:  make(chan struct{}),
	}
	vc.AddHandler(func(_ *discordgo.VoiceConnection, vsu *discordgo.VoiceSpeakingUpdate) {
		// The speaking-start event is the ONLY SSRC→user attribution source (the gate's seed).
		// Convert discordgo's int SSRC with a bare cast (the 32-bit wire value round-trips).
		// Drop if our buffer is full — the gate re-seeds on the next event.
		select {
		case ds.speak <- SpeakingEvent{SSRC: uint32(vsu.SSRC), UserID: vsu.UserID}:
		default:
		}
	})
	go ds.recvPump()
	go ds.sendPump()
	return ds
}

func (ds *discordSession) OpusRecv() <-chan Packet        { return ds.recv }
func (ds *discordSession) Speaking() <-chan SpeakingEvent { return ds.speak }
func (ds *discordSession) OpusSend() chan<- []byte        { return ds.send }

// Close tears down the session (idempotent). It stops the pumps and disconnects from the
// voice channel (best-effort — on a drop the connection is already gone).
func (ds *discordSession) Close() error {
	ds.closeOnce.Do(func() { close(ds.done) })
	_ = ds.vc.Disconnect()
	return nil
}

// recvPump copies received Opus packets onto our buffered channel, gated to the operator far
// downstream.
//
// The `lost` channel (consumed by Supervise as the reconnect trigger) closes if OpusRecv
// closes. HONEST CAVEAT for discordgo v0.29.0: opusReceiver NEVER closes OpusRecv — on a udp
// drop it spawns discordgo's OWN internal reconnect (voice.go: `go v.reconnect()`) which
// reuses the same channel — so this `lost` signal does NOT fire for ordinary drops, and the
// session is instead self-healed inside discordgo. Supervise's reconnect policy is therefore
// exercised today only on initial-connect failure + clean shutdown; a genuine drop→reconnect
// liveness signal (inter-packet/heartbeat watchdog that distinguishes a drop from normal
// push-to-talk silence) is tracked as a follow-up (issue #42). `lost` is kept (correct,
// guarded by lostOnce) so that signal slots in without reworking the seam.
func (ds *discordSession) recvPump() {
	for {
		select {
		case <-ds.done:
			return
		case pkt, ok := <-ds.vc.OpusRecv:
			if !ok {
				ds.lostOnce.Do(func() { close(ds.lost) }) // discordgo closed OpusRecv (rare on v0.29.0)
				return
			}
			if pkt == nil || len(pkt.Opus) == 0 {
				continue
			}
			select {
			case ds.recv <- Packet{SSRC: pkt.SSRC, Opus: pkt.Opus}:
			case <-ds.done:
				return
			default:
				// Buffer full (consumer stalled, e.g. during STT) — drop rather than block
				// discordgo's receiver. A frame lost mid-transcription is fine for push-to-talk.
			}
		}
	}
}

// sendPump forwards outbound frames to discordgo, toggling the Discord "speaking" indicator
// on activity (required — Discord drops audio from a non-speaking sender) and off after an
// idle gap. One reused idle timer (rearm/stopTimer from inbound.go).
func (ds *discordSession) sendPump() {
	speaking := false
	idle := time.NewTimer(speakingIdle)
	stopTimer(idle)
	defer idle.Stop()
	setSpeaking := func(on bool) {
		if on != speaking {
			_ = ds.vc.Speaking(on) // best-effort; a missed toggle only affects the indicator
			speaking = on
		}
	}
	for {
		select {
		case <-ds.done:
			setSpeaking(false)
			return
		case frame := <-ds.send:
			setSpeaking(true)
			select {
			case ds.vc.OpusSend <- frame:
			case <-ds.done:
				return
			}
			rearm(idle, speakingIdle)
		case <-idle.C:
			setSpeaking(false)
		}
	}
}
