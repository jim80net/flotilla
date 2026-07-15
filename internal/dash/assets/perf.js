/* flotilla dash startup diagnostics (#747).
 *
 * Local-only and fail-closed: this module stores fixed-shape timing data in a
 * bounded localStorage ring. It never sends a request and never persists a URL,
 * host, query, content, header, cookie, token, or fleet/deployment identity.
 */
(function () {
  "use strict";

  var STORAGE_KEY = "flotilla.dash.performance.v1";
  var MAX_SAMPLES = 20;
  var MAX_SERIALIZED_BYTES = 131072;
  var MAX_RESOURCES = 80;
  var MAX_LONG_TASKS = 50;
  var API_PATHS = {
    "/api/status": true,
    "/api/topology": true,
    "/api/history": true,
    "/api/goals": true,
    "/api/session-mirror": true,
    "/api/parades": true,
    "/api/issues": true,
    "/api/work-ledger": true,
  };
  var SERVER_STAGES = { "github-list": true, "derive": true, "total": true, "goals-load": true };
  var VIEWS = { conversations: true, goals: true, issues: true, decisions: true };
  var NAV_TYPES = { navigate: true, reload: true, back_forward: true, prerender: true };
  var INITIATORS = { fetch: true, xmlhttprequest: true };

  function finite(value) {
    var n = Number(value);
    return Number.isFinite(n) ? Math.round(n * 10) / 10 : null;
  }

  function nonNegative(value) {
    var n = finite(value);
    return n != null && n >= 0 ? n : null;
  }

  // Fixed classes only. Unknown paths collapse to /api/other; no path component,
  // query, issue number, or control action survives shaping.
  function endpointClass(pathname) {
    if (API_PATHS[pathname]) return pathname;
    if (String(pathname || "").indexOf("/api/issues/") === 0) return "/api/issues/:item";
    if (String(pathname || "").indexOf("/api/control/") === 0) return "/api/control/:action";
    return "/api/other";
  }

  function shapeServerTiming(entries) {
    return (Array.isArray(entries) ? entries : []).slice(0, 16).map(function (entry) {
      return {
        stage: SERVER_STAGES[entry && entry.name] ? entry.name : "other",
        duration_ms: nonNegative(entry && entry.duration),
      };
    });
  }

  function shapeResource(entry, pathname) {
    return {
      endpoint_class: endpointClass(pathname),
      duration_ms: nonNegative(entry && entry.duration),
      transfer_size: nonNegative(entry && entry.transferSize),
      encoded_body_size: nonNegative(entry && entry.encodedBodySize),
      decoded_body_size: nonNegative(entry && entry.decodedBodySize),
      initiator_type: INITIATORS[entry && entry.initiatorType] ? entry.initiatorType : "other",
      server_timing: shapeServerTiming(entry && entry.serverTiming),
    };
  }

  function utf8Bytes(value) {
    var text = String(value || ""), bytes = 0;
    for (var i = 0; i < text.length; i++) {
      var code = text.charCodeAt(i);
      if (code < 0x80) bytes += 1;
      else if (code < 0x800) bytes += 2;
      else if (code >= 0xd800 && code <= 0xdbff && i + 1 < text.length && text.charCodeAt(i + 1) >= 0xdc00 && text.charCodeAt(i + 1) <= 0xdfff) {
        bytes += 4;
        i += 1;
      } else bytes += 3;
    }
    return bytes;
  }

  // Pure retention gate. Newest samples win; an individually over-cap sample is
  // refused instead of silently exceeding the hard serialized-byte ceiling.
  function boundSamples(existing, sample, maxSamples, maxBytes) {
    var ring = (Array.isArray(existing) ? existing : []).slice();
    ring.push(sample);
    while (ring.length > maxSamples) ring.shift();
    while (ring.length && utf8Bytes(JSON.stringify({ schema_version: 1, samples: ring })) > maxBytes) ring.shift();
    return ring;
  }

  var pure = {
    endpointClass: endpointClass,
    shapeServerTiming: shapeServerTiming,
    shapeResource: shapeResource,
    utf8Bytes: utf8Bytes,
    boundSamples: boundSamples,
  };

  // The pure helpers are intentionally exposed for the goja behavior suite. They
  // accept only caller-provided values and perform no collection or persistence.
  window.flotillaPerfPure = pure;

  var support = {
    navigation_timing: (window.performance && typeof performance.getEntriesByType === "function") ? "supported" : "unsupported",
    resource_timing: (window.performance && typeof performance.getEntriesByType === "function") ? "supported" : "unsupported",
    largest_contentful_paint: "unsupported",
    long_task: "unsupported",
    local_persistence: "pending",
  };
  var lcp = null;
  var longTasks = [];
  var observers = [];
  var selectedView = null;
  var completed = false;
  var settled = false;
  var latestSample = null;
  var collectionOverhead = 0;

  function measured(fn) {
    var started = performance && typeof performance.now === "function" ? performance.now() : 0;
    var result = fn();
    if (started) collectionOverhead += Math.max(0, performance.now() - started);
    return result;
  }

  function mark(name) {
    try {
      if (performance && typeof performance.mark === "function" && !performance.getEntriesByName(name, "mark").length) performance.mark(name);
    } catch (_) { /* marks are diagnostics, never a dash dependency */ }
  }

  function observe(type, onEntries) {
    if (typeof window.PerformanceObserver !== "function") return "unsupported";
    try {
      var supported = PerformanceObserver.supportedEntryTypes;
      if (Array.isArray(supported) && supported.indexOf(type) < 0) return "unsupported";
      var observer = new PerformanceObserver(function (list) {
        measured(function () { onEntries(list.getEntries()); });
      });
      observer.observe({ type: type, buffered: true });
      observers.push(observer);
      return "supported";
    } catch (_) {
      return "observer-error";
    }
  }

  support.largest_contentful_paint = observe("largest-contentful-paint", function (entries) {
    var entry = entries.length ? entries[entries.length - 1] : null;
    if (!entry) return;
    // Deliberately omit url, element, id, and loadTime attribution.
    lcp = { start_time_ms: nonNegative(entry.startTime), render_time_ms: nonNegative(entry.renderTime), size: nonNegative(entry.size) };
  });
  support.long_task = observe("longtask", function (entries) {
    entries.forEach(function (entry) {
      if (longTasks.length < MAX_LONG_TASKS) longTasks.push({ start_time_ms: nonNegative(entry.startTime), duration_ms: nonNegative(entry.duration) });
    });
  });

  function accessClass() {
    // Read only to classify, never persist the hostname/IP itself.
    var host = String(location.hostname || "").toLowerCase();
    return host === "localhost" || host === "::1" || /^127(?:\.[0-9]{1,3}){3}$/.test(host) ? "loopback" : "private-network";
  }

  function navigationShape() {
    if (support.navigation_timing !== "supported") return null;
    var entry = performance.getEntriesByType("navigation")[0];
    if (!entry) return null;
    return {
      type: NAV_TYPES[entry.type] ? entry.type : "unknown",
      ttfb_ms: nonNegative(entry.responseStart - entry.startTime),
      dom_content_loaded_ms: entry.domContentLoadedEventEnd > 0 ? nonNegative(entry.domContentLoadedEventEnd - entry.startTime) : null,
      load_event_ms: entry.loadEventEnd > 0 ? nonNegative(entry.loadEventEnd - entry.startTime) : null,
      duration_ms: nonNegative(entry.duration),
      transfer_size: nonNegative(entry.transferSize),
      encoded_body_size: nonNegative(entry.encodedBodySize),
      decoded_body_size: nonNegative(entry.decodedBodySize),
    };
  }

  function resourceShapes() {
    if (support.resource_timing !== "supported") return [];
    var out = [];
    performance.getEntriesByType("resource").some(function (entry) {
      var parsed;
      try { parsed = new URL(entry.name, location.href); } catch (_) { return false; }
      if (parsed.origin !== location.origin || parsed.pathname.indexOf("/api/") !== 0) return false;
      out.push(shapeResource(entry, parsed.pathname));
      return out.length >= MAX_RESOURCES;
    });
    return out;
  }

  function marksShape() {
    var result = {};
    ["dash-start", "first-chrome", "first-selected-view-rendered", "dash-interactive"].forEach(function (name) {
      var entries = performance && performance.getEntriesByName ? performance.getEntriesByName(name, "mark") : [];
      result[name.replace(/-/g, "_") + "_ms"] = entries.length ? nonNegative(entries[0].startTime) : null;
    });
    return result;
  }

  function readRing() {
    try {
      var parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) || "{}");
      support.local_persistence = "supported";
      return parsed && Array.isArray(parsed.samples) ? parsed.samples : [];
    } catch (_) {
      support.local_persistence = "read-error";
      return [];
    }
  }

  function heuristic(existing) {
    // Deterministic run-order heuristic, scoped to this build + access class. It is
    // explicitly not a claim that transferSize proves a browser-cache hit.
    var repeated = existing.some(function (sample) {
      return sample && sample.build_revision === buildRevision() && sample.access_class === accessClass();
    });
    return {
      sample_order: repeated ? "repeat-sample" : "first-sample",
      cold_warm_label: repeated ? "warm-candidate" : "cold-candidate",
      heuristic: "first stored sample for the same build and access class is cold-candidate; later samples are warm-candidate",
      browser_cache_state: "not-determined",
      browser_cache_note: "raw transfer and navigation evidence retained; transferSize alone does not prove browser cache warmth; Server-Timing cache stages are server-side evidence",
    };
  }

  function buildRevision() {
    var raw = document.body && document.body.getAttribute("data-build-revision");
    return /^[0-9a-f]{7,64}$/.test(String(raw || "")) ? raw : "unavailable";
  }

  function persist(sample, existing) {
    var ring = boundSamples(existing, sample, MAX_SAMPLES, MAX_SERIALIZED_BYTES);
    if (!ring.length) {
      support.local_persistence = "sample-over-byte-cap";
      return [];
    }
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ schema_version: 1, samples: ring }));
      support.local_persistence = "supported";
      return ring;
    } catch (_) {
      support.local_persistence = "write-error";
      return [];
    }
  }

  function replaceLatest() {
    if (!latestSample) return;
    var existing = readRing();
    if (existing.length && existing[existing.length - 1].captured_at === latestSample.captured_at) existing.pop();
    persist(latestSample, existing);
  }

  function stopObserversWhenDone() {
    if (!completed || !settled) return;
    observers.forEach(function (observer) { try { observer.disconnect(); } catch (_) {} });
  }

  function finalize(view) {
    measured(function () {
      mark("first-selected-view-rendered");
      mark("dash-interactive");
      var existing = readRing();
      var sample = {
        schema_version: 1,
        captured_at: new Date().toISOString(),
        access_class: accessClass(),
        build_revision: buildRevision(),
        viewport: { width: Math.max(0, Math.round(window.innerWidth || 0)), height: Math.max(0, Math.round(window.innerHeight || 0)) },
        selected_view_class: VIEWS[view] ? view : "unavailable",
        classification: heuristic(existing),
        navigation: navigationShape(),
        api_resources: resourceShapes(),
        marks: marksShape(),
        largest_contentful_paint: { support: support.largest_contentful_paint, entry: lcp },
        long_tasks: { support: support.long_task, entries: longTasks.slice() },
        support: support,
        collector_overhead_ms: null,
      };
      latestSample = sample;
      persist(sample, existing);
    });
    if (latestSample) latestSample.collector_overhead_ms = finite(collectionOverhead);
    // Persist once more so the measured overhead and final persistence status are honest.
    replaceLatest();
    stopObserversWhenDone();
    updateDisclosure();
  }

  // dash.js calls this when its explicitly-started core + optional startup reads
  // have all settled. It updates the waterfall without delaying the interactive
  // mark, whose definition remains the minimum selected-view render.
  function startupSettled() {
    if (settled) return;
    settled = true;
    if (latestSample) {
      measured(function () {
        latestSample.navigation = navigationShape();
        latestSample.api_resources = resourceShapes();
        latestSample.largest_contentful_paint.entry = lcp;
        latestSample.long_tasks.entries = longTasks.slice();
      });
      latestSample.collector_overhead_ms = finite(collectionOverhead);
      replaceLatest();
      updateDisclosure();
    }
    stopObserversWhenDone();
  }

  function selectView(view) {
    if (!completed && VIEWS[view]) selectedView = view;
  }

  function viewRendered(view) {
    if (completed || !selectedView || view !== selectedView) return;
    completed = true;
    finalize(view);
  }

  function exportEnvelope() {
    var samples = readRing();
    return JSON.stringify({
      schema_version: 1,
      privacy: "fixed endpoint classes and numeric browser timings only; no hosts, URLs, queries, content, headers, cookies, tokens, or deployment identities",
      retention: { max_samples: MAX_SAMPLES, max_serialized_bytes: MAX_SERIALIZED_BYTES },
      samples: samples,
    }, null, 2);
  }

  function updateDisclosure(message) {
    var status = document.getElementById("perf-status");
    if (!status) return;
    if (message) { status.textContent = message; return; }
    var count = readRing().length;
    status.textContent = count ? (count + " local startup sample" + (count === 1 ? "" : "s")) : "No local startup sample yet";
  }

  function wireDisclosure() {
    var copy = document.getElementById("perf-copy");
    var save = document.getElementById("perf-save");
    if (copy) copy.addEventListener("click", function () {
      var payload = exportEnvelope();
      if (!navigator.clipboard || typeof navigator.clipboard.writeText !== "function") {
        updateDisclosure("Clipboard unavailable. Use Save JSON file (no network). ");
        return;
      }
      navigator.clipboard.writeText(payload).then(function () {
        updateDisclosure("Startup diagnostics copied as JSON.");
      }).catch(function () {
        updateDisclosure("Clipboard failed. Use Save JSON file (no network). ");
      });
    });
    if (save) save.addEventListener("click", function () {
      try {
        var blob = new Blob([exportEnvelope()], { type: "application/json" });
        var link = document.createElement("a");
        link.href = URL.createObjectURL(blob);
        link.download = "flotilla-dash-startup-diagnostics.json";
        link.click();
        URL.revokeObjectURL(link.href);
        updateDisclosure("Startup diagnostics saved locally.");
      } catch (_) {
        updateDisclosure("Local file export failed; JSON remains available through Copy JSON when clipboard access is restored.");
      }
    });
    updateDisclosure();
  }

  mark("first-chrome");
  wireDisclosure();
  if (typeof window.addEventListener === "function") {
    window.addEventListener("load", function () {
      if (!latestSample) return;
      latestSample.navigation = navigationShape();
      replaceLatest();
    }, { once: true });
  }
  window.flotillaPerf = {
    selectView: selectView,
    viewRendered: viewRendered,
    startupSettled: startupSettled,
    exportJSON: exportEnvelope,
    latestSample: function () { return latestSample; },
  };
})();
