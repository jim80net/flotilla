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

	if !strings.Contains(css, "grid-template-columns: minmax(0, 3.25fr)") {
		t.Error("Walk 33 mobile chrome CSS missing the single-row five-destination grid")
	}
	mobileStart := strings.Index(css, "@media (max-width: 640px) {")
	if mobileStart < 0 {
		t.Fatal("Walk 33 mobile chrome CSS missing the 640px media block")
	}
	mobileCSS := css[mobileStart:]
	if next := strings.Index(mobileCSS[1:], "\n@media "); next >= 0 {
		mobileCSS = mobileCSS[:next+1]
	}
	for _, marker := range []string{
		"html.conv-chooser-open, body.conv-chooser-open",
		"body.conv-chooser-open > .bar",
		"body.conv-chooser-open #view-conversations",
		"height: min(78dvh, 620px)",
	} {
		if !strings.Contains(mobileCSS, marker) {
			t.Errorf("Walk 33 640px overlay CSS missing %q", marker)
		}
	}
	railStart := strings.Index(mobileCSS, ".conv-nav.mobile-expanded .conv-rail-list {")
	if railStart < 0 {
		t.Fatal("Walk 33 640px overlay CSS missing the expanded rail rule")
	}
	railRule := mobileCSS[railStart:]
	if end := strings.Index(railRule, "}"); end >= 0 {
		railRule = railRule[:end]
	}
	for _, marker := range []string{"min-height: 0", "overflow-y: auto"} {
		if !strings.Contains(railRule, marker) {
			t.Errorf("Walk 33 expanded rail rule missing %q", marker)
		}
	}
	for _, marker := range []string{
		"function closeChooser(restoreFocus)",
		"document.documentElement.classList",
		"if (chooserWasOpen) closeChooser(false)",
		"if (chooserWasOpen) closeChooser(true)",
		`if (view !== "conversations") closeChooser(false)`,
		`event.key !== "Escape"`,
	} {
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
