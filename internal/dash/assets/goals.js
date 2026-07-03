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
  var escapeHtml = D.escapeHtml, getJSON = D.getJSON;

  var cache = null;      // last /api/goals document
  var lastSig = null;    // stringified last-rendered doc — skip a no-op re-render
  var activated = false; // becomes true once the operator opens the Goals tab (lazy fetch)
  var epoch = 0;         // fetch + deferred-layout ordering guard

  var nodeById = {};     // id → RenderedGoal (with laid-out _x/_y/_w/_h attached)
  var view = { scale: 1, tx: 0, ty: 0, worldW: 0, worldH: 0, fitted: false };
  var panWired = false;

  /* ── tier geometry (ported from the prototype: TIER_X=[40,470,900]) ─────── */
  // Columns are derived from depth so a tree deeper than the canonical 3 tiers
  // still lays out one column per level (depth 0/1/2 reproduce the prototype's
  // 40/470/900 exactly) instead of collapsing deep nodes into one overlapping
  // column.
  var COL_STEP = 430, COL_X0 = 40, DEFAULT_H = 60, GAP = 18, TOP = 46, PAD = 30;
  function colX(depth) { return COL_X0 + depth * COL_STEP; }
  function colW(depth) { return depth === 0 ? 320 : depth === 1 ? 270 : 290; }
  function depthOf(n) { return n.depth || 0; }
  function heightOf(n) { return n._h || DEFAULT_H; }

  function q(id) { return document.getElementById(id); }
  function isVisible() { var v = q("view-goals"); return v && !v.classList.contains("hidden"); }

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
  // Two-pass: pass 1 inserts cards at their column x with a provisional y so the
  // browser can measure real heights (titles wrap, work-item lists vary); pass 2
  // assigns final y bottom-up and draws the edges from the measured geometry.
  function buildNodeIndex(goals) {
    nodeById = {};
    goals.forEach(function (n) {
      var d = depthOf(n);
      n._x = colX(d); n._w = colW(d);
      nodeById[n.id] = n;
    });
  }

  // layoutY places leaves stacked and centers each parent on its children's
  // span. A parent card taller than that span is NEVER allowed to rise above its
  // subtree's top (which would collide with the previous sibling) and always
  // pushes the cursor past its own bottom (so the next sibling clears it) — so a
  // tall mid-node can't overlap a sibling subtree. Depth-derived columns mean a
  // parent is always in a shallower column than its children, so parent↔child
  // never share an x.
  function layoutY(roots) {
    var cursor = TOP, maxBot = TOP;
    function place(n) {
      var kids = (n.children || []).map(function (id) { return nodeById[id]; }).filter(Boolean);
      var h = heightOf(n);
      if (!kids.length) {
        n._y = cursor;
      } else {
        var bandTop = cursor;
        kids.forEach(place);
        var top = Infinity, bot = -Infinity;
        kids.forEach(function (k) { top = Math.min(top, k._y); bot = Math.max(bot, k._y + heightOf(k)); });
        n._y = (top + bot) / 2 - h / 2;
        if (n._y < bandTop) n._y = bandTop; // never rise into the previous sibling
      }
      var bottom = n._y + h;
      if (bottom + GAP > cursor) cursor = bottom + GAP; // next sibling clears this card
      maxBot = Math.max(maxBot, bottom);
    }
    roots.forEach(function (r) { place(r); cursor += GAP; });

    var maxDepth = 0;
    Object.keys(nodeById).forEach(function (id) { maxDepth = Math.max(maxDepth, depthOf(nodeById[id])); });
    view.worldW = colX(maxDepth) + colW(maxDepth) + 40;
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
      var a = { x: parent._x + parent._w, y: parent._y + heightOf(parent) / 2 };
      var b = { x: child._x, y: child._y + heightOf(child) / 2 };
      var dx = Math.max(40, (b.x - a.x) * 0.5);
      var state = escapeHtml(visToken(child)); // bounded enum, escaped for consistency + defense-in-depth
      paths.push('<path class="gedge gedge-' + state + '" data-child="' + escapeHtml(id) +
        '" d="M ' + a.x + " " + a.y + " C " + (a.x + dx) + " " + a.y + ", " +
        (b.x - dx) + " " + b.y + ", " + b.x + " " + b.y + '"/>');
    });
    svg.innerHTML = paths.join("");
  }

  /* ── tier column headers (one per depth present) ───────────────────────── */
  function tierLabel(depth) {
    if (depth === 0) return "Fleet goals";
    if (depth === 1) return "Workstreams";
    if (depth === 2) return "Active tasks · desks";
    return "Sub-goals";
  }
  function renderTierLabels(maxDepth) {
    var out = [];
    for (var d = 0; d <= maxDepth; d++) {
      out.push('<div class="gtier-label" style="left:' + colX(d) + "px;width:" + colW(d) + 'px">' +
        escapeHtml(tierLabel(d)) + "</div>");
    }
    q("goals-tierlabels").innerHTML = out.join("");
  }

  /* ── main render (two-pass measure) ────────────────────────────────────── */
  // render(sig): sig is the JSON signature of the doc being rendered; it is committed
  // to lastSig ONLY at each point the render actually completes (the synchronous
  // empty/error paths, or the end of the deferred pass-2) — never before. sig is
  // recomputed from cache when omitted (the show()/error paths).
  function render(sig) {
    var doc = cache || {};
    if (sig === undefined) sig = JSON.stringify(doc);
    var graph = q("goals-graph"), empty = q("goals-empty");
    renderSituation(doc);
    renderLegend();

    if (!doc.found) {
      graph.classList.add("hidden");
      empty.classList.remove("hidden");
      empty.textContent = doc.error
        ? ("Goals file could not be loaded: " + doc.error)
        : (doc.message || "No goals file configured.");
      lastSig = sig; // a complete (synchronous) render
      return;
    }
    var goals = Array.isArray(doc.goals) ? doc.goals : [];
    if (!goals.length) {
      graph.classList.add("hidden");
      empty.classList.remove("hidden");
      empty.textContent = "No goals defined yet.";
      lastSig = sig; // a complete (synchronous) render
      return;
    }
    graph.classList.remove("hidden");
    empty.classList.add("hidden");

    buildNodeIndex(goals);
    var roots = goals.filter(function (n) { return !n.parent || !nodeById[n.parent]; });
    var maxDepth = 0;
    goals.forEach(function (n) { maxDepth = Math.max(maxDepth, depthOf(n)); });

    // Pass 1: render at column x with provisional y=0 so heights can be measured.
    goals.forEach(function (n) { n._y = 0; });
    var nodesEl = q("goals-nodes");
    nodesEl.innerHTML = goals.map(nodeCard).join("");
    renderTierLabels(maxDepth);

    // Measure + final layout after the browser flushes layout. Guarded so a newer
    // render (a refresh that landed between here and the frame) wins.
    var myEpoch = epoch;
    requestAnimationFrame(function () {
      // Aborted — superseded by a newer refresh, or the tab went hidden (rAF is
      // suspended in a backgrounded tab while dash.js keeps calling refresh()). Do
      // NOT commit lastSig: the canvas is still at its provisional pass-1 layout, so
      // the next refresh must re-render it rather than dedup-skip a half-finished map.
      // show() re-renders on tab return.
      if (myEpoch !== epoch || !isVisible()) return;
      // Cards render in goals[] order, so children[i] ↔ goals[i] — read heights
      // in one pass (all reads batched before any write) to avoid layout thrash.
      goals.forEach(function (n, i) { n._h = nodesEl.children[i] ? nodesEl.children[i].offsetHeight : DEFAULT_H; });
      layoutY(roots);
      goals.forEach(function (n, i) { if (nodesEl.children[i]) nodesEl.children[i].style.top = n._y + "px"; });
      var world = q("goals-world");
      world.style.width = view.worldW + "px";
      world.style.height = view.worldH + "px";
      drawEdges();
      if (!view.fitted) { fit(); view.fitted = true; }
      applyTransform();
      lastSig = sig; // commit ONLY after a complete pass-2 render
    });
  }

  /* ── pan / zoom (ported) ───────────────────────────────────────────────── */
  function applyTransform() {
    var world = q("goals-world");
    if (world) world.style.transform = "translate(" + view.tx + "px," + view.ty + "px) scale(" + view.scale + ")";
  }

  // fit: scale to width (never upscale past 1), anchor top so the task-column
  // desks are legible on load; the operator pans/zooms for the rest.
  function fit() {
    var vp = q("goals-viewport");
    if (!vp || !view.worldW || !view.worldH) return;
    view.scale = Math.min(1, (vp.clientWidth - PAD * 2) / view.worldW);
    view.tx = Math.max(PAD, (vp.clientWidth - view.worldW * view.scale) / 2);
    view.ty = (view.worldH * view.scale < vp.clientHeight - PAD * 2)
      ? (vp.clientHeight - view.worldH * view.scale) / 2 : PAD;
  }

  // fitOverview: zoom out so the whole DAG is on screen at once.
  function fitOverview() {
    var vp = q("goals-viewport");
    if (!vp || !view.worldW || !view.worldH) return;
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
    function endDrag() { drag = false; vp.classList.remove("grabbing"); }
    vp.addEventListener("pointerdown", function (e) {
      if (e.target.closest(".gnode") || e.target.closest(".gzoomctl")) return;
      drag = true; sx = e.clientX - view.tx; sy = e.clientY - view.ty;
      vp.classList.add("grabbing"); vp.setPointerCapture(e.pointerId);
    });
    vp.addEventListener("pointermove", function (e) {
      if (!drag) return;
      view.tx = e.clientX - sx; view.ty = e.clientY - sy; applyTransform();
    });
    vp.addEventListener("pointerup", endDrag);
    vp.addEventListener("pointercancel", endDrag);
    vp.addEventListener("lostpointercapture", endDrag); // capture yanked without pointerup → don't strand drag

    var zin = q("goals-zin"), zout = q("goals-zout"), zfit = q("goals-zfit");
    if (zin) zin.onclick = function () { view.scale = Math.min(2.2, view.scale * 1.18); applyTransform(); };
    if (zout) zout.onclick = function () { view.scale = Math.max(0.25, view.scale * 0.85); applyTransform(); };
    if (zfit) zfit.onclick = fitOverview;
  }

  /* ── lifecycle ─────────────────────────────────────────────────────────── */
  function refresh() {
    if (!activated) return Promise.resolve();
    var e = ++epoch;
    return getJSON("/api/goals").then(function (doc) {
      if (e !== epoch) return; // a newer refresh already superseded this one
      cache = doc;
      var sig = JSON.stringify(doc);
      if (sig === lastSig) return; // this exact doc is already FULLY rendered — skip
      // Pass the sig to render, which commits lastSig only when the render actually
      // COMPLETES. Committing here (before render) would falsely mark a doc rendered
      // even if render's deferred pass-2 aborts (superseded / tab hidden) — then a
      // later identical refresh would dedup-skip and strand the provisional canvas.
      if (isVisible()) render(sig);
      // Hidden: do not render and do not commit lastSig — show() renders on tab open.
    }).catch(function (err) {
      if (e !== epoch) return;
      cache = { found: false, error: err.message };
      if (isVisible()) render(); // render() computes + commits the error-state sig
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
