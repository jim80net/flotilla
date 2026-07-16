package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
)

const reloadRosterA = `{
  "xo_agent":"root","heartbeat_interval":"20m",
  "channels":[{"channel_id":"C_ROOT","xo_agent":"root","members":["alpha"]}],
  "agents":[{"name":"root"},{"name":"alpha"},{"name":"beta"}]
}`

const reloadRosterB = `{
  "xo_agent":"root","heartbeat_interval":"20m",
  "channels":[{"channel_id":"C_ROOT","xo_agent":"root","members":["beta"]}],
  "agents":[{"name":"root"},{"name":"alpha"},{"name":"beta"}]
}`

const reloadRosterCycle = `{
  "xo_agent":"root","heartbeat_interval":"20m",
  "channels":[
    {"channel_id":"C_ROOT","xo_agent":"root","members":["alpha"]},
    {"channel_id":"C_ALPHA","xo_agent":"alpha","members":["root"]}
  ],
  "agents":[{"name":"root"},{"name":"alpha"}]
}`

func TestWatchRosterReloadAdoptsCompleteValidatedSnapshot(t *testing.T) {
	path, initial := reloadFixture(t, reloadRosterA)
	r, err := newWatchRosterReloader(path, "", initial)
	if err != nil {
		t.Fatal(err)
	}
	writeReloadRoster(t, path, reloadRosterB)
	adopted, err := r.Check()
	if err != nil || !adopted {
		t.Fatalf("Check = (%v, %v), want clean adoption", adopted, err)
	}
	s := r.Snapshot()
	if s.Generation != 2 || firstReloadMember(s.Config) != "beta" || firstReloadBelow(s.Config) != "beta" || s.Config.Org() == nil {
		t.Fatalf("snapshot = generation %d member %q below=%q org=%v", s.Generation, firstReloadMember(s.Config), firstReloadBelow(s.Config), s.Config.Org())
	}
}

func TestWatchRosterReloadInvalidRetainsLastGoodThenRecovers(t *testing.T) {
	path, initial := reloadFixture(t, reloadRosterA)
	r, err := newWatchRosterReloader(path, "", initial)
	if err != nil {
		t.Fatal(err)
	}
	writeReloadRoster(t, path, `{"agents":[`)
	if adopted, err := r.Check(); err == nil || adopted {
		t.Fatalf("invalid Check = (%v, %v), want rejection", adopted, err)
	}
	if s := r.Snapshot(); s.Generation != 1 || firstReloadMember(s.Config) != "alpha" {
		t.Fatalf("invalid edit changed last-good snapshot: generation=%d member=%q", s.Generation, firstReloadMember(s.Config))
	}
	writeReloadRoster(t, path, reloadRosterB)
	if adopted, err := r.Check(); err != nil || !adopted {
		t.Fatalf("recovery Check = (%v, %v), want adoption", adopted, err)
	}
	if s := r.Snapshot(); s.Generation != 2 || firstReloadMember(s.Config) != "beta" {
		t.Fatalf("recovery snapshot = generation=%d member=%q", s.Generation, firstReloadMember(s.Config))
	}
}

func TestWatchRosterReloadRejectsInvalidDerivedTopology(t *testing.T) {
	path, initial := reloadFixture(t, reloadRosterA)
	r, err := newWatchRosterReloader(path, "", initial)
	if err != nil {
		t.Fatal(err)
	}
	writeReloadRoster(t, path, reloadRosterCycle)
	if adopted, err := r.Check(); err == nil || adopted {
		t.Fatalf("cyclic topology Check = (%v, %v), want rejection", adopted, err)
	}
	if s := r.Snapshot(); s.Generation != 1 || firstReloadMember(s.Config) != "alpha" {
		t.Fatalf("cyclic edit changed last-good snapshot: generation=%d member=%q", s.Generation, firstReloadMember(s.Config))
	}
}

func TestWatchRosterReloadNeverPublishesMixedDerivations(t *testing.T) {
	path, initial := reloadFixture(t, reloadRosterA)
	r, err := newWatchRosterReloader(path, "", initial)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 500; j++ {
				s := r.Snapshot()
				member, below := firstReloadMember(s.Config), firstReloadBelow(s.Config)
				if member != below || (s.Generation == 1 && member != "alpha") || (s.Generation == 2 && member != "beta") {
					t.Errorf("mixed snapshot: generation=%d member=%q derived-below=%q", s.Generation, member, below)
					return
				}
			}
		}()
	}
	close(start)
	writeReloadRoster(t, path, reloadRosterB)
	if adopted, err := r.Check(); err != nil || !adopted {
		t.Fatalf("Check = (%v, %v)", adopted, err)
	}
	wg.Wait()
}

func reloadFixture(t *testing.T, body string) (string, *roster.Config) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "flotilla.json")
	writeReloadRoster(t, path, body)
	cfg, err := roster.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, cfg
}

func writeReloadRoster(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func firstReloadMember(cfg *roster.Config) string {
	bindings := cfg.Bindings()
	if len(bindings) == 0 || len(bindings[0].Members) == 0 {
		return ""
	}
	return bindings[0].Members[0]
}

func firstReloadBelow(cfg *roster.Config) string {
	below := cfg.Org().Children["root"]
	if len(below) == 0 {
		return ""
	}
	return below[0]
}
