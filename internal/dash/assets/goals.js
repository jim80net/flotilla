/* flotilla dash — Goals view (#267/#268).
 *
 * The fleet's PURPOSE hierarchy: goal nodes in altitude columns (fleet → project
 * → task), each carrying live work-item chips whose status binds to the SAME
 * fleet board the Conversations view reads. Read-only. Structure comes from
 * /api/goals (compiled from fleet-goals.json); each node's `status_display` is
 * the ratified computed roll-up (blocked/awaiting/in-flight/achieved/active/
 * paused/cancelled). The visual token below is derived from it — `achieved`
 * renders as "realized" green, and an empty `active` node renders ghosted
 * ("aspirational"), a rendering refinement over the contract, not a new state.
 *
 * Rendering is layout-then-connect: the browser lays the cards out in flex
 * columns, then edges are drawn from real DOM rects into an SVG overlay — so the
 * DAG connectors are always correct, with no hand-rolled coordinate engine.
 */
(function () {
  "use strict";

  var D = window.flotillaDash;
  var el = D.el, escapeHtml = D.escapeHtml, getJSON = D.getJSON;

  var cache = null;      // last /api/goals document
  var activated = false; // becomes true once the operator opens the Goals tab (lazy fetch)
  var epoch = 0;         // fetch-ordering guard: a stale in-flight refresh never clobbers a newer one

  function isVisible() {
    var v = el("view-goals");
    return v && !v.classList.contains("hidden");
  }

  /* ── column altitude labels ──────────────────────────────────────────── */
  function columnLabel(depth) {
    if (depth === 0) return "Fleet goals";
    if (depth === 1) return "Workstreams";
    if (depth === 2) return "Tasks & desks";
    return "Sub-goals";
  }

  // visToken maps the ratified status_display onto a CSS state token. `achieved`
  // renders as the green "realized" look; an `active` node with no work renders
  // ghosted ("aspirational"). Everything else is the status_display value itself.
  function visToken(n) {
    var sd = n.status_display;
    if (sd === "achieved") return "realized";
    if (sd === "active" && !(n.work_items && n.work_items.length) && !(n.children && n.children.length)) {
      return "aspirational";
    }
    return sd;
  }

  var STATE_LABEL = {
    realized: "realized", "in-flight": "in flight", awaiting: "awaiting you",
    blocked: "blocked", active: "active", aspirational: "aspirational",
    paused: "paused", cancelled: "cancelled",
  };

  /* ── situation strip ─────────────────────────────────────────────────── */
  function renderSituation(doc) {
    var c = doc.counts || {};
    var tiles = [
      { k: "Fleet goals", v: c.fleet || 0, tone: "goal", d: (c.total || 0) + " nodes total" },
      { k: "In flight", v: c.in_flight || 0, tone: "inflight", d: "desks working now" },
      { k: "Awaiting you", v: c.awaiting || 0, tone: "awaiting", d: "your decisions & blocks" },
      { k: "Realized", v: c.realized || 0, tone: "realized", d: "done & solidified" },
      { k: "Aspirational", v: c.aspirational || 0, tone: "aspirational", d: "planned / not yet done" },
    ];
    el("goals-situation").innerHTML = tiles.map(function (t) {
      return '<div class="gtile gtile-' + t.tone + '">' +
        '<div class="gtile-k">' + escapeHtml(t.k) + "</div>" +
        '<div class="gtile-v">' + escapeHtml(String(t.v)) + "</div>" +
        '<div class="gtile-d">' + escapeHtml(t.d) + "</div>" +
        "</div>";
    }).join("");
  }

  function renderLegend() {
    var items = [
      ["realized", "realized"], ["in-flight", "in flight"],
      ["awaiting", "awaiting you"], ["aspirational", "aspirational"],
    ];
    el("goals-legend").innerHTML = items.map(function (i) {
      return '<span class="glegend"><span class="gdot gdot-' + i[0] + '"></span>' + escapeHtml(i[1]) + "</span>";
    }).join("");
  }

  /* ── work-item chip ──────────────────────────────────────────────────── */
  function motif(cls) {
    if (cls === "in-flight") return '<span class="gmotif gmotif-build"><i></i><i></i><i></i></span>';
    if (cls === "done") return '<span class="gmotif gmotif-done">✓</span>';
    if (cls === "awaiting") return '<span class="gmotif gmotif-wait">○</span>';
    if (cls === "blocked") return '<span class="gmotif gmotif-blocked">!</span>';
    return '<span class="gmotif gmotif-dot"></span>';
  }

  function workItem(wi) {
    var kind = escapeHtml(wi.kind || "");
    var label = escapeHtml(wi.label || wi.kind || "");
    var detail = wi.detail ? '<span class="gwi-detail">' + escapeHtml(wi.detail) + "</span>" : "";
    return '<span class="gwi gwi-' + escapeHtml(wi.class || "unknown") + '" title="' + kind + " · " + escapeHtml(wi.detail || "") + '">' +
      motif(wi.class) +
      '<span class="gwi-kind">' + kind + "</span>" +
      '<span class="gwi-label">' + label + "</span>" +
      detail +
      "</span>";
  }

  /* ── node card ───────────────────────────────────────────────────────── */
  function nodeCard(n) {
    var vis = visToken(n);
    var owner = n.owner ? '<span class="gnode-owner">led by ' + escapeHtml(n.owner) + "</span>" : "";
    var desc = n.description ? '<p class="gnode-desc">' + escapeHtml(n.description) + "</p>" : "";
    var items = (n.work_items || []).map(workItem).join("");
    var itemsBlock = items ? '<div class="gnode-items">' + items + "</div>" : "";
    var pill = '<span class="gpill gpill-' + escapeHtml(vis) + '">' + escapeHtml(STATE_LABEL[vis] || vis) + "</span>";
    return '<article class="gnode gnode-' + escapeHtml(n.scope) + " state-" + escapeHtml(vis) + '" ' +
      'data-id="' + escapeHtml(n.id) + '" data-parent="' + escapeHtml(n.parent || "") + '" role="treeitem" tabindex="0">' +
      '<div class="gnode-eyebrow">' + escapeHtml(n.scope) + owner + "</div>" +
      '<div class="gnode-title">' + escapeHtml(n.title) + "</div>" +
      desc +
      '<div class="gnode-foot">' + pill + "</div>" +
      itemsBlock +
      "</article>";
  }

  /* ── main render ─────────────────────────────────────────────────────── */
  function render() {
    var doc = cache || {};
    var graph = el("goals-graph"), cols = el("goals-columns"), empty = el("goals-empty");
    renderSituation(doc);
    renderLegend();

    if (!doc.found) {
      graph.classList.add("hidden");
      empty.classList.remove("hidden");
      empty.textContent = doc.error
        ? ("Goals file could not be loaded: " + doc.error)
        : (doc.message || "No goals file configured.");
      return;
    }
    graph.classList.remove("hidden");
    empty.classList.add("hidden");

    var goals = Array.isArray(doc.goals) ? doc.goals : [];
    var maxDepth = 0;
    goals.forEach(function (n) { if (n.depth > maxDepth) maxDepth = n.depth; });

    var columns = [];
    for (var d = 0; d <= maxDepth; d++) columns.push([]);
    goals.forEach(function (n) { columns[n.depth].push(n); });

    cols.innerHTML = columns.map(function (nodes, depth) {
      var cards = nodes.map(nodeCard).join("");
      return '<div class="gcol" data-depth="' + depth + '">' +
        '<div class="gcol-head">' + escapeHtml(columnLabel(depth)) + "</div>" +
        '<div class="gcol-cards">' + cards + "</div>" +
        "</div>";
    }).join("");

    // Edges are drawn after the browser has laid the cards out.
    requestAnimationFrame(drawEdges);
  }

  /* ── edges: parent → child, from real DOM rects ──────────────────────── */
  function drawEdges() {
    var cols = el("goals-columns"), svg = el("goals-edges");
    if (!cols || !svg || !isVisible()) return;
    var w = cols.scrollWidth, h = cols.scrollHeight;
    svg.setAttribute("width", w);
    svg.setAttribute("height", h);
    svg.setAttribute("viewBox", "0 0 " + w + " " + h);

    var cRect = cols.getBoundingClientRect();
    function pt(node, right) {
      var r = node.getBoundingClientRect();
      return {
        x: (right ? r.right : r.left) - cRect.left + cols.scrollLeft,
        y: r.top + r.height / 2 - cRect.top + cols.scrollTop,
      };
    }
    var cards = cols.querySelectorAll(".gnode");
    var byId = {};
    cards.forEach(function (c) { byId[c.getAttribute("data-id")] = c; });

    var paths = [];
    cards.forEach(function (child) {
      var pid = child.getAttribute("data-parent");
      if (!pid || !byId[pid]) return;
      var a = pt(byId[pid], true), b = pt(child, false);
      var dx = Math.max(24, (b.x - a.x) * 0.5);
      var state = (child.className.match(/state-([a-z-]+)/) || [])[1] || "active";
      paths.push('<path class="gedge gedge-' + state + '" d="M ' + a.x + " " + a.y +
        " C " + (a.x + dx) + " " + a.y + ", " + (b.x - dx) + " " + b.y + ", " + b.x + " " + b.y + '"/>');
    });
    svg.innerHTML = paths.join("");
  }

  /* ── lifecycle ───────────────────────────────────────────────────────── */
  function refresh() {
    if (!activated) return Promise.resolve();
    var e = ++epoch;
    return getJSON("/api/goals").then(function (doc) {
      if (e !== epoch) return; // a newer refresh already superseded this one
      cache = doc;
      if (isVisible()) render();
    }).catch(function (err) {
      if (e !== epoch) return;
      cache = { found: false, error: err.message };
      if (isVisible()) render();
    });
  }

  function show() {
    activated = true;
    if (cache) { render(); } else { refresh(); }
  }

  // Redraw edges on resize AND on horizontal scroll of the columns (the edge
  // overlay is inside the scroller, but a debounced redraw keeps the connectors
  // crisp through momentum scrolling / zoom). Both listeners are wired once.
  var redrawTimer = null;
  function scheduleRedraw() {
    if (!isVisible()) return;
    clearTimeout(redrawTimer);
    redrawTimer = setTimeout(drawEdges, 100);
  }
  window.addEventListener("resize", scheduleRedraw);
  (function wireScroll() {
    var cols = el("goals-columns");
    if (cols) cols.addEventListener("scroll", scheduleRedraw, { passive: true });
  })();

  window.flotillaGoals = { show: show, refresh: refresh };
})();
