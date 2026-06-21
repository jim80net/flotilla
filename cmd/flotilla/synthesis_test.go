package main

import (
	"errors"
	"strings"
	"testing"
)

// §3.1 — the synthesis read resolves a subordinate's latest turn-final text via the injected
// turn-final reader (the production wiring is ResolvePane(agentTitle) → ResultReader.LatestResult).
// A subordinate whose pane will not resolve / whose surface has no ResultReader is a CLEAN SKIP
// (ok=false), never a crashed wake.
func TestSynthesisReadResolvesEachSubordinate(t *testing.T) {
	reads := map[string]string{"a": "state-a", "b": "state-b"}
	readable := map[string]bool{"a": true, "b": true, "c": false} // c is unreadable (no pane / no ResultReader)
	read := synthReadOneFromTurnFinal(func(sub string) (string, bool, error) {
		if !readable[sub] {
			return "", false, nil
		}
		return reads[sub], true, nil
	})

	if got, ok := read("a"); !ok || got != "state-a" {
		t.Errorf("read(a) = (%q,%v), want (state-a,true)", got, ok)
	}
	if got, ok := read("c"); ok {
		t.Errorf("an unreadable subordinate must be a clean skip (ok=false), got (%q,%v)", got, ok)
	}
}

// §3.2 — the read is read-only: a read failure is a SKIP (ok=false), never a panic / never a write.
func TestSynthesisReadFailureIsCleanSkip(t *testing.T) {
	read := synthReadOneFromTurnFinal(func(string) (string, bool, error) {
		return "", false, errors.New("tmux boom")
	})
	if got, ok := read("a"); ok {
		t.Errorf("a read error must be a clean skip (ok=false), got (%q,%v)", got, ok)
	}
}

// §7.1 — the WakeSynthesis prompt names the read set (agents below), the post target (owned
// channel), the per-tier output contract, the narrow-answer discipline, and references the embedded
// skill.
func TestSynthesisWakeBodyContents(t *testing.T) {
	body := synthesisWakeBody("family-office", []string{"v12-dev", "macro-desk"}, []string{"spark-xo"}, "\n(ack: touch /tmp/ack)")

	for _, want := range []string{"v12-dev", "macro-desk", "spark-xo", "visibility-synthesis", "idle"} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis wake body missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "/tmp/ack") {
		t.Errorf("synthesis wake body must carry the ack instruction:\n%s", body)
	}
}

// §7.1 — when the synthesizing agent owns NO resolvable channel (no post target), the body still
// composes (it names the read set + discipline) so a misprovisioned post target never crashes the
// wake; the empty post-target case degrades to a clear "no post target" note rather than a panic.
func TestSynthesisWakeBodyNoPostTarget(t *testing.T) {
	body := synthesisWakeBody("orphan", []string{"sub"}, nil, "")
	if !strings.Contains(body, "sub") {
		t.Errorf("body must still name the read set even with no post target:\n%s", body)
	}
}
