//go:build !voiceopus

package main

import "fmt"

// cmdVoice (stub) — the voice process links libopus via CGO and is built ONLY with
// `-tags voiceopus`. The default core binary (CGO_ENABLED=0) ships this stub so `flotilla
// voice` fails with a clear, actionable message instead of being silently absent. This mirrors
// the internal/voice opus_stub.go isolation: the clock binary needs no libopus.
func cmdVoice(_ []string) error {
	return fmt.Errorf("flotilla voice requires a build with -tags voiceopus (CGO + libopus-dev); this binary was built without it")
}
