//go:build !voiceopus

package voice

import "errors"

// ErrNoOpusCodec is returned by NewOpusCodec in a build WITHOUT the `voiceopus` tag. The
// core flotilla binary is built CGO_ENABLED=0 and must never link libopus; this stub is
// what makes that possible — the Opus codec exists only in the voice process, which is
// built with `-tags voiceopus` (CGO + libopus-dev). If the core ever tries to construct a
// codec, it gets this error instead of a link failure.
var ErrNoOpusCodec = errors.New("voice: built without the opus codec — rebuild the voice process with `-tags voiceopus` (requires CGO_ENABLED=1 and libopus-dev)")

// NewOpusCodec (stub) always fails: this build has no libopus linked.
func NewOpusCodec() (OpusCodec, error) {
	return nil, ErrNoOpusCodec
}
