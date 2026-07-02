package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These tests lock the behavior of deploy/flotilla-watch-install.sh — the generator
// that produces ~/.config/systemd/user/flotilla-watch.service from the repo template
// + a host-path env. The installer is the anti-drift mechanism; this is the
// functional-identity regression the unit must not silently break.
//
// go test runs with CWD = this package dir (cmd/flotilla), so deploy/ is two up.
const (
	installerSh = "../../deploy/flotilla-watch-install.sh"
	exampleEnv  = "../../deploy/flotilla-watch.env.example"
	fixtureEnv  = "../../deploy/testdata/flotilla-watch.fixture.env"
)

var placeholderRe = regexp.MustCompile(`@FLOTILLA_[A-Z_]+@`)

// funcLineRe matches the section headers + the directives systemd acts on (excludes
// comments, Description, Documentation) so the test asserts FUNCTIONAL equivalence,
// not prose. Capturing the [Section] headers locks placement too: a directive moved
// to the wrong section would change this sequence and fail, even though systemd
// would silently ignore it.
var funcLineRe = regexp.MustCompile(`(?m)^(\[(?:Unit|Service|Install)\]|(?:Type|WorkingDirectory|ExecStartPre|ExecStart|RestartSec|Restart|StartLimitIntervalSec|StartLimitBurst|KillSignal|TimeoutStopSec|After|Wants|WantedBy)=.*)$`)

// renderUnit runs the installer in --print mode (pure render: no path-existence
// checks, no write, no daemon-reload) and returns the generated unit text.
func renderUnit(t *testing.T, envPath string) string {
	t.Helper()
	return renderUnitEnv(t, envPath)
}

// renderUnitEnv is renderUnit with extra "KEY=value" entries appended to the
// installer subprocess's environment. Used to prove the installer ignores an
// inherited FLOTILLA_BACKLOG_FILE (the live fleet host exports it) and reads the
// value ONLY from the .env file.
func renderUnitEnv(t *testing.T, envPath string, extraEnv ...string) string {
	t.Helper()
	cmd := exec.Command("bash", installerSh, "--print", envPath)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("installer --print %s failed: %v\n%s", envPath, err, out)
	}
	return string(out)
}

// execLine extracts the single ExecStart= directive (not ExecStartPre=) from a
// rendered unit, for exact-match assertions on the backlog arg.
func execLine(t *testing.T, unit string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^ExecStart=.*$`)
	m := re.FindString(unit)
	if m == "" {
		t.Fatalf("no ExecStart line in rendered unit:\n%s", unit)
	}
	return m
}

func TestInstallerGeneratesExpectedFunctionalUnit(t *testing.T) {
	unit := renderUnit(t, fixtureEnv)

	if m := placeholderRe.FindAllString(unit, -1); len(m) > 0 {
		t.Fatalf("unsubstituted placeholders remain: %v", m)
	}

	// Functional directives in file order, fully substituted from the fixture env.
	// A change to the template or substitution that alters what systemd acts on
	// fails here — that is the lock the XO asked for.
	want := []string{
		"[Unit]",
		"After=network-online.target",
		"Wants=network-online.target",
		"StartLimitIntervalSec=600",
		"StartLimitBurst=5",
		"[Service]",
		"Type=simple",
		"WorkingDirectory=/srv/fleet",
		`ExecStartPre=/bin/sh -c 'for i in $(seq 1 30); do getent hosts discord.com >/dev/null 2>&1 && exit 0; sleep 2; done; exit 0'`,
		"ExecStart=%h/go/bin/flotilla watch --roster /srv/fleet/flotilla.json --secrets /srv/fleet/secrets.env --ack-file /srv/fleet/xo-alive",
		"Restart=on-failure",
		"RestartSec=15",
		"KillSignal=SIGTERM",
		"TimeoutStopSec=15",
		"[Install]",
		"WantedBy=default.target",
	}
	got := funcLineRe.FindAllString(unit, -1)
	if len(got) != len(want) {
		t.Fatalf("functional line count = %d, want %d\ngot:  %#v\nwant: %#v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("functional line %d:\n got:  %q\n want: %q", i, got[i], want[i])
		}
	}
}

// The committed example env must keep the template fully substitutable: if a new
// @PLACEHOLDER@ is added to the template but not the example (or vice versa), this
// catches the drift before it ships.
func TestInstallerExampleEnvSubstitutesFully(t *testing.T) {
	unit := renderUnit(t, exampleEnv)
	if m := placeholderRe.FindAllString(unit, -1); len(m) > 0 {
		t.Fatalf("example env leaves placeholders (template/example out of sync): %v", m)
	}
}

func TestInstallerRenderIsDeterministic(t *testing.T) {
	if a, b := renderUnit(t, fixtureEnv), renderUnit(t, fixtureEnv); a != b {
		t.Fatal("installer render is not deterministic")
	}
}

// An env missing any of the five required vars must fail loudly (the guard fires
// before render, so even --print rejects it) — never silently emit a half-wired unit.
func TestInstallerRejectsIncompleteEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), "incomplete.env")
	if err := os.WriteFile(p, []byte("FLOTILLA_WORKDIR=/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", installerSh, "--print", p).CombinedOutput(); err == nil {
		t.Fatalf("expected failure on incomplete env, got success:\n%s", out)
	}
}

// `KEY = value` (spaces around the =, a common .env habit) must not leave a leading
// space in the value — that would yield an invalid `ExecStart= %h/...`. A
// tab-indented key must also still resolve (if the key-strip missed tabs, FLOTILLA_BIN
// would become an "unknown key" → missing-var → render failure here).
func TestInstallerTrimsWhitespaceAroundEquals(t *testing.T) {
	p := filepath.Join(t.TempDir(), "spaced.env")
	body := "FLOTILLA_WORKDIR = /srv/fleet\n" +
		"\tFLOTILLA_BIN = %h/go/bin/flotilla\n" +
		"FLOTILLA_ROSTER=/srv/fleet/flotilla.json\n" +
		"FLOTILLA_SECRETS=/srv/fleet/secrets.env\n" +
		"FLOTILLA_ACK_FILE=/srv/fleet/xo-alive\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	unit := renderUnit(t, p)
	if !strings.Contains(unit, "ExecStart=%h/go/bin/flotilla watch ") {
		t.Errorf("ExecStart not clean after a spaced env:\n%s", funcLineRe.FindString(unit))
	}
	if strings.Contains(unit, "ExecStart= ") || strings.Contains(unit, "WorkingDirectory= ") {
		t.Errorf("a value retained leading whitespace:\n%s", unit)
	}
}

// A value that itself contains a template token would be rewritten by a later
// substitution pass; the installer must refuse rather than emit a corrupted unit.
func TestInstallerRejectsPlaceholderInValue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "evil.env")
	body := "FLOTILLA_WORKDIR=/srv/fleet\n" +
		"FLOTILLA_BIN=@FLOTILLA_ROSTER@\n" +
		"FLOTILLA_ROSTER=/srv/fleet/flotilla.json\n" +
		"FLOTILLA_SECRETS=/srv/fleet/secrets.env\n" +
		"FLOTILLA_ACK_FILE=/srv/fleet/xo-alive\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", installerSh, "--print", p).CombinedOutput(); err == nil {
		t.Fatalf("expected failure on placeholder-in-value, got success:\n%s", out)
	}
}

// --- Optional 6th key: FLOTILLA_BACKLOG_FILE (the goal-driven loop's backlog gate) ---
//
// The key is OPTIONAL: when SET the ExecStart gains ` --backlog-file <path>`; when
// UNSET the unit is byte-identical to a no-backlog install (gate OFF = today's
// default). The byte-identical-when-unset case is the load-bearing no-regression
// lock — TestInstallerGeneratesExpectedFunctionalUnit already pins the exact unset
// ExecStart line (no --backlog-file, no trailing space) against the 5-key fixture;
// the three tests below add the SET path, an explicit unset assertion, and the
// inherited-env-no-leak guard that protects the byte-identical guarantee on the live
// fleet host (which exports FLOTILLA_BACKLOG_FILE for the binary to read).

func baseEnv(t *testing.T, name string, extra map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	body := "FLOTILLA_WORKDIR=/srv/fleet\n" +
		"FLOTILLA_BIN=%h/go/bin/flotilla\n" +
		"FLOTILLA_ROSTER=/srv/fleet/flotilla.json\n" +
		"FLOTILLA_SECRETS=/srv/fleet/secrets.env\n" +
		"FLOTILLA_ACK_FILE=/srv/fleet/xo-alive\n"
	for k, v := range extra {
		if v != "" {
			body += k + "=" + v + "\n"
		}
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func backlogEnv(t *testing.T, backlog string) string {
	t.Helper()
	extra := map[string]string{}
	if backlog != "" {
		extra["FLOTILLA_BACKLOG_FILE"] = backlog
	}
	return baseEnv(t, "backlog.env", extra)
}

// (b) SET ⇒ exactly one ` --backlog-file <path>` appended after --ack-file.
func TestInstallerBacklogSetAppendsArg(t *testing.T) {
	unit := renderUnitEnv(t, backlogEnv(t, "/srv/fleet/backlog.md"))
	if m := placeholderRe.FindAllString(unit, -1); len(m) > 0 {
		t.Fatalf("unsubstituted placeholders remain: %v", m)
	}
	want := "ExecStart=%h/go/bin/flotilla watch --roster /srv/fleet/flotilla.json --secrets /srv/fleet/secrets.env --ack-file /srv/fleet/xo-alive --backlog-file /srv/fleet/backlog.md"
	if got := execLine(t, unit); got != want {
		t.Errorf("ExecStart with backlog SET:\n got:  %q\n want: %q", got, want)
	}
}

// (a) UNSET ⇒ no --backlog-file, no trailing space (byte-identical to today). The
// 6th key absent from the env must leave the ExecStart line exactly as the 5-key
// baseline — the explicit no-regression assertion the XO asked to lock.
func TestInstallerBacklogUnsetOmitsArg(t *testing.T) {
	unit := renderUnitEnv(t, backlogEnv(t, ""))
	want := "ExecStart=%h/go/bin/flotilla watch --roster /srv/fleet/flotilla.json --secrets /srv/fleet/secrets.env --ack-file /srv/fleet/xo-alive"
	if got := execLine(t, unit); got != want {
		t.Errorf("ExecStart with backlog UNSET must be byte-identical to baseline:\n got:  %q\n want: %q", got, want)
	}
}

// A backlog path containing `&` must render LITERALLY. bash 5.2+ enables
// patsub_replacement by default, under which a `&` in a ${var//pat/repl} replacement
// expands to the matched text — corrupting the path into the placeholder token and
// tripping the fail-loud guard. The installer disables patsub_replacement so every
// value substitutes literally; this locks that contract (it covers all keys uniformly).
func TestInstallerBacklogPathWithAmpersand(t *testing.T) {
	unit := renderUnitEnv(t, backlogEnv(t, "/srv/fleet/a&b.md"))
	if m := placeholderRe.FindAllString(unit, -1); len(m) > 0 {
		t.Fatalf("unsubstituted placeholders remain (& corrupted the substitution): %v", m)
	}
	want := "ExecStart=%h/go/bin/flotilla watch --roster /srv/fleet/flotilla.json --secrets /srv/fleet/secrets.env --ack-file /srv/fleet/xo-alive --backlog-file /srv/fleet/a&b.md"
	if got := execLine(t, unit); got != want {
		t.Errorf("ExecStart with `&` in backlog path:\n got:  %q\n want: %q", got, want)
	}
}

// (c) An inherited (exported) FLOTILLA_BACKLOG_FILE must NOT leak into a render whose
// .env omits the key — the installer reads the value ONLY from the .env file. This
// guards the pre-clear: the live fleet host exports FLOTILLA_BACKLOG_FILE (the binary
// reads it), so without the pre-clear the byte-identical-when-unset guarantee would
// fail on exactly the host this work targets.
func latencyEnv(t *testing.T, interval, eventPoll string) string {
	t.Helper()
	extra := map[string]string{}
	if interval != "" {
		extra["FLOTILLA_WATCH_INTERVAL"] = interval
	}
	if eventPoll != "" {
		extra["FLOTILLA_EVENT_POLL_INTERVAL"] = eventPoll
	}
	return baseEnv(t, "latency.env", extra)
}

func TestInstallerLatencyArgsUnsetOmitsFragment(t *testing.T) {
	unit := renderUnitEnv(t, latencyEnv(t, "", ""))
	want := "ExecStart=%h/go/bin/flotilla watch --roster /srv/fleet/flotilla.json --secrets /srv/fleet/secrets.env --ack-file /srv/fleet/xo-alive"
	if got := execLine(t, unit); got != want {
		t.Errorf("ExecStart without latency env:\n got:  %q\n want: %q", got, want)
	}
}

func TestInstallerLatencyArgsSetAppendsFlags(t *testing.T) {
	unit := renderUnitEnv(t, latencyEnv(t, "5m", "3s"))
	want := "ExecStart=%h/go/bin/flotilla watch --roster /srv/fleet/flotilla.json --secrets /srv/fleet/secrets.env --ack-file /srv/fleet/xo-alive --interval 5m --event-poll-interval 3s"
	if got := execLine(t, unit); got != want {
		t.Errorf("ExecStart with latency env:\n got:  %q\n want: %q", got, want)
	}
}

func adaptiveEnv(t *testing.T, extra map[string]string) string {
	t.Helper()
	return baseEnv(t, "adaptive.env", extra)
}

func TestInstallerAdaptiveArgsUnsetOmitsFragment(t *testing.T) {
	unit := renderUnitEnv(t, adaptiveEnv(t, nil))
	if strings.Contains(execLine(t, unit), "--adaptive-interval") {
		t.Fatal("unset adaptive env must omit adaptive flags")
	}
}

func TestInstallerAdaptiveArgsSetAppendsFlags(t *testing.T) {
	unit := renderUnitEnv(t, adaptiveEnv(t, map[string]string{
		"FLOTILLA_ADAPTIVE_INTERVAL":     "false",
		"FLOTILLA_INTERVAL_FLOOR":        "3m",
		"FLOTILLA_INTERVAL_WARM":         "6m",
		"FLOTILLA_INTERVAL_IDLE_STABLE":  "12m",
		"FLOTILLA_INTERVAL_RELEASE_STEP": "4m",
	}))
	want := "ExecStart=%h/go/bin/flotilla watch --roster /srv/fleet/flotilla.json --secrets /srv/fleet/secrets.env --ack-file /srv/fleet/xo-alive --adaptive-interval false --interval-floor 3m --interval-warm 6m --interval-idle-stable 12m --interval-release-step 4m"
	if got := execLine(t, unit); got != want {
		t.Errorf("ExecStart with adaptive env:\n got:  %q\n want: %q", got, want)
	}
}

func TestInstallerBacklogInheritedEnvNoLeak(t *testing.T) {
	unit := renderUnitEnv(t, backlogEnv(t, ""), "FLOTILLA_BACKLOG_FILE=/leak/should/not/appear.md")
	// Scope the assertion to the ExecStart directive — the template's descriptive
	// comment legitimately contains the literal "--backlog-file".
	if got := execLine(t, unit); strings.Contains(got, "--backlog-file") {
		t.Errorf("inherited FLOTILLA_BACKLOG_FILE leaked into ExecStart (pre-clear missing):\n%s", got)
	}
}
