package transport

import (
	"fmt"
	"sort"

	"github.com/jim80net/flotilla/internal/roster"
)

// DefaultTransport is used when a configuration names no coordination medium; it
// resolves to discord so a roster that predates the Transport SPI behaves exactly
// as before this change.
const DefaultTransport = "discord"

// registry holds CONSTRUCTED transports, keyed by name — the live instance Get
// returns. It mirrors surface's name-keyed Driver registry (internal/surface
// Register/Get), with one divergence: a Transport is stateful, so the live instance
// is produced by Construct at daemon start (not a zero-value registered at init).
var registry = map[string]Transport{}

// factories holds INIT-TIME transport factories, keyed by name. A transport's
// init() calls RegisterFactory; Construct invokes the named factory (with the bot
// token + destinations + cursor path that are unavailable at init) to build the
// live instance. This is the REGISTRATION-vs-CONSTRUCTION split a stateful transport
// requires — a surface.Driver, being stateless, needs only the former.
var factories = map[string]Factory{}

// Config carries the runtime parameters a transport's Factory needs to build its
// live instance — the values that are NOT available at init() (loaded at daemon
// start from the roster + secrets). It is medium-agnostic: a transport reads only
// the fields it needs (discord reads BotToken + Destinations + CursorPath; a web
// transport would read a bind address it adds here later).
type Config struct {
	// BotToken authenticates the discord gateway + REST sessions.
	BotToken string
	// CursorPath is the durable per-destination catch-up cursor file (the discord
	// transport's at-least-once backstop state). Empty ⇒ the caller wires no catch-up.
	CursorPath string
	// Roster is the config-level identity the transport CONSUMES to resolve a
	// destination: the channel→XO bindings (BindingForChannel) the addressing seam
	// reads. It is the config binding, distinct from the transport mechanism (see the
	// design's "roster.Channel (config) vs transport.Transport (mechanism)" note).
	Roster *roster.Config
	// Secrets resolves an agent's channel-bound webhook URL (the discord credential
	// kept INSIDE the transport's Destination, never crossing the seam to a caller).
	Secrets *roster.Secrets
}

// Factory builds a live Transport from its runtime Config. Registered at init() by
// each transport; invoked once per daemon run by Construct.
type Factory func(Config) (Transport, error)

// Register adds a CONSTRUCTED transport to the registry (so Get resolves it by
// name). Construct calls this after a Factory builds the live instance; a test may
// call it directly with a fake. Mirrors surface.Register.
func Register(t Transport) { registry[t.Name()] = t }

// Get resolves a constructed transport by name; an empty name resolves to
// DefaultTransport. Mirrors surface.Get. ok=false ⇒ no transport of that name has
// been constructed/registered.
func Get(name string) (Transport, bool) {
	if name == "" {
		name = DefaultTransport
	}
	t, ok := registry[name]
	return t, ok
}

// RegisterFactory registers a transport's init-time factory under name. Called from
// each transport's init() (discord.go's init registers the discord factory). A
// duplicate name is a programming error (two transports claiming the same key).
func RegisterFactory(name string, f Factory) { factories[name] = f }

// Construct builds the live transport named name from cfg, registers it (so Get
// resolves it), and returns it. An empty name resolves to DefaultTransport. It is
// the daemon-start construction step the stateful-transport lifecycle requires. An
// unknown name (no registered factory) is a clear error, never a silent nil.
func Construct(name string, cfg Config) (Transport, error) {
	if name == "" {
		name = DefaultTransport
	}
	f, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown transport %q (known: %v)", name, knownFactories())
	}
	t, err := f(cfg)
	if err != nil {
		return nil, err
	}
	Register(t)
	return t, nil
}

// knownFactories returns the registered factory names, sorted, for a clear error.
func knownFactories() []string {
	out := make([]string, 0, len(factories))
	for name := range factories {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
