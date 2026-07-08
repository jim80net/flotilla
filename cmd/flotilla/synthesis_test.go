package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
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

// §7.1 — the WakeSynthesis prompt names the read set (agents below), the CONCRETE read command, the
// post target (owned channel), the per-tier output contract, the narrow-answer discipline, and
// references the embedded skill.
func TestSynthesisWakeBodyContents(t *testing.T) {
	body := synthesisWakeBody("xo", "/home/operator/go/bin/flotilla", "/r/flotilla.json", []string{"backend", "data"}, []string{"xo-2"}, "")

	for _, want := range []string{"backend", "data", "xo-2", "visibility-synthesis", "idle", "result --roster", "SKIP an unreadable"} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis wake body missing %q:\n%s", want, body)
		}
	}
}

// REGRESSION (#190): ack follows the liveness-tracked TARGET — sub-coordinators must NOT receive it.
func TestSynthesisWakeBodyNoLivenessAckForSubCoordinator(t *testing.T) {
	body := synthesisWakeBody("alpha-xo", "/home/operator/go/bin/flotilla", "/r/flotilla.json", []string{"backend"}, []string{"alpha-ch"}, "")
	for _, banned := range []string{"ack you are alive", "flotilla-xo-alive", "-alive", "To ack you are alive"} {
		if strings.Contains(body, banned) {
			t.Errorf("sub-coordinator synthesis wake must NOT carry liveness-ack (%q in body)\n%s", banned, body)
		}
	}
}

// REGRESSION (#190 complement): when the meta-XO IS the clock XO, synthesis wakes to it MUST still
// carry the ack instruction so AckAge does not false-wedge during synthesis-only quiet stretches.
func TestSynthesisWakeBodyClockXOLivenessAck(t *testing.T) {
	const ack = "\n(To ack you are alive, run: touch /state/flotilla-xo-alive)"
	body := synthesisWakeBody("meta-xo", "/home/operator/go/bin/flotilla", "/r/flotilla.json", []string{"alpha-xo", "beta-xo"}, []string{"fleet-cmd"}, ack)
	if !strings.Contains(body, ack) {
		t.Errorf("clock-XO-target synthesis wake must append ackInstr; want %q in body\n%s", ack, body)
	}
	if !strings.Contains(body, "To ack you are alive") {
		t.Errorf("clock-XO synthesis wake must name the ack touch command\n%s", body)
	}
}

// The wake prompt must inject the DAEMON'S loaded roster path into the read command (NOT a default /
// hardcoded path), so a directly-launched agent runs `<bin> result --roster <live-path> <name>` and
// resolves the live roster from its OWN cwd. (Trio-nail item 1.)
func TestSynthesisWakeBodyInjectsDaemonRosterPath(t *testing.T) {
	const rosterPath = "/home/operator/workspace/the-fleet/state/flotilla.json"
	const binPath = "/home/operator/go/bin/flotilla"
	body := synthesisWakeBody("xo", binPath, rosterPath, []string{"backend"}, []string{"xo-2"}, "")
	want := binPath + " result --roster " + rosterPath + " <name>"
	if !strings.Contains(body, want) {
		t.Errorf("wake body must inject the daemon's roster path in the read command\nwant substring: %q\ngot:\n%s", want, body)
	}
}

// The read command must use the daemon's ABSOLUTE BINARY PATH (os.Executable in production), NOT bare
// `flotilla` — a directly-launched agent may not have flotilla on its $PATH (the live fleet invokes
// ~/go/bin/flotilla by absolute path). The bare-`flotilla` fallback (when os.Executable errors) is
// honored: synthesisWakeBody uses whatever binPath the caller passes.
func TestSynthesisWakeBodyUsesAbsoluteBinaryPath(t *testing.T) {
	abs := synthesisWakeBody("xo", "/home/operator/go/bin/flotilla", "/r.json", []string{"sub"}, []string{"c"}, "")
	if !strings.Contains(abs, "`/home/operator/go/bin/flotilla result --roster /r.json <name>`") {
		t.Errorf("read command must use the absolute binary path:\n%s", abs)
	}
	// The fallback path (bare "flotilla") is composed verbatim when the caller passes it.
	fb := synthesisWakeBody("xo", "flotilla", "/r.json", []string{"sub"}, []string{"c"}, "")
	if !strings.Contains(fb, "`flotilla result --roster /r.json <name>`") {
		t.Errorf("bare-flotilla fallback must compose verbatim:\n%s", fb)
	}
}

// §7.1 — when the synthesizing agent owns NO resolvable channel (no post target), the body still
// composes (it names the read set + discipline) so a misprovisioned post target never crashes the
// wake; the empty post-target case degrades to a clear "no post target" note rather than a panic.
func TestSynthesisWakeBodyNoPostTarget(t *testing.T) {
	body := synthesisWakeBody("orphan", "/home/operator/go/bin/flotilla", "/r/flotilla.json", []string{"sub"}, nil, "")
	if !strings.Contains(body, "sub") {
		t.Errorf("body must still name the read set even with no post target:\n%s", body)
	}
}

// Per-tier granularity (trio-nail item 2): the read set the wake body names, plus the `flotilla
// result` command, are correct for BOTH a Tier-2 XO (reads its boats) AND the Tier-3 meta-XO (reads
// the project-XOs — subordinates that are THEMSELVES synthesizers). `flotilla result <name>` returns
// each subordinate's latest turn-final regardless of its tier, so the same command serves both.
func TestSynthesisWakeBodyPerTierReadSet(t *testing.T) {
	// A federated live-shape roster: meta-xo (Tier 3) over alpha-xo + beta-xo (project-XOs); alpha-xo
	// (Tier 2) over its boats alpha-be + alpha-data. Each agent owns its home channel listing its
	// PARENT; the broadcast channel is tagged fleet-command (excluded from synthesis edges).
	rosterPath := writeRosterFile(t, `{
	  "operator_user_id":"U",
	  "agents":[{"name":"meta-xo"},{"name":"alpha-xo"},{"name":"alpha-be"},{"name":"alpha-data"},{"name":"beta-xo"}],
	  "channels":[
	    {"channel_id":"C_CMD","xo_agent":"meta-xo","role":"fleet-command","members":["meta-xo","alpha-xo","alpha-be","alpha-data","beta-xo"]},
	    {"channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["meta-xo"]},
	    {"channel_id":"C_BETA","xo_agent":"beta-xo","members":["meta-xo"]},
	    {"channel_id":"C_ABE","xo_agent":"alpha-be","members":["alpha-xo"]},
	    {"channel_id":"C_ADATA","xo_agent":"alpha-data","members":["alpha-xo"]}]}`)
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}

	const binPath = "/home/operator/go/bin/flotilla"
	// Tier 2: alpha-xo reads its boats (alpha-be, alpha-data) — NOT meta-xo, NOT itself.
	t2Read := synthesisReadSet(cfg, "alpha-xo")
	t2Body := synthesisWakeBody("alpha-xo", binPath, rosterPath, t2Read, cfg.OwnedChannels("alpha-xo"), "")
	for _, boat := range []string{"alpha-be", "alpha-data"} {
		if !strings.Contains(t2Body, boat) {
			t.Errorf("Tier-2 alpha-xo read set must name boat %q; read set=%v body=\n%s", boat, t2Read, t2Body)
		}
	}
	if strings.Contains(t2Body, "meta-xo") {
		t.Errorf("Tier-2 alpha-xo must NOT read its parent meta-xo; read set=%v", t2Read)
	}

	// Tier 3: meta-xo reads the project-XOs (alpha-xo, beta-xo) — subordinates that are themselves
	// synthesizers — via the SAME `flotilla result` command.
	t3Read := synthesisReadSet(cfg, "meta-xo")
	t3Body := synthesisWakeBody("meta-xo", binPath, rosterPath, t3Read, cfg.OwnedChannels("meta-xo"), "")
	for _, xo := range []string{"alpha-xo", "beta-xo"} {
		if !strings.Contains(t3Body, xo) {
			t.Errorf("Tier-3 meta-xo read set must name project-XO %q; read set=%v body=\n%s", xo, t3Read, t3Body)
		}
	}
	if strings.Contains(t3Body, "alpha-be") {
		t.Errorf("Tier-3 meta-xo reads project-XOs, not the leaf boats directly; read set=%v", t3Read)
	}
	if !strings.Contains(t3Body, binPath+" result --roster "+rosterPath) {
		t.Errorf("Tier-3 wake body must carry the same absolute-binary result command:\n%s", t3Body)
	}
}

// REGRESSION: a coordinator provisioned only into the fleet-command broadcast channel (no home
// channel yet) must appear in the owner's visibility-synthesis wake prompt subordinate list.
func TestSynthesisWakeBodyIncludesFleetCommandCoordinator(t *testing.T) {
	rosterPath := writeRosterFile(t, `{
	  "operator_user_id":"U",
	  "cos_agent":"cos",
	  "xo_agent":"cos",
	  "agents":[{"name":"cos"},{"name":"flotilla-backlog-xo","surface":"claude-code"},
	            {"name":"alpha-xo"}],
	  "channels":[
	    {"channel_id":"C_CMD","xo_agent":"cos","role":"fleet-command",
	     "members":["cos","flotilla-backlog-xo","alpha-xo"]},
	    {"channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["cos"]}]}`)
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	const binPath = "/home/operator/go/bin/flotilla"
	readSet := synthesisReadSet(cfg, "cos")
	body := synthesisWakeBody("cos", binPath, rosterPath, readSet, cfg.OwnedChannels("cos"), "")
	for _, sub := range []string{"flotilla-backlog-xo", "alpha-xo"} {
		if !strings.Contains(body, sub) {
			t.Errorf("cos synthesis wake must name subordinate %q; readSet=%v body=\n%s", sub, readSet, body)
		}
	}
	if !strings.Contains(body, "C_CMD") {
		t.Errorf("cos synthesis wake must name owned post channel C_CMD; body=\n%s", body)
	}
}
