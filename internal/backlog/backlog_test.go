package backlog

import (
	"strings"
	"testing"
)

// contractFixture mirrors the live fleet-backlog.md migrated to the item-line CONTRACT
// (- [<status>] …): 5 unblocked (in-flight/next), 1 operator-blocked, plus other sections that
// MUST be ignored. This is the backward-compat shape the gate parses in production.
const contractFixture = `# Fleet backlog

## Goals
- [next] this is in the GOALS section and must be ignored

## Backlog (prioritized; advance the top UNBLOCKED item every wake)
- [in-flight] SPCX options analysis — before the open (delta-xo)
- [in-flight] Grok desk up (flotilla-dev)
- [in-flight] Equities/options framework extension (crypto-trend → delta-xo)
- [next] Goal-driven loop mechanism (flotilla-dev)
- [done] Inbound-path bug fix (flotilla-dev) — #71/#74 merged
- [in-flight] PR-D multi-instrument rollout
- [blocked] PR-E loss-cap values — awaiting operator value sign-off

## Operator decisions queued
- [next] this bullet is in a DIFFERENT section and must be ignored

## Dropped / parked
- [in-flight] cursor driver (dropped) — must be ignored
`

func TestParseContractFixture(t *testing.T) {
	st := Parse(contractFixture)
	if !st.Found {
		t.Fatal("Found = false, want true (## Backlog section is present)")
	}
	// 5 in-flight/next + 1 done + 1 blocked = 7 item lines; 5 unblocked, 1 blocked, 1 done.
	if got := len(st.Unblocked); got != 5 {
		t.Errorf("len(Unblocked) = %d, want 5 (items in Goals/Operator/Dropped sections must be ignored): %v", got, st.Unblocked)
	}
	if st.Blocked != 1 {
		t.Errorf("Blocked = %d, want 1", st.Blocked)
	}
	if st.Done != 1 {
		t.Errorf("Done = %d, want 1", st.Done)
	}
	if st.Malformed != 0 {
		t.Errorf("Malformed = %d, want 0 (the fixture is well-formed)", st.Malformed)
	}
	if st.Items != 7 {
		t.Errorf("Items = %d, want 7", st.Items)
	}
	if len(st.Unblocked) > 0 && !strings.Contains(st.Unblocked[0], "SPCX") {
		t.Errorf("Unblocked[0] = %q, want the top (SPCX) item — file order is the drive priority", st.Unblocked[0])
	}
}

func TestParseEdgeAndFailSafe(t *testing.T) {
	t.Run("empty section → settle-eligible, found", func(t *testing.T) {
		st := Parse("## Backlog\n\n## Next\n")
		if !st.Found || len(st.Unblocked) != 0 || st.Items != 0 {
			t.Errorf("empty section: %+v, want Found:true Unblocked:0 Items:0", st)
		}
	})
	t.Run("no Backlog section → Found false", func(t *testing.T) {
		st := Parse("# Doc\n## Goals\n- [next] x\n## Other\ntext\n")
		if st.Found {
			t.Errorf("Found = true, want false (no ## Backlog section); %+v", st)
		}
	})
	t.Run("markerless item → Malformed AND Unblocked (err toward driving + flag)", func(t *testing.T) {
		st := Parse("## Backlog\n1. **SPCX** (delta-xo, IN FLIGHT) — no bracket marker\n")
		if st.Malformed != 1 {
			t.Errorf("Malformed = %d, want 1 (a markerless item is flagged)", st.Malformed)
		}
		if len(st.Unblocked) != 1 {
			t.Errorf("len(Unblocked) = %d, want 1 (a markerless item still drives — never silently dropped)", len(st.Unblocked))
		}
	})
	t.Run("unrecognized marker → malformed, not silently classified", func(t *testing.T) {
		st := Parse("## Backlog\n- [whoknows] mystery item\n")
		if st.Malformed != 1 || len(st.Unblocked) != 1 {
			t.Errorf("unrecognized marker: %+v, want Malformed:1 + in Unblocked", st)
		}
	})
	t.Run("done variants excluded; lowercase prose 'done' does NOT match", func(t *testing.T) {
		st := Parse("## Backlog\n- [done] a\n- [x] b\n- ~~c struck~~\n- [in-flight] DRAFT #970 done but in-flight\n")
		if st.Done != 3 {
			t.Errorf("Done = %d, want 3 ([done], [x], ~~strike~~)", st.Done)
		}
		// The 4th item is [in-flight] with the lowercase word "done" in its TEXT — must be unblocked,
		// not done (only the leading [status] marker decides).
		if len(st.Unblocked) != 1 || !strings.Contains(st.Unblocked[0], "DRAFT") {
			t.Errorf("Unblocked = %v, want exactly the [in-flight] item (prose 'done' must not classify it done)", st.Unblocked)
		}
	})
	t.Run("blocked + needs-attention → operator-blocked", func(t *testing.T) {
		st := Parse("## Backlog\n- [blocked] a\n- [needs-attention] b\n")
		if st.Blocked != 2 || len(st.Unblocked) != 0 {
			t.Errorf("%+v, want Blocked:2 Unblocked:0", st)
		}
	})
	t.Run("total / never panics on pathological input", func(t *testing.T) {
		for _, in := range []string{"", "## Backlog", "## Backlog\n- [", "## Backlog\n- []\n", "\x00\n## Backlog\n- [in-flight]", strings.Repeat("- [in-flight] x\n", 1000)} {
			_ = Parse(in) // must not panic
		}
	})
}
