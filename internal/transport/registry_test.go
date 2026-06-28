package transport

import (
	"context"
	"testing"
)

// fakeTransport is a minimal Transport for registry/capability tests. It implements
// only what the registry needs; the bus-behavior methods are inert.
type fakeTransport struct{ name string }

func (f *fakeTransport) Name() string { return f.name }
func (f *fakeTransport) Subscribe(context.Context, []Destination, MessageHandler, func()) error {
	return nil
}
func (f *fakeTransport) Destinations([]string) []Destination    { return nil }
func (f *fakeTransport) Post(Destination, string, string) error { return nil }
func (f *fakeTransport) ResolveDestination(string, string) (Destination, string, bool) {
	return nil, "", false
}
func (f *fakeTransport) MaxContentRunes() int       { return 2000 }
func (f *fakeTransport) Chunk(text string) []string { return []string{text} }
func (f *fakeTransport) Close() error               { return nil }

func TestRegistry_ResolvesByName(t *testing.T) {
	// Register under a non-default name so this test is independent of whether the
	// discord transport has been constructed.
	const name = "fake-registry"
	prev, hadPrev := registry[name]
	t.Cleanup(func() {
		if hadPrev {
			registry[name] = prev
		} else {
			delete(registry, name)
		}
	})

	Register(&fakeTransport{name: name})
	got, ok := Get(name)
	if !ok {
		t.Fatalf("Get(%q) ok=false, want a registered transport", name)
	}
	if got.Name() != name {
		t.Errorf("Get(%q).Name() = %q, want %q", name, got.Name(), name)
	}
}

func TestRegistry_EmptyNameResolvesToDiscord(t *testing.T) {
	// An empty name MUST resolve to DefaultTransport ("discord") so a roster naming
	// no medium behaves exactly as before this change.
	if DefaultTransport != "discord" {
		t.Fatalf("DefaultTransport = %q, want \"discord\"", DefaultTransport)
	}
	prev, hadPrev := registry[DefaultTransport]
	t.Cleanup(func() {
		if hadPrev {
			registry[DefaultTransport] = prev
		} else {
			delete(registry, DefaultTransport)
		}
	})

	sentinel := &fakeTransport{name: DefaultTransport}
	Register(sentinel)
	got, ok := Get("")
	if !ok {
		t.Fatal("Get(\"\") ok=false, want the default (discord) transport")
	}
	if got != sentinel {
		t.Errorf("Get(\"\") resolved to %v, want the default-registered transport", got)
	}
}

func TestRegistry_UnknownNameNotOK(t *testing.T) {
	if _, ok := Get("no-such-transport"); ok {
		t.Error("Get of an unregistered name returned ok=true, want false")
	}
}

func TestConstruct_UnknownNameErrors(t *testing.T) {
	if _, err := Construct("no-such-transport", Config{}); err == nil {
		t.Error("Construct of an unregistered name returned nil error, want a clear error")
	}
}
