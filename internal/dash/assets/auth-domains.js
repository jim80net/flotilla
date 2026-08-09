/* Authorization Domains I1a — read-only SHADOW status. This surface never
 * sends a mutation and never translates shadow evidence into authority. */
(function () {
  "use strict";

  var loaded = false;
  var loading = null;

  function el(id) { return document.getElementById(id); }
  function text(value, fallback) {
    var valueText = value === undefined || value === null ? "" : String(value).trim();
    return valueText || fallback || "unknown";
  }
  function shortDigest(value) {
    var digest = text(value, "");
    return digest.length > 18 ? digest.slice(0, 12) + "…" + digest.slice(-6) : (digest || "unknown");
  }
  function append(parent, tag, className, value) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    node.textContent = value;
    parent.appendChild(node);
    return node;
  }
  function setState(id, value, failure) {
    var target = el(id);
    if (!target) return;
    target.textContent = text(value, "unknown").replace(/_/g, " ");
    target.className = "auth-state auth-state-" + (/^(passed|healthy|loaded|shadow)$/.test(value) ? "observed" : "failure");
    target.title = failure || "";
  }
  function facts(id, rows) {
    var list = el(id);
    if (!list) return;
    list.replaceChildren();
    rows.forEach(function (row) {
      append(list, "dt", "", row[0]);
      append(list, "dd", "", text(row[1], "unknown"));
    });
  }

  function contextSummary(ctx) {
    if (!ctx) return "none supplied";
    return "context " + text(ctx.context_id) + " · domain " + text(ctx.domain_id) +
      " · worker " + text(ctx.worker_id) + " · minted by " + text(ctx.minted_by);
  }

  function renderContexts(records) {
    var root = el("auth-context-records");
    if (!root) return;
    root.replaceChildren();
    if (!records.length) {
      append(root, "p", "empty", "No replay records are available. Claimed and resolved DomainContext are unknown.");
      return;
    }
    records.forEach(function (record) {
      var card = append(root, "article", "auth-context-record", "");
      var head = append(card, "div", "auth-context-head", "");
      append(head, "strong", "", text(record.id, "unnamed replay record"));
      append(head, "span", "auth-context-outcome", text(record.outcome) + " · " + text(record.reason));
      var pair = append(card, "div", "auth-context-pair", "");
      var claimed = append(pair, "div", "auth-context-side claimed", "");
      append(claimed, "span", "auth-context-label", "Claimed DomainContext");
      append(claimed, "p", "", contextSummary(record.claimed_context));
      var resolved = append(pair, "div", "auth-context-side resolved", "");
      append(resolved, "span", "auth-context-label", "Server-resolved DomainContext");
      append(resolved, "p", "", contextSummary(record.resolved_context));
      append(card, "p", "auth-context-source", "Decision context source: " + text(record.context_source) + " · seam: " + text(record.seam));
    });
  }

  function renderCoverage(doc) {
    var body = el("auth-coverage-rows");
    if (!body) return;
    body.replaceChildren();
    var coverage = Array.isArray(doc.coverage) ? doc.coverage : [];
    var summary = doc.coverage_summary || {};
    el("auth-coverage-summary").textContent = coverage.length
      ? text(summary.coverage_failures, "0") + " coverage failures across " + text(summary.critical, "0") + " critical seams. Unknown or untraced critical seams fail coverage."
      : "Coverage unavailable — treated as a failure. Unknown or untraced critical seams fail coverage.";
    if (!coverage.length) {
      var emptyRow = document.createElement("tr");
      var emptyCell = append(emptyRow, "td", "auth-coverage-empty", "Coverage contract absent or corrupt — coverage failure.");
      emptyCell.colSpan = 4;
      body.appendChild(emptyRow);
      return;
    }
    coverage.forEach(function (path) {
      var row = document.createElement("tr");
      if (path.coverage_failure) row.className = "coverage-failure";
      var identity = append(row, "td", "", "");
      append(identity, "strong", "", text(path.id));
      append(identity, "span", "auth-coverage-meta", text(path.kind) + " · " + text(path.owner));
      var state = append(row, "td", "", text(path.state));
      if (path.coverage_failure) append(state, "span", "auth-coverage-failure", "COVERAGE FAILURE");
      append(row, "td", "", path.traced ? "traced · " + text(path.trace_action) : "UNTRACED");
      var gap = append(row, "td", "", text(path.known_bypass, "none declared"));
      if (path.negative_fixture) append(gap, "span", "auth-coverage-meta", "negative fixture: " + path.negative_fixture);
      if (path.failure_reason) append(gap, "span", "auth-coverage-reason", path.failure_reason);
      body.appendChild(row);
    });
  }

  function render(doc) {
    doc = doc || {};
    el("auth-domains-mode").textContent = "SHADOW · NOT ENFORCING";
    var generation = doc.generation || {};
    var replay = doc.replay || {};
    var audit = doc.audit_wal || {};
    var lifecycle = doc.lifecycle || {};
    var contract = doc.contract || {};

    setState("auth-generation-state", generation.state, generation.failure);
    facts("auth-generation-provenance", [
      ["generation", generation.generation], ["digest", shortDigest(generation.digest)],
      ["created", generation.created_at], ["source", text(generation.source) + " · " + shortDigest(generation.source_sha256)]
    ]);
    setState("auth-replay-state", replay.state, replay.failure);
    facts("auth-replay-provenance", [
      ["schema", replay.schema], ["lifecycle digest", shortDigest(replay.lifecycle_contract_sha256)],
      ["records / probes", (replay.records || []).length + " / " + text(replay.probe_count, "0")], ["source", text(replay.source) + " · " + shortDigest(replay.source_sha256)]
    ]);
    setState("auth-audit-state", audit.health, audit.failure);
    facts("auth-audit-provenance", [
      ["checked", audit.checked_at], ["policy generation", audit.policy_generation],
      ["records / last sequence", text(audit.records, "0") + " / " + text(audit.last_sequence, "0")],
      ["last hash", shortDigest(audit.last_hash)], ["source", text(audit.source) + " · " + shortDigest(audit.source_sha256)]
    ]);
    setState("auth-lifecycle-state", lifecycle.effective_claim, lifecycle.failure);
    facts("auth-lifecycle-provenance", [
      ["claimed", lifecycle.claimed_isolation], ["effective", lifecycle.effective_claim],
      ["active invalidators", (lifecycle.invalidators || []).join(", ") || "none reported"],
      ["claim invalidator registry", (contract.claim_invalidators || []).join(", ") || "unknown"],
      ["probes", text(lifecycle.probes_passed, "0") + "/" + text(lifecycle.probes_total, "0") + " passed · " + text(lifecycle.probes_traced, "0") + " traced"],
      ["runtime generation", lifecycle.runtime_generation], ["spec digest", shortDigest(lifecycle.spec_digest)],
      ["source", text(lifecycle.source) + " · " + shortDigest(lifecycle.source_sha256)]
    ]);

    renderContexts(Array.isArray(replay.records) ? replay.records : []);
    renderCoverage(doc);
    el("auth-contract-provenance").textContent = "Contract " + text(contract.revision) + " · " +
      "registry " + ((contract.registry_valid && (contract.actions || []).join(",") === "read") ? "read" : "invalid or unavailable") +
      " · 38-probe registry " + text(contract.lifecycle_probe_count) + " · sources " +
      [
        [contract.source, contract.coverage_sha256], [contract.registry_source, contract.registry_sha256],
        [contract.lifecycle_source, contract.lifecycle_registry_sha256]
      ].map(function (source) { return text(source[0]) + "@" + shortDigest(source[1]); }).join(", ");

    var errors = Array.isArray(doc.errors) ? doc.errors : [];
    var errorBox = el("auth-domains-error");
    errorBox.hidden = errors.length === 0;
    errorBox.textContent = errors.length ? "Shadow evidence incomplete: " + errors.join(" · ") + ". NOT ENFORCING." : "";
  }

  function load() {
    if (loading) return loading;
    loading = fetch("/api/auth-domains/status", { headers: { "Accept": "application/json" } })
      .then(function (response) {
        if (!response.ok) throw new Error("HTTP " + response.status);
        return response.json();
      })
      .then(function (doc) { loaded = true; render(doc); })
      .catch(function (err) {
        loaded = false;
        render({ errors: ["status API unavailable (" + err.message + ")"] });
      })
      .then(function () { loading = null; });
    return loading;
  }

  window.flotillaAuthDomains = {
    show: load,
    refresh: load,
    render: render
  };
})();
