package voice

import "sync"

// SpeakerTable is the POSITIVE, fail-closed operator gate for inbound voice (design
// P1-1). Discord's received audio packets carry an SSRC but NO user id; the only
// SSRC→user source is the "speaking" event (discordgo VoiceSpeakingUpdate), which the
// caller feeds in via Note. Audio is treated as the operator's ONLY when its SSRC is
// positively mapped to the configured operator user id — an UNMAPPED SSRC (a speaker
// already talking at join, or the first packets before the speaking event lands) or a
// NON-operator SSRC is never the operator. The caller drops everything IsOperator
// rejects, so unattributed audio is never injected as an XO command.
type SpeakerTable struct {
	mu       sync.RWMutex
	operator string            // operator_user_id; empty ⇒ nobody is trusted (fail-closed)
	bySSRC   map[uint32]string // ssrc → user id, seeded from speaking events
}

// NewSpeakerTable creates the gate for a given operator user id.
func NewSpeakerTable(operatorUserID string) *SpeakerTable {
	return &SpeakerTable{operator: operatorUserID, bySSRC: map[uint32]string{}}
}

// Note records an SSRC→user mapping observed from a "speaking start" event. (discordgo
// reports SSRC as an int; the received audio Packet uses uint32 — the caller converts via
// a bare uint32(...) cast, which round-trips the 32-bit wire value even when the high bit
// makes the int negative.) A zero SSRC (Discord never assigns it; also the zero-value a
// malformed/empty packet would carry) or an empty user id is a meaningless mapping and is
// rejected — defense in depth so the map only ever holds real attributions, even though
// IsOperator is already fail-closed against both.
func (t *SpeakerTable) Note(ssrc uint32, userID string) {
	if ssrc == 0 || userID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.bySSRC[ssrc] = userID
}

// IsOperator reports whether an audio SSRC is POSITIVELY mapped to the operator.
// Fail-closed: an unconfigured operator, an unmapped SSRC, or a non-operator SSRC all
// return false — never inject unattributed or non-operator audio.
func (t *SpeakerTable) IsOperator(ssrc uint32) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.operator == "" {
		return false
	}
	uid, ok := t.bySSRC[ssrc]
	return ok && uid == t.operator
}

// Forget drops an SSRC mapping (e.g. on a speaking-stop or a user leaving), so a reused
// SSRC cannot inherit a prior speaker's identity.
func (t *SpeakerTable) Forget(ssrc uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.bySSRC, ssrc)
}
