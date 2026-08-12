package surface

import (
	"errors"
	"testing"
	"time"
)

type temporalFixtureDriver struct {
	state  State
	result string
}

func (d *temporalFixtureDriver) Name() string                             { return "fixture" }
func (d *temporalFixtureDriver) Submit(string, string) error              { return nil }
func (d *temporalFixtureDriver) Assess(string) State                      { return d.state }
func (d *temporalFixtureDriver) Rotate(string) error                      { return nil }
func (d *temporalFixtureDriver) RotateStrategy() Strategy                 { return SlashCommand }
func (d *temporalFixtureDriver) Close(string) error                       { return nil }
func (d *temporalFixtureDriver) LatestResult(string) (string, error)      { return d.result, nil }
func (d *temporalFixtureDriver) ComposerState(string) ComposerDisposition { return ComposerCleared }

func TestTemporalStaticWorkingWithFinalBecomesWedge(t *testing.T) {
	now := time.Unix(0, 0)
	frame := "static working frame"
	tracker := NewTemporalClassifier(func(string) (string, error) { return frame, nil }).WithTiming(func() time.Time { return now }, time.Minute)
	d := &temporalFixtureDriver{state: StateWorking, result: "completed result"}
	if got := tracker.Assess(d, "p"); got != StateWorking {
		t.Fatalf("first=%s", got)
	}
	now = now.Add(time.Minute)
	if got := tracker.Assess(d, "p"); got != StateWedge {
		t.Fatalf("bounded=%s want wedge", got)
	}
}

func TestTemporalMovingPaneKeepsFastPath(t *testing.T) {
	now := time.Unix(0, 0)
	frame := "frame-a"
	tracker := NewTemporalClassifier(func(string) (string, error) { return frame, nil }).WithTiming(func() time.Time { return now }, time.Minute)
	d := &temporalFixtureDriver{state: StateWorking, result: "prior final"}
	tracker.Assess(d, "p")
	now = now.Add(time.Minute)
	frame = "frame-b"
	if got := tracker.Assess(d, "p"); got != StateWorking {
		t.Fatalf("moving=%s", got)
	}
	now = now.Add(time.Minute - time.Second)
	if got := tracker.Assess(d, "p"); got != StateWorking {
		t.Fatalf("reset window=%s", got)
	}
}

func TestTemporalNeedsPositiveCompletedResultEvidence(t *testing.T) {
	now := time.Unix(0, 0)
	tracker := NewTemporalClassifier(func(string) (string, error) { return "static", nil }).WithTiming(func() time.Time { return now }, time.Second)
	d := &temporalFixtureDriver{state: StateWorking}
	tracker.Assess(d, "p")
	now = now.Add(time.Hour)
	if got := tracker.Assess(d, "p"); got != StateWorking {
		t.Fatalf("no result=%s", got)
	}
	d.result = "final"
	tracker.capture = func(string) (string, error) { return "", errors.New("capture") }
	if got := tracker.Assess(d, "p"); got != StateWorking {
		t.Fatalf("capture error=%s", got)
	}
}

func TestConfirmRefusesTemporalWedge(t *testing.T) {
	d := &temporalFixtureDriver{state: StateWedge, result: "final"}
	err := (Confirm{Sleep: func(time.Duration) {}}).Submit(d, "p", "body")
	if !errors.Is(err, ErrWedge) {
		t.Fatalf("err=%v want ErrWedge", err)
	}
}
