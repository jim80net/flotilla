/* flotilla dash — conversation-centric read surface (#210).
 *
 * IA: sidebar fleet map (channel → desks) → selected desk thread + inline control.
 * All dynamic data via fetch() — never server-rendered into <script> literals.
 * Live updates: EventSource on /events; /api/status is the poll fallback.
 */
(function () {
  "use strict";

  var POLL_FALLBACK_MS = 5000;

  function el(id) { return document.getElementById(id); }

  function escapeHtml(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  }

  function getJSON(path) {
    return fetch(path, { cache: "no-store" }).then(function (res) {
      if (!res.ok) throw new Error(path + " → " + res.status);
      return res.json();
    });
  }

  function postJSON(path, body) {
    return fetch(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Content-Type": "application/json", "X-Flotilla-Dash": "1" },
      body: body == null ? "" : JSON.stringify(body),
    }).then(function (res) {
      return res.text().then(function (text) {
        var data = {};
        if (text) { try { data = JSON.parse(text); } catch (e) { /* non-JSON */ } }
        if (!res.ok) throw new Error(data.error || (path + " → " + res.status));
        return data;
      });
    });
  }

  window.flotillaDash = { el: el, escapeHtml: escapeHtml, getJSON: getJSON, postJSON: postJSON };

  /* ── cached read model (combined on refresh) ───────────────────────────── */
  var cache = { status: null, topology: null, history: null, mirror: null };
  var selectedDesk = null;
  // Whether the operator has edited a control target field (route/resume). Once
  // touched, a background refresh must NOT overwrite it — otherwise a refresh
  // landing mid-typing silently replaces the operator's target and the control
  // action fires at a different desk than the field shows (#235 cubic P2). An
  // explicit desk-selection (rail click) resets this and re-sets the fields.
  var controlTargetsTouched = false;

  function agentMap(status) {
    var map = {};
    var agents = (status && Array.isArray(status.agents)) ? status.agents : [];
    agents.forEach(function (a) { map[String(a.name).toLowerCase()] = a; });
    return map;
  }

  function deskStateClass(state) {
    return "state-" + escapeHtml(String(state || "unknown"));
  }

  function renderFreshness(data) {
    var fresh = data.freshness || { state: "absent", message: "" };
    var banner = el("freshness");
    banner.className = "freshness show " + escapeHtml(fresh.state);
    banner.textContent = fresh.message || "";
    return fresh;
  }

  function renderRailMeta(status, fresh) {
    var meta = el("rail-meta");
    var xl = status.xo_liveness || {};
    var bits = [];
    if (status.xo) {
      var ack = xl.acked ? ("ack " + escapeHtml(xl.ack_age) + " ago") : "never acked";
      var settled = xl.settled_known ? (xl.settled ? "settled" : "active") : "settled unknown";
      bits.push(escapeHtml(status.xo) + " · " + ack + " · " + settled);
    }
    if (fresh.state === "stale") bits.push("snapshot stale");
    meta.innerHTML = bits.join(" · ");
  }

  function buildRailGroups(topology) {
    var channels = (topology && Array.isArray(topology.channels)) ? topology.channels : [];
    if (!channels.length) return [];
    return channels.map(function (ch) {
      var desks = [];
      var seen = {};
      function add(name, role) {
        var key = String(name).toLowerCase();
        if (!name || seen[key]) return;
        seen[key] = true;
        desks.push({ name: name, role: role || "" });
      }
      add(ch.xo_agent, "xo");
      (ch.members || []).forEach(function (m) { add(m, "member"); });
      return {
        channel_id: ch.channel_id,
        role: ch.role || "",
        desks: desks,
      };
    });
  }

  function ensureSelection(status, groups) {
    if (selectedDesk) return;
    if (status && status.xo) {
      selectedDesk = status.xo;
      return;
    }
    for (var i = 0; i < groups.length; i++) {
      if (groups[i].desks.length) {
        selectedDesk = groups[i].desks[0].name;
        return;
      }
    }
  }

  function renderConversationRail(status, topology, fresh) {
    var groups = buildRailGroups(topology);
    ensureSelection(status, groups);
    var agents = agentMap(status);
    var stale = fresh.state === "stale";
    var rail = el("conv-rail");

    if (!groups.length) {
      rail.innerHTML = '<div class="topo-note">' + escapeHtml(topology.note || "no channel bindings") + "</div>";
      return;
    }

    rail.innerHTML = groups.map(function (grp) {
      var role = grp.role ? '<span class="chan-role">' + escapeHtml(grp.role) + "</span>" : "";
      var items = grp.desks.map(function (d) {
        var key = String(d.name).toLowerCase();
        var a = agents[key] || {};
        var state = String(a.state || "unknown");
        var on = selectedDesk && String(selectedDesk).toLowerCase() === key;
        var roleTag = d.role === "xo"
          ? '<span class="conv-role xo">xo</span>'
          : (d.role ? '<span class="conv-role">' + escapeHtml(d.role) + "</span>" : "");
        return (
          '<button type="button" class="conv-item' + (on ? " selected" : "") + (stale ? " desk-stale" : "") + '" ' +
            'data-desk="' + escapeHtml(d.name) + '" role="listitem" aria-pressed="' + String(on) + '">' +
            '<span class="conv-rail ' + deskStateClass(state) + '" aria-hidden="true"></span>' +
            '<span class="conv-item-body">' +
              '<span class="conv-item-name">' + escapeHtml(d.name) + roleTag + "</span>" +
              '<span class="conv-item-state ' + deskStateClass(state) + '">' + escapeHtml(state) + "</span>" +
            "</span>" +
          "</button>"
        );
      }).join("");
      return (
        '<div class="conv-group">' +
          '<div class="conv-group-head">' +
            '<span class="chan-id">#' + escapeHtml(grp.channel_id) + "</span>" + role +
          "</div>" +
          '<div class="conv-group-items" role="list">' + items + "</div>" +
        "</div>"
      );
    }).join("");

    var buttons = rail.querySelectorAll(".conv-item");
    for (var i = 0; i < buttons.length; i++) {
      buttons[i].addEventListener("click", function () {
        selectedDesk = this.getAttribute("data-desk");
        renderConversations();
        syncControlTargets(true); // explicit desk-selection: set the targets authoritatively
        fetchMirror();            // load the newly-selected desk's session mirror
      });
    }
  }

  function ledgerMatchesDesk(entry, desk) {
    if (!desk) return false;
    var d = String(desk).toLowerCase();
    if (entry.parsed) {
      var from = String(entry.from || "").toLowerCase();
      var to = String(entry.to || "").toLowerCase();
      if (from === d || to === d) return true;
      if (to === "@" + d) return true;
      return false;
    }
    var raw = String(entry.raw || "").toLowerCase();
    return raw.indexOf(d) !== -1;
  }

  function renderDeskCard(status, fresh) {
    var card = el("conv-desk-card");
    if (!selectedDesk) {
      card.innerHTML = "";
      return;
    }
    var agents = agentMap(status);
    var a = agents[String(selectedDesk).toLowerCase()] || {};
    var state = String(a.state || "unknown");
    var stale = fresh.state === "stale";
    card.innerHTML =
      '<article class="desk conv-desk-mini' + (stale ? " desk-stale" : "") + '">' +
        '<div class="desk-rail ' + deskStateClass(state) + '" aria-hidden="true"></div>' +
        '<div class="desk-body">' +
          '<header class="desk-head">' +
            '<span class="desk-name">' + escapeHtml(selectedDesk) + "</span>" +
            '<span class="desk-state ' + deskStateClass(state) + '">' + escapeHtml(state) + "</span>" +
          "</header>" +
          '<span class="desk-surface">' + escapeHtml(a.surface || "—") + "</span>" +
        "</div>" +
      "</article>";
  }

  function renderConversationHeader(topology) {
    el("conv-title").textContent = selectedDesk ? selectedDesk : "Conversation";
    var channel = "—";
    var groups = buildRailGroups(topology);
    for (var i = 0; i < groups.length; i++) {
      for (var j = 0; j < groups[i].desks.length; j++) {
        if (String(groups[i].desks[j].name).toLowerCase() === String(selectedDesk || "").toLowerCase()) {
          channel = "#" + groups[i].channel_id;
          break;
        }
      }
    }
    el("conv-sub").textContent = selectedDesk
      ? ("Coordination on " + channel + " — ledger filtered to this desk")
      : "Select a desk from the fleet map";
  }

  // renderSessionMirror is the glance widget (design §2.5): the selected desk's
  // LATEST session-mirror entry (the desk's own session output at info level),
  // replacing the old reader-map placeholder. Reads /api/session-mirror
  // (SessionMirrorDoc, entries ascending oldest→newest, so the latest is last).
  // Degrades gracefully (empty state) when the endpoint or desk is absent — the
  // full history thread is a follow-on increment.
  function relTime(iso) {
    var t = Date.parse(iso);
    if (isNaN(t)) return iso || "";
    var s = Math.max(0, Math.round((Date.now() - t) / 1000));
    if (s < 60) return "just now";
    if (s < 3600) return Math.floor(s / 60) + "m ago";
    if (s < 86400) return Math.floor(s / 3600) + "h ago";
    return Math.floor(s / 86400) + "d ago";
  }
  // cheapHash (djb2) folds the entry content into the dedup key — ts alone is
  // RFC3339 second-resolution, so two entries in the same second would otherwise
  // dedup-skip the newer one and render stale.
  function cheapHash(s) {
    var h = 5381, i = (s || "").length;
    while (i) { h = ((h * 33) ^ s.charCodeAt(--i)) >>> 0; }
    return h;
  }
  var lastMirrorKey = null; // dedup key so an SSE tick doesn't re-announce / reset scroll
  function mirrorEmpty(text, key) {
    if (lastMirrorKey === key) return;
    lastMirrorKey = key;
    el("conv-map").innerHTML = '<span class="conv-map-label">session mirror</span>' +
      '<span class="conv-map-empty">' + text + "</span>";
  }
  function renderSessionMirror() {
    if (!selectedDesk) { mirrorEmpty("Select a desk to see its latest session output.", "none"); return; }
    var doc = cache.mirror || {};
    // Identity guard: cache.mirror may still hold the PREVIOUS desk's doc (the async
    // fetch for the new selection hasn't landed) — show a neutral loading state, never
    // the wrong desk's turn-final under this desk's header.
    if (doc.agent && String(doc.agent).toLowerCase() !== String(selectedDesk).toLowerCase()) {
      mirrorEmpty("Loading…", "load:" + selectedDesk); return;
    }
    var entries = Array.isArray(doc.entries) ? doc.entries : [];
    if (!entries.length) {
      mirrorEmpty(doc.error ? "Session mirror unavailable." : "No session mirror yet for this desk.",
        (doc.error ? "err:" : "empty:") + selectedDesk);
      return;
    }
    var latest = entries[entries.length - 1]; // ascending order → newest is last
    // Dedup: skip the innerHTML rewrite (which re-announces via the aria-live region
    // AND resets the operator's scroll position) when the shown entry is unchanged.
    // Key on ts PLUS a content hash+length — ts is second-resolution, so a new entry
    // in the same second must still re-render (not dedup-skip as stale).
    var info = latest.info || "";
    var key = selectedDesk + "|" + (latest.ts || "") + "|" + info.length + ":" + cheapHash(info);
    if (key === lastMirrorKey) return;
    lastMirrorKey = key;
    var when = latest.ts
      ? '<time class="mirror-when" datetime="' + escapeHtml(latest.ts) + '" title="' + escapeHtml(latest.ts) + '">' + escapeHtml(relTime(latest.ts)) + "</time>"
      : "";
    var body = escapeHtml(latest.info || "").replace(/\r?\n/g, "<br>");
    el("conv-map").innerHTML =
      '<span class="conv-map-label">session mirror ' + when + "</span>" +
      '<div class="mirror-glance">' + body + "</div>";
  }

  // fetchMirror loads the selected desk's session mirror and re-renders the glance.
  // Guarded on selectedDesk so a slow response for a de-selected desk is dropped.
  // limit=100 fetches the full recent tail (the glance uses only the last entry) —
  // sized for the Inc 2 thread-merge, which reuses cache.mirror.entries in full.
  function fetchMirror() {
    var want = selectedDesk;
    if (!want) { cache.mirror = null; renderSessionMirror(); return; }
    getJSON("/api/session-mirror?agent=" + encodeURIComponent(want) + "&limit=100").then(function (d) {
      if (selectedDesk === want) { cache.mirror = d; renderSessionMirror(); }
    }).catch(function (err) {
      if (selectedDesk === want) { cache.mirror = { agent: want, entries: [], error: err.message }; renderSessionMirror(); }
    });
  }

  function renderThread(history) {
    var thread = el("conv-thread");
    var entries = (history && Array.isArray(history.ledger)) ? history.ledger : [];
    var filtered = selectedDesk
      ? entries.filter(function (e) { return ledgerMatchesDesk(e, selectedDesk); })
      : [];
    if (!selectedDesk) {
      thread.innerHTML = '<div class="empty">Pick a desk to read its coordination thread.</div>';
      return;
    }
    if (!filtered.length) {
      thread.innerHTML = '<div class="empty">No ledger entries for this desk yet.</div>';
      return;
    }
    thread.innerHTML = filtered.map(function (e) {
      if (!e.parsed) {
        return '<div class="thread-msg thread-raw">' + escapeHtml(e.raw) + "</div>";
      }
      var deskKey = String(selectedDesk).toLowerCase();
      var outbound = String(e.from || "").toLowerCase() === deskKey;
      var cls = outbound ? "thread-out" : "thread-in";
      return (
        '<div class="thread-msg ' + cls + '">' +
          '<header class="thread-head">' +
            '<span class="thread-route">' + escapeHtml(e.from) + " → " + escapeHtml(e.to) + "</span>" +
            '<time class="thread-time">' + escapeHtml(e.time) + "</time>" +
          "</header>" +
          (e.channel && e.channel !== "-"
            ? '<span class="thread-chan muted">#' + escapeHtml(e.channel) + "</span>"
            : "") +
          '<p class="thread-gist">' + escapeHtml(e.gist) + "</p>" +
        "</div>"
      );
    }).join("");
  }

  function renderBacklogStrip(history) {
    var bl = (history && history.backlog) ? history.backlog : {};
    var box = el("conv-backlog");
    var counts =
      '<div class="backlog-counts">' +
        '<span>' + (bl.items || 0) + " items</span>" +
        '<span class="count-blocked">' + (bl.blocked || 0) + " blocked</span>" +
        (bl.awaiting_auth ? '<span class="count-awaiting-auth">' + bl.awaiting_auth + " awaiting-auth</span>" : "") +
        '<span class="count-done">' + (bl.done || 0) + " done</span>" +
      "</div>";
    var unblocked = Array.isArray(bl.unblocked) ? bl.unblocked : [];
    var items = unblocked.length
      ? unblocked.map(function (line) {
          return '<div class="backlog-item">' + escapeHtml(line) + "</div>";
        }).join("")
      : (bl.found ? '<div class="empty">No unblocked items.</div>' : '<div class="empty">No backlog section found.</div>');
    box.innerHTML = counts + items;
  }

  function renderConversations() {
    var status = cache.status || {};
    var topology = cache.topology || {};
    var history = cache.history || {};
    var fresh = renderFreshness(status);
    renderRailMeta(status, fresh);
    renderConversationRail(status, topology, fresh);
    renderConversationHeader(topology);
    renderDeskCard(status, fresh);
    renderSessionMirror();
    renderThread(history);
    renderBacklogStrip(history);
  }

  // syncControlTargets prefills the route/resume target fields with the selected
  // desk. `explicit` (a rail desk-click) sets them AUTHORITATIVELY and clears the
  // touched flag — a deliberate "target this desk" action. A background refresh
  // (explicit falsy) prefills ONLY when the operator has not edited the fields, so
  // a refresh can never clobber an in-progress edit and misdirect the control
  // action to the wrong desk (#235 cubic P2).
  function syncControlTargets(explicit) {
    if (!selectedDesk) return;
    if (!explicit && controlTargetsTouched) return;
    el("route-target").value = selectedDesk;
    el("resume-agent").value = selectedDesk;
    if (explicit) controlTargetsTouched = false;
  }

  /* ── refresh orchestration ───────────────────────────────────────────── */
  var refreshEpoch = 0;
  function refresh() {
    var epoch = ++refreshEpoch;
    function current() { return epoch === refreshEpoch; }

    var pStatus = getJSON("/api/status").then(function (d) {
      if (current()) cache.status = d;
    }).catch(function (err) {
      if (current()) {
        cache.status = { freshness: { state: "absent", message: "Could not load fleet status (" + err.message + ")" } };
      }
    });

    var pTopo = getJSON("/api/topology").then(function (d) {
      if (current()) cache.topology = d;
    }).catch(function (err) {
      if (current()) cache.topology = { channels: [], note: err.message };
    });

    var pHist = getJSON("/api/history").then(function (d) {
      if (current()) cache.history = d;
    }).catch(function (err) {
      if (current()) cache.history = { ledger: [], backlog: { found: false, unblocked: [] } };
    });

    return Promise.all([pStatus, pTopo, pHist]).then(function () {
      if (current()) {
        renderConversations();
        syncControlTargets();
        fetchMirror(); // keep the selected desk's session-mirror glance current on each tick
      }
      // Keep the Goals view live off the same refresh cadence (SSE-triggered). It
      // fetches /api/goals itself and no-ops until the operator has opened the tab.
      if (window.flotillaGoals) window.flotillaGoals.refresh();
    });
  }

  /* ── live link: SSE with polling fallback ────────────────────────────── */
  function setConn(state) {
    var c = el("conn");
    c.className = "conn " + state;
    var label = state === "live" ? "Live update link connected" :
      state === "down" ? "Live update link reconnecting" : "Live update link idle";
    c.setAttribute("aria-label", label);
    c.title = state === "live" ? "live (SSE)" : state === "down" ? "reconnecting…" : "link";
  }

  function connect() {
    refresh();
    if (typeof EventSource === "undefined") {
      setInterval(refresh, POLL_FALLBACK_MS);
      return;
    }
    var es = new EventSource("/events");
    var pollTimer = null;
    function stopPolling() { if (pollTimer) { clearInterval(pollTimer); pollTimer = null; } }
    es.addEventListener("update", function () {
      setConn("live");
      stopPolling();
      refresh();
    });
    es.onopen = function () { setConn("live"); stopPolling(); };
    es.onerror = function () {
      setConn("down");
      if (!pollTimer) pollTimer = setInterval(refresh, POLL_FALLBACK_MS);
    };
  }

  connect();

  /* ── tab nav: Conversations ⇄ Goals ⇄ Issues ───────────────────────── */
  var VIEWS = ["conversations", "goals", "issues"];
  function showView(view) {
    VIEWS.forEach(function (v) {
      var on = v === view;
      el("view-" + v).classList.toggle("hidden", !on);
      el("tab-" + v).classList.toggle("active", on);
      el("tab-" + v).setAttribute("aria-selected", String(on));
    });
    el("freshness").classList.toggle("hidden", view !== "conversations");
    if (view === "goals" && window.flotillaGoals) window.flotillaGoals.show();
    if (view === "issues" && window.flotillaTracker) window.flotillaTracker.show();
  }
  var tabs = document.querySelectorAll(".tab");
  for (var i = 0; i < tabs.length; i++) {
    tabs[i].addEventListener("click", function () { showView(this.getAttribute("data-view")); });
  }

  // openConversation is the deep-link the Goals map calls (feedback #3): select a
  // desk's conversation thread and switch to the Conversations view. Mirrors the
  // rail's own desk-click (selectedDesk + render + authoritative control targets).
  function openConversation(desk) {
    if (!desk) return;
    selectedDesk = desk;
    showView("conversations");
    renderConversations();
    syncControlTargets(true);
    fetchMirror(); // load the deep-linked desk's session mirror (the identity guard hides the prior desk's until it lands)
    // Move focus into the now-visible Conversations view — the deep-link hid the
    // Goals view, so leaving focus on the goals node would strand it on <body>.
    var title = el("conv-title");
    if (title) { title.setAttribute("tabindex", "-1"); title.focus(); }
  }
  window.flotillaDash.openConversation = openConversation;

  // Mark the control targets "touched" the instant the operator edits either field
  // (type, paste, or clear all fire `input`), so a background refresh stops
  // auto-prefilling and can never overwrite the operator's chosen target (#235).
  // The fields are static chrome, so this one-time wiring holds for the session.
  ["route-target", "resume-agent"].forEach(function (id) {
    var field = el(id);
    if (field) field.addEventListener("input", function () { controlTargetsTouched = true; });
  });
})();