package surface

import (
	"errors"
	"os"
	"testing"
)

func piFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPiRegistered(t *testing.T) {
	d, ok := Get("pi")
	if !ok || d.Name() != "pi" {
		t.Fatalf(`Get("pi") = (%v, %v), want pi driver`, d, ok)
	}
	var _ ComposerStateProbe = pi{}
}

func TestParsePiStateLiveFixtures(t *testing.T) {
	cases := []struct {
		name string
		want State
	}{
		{"pi-0.73.1-idle.txt", StateIdle},
		{"pi-0.73.1-working.txt", StateWorking},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parsePiState(piFixture(t, tc.name)); got != tc.want {
				t.Fatalf("parsePiState = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParsePiStateFailsClosed(t *testing.T) {
	cases := []string{"", "ordinary model output", "────────\n\n────────"}
	for _, captured := range cases {
		if got := parsePiState(captured); got != StateUnknown {
			t.Errorf("parsePiState(%q) = %v, want unknown", captured, got)
		}
	}
}

func TestClassifyPiComposerLine(t *testing.T) {
	rule := "────────────────────────────────────────"
	cases := []struct {
		name, captured string
		x, y           int
		want           ComposerDisposition
	}{
		{"cleared", "output\n" + rule + "\n\n" + rule + "\nfooter", 0, 2, ComposerCleared},
		{"pending", "output\n" + rule + "\nbeta-probe\n" + rule + "\nfooter", 10, 2, ComposerPending},
		{"blank with displaced cursor", "output\n" + rule + "\n\n" + rule + "\nfooter", 3, 2, ComposerUndetermined},
		{"non-composer", "output\nnot a rule\nbeta-probe\nnot a rule", 10, 2, ComposerUndetermined},
		{"out of range", rule, 0, 0, ComposerUndetermined},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyPiComposerLine(tc.captured, tc.x, tc.y); got != tc.want {
				t.Fatalf("classifyPiComposerLine = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPiAssess(t *testing.T) {
	boom := errors.New("tmux boom")
	cases := []struct {
		name       string
		cmdErr     error
		shell      bool
		captured   string
		captureErr error
		want       State
	}{
		{"command error", boom, false, "", nil, StateUnknown},
		{"shell", nil, true, "", nil, StateShell},
		{"capture error", nil, false, "", boom, StateUnknown},
		{"idle", nil, false, piFixture(t, "pi-0.73.1-idle.txt"), nil, StateIdle},
		{"working", nil, false, piFixture(t, "pi-0.73.1-working.txt"), nil, StateWorking},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := pi{
				paneCommand: func(string) (string, error) { return "pi", tc.cmdErr },
				isShell:     func(string) bool { return tc.shell },
				capturePane: func(string) (string, error) { return tc.captured, tc.captureErr },
				classify:    parsePiState,
			}
			if got := p.Assess("alpha"); got != tc.want {
				t.Fatalf("Assess = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPiComposerState(t *testing.T) {
	idle := piFixture(t, "pi-0.73.1-idle.txt")
	_, bodyRow, ok := findPiComposer(idle)
	if !ok {
		t.Fatal("idle fixture does not contain a Pi composer")
	}
	p := pi{
		cursorSnapshot: func(string) (int, int, bool, bool, error) { return 0, bodyRow, false, false, nil },
		capturePane:    func(string) (string, error) { return idle, nil },
		classify:       parsePiState,
	}
	if got := p.ComposerState("alpha"); got != ComposerCleared {
		t.Fatalf("ComposerState = %v, want cleared", got)
	}
	p.cursorSnapshot = func(string) (int, int, bool, bool, error) { return 0, 0, false, true, nil }
	if got := p.ComposerState("alpha"); got != ComposerUndetermined {
		t.Fatalf("copy-mode ComposerState = %v, want undetermined", got)
	}
}

func TestPiSubmitRotateAndClose(t *testing.T) {
	var submitted, injected string
	p := pi{
		send:   func(_ string, text string) error { submitted = text; return nil },
		inject: func(_ string, command string) error { injected = command; return nil },
	}
	if err := p.Submit("alpha", "hello"); err != nil || submitted != "hello" {
		t.Fatalf("Submit = %q, %v", submitted, err)
	}
	if err := p.Rotate("alpha"); err != nil || injected != "/new" {
		t.Fatalf("Rotate injected %q, err %v", injected, err)
	}
	if p.RotateStrategy() != SlashCommand {
		t.Fatalf("RotateStrategy = %v, want slash command", p.RotateStrategy())
	}
	if err := p.Close("alpha"); !errors.Is(err, ErrNoGracefulClose) {
		t.Fatalf("Close err = %v, want ErrNoGracefulClose", err)
	}
}
