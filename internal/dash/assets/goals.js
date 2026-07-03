/* flotilla dash — Goals view (#267 / #280 UI): the Fleet Situation Map.
 *
 * The fleet's PURPOSE hierarchy rendered as the operator-approved 2D canvas:
 * goal nodes in altitude columns (fleet → project → task), pannable and
 * zoomable, each carrying live work-item chips whose status binds to the SAME
 * fleet board the Conversations view reads. Read-only. Structure + the ratified
 * computed roll-up (`status_display`) come from /api/goals; the visual token is
 * derived from it (`achieved` → "realized" green; an empty `active` node →
 * "aspirational" ghost — a rendering refinement over the contract, not a state).
 *
 * INCREMENT 1 (this file) ports the prototype's spatial engine — an absolute
 * tiered layout inside a transform-driven pan/zoom world, with SVG edges drawn
 * from the laid-out card geometry (not live DOM rects, so they are correct under
 * any zoom). The card MARKUP + CSS are reused from the merged view, so work-item
 * detail stays visible (no regression); the node-detail drawer, hover
 * chain-highlight, event pulses, dependency lines and conversation deep-links
 * are later increments layered on this canvas.
 */
(function () {
  "use strict";

  var D = window.flotillaDash;
  var el = D.el, escapeHtml = D.escapeHtml, getJSON = D.getJSON;

  var cache = null;      // last /api/goals document
  var activated = false; // becomes true once the operator opens the Goals tab (lazy fetch)
  var epoch = 0;         // fetch-ordering guard: a stale in-flight refresh never clobbers a newer one

  var nodeById = {};     // id → RenderedGoal (with laid-out x/y/w/h attached)
  var view = { scale: 1, tx: 0, ty: 0, worldW: 0, worldH: 0, fitted: false };
  var panWired = false;

  /* ── tier geometry (ported from the prototype: TIER_X / widths) ────────── */
  var TIER_X = [40, 470, 900];       // fleet | project | task column origins
  var CARD_W = { 0: 320, 1: 270, 2: 290 };
  var GAP = 18, TOP = 46, PAD = 30;  // vertical gap between leaves, top inset, viewport padding

  function q(id) { return document.getElementById(id); }
  function isVisible() { var v = q("view-goals"); return v && !v.classList.contains("hidden"); }

  // tierOf maps scope → column index (0 fleet, 1 project, 2 task), falling back
  // to the node's depth for a tree that is deeper/shallower than the 3 canonical
  // tiers so an unusual file still lays out.
  function tierOf(n) {
    if (n.scope === "fleet") return 0;
    if (n.scope === "project") return 1;
    if (n.scope === "task") return 2;
    return Math.min(2, n.depth || 0);
  }

  // visToken maps the ratified status_display onto a CSS state token (identical
  // to the merged view's mapping so the card styling is unchanged).
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

  /* ── situation strip + legend (unchanged from the merged view) ─────────── */
  function renderSituation(doc) {
    var c = doc.counts || {};
    var tiles = [
      { k: "Fleet goals", v: c.fleet || 0, tone: "goal", d: (c.total || 0) + " nodes total" },
      { k: "In flight", v: c.in_flight || 0, tone: "inflight", d: "desks working now" },
      { k: "Awaiting you", v: c.awaiting || 0, tone: "awaiting", d: "your decisions & blocks" },
      { k: "Realized", v: c.realized || 0, tone: "realized", d: "done & solidified" },
      { k: "Aspirational", v: c.aspirational || 0, tone: "aspirational", d: "planned / not yet done" },
    ];
    q("goals-situation").innerHTML = tiles.map(function (t) {
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
    q("goals-legend").innerHTML = items.map(function (i) {
      return '<span class="glegend"><span class="gdot gdot-' + i[0] + '"></span>' + escapeHtml(i[1]) + "</span>";
    }).join("");
  }

  /* ── work-item chip + node card (markup reused from the merged view) ────── */
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

  function nodeCard(n) {
    var vis = visToken(n);
    var owner = n.owner ? '<span class="gnode-owner">led by ' + escapeHtml(n.owner) + "</span>" : "";
    var desc = n.description ? '<p class="gnode-desc">' + escapeHtml(n.description) + "</p>" : "";
    var items = (n.work_items || []).map(workItem).join("");
    var itemsBlock = items ? '<div class="gnode-items">' + items + "</div>" : "";
    var pill = '<span class="gpill gpill-' + escapeHtml(vis) + '">' + escapeHtml(STATE_LABEL[vis] || vis) + "</span>";
    return '<article class="gnode gnode-' + escapeHtml(n.scope) + " state-" + escapeHtml(vis) + '" ' +
      'data-id="' + escapeHtml(n.id) + '" data-parent="' + escapeHtml(n.parent || "") + '" ' +
      'style="left:' + n._x + "px;top:" + n._y + "px;width:" + n._w + 'px" ' +
      'role="treeitem" tabindex="0">' +
      '<div class="gnode-eyebrow">' + escapeHtml(n.scope) + owner + "</div>" +
      '<div class="gnode-title">' + escapeHtml(n.title) + "</div>" +
      desc +
      '<div class="gnode-foot">' + pill + "</div>" +
      itemsBlock +
      "</article>";
  }

  /* ── layout: absolute tiered, bottom-up y-centering (ported) ───────────── */
  // Two-pass: pass 1 inserts cards at their tier x with a provisional y so the
  // browser can measure real heights (titles wrap, work-item lists vary); pass 2
  // assigns final y bottom-up (leaves stacked; a parent centered on its children)
  // and draws the edges from the measured geometry.
  function buildNodeIndex(goals) {
    nodeById = {};
    goals.forEach(function (n) { n._x = TIER_X[tierOf(n)]; n._w = CARD_W[tierOf(n)]; nodeById[n.id] = n; });
  }

  function layoutY(roots) {
    var cursor = TOP;
    function place(n) {
      var kids = (n.children || []).map(function (id) { return nodeById[id]; }).filter(Boolean);
      if (!kids.length) {
        n._y = cursor;
        cursor += (n._h || 60) + GAP;
        return;
      }
      kids.forEach(place);
      var top = Infinity, bot = -Infinity;
      kids.forEach(function (k) { top = Math.min(top, k._y); bot = Math.max(bot, k._y + (k._h || 60)); });
      n._y = (top + bot) / 2 - (n._h || 60) / 2;
    }
    roots.forEach(function (r) { place(r); cursor += GAP; });
    var maxBot = TOP;
    Object.keys(nodeById).forEach(function (id) {
      var n = nodeById[id];
      maxBot = Math.max(maxBot, n._y + (n._h || 60));
    });
    view.worldW = TIER_X[2] + CARD_W[2] + 40;
    view.worldH = maxBot + 30;
  }

  /* ── edges: parent → child, from laid-out geometry ─────────────────────── */
  function drawEdges() {
    var svg = q("goals-edges");
    if (!svg) return;
    svg.setAttribute("width", view.worldW);
    svg.setAttribute("height", view.worldH);
    svg.setAttribute("viewBox", "0 0 " + view.worldW + " " + view.worldH);
    var paths = [];
    Object.keys(nodeById).forEach(function (id) {
      var child = nodeById[id];
      var parent = child.parent ? nodeById[child.parent] : null;
      if (!parent) return;
      var a = { x: parent._x + parent._w, y: parent._y + (parent._h || 60) / 2 };
      var b = { x: child._x, y: child._y + (child._h || 60) / 2 };
      var dx = Math.max(40, (b.x - a.x) * 0.5);
      var state = visToken(child);
      paths.push('<path class="gedge gedge-' + state + '" data-child="' + escapeHtml(id) +
        '" d="M ' + a.x + " " + a.y + " C " + (a.x + dx) + " " + a.y + ", " +
        (b.x - dx) + " " + b.y + ", " + b.x + " " + b.y + '"/>');
    });
    svg.innerHTML = paths.join("");
  }

  /* ── tier column headers ───────────────────────────────────────────────── */
  function renderTierLabels() {
    var labels = ["Fleet goals", "Workstreams", "Tasks & desks"];
    q("goals-tierlabels").innerHTML = labels.map(function (lbl, i) {
      return '<div class="gtier-label" style="left:' + TIER_X[i] + "px;width:" + CARD_W[i] + 'px">' +
        escapeHtml(lbl) + "</div>";
    }).join("");
  }

  /* ── main render (two-pass measure) ────────────────────────────────────── */
  function render() {
    var doc = cache || {};
    var graph = q("goals-graph"), empty = q("goals-empty");
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
    buildNodeIndex(goals);
    var roots = goals.filter(function (n) { return !n.parent || !nodeById[n.parent]; });

    // Pass 1: render at tier x with provisional y=0 so heights can be measured.
    goals.forEach(function (n) { n._y = 0; });
    var nodesEl = q("goals-nodes");
    nodesEl.innerHTML = goals.map(nodeCard).join("");
    renderTierLabels();

    // Measure, then pass 2 (final y + edges) after layout is flushed.
    requestAnimationFrame(function () {
      if (!isVisible()) return; // measurement needs the view on-screen; show() re-runs it
      goals.forEach(function (n) {
        var card = nodesEl.querySelector('[data-id="' + cssEscape(n.id) + '"]');
        n._h = card ? card.offsetHeight : 60;
      });
      layoutY(roots);
      goals.forEach(function (n) {
        var card = nodesEl.querySelector('[data-id="' + cssEscape(n.id) + '"]');
        if (card) card.style.top = n._y + "px";
      });
      var world = q("goals-world");
      world.style.width = view.worldW + "px";
      world.style.height = view.worldH + "px";
      drawEdges();
      if (!view.fitted) { fit(); view.fitted = true; }
      applyTransform();
    });
  }

  // cssEscape guards the attribute selector against ids with quotes/specials.
  function cssEscape(s) { return String(s).replace(/["\\]/g, "\\$&"); }

  /* ── pan / zoom (ported) ───────────────────────────────────────────────── */
  function applyTransform() {
    var world = q("goals-world");
    if (world) world.style.transform = "translate(" + view.tx + "px," + view.ty + "px) scale(" + view.scale + ")";
  }

  // fit: scale to width (never upscale past 1), anchor top so the task-column
  // desks are legible on load; the operator pans/zooms for the rest.
  function fit() {
    var vp = q("goals-viewport");
    if (!vp) return;
    view.scale = Math.min(1, (vp.clientWidth - PAD * 2) / view.worldW);
    view.tx = Math.max(PAD, (vp.clientWidth - view.worldW * view.scale) / 2);
    view.ty = (view.worldH * view.scale < vp.clientHeight - PAD * 2)
      ? (vp.clientHeight - view.worldH * view.scale) / 2 : PAD;
  }

  // fitOverview: zoom out so the whole DAG is on screen at once.
  function fitOverview() {
    var vp = q("goals-viewport");
    if (!vp) return;
    var sx = (vp.clientWidth - PAD * 2) / view.worldW, sy = (vp.clientHeight - PAD * 2) / view.worldH;
    view.scale = Math.min(sx, sy, 1);
    view.tx = (vp.clientWidth - view.worldW * view.scale) / 2;
    view.ty = (vp.clientHeight - view.worldH * view.scale) / 2;
    applyTransform();
  }

  function setupPanZoom() {
    if (panWired) return;
    var vp = q("goals-viewport");
    if (!vp) return;
    panWired = true;

    vp.addEventListener("wheel", function (e) {
      e.preventDefault();
      var r = vp.getBoundingClientRect(), mx = e.clientX - r.left, my = e.clientY - r.top;
      var f = e.deltaY < 0 ? 1.12 : 0.89;
      var ns = Math.min(2.2, Math.max(0.25, view.scale * f));
      view.tx = mx - (mx - view.tx) * (ns / view.scale);
      view.ty = my - (my - view.ty) * (ns / view.scale);
      view.scale = ns;
      applyTransform();
    }, { passive: false });

    var drag = false, sx = 0, sy = 0;
    vp.addEventListener("pointerdown", function (e) {
      if (e.target.closest(".gnode") || e.target.closest(".gzoomctl")) return;
      drag = true; sx = e.clientX - view.tx; sy = e.clientY - view.ty;
      vp.classList.add("grabbing"); vp.setPointerCapture(e.pointerId);
    });
    vp.addEventListener("pointermove", function (e) {
      if (!drag) return;
      view.tx = e.clientX - sx; view.ty = e.clientY - sy; applyTransform();
    });
    vp.addEventListener("pointerup", function () { drag = false; vp.classList.remove("grabbing"); });

    q("goals-zin").onclick = function () { view.scale = Math.min(2.2, view.scale * 1.18); applyTransform(); };
    q("goals-zout").onclick = function () { view.scale = Math.max(0.25, view.scale * 0.85); applyTransform(); };
    q("goals-zfit").onclick = fitOverview;
  }

  /* ── lifecycle ─────────────────────────────────────────────────────────── */
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
    setupPanZoom();
    if (cache) { render(); } else { refresh(); }
  }

  // Re-fit on resize (keeps the map framed); the transform is otherwise the
  // operator's to drive via pan/zoom.
  var resizeTimer = null;
  window.addEventListener("resize", function () {
    if (!isVisible()) return;
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(function () { fit(); applyTransform(); }, 120);
  });

  window.flotillaGoals = { show: show, refresh: refresh };
})();
