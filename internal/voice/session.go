package voice

// This file defines the voice-transport SEAM between the pipelines and Discord. The
// pipelines (inbound/outbound) are pure-Go and depend only on this interface, so they are
// fully unit-testable with a fake Session and never import discordgo. The real adapter
// (PR-C, the `flotilla voice` command) wraps a discordgo VoiceConnection — OpusSend /
// OpusRecv / VoiceSpeakingUpdate — and translates it into these channels. Keeping discordgo
// out of the engine is the same isolation spine as the rest of the design: the testable
// logic never touches the WIP voice gateway directly.

// Packet is one received audio frame: an Opus payload tagged with its source SSRC.
// Discord's received packets carry an SSRC but NO user id (the SSRC→user mapping arrives
// separately, via SpeakingEvent), which is why the operator gate is SSRC-based and
// fail-closed (see SpeakerTable).
type Packet struct {
	SSRC uint32
	Opus []byte
}

// SpeakingEvent maps an SSRC to a user id, as reported by Discord's "speaking start" event
// (discordgo VoiceSpeakingUpdate). It is the ONLY source that attributes received audio to
// a user; the inbound pipeline feeds every event into the SpeakerTable.
type SpeakingEvent struct {
	SSRC   uint32
	UserID string
}

// Session is the voice connection the pipelines run over: received audio + speaking
// attributions in, encoded frames out. In production it is a thin discordgo adapter; in
// tests it is a fake. All channels are owned by the Session — it closes them on Close.
type Session interface {
	// OpusRecv yields received audio frames (one 20 ms Opus packet each).
	OpusRecv() <-chan Packet
	// Speaking yields SSRC→user attributions from "speaking start" events.
	Speaking() <-chan SpeakingEvent
	// OpusSend accepts encoded 20 ms Opus frames to transmit (used by the outbound pipeline).
	OpusSend() chan<- []byte
	// Close tears down the voice connection and its channels.
	Close() error
}
