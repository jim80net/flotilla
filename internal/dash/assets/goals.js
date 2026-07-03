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
  var lastStructSig = null; // structural signature — decides in-place update vs full re-layout
  var laidOut = false;      // true only between a completed pass-2 and the next full rebuild
  var activated = false; // becomes true once the operator opens the Goals tab (lazy fetch)
  var epoch = 0;         // fetch + deferred-layout ordering guard

  var nodeById = {};     // id → RenderedGoal (with laid-out _x/_y/_w/_h attached)
  var view = { scale: 1, tx: 0, ty: 0, worldW: 0, worldH: 0, fitted: false };
  var panWired = false;
  var edgeIndex = {};    // child id → { path, parent } — for the hover chain-highlight
  var depEdges = [];     // cross-dependency edges (GoalsDoc.edges: {from,to,kind:"depends_on"})
  var collaborations = []; // desk collaboration groups (GoalsDoc.collaborations, #324 Inc 3)
  var selectedId = null; // node whose detail drawer is open (transient state: on the article)
  var hoveredId = null;  // node currently hovered (re-applied after a render)
  var nodesWired = false;
  var kbdNav = false;    // true when focus is moving by keyboard (Tab) — gates focus-recenter
  var modalReturn = null; // element to restore focus to when the intervention modal closes
  // Layout mode (org-graph v2 §2/§7.4). "org" = hub-and-spoke — the coordinator
  // (layout.hub_center) at the visual center, org units on concentric rings, spoke edges
  // to children. "tree" = the tiered altitude columns (the toggle alternative). Default is
  // ORG per the operator's UX blessing (#324), superseding design §7.4's provisional
  // tree-default "until UX proven". A deployment may seed the default via the
  // FLOTILLA_DASH_GOALS_LAYOUT env → the body's data-goals-layout attribute (#317); the
  // live toggle still overrides it at runtime.
  var goalsLayout = (function () {
    var v = (document.body && document.body.getAttribute("data-goals-layout")) || "";
    return v === "tree" ? "tree" : "org";
  })();

  /* ── tier geometry (ported from the prototype: TIER_X=[40,470,900]) ─────── */
  // Columns are derived from depth so a tree deeper than the canonical 3 tiers
  // still lays out one column per level (depth 0/1/2 reproduce the prototype's
  // 40/470/900 exactly) instead of collapsing deep nodes into one overlapping
  // column.
  var COL_STEP = 430, COL_X0 = 40, DEFAULT_H = 60, GAP = 18, TOP = 46, PAD = 30;
  function colX(depth) { return COL_X0 + depth * COL_STEP; }
  function colW(depth) { return depth === 0 ? 320 : depth === 1 ? 270 : 290; }
  // Card width per layout. The tree uses wide altitude columns; the org (hub-spoke)
  // graph uses narrower cards so the radial map packs tightly and reads as a node
  // graph rather than a sprawl of columns (#324 — tighter, content-aware geometry).
  function nodeW(depth) { return goalsLayout === "org" ? (depth === 0 ? 216 : 176) : colW(depth); }
  function depthOf(n) { return n.depth || 0; }
  function heightOf(n) { return n._h || DEFAULT_H; }

  function q(id) { return document.getElementById(id); }
  function isVisible() { var v = q("view-goals"); return v && !v.classList.contains("hidden"); }

  // visToken maps the ratified status_display onto a CSS state token (identical
  // to the merged view's mapping so the card styling is unchanged).
  function visToken(n) {
    var sd = n.status_display;
    if (sd === "achieved") return "realized";
    // An authored goal that is active-but-empty (no work, no children) reads as
    // aspirational/planned. A roster-materialized DESK (#324 Inc 2), though, is a LIVE
    // entity — its emptiness means "no live work signal right now", NOT "planned" — so
    // it renders as active, never ghosted.
    if (sd === "active" && n.source !== "roster" &&
        !(n.work_items && n.work_items.length) && !(n.children && n.children.length)) {
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
      { k: "Flotillas", v: c.fleet || 0, tone: "goal", d: (c.total || 0) + " nodes total" },
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
    // NOTE: the aria-live announcement is NOT made here — renderSituation runs on
    // every render (incl. error/empty), so announcing the count summary here would
    // alternate with the error/empty announcement each refresh and defeat the dedup.
    // render() announces the summary only on the success path.
  }

  // updateLive announces the fleet-goal situation to a screen reader (an aria-live
  // region), but ONLY when the summary changes — so a live-status tick that moved a
  // goal into "awaiting you" is announced, without chattering on every no-op refresh.
  var lastLive = "";
  function announce(msg) {
    if (msg === lastLive) return;
    lastLive = msg;
    var region = q("goals-live");
    if (region) region.textContent = msg;
  }
  function updateLive(c) {
    // "goal nodes" (not "fleet goals") — total counts all nodes, not just the fleet tier.
    announce((c.awaiting || 0) + " awaiting you, " + (c.in_flight || 0) + " in flight, " +
      (c.realized || 0) + " realized, of " + (c.total || 0) + " goal nodes.");
  }

  function renderLegend() {
    var items = [
      ["realized", "realized"], ["in-flight", "in flight"],
      ["awaiting", "awaiting you"], ["aspirational", "aspirational"], ["dep", "depends on"],
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

  // nodeInner is the live-updatable content of a card (state pill + work-item
  // chips + the text that carries status). It is regenerated on an in-place
  // refresh; the article WRAPPER (identity, geometry, focus, transient classes)
  // is left untouched — see updateInPlace.
  //
  // TWO CONTRACTS for later increments (Inc 2+):
  //  1. Transient UI state (a drawer-selected marker, a hover-chain class, a
  //     pulse) MUST be attached to the ARTICLE element, NOT its inner children —
  //     an in-place refresh replaces the inner html but keeps the article, so only
  //     article-level state survives an SSE tick.
  //  2. Any field rendered here that can change the card's HEIGHT (currently only
  //     title/description wrap and the work-item COUNT) MUST also be in
  //     structuralSig, or an in-place update will leave stale geometry. The
  //     excluded live fields (status_display, a work-item's class/detail) are
  //     colour/text-only and the chip CSS pins each work-item to a fixed
  //     single-line row (.gwi-label / .gwi-detail nowrap), so they can't.
  function nodeInner(n) {
    var vis = visToken(n);
    var owner = n.owner ? '<span class="gnode-owner">led by ' + escapeHtml(n.owner) + "</span>" : "";
    var desc = n.description ? '<p class="gnode-desc">' + escapeHtml(n.description) + "</p>" : "";
    var items = (n.work_items || []).map(workItem).join("");
    var itemsBlock = items ? '<div class="gnode-items">' + items + "</div>" : "";
    var pill = '<span class="gpill gpill-' + escapeHtml(vis) + '">' + escapeHtml(STATE_LABEL[vis] || vis) + "</span>";
    // Per-node controls. #349 A2 SWAP: the node BODY now opens the detail drawer (the
    // primary "see the thing" action); the conversation jump is a distinct labelled
    // "→ desk" button (only on a routable node), synchronized with the drawer's own
    // open-conversation. A ⚠ Respond button on operator-gated nodes opens the
    // waiting-on-you modal. Positioned absolute so they never change card height (#283).
    var gated = vis === "awaiting" || vis === "blocked";
    var routable = !!convAgent(n);
    var controls = '<span class="gnode-ctl">' +
      (gated ? '<button class="gnode-respond" type="button" title="Respond to what this needs" aria-label="Respond">&#9888;</button>' : "") +
      (routable ? '<button class="gnode-godesk" type="button" title="Go to this desk’s conversation" aria-label="Go to desk">&#8594;&#8202;desk</button>' : "") +
      "</span>";
    // org-graph v2 enrichment: priorities (flotilla-level, operator-facing) and
    // milestones (desk-level current work) render as short ordered lists; the harness
    // surface (grok / claude-code / …) renders as a subdued right-aligned badge in the
    // foot (design §3). All are height-affecting → mirrored in structuralSig.
    var prios = nodeList(n.priorities, "gnode-prios", "priorities");
    var miles = nodeList(n.milestones, "gnode-miles", "current work");
    var harness = (n.harness && n.harness.surface)
      ? '<span class="gnode-harness" title="harness surface">' + escapeHtml(n.harness.surface) + "</span>"
      : "";
    return controls +
      '<div class="gnode-eyebrow">' + escapeHtml(scopeNoun(n)) + owner + "</div>" +
      '<div class="gnode-title">' + escapeHtml(n.title) + "</div>" +
      desc + prios + miles +
      '<div class="gnode-foot">' + pill + harness + "</div>" +
      itemsBlock;
  }

  // nodeList renders a short labeled ordered list (priorities / milestones), or "" when
  // the field is absent/empty. Capped at 4 rows with a "+N more" tail so a long list
  // can't blow up the card height (the drawer shows the full list).
  function nodeList(arr, cls, label) {
    var xs = Array.isArray(arr) ? arr : [];
    if (!xs.length) return "";
    var shown = xs.slice(0, 4);
    var more = xs.length > 4 ? '<li class="gnode-list-more">+' + (xs.length - 4) + " more</li>" : "";
    return '<div class="gnode-list ' + cls + '"><span class="gnode-list-lab">' + label + "</span><ul>" +
      shown.map(function (x) { return "<li>" + escapeHtml(x) + "</li>"; }).join("") + more + "</ul></div>";
  }

  function nodeCard(n) {
    var vis = visToken(n);
    return '<article class="gnode gnode-' + escapeHtml(n.scope) + " state-" + escapeHtml(vis) + '" ' +
      'data-id="' + escapeHtml(n.id) + '" data-parent="' + escapeHtml(n.parent || "") + '" ' +
      'style="left:' + n._x + "px;top:" + n._y + "px;width:" + n._w + 'px" ' +
      'role="treeitem" aria-level="' + (depthOf(n) + 1) + '" tabindex="0">' +
      nodeInner(n) +
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
      n._x = colX(d); n._w = nodeW(d);
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

  /* ── org (hub-and-spoke) layout — org-graph v2 §2 ──────────────────────── */
  // The coordinator (the layout.hub_center node) sits at the world center; the fleet
  // orbits on concentric rings (ring = distance-from-hub), each node's children fanned
  // across the parent's angular slice (spoke geometry). With no hub_center hint, the
  // roots themselves form ring 1 around an empty center. Positions are polar → the same
  // absolute _x/_y the tree layout uses, so nodeCard/pan-zoom/keyed-update are unchanged.
  var RING_GAP = 52; // radial breathing room between adjacent rings
  function childrenOf(n) { return (n.children || []).map(function (id) { return nodeById[id]; }).filter(Boolean); }

  // collabIndexOf returns the collaboration group a node belongs to, or -1 (#324 Inc 3).
  function collabIndexOf(id) {
    for (var i = 0; i < collaborations.length; i++) {
      if ((collaborations[i].desks || []).indexOf(id) >= 0) return i;
    }
    return -1;
  }
  // clusterAdjacent stable-sorts a sibling list so desks in the SAME collaboration sit
  // next to each other (contiguous), keeping non-collaborating siblings in place — so the
  // dotted container wraps a tight, adjacent cluster instead of a band across the ring.
  function clusterAdjacent(nodes) {
    if (!collaborations.length) return nodes;
    return nodes
      .map(function (n, i) { return { n: n, i: i, c: collabIndexOf(n.id) }; })
      .sort(function (a, b) {
        var ka = a.c < 0 ? collaborations.length : a.c;
        var kb = b.c < 0 ? collaborations.length : b.c;
        return ka !== kb ? ka - kb : a.i - b.i;
      })
      .map(function (x) { return x.n; });
  }
  function nodeCenter(n) { return { x: n._x + n._w / 2, y: n._y + heightOf(n) / 2 }; }

  // layoutOrg is CONTENT-AWARE (#324): rings are sized from the actual card extents
  // (tight near the hub — no fixed-step waste) and children are angularly PACKED by
  // subtree leaf-weight (a small subtree clusters near its parent's direction instead
  // of spreading over an empty arc). Positions are polar → the same absolute _x/_y the
  // tree layout writes, so nodeCard / pan-zoom / keyed-update / drawer / modal are
  // unchanged. Runs in pass-2, so measured heights (n._h) are available.
  function layoutOrg(goals, roots) {
    // 1. the hub (coordinator) — layout.hub_center; else an empty center + roots on ring 1.
    var center = null;
    for (var i = 0; i < goals.length; i++) {
      if (goals[i].layout && goals[i].layout.hub_center) { center = goals[i]; break; }
    }

    // 2. ring numbers (center = 0). A cycle guard (server validates acyclic) bounds it.
    var ringOf = {}, seen = {};
    function assignRing(n, r) {
      if (seen[n.id]) return;
      seen[n.id] = true;
      ringOf[n.id] = r;
      childrenOf(n).forEach(function (k) { assignRing(k, r + 1); });
    }
    var ring1;
    if (center) {
      ringOf[center.id] = 0; seen[center.id] = true;
      ring1 = childrenOf(center);
      roots.forEach(function (r) { if (r !== center) ring1.push(r); }); // sibling flotillas orbit too
    } else {
      ring1 = roots.slice();
    }
    ring1.forEach(function (n) { assignRing(n, 1); });

    // 3. subtree leaf-weight = angular demand (memoized, cycle-safe).
    var leaves = {};
    function leafCount(n, path) {
      if (leaves[n.id] != null) return leaves[n.id];
      if (path[n.id]) return 1; // cycle → treat as a leaf
      path[n.id] = true;
      var kids = childrenOf(n), c = 0;
      kids.forEach(function (k) { c += leafCount(k, path); });
      path[n.id] = false;
      return (leaves[n.id] = Math.max(1, kids.length ? c : 1));
    }
    goals.forEach(function (n) { leafCount(n, {}); });

    // 4. per-ring max card extents (measured) + node counts.
    var maxRing = 0, maxH = {}, maxW = {}, countRing = {};
    Object.keys(ringOf).forEach(function (id) {
      var r = ringOf[id], n = nodeById[id];
      if (!n) return;
      maxRing = Math.max(maxRing, r);
      maxH[r] = Math.max(maxH[r] || 0, heightOf(n));
      maxW[r] = Math.max(maxW[r] || 0, n._w);
      countRing[r] = (countRing[r] || 0) + 1;
    });

    // 5. content-aware ring radii: accumulate outward, each ring's clearance sized by
    //    its cards' REACH — half the LARGER dimension (width dominates a wide card), so
    //    a card at any angle clears the inner ring without a corner collision (cards are
    //    wider than tall; a height-only gap let them overlap horizontally near the hub).
    //    ALSO honor a circumference minimum so a crowded ring's cards don't overlap
    //    tangentially. Inner rings stay tight — no fixed-step waste.
    function reach(r) { return Math.max(maxW[r] || 200, maxH[r] || DEFAULT_H) / 2; }
    var radius = [0];
    for (var r = 1; r <= maxRing; r++) {
      var accum = radius[r - 1] + reach(r - 1) + RING_GAP + reach(r);
      var circMin = (countRing[r] * ((maxW[r] || 200) + 24)) / (2 * Math.PI);
      radius[r] = Math.max(accum, circMin);
    }

    // 6. world sized to the outermost node extent, centered.
    var outerR = (radius[maxRing] || 0) + reach(maxRing);
    var worldSize = 2 * (outerR + 40);
    view.worldW = worldSize; view.worldH = worldSize;
    var cx = worldSize / 2, cy = worldSize / 2;

    // 7. placement: a node sits at its ring radius + slice midpoint; its children take
    //    sub-slices of the node's slice PROPORTIONAL to their leaf-weight — so a small
    //    subtree packs tightly around the parent's direction, a large one gets the room.
    var pplaced = {};
    function place(n, a0, a1) {
      if (pplaced[n.id]) return;
      pplaced[n.id] = true;
      var mid = (a0 + a1) / 2, rr = ringOf[n.id];
      if (rr === 0) {
        n._x = cx - n._w / 2; n._y = cy - heightOf(n) / 2;
      } else {
        var rad = radius[rr];
        n._x = cx + rad * Math.cos(mid) - n._w / 2;
        n._y = cy + rad * Math.sin(mid) - heightOf(n) / 2;
      }
      var kids = clusterAdjacent(childrenOf(n)); // collaborating siblings sit together
      if (!kids.length) return;
      var total = 0; kids.forEach(function (k) { total += leaves[k.id]; });
      var cursor = a0;
      kids.forEach(function (k) {
        var w = (a1 - a0) * (leaves[k.id] / (total || 1));
        place(k, cursor, cursor + w);
        cursor += w;
      });
    }

    if (center) { center._x = cx - center._w / 2; center._y = cy - heightOf(center) / 2; pplaced[center.id] = true; }
    // ring-1 nodes split the full circle by leaf-weight, starting at the top (-π/2);
    // clustered so collaborating desks are adjacent (their container stays tight).
    var ordered1 = clusterAdjacent(ring1);
    var total1 = 0; ordered1.forEach(function (n) { total1 += leaves[n.id]; });
    var cur = -Math.PI / 2;
    ordered1.forEach(function (n) {
      var w = 2 * Math.PI * (leaves[n.id] / (total1 || 1));
      place(n, cur, cur + w);
      cur += w;
    });
  }

  // collabMarkup draws a dotted container (+ lane label) around each collaboration's desk
  // nodes (#324 Inc 3). Org-mode only — the desks are clustered adjacent there, so the
  // padded bounding box wraps a tight group. Rendered behind the edges + nodes.
  function collabMarkup() {
    if (goalsLayout !== "org" || !collaborations.length) return "";
    var out = [];
    collaborations.forEach(function (cb) {
      var ns = (cb.desks || []).map(function (id) { return nodeById[id]; }).filter(Boolean);
      if (ns.length < 2) return;
      var x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
      ns.forEach(function (n) {
        x0 = Math.min(x0, n._x); y0 = Math.min(y0, n._y);
        x1 = Math.max(x1, n._x + n._w); y1 = Math.max(y1, n._y + heightOf(n));
      });
      var pad = 18;
      x0 -= pad; y0 -= pad; x1 += pad; y1 += pad;
      out.push('<rect class="gcollab" x="' + x0 + '" y="' + y0 + '" width="' + (x1 - x0) + '" height="' + (y1 - y0) + '" rx="16"/>');
      out.push('<text class="gcollab-label" x="' + (x0 + 12) + '" y="' + (y0 + 17) + '">' + escapeHtml(cb.lane || "") + "</text>");
    });
    return out.join("");
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
      var state = escapeHtml(visToken(child)); // bounded enum, escaped for consistency + defense-in-depth
      var d;
      if (goalsLayout === "org") {
        // radial spoke: a straight line from hub/parent center to child center.
        var pc = nodeCenter(parent), cc = nodeCenter(child);
        d = "M " + pc.x + " " + pc.y + " L " + cc.x + " " + cc.y;
      } else {
        var a = { x: parent._x + parent._w, y: parent._y + heightOf(parent) / 2 };
        var b = { x: child._x, y: child._y + heightOf(child) / 2 };
        var dx = Math.max(40, (b.x - a.x) * 0.5);
        d = "M " + a.x + " " + a.y + " C " + (a.x + dx) + " " + a.y + ", " + (b.x - dx) + " " + b.y + ", " + b.x + " " + b.y;
      }
      paths.push('<path class="gedge gedge-' + state + '" data-child="' + escapeHtml(id) + '" d="' + d + '"/>');
    });
    // Cross-dependency edges (depends_on) — rendered as faint dashed arcs bowed out
    // to the right, visually distinct from the solid parent-child tree edges (a
    // dependency is NOT a re-parenting; feedback #2). Emphasized on hover of an end.
    for (var di = 0; di < depEdges.length; di++) {
      if (depEdges[di].kind !== "depends_on") continue; // only depends_on edges are dep arcs
      var f = nodeById[depEdges[di].from], t = nodeById[depEdges[di].to];
      if (!f || !t) continue;
      var dd;
      if (goalsLayout === "org") {
        // center-to-center dashed chord (the column-relative bow is meaningless radially).
        var fc = nodeCenter(f), tc = nodeCenter(t);
        dd = "M " + fc.x + " " + fc.y + " L " + tc.x + " " + tc.y;
      } else {
        var pa = { x: f._x + f._w, y: f._y + heightOf(f) / 2 };
        var pb = { x: t._x + t._w, y: t._y + heightOf(t) / 2 };
        var bow = 44 + Math.abs(pa.y - pb.y) * 0.12;
        var cxx = Math.max(pa.x, pb.x) + bow;
        dd = "M " + pa.x + " " + pa.y + " C " + cxx + " " + pa.y + ", " + cxx + " " + pb.y + ", " + pb.x + " " + pb.y;
      }
      paths.push('<path class="gdep" data-from="' + escapeHtml(depEdges[di].from) + '" data-to="' + escapeHtml(depEdges[di].to) + '" d="' + dd + '"/>');
    }
    // Collaboration containers first so they sit BEHIND the spoke edges + nodes.
    svg.innerHTML = collabMarkup() + paths.join("");
    // Index each edge by its child id for the hover chain-highlight (rebuilt every
    // draw since the SVG is regenerated).
    edgeIndex = {};
    var pathEls = svg.querySelectorAll("path[data-child]");
    for (var k = 0; k < pathEls.length; k++) {
      var cid = pathEls[k].getAttribute("data-child");
      var childNode = nodeById[cid];
      edgeIndex[cid] = { path: pathEls[k], parent: childNode ? childNode.parent : null };
    }
  }

  /* ── tier column headers (one per depth present) ───────────────────────── */
  // Vocabulary per operator feedback #302: top-level = "flotilla", mid = "desk"
  // (labels first; the scope-schema rename is flotilla-dev's v2).
  function tierLabel(depth) {
    if (depth === 0) return "Flotilla";
    if (depth === 1) return "Desks";
    if (depth === 2) return "Tasks";
    return "Sub-tasks";
  }
  function renderTierLabels(maxDepth) {
    var out = [];
    for (var d = 0; d <= maxDepth; d++) {
      out.push('<div class="gtier-label" style="left:' + colX(d) + "px;width:" + colW(d) + 'px">' +
        escapeHtml(tierLabel(d)) + "</div>");
    }
    q("goals-tierlabels").innerHTML = out.join("");
  }

  /* ── keyed update: refresh live status in place, keep layout + focus ───── */
  // structuralSig captures ONLY the fields that affect layout or node identity —
  // id/parent/depth/scope/title/description/owner and each work-item's structural
  // fields (kind/label/ref/text). It deliberately EXCLUDES the live-changing bits
  // (status_display, a work-item's class/detail) because those change colour + a
  // pill word but never the card's size or position. When the structure is
  // unchanged, an SSE refresh updates the existing cards in place — preserving
  // element identity so keyboard focus and any transient UI classes (the drawer
  // selection / hover chain / pulse that later increments add to the article)
  // survive the tick, instead of being wiped by a full innerHTML teardown.
  // The tuple is DELIBERATELY order-sensitive (JSON.stringify of an array): a
  // reorder of goals[] changes the sig → a full rebuild → so updateInPlace's
  // index-based children[i] ↔ goals[i] mapping is never handed a reordered array.
  // Work items contribute only kind+label (their identity + count) — enough to
  // detect an add/remove/retitle; class/detail are excluded per contract #2 above.
  function structuralSig(goals) {
    return JSON.stringify({
      nodes: goals.map(function (n) {
        return [n.id, n.parent || "", n.depth || 0, n.scope || "", n.title || "",
          n.description || "", n.owner || "",
          // org-graph v2 enrichment — each is rendered into the card and changes its
          // height, so a change must trigger a full rebuild (not an in-place text swap).
          n.priorities || [], n.milestones || [], (n.harness && n.harness.surface) || "",
          (n.work_items || []).map(function (wi) { return [wi.kind || "", wi.label || ""]; })];
      }),
      // Collaboration membership drives clusterAdjacent — a lane change MOVES nodes
      // (re-angles the cluster), so it is STRUCTURAL: fold it in so a collaborations-only
      // change forces a full re-layout, not just an in-place SVG redraw (#324 Inc 3, #283).
      collab: collaborations.map(function (c) { return [c.lane || "", (c.desks || []).join(",")]; }),
    });
  }

  // updateInPlace refreshes each existing card's state token + inner content from
  // the new doc WITHOUT touching the article element's identity, geometry, or
  // non-state classes. Cards are in goals[] order (structure unchanged ⇒ same
  // order), so children[i] ↔ goals[i].
  function updateInPlace(goals, nodesEl) {
    goals.forEach(function (n, i) {
      var card = nodesEl.children[i];
      if (!card) return;
      var want = "state-" + visToken(n);
      if (!card.classList.contains(want)) {
        // swap the state-* token via classList (robust to an empty state or a
        // future transient class that also starts with "state-"); keep gnode /
        // gnode-<scope> / any transient class.
        for (var j = card.classList.length - 1; j >= 0; j--) {
          if (card.classList[j] !== want && card.classList[j].indexOf("state-") === 0) card.classList.remove(card.classList[j]);
        }
        card.classList.add(want);
      }
      // Rewrite inner content only when it actually changed — cuts per-tick churn
      // and preserves inner state on the cards that DID NOT change this tick.
      var html = nodeInner(n);
      if (card._inner !== html) { card.innerHTML = html; card._inner = html; }
    });
  }

  // focus preserved across a FULL rebuild (which replaces the articles): remember
  // the focused node id, restore focus to its new card after the rebuild.
  function focusedNodeId() {
    var a = document.activeElement;
    var card = a && a.closest ? a.closest(".gnode") : null;
    return card ? card.getAttribute("data-id") : null;
  }
  function restoreFocus(id) {
    if (!id) return;
    var card = cardEl(id);
    // preventScroll: the map is transform-positioned, not scroll-positioned, so the
    // default focus scroll-into-view would jerk the viewport/page to a card's raw
    // world coordinate. The operator's pan/zoom owns framing.
    if (card) card.focus({ preventScroll: true });
  }

  /* ── detail drawer + hover chain-highlight (Inc 2) ─────────────────────── */
  function cssIdEsc(id) { return String(id).replace(/["\\]/g, "\\$&"); }
  function cardEl(id) { return q("goals-nodes").querySelector('[data-id="' + cssIdEsc(id) + '"]'); }
  // scopeNoun maps the v2 scope to its UI label (design §1). Dual-reads the legacy v1
  // tokens (fleet/project) defensively so a not-yet-recompiled fixture still labels
  // correctly — the live API emits v2 (flotilla/desk/task).
  function scopeNoun(n) {
    var s = n.scope;
    if (s === "flotilla" || s === "fleet") return "Flotilla";
    if (s === "desk" || s === "project") return "Desk";
    return "Task";
  }
  function isFlotilla(n) { return n.scope === "flotilla" || n.scope === "fleet"; }

  // convAgent resolves the deep-link target: the explicit conversation_agent, else
  // the first desk work-item's agent (a real thread), else the owner label.
  function convAgent(n) {
    if (n.conversation_agent) return n.conversation_agent;
    var items = n.work_items || [];
    for (var i = 0; i < items.length; i++) { if (items[i].kind === "desk" && items[i].agent) return items[i].agent; }
    return n.owner || null;
  }

  // highlightChain lights the edges from a node up its parent chain to the root, so
  // hovering a task shows which workstream + fleet goal it rolls up to. Bounded
  // against a cycle the server should never emit.
  function highlightChain(id, on) {
    var cur = id, guard = 0;
    while (cur && edgeIndex[cur] && guard++ < 64) {
      if (edgeIndex[cur].path) edgeIndex[cur].path.classList.toggle("lit", on);
      cur = edgeIndex[cur].parent;
    }
  }

  // lightDeps emphasizes the cross-dependency arcs connected to a node (either end),
  // so hovering a node reveals what it depends on / what depends on it.
  function lightDeps(id, on) {
    var svg = q("goals-edges");
    if (!svg) return;
    var els = svg.querySelectorAll('.gdep[data-from="' + cssIdEsc(id) + '"], .gdep[data-to="' + cssIdEsc(id) + '"]');
    for (var i = 0; i < els.length; i++) els[i].classList.toggle("lit", on);
  }

  // drawerBody renders the node's detail from the SAME cached /api/goals data the
  // map draws — no extra endpoint. Every interpolated value is escaped.
  // drawerList renders a full ordered list section (priorities / milestones) in the
  // drawer, or "" when absent — parts.push("") is harmless (join ignores it).
  function drawerList(arr, heading) {
    var xs = Array.isArray(arr) ? arr : [];
    if (!xs.length) return "";
    return '<div class="gd-sec"><h4>' + heading + "</h4><ol class=\"gd-list\">" +
      xs.map(function (x) { return "<li>" + escapeHtml(x) + "</li>"; }).join("") + "</ol></div>";
  }

  function drawerBody(n) {
    var parts = [];
    // Deep-link to this cell's conversation (feedback #3): prefer the explicit
    // conversation_agent, then an actual desk work-item's agent (a routable
    // thread), and only then the owner label (a lead role may have no thread).
    var agent = convAgent(n);
    if (agent) {
      parts.push('<div class="gd-sec"><button class="gd-convo" type="button" data-agent="' + escapeHtml(agent) +
        '">Open ' + escapeHtml(agent) + "&rsquo;s conversation &rarr;</button></div>");
    }
    if (n.description) parts.push('<div class="gd-sec"><h4>What this is</h4><p>' + escapeHtml(n.description) + "</p></div>");
    if (n.harness && n.harness.surface) {
      parts.push('<div class="gd-sec gd-harness"><h4>Harness</h4><p>' + escapeHtml(n.harness.surface) + "</p></div>");
    }
    // org-graph v2: full priorities (flotilla) / milestones (desk) — the drawer shows
    // the complete list (the node card caps at 4).
    parts.push(drawerList(n.priorities, "Priorities"));
    parts.push(drawerList(n.milestones, "Current work"));
    // Operator-gated items (awaiting / blocked) surfaced as the "waiting on you" call-out.
    var gated = (n.work_items || []).filter(function (wi) { return wi.class === "awaiting" || wi.class === "blocked"; });
    if (gated.length) {
      parts.push('<div class="gd-sec"><div class="gd-ask"><div class="gd-ask-lab">Waiting on you</div>' +
        gated.map(function (wi) { return "<p>" + escapeHtml(wi.label || wi.kind || "") + (wi.detail ? " — " + escapeHtml(wi.detail) : "") + "</p>"; }).join("") +
        "</div></div>");
    }
    if ((n.work_items || []).length) {
      parts.push('<div class="gd-sec"><h4>Work (' + n.work_items.length + ")</h4>" +
        n.work_items.map(function (wi) {
          return '<div class="gd-row"><span class="gd-row-l">' + escapeHtml(wi.label || wi.kind || "") + "</span>" +
            '<span class="gwi-detail gwi-' + escapeHtml(wi.class || "unknown") + '">' + escapeHtml(wi.detail || wi.class || "") + "</span></div>";
        }).join("") + "</div>");
    }
    var kids = (n.children || []).map(function (id) { return nodeById[id]; }).filter(Boolean);
    if (kids.length) {
      parts.push('<div class="gd-sec"><h4>' + (isFlotilla(n) ? "Desks" : "Tasks") + " (" + kids.length + ")</h4>" +
        kids.map(function (k) {
          var kv = visToken(k);
          return '<div class="gd-row"><span class="gd-row-l">' + escapeHtml(k.title || k.id) + "</span>" +
            '<span class="gpill gpill-' + escapeHtml(kv) + '">' + escapeHtml(STATE_LABEL[kv] || kv) + "</span></div>";
        }).join("") + "</div>");
    }
    // Cross-dependencies — the gantt-style ID labels for feedback #2, shown cleanly
    // in the drawer alongside the faint canvas arcs. Derived from GoalsDoc.edges
    // (the API exposes dependencies there, not as a per-node field).
    var deps = depEdges.filter(function (e) { return e.kind === "depends_on" && e.from === n.id; })
      .map(function (e) { return nodeById[e.to]; }).filter(Boolean);
    if (deps.length) {
      parts.push('<div class="gd-sec"><h4>Depends on</h4>' +
        deps.map(function (d) {
          var dv = visToken(d);
          return '<div class="gd-row"><span class="gd-row-l">' + escapeHtml(d.title || d.id) + "</span>" +
            '<span class="gpill gpill-' + escapeHtml(dv) + '">' + escapeHtml(STATE_LABEL[dv] || dv) + "</span></div>";
        }).join("") + "</div>");
    }
    return parts.join("");
  }

  var lastDrawerHtml = null; // dirty-check: don't rewrite the drawer body (and reset its scroll) on a no-op tick
  function fillDrawer(n) {
    var vis = visToken(n);
    q("goals-drawer-eyebrow").textContent = scopeNoun(n);
    q("goals-drawer-title").textContent = n.title || n.id;
    var pills = '<span class="gpill gpill-' + escapeHtml(vis) + '">' + escapeHtml(STATE_LABEL[vis] || vis) + "</span>";
    if (n.owner) pills += '<span class="gd-owner">led by ' + escapeHtml(n.owner) + "</span>";
    // parent-goal pill — which fleet goal / workstream this rolls up to (prototype fidelity).
    var parent = n.parent ? nodeById[n.parent] : null;
    if (parent) pills += '<span class="gd-parent">&#8627; ' + escapeHtml(parent.title || parent.id) + "</span>";
    q("goals-drawer-pills").innerHTML = pills;
    // Rewrite the body only when it changed, so a background SSE tick never resets
    // the operator's scroll position or text selection in the open drawer.
    var html = drawerBody(n);
    if (lastDrawerHtml !== html) { q("goals-drawer-body").innerHTML = html; lastDrawerHtml = html; }
  }

  function selectNode(id) {
    deselect();
    selectedId = id;
    lastDrawerHtml = null; // a new selection always fills fresh
    var card = cardEl(id);
    if (card) card.classList.add("gnode-selected"); // transient state lives on the ARTICLE (#283 contract)
  }
  function deselect() {
    if (selectedId) { var c = cardEl(selectedId); if (c) c.classList.remove("gnode-selected"); }
    selectedId = null;
  }
  var restoringNode = false; // true while the history controller restores a drawer (no re-push)
  function openDrawer(id) {
    var n = nodeById[id];
    if (!n) return;
    selectNode(id);
    fillDrawer(n);
    var d = q("goals-drawer");
    if (!d) return;
    d.classList.add("open");
    d.setAttribute("aria-hidden", "false");
    var close = q("goals-drawer-close");
    if (close) close.focus({ preventScroll: true }); // move focus into the dialog for keyboard users
    // Opening a drawer is a reversible nav step (#349 A1) — Back closes it.
    if (!restoringNode && window.flotillaDash && window.flotillaDash.pushNav) {
      window.flotillaDash.pushNav({ view: "goals", node: id });
    }
  }
  // restoreNode is the history controller's hook (#349 A1): open the drawer on the given
  // node (or close it) WITHOUT pushing a new entry. Best-effort — if the node isn't in the
  // freshly-rendered map yet, it no-ops rather than throwing.
  function restoreNode(id) {
    restoringNode = true;
    if (id && nodeById[id]) openDrawer(id);
    else closeDrawer();
    restoringNode = false;
  }
  function closeDrawer() {
    var d = q("goals-drawer");
    if (d) { d.classList.remove("open"); d.setAttribute("aria-hidden", "true"); }
    var card = selectedId ? cardEl(selectedId) : null;
    lastDrawerHtml = null;
    deselect();
    if (card) card.focus({ preventScroll: true }); // return focus to the node that opened it
  }

  // nodeActivate is the primary node action (#302): deep-link to the node's
  // Conversations thread. A node with no conversation agent (an abstract flotilla
  // aim) falls back to the detail drawer.
  // nodeActivate is the primary node action (#349 A2): open the detail drawer — "the
  // point is I need to see the thing". (Was: deep-link to Conversations; that jump is now
  // the explicit → desk button / goToDesk.)
  function nodeActivate(id) {
    if (nodeById[id]) openDrawer(id);
  }

  // goToDesk jumps to a node's Conversations thread — the → desk button, and the drawer's
  // own open-conversation button, both route here (synchronized, #349 A2).
  function goToDesk(id) {
    var n = nodeById[id];
    if (!n) return;
    var agent = convAgent(n);
    if (agent && window.flotillaDash && window.flotillaDash.openConversation) {
      window.flotillaDash.openConversation(agent);
    }
  }

  // openModal — the "waiting on you" intervention modal (#302): a situation brief
  // (scope, description, the operator-gated items) + a text input for a response.
  // The reply PATH is a stub for this prototype (wired to the control API later).
  // renderBrief turns a decision-package markdown string into safe HTML (#347). It ESCAPES
  // first (no raw HTML from the brief ever reaches the DOM), then applies a small, fixed
  // markdown subset: #/##/### headings, - / * bullet lists, blank-line paragraphs, and
  // inline **bold** + `code`. Enough for a real decision brief; nothing that can inject.
  function renderBrief(md) {
    var lines = escapeHtml(String(md == null ? "" : md)).split(/\r?\n/);
    function inline(s) {
      return s.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>").replace(/`([^`]+)`/g, "<code>$1</code>");
    }
    var out = [], list = null;
    function flush() { if (list) { out.push("<ul>" + list.join("") + "</ul>"); list = null; } }
    for (var i = 0; i < lines.length; i++) {
      var ln = lines[i], h = /^(#{1,3})\s+(.*)$/.exec(ln), li = /^\s*[-*]\s+(.*)$/.exec(ln);
      if (h) { flush(); out.push('<div class="gm-brief-h">' + inline(h[2]) + "</div>"); }
      else if (li) { (list = list || []).push("<li>" + inline(li[1]) + "</li>"); }
      else if (ln.trim() === "") { flush(); }
      else { flush(); out.push("<p>" + inline(ln) + "</p>"); }
    }
    flush();
    return out.join("");
  }

  var BRIEF_EMPTY = '<p class="gm-brief-empty muted">No decision brief yet — ask the desk for the recommendation, the value, and the tradeoff before deciding.</p>';
  // hasBrief trims before the presence check so a whitespace-only brief shows the honest
  // no-brief state instead of an empty rendered block (cubic #348 P2).
  function hasBrief(s) { return !!(s && String(s).trim()); }

  function openModal(id) {
    var n = nodeById[id];
    if (!n) return;
    var gated = (n.work_items || []).filter(function (wi) { return wi.class === "awaiting" || wi.class === "blocked"; });
    var parts = ['<p class="gm-scope">' + escapeHtml(scopeNoun(n)) + "</p>"];
    if (n.description) parts.push("<p>" + escapeHtml(n.description) + "</p>");
    // A node-level decision package renders in full (the decision is on the node itself).
    if (hasBrief(n.brief)) parts.push('<div class="gm-brief-full">' + renderBrief(n.brief) + "</div>");
    if (gated.length) {
      parts.push('<div class="gm-gated"><div class="gm-gated-lab">Waiting on you</div>' +
        gated.map(function (wi) {
          var head = '<p class="gm-gated-item">' + escapeHtml(wi.label || wi.kind || "") + (wi.detail ? " — " + escapeHtml(wi.detail) : "") + "</p>";
          // The decision package inline (#347) — or an honest empty state when the desk
          // has not attached one yet.
          var body = hasBrief(wi.brief) ? '<div class="gm-brief-full">' + renderBrief(wi.brief) + "</div>" : BRIEF_EMPTY;
          return '<div class="gm-gated-row">' + head + body + "</div>";
        }).join("") + "</div>");
    } else if (!hasBrief(n.brief)) {
      parts.push('<p class="muted">Nothing is gated on you here.</p>');
    }
    q("goals-modal-title").textContent = n.title || n.id;
    q("goals-modal-brief").innerHTML = parts.join("");
    var ta = q("goals-modal-input");
    if (ta) ta.value = "";
    var note = q("goals-modal-note");
    if (note) note.textContent = ""; // clear the stub "not sent" note from a prior open
    var m = q("goals-modal");
    m.classList.add("open");
    m.setAttribute("aria-hidden", "false");
    modalReturn = document.activeElement;
    if (ta) ta.focus();
  }
  function closeModal() {
    var m = q("goals-modal");
    if (m) { m.classList.remove("open"); m.setAttribute("aria-hidden", "true"); }
    if (modalReturn && modalReturn.focus) modalReturn.focus({ preventScroll: true });
    modalReturn = null;
  }

  // reapplyTransient re-establishes selection + hover after a render. On an in-place
  // update the article keeps its .gnode-selected class, but on a full rebuild the
  // articles are replaced — so re-add the selection and refresh the open drawer's
  // content (live status may have moved). If the selected node vanished (a
  // structural change removed it), close the drawer.
  function reapplyTransient() {
    if (selectedId) {
      var n = nodeById[selectedId];
      if (!n) { closeDrawer(); }
      else {
        var card = cardEl(selectedId);
        if (card) card.classList.add("gnode-selected");
        var d = q("goals-drawer");
        if (d && d.classList.contains("open")) fillDrawer(n);
      }
    }
    if (hoveredId) {
      if (nodeById[hoveredId]) { highlightChain(hoveredId, true); lightDeps(hoveredId, true); }
      else hoveredId = null;
    }
  }

  function wireNodes() {
    if (nodesWired) return;
    var nodesEl = q("goals-nodes");
    if (!nodesEl) return;
    nodesWired = true;
    nodesEl.addEventListener("click", function (e) {
      var respond = e.target.closest(".gnode-respond");
      if (respond) { var rc = respond.closest(".gnode"); if (rc) openModal(rc.getAttribute("data-id")); return; }
      var godesk = e.target.closest(".gnode-godesk");
      if (godesk) { var gc = godesk.closest(".gnode"); if (gc) goToDesk(gc.getAttribute("data-id")); return; }
      var card = e.target.closest(".gnode");
      if (card) nodeActivate(card.getAttribute("data-id")); // #349 A2: node body → detail drawer
    });
    nodesEl.addEventListener("keydown", function (e) {
      if (e.key !== "Enter" && e.key !== " ") return;
      var card = e.target.closest(".gnode");
      // Only the article itself deep-links on Enter; a control button (⚠ respond /
      // ⓘ details) focused inside the card handles its OWN Enter (opens the modal /
      // drawer) — don't preventDefault it here or the keyboard route to them is lost.
      if (!card || e.target !== card) return;
      e.preventDefault();
      nodeActivate(card.getAttribute("data-id"));
    });
    // Tabbing to a node that's panned off-screen recenters the map on it (the
    // transform equivalent of scroll-into-view — the world can't be scrolled). Only
    // on KEYBOARD focus: a mouse click (or a programmatic restoreFocus after a
    // rebuild) must NOT yank the map — that would break the #283 "operator's pan/zoom
    // owns framing" contract.
    nodesEl.addEventListener("focusin", function (e) {
      var kbd = kbdNav;
      kbdNav = false; // one-shot: consume it here so a LATER programmatic focus
      // (e.g. restoreFocus after a rebuild, or focus after a wheel-zoom) can't recenter
      if (!kbd) return;
      var card = e.target.closest(".gnode");
      if (!card) return;
      var n = nodeById[card.getAttribute("data-id")];
      if (n && !nodeVisible(n)) recenterOn(n);
    });
    // Track focus modality: Tab means keyboard navigation; any pointer press means
    // mouse/touch (so its focus won't recenter).
    document.addEventListener("keydown", function (e) { if (e.key === "Tab") kbdNav = true; }, true);
    document.addEventListener("pointerdown", function () { kbdNav = false; }, true);
    nodesEl.addEventListener("mouseover", function (e) {
      var card = e.target.closest(".gnode");
      if (!card) return;
      var id = card.getAttribute("data-id");
      if (id === hoveredId) return; // still within the same card (delegation fires on inner spans too)
      hoveredId = id;
      highlightChain(id, true);
      lightDeps(id, true);
    });
    nodesEl.addEventListener("mouseout", function (e) {
      var card = e.target.closest(".gnode");
      if (!card) return;
      if (e.relatedTarget && card.contains(e.relatedTarget)) return; // moving within the same card
      var id = card.getAttribute("data-id");
      highlightChain(id, false);
      lightDeps(id, false);
      if (hoveredId === id) hoveredId = null;
    });
    var close = q("goals-drawer-close");
    if (close) close.onclick = closeDrawer;
    // Deep-link: the drawer's "Open …'s conversation" button jumps to that desk's
    // Conversations thread (delegated — the body is rebuilt on each fill).
    var drawer = q("goals-drawer");
    if (drawer) drawer.addEventListener("click", function (e) {
      var btn = e.target.closest(".gd-convo");
      if (!btn) return;
      var agent = btn.getAttribute("data-agent");
      if (agent && window.flotillaDash && window.flotillaDash.openConversation) {
        closeDrawer();
        window.flotillaDash.openConversation(agent);
      }
    });
    // Help tooltip: also toggle on click (touch has no hover) — CSS shows it on
    // hover/focus AND when aria-expanded is true.
    var help = q("goals-help");
    if (help) help.onclick = function () {
      help.setAttribute("aria-expanded", help.getAttribute("aria-expanded") === "true" ? "false" : "true");
    };
    // Intervention modal (#302): close on the × / backdrop; the "Send" is a stub for
    // this prototype (the reply path wires to the control API in a follow-on).
    var modal = q("goals-modal");
    if (modal) modal.addEventListener("click", function (e) {
      if (e.target.closest(".gm-close") || e.target.classList.contains("goals-modal")) closeModal();
    });
    // Focus trap (aria-modal): keep Tab / Shift+Tab cycling among the modal's
    // controls (close, textarea, send) while it's open — Tab must not escape onto
    // the background content behind the overlay.
    if (modal) modal.addEventListener("keydown", function (e) {
      if (e.key !== "Tab" || !modal.classList.contains("open")) return;
      var f = modal.querySelectorAll(".gm-close, #goals-modal-input, #goals-modal-send");
      if (!f.length) return;
      var first = f[0], last = f[f.length - 1];
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    });
    var send = q("goals-modal-send");
    if (send) send.onclick = function () {
      var note = q("goals-modal-note");
      if (note) note.textContent = "Reply path is a prototype stub — not sent (wiring to the control API is a follow-on).";
    };
    document.addEventListener("keydown", function (e) {
      if (e.key !== "Escape" || !isVisible()) return;
      if (help) help.setAttribute("aria-expanded", "false"); // Esc dismisses the tooltip too
      if (q("goals-modal") && q("goals-modal").classList.contains("open")) { closeModal(); return; }
      closeDrawer();
    });
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
      closeDrawer(); // a drawer open over the now-hidden graph would be stale
      graph.classList.add("hidden");
      empty.classList.remove("hidden");
      empty.textContent = doc.error
        ? ("Goals file could not be loaded: " + doc.error)
        : (doc.message || "No goals file configured.");
      announce(empty.textContent); // mirror the state to the screen reader, not "0 of 0"
      lastSig = sig; // a complete (synchronous) render
      return;
    }
    var goals = Array.isArray(doc.goals) ? doc.goals : [];
    if (!goals.length) {
      closeDrawer();
      graph.classList.add("hidden");
      empty.classList.remove("hidden");
      empty.textContent = "No goals defined yet.";
      announce(empty.textContent);
      lastSig = sig; // a complete (synchronous) render
      return;
    }
    graph.classList.remove("hidden");
    empty.classList.add("hidden");
    updateLive(doc.counts || {}); // announce the situation summary — success path only (see renderSituation)
    depEdges = Array.isArray(doc.edges) ? doc.edges : []; // cross-dependency edges for drawEdges
    collaborations = Array.isArray(doc.collaborations) ? doc.collaborations : []; // desk lanes (#324 Inc 3)

    var nodesEl = q("goals-nodes");
    var ssig = structuralSig(goals);

    // In-place fast path: the structure is unchanged AND the canvas is already laid
    // out, so only live status moved. Update the existing cards in place — keeping
    // their geometry, keyboard focus, and any transient classes — and recolour the
    // edges. No teardown, no re-layout, no rAF. (laidOut guards against updating a
    // provisional/aborted DOM; the count guard against a desync.)
    if (laidOut && ssig === lastStructSig && nodesEl.children.length === goals.length) {
      goals.forEach(function (n) {
        var prev = nodeById[n.id];
        if (prev) { prev.status_display = n.status_display; prev.work_items = n.work_items; }
      });
      updateInPlace(goals, nodesEl);
      drawEdges(); // child state may have changed → recolour (the SVG is stateless)
      reapplyTransient(); // re-light hover chain + refresh the open drawer's live status
      lastSig = sig;
      return;
    }

    // Structural change ⇒ full rebuild + re-layout. Preserve keyboard focus across
    // the article replacement.
    laidOut = false;
    var keepFocus = focusedNodeId();
    buildNodeIndex(goals);
    var roots = goals.filter(function (n) { return !n.parent || !nodeById[n.parent]; });
    var maxDepth = 0;
    goals.forEach(function (n) { maxDepth = Math.max(maxDepth, depthOf(n)); });

    // Pass 1: render at column x with provisional y=0 so heights can be measured.
    goals.forEach(function (n) { n._y = 0; });
    nodesEl.innerHTML = goals.map(nodeCard).join("");
    // Tier column headers belong to the tree layout only — the org layout has no columns.
    if (goalsLayout === "org") q("goals-tierlabels").innerHTML = "";
    else renderTierLabels(maxDepth);

    // Measure + final layout after the browser flushes layout. Guarded so a newer
    // render (a refresh that landed between here and the frame) wins.
    var myEpoch = epoch;
    requestAnimationFrame(function () {
      // Aborted — superseded by a newer refresh, or the tab went hidden (rAF is
      // suspended in a backgrounded tab while dash.js keeps calling refresh()). Do
      // NOT commit lastSig/lastStructSig/laidOut: the canvas is still at its
      // provisional pass-1 layout, so the next refresh must re-render it rather than
      // dedup-skip or in-place-update a half-finished map. show() re-renders on
      // tab return.
      if (myEpoch !== epoch || !isVisible()) return;
      // Cards render in goals[] order, so children[i] ↔ goals[i] — read heights
      // in one pass (all reads batched before any write) to avoid layout thrash.
      goals.forEach(function (n, i) { n._h = nodesEl.children[i] ? nodesEl.children[i].offsetHeight : DEFAULT_H; });
      if (goalsLayout === "org") layoutOrg(goals, roots);
      else layoutY(roots);
      goals.forEach(function (n, i) {
        var c = nodesEl.children[i];
        // Both left AND top: the org layout moves x per node (radial), where the tree
        // layout kept x fixed at its column. Setting left is a no-op in tree mode.
        if (c) { c.style.left = n._x + "px"; c.style.top = n._y + "px"; c._inner = nodeInner(n); } // seed _inner so the in-place dirty-skip works from tick 1
      });
      var world = q("goals-world");
      world.style.width = view.worldW + "px";
      world.style.height = view.worldH + "px";
      drawEdges();
      // Fit: the tree anchors top (columns read down); the org graph is a centered
      // disc, so frame the whole thing centered.
      if (!view.fitted) { (goalsLayout === "org" ? fitOverview : fit)(); view.fitted = true; }
      applyTransform();
      restoreFocus(keepFocus);
      reapplyTransient(); // re-select the drawer's node (articles were replaced) + re-light hover
      lastStructSig = ssig; // commit ONLY after a complete pass-2 render
      laidOut = true;
      lastSig = sig;
    });
  }

  /* ── pan / zoom (ported) ───────────────────────────────────────────────── */
  function applyTransform() {
    var world = q("goals-world");
    if (!world) return;
    world.style.transform = "translate(" + view.tx + "px," + view.ty + "px) scale(" + view.scale + ")";
    // Counter-scale the node controls so they stay screen-constant (tappable) as the map
    // zooms out — inherited by every .gnode-ctl (mobile-QA #330). Only enlarge (never
    // shrink below base) when zoomed out; base size when zoomed in.
    world.style.setProperty("--ctl-scale", Math.max(1, 1 / (view.scale || 1)));
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

    // Deliberate-pan gate (#330): a touch-drag pans the map ONLY after the operator
    // toggles "move map"; until then the viewport's touch-action:pan-y lets the gesture
    // scroll the PAGE through the map (org is the phone default — no nested-scroll trap).
    // Mouse panning is always on (desktop unchanged).
    var touchPanActive = false;
    var panlock = q("goals-panlock");
    if (panlock) {
      panlock.addEventListener("click", function () {
        touchPanActive = !touchPanActive;
        vp.classList.toggle("pan-active", touchPanActive);
        panlock.classList.toggle("active", touchPanActive);
        panlock.setAttribute("aria-pressed", String(touchPanActive));
      });
    }

    var drag = false, sx = 0, sy = 0;
    function endDrag() { drag = false; vp.classList.remove("grabbing"); }
    vp.addEventListener("pointerdown", function (e) {
      if (e.target.closest(".gnode") || e.target.closest(".gzoomctl")) return;
      // On touch, do NOT capture the gesture unless pan mode is active — let it fall
      // through to the browser's pan-y page scroll. Mouse always pans.
      if (e.pointerType === "touch" && !touchPanActive) return;
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

    // Keyboard pan/zoom when the MAP CONTAINER itself holds focus (the viewport is
    // tabbable). Guarded on e.target === vp so arrow keys still Tab between the
    // focusable node treeitems inside — the map is one tab stop, its nodes another.
    vp.addEventListener("keydown", function (e) {
      if (e.target !== vp) return;
      if (e.ctrlKey || e.metaKey || e.altKey) return; // never swallow native browser zoom (Ctrl/Cmd +/-/0)
      var step = 60, handled = true;
      switch (e.key) {
        case "ArrowLeft": view.tx += step; break;
        case "ArrowRight": view.tx -= step; break;
        case "ArrowUp": view.ty += step; break;
        case "ArrowDown": view.ty -= step; break;
        case "+": case "=": view.scale = Math.min(2.2, view.scale * 1.18); break;
        case "-": case "_": view.scale = Math.max(0.25, view.scale * 0.85); break;
        case "0": e.preventDefault(); fitOverview(); return;
        default: handled = false;
      }
      if (handled) { e.preventDefault(); applyTransform(); }
    });
  }

  // nodeVisible / recenterOn: a node moved off-screen by pan/zoom can't be scrolled
  // into view (the world is transform-positioned, not scrolled) — so when KEYBOARD
  // focus lands on an off-screen node, recenter the world on it, the transform
  // equivalent of scroll-into-view. Visibility is judged on the node's CENTER (not
  // full containment), so a node flush against an edge or taller than the viewport
  // doesn't trigger a needless jump. The zoom is left untouched — the operator's
  // chosen scale is theirs to keep (WCAG change-on-request).
  function nodeVisible(n) {
    var vp = q("goals-viewport");
    if (!vp || !n) return true;
    var cx = (n._x + n._w / 2) * view.scale + view.tx;
    var cy = (n._y + heightOf(n) / 2) * view.scale + view.ty;
    return cx >= 0 && cx <= vp.clientWidth && cy >= 0 && cy <= vp.clientHeight;
  }
  function recenterOn(n) {
    var vp = q("goals-viewport");
    if (!vp || !n) return;
    view.tx = vp.clientWidth / 2 - (n._x + n._w / 2) * view.scale;
    view.ty = vp.clientHeight / 2 - (n._y + heightOf(n) / 2) * view.scale;
    applyTransform();
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

  // setLayout flips tree ⇄ org and forces a full re-layout: the mode isn't per-node
  // data, so it doesn't change structuralSig — null it to defeat the in-place fast path,
  // reset the fit so the new geometry re-frames, and re-render.
  function setLayout(mode) {
    var next = mode === "org" ? "org" : "tree";
    if (next === goalsLayout) return;
    goalsLayout = next;
    var btns = document.querySelectorAll(".glayout-btn");
    for (var i = 0; i < btns.length; i++) {
      var on = btns[i].getAttribute("data-layout") === goalsLayout;
      btns[i].classList.toggle("active", on);
      btns[i].setAttribute("aria-pressed", String(on));
    }
    lastStructSig = null; // force a full rebuild (not an in-place status swap)
    view.fitted = false;  // re-frame for the new geometry
    // Only render immediately when a doc is already cached — a toggle BEFORE the first
    // fetch would otherwise render the empty doc and flash a false "No goals file
    // configured" state. The new mode is retained and applies to the next render (the
    // pending/next refresh) either way (cubic #316 P2).
    if (cache && isVisible()) render();
  }

  var layoutWired = false;
  function wireLayoutToggle() {
    if (layoutWired) return;
    var btns = document.querySelectorAll(".glayout-btn");
    if (!btns.length) return;
    layoutWired = true;
    for (var i = 0; i < btns.length; i++) {
      // Sync the active button to the seeded default (the markup hardcodes org active,
      // but an env-seeded tree default must show tree active) — #317.
      var on = btns[i].getAttribute("data-layout") === goalsLayout;
      btns[i].classList.toggle("active", on);
      btns[i].setAttribute("aria-pressed", String(on));
      btns[i].addEventListener("click", function () { setLayout(this.getAttribute("data-layout")); });
    }
  }

  function show() {
    activated = true;
    setupPanZoom();
    wireNodes();
    wireLayoutToggle();
    if (cache) { render(); } else { refresh(); }
  }

  // Re-fit on resize (keeps the map framed); the transform is otherwise the
  // operator's to drive via pan/zoom. Mode-aware, matching the first-layout branch:
  // the tree top-anchors (fit), the org graph frames centered (fitOverview) — a
  // resize on the default org view must NOT jump it to tree framing (cubic #327 P2).
  var resizeTimer = null;
  window.addEventListener("resize", function () {
    if (!isVisible()) return;
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(function () {
      (goalsLayout === "org" ? fitOverview : fit)();
      applyTransform();
    }, 120);
  });

  window.flotillaGoals = {
    show: show, refresh: refresh, restoreNode: restoreNode,
    openNode: function () { return selectedId; }, // the open drawer's node id (or null) — for history state
  };
})();
