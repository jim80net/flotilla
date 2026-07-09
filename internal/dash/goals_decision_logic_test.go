package dash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// classListStub returns a minimal classList object (add/remove/toggle/contains).
func classListStub(vm *goja.Runtime) *goja.Object {
	o := vm.NewObject()
	noop := func(goja.FunctionCall) goja.Value { return goja.Undefined() }
	_ = o.Set("add", noop)
	_ = o.Set("remove", noop)
	_ = o.Set("toggle", noop)
	_ = o.Set("contains", func(goja.FunctionCall) goja.Value { return vm.ToValue(false) })
	return o
}

// loadGoalsDecisionVM boots goals.js under goja with a minimal DOM + flotillaDash
// stub so flotillaGoals._test (pure decision-room callables) is executable (#509).
// Substring greps of goals.js cannot catch a removed hasBrief() gate (#501 class).
func loadGoalsDecisionVM(t *testing.T) (*goja.Runtime, *goja.Object) {
	t.Helper()
	// Prefer embed path next to this package; fall back to source tree for -count runs.
	jsPath := filepath.Join("assets", "goals.js")
	raw, err := os.ReadFile(jsPath)
	if err != nil {
		// When tests run with package dir as cwd this works; otherwise try module root.
		raw, err = os.ReadFile(filepath.Join("internal", "dash", "assets", "goals.js"))
	}
	if err != nil {
		t.Fatalf("read goals.js: %v", err)
	}

	vm := goja.New()
	// console.log → discard (goals.js does not require it; stubs may).
	console := vm.NewObject()
	_ = console.Set("log", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	_ = console.Set("warn", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	_ = console.Set("error", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	_ = vm.Set("console", console)

	// Minimal Promise polyfill (goja has no built-in Promise; goals.js uses .then/.catch).
	if _, err := vm.RunString(minimalPromiseJS); err != nil {
		t.Fatalf("promise polyfill: %v", err)
	}

	// document / window stubs — goals.js only needs getElementById at load (wire hooks no-op).
	doc := vm.NewObject()
	body := vm.NewObject()
	_ = body.Set("getAttribute", func(call goja.FunctionCall) goja.Value {
		if call.Argument(0).String() == "data-xo" {
			return vm.ToValue("meta-xo")
		}
		return vm.ToValue("")
	})
	_ = body.Set("classList", vm.NewObject())
	_ = doc.Set("body", body)
	_ = doc.Set("getElementById", func(goja.FunctionCall) goja.Value { return goja.Null() })
	_ = doc.Set("querySelector", func(goja.FunctionCall) goja.Value { return goja.Null() })
	_ = doc.Set("addEventListener", func(goja.FunctionCall) goja.Value { return goja.Undefined() })

	win := vm.NewObject()
	_ = win.Set("document", doc)
	// No-op event wiring — goals.js registers resize/keyboard listeners at load.
	noop := func(goja.FunctionCall) goja.Value { return goja.Undefined() }
	_ = win.Set("addEventListener", noop)
	_ = win.Set("removeEventListener", noop)
	_ = doc.Set("addEventListener", noop)
	_ = doc.Set("removeEventListener", noop)
	_ = doc.Set("createElement", func(goja.FunctionCall) goja.Value {
		el := vm.NewObject()
		_ = el.Set("style", vm.NewObject())
		_ = el.Set("classList", classListStub(vm))
		_ = el.Set("setAttribute", noop)
		_ = el.Set("addEventListener", noop)
		_ = el.Set("appendChild", noop)
		return el
	})
	_ = doc.Set("contains", func(goja.FunctionCall) goja.Value { return vm.ToValue(false) })
	_ = body.Set("appendChild", noop)
	_ = vm.Set("document", doc)
	_ = vm.Set("window", win)
	// clearTimeout / setTimeout: promise polyfill defines setTimeout; clearTimeout no-ops.
	_ = vm.Set("clearTimeout", noop)

	// flotillaDash.escapeHtml mirrors dash.js (XSS-safe string escape).
	dash := vm.NewObject()
	_ = dash.Set("escapeHtml", func(call goja.FunctionCall) goja.Value {
		s := call.Argument(0).String()
		r := strings.NewReplacer(
			"&", "&amp;",
			"<", "&lt;",
			">", "&gt;",
			`"`, "&quot;",
			"'", "&#39;",
		)
		return vm.ToValue(r.Replace(s))
	})
	_ = dash.Set("getJSON", func(call goja.FunctionCall) goja.Value {
		// Return a resolved Promise of null (prime badge path no-ops).
		p, err := vm.RunString(`Promise.resolve(null)`)
		if err != nil {
			t.Fatalf("Promise.resolve: %v", err)
		}
		return p
	})
	// postJSON is overridden per-test for respond outcome checks.
	_ = dash.Set("postJSON", func(call goja.FunctionCall) goja.Value {
		p, err := vm.RunString(`Promise.resolve({outcome:"delivered",target:"backend"})`)
		if err != nil {
			t.Fatalf("Promise.resolve post: %v", err)
		}
		return p
	})
	_ = win.Set("flotillaDash", dash)
	_ = vm.Set("flotillaDash", dash)

	if _, err := vm.RunString(string(raw)); err != nil {
		t.Fatalf("eval goals.js: %v", err)
	}
	fg := win.Get("flotillaGoals")
	if fg == nil || goja.IsUndefined(fg) || goja.IsNull(fg) {
		t.Fatal("window.flotillaGoals missing after goals.js load")
	}
	testAPI := fg.ToObject(vm).Get("_test")
	if testAPI == nil || goja.IsUndefined(testAPI) || goja.IsNull(testAPI) {
		t.Fatal("flotillaGoals._test missing — #509 export required for executable regression")
	}
	return vm, testAPI.ToObject(vm)
}

// synthetic decision fixtures — generic roles only (public fixtures).
func decisionFixturesJS() string {
	return `
var fixtures = {
  byId: {
    "proj-a": {
      id: "proj-a", title: "Project Alpha", owner: "backend",
      conversation_agent: "backend",
      status_display: "awaiting",
      brief: "  ",  // whitespace-only — must FAIL closed into preparing
      work_items: [],
      children: [], parent: null
    },
    "proj-b": {
      id: "proj-b", title: "Project Beta", owner: "frontend",
      conversation_agent: "frontend",
      status_display: "awaiting",
      brief: "## What it is\nShip the gate.\n\n## Recommendation\nApprove.",
      work_items: [],
      children: [], parent: null
    },
    "proj-c": {
      id: "proj-c", title: "Project Gamma", owner: "data",
      conversation_agent: "data",
      status_display: "active",
      brief: "non-gated brief must not pollute decisions",
      work_items: [
        { class: "awaiting", label: "needs brief", kind: "inline", brief: "" },
        { class: "awaiting", label: "ready item", kind: "inline",
          brief: "### Concrete value\nUnblock the lane.\n### Recommendation\nYes." }
      ],
      children: [], parent: null
    }
  }
};
`
}

func TestGatherDecisionsFailClosedExecutable509(t *testing.T) {
	vm, api := loadGoalsDecisionVM(t)
	if _, err := vm.RunString(decisionFixturesJS()); err != nil {
		t.Fatal(err)
	}
	// Call gatherDecisions(fixtures.byId)
	gather, ok := goja.AssertFunction(api.Get("gatherDecisions"))
	if !ok {
		t.Fatal("gatherDecisions not a function")
	}
	byId := vm.Get("fixtures").ToObject(vm).Get("byId")
	res, err := gather(goja.Undefined(), byId)
	if err != nil {
		t.Fatalf("gatherDecisions: %v", err)
	}
	obj := res.ToObject(vm)
	decisions := obj.Get("decisions").Export()
	preparing := obj.Get("preparing").Export()

	decList, _ := decisions.([]interface{})
	prepList, _ := preparing.([]interface{})

	// proj-a: gated + whitespace brief → preparing, never decisions
	// proj-b: gated + real brief → decisions
	// proj-c: work item empty brief → preparing; work item with brief → decisions
	if len(decList) != 2 {
		t.Fatalf("decisions len = %d, want 2 (proj-b node + proj-c ready item); got %#v", len(decList), decList)
	}
	if len(prepList) != 2 {
		t.Fatalf("preparing len = %d, want 2 (proj-a + proj-c empty item); got %#v", len(prepList), prepList)
	}

	// No decision entry may reference proj-a (whitespace brief fail-closed).
	for _, d := range decList {
		m, _ := d.(map[string]interface{})
		node, _ := m["node"].(map[string]interface{})
		if id, _ := node["id"].(string); id == "proj-a" {
			t.Fatal("proj-a (whitespace brief) must never appear in decisions bucket")
		}
	}
	// At least one preparing entry is proj-a.
	foundPrepA := false
	for _, p := range prepList {
		m, _ := p.(map[string]interface{})
		node, _ := m["node"].(map[string]interface{})
		if id, _ := node["id"].(string); id == "proj-a" {
			foundPrepA = true
		}
	}
	if !foundPrepA {
		t.Fatal("proj-a must appear in preparing bucket")
	}

	// Mutating-gate regression: hasBrief must reject whitespace (the #501 defect class).
	hasBrief, ok := goja.AssertFunction(api.Get("hasBrief"))
	if !ok {
		t.Fatal("hasBrief not a function")
	}
	for _, s := range []string{"", "  ", "\n\t"} {
		v, err := hasBrief(goja.Undefined(), vm.ToValue(s))
		if err != nil {
			t.Fatal(err)
		}
		if v.ToBoolean() {
			t.Fatalf("hasBrief(%q) = true, want false (fail-closed)", s)
		}
	}
	v, err := hasBrief(goja.Undefined(), vm.ToValue("real brief"))
	if err != nil || !v.ToBoolean() {
		t.Fatalf("hasBrief(real) = %v %v, want true", v, err)
	}
}

func TestRenderDecisionCardRespondAffordance509(t *testing.T) {
	vm, _ := loadGoalsDecisionVM(t)
	if _, err := vm.RunString(decisionFixturesJS()); err != nil {
		t.Fatal(err)
	}
	// Use JS to find and render proj-b decision card
	script := `
(function() {
  var g = flotillaGoals._test.gatherDecisions(fixtures.byId);
  var dec = null;
  for (var i = 0; i < g.decisions.length; i++) {
    if (g.decisions[i].node.id === "proj-b") { dec = g.decisions[i]; break; }
  }
  if (!dec) throw new Error("proj-b decision missing");
  return flotillaGoals._test.renderDecisionCard(dec, 0, 1);
})()
`
	// flotillaGoals is on window — set global
	_ = vm.Set("flotillaGoals", vm.Get("window").ToObject(vm).Get("flotillaGoals"))
	htmlVal, err := vm.RunString(script)
	if err != nil {
		t.Fatalf("renderDecisionCard: %v", err)
	}
	html := htmlVal.String()
	if !strings.Contains(html, `class="gdec-respond"`) {
		t.Fatalf("decision card missing .gdec-respond affordance:\n%s", html)
	}
	if !strings.Contains(html, `data-resp-target="frontend"`) {
		t.Fatalf("respond target must be conversation_agent frontend:\n%s", html)
	}
	if !strings.Contains(html, `data-resp-goal="proj-b"`) {
		t.Fatalf("respond goal id missing:\n%s", html)
	}
	// Preparing rows must never include respond affordance
	prepScript := `
(function() {
  var g = flotillaGoals._test.gatherDecisions(fixtures.byId);
  var html = "";
  for (var i = 0; i < g.preparing.length; i++) {
    html += flotillaGoals._test.renderPreparingRow(g.preparing[i]);
  }
  return html;
})()
`
	prepVal, err := vm.RunString(prepScript)
	if err != nil {
		t.Fatalf("renderPreparingRow: %v", err)
	}
	prepHTML := prepVal.String()
	if strings.Contains(prepHTML, "gdec-respond") {
		t.Fatalf("preparing bucket must not render .gdec-respond:\n%s", prepHTML)
	}
	if !strings.Contains(prepHTML, "gdec-prep-row") {
		t.Fatalf("preparing rows missing:\n%s", prepHTML)
	}
}

func TestFormatRespondOutcomeCardAndModalIdentical509(t *testing.T) {
	vm, api := loadGoalsDecisionVM(t)
	fmtOut, ok := goja.AssertFunction(api.Get("formatRespondOutcome"))
	if !ok {
		t.Fatal("formatRespondOutcome not a function")
	}
	// Same pure formatter is the only path from server response → UI string for
	// both card Send and modal Send (sendDecisionResponse → formatRespondOutcome).
	cases := []struct {
		res  map[string]interface{}
		want string
	}{
		{
			map[string]interface{}{"outcome": "delivered", "target": "backend"},
			"Delivered to backend — turn confirmed.",
		},
		{
			map[string]interface{}{"outcome": "queued", "target": "frontend", "queued_id": "abc", "detail": "busy"},
			"Queued durably for frontend (id abc) — the fleet daemon delivers it when the desk can receive. (busy)",
		},
		{
			map[string]interface{}{"outcome": "mystery"},
			"Response state unclear — check the desk's conversation thread.",
		},
	}
	for _, tc := range cases {
		v, err := fmtOut(goja.Undefined(), vm.ToValue(tc.res))
		if err != nil {
			t.Fatal(err)
		}
		if got := v.String(); got != tc.want {
			t.Errorf("formatRespondOutcome(%v) = %q, want %q", tc.res, got, tc.want)
		}
	}

	// sendDecisionResponse resolves to the same string (shared path).
	_ = vm.Set("flotillaGoals", vm.Get("window").ToObject(vm).Get("flotillaGoals"))
	// Override postJSON to return a fixed delivered payload.
	dash := vm.Get("window").ToObject(vm).Get("flotillaDash").ToObject(vm)
	_ = dash.Set("postJSON", func(call goja.FunctionCall) goja.Value {
		p, err := vm.RunString(`Promise.resolve({outcome:"delivered",target:"backend"})`)
		if err != nil {
			panic(err)
		}
		return p
	})
	line, err := vm.RunString(`
(function() {
  var out = null, done = false, err = null;
  flotillaGoals._test.sendDecisionResponse("backend", "proj-b", "", "approve")
    .then(function(line) { out = line; done = true; })
    .catch(function(e) { err = String(e); done = true; });
  // Minimal Promise resolves synchronously in our polyfill when already fulfilled.
  if (!done) throw new Error("promise did not settle synchronously");
  if (err) throw new Error(err);
  return out;
})()
`)
	if err != nil {
		t.Fatalf("sendDecisionResponse: %v", err)
	}
	want := "Delivered to backend — turn confirmed."
	if line.String() != want {
		t.Fatalf("sendDecisionResponse outcome = %q, want %q (must match formatRespondOutcome)", line.String(), want)
	}
}

// minimalPromiseJS is a tiny Promise implementation sufficient for goals.js
// .then/.catch chains under goja (no native Promise).
const minimalPromiseJS = `
function Promise(executor) {
  var self = this;
  self._state = "pending";
  self._value = undefined;
  self._handlers = [];
  function settle(state, value) {
    if (self._state !== "pending") return;
    if (state === "fulfilled" && value && typeof value.then === "function") {
      value.then(function (v) { settle("fulfilled", v); }, function (e) { settle("rejected", e); });
      return;
    }
    self._state = state;
    self._value = value;
    self._handlers.splice(0).forEach(run);
  }
  function run(h) {
    if (self._state === "pending") { self._handlers.push(h); return; }
    setTimeout(function () {
      var cb = self._state === "fulfilled" ? h.onFulfilled : h.onRejected;
      if (typeof cb !== "function") {
        if (self._state === "fulfilled") h.resolve(self._value); else h.reject(self._value);
        return;
      }
      try { h.resolve(cb(self._value)); } catch (e) { h.reject(e); }
    }, 0);
  }
  this.then = function (onFulfilled, onRejected) {
    return new Promise(function (resolve, reject) {
      run({ onFulfilled: onFulfilled, onRejected: onRejected, resolve: resolve, reject: reject });
    });
  };
  this.catch = function (onRejected) { return this.then(null, onRejected); };
  try { executor(function (v) { settle("fulfilled", v); }, function (e) { settle("rejected", e); }); }
  catch (e) { settle("rejected", e); }
}
Promise.resolve = function (v) { return new Promise(function (r) { r(v); }); };
Promise.reject = function (e) { return new Promise(function (_, j) { j(e); }); };
// goja has no setTimeout — make it synchronous so tests settle immediately.
function setTimeout(fn, _ms) { fn(); }
`
