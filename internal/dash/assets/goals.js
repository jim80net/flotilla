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
  // #405 Inc 3 Items 5+6 ────────────────────────────────────────────────────────
  // Item 5: stat-cell click-to-highlight. activeCellTone is the tone of the
  // currently-active filter ("goal" | "inflight" | "pending" | "aspirational" |
  // null). On a live-tick re-render, reapplyTransient() re-applies the filter so
  // SSE updates don't wipe it.
  var activeCellTone = null;
  // Item 6a (Realized look-back) — LIVE as of #418: the server records roll-up
  // transitions in goals-done.jsonl and stamps achieved_at (+ achieved_seed) onto
  // currently-achieved goals, so a bounded window counts REAL history. Seeds (goals
  // already achieved when history began — true achieve time unknown) are excluded
  // from bounded windows rather than counted as fabricated recency.
  var realizedWindow = "all"; // "1d" | "7d" | "30d" | "all"
  var WINDOW_MS = { "1d": 864e5, "7d": 7 * 864e5, "30d": 30 * 864e5 };
  // Item 6b: node hover tooltip — a fixed-positioned overlay that avoids the
  // CSS transform on goals-world and uses screen coords directly.
  var tipEl = null;
  var modalReturn = null;   // fallback element to restore focus to when the modal closes
  var modalNodeId = null;   // #501: the node the modal CURRENTLY shows — the respond target
                            // (modalReturnId keeps the ORIGINAL opener across drill-ins; the
                            // send must address the node on screen, so it tracks separately)
  var modalReturnId = null; // #354 P2: the NODE id that opened the modal — re-queried live on
                            // close so a drill-in / SSE re-render that replaced the trigger
                            // element still restores focus (survives detach).
  // Layout mode. The Goals map renders as a MIND MAP ONLY (operator directive 2026-07-06:
  // "mind-map should be the only view option" — the tree/mind-map toggle was removed). The
  // "tree" (tiered altitude columns) and "org" (hub-and-spoke) geometries below stay in the
  // code DORMANT and unreachable — there is no picker and no seed that can activate them — so
  // this is a view-picker simplification, not a rebuild of the map. Any config/env that used
  // to seed "tree" is redirected to mindmap server-side (normalizeGoalsLayout).
  var goalsLayout = "mindmap";
  // Radial modes (org pinwheel + the mind map) share card sizing, radial fit, no tier
  // labels, and center-anchored geometry — only their placement + edge shape differ. The
  // tree is the odd one out (altitude columns).
  function isRadial() { return goalsLayout === "org" || goalsLayout === "mindmap"; }

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
  function nodeW(depth) { return isRadial() ? (depth === 0 ? 216 : 176) : colW(depth); }
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
    blocked: "blocked", pending: "waiting on a dependency", active: "active",
    // #405 Inc 3 Q2: the aspirational state reads "planned" everywhere it surfaces (pill labels,
    // the legend) to match the renamed "Planned" tile — sweep the rename, not just the tiles.
    aspirational: "planned", paused: "paused", cancelled: "cancelled",
  };

  // ── #405 Inc 3 Item 5: stat-cell click-to-highlight helpers ────────────────
  // TONE_TO_SEL maps a tile tone to the CSS selector for its matching nodes.
  var TONE_TO_SEL = {
    // "Flotillas" tile → flotilla-scope nodes. Match BOTH the v2 `flotilla` class and
    // the legacy v1 `fleet` class (nodeCard emits gnode-<scope>, and scopeNoun/isFlotilla
    // dual-read fleet) so older/compat inputs still highlight (cubic #405 P2).
    "goal":        ".gnode-flotilla, .gnode-fleet",
    "inflight":    ".state-in-flight",   // "In flight" tile → in-flight state nodes
    "pending":     ".state-pending",     // "Blocked" tile → pending state nodes
    "aspirational":".state-aspirational",// "Planned" tile → aspirational state nodes
  };

  // applyFilter dims all nodes except those matching the tile's selector. Re-calling
  // with the same tone re-applies (safe for reapplyTransient). The #goals-nodes
  // container gets .gcell-focus (dims everything via CSS opacity); each matching node
  // gets .gnode-hl (restores full opacity).
  function applyFilter(tone) {
    var nodesEl = q("goals-nodes"), sit = q("goals-situation");
    if (!nodesEl || !TONE_TO_SEL[tone]) return;
    activeCellTone = tone;
    nodesEl.classList.add("gcell-focus");
    var all = nodesEl.querySelectorAll(".gnode");
    for (var i = 0; i < all.length; i++) {
      if (all[i].matches(TONE_TO_SEL[tone])) all[i].classList.add("gnode-hl");
      else all[i].classList.remove("gnode-hl");
    }
    // Mark the active tile; clear any previously active tile.
    if (sit) {
      var tiles = sit.querySelectorAll("[data-filter-tone]");
      for (var j = 0; j < tiles.length; j++) {
        tiles[j].classList.toggle("gcell-active", tiles[j].getAttribute("data-filter-tone") === tone);
      }
    }
  }

  // clearFilter removes the cell filter: restores all nodes to full opacity.
  function clearFilter() {
    var nodesEl = q("goals-nodes"), sit = q("goals-situation");
    if (nodesEl) {
      nodesEl.classList.remove("gcell-focus");
      var all = nodesEl.querySelectorAll(".gnode-hl");
      for (var i = 0; i < all.length; i++) all[i].classList.remove("gnode-hl");
    }
    if (sit) {
      var tiles = sit.querySelectorAll(".gcell-active");
      for (var j = 0; j < tiles.length; j++) tiles[j].classList.remove("gcell-active");
    }
    activeCellTone = null;
  }

  // ── #405 Inc 3 Item 6b: node hover tooltip ──────────────────────────────────
  // A fixed-position overlay positioned at the cursor (screen coordinates), so it is
  // unaffected by the CSS transform on goals-world. Injected once into document.body.
  function ensureTip() {
    if (!tipEl) {
      tipEl = document.createElement("div");
      tipEl.id = "goals-node-tip";
      tipEl.className = "gnode-tip";
      tipEl.setAttribute("role", "tooltip");
      tipEl.setAttribute("aria-hidden", "true");
      document.body.appendChild(tipEl);
    }
    return tipEl;
  }

  function showTip(n, x, y) {
    var t = ensureTip();
    var vis = visToken(n);
    var stateLabel = STATE_LABEL[vis] || vis;
    // "what it's doing": first active work item's detail or label.
    var doing = "";
    var items = n.work_items || [];
    for (var i = 0; i < items.length; i++) {
      var wi = items[i];
      if (wi.class === "in-flight" || wi.class === "awaiting" || wi.class === "blocked") {
        doing = wi.detail || wi.label || "";
        break;
      }
    }
    var meta = escapeHtml(stateLabel);
    if (n.owner) meta += " · led by " + escapeHtml(n.owner);
    if (doing) meta += " · " + escapeHtml(doing);
    t.innerHTML =
      '<span class="gnt-scope">' + escapeHtml(scopeNoun(n)) + "</span>" +
      " <strong>" + escapeHtml(n.title || n.id) + "</strong>" +
      "<br><span class=\"gnt-meta gnt-" + escapeHtml(vis) + "\">" + meta + "</span>";
    // Position near cursor; clamp to viewport edges so the tooltip never clips.
    var vw = window.innerWidth, vh = window.innerHeight;
    var m = 14;
    var tx = x + m, ty = y + m;
    // Read the actual rendered size if available; fall back to a generous estimate.
    var tw = t.offsetWidth || 260, th = t.offsetHeight || 56;
    if (tx + tw > vw - m) tx = x - tw - m;
    if (ty + th > vh - m) ty = y - th - m;
    t.style.left = Math.max(0, tx) + "px";
    t.style.top  = Math.max(0, ty) + "px";
    t.classList.add("visible");
    t.setAttribute("aria-hidden", "false");
  }

  function hideTip() {
    if (tipEl) {
      tipEl.classList.remove("visible");
      tipEl.setAttribute("aria-hidden", "true");
    }
  }

  // realizedInWindow counts goals achieved within the look-back window — achieved NOW,
  // carrying a real (non-seed) achieved_at inside the window. Returns null for "all"
  // (the point-in-time snapshot count is the honest total).
  function realizedInWindow(doc, w) {
    var ms = WINDOW_MS[w];
    if (!ms) return null;
    var cutoff = Date.now() - ms;
    var goals = Array.isArray(doc.goals) ? doc.goals : [];
    var n = 0;
    goals.forEach(function (g) {
      if (g.status_display !== "achieved" || !g.achieved_at || g.achieved_seed) return;
      var t = Date.parse(g.achieved_at);
      if (isFinite(t) && t >= cutoff) n++;
    });
    return n;
  }

  // ── #418: realized look-back slider (revived from #405 Inc 3 Item 6a, now LIVE) ────
  // A segment control above the situation tiles; picking a window re-renders the strip
  // so the Realized tile counts real done-history inside it.
  var sliderWired = false;
  function injectRealizedSlider() {
    if (sliderWired) return;
    var sit = q("goals-situation");
    if (!sit) return;
    sliderWired = true;
    var bar = document.createElement("div");
    bar.id = "goals-realized-slider";
    bar.className = "grealized-slider";
    bar.setAttribute("role", "group");
    bar.setAttribute("aria-label", "Realized look-back window");
    bar.innerHTML =
      '<span class="grealized-lab">Realized window</span>' +
      ["1d", "7d", "30d", "all"].map(function (w) {
        return '<button type="button" class="grealized-btn' +
          (w === realizedWindow ? " active" : "") +
          '" data-window="' + w + '" aria-pressed="' + (w === realizedWindow) + '">' +
          w + "</button>";
      }).join("");
    sit.parentNode.insertBefore(bar, sit);
    bar.addEventListener("click", function (e) {
      var btn = e.target.closest(".grealized-btn");
      if (!btn) return;
      var w = btn.getAttribute("data-window");
      if (!w || w === realizedWindow) return;
      realizedWindow = w;
      var btns = bar.querySelectorAll(".grealized-btn");
      for (var i = 0; i < btns.length; i++) {
        var match = btns[i].getAttribute("data-window") === w;
        btns[i].classList.toggle("active", match);
        btns[i].setAttribute("aria-pressed", String(match));
      }
      if (cache) renderSituation(cache); // the Realized tile re-counts for the new window
    });
    // A bounded window ages against the WALL CLOCK, not the data: with the doc unchanged
    // (refresh dedups on its signature), a goal falling out of the 1d/7d/30d window would
    // stay counted forever (cubic #449 P2). Re-count the strip once a minute while a
    // bounded window is active and the map is visible — cheap (six tiles), and "all"
    // never needs it (the snapshot count doesn't age).
    setInterval(function () {
      if (realizedWindow !== "all" && cache && isVisible()) renderSituation(cache);
    }, 60000);
  }

  /* ── situation strip + legend ──────────────────────────────────────────── */
  function renderSituation(doc) {
    var c = doc.counts || {};
    // Realized: the "all" window is the live point-in-time snapshot count; a bounded
    // window counts real recorded achievements inside it (#418 done-history).
    var winCount = realizedInWindow(doc, realizedWindow);
    var realizedV = winCount === null ? (c.realized || 0) : winCount;
    var realizedD = winCount === null ? "done & solidified" : "achieved in the last " + realizedWindow;
    // #451: the ONE decisions number — the same gatherDecisions population the reading
    // room lists — shown by the tile, the tab badge, and the page header alike. The old
    // badge counted gated NODES (server counts.awaiting) while the page listed decision
    // CARDS; 6 vs 3 on the same screen destroyed trust in the number.
    var decisions = decisionsCount(doc);
    var tiles = [
      // filter:"goal"|"inflight"|"pending"|"aspirational" → clicking highlights matching nodes.
      // "Awaiting you" and "Realized" have no node-state filter (awaiting opens the decision
      // page; realized is a done state, not a live-map filter with data coverage yet).
      { k: "Flotillas",   v: c.fleet || 0,       tone: "goal",        d: (c.total || 0) + " nodes total",     filter: "goal" },
      { k: "In flight",   v: c.in_flight || 0,   tone: "inflight",    d: "desks working now",                 filter: "inflight" },
      { k: "Awaiting you",v: decisions,          tone: "awaiting",    d: "your decisions & blocks" },
      // #405 Inc 3 (Q2): renamed Pending→Blocked, Aspirational→Planned.
      { k: "Blocked",     v: c.pending || 0,     tone: "pending",     d: "waiting on a dependency",           filter: "pending" },
      { k: "Realized",    v: realizedV,          tone: "realized",    d: realizedD },
      { k: "Planned",     v: c.aspirational || 0,tone: "aspirational",d: "not started",                       filter: "aspirational" },
    ];
    q("goals-situation").innerHTML = tiles.map(function (t) {
      var isDecisions = t.tone === "awaiting";
      var isFilter    = !!t.filter;
      var cls  = "gtile gtile-" + t.tone;
      var attrs = "";
      if (isDecisions) {
        cls  += " gtile-click";
        attrs = ' data-open-decisions role="button" tabindex="0" title="Open the decisions awaiting you"';
      } else if (isFilter) {
        cls  += " gtile-click";
        // The title is read by screen readers; also serves as the visible tooltip on hover.
        attrs = ' data-filter-tone="' + t.filter + '" role="button" tabindex="0"' +
          ' title="Highlight ' + escapeHtml(t.k) + ' goals on the map — click again or press Escape to clear"';
      }
      return '<div class="' + cls + '"' + attrs + ">" +
        '<div class="gtile-k">' + escapeHtml(t.k) + "</div>" +
        '<div class="gtile-v">' + escapeHtml(String(t.v)) + "</div>" +
        '<div class="gtile-d">' + escapeHtml(t.d) + "</div>" +
        "</div>";
    }).join("");
    // #429: the awaiting-count badge lives on the Decisions TAB (the reading room is a
    // first-class view, not a header-button modal). #451: it shows the SAME decisions
    // count as the tile and the page header — never the node-population count again.
    var hdrCount = q("hdr-decisions-count");
    var hdrBtn = q("tab-decisions");
    var awaiting = decisions;
    if (hdrCount && hdrBtn) {
      if (awaiting > 0) {
        hdrCount.textContent = String(awaiting);
        hdrCount.hidden = false;
        hdrBtn.classList.add("hdr-decisions-hot");
      } else {
        hdrCount.textContent = "";
        hdrCount.hidden = true;
        hdrBtn.classList.remove("hdr-decisions-hot");
      }
    }
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
  function updateLive(c, doc) {
    // "goal nodes" (not "fleet goals") — total counts all nodes, not just the fleet tier.
    // Announce pending too, so the spoken summary matches the visual situation strip — a
    // screen-reader user must hear about dependency-gated goals (cubic #359 P2).
    // #451: the awaiting clause speaks the SAME decisions count the tile/badge show —
    // a screen-reader user must never hear a different number than the sighted one sees.
    announce(decisionsCount(doc) + " awaiting you, " + (c.pending || 0) + " blocked on a dependency, " +
      (c.in_flight || 0) + " in flight, " +
      (c.realized || 0) + " realized, of " + (c.total || 0) + " goal nodes.");
  }

  // #349 Inc 5 F13: gather every realized goal into one "history of done" list. The live
  // map greens/ghosts realized nodes so they recede; this panel makes completed goals
  // legible in one place, with an honest empty state and a count. A row opens that goal's
  // detail drawer (wired in the init block).
  function renderDoneHistory(doc) {
    var list = q("goals-done-list"), countEl = q("goals-done-count");
    if (!list) return;
    if (countEl) countEl.textContent = "";
    // Distinguish an ERROR / no-goals-file state from a genuine empty: when the goals doc
    // could not be loaded (found === false), "No realized goals yet" would dress the error
    // as a clean empty (implying goals loaded and none are done). Show an honest unavailable
    // state instead — same discipline as the decision-brief modal's empty state (cubic #363).
    if (!doc.found) {
      list.innerHTML = '<div class="empty">Realized goals are unavailable — the goals map could not be loaded.</div>';
      return;
    }
    var goals = Array.isArray(doc.goals) ? doc.goals : [];
    var done = goals.filter(function (g) { return g.status_display === "achieved"; });
    if (countEl) countEl.textContent = done.length ? String(done.length) : "";
    if (!done.length) {
      list.innerHTML = '<div class="empty">No realized goals yet.</div>';
      return;
    }
    list.innerHTML = done.map(function (g) {
      // #418: show WHEN the goal was achieved where history recorded it. A seed stamp
      // (already achieved when history began) has no known achieve time — omit rather
      // than fabricate recency.
      var when = (g.achieved_at && !g.achieved_seed)
        ? '<span class="gdone-when">' + escapeHtml(relTime(g.achieved_at)) + "</span>" : "";
      return '<button class="gdone-row" type="button" data-open-node="' + escapeHtml(g.id) + '">' +
        '<span class="gdone-check" aria-hidden="true">✓</span>' +
        '<span class="gdone-title">' + escapeHtml(g.title || g.id) + "</span>" +
        when +
        '<span class="gdone-scope">' + escapeHtml(scopeNoun(g)) + "</span>" +
        "</button>";
    }).join("");
  }

  // relTime renders an ISO stamp as a coarse relative age ("14m ago" / "6h ago" / "3d ago").
  function relTime(iso) {
    var t = Date.parse(iso);
    if (!isFinite(t)) return "";
    var mins = Math.max(0, Math.floor((Date.now() - t) / 60000));
    if (mins < 60) return mins + "m ago";
    var hrs = Math.floor(mins / 60);
    if (hrs < 48) return hrs + "h ago";
    return Math.floor(hrs / 24) + "d ago";
  }

  function renderLegend() {
    var items = [
      ["realized", "realized"], ["in-flight", "in flight"],
      ["awaiting", "awaiting you"], ["pending", "waiting on a dependency"],
      ["aspirational", "planned"], ["dep", "depends on"],
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

  function workItem(wi, node) {
    var kind = escapeHtml(wi.kind || "");
    var label = escapeHtml(wi.label || wi.kind || "");
    var detail = wi.detail ? '<span class="gwi-detail">' + escapeHtml(wi.detail) + "</span>" : "";
    // #349 B5: the hover title carries a breadcrumb + status so a bare item ("TG3 prep
    // idle") is legible in context ("what does that even mean?").
    var tip = (node ? nodePath(node) + " · " : "") + (wi.label || wi.kind || "") + (wi.detail ? " · " + wi.detail : "");
    return '<span class="gwi gwi-' + escapeHtml(wi.class || "unknown") + '" title="' + escapeHtml(tip) + '">' +
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
    var items = (n.work_items || []).map(function (wi) { return workItem(wi, n); }).join("");
    var itemsBlock = items ? '<div class="gnode-items">' + items + "</div>" : "";
    var pill = '<span class="gpill gpill-' + escapeHtml(vis) + '">' + escapeHtml(STATE_LABEL[vis] || vis) + "</span>";
    // Per-node controls. #349 A2 SWAP: the node BODY now opens the detail drawer (the
    // primary "see the thing" action); the conversation jump is a distinct labelled
    // "→ desk" button (only on a routable node), synchronized with the drawer's own
    // open-conversation. A ⚠ Respond button on operator-gated nodes opens the
    // waiting-on-you modal. Positioned absolute so they never change card height (#283).
    var gated = vis === "awaiting" || vis === "blocked";
    var routable = !!convAgent(n);
    var agent = convAgent(n);
    var menu = "";
    if (gated || routable) {
      menu = '<div class="gnode-pop" role="menu" hidden>' +
        (routable ? '<button type="button" class="gnode-pop-item" role="menuitem" data-gnode-action="desk" data-gnode-agent="' + escapeHtml(agent) + '">Open conversation</button>' : "") +
        (gated ? '<button type="button" class="gnode-pop-item" role="menuitem" data-gnode-action="respond" data-gnode-id="' + escapeHtml(n.id) + '">Respond</button>' : "") +
        "</div>";
    }
    var controls = (gated || routable)
      ? ('<span class="gnode-ctl" data-gnode-id="' + escapeHtml(n.id) + '">' +
          '<button class="gnode-kebab" type="button" aria-haspopup="menu" aria-expanded="false" title="Actions" aria-label="Goal actions">&#8942;</button>' +
          menu + "</span>")
      : "";
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
  // sequenceOrder (F12): a STABLE topological sort of a sibling list by the authored `after`
  // sequence — a node is emitted only once every sibling it comes `after` (that is in this set)
  // has been emitted, otherwise siblings keep their original order. Cycle-safe: if no node is
  // ready (a cycle — the server validates acyclic, so this is a defensive fallback), emit the
  // earliest remaining to make progress. Used by the mind map so a limb reads as a roadmap.
  function sequenceOrder(nodes) {
    if (nodes.length < 2) return nodes.slice();
    var inSet = {};
    nodes.forEach(function (n) { inSet[n.id] = true; });
    var preds = {};
    nodes.forEach(function (n) {
      preds[n.id] = (n.after || []).filter(function (a) { return inSet[a]; });
    });
    var emitted = {}, out = [], remaining = nodes.slice();
    while (remaining.length) {
      var pick = -1;
      for (var i = 0; i < remaining.length; i++) {
        var ready = preds[remaining[i].id].every(function (p) { return emitted[p]; });
        if (ready) { pick = i; break; }
      }
      if (pick < 0) pick = 0; // cycle fallback — take the earliest remaining
      var node = remaining.splice(pick, 1)[0];
      emitted[node.id] = true;
      out.push(node);
    }
    return out;
  }
  function nodeCenter(n) { return { x: n._x + n._w / 2, y: n._y + heightOf(n) / 2 }; }

  // leafWeights returns a memoized, cycle-safe map id→subtree-leaf-count — a node's angular
  // DEMAND (its number of leaf descendants, min 1). Shared by BOTH radial layouts (org
  // pinwheel + the mind map) so the identical demand model isn't cloned (cubic #364 P2).
  function leafWeights(goals) {
    var leaves = {};
    function count(n, path) {
      if (leaves[n.id] != null) return leaves[n.id];
      if (path[n.id]) return 1; // cycle → treat as a leaf
      path[n.id] = true;
      var kids = childrenOf(n), c = 0;
      kids.forEach(function (k) { c += count(k, path); });
      path[n.id] = false;
      return (leaves[n.id] = Math.max(1, kids.length ? c : 1));
    }
    goals.forEach(function (n) { count(n, {}); });
    return leaves;
  }

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

    // 3. subtree leaf-weight = angular demand (memoized, cycle-safe; shared helper).
    var leaves = leafWeights(goals);

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

  // layoutMindmap (org v3 — the mind map): unlike layoutOrg's concentric rings anchored at
  // ONE center (which crams depth into a cramped pinwheel), each node's children fan out
  // LOCALLY from that node, in the node's own outward direction — so depth reads as branches
  // with sub-branches (limbs), not one ring. Ring-1 (the flotillas/roots) still splits the
  // full circle by leaf-weight; every deeper node fans its children within a CAPPED wedge
  // around the parent's outward heading, so a subtree stays a cohesive limb. Positions are the
  // same absolute _x/_y the tree/org layouts use, so nodeCard/pan-zoom/keyed-update are
  // unchanged; edges draw as organic curves (drawEdges mindmap branch). _dir carries a node's
  // outward heading so its own children continue the limb outward.
  // ── per-limb hue (mind-map only) ──────────────────────────────────────────────
  // Each top-level limb (a hub child / root and its whole subtree) gets a distinct hue so
  // the branches are visually traceable out from the hub — the canonical mind-map look. The
  // hue rides on the EDGES (the limbs themselves); node cards keep their STATUS colour, so
  // limb-identity and status are two independent, non-clashing signals. Empty for tree/org.
  var limbHueById = {};
  // limbRing1 mirrors layoutMindmap's ring-1 selection so a limb's hue matches its geometry.
  function limbRing1(goals, roots) {
    var center = null;
    for (var i = 0; i < goals.length; i++) {
      if (goals[i].layout && goals[i].layout.hub_center) { center = goals[i]; break; }
    }
    if (!center) return roots.slice();
    var ring1 = childrenOf(center);
    roots.forEach(function (r) { if (r !== center) ring1.push(r); });
    return ring1;
  }
  function computeLimbHues(goals, roots) {
    limbHueById = {};
    if (goalsLayout !== "mindmap") return;
    var limbs = sequenceOrder(limbRing1(goals, roots)); // hue follows the authored limb order (F12)
    var n = limbs.length || 1;
    limbs.forEach(function (root, i) {
      // evenly spaced around the wheel (+ a small offset off pure red); the golden-ish
      // spacing keeps adjacent limbs distinct even at high limb counts.
      var hue = Math.round((i * 360) / n + 18) % 360;
      (function paint(node, seen) {
        if (!node || seen[node.id]) return; // cycle-safe (server validates acyclic; be defensive)
        seen[node.id] = true;
        limbHueById[node.id] = hue;
        childrenOf(node).forEach(function (c) { paint(c, seen); });
      })(root, {});
    });
  }
  // limbStroke is the CSS colour for a node's limb, or "" when it has none (hub / non-mindmap).
  function limbStroke(id) {
    return (id in limbHueById) ? "hsl(" + limbHueById[id] + " 55% 62%)" : "";
  }

  function layoutMindmap(goals, roots) {
    var center = null;
    for (var i = 0; i < goals.length; i++) {
      if (goals[i].layout && goals[i].layout.hub_center) { center = goals[i]; break; }
    }
    var ring1;
    if (center) {
      ring1 = childrenOf(center);
      roots.forEach(function (r) { if (r !== center) ring1.push(r); });
    } else {
      ring1 = roots.slice();
    }
    // leaf-weight = angular demand (memoized, cycle-safe; shared helper — same as org).
    var leaves = leafWeights(goals);
    // per-node depth (for per-level segment length). Cycle-safe: a `seen` guard stops the
    // traversal from looping on a cyclic graph (the server validates acyclic, but this path
    // must not hang on bad/partial data — matching the other passes; cubic #364 P2).
    var nodeDepth = {}, seenDepth = {};
    (function setDepth(list, d) {
      list.forEach(function (n) {
        if (seenDepth[n.id]) return;
        seenDepth[n.id] = true;
        nodeDepth[n.id] = d;
        setDepth(childrenOf(n), d + 1);
      });
    })(ring1, 1);
    if (center) nodeDepth[center.id] = 0;

    function segLen(d) { return 116 + 20 * Math.min(d, 4); } // base branch length per level
    var GAP = 30; // tangential breathing room between adjacent sibling cards
    var placed = {};

    // place a node's children within the node's DISJOINT angular sector [a0,a1]: each child
    // gets a sub-sector proportional to its leaf-weight and is positioned PARENT-RELATIVE at
    // a segment length in the child's sub-sector midpoint. Disjoint sectors guarantee sibling
    // AND cousin subtrees never angularly overlap (each subtree stays inside its wedge); the
    // segment honours a circumference-minimum so a node's own children always arc-clear their
    // card widths at that radius (the org circMin, applied locally). This is what tunes the
    // limb geometry to hold at real fleet depth (19+ nodes, deep chains) without collisions.
    function place(n, a0, a1) {
      var kids = sequenceOrder(childrenOf(n)); // siblings in authored `after` order (F12)
      if (!kids.length) return;
      var pc = nodeCenter(n), sector = a1 - a0, d = (nodeDepth[n.id] || 0) + 1;
      var total = 0, need = 0;
      kids.forEach(function (k) { total += leaves[k.id]; need += k._w + GAP; });
      // radius: the larger of the base per-level length and the arc-fit radius (need/sector),
      // plus clearance from the parent card — so narrow sectors push their children outward.
      var seg = Math.max(segLen(d), sector > 0.02 ? need / sector : 0) + Math.max(n._w, heightOf(n)) / 2;
      var cursor = a0;
      kids.forEach(function (k) {
        var w = sector * (leaves[k.id] / (total || 1));
        var mid = kids.length === 1 ? (a0 + a1) / 2 : cursor + w / 2; // a lone child continues straight out
        k._x = pc.x + seg * Math.cos(mid) - k._w / 2;
        k._y = pc.y + seg * Math.sin(mid) - heightOf(k) / 2;
        placed[k.id] = true;
        place(k, cursor, cursor + w); // the child fans its own children within ITS sector
        cursor += w;
      });
    }

    // hub at the origin; ring-1 (flotillas/roots) splits the full circle by leaf-weight into
    // disjoint sectors — each becomes a limb; place() then grows each limb outward.
    if (center) { center._x = -center._w / 2; center._y = -heightOf(center) / 2; placed[center.id] = true; }
    var ordered1 = sequenceOrder(ring1), total1 = 0, need1 = 0; // top-level limbs in authored order (F12)
    ordered1.forEach(function (n) { total1 += leaves[n.id]; need1 += n._w + GAP; });
    var seg1 = Math.max(segLen(1), need1 / (2 * Math.PI)) + 40;
    var cur = -Math.PI / 2;
    ordered1.forEach(function (n) {
      var w = 2 * Math.PI * (leaves[n.id] / (total1 || 1)), mid = cur + w / 2;
      n._x = seg1 * Math.cos(mid) - n._w / 2;
      n._y = seg1 * Math.sin(mid) - heightOf(n) / 2;
      placed[n.id] = true;
      place(n, cur, cur + w);
      cur += w;
    });

    // Collision relaxation: disjoint sectors bound the ANGLE, but parent-relative placement
    // lets cousins in adjacent sectors drift close near the congested center. A few passes of
    // gentle axis-aligned separation nudge any residual overlapping pair apart along their
    // smaller-overlap axis — n is small (tens of nodes), so O(n²)×passes is cheap. The hub is
    // pinned (it anchors the map); everything else may shift a little to clear.
    var relaxIds = Object.keys(nodeById).filter(function (id) { return placed[id]; });
    for (var pass = 0; pass < 40; pass++) {
      var moved = false;
      for (var ri = 0; ri < relaxIds.length; ri++) {
        for (var rj = ri + 1; rj < relaxIds.length; rj++) {
          var A = nodeById[relaxIds[ri]], B = nodeById[relaxIds[rj]];
          var aw = A._w, ah = heightOf(A), bw = B._w, bh = heightOf(B);
          var dx = (B._x + bw / 2) - (A._x + aw / 2), dy = (B._y + bh / 2) - (A._y + ah / 2);
          var ox = (aw + bw) / 2 + 16 - Math.abs(dx), oy = (ah + bh) / 2 + 12 - Math.abs(dy);
          if (ox <= 0 || oy <= 0) continue; // no overlap (with margin)
          moved = true;
          var aPin = center && A === center, bPin = center && B === center;
          if (ox < oy) { // separate on x (the smaller penetration axis)
            var px = (dx < 0 ? -1 : 1) * ox;
            if (aPin) B._x += px; else if (bPin) A._x -= px; else { A._x -= px / 2; B._x += px / 2; }
          } else { // separate on y
            var py = (dy < 0 ? -1 : 1) * oy;
            if (aPin) B._y += py; else if (bPin) A._y -= py; else { A._y -= py / 2; B._y += py / 2; }
          }
        }
      }
      if (!moved) break;
    }

    // shift the placed cloud into positive world coords + size the world to its extent.
    var minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    Object.keys(nodeById).forEach(function (id) {
      if (!placed[id]) return;
      var n = nodeById[id];
      minX = Math.min(minX, n._x); minY = Math.min(minY, n._y);
      maxX = Math.max(maxX, n._x + n._w); maxY = Math.max(maxY, n._y + heightOf(n));
    });
    if (!isFinite(minX)) { minX = minY = 0; maxX = maxY = 100; }
    var worldPad = 90, shx = worldPad - minX, shy = worldPad - minY;
    Object.keys(nodeById).forEach(function (id) { if (!placed[id]) return; var n = nodeById[id]; n._x += shx; n._y += shy; });
    view.worldW = (maxX - minX) + 2 * worldPad;
    view.worldH = (maxY - minY) + 2 * worldPad;
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
      if (goalsLayout === "mindmap") {
        // organic limb: a gently-bowed cubic from parent centre to child centre, so a
        // branch reads as a curved connector rather than a straight spoke.
        var mpc = nodeCenter(parent), mcc = nodeCenter(child);
        var vx = mcc.x - mpc.x, vy = mcc.y - mpc.y, len = Math.hypot(vx, vy) || 1;
        var nx = -vy / len, ny = vx / len, bow = Math.min(38, len * 0.16);
        var mc1x = mpc.x + vx * 0.35 + nx * bow, mc1y = mpc.y + vy * 0.35 + ny * bow;
        var mc2x = mpc.x + vx * 0.65 + nx * bow, mc2y = mpc.y + vy * 0.65 + ny * bow;
        d = "M " + mpc.x + " " + mpc.y + " C " + mc1x + " " + mc1y + ", " + mc2x + " " + mc2y + ", " + mcc.x + " " + mcc.y;
        // per-limb hue: colour the branch by its limb so each subtree traces out from the hub
        // in one colour (status stays on the node cards). Only the mind map does this.
        var mh = limbStroke(id);
        if (mh) { paths.push('<path class="gedge gedge-limb" data-child="' + escapeHtml(id) + '" style="stroke:' + mh + '" d="' + d + '"/>'); return; }
      } else if (goalsLayout === "org") {
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
      if (isRadial()) {
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
  // node (or close it) WITHOUT pushing a new entry. Restore-after-load (cubic #351 P2):
  // a Back/Forward that lands on a node state while the goals data is still fetching would
  // find nodeById empty — so instead of bailing (closing the drawer), it QUEUES the target
  // and tryRestore() reapplies it the moment the render populates nodeById.
  var pendingRestore; // undefined = nothing queued; null = restore-to-closed; string = target node id
  function restoreNode(id) {
    pendingRestore = id || null;
    tryRestore();
  }
  function tryRestore() {
    if (pendingRestore === undefined) return;
    restoringNode = true;
    if (pendingRestore === null) { closeDrawer(); pendingRestore = undefined; }
    else if (nodeById[pendingRestore]) { openDrawer(pendingRestore); pendingRestore = undefined; }
    // else: the node isn't rendered yet — keep the target and retry after the next render.
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
  // ── GFM pipe-table helpers (#450 — ported from the parade renderer, parade.js/#428).
  // Pure row/delimiter parsing. renderBrief escapes the WHOLE brief before splitting,
  // but the structural chars ('|', '-', ':') survive escapeHtml — so detection runs on
  // the escaped lines and each cell's text is ALREADY escaped (inline() only, never a
  // second escape). A table is a header row IMMEDIATELY followed by a delimiter row, so
  // prose that merely contains a pipe is never mistaken for a table. ──
  function splitTableRow(raw) {
    var s = raw.trim().replace(/^\|/, "").replace(/\|$/, "");
    var cells = [], cur = "";
    for (var k = 0; k < s.length; k++) {
      if (s[k] === "\\" && s[k + 1] === "|") { cur += "|"; k++; } // \| is a literal pipe in a cell
      else if (s[k] === "|") { cells.push(cur); cur = ""; }
      else cur += s[k];
    }
    cells.push(cur);
    return cells.map(function (c) { return c.trim(); });
  }
  // isTableDelimiter: every cell is dashes with an optional leading/trailing alignment colon.
  function isTableDelimiter(raw) {
    if (raw.indexOf("|") === -1) return false;
    var cells = splitTableRow(raw);
    return cells.length > 0 && cells.every(function (c) { return /^:?-+:?$/.test(c); });
  }
  // tableAligns maps each delimiter cell to a text-align keyword, padded/truncated to n columns.
  function tableAligns(raw, n) {
    var cells = splitTableRow(raw), aligns = [];
    for (var k = 0; k < n; k++) {
      var c = cells[k] || "", l = c.charAt(0) === ":", r = c.charAt(c.length - 1) === ":";
      aligns.push(l && r ? "center" : r ? "right" : l ? "left" : "");
    }
    return aligns;
  }

  function renderBrief(md) {
    var lines = escapeHtml(String(md == null ? "" : md)).split(/\r?\n/);
    // inline: **bold**, `code`, and [text](https://…) reference links (#405 Inc 2 — "references
    // littered throughout"). The URL is restricted to http(s) so nothing script-like reaches href;
    // the text was escaped upfront, and entity-encoded URLs decode correctly inside the attribute.
    function inline(s) {
      return s
        .replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>')
        .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
        .replace(/`([^`]+)`/g, "<code>$1</code>");
    }
    // renderTable emits a brief's table from a header row + body rows + per-column
    // alignments (#450 — the parade renderer's shape, #428). Cell text arrives ALREADY
    // escaped (the whole brief was escaped upfront); inline() layers markdown on top.
    // GFM: body rows shorter than the header pad with empty cells; excess is ignored.
    function renderTable(head, rows, aligns) {
      function cellHtml(tag, text, k) {
        var a = aligns[k] ? ' style="text-align:' + aligns[k] + '"' : ""; // aligns ∈ {left,center,right} — a fixed, non-injectable set
        return "<" + tag + a + ">" + inline(text) + "</" + tag + ">";
      }
      function rowHtml(cells, tag) {
        var cs = [];
        for (var k = 0; k < head.length; k++) cs.push(cellHtml(tag, cells[k] || "", k));
        return "<tr>" + cs.join("") + "</tr>";
      }
      var thead = "<thead>" + rowHtml(head, "th") + "</thead>";
      var tbody = rows.length ? "<tbody>" + rows.map(function (r) { return rowHtml(r, "td"); }).join("") + "</tbody>" : "";
      return '<table class="gm-table">' + thead + tbody + "</table>";
    }
    var out = [], list = null;
    function flush() { if (list) { out.push("<ul>" + list.join("") + "</ul>"); list = null; } }
    for (var i = 0; i < lines.length; i++) {
      var ln = lines[i];
      // GFM pipe-table (#450): a header row with a '|' IMMEDIATELY followed by a delimiter
      // row; the block runs until a blank or pipe-less line. Decision briefs carry cost
      // tables and tradeoff matrices — raw pipes defeat the operator's reading surface.
      if (ln.indexOf("|") !== -1 && i + 1 < lines.length && isTableDelimiter(lines[i + 1])) {
        flush();
        var headCells = splitTableRow(ln);
        var aligns = tableAligns(lines[i + 1], headCells.length);
        var bodyRows = [];
        var j = i + 2;
        for (; j < lines.length && lines[j].trim() !== "" && lines[j].indexOf("|") !== -1; j++) {
          bodyRows.push(splitTableRow(lines[j]));
        }
        out.push(renderTable(headCells, bodyRows, aligns));
        i = j - 1; // the for-loop's i++ advances past the last consumed row
        continue;
      }
      // ![alt](https://…) — an illustrative demo image (#405 Inc 2, "when available").
      var img = /^!\[([^\]]*)\]\((https?:\/\/[^\s)]+)\)\s*$/.exec(ln);
      var h = /^(#{1,3})\s+(.*)$/.exec(ln), li = /^\s*[-*]\s+(.*)$/.exec(ln);
      if (img) { flush(); out.push('<img class="gm-brief-img" src="' + img[2] + '" alt="' + img[1] + '" loading="lazy">'); }
      else if (h) { flush(); out.push('<div class="gm-brief-h">' + inline(h[2]) + "</div>"); }
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

  // nodePath is the ancestor breadcrumb for a node ("hub › flotilla › desk › task") — a
  // hover summary so a bare item (e.g. "TG3 prep idle") is legible in context (#349 B5).
  function nodePath(n) {
    var out = [], cur = n, guard = 0;
    while (cur && guard++ < 20) { out.unshift(cur.title || cur.id); cur = cur.parent ? nodeById[cur.parent] : null; }
    return out.join(" › ");
  }
  // downstreamGated collects the operator-gated work items from a node's DESCENDANTS (not
  // itself). For an aggregate node whose ⚠ is a roll-up, the decisions live downstream —
  // so the modal routes THERE instead of "nothing gated here, so why ask?" (#349 B6).
  function downstreamGated(n, seen) {
    seen = seen || {};
    var out = [];
    (n.children || []).forEach(function (cid) {
      if (seen[cid]) return;
      seen[cid] = true;
      var c = nodeById[cid];
      if (!c) return;
      (c.work_items || []).forEach(function (wi) {
        if (wi.class === "awaiting" || wi.class === "blocked") out.push({ node: c, wi: wi });
      });
      downstreamGated(c, seen).forEach(function (x) { out.push(x); });
    });
    return out;
  }
  // itemLinkAttrs marks a gated item CLICK-THROUGH to its target (#349 B4/B5): a desk item →
  // its Conversations thread; anything else stays a plain (non-routing) row. The breadcrumb
  // rides along as the title tooltip.
  function itemLinkAttrs(wi, node) {
    var t = ' title="' + escapeHtml(nodePath(node) + (wi.detail ? " · " + wi.detail : "")) + '"';
    if (wi.kind === "desk" && wi.agent) return ' data-goto-desk="' + escapeHtml(wi.agent) + '"' + t;
    return t;
  }
  // sameBrief compares two briefs by trimmed content — a gated item whose brief is
  // identical to the node-level brief already shown above must NOT re-render it (the
  // modal was printing the same six-element decision package twice; Wave 4 readability).
  function sameBrief(a, b) { return hasBrief(a) && hasBrief(b) && String(a).trim() === String(b).trim(); }
  function gatedRow(node, wi, nodeBrief) {
    var link = wi.kind === "desk" && wi.agent; // a routable item is a button; others are plain
    var head = "<" + (link ? "button type=\"button\"" : "div") + ' class="gm-gated-item' + (link ? " gm-item-link" : "") + '"' + itemLinkAttrs(wi, node) + ">" +
      escapeHtml(wi.label || wi.kind || "") + (wi.detail ? ' <span class="muted">— ' + escapeHtml(wi.detail) + "</span>" : "") +
      "</" + (link ? "button" : "div") + ">";
    // Skip the brief body when it duplicates the node brief printed above; otherwise show
    // the item's own brief (or the honest no-brief note).
    var body = sameBrief(wi.brief, nodeBrief) ? ""
      : (hasBrief(wi.brief) ? '<div class="gm-brief-full">' + renderBrief(wi.brief) + "</div>" : BRIEF_EMPTY);
    return '<div class="gm-gated-row">' + head + body + "</div>";
  }

  function openModal(id) {
    var n = nodeById[id];
    if (!n) return;
    modalNodeId = id; // #501: the send targets the node ON SCREEN (updates on drill-ins)
    var gated = (n.work_items || []).filter(function (wi) { return wi.class === "awaiting" || wi.class === "blocked"; });
    var parts = ['<p class="gm-scope">' + escapeHtml(scopeNoun(n)) + "</p>"];
    if (n.description) parts.push("<p>" + escapeHtml(n.description) + "</p>");
    // A node-level decision package renders in full (the decision is on the node itself).
    if (hasBrief(n.brief)) parts.push('<div class="gm-brief-full">' + renderBrief(n.brief) + "</div>");
    if (gated.length) {
      // Each gated item clicks through to its target + shows its decision package (#347/B4).
      parts.push('<div class="gm-gated"><div class="gm-gated-lab">Waiting on you</div>' +
        gated.map(function (wi) { return gatedRow(n, wi, n.brief); }).join("") + "</div>");
    } else {
      // B6: no DIRECT gate — surface the DOWNSTREAM decisions this node rolls up, each
      // routing into the descendant that actually owns it.
      var down = downstreamGated(n);
      if (down.length) {
        parts.push('<div class="gm-gated"><div class="gm-gated-lab">Downstream decisions</div>' +
          '<p class="gm-down-note muted">Nothing is gated on this node itself — these are waiting under it:</p>' +
          down.map(function (x) {
            return '<div class="gm-gated-row"><button type="button" class="gm-gated-item gm-item-link" data-open-node="' + escapeHtml(x.node.id) + '" title="' + escapeHtml(nodePath(x.node)) + '">' +
              escapeHtml(x.node.title || x.node.id) + ' <span class="muted">→ ' + escapeHtml(x.wi.label || x.wi.detail || x.wi.kind || "") + "</span></button></div>";
          }).join("") + "</div>");
      } else if (!hasBrief(n.brief)) {
        parts.push('<p class="muted">Nothing is gated on you here.</p>');
      }
    }
    q("goals-modal-title").textContent = n.title || n.id;
    q("goals-modal-brief").innerHTML = parts.join("");
    var ta = q("goals-modal-input");
    if (ta) ta.value = "";
    var note = q("goals-modal-note");
    if (note) note.textContent = ""; // clear the stub "not sent" note from a prior open
    var m = q("goals-modal");
    var wasOpen = m.classList.contains("open");
    m.classList.add("open");
    m.setAttribute("aria-hidden", "false");
    // Anchor focus-restore ONLY on the first open (from closed). A drill-in re-render (B6
    // "Downstream decisions" → openModal(descendant)) replaces the in-modal link that was
    // just clicked, so capturing document.activeElement here would leave close() focusing a
    // DETACHED element; and the opener node id is re-queried LIVE on close so an SSE map
    // re-render that rebuilt the card still restores focus (cubic #354 P2). Keep the ORIGINAL
    // external trigger across drill-ins.
    if (!wasOpen) {
      modalReturnId = id;                   // the node whose ⚠/pill opened this — re-queried on close
      modalReturn = document.activeElement; // fallback for a non-node trigger
    }
    if (ta) ta.focus();
  }
  function closeModal() {
    var m = q("goals-modal");
    if (m) { m.classList.remove("open"); m.setAttribute("aria-hidden", "true"); }
    // Re-query the opening node's trigger LIVE — it may have been replaced by a drill-in
    // re-render or an SSE map update since open — then fall back to the captured element.
    var target = null;
    if (modalReturnId) {
      var card = cardEl(modalReturnId);
      if (card) target = card.querySelector(".gnode-kebab") || card.querySelector(".gpill") || card;
    }
    if (!target && modalReturn && modalReturn.focus && document.contains(modalReturn)) target = modalReturn;
    if (target && target.focus) target.focus({ preventScroll: true });
    modalReturn = null;
    modalReturnId = null;
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
    // #405 Inc 3 Item 5: re-apply the stat-cell highlight after every render so SSE
    // ticks (which replace or update node articles) don't wipe the active filter.
    if (activeCellTone) applyFilter(activeCellTone);
  }

  // ── #405 Inc 2: the decision page (the operator's centerpiece) ────────────────────
  // A full reading room for EVERY open decision. The canonical 6-element briefs are the data;
  // this page formats them well: each decision shows which goal it drives (Context, linked into
  // the map), then the brief — background / value / mechanics / options+tradeoffs / recommendation
  // / reversibility — with references (links) and demo images rendered inline via renderBrief.
  // Opened from the "Awaiting you" situation tile.
  // gatherDecisions collects the decision population from an index (default: the module
  // nodeById). The optional index keeps the count PURE for callers that hold a doc the
  // module hasn't indexed yet (#451: renderSituation runs before render() rebuilds
  // nodeById, so counting the module state would count the PREVIOUS doc).
  // #501: gatherDecisions returns TWO buckets from one walk. `decisions` are COMPLETE —
  // a brief is present — and are the only items that render as answerable decisions
  // ("if you have a decision for me, it comes with a brief" — mechanical, not remembered).
  // `preparing` is the fail-closed bucket: operator-gated items whose brief is missing
  // render as "brief being prepared" — visible (nothing the operator is owed can hide,
  // the #451 lesson) but NEVER as a decision (the #501 lesson: a brief-less card asking
  // the operator to chase the desk is a walk failure — the watch daemon already chases
  // the owning desk mechanically, #349 item D).
  function gatherDecisions(index) {
    var byId = index || nodeById;
    var out = [], preparing = [];
    Object.keys(byId).forEach(function (id) {
      var n = byId[id];
      if (!n) return;
      // A node-level brief is a decision ONLY when the node itself is operator-gated
      // (awaiting/blocked). Without this, a node carrying a brief in any other state would
      // pollute the decision room with a non-decision — the exact anti-pattern this page kills.
      var vis = visToken(n);
      var gated = vis === "awaiting" || vis === "blocked";
      var before = out.length + preparing.length;
      if (gated && hasBrief(n.brief)) out.push({ node: n, label: "", brief: n.brief });
      (n.work_items || []).forEach(function (wi) {
        if ((wi.class === "awaiting" || wi.class === "blocked") && !sameBrief(wi.brief, n.brief)) {
          if (hasBrief(wi.brief)) out.push({ node: n, label: wi.label || wi.detail || wi.kind || "", brief: wi.brief });
          else preparing.push({ node: n, label: wi.label || wi.detail || wi.kind || "" });
        }
      });
      // #451: a gated node that produced NOTHING above is still real — the operator must
      // see it. #501 refines WHERE: with no brief it is being prepared, not decidable.
      if (gated && out.length + preparing.length === before) preparing.push({ node: n, label: "" });
    });
    return { decisions: out, preparing: preparing };
  }
  // decisionsCount is THE number every decisions surface shows — the tab badge, the
  // "Awaiting you" tile, the reading-room header, and the cards themselves all count
  // the same population (#451: two count sources may never disagree). #501 narrows the
  // population to COMPLETE decisions — the preparing bucket has its own labeled count
  // on the page (visible, but not something the operator can act on yet). Pure over
  // the doc it is handed — no module-index side effects, no stale-index hazards.
  function decisionsCount(doc) {
    if (!doc || !Array.isArray(doc.goals)) return 0;
    var byId = {};
    doc.goals.forEach(function (n) { if (n && n.id) byId[n.id] = n; });
    return gatherDecisions(byId).decisions.length;
  }
  // respondTarget resolves the desk an operator response routes to: the node's
  // conversation agent, else its owner, else the fleet coordinator (body data-xo).
  function respondTarget(n) {
    return (n.conversation_agent || n.owner || document.body.getAttribute("data-xo") || "").trim();
  }
  function renderDecisionCard(dec, idx, total) {
    var n = dec.node;
    var titleItem = dec.label ? ' <span class="gdec-item muted">— ' + escapeHtml(dec.label) + "</span>" : "";
    // #501: only COMPLETE briefs reach this renderer (gatherDecisions fails closed), so
    // there is no placeholder branch — a brief-less card here would be a bug, not a state.
    var target = respondTarget(n);
    return '<article class="gdec-card">' +
      '<div class="gdec-eyebrow">Decision ' + (idx + 1) + " of " + total + " · " + escapeHtml(scopeNoun(n)) + "</div>" +
      '<h3 class="gdec-card-title">' + escapeHtml(n.title || n.id) + titleItem + "</h3>" +
      '<div class="gdec-ctx"><span class="gdec-ctx-lab">Drives</span> ' +
        '<button type="button" class="gdec-ctx-link" data-gdec-goto="' + escapeHtml(n.id) + '" title="Open this goal in the map">' + escapeHtml(nodePath(n)) + "</button></div>" +
      '<div class="gdec-brief gm-brief-full">' + renderBrief(dec.brief) + "</div>" +
      // #501: the response affordance — every rendered decision is answerable INLINE.
      // The reply posts to /api/control/respond (confirmed delivery, durable-outbox
      // fallback); the outcome line reports delivered/queued honestly, never a stub.
      '<div class="gdec-respond" data-resp-target="' + escapeHtml(target) + '" data-resp-goal="' + escapeHtml(n.id) + '" data-resp-item="' + escapeHtml(dec.label || "") + '">' +
        '<textarea class="gdec-resp-input" rows="2" placeholder="Respond to ' + escapeHtml(target || "the fleet") + '&#8230; (approve / question / answer)" aria-label="Respond to this decision"></textarea>' +
        '<div class="gdec-resp-row">' +
          '<span class="gdec-resp-msg" role="status" aria-live="polite"></span>' +
          '<button type="button" class="btn btn-primary gdec-resp-send">Respond</button>' +
        "</div>" +
      "</div>" +
      "</article>";
  }
  // renderPreparingRow is the fail-closed face of a brief-less gated item (#501): the
  // operator sees WHAT is coming and WHO owes the brief — and nothing to answer yet.
  function renderPreparingRow(p) {
    var n = p.node;
    var item = p.label ? ' <span class="gdec-item muted">— ' + escapeHtml(p.label) + "</span>" : "";
    var owner = respondTarget(n);
    return '<div class="gdec-prep-row">' +
      '<span class="gdec-prep-spin" aria-hidden="true"></span>' +
      '<span class="gdec-prep-title">' + escapeHtml(n.title || n.id) + item + "</span>" +
      '<span class="gdec-prep-owner">' + escapeHtml(owner ? "brief being prepared by " + owner : "brief being prepared") + "</span>" +
      "</div>";
  }
  function indexGoalsNodes(doc) {
    nodeById = {};
    (Array.isArray(doc.goals) ? doc.goals : []).forEach(function (n) { nodeById[n.id] = n; });
    // A flat re-index carries no laid-out geometry (_x/_y). Invalidate the map's render
    // signatures so its next render takes the FULL layout path — an in-place update over
    // bare nodes would compute NaN positions.
    laidOut = false; lastStructSig = null; lastSig = null;
  }
  // #429: the reading room is a first-class TAB, not a modal.
  function decisionsVisible() {
    var v = q("view-decisions");
    return v && !v.classList.contains("hidden");
  }
  // paintDecisions renders the page from the current cache/nodeById. Honest states (the
  // done-history discipline, cubic #363): a doc that failed to load must NOT masquerade
  // as a clean "nothing awaiting you" — the error is the finding.
  function paintDecisions() {
    var list = q("gdec-list");
    if (!list) return;
    var titleEl = q("gdec-title");
    if (titleEl) titleEl.textContent = "Decisions awaiting you";
    if (!cache) { list.innerHTML = '<div class="gdec-empty">Loading decisions…</div>'; return; }
    if (cache.found === false) {
      list.innerHTML = '<div class="gdec-empty">The fleet goals data is unavailable right now, so decisions can&#8217;t be listed. This page reloads on the next visit or live update.</div>';
      return;
    }
    // The load-time badge prime sets cache without indexing; index on demand so the
    // first paint reads the real doc instead of an empty map.
    if (!Object.keys(nodeById).length) indexGoalsNodes(cache);
    var g = gatherDecisions();
    var html = g.decisions.length
      ? g.decisions.map(function (d, i) { return renderDecisionCard(d, i, g.decisions.length); }).join("")
      : '<div class="gdec-empty">Nothing is awaiting your decision right now.</div>';
    // #501 fail-closed bucket: brief-less gated items are VISIBLE (coming work is never
    // hidden) but distinct — nothing to answer until the brief arrives; the watch daemon
    // is already chasing the owning desk for it (#349 item D).
    if (g.preparing.length) {
      html += '<section class="gdec-prep" aria-label="Briefs being prepared">' +
        '<h3 class="gdec-prep-h">Briefs being prepared · ' + g.preparing.length + "</h3>" +
        '<p class="gdec-prep-d muted">These items are waiting on you but arrived without a decision brief — the fleet daemon has asked the owning desks to author them; each appears above, answerable, the moment its brief is complete.</p>' +
        g.preparing.map(renderPreparingRow).join("") +
        "</section>";
    }
    list.innerHTML = html;
    // The count is ALWAYS stated on a loaded doc — "· 0" with a preparing bucket present
    // would otherwise read as "the header forgot", not "nothing to decide" (OCR #505).
    if (titleEl) titleEl.textContent = "Decisions awaiting you · " + g.decisions.length;
    lastDecsSig = JSON.stringify(cache);
  }
  // lastDecsSig dedups live-tick repaints of the decisions page (the map's lastSig is
  // reset by every flat re-index, so it can't serve): an unchanged doc must not churn
  // the list's DOM (and blow away focus) on every poll.
  var lastDecsSig = null;
  // openDecisions runs on every Decisions-tab open (dash.js showView): paint instantly
  // from the last-known doc, then ALWAYS refetch — a standalone tab must not fossilize
  // its first-load list (the old modal only fetched when the map had never rendered).
  // The shared epoch orders this fetch against refresh() so a stale response never
  // overwrites a newer doc.
  function openDecisions() {
    paintDecisions();
    var e = ++epoch;
    getJSON("/api/goals").then(function (doc) {
      if (e !== epoch) return;
      cache = doc;
      indexGoalsNodes(doc);
      renderSituation(doc);
      if (decisionsVisible()) paintDecisions();
    }).catch(function () {
      if (e !== epoch) return;
      // Keep a last-known-good list; only surface the unavailable state when empty-handed.
      if (!Object.keys(nodeById).length && decisionsVisible()) {
        cache = cache && cache.found !== false ? cache : { found: false };
        paintDecisions();
      }
    });
  }

  function closeAllGnodeMenus() {
    document.querySelectorAll(".gnode-pop").forEach(function (p) { p.hidden = true; });
    document.querySelectorAll(".gnode-kebab").forEach(function (b) { b.setAttribute("aria-expanded", "false"); });
    // #503: undo the overflow/z-index release (see dash.css) once every menu is shut.
    document.querySelectorAll(".gnode.gnode-menu-open").forEach(function (g) { g.classList.remove("gnode-menu-open"); });
  }

  function wireNodes() {
    if (nodesWired) return;
    var nodesEl = q("goals-nodes");
    if (!nodesEl) return;
    nodesWired = true;
    nodesEl.addEventListener("click", function (e) {
      var popItem = e.target.closest("[data-gnode-action]");
      if (popItem) {
        e.stopPropagation();
        closeAllGnodeMenus();
        var action = popItem.getAttribute("data-gnode-action");
        if (action === "desk") {
          var agent = popItem.getAttribute("data-gnode-agent");
          if (agent && D.openConversation) D.openConversation(agent);
        } else if (action === "respond") {
          openModal(popItem.getAttribute("data-gnode-id"));
        }
        return;
      }
      var kebab = e.target.closest(".gnode-kebab");
      if (kebab) {
        e.stopPropagation();
        var ctl = kebab.closest(".gnode-ctl");
        var pop = ctl && ctl.querySelector(".gnode-pop");
        var willOpen = pop && pop.hidden;
        closeAllGnodeMenus();
        if (willOpen && pop) {
          pop.hidden = false;
          kebab.setAttribute("aria-expanded", "true");
          var openCard = kebab.closest(".gnode");
          if (openCard) openCard.classList.add("gnode-menu-open");
        }
        return;
      }
      closeAllGnodeMenus();
      // #349 B3: clicking the STATUS PILL of a gated node opens the list of blockers (the
      // respond modal) — "go through and clean those up." A non-gated pill falls through to
      // the node body's detail-drawer action.
      var pill = e.target.closest(".gpill");
      if (pill) {
        var pn = pill.closest(".gnode"), pnode = pn ? nodeById[pn.getAttribute("data-id")] : null;
        var pv = pnode ? visToken(pnode) : "";
        if (pv === "awaiting" || pv === "blocked") { openModal(pn.getAttribute("data-id")); return; }
      }
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
    // #405 Inc 3 Item 6b: track mouse position so the tooltip follows the cursor.
    var tipX = 0, tipY = 0;
    nodesEl.addEventListener("mousemove", function (e) {
      tipX = e.clientX; tipY = e.clientY;
      // Reposition the tip if it is already visible (cursor moved within the card).
      if (hoveredId && nodeById[hoveredId]) showTip(nodeById[hoveredId], tipX, tipY);
    });
    nodesEl.addEventListener("mouseover", function (e) {
      var card = e.target.closest(".gnode");
      if (!card) return;
      var id = card.getAttribute("data-id");
      if (id === hoveredId) return; // still within the same card (delegation fires on inner spans too)
      hoveredId = id;
      highlightChain(id, true);
      lightDeps(id, true);
      // #405 Inc 3 Item 6b: show the richer tooltip for this node. Seed the position
      // from THIS event's cursor coords — on the first hover, mousemove has not yet
      // fired, so tipX/tipY are still stale (0,0) and the tip would flash top-left
      // (cubic #405 P3). Using e.clientX/Y places it at the cursor immediately.
      tipX = e.clientX; tipY = e.clientY;
      if (nodeById[id]) showTip(nodeById[id], tipX, tipY);
    });
    nodesEl.addEventListener("mouseout", function (e) {
      var card = e.target.closest(".gnode");
      if (!card) return;
      if (e.relatedTarget && card.contains(e.relatedTarget)) return; // moving within the same card
      var id = card.getAttribute("data-id");
      highlightChain(id, false);
      lightDeps(id, false);
      if (hoveredId === id) hoveredId = null;
      hideTip(); // #405 Inc 3 Item 6b
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
    // #349 Inc 5 F13: a history-of-done row opens that realized goal's detail drawer.
    var doneList = q("goals-done-list");
    if (doneList) doneList.addEventListener("click", function (e) {
      var row = e.target.closest ? e.target.closest("[data-open-node]") : null;
      if (row) openDrawer(row.getAttribute("data-open-node"));
    });
    // Intervention modal (#302): close on the × / backdrop; the "Send" is a stub for
    // this prototype (the reply path wires to the control API in a follow-on).
    var modal = q("goals-modal");
    if (modal) modal.addEventListener("click", function (e) {
      if (e.target.closest(".gm-close") || e.target.classList.contains("goals-modal")) { closeModal(); return; }
      // #349 B4/B6: a gated item clicks through to its target — a desk item jumps to its
      // Conversations thread; a downstream item drills into the descendant that owns it.
      var link = e.target.closest(".gm-item-link");
      if (link) {
        var desk = link.getAttribute("data-goto-desk"), node = link.getAttribute("data-open-node");
        if (desk && window.flotillaDash && window.flotillaDash.openConversation) { closeModal(); window.flotillaDash.openConversation(desk); }
        else if (node) { openModal(node); } // re-render the modal on the descendant's decision
      }
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
    // #501: the modal's Send is the REAL reply path — the same /api/control/respond leg
    // the decision cards use (ONE shared runner; confirmed delivery + durable-outbox
    // fallback). The stub ("not sent — wiring is a follow-on") is gone.
    var send = q("goals-modal-send");
    if (send) send.onclick = function () {
      var note = q("goals-modal-note");
      var n = modalNodeId ? nodeById[modalNodeId] : null;
      if (!n) { if (note) note.textContent = "No goal is open — close and reopen this dialog."; return; }
      runRespond(send, note, q("goals-modal-input"), respondTarget(n), n.id, "");
    };
    // Situation strip: "Awaiting you" opens the decision page; filter tiles highlight
    // their matching nodes. Re-clicking an active filter tile clears it (toggle).
    var sit = q("goals-situation");
    if (sit) {
      sit.addEventListener("click", function (e) {
        // [data-open-decisions] is owned by the single document-level handler in dash.js
        // (it opens Decisions from ANY view, including this tile) — handling it here too
        // double-fired openDecisions and could clobber decision state (cubic #421 P2).
        // #405 Inc 3 Item 5: stat-cell filter.
        var tile = e.target.closest("[data-filter-tone]");
        if (tile) {
          var tone = tile.getAttribute("data-filter-tone");
          if (activeCellTone === tone) clearFilter(); // toggle off on re-click
          else applyFilter(tone);
        }
      });
      sit.addEventListener("keydown", function (e) {
        if (e.key !== "Enter" && e.key !== " ") return;
        // [data-open-decisions] keyboard activation is owned by dash.js's document-level
        // keydown handler (single owner — see the click handler above; cubic #421 P2).
        // #405 Inc 3 Item 5: keyboard activation for filter tiles.
        var tile = e.target.closest("[data-filter-tone]");
        if (tile) {
          e.preventDefault();
          var tone = tile.getAttribute("data-filter-tone");
          if (activeCellTone === tone) clearFilter();
          else applyFilter(tone);
        }
      });
    }
    document.addEventListener("keydown", function (e) {
      if (e.key !== "Escape" || !isVisible()) return;
      if (help) help.setAttribute("aria-expanded", "false"); // Esc dismisses the tooltip too
      if (q("goals-modal") && q("goals-modal").classList.contains("open")) { closeModal(); return; }
      // #405 Inc 3 Item 5: Escape clears the stat-cell highlight before closing the drawer.
      if (activeCellTone) { clearFilter(); return; }
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
    renderDoneHistory(doc); // #349 Inc 5 F13 — realized goals, on every path (empty-safe)

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
    updateLive(doc.counts || {}, doc); // announce the situation summary — success path only (see renderSituation)
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
        // brief is deliberately NOT structural (a brief edit must not re-layout the map),
        // so it must be copied here like the live-status fields — otherwise the module
        // index holds a stale brief and every nodeById reader (gatherDecisions' instant
        // paint, the respond modal, the drawer) transiently splits/merges a decision
        // until the next full re-index (#458 gate-review P3).
        if (prev) { prev.status_display = n.status_display; prev.work_items = n.work_items; prev.brief = n.brief; }
      });
      updateInPlace(goals, nodesEl);
      drawEdges(); // child state may have changed → recolour (the SVG is stateless)
      reapplyTransient(); // re-light hover chain + refresh the open drawer's live status
      tryRestore(); // a queued Back/Forward drawer target may now be renderable (#351 P2)
      lastSig = sig;
      return;
    }

    // Structural change ⇒ full rebuild + re-layout. Preserve keyboard focus across
    // the article replacement.
    laidOut = false;
    var keepFocus = focusedNodeId();
    buildNodeIndex(goals);
    var roots = goals.filter(function (n) { return !n.parent || !nodeById[n.parent]; });
    computeLimbHues(goals, roots); // per-limb hue for the mind map (no-op for tree/org)
    var maxDepth = 0;
    goals.forEach(function (n) { maxDepth = Math.max(maxDepth, depthOf(n)); });

    // Pass 1: render at column x with provisional y=0 so heights can be measured.
    goals.forEach(function (n) { n._y = 0; });
    nodesEl.innerHTML = goals.map(nodeCard).join("");
    // Tier column headers belong to the tree layout only — the radial layouts have no columns.
    if (isRadial()) q("goals-tierlabels").innerHTML = "";
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
      if (goalsLayout === "mindmap") layoutMindmap(goals, roots);
      else if (goalsLayout === "org") layoutOrg(goals, roots);
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
      if (!view.fitted) { (isRadial() ? fitOverview : fit)(); view.fitted = true; }
      applyTransform();
      restoreFocus(keepFocus);
      reapplyTransient(); // re-select the drawer's node (articles were replaced) + re-light hover
      lastStructSig = ssig; // commit ONLY after a complete pass-2 render
      laidOut = true;
      lastSig = sig;
      tryRestore(); // nodeById is now populated — apply a queued Back/Forward drawer target (#351 P2)
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
    // #429: a live tick must also reach the Decisions tab — it reads the same doc, and
    // it can be the operator's ONLY open view (the goals map never activated).
    if (!activated && !decisionsVisible()) return Promise.resolve();
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
      // #429: the operator is reading the decisions page — re-index + repaint it (own
      // dedup: the flat re-index resets lastSig, so sig can't dedup this branch).
      else if (decisionsVisible() && sig !== lastDecsSig) {
        indexGoalsNodes(doc);
        renderSituation(doc);
        paintDecisions();
      }
      // Hidden: do not render and do not commit lastSig — show() renders on tab open.
    }).catch(function (err) {
      if (e !== epoch) return;
      cache = { found: false, error: err.message };
      if (isVisible()) render(); // render() computes + commits the error-state sig
      else if (decisionsVisible()) paintDecisions(); // honest unavailable state (#429)
    });
  }

  // (The tree/mind-map layout picker and its two wiring functions were removed 2026-07-06:
  // the map renders as a mind map only, so there is nothing to switch between.)

  function show() {
    activated = true;
    setupPanZoom();
    wireNodes();
    injectRealizedSlider(); // #418: revive the look-back control (now live data)
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
      (isRadial() ? fitOverview : fit)();
      applyTransform();
    }, 120);
  });

  // #429: a decision card's "Drives" link jumps into the Goals map and opens that goal's
  // drawer. Wired ONCE at load (the list container is static chrome in index.html) — the
  // Decisions tab can be opened before the Goals tab has ever rendered, so the drawer-open
  // goes through restoreNode, which QUEUES the target until a render populates the map
  // (the same deferred path Back/Forward uses — cubic #351 P2).
  // #501: ONE reply path for both response surfaces (the decision cards and the map's
  // respond modal): POST /api/control/respond — the confirmed-delivery route with a
  // durable-outbox fallback. Resolves to an honest outcome line for the UI; rejects
  // with the server's error text (surfaced verbatim, never swallowed).
  // formatRespondOutcome is pure (#509): card Send and modal Send share sendDecisionResponse,
  // so identical server payloads produce identical operator-facing strings.
  function formatRespondOutcome(res) {
    if (res && res.outcome === "delivered") return "Delivered to " + res.target + " — turn confirmed.";
    if (res && res.outcome === "queued") {
      return "Queued durably for " + res.target + " (id " + res.queued_id + ") — the fleet daemon delivers it when the desk can receive." +
        (res.detail ? " (" + res.detail + ")" : "");
    }
    return "Response state unclear — check the desk's conversation thread.";
  }
  function sendDecisionResponse(target, goalId, itemLabel, text) {
    return D.postJSON("/api/control/respond", {
      target: target, goal_id: goalId, item: itemLabel || "", message: text,
    }).then(function (res) { return formatRespondOutcome(res); });
  }
  // runRespond drives BOTH response surfaces (the decision cards and the map's respond
  // modal) through one path — validate, disable the button in flight, render the honest
  // outcome, KEEP the operator's words on failure, clear only on success (OCR #505:
  // duplicated handlers drift; one runner cannot).
  function runRespond(btn, msgEl, input, target, goalId, itemLabel) {
    var text = input ? (input.value || "").trim() : "";
    if (!text) { if (msgEl) msgEl.textContent = "Type a response first."; return; }
    if (!target) { if (msgEl) msgEl.textContent = "No desk owns this decision — respond via the coordinator's conversation."; return; }
    btn.disabled = true;
    if (msgEl) msgEl.textContent = "Sending…";
    sendDecisionResponse(target, goalId, itemLabel, text)
      .then(function (line) { if (msgEl) msgEl.textContent = line; if (input) input.value = ""; })
      .catch(function (err) { if (msgEl) msgEl.textContent = "NOT sent: " + ((err && err.message) || err); })
      .then(function () { btn.disabled = false; });
  }

  (function wireDecisionsPage() {
    var list = q("gdec-list");
    if (!list) return;
    list.addEventListener("click", function (e) {
      // #501: the per-card Respond button — the shared runner sends to the owning desk.
      var sendBtn = e.target.closest(".gdec-resp-send");
      if (sendBtn) {
        var box = sendBtn.closest(".gdec-respond");
        runRespond(sendBtn, box.querySelector(".gdec-resp-msg"), box.querySelector(".gdec-resp-input"),
          box.getAttribute("data-resp-target"), box.getAttribute("data-resp-goal"), box.getAttribute("data-resp-item"));
        return;
      }
      var goto = e.target.closest("[data-gdec-goto]");
      if (!goto) return;
      var id = goto.getAttribute("data-gdec-goto");
      if (D.showView) D.showView("goals"); // renders the map if this is its first open
      restoreNode(id);
      if (D.pushNav) D.pushNav({ view: "goals", node: id });
    });
  })();

  // Prime situation counts for the Decisions tab badge before the Goals tab opens.
  getJSON("/api/goals").then(function (doc) {
    cache = cache || doc;
    renderSituation(doc);
  }).catch(function () {});

  window.flotillaGoals = {
    show: show, refresh: refresh, restoreNode: restoreNode,
    openNode: function () { return selectedId; }, // the open drawer's node id (or null) — for history state
    openDecisions: openDecisions,
    // #509: pure decision-room surface for executable goja/headless regression.
    // Substring greps of goals.js cannot catch a removed hasBrief() gate; these
    // callables are the real functions the browser runs.
    _test: {
      hasBrief: hasBrief,
      gatherDecisions: gatherDecisions,
      renderDecisionCard: renderDecisionCard,
      renderPreparingRow: renderPreparingRow,
      respondTarget: respondTarget,
      decisionsCount: decisionsCount,
      formatRespondOutcome: formatRespondOutcome,
      runRespond: runRespond,
      sendDecisionResponse: sendDecisionResponse,
      indexGoalsNodes: indexGoalsNodes,
    },
  };
})();
