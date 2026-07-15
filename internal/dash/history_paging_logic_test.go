package dash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// TestHistoryPagingClientLogic executes the production URL and merge functions.
// This pins desk scoping, bounded limits, stable-ID dedupe, and the exact
// remainder cursor without emulating the full browser DOM.
func TestHistoryPagingClientLogic(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("assets", "dash.js"))
	if err != nil {
		raw, err = os.ReadFile(filepath.Join("internal", "dash", "assets", "dash.js"))
	}
	if err != nil {
		t.Fatalf("read dash.js: %v", err)
	}
	js := string(raw)
	start := strings.Index(js, "  function historyURL(")
	end := strings.Index(js, "  function storeHistoryWindow(")
	if start < 0 || end <= start {
		t.Fatal("could not extract executable history paging helpers")
	}

	vm := goja.New()
	if _, err := vm.RunString("var HISTORY_PAGE_LIMIT = 100;\n" + js[start:end]); err != nil {
		t.Fatalf("load history paging helpers: %v", err)
	}
	urlFn, ok := goja.AssertFunction(vm.Get("historyURL"))
	if !ok {
		t.Fatal("historyURL is not callable")
	}
	url, err := urlFn(goja.Undefined(), vm.ToValue("alpha desk"), vm.ToValue(map[string]any{"meta": true, "before": "17"}))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := url.String(), "/api/history?desk=alpha%20desk&limit=100&meta=1&before=17"; got != want {
		t.Fatalf("historyURL = %q, want %q", got, want)
	}

	mergeFn, ok := goja.AssertFunction(vm.Get("mergeLatestHistory"))
	if !ok {
		t.Fatal("mergeLatestHistory is not callable")
	}
	fresh := map[string]any{
		"ledger": []map[string]any{{"id": "12"}, {"id": "11"}},
		"total":  5,
	}
	existing := map[string]any{
		"ledger": []map[string]any{{"id": "11"}, {"id": "10"}, {"id": "9"}},
	}
	value, err := mergeFn(goja.Undefined(), vm.ToValue(fresh), vm.ToValue(existing))
	if err != nil {
		t.Fatal(err)
	}
	got := value.ToObject(vm)
	ledger := got.Get("ledger").Export().([]any)
	ids := make([]string, 0, len(ledger))
	for _, rawEntry := range ledger {
		ids = append(ids, rawEntry.(map[string]any)["id"].(string))
	}
	if strings.Join(ids, ",") != "12,11,10,9" {
		t.Fatalf("merged IDs = %v", ids)
	}
	if got.Get("remaining").ToInteger() != 1 || !got.Get("has_more").ToBoolean() || got.Get("next_cursor").String() != "9" {
		t.Fatalf("merged cursor contract = %v", got.Export())
	}
}
