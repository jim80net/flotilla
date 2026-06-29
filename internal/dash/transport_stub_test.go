package dash

import (
	"context"

	"github.com/jim80net/flotilla/internal/transport"
)

// stubTransport is a no-op transport.Transport for the dash server tests: NewServer
// now requires a coordination Transport (the notify's post medium), so the test
// helpers inject this stub. The dash control tests that actually exercise notify
// behavior live in internal/dash/control (against the real LibraryController seams);
// these server-level tests exercise the HTTP/read surfaces through a fakeController,
// so the stub only needs to satisfy the interface and report the Discord 2000-rune
// cap (the value NewLibrary reads at construction).
type stubTransport struct{}

func (stubTransport) Name() string { return "stub" }
func (stubTransport) Subscribe(context.Context, []transport.Destination, transport.MessageHandler, func()) error {
	return nil
}
func (stubTransport) Destinations([]string) []transport.Destination    { return nil }
func (stubTransport) Post(transport.Destination, string, string) error { return nil }
func (stubTransport) ResolveDestination(string, string) (transport.Destination, string, bool) {
	return nil, "", false
}
func (stubTransport) MaxContentRunes() int       { return 2000 }
func (stubTransport) Chunk(text string) []string { return []string{text} }
func (stubTransport) Close() error               { return nil }
