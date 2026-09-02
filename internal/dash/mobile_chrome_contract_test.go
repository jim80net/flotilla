package dash

import (
	"strings"
	"testing"
	"time"
)

func TestWalk33MobileChromeContract(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	css := doGet(t, srv, "/static/dash.css").Body.String()
	dashJS := doGet(t, srv, "/static/dash.js").Body.String()
	trackerJS := doGet(t, srv, "/static/tracker.js").Body.String()

	for _, marker := range []string{
		"grid-template-columns: minmax(0, 3.25fr)",
		".conv-nav.mobile-expanded",
		"height: min(78dvh, 620px)",
		"overflow-y: auto",
	} {
		if !strings.Contains(css, marker) {
			t.Errorf("Walk 33 mobile chrome CSS missing %q", marker)
		}
	}
	for _, marker := range []string{"conv-chooser-open", `event.key !== "Escape"`} {
		if !strings.Contains(dashJS, marker) {
			t.Errorf("Walk 33 desk chooser behavior missing %q", marker)
		}
	}
	for _, marker := range []string{
		`<details class="issue-ledger-section">`,
		`<details class="issue-desk issue-desk-fold">`,
	} {
		if !strings.Contains(trackerJS, marker) {
			t.Errorf("Walk 33 folded Issues renderer missing %q", marker)
		}
	}
}
