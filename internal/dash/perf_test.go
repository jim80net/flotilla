package dash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
)

func loadPerfPure(t *testing.T) (*goja.Runtime, *goja.Object) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("assets", "perf.js"))
	if err != nil {
		t.Fatal(err)
	}
	vm := goja.New()
	window := vm.GlobalObject()
	_ = vm.Set("window", window)
	perf := vm.NewObject()
	_ = perf.Set("now", func() float64 { return 1 })
	_ = perf.Set("mark", func(string) {})
	_ = perf.Set("getEntriesByName", func() []any { return nil })
	_ = perf.Set("getEntriesByType", func() []any { return nil })
	_ = vm.Set("performance", perf)
	_ = window.Set("performance", perf)
	doc := vm.NewObject()
	body := vm.NewObject()
	_ = body.Set("getAttribute", func(string) string { return "unavailable" })
	_ = doc.Set("body", body)
	_ = doc.Set("getElementById", func(string) any { return nil })
	_ = vm.Set("document", doc)
	location := vm.NewObject()
	_ = location.Set("hostname", "127.0.0.1")
	_ = location.Set("origin", "http://127.0.0.1:8787")
	_ = location.Set("href", "http://127.0.0.1:8787/")
	_ = vm.Set("location", location)
	storage := vm.NewObject()
	_ = storage.Set("getItem", func(string) any { return nil })
	_ = storage.Set("setItem", func(string, string) {})
	_ = vm.Set("localStorage", storage)
	_ = vm.Set("navigator", vm.NewObject())
	if _, err := vm.RunString(string(raw)); err != nil {
		t.Fatalf("run perf.js without PerformanceObserver: %v", err)
	}
	pure := window.Get("flotillaPerfPure")
	if goja.IsUndefined(pure) || goja.IsNull(pure) {
		t.Fatal("perf.js did not expose pure shaping helpers")
	}
	if api := window.Get("flotillaPerf"); goja.IsUndefined(api) || goja.IsNull(api) {
		t.Fatal("unsupported PerformanceObserver must not prevent diagnostics initialization")
	}
	return vm, pure.ToObject(vm)
}

func callPerfJSON(t *testing.T, vm *goja.Runtime, obj *goja.Object, name string, args ...any) string {
	t.Helper()
	fn, ok := goja.AssertFunction(obj.Get(name))
	if !ok {
		t.Fatalf("%s is not callable", name)
	}
	values := make([]goja.Value, len(args))
	for i := range args {
		values[i] = vm.ToValue(args[i])
	}
	got, err := fn(goja.Undefined(), values...)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	b, err := json.Marshal(got.Export())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPerfMetricShapingRedactsToFixedClasses(t *testing.T) {
	vm, pure := loadPerfPure(t)
	entry := map[string]any{
		"duration": 12.345, "transferSize": 400, "encodedBodySize": 300,
		"decodedBodySize": 500, "initiatorType": "fetch",
		"name":    "http://secret-host/api/issues/747?token=secret",
		"headers": map[string]string{"Authorization": "secret"},
		"serverTiming": []map[string]any{
			{"name": "github-list", "duration": 7.25, "description": "private deployment"},
			{"name": "desk-secret", "duration": 1.5},
		},
	}
	got := callPerfJSON(t, vm, pure, "shapeResource", entry, "/api/issues/747")
	for _, leak := range []string{"secret-host", "token", "Authorization", "private deployment", "desk-secret", "747"} {
		if strings.Contains(got, leak) {
			t.Fatalf("shaped resource leaked %q: %s", leak, got)
		}
	}
	for _, want := range []string{`"endpoint_class":"/api/issues/:item"`, `"stage":"github-list"`, `"stage":"other"`, `"duration_ms":12.3`} {
		if !strings.Contains(got, want) {
			t.Errorf("shaped resource missing %s: %s", want, got)
		}
	}
}

func TestPerfRingHasCountAndSerializedByteCaps(t *testing.T) {
	vm, pure := loadPerfPure(t)
	existing := make([]map[string]any, 25)
	for i := range existing {
		existing[i] = map[string]any{"sample": i}
	}
	got := callPerfJSON(t, vm, pure, "boundSamples", existing, map[string]any{"sample": 25}, 20, 131072)
	var ring []map[string]any
	if err := json.Unmarshal([]byte(got), &ring); err != nil {
		t.Fatal(err)
	}
	if len(ring) != 20 || ring[0]["sample"] != float64(6) || ring[19]["sample"] != float64(25) {
		t.Fatalf("count-bounded ring = %s", got)
	}

	over := callPerfJSON(t, vm, pure, "boundSamples", nil, map[string]any{"oversize": strings.Repeat("x", 1024)}, 20, 128)
	if over != "[]" {
		t.Fatalf("individually over-cap sample must fail closed, got %s", over)
	}
	bytesJSON := callPerfJSON(t, vm, pure, "utf8Bytes", "€")
	if bytesJSON != "3" {
		t.Fatalf("utf8Bytes(€) = %s, want 3", bytesJSON)
	}
}

func TestPerfAssetsAndBuildRevisionSeam(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	page := doGet(t, srv, "/").Body.String()
	for _, marker := range []string{`performance.mark("dash-start")`, `data-build-revision="unavailable"`, `/static/perf.js`, `id="perf-copy"`, `id="perf-save"`} {
		if !strings.Contains(page, marker) {
			t.Errorf("index missing performance marker %q", marker)
		}
	}
	js := doGet(t, srv, "/static/perf.js").Body.String()
	for _, forbidden := range []string{"fetch(", "XMLHttpRequest", "sendBeacon", "new Image", "WebSocket", "EventSource"} {
		if strings.Contains(js, forbidden) {
			t.Errorf("perf collection must not create a request (found %q)", forbidden)
		}
	}
	if got := normalizeBuildRevision("508af35"); got != "508af35" {
		t.Fatalf("normalizeBuildRevision(valid) = %q", got)
	}
	for _, invalid := range []string{"", "inferred-main", "508AF35", "abc<script"} {
		if got := normalizeBuildRevision(invalid); got != "unavailable" {
			t.Errorf("normalizeBuildRevision(%q) = %q, want unavailable", invalid, got)
		}
	}
}
