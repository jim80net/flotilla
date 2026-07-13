package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/readermap"
	"github.com/jim80net/flotilla/internal/sessionmirror"
)

// #465: the partition firewall guards PUBLIC egress only. Fleet-internal surfaces
// (operator notify, desk/coordinator mirror, hotline reply) pass deployment vocabulary
// through unimpeded; the static guard (check-private-boundary.sh + pre-push) still
// refuses the same content on public-repo paths.

func TestPublicEgressFirewallRefusesDeploymentTerm(t *testing.T) {
	clearFirewallEnv(t)
	t.Setenv(denylistEnv, "acme-desk")
	ts, err := LoadFirewall()
	if err != nil {
		t.Fatal(err)
	}
	r := readermap.Check("status from the acme-desk", ts)
	if r.Decision != readermap.FirewallRefuse {
		t.Fatalf("public egress guard must REFUSE deployment vocabulary; got %v", r.Decision)
	}
}

func TestOperatorNotifyPassesFirewallDenylist(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	const agent = "xo"
	secrets := writeSecrets(t, agent, srv.URL)
	clearFirewallEnv(t)
	t.Setenv(denylistEnv, "acme-desk")
	if err := cmdNotify([]string{"--from", agent, "--secrets", secrets, "ping from the acme-desk"}); err != nil {
		t.Fatalf("operator notify is fleet-internal — denylist must not bounce; got %v", err)
	}
	if hits != 1 {
		t.Errorf("notify must post once; got %d requests", hits)
	}
}

func TestDeskMirrorPassesFirewallDenylist(t *testing.T) {
	var lines []string
	var body string
	m := deskMirror{
		allowDiscord: true,
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return "the acme-desk finished its run", true, nil },
		post:         func(_, _, b string) error { body = b; return nil },
		logf:         recordLogf(&lines),
	}
	m.run("backend")
	if body != "the acme-desk finished its run" {
		t.Fatalf("desk mirror must publish fleet-internal vocabulary; got %q", body)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "POST backend") {
		t.Fatalf("decision lines = %v, want one POST", lines)
	}
}

func TestCoordinatorLedgerPassesFirewallDenylist(t *testing.T) {
	dir := t.TempDir()
	var rec sessionmirror.Record
	m := deskMirror{
		rosterDir: dir,
		turnFinal: func(string) (string, bool, error) { return "coordinator acme-desk status", true, nil },
		post:      func(_, _, _ string) error { t.Fatal("ledger-only must not post"); return nil },
		logf:      func(string, ...any) {},
		ledgerAppend: func(_, _ string, r sessionmirror.Record) error {
			rec = r
			return nil
		},
	}
	m.run("xo")
	if rec.Verbose != "coordinator acme-desk status" {
		t.Fatalf("coordinator ledger must keep raw turn-final; Verbose=%q", rec.Verbose)
	}
	if rec.Suppressed {
		t.Error("fleet-internal ledger must not mark Suppressed on denylist vocabulary")
	}
}

func TestHotlineReplyPassesFirewallDenylist(t *testing.T) {
	var posts, escalations []string
	d := replyDeps{
		dest:     func(string) (string, bool) { return "wh", true },
		post:     func(url, _, content string) error { posts = append(posts, content); return nil },
		escalate: func(_, msg string) { escalations = append(escalations, msg) },
		logf:     func(string, ...any) {},
	}
	d.route(context.Background(), "xo", "chanA", "the acme-desk reported in")
	if len(posts) != 1 {
		t.Fatalf("hotline reply must route fleet-internal vocabulary; got posts=%v", posts)
	}
	if len(escalations) != 0 {
		t.Fatalf("hotline reply must not escalate on denylist vocabulary; got %v", escalations)
	}
}
