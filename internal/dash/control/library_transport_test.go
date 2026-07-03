package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/transport"
)

// fakeNotifyTransport is a discord-backed Transport stand-in for the notify seam:
// it records the destination + identity Post was called with and reports a
// content cap. It carries ONLY the methods Notify exercises (Post, MaxContentRunes);
// the rest of the Transport interface is unused by the notify path, so they are
// no-op/zero — the notify seam depends on a transport VALUE, not on a concrete
// medium, which is the whole point of the re-point (the dash control library no
// longer imports internal/discord).
type fakeNotifyTransport struct {
	postDest    transport.Destination
	postUser    string
	postContent string
	postCalls   int
	postErr     error
	maxRunes    int
}

func (f *fakeNotifyTransport) Name() string { return "fake" }
func (f *fakeNotifyTransport) Subscribe(context.Context, []transport.Destination, transport.MessageHandler, func()) error {
	return nil
}
func (f *fakeNotifyTransport) Destinations([]string) []transport.Destination { return nil }
func (f *fakeNotifyTransport) Post(dest transport.Destination, username, content string) error {
	f.postCalls++
	f.postDest, f.postUser, f.postContent = dest, username, content
	return f.postErr
}
func (f *fakeNotifyTransport) PostWithAttachments(dest transport.Destination, username, content string, _ []string) error {
	return f.Post(dest, username, content)
}
func (f *fakeNotifyTransport) ResolveDestination(string, string) (transport.Destination, string, bool) {
	return nil, "", false
}
func (f *fakeNotifyTransport) MaxContentRunes() int       { return f.maxRunes }
func (f *fakeNotifyTransport) Chunk(text string) []string { return []string{text} }
func (f *fakeNotifyTransport) Close() error               { return nil }

// newTransportTestController builds a LibraryController over an in-memory roster +
// secrets file, injecting fakeTr as the notify transport and recording the cos
// seam — but NOT overriding c.post, so the notify exercises the injected
// transport's Post (the re-point under test).
func newTransportTestController(t *testing.T, fakeTr transport.Transport) (*LibraryController, *capture) {
	t.Helper()
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(rosterCos), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatalf("roster.Load: %v", err)
	}
	secretsPath := filepath.Join(dir, "secrets.env")
	if err := os.WriteFile(secretsPath, []byte(secretsXO), 0o600); err != nil {
		t.Fatal(err)
	}
	// This suite exercises the NOTIFY (outbound) path only, so the web (inbound) transport
	// is an unused fake; fakeTr is the discord-backed notify transport under test.
	c := NewLibrary(rc, "xo", secretsPath, fakeTr, &fakeNotifyTransport{maxRunes: 2000})
	cap := &capture{}
	c.appendCos = cap.append
	c.now = func() time.Time { return fixedTime }
	return c, cap
}

// TestNotify_PostsThroughInjectedTransport pins the outbound re-point (task 1.2):
// the notify's post seam is satisfied by the injected transport.Transport.Post —
// not internal/discord.Post — and the resolved webhook reaches it as a webhook
// Destination (the wiring-boundary pattern watch.go uses for its down-alert post).
func TestNotify_PostsThroughInjectedTransport(t *testing.T) {
	fakeTr := &fakeNotifyTransport{maxRunes: 2000}
	c, cap := newTransportTestController(t, fakeTr)

	if err := c.Notify(context.Background(), "fleet, stand by"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if fakeTr.postCalls != 1 {
		t.Fatalf("transport.Post calls = %d, want 1 (notify must post through the injected transport)", fakeTr.postCalls)
	}
	if fakeTr.postUser != dashProvenance {
		t.Errorf("post username = %q, want %q", fakeTr.postUser, dashProvenance)
	}
	if fakeTr.postContent != "fleet, stand by" {
		t.Errorf("post content = %q", fakeTr.postContent)
	}
	// The resolved webhook reaches Post as a webhook Destination (opaque to the
	// caller, carrying the credential inside the transport) — the credential is NOT
	// a stringly-typed leak. The destination must be the one built from the XO's
	// resolved webhook.
	wantDest := transport.NewWebhookDestination("https://discord.example/webhook/xo")
	if fakeTr.postDest != wantDest {
		t.Errorf("post destination = %#v, want the webhook destination %#v", fakeTr.postDest, wantDest)
	}
	// Behavior preserved: the CoS mirror still records with dash provenance.
	if len(cap.ledger) != 1 || cap.ledger[0].From != dashProvenance || cap.ledger[0].To != "xo" {
		t.Errorf("ledger = %+v, want one dash-provenance entry to xo", cap.ledger)
	}
}

// TestNotify_OverLengthReadsTransportCap pins task 1.2's second half: the
// over-length guard reads the limit from transport.MaxContentRunes(), not a
// hard-coded discord constant. A transport reporting a 10-rune cap rejects an
// 11-rune note — proving the cap is sourced from the transport, not leaked.
func TestNotify_OverLengthReadsTransportCap(t *testing.T) {
	fakeTr := &fakeNotifyTransport{maxRunes: 10}
	c, _ := newTransportTestController(t, fakeTr)

	if err := c.Notify(context.Background(), "0123456789A"); !errors.Is(err, ErrOverLength) { // 11 runes > 10
		t.Errorf("11 runes over a 10-cap transport → %v, want ErrOverLength", err)
	}
	if fakeTr.postCalls != 0 {
		t.Error("an over-length note must not post")
	}
	// At exactly the cap it posts (boundary is inclusive — same as the discord 2000 guard).
	if err := c.Notify(context.Background(), "0123456789"); err != nil { // 10 runes == cap
		t.Errorf("10 runes at a 10-cap transport → %v, want a successful post", err)
	}
}
