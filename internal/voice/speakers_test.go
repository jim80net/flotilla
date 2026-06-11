package voice

import (
	"sync"
	"testing"
)

// The gate is fail-closed: audio is the operator's ONLY when its SSRC is POSITIVELY
// mapped to the operator. Every non-positive case (no operator configured, an SSRC the
// speaking-event never mapped, or an SSRC mapped to someone else) must return false so
// the caller drops it rather than injecting unattributed audio as an XO command.
func TestSpeakerTableFailClosed(t *testing.T) {
	const op = "operator-123"

	t.Run("empty operator trusts nobody", func(t *testing.T) {
		tbl := NewSpeakerTable("")
		tbl.Note(7, op) // even a mapping to the (unconfigured) operator is not trusted
		if tbl.IsOperator(7) {
			t.Error("unconfigured operator must trust nobody (fail-closed)")
		}
	})

	t.Run("unmapped SSRC is not the operator", func(t *testing.T) {
		tbl := NewSpeakerTable(op)
		// No Note for ssrc 42 — a speaker already talking at join, or the first packets
		// before the speaking event lands. Must NOT be treated as the operator.
		if tbl.IsOperator(42) {
			t.Error("unmapped SSRC must not be the operator")
		}
	})

	t.Run("non-operator SSRC is dropped", func(t *testing.T) {
		tbl := NewSpeakerTable(op)
		tbl.Note(9, "someone-else")
		if tbl.IsOperator(9) {
			t.Error("a non-operator speaker must not pass the gate")
		}
	})

	t.Run("operator SSRC passes", func(t *testing.T) {
		tbl := NewSpeakerTable(op)
		tbl.Note(5, op)
		if !tbl.IsOperator(5) {
			t.Error("an SSRC positively mapped to the operator must pass")
		}
	})

	t.Run("zero SSRC and empty user id are rejected", func(t *testing.T) {
		tbl := NewSpeakerTable(op)
		tbl.Note(0, op) // SSRC 0 — Discord never assigns it; the malformed-packet zero value
		tbl.Note(5, "") // empty user id — a meaningless mapping
		if tbl.IsOperator(0) {
			t.Error("SSRC 0 must never be a trusted mapping")
		}
		if tbl.IsOperator(5) {
			t.Error("an empty-user-id mapping must not exist (Note rejected it)")
		}
	})

	t.Run("high-bit SSRC round-trips int→uint32", func(t *testing.T) {
		// discordgo reports VoiceSpeakingUpdate.SSRC as int; a wire SSRC with the high bit
		// set arrives as a NEGATIVE int. The §3b caller converts with a bare uint32(...),
		// which must match the uint32 the audio Packet carries for the same speaker. Pin
		// that the conversion preserves the bit pattern end-to-end.
		var wire uint32 = 0x80000001  // a var, not a const: int32(wire) must wrap at runtime
		ssrcAsInt := int(int32(wire)) // how discordgo surfaces it (negative)
		tbl := NewSpeakerTable(op)
		tbl.Note(uint32(ssrcAsInt), op) // caller's bare cast
		if !tbl.IsOperator(wire) {      // packet's uint32
			t.Errorf("high-bit SSRC %#x did not round-trip int(%d)→uint32", wire, ssrcAsInt)
		}
	})

	t.Run("Forget revokes a mapping so a reused SSRC cannot inherit identity", func(t *testing.T) {
		tbl := NewSpeakerTable(op)
		tbl.Note(5, op)
		tbl.Forget(5)
		if tbl.IsOperator(5) {
			t.Error("a forgotten SSRC must revert to not-the-operator")
		}
		// A different user reusing SSRC 5 must NOT inherit the operator's identity.
		tbl.Note(5, "someone-else")
		if tbl.IsOperator(5) {
			t.Error("a reused SSRC must take the new speaker's identity, not the prior one")
		}
	})
}

// The table is read on every inbound packet and written on every speaking event, from
// different goroutines — it must be race-free under -race.
func TestSpeakerTableConcurrent(t *testing.T) {
	const op = "operator-123"
	tbl := NewSpeakerTable(op)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)
		ssrc := uint32(i % 8)
		go func() { defer wg.Done(); tbl.Note(ssrc, op) }()
		go func() { defer wg.Done(); _ = tbl.IsOperator(ssrc) }()
		go func() { defer wg.Done(); tbl.Forget(ssrc) }()
	}
	wg.Wait()
}
