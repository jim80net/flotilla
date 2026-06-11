//go:build !voiceopus

package voice

import (
	"errors"
	"testing"
)

// In the default (no-tag, CGO_ENABLED=0) build, constructing a codec must fail with the
// sentinel — proving the core never links libopus and degrades with a clear, actionable
// error rather than a mysterious link failure.
func TestNewOpusCodecStubFailsClosed(t *testing.T) {
	c, err := NewOpusCodec()
	if c != nil {
		t.Errorf("stub returned a non-nil codec: %v", c)
	}
	if !errors.Is(err, ErrNoOpusCodec) {
		t.Errorf("err = %v, want ErrNoOpusCodec", err)
	}
}
