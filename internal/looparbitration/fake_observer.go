package looparbitration

// FakeObserver is a test double for LoopObserver.
type FakeObserver struct {
	Postures    map[string]Posture
	GoalActives map[string]bool
}

func (f *FakeObserver) Posture(agent string) (Posture, bool) {
	if f == nil || f.Postures == nil {
		return "", false
	}
	p, ok := f.Postures[agent]
	return p, ok
}

func (f *FakeObserver) GoalActive(agent string) (bool, bool) {
	if f == nil || f.GoalActives == nil {
		return false, false
	}
	g, ok := f.GoalActives[agent]
	return g, ok
}
