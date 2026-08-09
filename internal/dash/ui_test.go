package dash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
)

func loadUIHelpers(t *testing.T) (*goja.Runtime, *goja.Object) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("assets", "ui.js"))
	if err != nil {
		t.Fatal(err)
	}
	vm := goja.New()
	window := vm.NewObject()
	if err := vm.Set("window", window); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.RunString(string(raw)); err != nil {
		t.Fatal(err)
	}
	return vm, window.Get("flotillaUI").ToObject(vm)
}

func TestSharedInlineMarkdownRendering961(t *testing.T) {
	vm, ui := loadUIHelpers(t)
	render, ok := goja.AssertFunction(ui.Get("renderInlineMarkdown"))
	if !ok {
		t.Fatal("renderInlineMarkdown not callable")
	}
	got, err := render(goja.Undefined(), vm.ToValue("# Update\n**Safe default:** [review](https://example.test/item) and `hold` <script>"))
	if err != nil {
		t.Fatal(err)
	}
	html := got.String()
	for _, want := range []string{"Update<br>", "<strong>Safe default:</strong>", `href="https://example.test/item"`, "<code>hold</code>", "&lt;script&gt;"} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered markdown missing %q: %s", want, html)
		}
	}
	for _, raw := range []string{"# Update", "**Safe default:**", "[review]("} {
		if strings.Contains(html, raw) {
			t.Errorf("rendered markdown leaked %q: %s", raw, html)
		}
	}
}

func TestSharedRecoverableFailureDisclosure961(t *testing.T) {
	vm, ui := loadUIHelpers(t)
	render, ok := goja.AssertFunction(ui.Get("failurePanel"))
	if !ok {
		t.Fatal("failurePanel not callable")
	}
	got, err := render(goja.Undefined(), vm.ToValue("Parades are unavailable right now."), vm.ToValue("Retry"), vm.ToValue("/api/parades → 503"), vm.ToValue("data-parade-retry"))
	if err != nil {
		t.Fatal(err)
	}
	html := got.String()
	for _, want := range []string{"Parades are unavailable right now.", "data-parade-retry", "Technical details", "/api/parades → 503"} {
		if !strings.Contains(html, want) {
			t.Errorf("failure panel missing %q: %s", want, html)
		}
	}
	if strings.Index(html, "/api/parades") < strings.Index(html, "<details>") {
		t.Fatalf("raw endpoint detail must stay behind disclosure: %s", html)
	}
}

func TestSharedUIAssetWiredAcrossOperatorPages961(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	for _, path := range []string{"/", "/research", "/parade"} {
		body := doGet(t, srv, path).Body.String()
		if !strings.Contains(body, `<script src="/static/ui.js"></script>`) {
			t.Errorf("%s missing shared UI helper", path)
		}
	}
	if got := doGet(t, srv, "/static/ui.js"); got.Code != 200 || !strings.Contains(got.Body.String(), "renderInlineMarkdown") {
		t.Fatalf("shared UI asset response = %d %s", got.Code, got.Body.String())
	}
}

func TestOperatorSummaryAndFailureSinksUseSharedUI961(t *testing.T) {
	checks := map[string][]string{
		"assets/dash.js":     {"renderInlineMarkdown(latest.info", "renderInlineMarkdown(e.body || e.gist)"},
		"assets/research.js": {`research-decision-summary").innerHTML = inline`},
		"assets/tracker.js":  {`failurePanel("The work ledger is unavailable right now."`, "data-ledger-retry"},
		"assets/parade.js":   {`failurePanel("Parades are unavailable right now."`, "data-parade-retry"},
	}
	for path, markers := range checks {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range markers {
			if !strings.Contains(string(raw), marker) {
				t.Errorf("%s missing shared-surface marker %q", path, marker)
			}
		}
	}
}
