package watch

import "github.com/jim80net/flotilla/internal/surface"

// AuthStateAssessor is the production classification→episode→alarm composition.
// Construction binds an Injector once; callers provide only the observed seat identity,
// driver, and pane.
type AuthStateAssessor struct{ injector *Injector }

func NewAuthStateAssessor(injector *Injector) AuthStateAssessor {
	return AuthStateAssessor{injector: injector}
}

func (a AuthStateAssessor) Assess(agent string, driver surface.Driver, pane string) surface.State {
	return surface.AssessForFleetAuth(driver, pane, func(observation surface.AuthObservation) bool {
		return a.injector != nil && a.injector.ObserveAuthState(agent, observation)
	})
}
