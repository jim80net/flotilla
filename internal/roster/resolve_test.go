package roster

import (
	"errors"
	"testing"
)

// rosterFromAgents builds a Config with the given agent names (no file I/O) — the
// resolver only reads Agents + the passed-in xo.
func rosterFromAgents(names ...string) *Config {
	c := &Config{}
	for _, n := range names {
		c.Agents = append(c.Agents, Agent{Name: n})
	}
	return c
}

// TestResolveTarget is the shared roster-wide resolver — the ONE implementation both
// the dash control library and the web transport call (so the exact-wins-else-
// ambiguous rule cannot drift between them). It pins: empty → the XO; "@name"/"name"
// → the canonical agent (case-insensitive); an exact case-sensitive match wins over a
// case-insensitive collision; a case-insensitive collision with NO exact match is
// rejected as ambiguous; an unknown target errors.
func TestResolveTarget(t *testing.T) {
	c := rosterFromAgents("xo", "alpha", "Alpha")

	t.Run("empty target goes to the XO", func(t *testing.T) {
		got, err := c.ResolveTarget("xo", "")
		if err != nil || got != "xo" {
			t.Fatalf("empty → (%q, %v), want (xo, nil)", got, err)
		}
	})

	t.Run("at-prefix and whitespace are trimmed", func(t *testing.T) {
		got, err := c.ResolveTarget("xo", "  @xo  ")
		if err != nil || got != "xo" {
			t.Fatalf("'  @xo  ' → (%q, %v), want (xo, nil)", got, err)
		}
	})

	t.Run("exact case-sensitive match wins over a collision", func(t *testing.T) {
		got, err := c.ResolveTarget("xo", "Alpha")
		if err != nil || got != "Alpha" {
			t.Fatalf("'Alpha' → (%q, %v), want (Alpha, nil) — exact match is unambiguous", got, err)
		}
		got, err = c.ResolveTarget("xo", "alpha")
		if err != nil || got != "alpha" {
			t.Fatalf("'alpha' → (%q, %v), want (alpha, nil)", got, err)
		}
	})

	t.Run("case-insensitive collision with no exact match is ambiguous", func(t *testing.T) {
		_, err := c.ResolveTarget("xo", "ALPHA")
		if !errors.Is(err, ErrAmbiguousTarget) {
			t.Fatalf("'ALPHA' → %v, want ErrAmbiguousTarget", err)
		}
	})

	t.Run("case-insensitive single match resolves", func(t *testing.T) {
		single := rosterFromAgents("xo", "alpha")
		got, err := single.ResolveTarget("xo", "ALPHA")
		if err != nil || got != "alpha" {
			t.Fatalf("'ALPHA' (single) → (%q, %v), want (alpha, nil)", got, err)
		}
	})

	t.Run("unknown target errors", func(t *testing.T) {
		_, err := c.ResolveTarget("xo", "ghost")
		if !errors.Is(err, ErrUnknownTarget) {
			t.Fatalf("'ghost' → %v, want ErrUnknownTarget", err)
		}
	})
}
