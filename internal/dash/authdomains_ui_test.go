package dash

import (
	"io/fs"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAuthDomainsUICopyCannotOverclaim(t *testing.T) {
	htmlBytes, err := fs.ReadFile(assetsFS, "assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	jsBytes, err := fs.ReadFile(assetsFS, "assets/auth-domains.js")
	if err != nil {
		t.Fatal(err)
	}
	html, js := string(htmlBytes), string(jsBytes)
	copySurface := html + "\n" + js
	for _, required := range []string{
		"SHADOW · NOT ENFORCING",
		"A passing result does not authorize or materialize a production effect",
		"Claimed DomainContext",
		"Server-resolved DomainContext",
		"Unknown or untraced critical seams are failures",
		"No mutation controls, grant apply/revoke actions, credential locators, or production effects are present",
	} {
		if !strings.Contains(copySurface, required) {
			t.Errorf("missing anti-overclaim copy %q", required)
		}
	}
	if strings.Contains(js, `method: "POST"`) || strings.Contains(js, `method: "PUT"`) || strings.Contains(js, `method: "DELETE"`) {
		t.Fatal("Auth Domains status UI must issue reads only")
	}
	if !strings.Contains(js, `fetch("/api/auth-domains/status"`) {
		t.Fatal("Auth Domains status UI does not read the status API")
	}
}

func TestAuthDomainsAbsentAndCorruptRendered(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Chromium regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatal(err)
	}
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(func() { httpServer.CloseClientConnections(); httpServer.Close() })

	script := `
import json
import sys
from playwright.sync_api import sync_playwright, expect

url = sys.argv[1]
absent = {
  "schema_version":"authorization-domains/status/v1", "mode":"shadow", "enforcement":False,
  "label":"SHADOW · NOT ENFORCING", "contract":{"revision":"3f0b3e1", "actions":[], "claim_invalidators":[]},
  "generation":{"state":"absent"}, "replay":{"state":"absent", "records":[]},
  "audit_wal":{"state":"absent", "health":"unknown"},
  "lifecycle":{"state":"absent", "claimed_isolation":"unknown", "effective_claim":"unproved", "invalidators":[]},
  "coverage":[], "coverage_summary":{"coverage_failures":1}
}
corrupt = dict(absent)
corrupt.update({"generation":{"state":"corrupt", "failure":"generation.json: corrupt JSON"},
                "replay":{"state":"corrupt", "records":[], "failure":"neutral-replay.json: corrupt JSON"},
                "audit_wal":{"state":"corrupt", "health":"failed", "failure":"audit-health.json: corrupt JSON"},
                "errors":["generation.json: corrupt JSON", "neutral-replay.json: corrupt JSON"]})

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        page = browser.new_page(viewport={"width":390, "height":844})
        page.add_init_script("window.EventSource = undefined")
        current = {"doc": absent}
        page.route("**/api/auth-domains/status", lambda route: route.fulfill(
            status=200, content_type="application/json", body=json.dumps(current["doc"])))
        page.goto(url, wait_until="domcontentloaded")
        page.locator("#tab-auth-domains").click()
        expect(page.locator("#auth-domains-mode")).to_have_text("SHADOW · NOT ENFORCING")
        expect(page.locator("#auth-generation-state")).to_have_text("absent")
        expect(page.locator("#auth-coverage-rows")).to_contain_text("coverage failure")
        expect(page.locator("#auth-lifecycle-state")).to_have_text("unproved")
        current["doc"] = corrupt
        page.evaluate("() => window.flotillaAuthDomains.refresh()")
        expect(page.locator("#auth-domains-error")).to_contain_text("NOT ENFORCING")
        expect(page.locator("#auth-generation-state")).to_have_text("corrupt")
        assert page.locator("#view-auth-domains form").count() == 0
        assert page.locator("#view-auth-domains button").count() == 0
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Auth Domains regression: %v\n%s", err, out)
	}
}
