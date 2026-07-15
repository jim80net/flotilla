package status

import (
	"strings"
	"testing"
)

const fixtureJSON = `{
  "generated_at": "2026-06-17T17:26:31Z",
  "xo": "xo",
  "agents": [
    {"name": "xo", "role": "hub", "state": "idle", "loop_posture": "parked"},
    {"name": "xo-adj", "state": "idle", "loop_posture": "parked"},
    {"name": "backend", "state": "working", "loop_posture": "available"},
    {"name": "frontend", "state": "awaiting-approval", "loop_posture": "available"},
    {"name": "data", "state": "working", "loop_posture": "available"},
    {"name": "infra", "state": "idle", "loop_posture": "available", "raw_loop_posture": "awaiting-authority"},
    {"name": "ops", "state": "idle", "loop_posture": "blocked"},
    {"name": "research", "state": "crashed", "loop_posture": "available"}
  ]
}`

func TestCompressBlock_FromFixtureJSON(t *testing.T) {
	doc, err := ParseDoc([]byte(fixtureJSON))
	if err != nil {
		t.Fatal(err)
	}
	// Skip self (xo) + adj noise — deployment wrapper semantics.
	got := CompressBlock(doc, CompressOptions{Skip: SkipSet("xo", "xo-adj")})
	if !strings.HasPrefix(got, "**Status of the fleet**\n") {
		t.Fatalf("header missing:\n%s", got)
	}
	for _, want := range []string{
		"as of 2026-06-17T17:26:31Z",
		"6 seats", // 8 agents minus xo + xo-adj
		"working:2",
		"awaiting:1",
		"available:1", // awaiting-authority is operator-facing available, not blocked
		"blocked:1",   // real blocked posture remains strong
		"crashed:1",
		"working: backend, data",
		"blocked: ops",
		"awaiting: frontend",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q\n---\n%s", want, got)
		}
	}
	// Self + adj must not appear in lists.
	if strings.Contains(got, "xo-adj") || strings.Contains(got, "working: xo") {
		t.Errorf("self/adj noise leaked:\n%s", got)
	}
	// Idle seats are histogram-only (no idle: list line).
	if strings.Contains(got, "\nidle:") {
		t.Errorf("idle list should be omitted (histogram only):\n%s", got)
	}
}

func TestCompressBlock_EmptyAgentsUnavailable(t *testing.T) {
	got := CompressBlock(Doc{}, CompressOptions{})
	if got != UnavailableBlock() {
		t.Fatalf("got %q", got)
	}
}

func TestHasFleetStatusHeader_Idempotent(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{"topic only", false},
		{"hello\n\n**Status of the fleet**\nas of …", true},
		{"**Fleet status** one-liner", true},
		{"Status of the fleet without bold", false},
	}
	for _, c := range cases {
		if got := HasFleetStatusHeader(c.body); got != c.want {
			t.Errorf("HasFleetStatusHeader(%q) = %v, want %v", c.body, got, c.want)
		}
	}
}

func TestAppendFleetStatus_IdempotentAndFailClosed(t *testing.T) {
	body := "Deploy complete."
	block := CompressBlock(mustDoc(t, fixtureJSON), CompressOptions{Skip: SkipSet("xo", "xo-adj")})
	once := AppendFleetStatus(body, block)
	if !strings.Contains(once, "Deploy complete.") || !strings.Contains(once, "**Status of the fleet**") {
		t.Fatalf("append failed:\n%s", once)
	}
	twice := AppendFleetStatus(once, "**Status of the fleet**\nSHOULD-NOT-APPEAR")
	if strings.Contains(twice, "SHOULD-NOT-APPEAR") {
		t.Fatal("second append must be idempotent")
	}
	if twice != once {
		t.Fatalf("idempotent append mutated body")
	}

	// Empty block → unavailable.
	u := AppendFleetStatus("hi", "")
	if !strings.Contains(u, "(unavailable)") {
		t.Fatalf("empty block must fail-closed unavailable:\n%s", u)
	}
}

func TestClassifyState(t *testing.T) {
	cases := []struct{ state, lp, want string }{
		{"working", "available", "working"},
		{"idle", "awaiting-authority", "available"},
		{"idle", "blocked", "blocked"},
		{"awaiting-input", "", "awaiting"},
		{"awaiting-approval", "available", "awaiting"},
		{"crashed", "", "crashed"},
		{"blocked", "", "blocked"},
		{"", "", "unknown"},
	}
	for _, c := range cases {
		if got := classifyState(c.state, c.lp, ""); got != c.want {
			t.Errorf("classify(%q,%q)=%q want %q", c.state, c.lp, got, c.want)
		}
	}
}

func mustDoc(t *testing.T, raw string) Doc {
	t.Helper()
	d, err := ParseDoc([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
