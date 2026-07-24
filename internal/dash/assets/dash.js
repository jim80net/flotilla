/* flotilla dash — conversation-centric read surface (#210).
 *
 * IA: sidebar fleet map (organization → desks) → selected desk thread + inline control.
 * All dynamic data via fetch() — never server-rendered into <script> literals.
 * Live updates: EventSource on /events; /api/status is the poll fallback.
 */
(function () {
  "use strict";

  var POLL_FALLBACK_MS = 5000;
  var queueItems = []; // structured /api/history backlog projection (#419)
  var liveUpdateListeners = [];

  function el(id) { return document.getElementById(id); }

  function escapeHtml(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  }

  // Coalesce identical reads while they are in flight. Startup and one SSE tick
  // have several independent consumers (landing, tab dots, Goals badge/view),
  // but the browser should put only one request for an API key on the wire.
  var inFlightJSON = {};
  function getJSON(path) {
    if (inFlightJSON[path]) return inFlightJSON[path];
    var request = fetch(path, { cache: "no-store" }).then(function (res) {
      if (!res.ok) throw new Error(path + " → " + res.status);
      return res.json();
    });
    inFlightJSON[path] = request;
    var clear = function () {
      if (inFlightJSON[path] === request) delete inFlightJSON[path];
    };
    request.then(clear, clear);
    return request;
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

  function onLiveUpdate(listener) {
    if (typeof listener !== "function") return function () {};
    liveUpdateListeners.push(listener);
    return function () {
      liveUpdateListeners = liveUpdateListeners.filter(function (fn) { return fn !== listener; });
    };
  }
  function notifyLiveUpdate() {
    liveUpdateListeners.slice().forEach(function (listener) {
      try { listener(); } catch (e) { /* one consumer must not break the shared SSE link */ }
    });
  }
  function routeMessage(target, message) {
    return postJSON("/api/control/route", { target: target, message: message });
  }
  function routeOutcomeCopy(res) {
    var outcome = (res && res.outcome) || "(no outcome reported)";
    var detail = res && res.detail ? " — " + res.detail : "";
    if (outcome === "delivered") {
      return { outcome: outcome, text: "Delivered to " + (res.target || "the desk") + ".", ok: true };
    }
    if (outcome === "queued") {
      var id = res.queued_id ? " (id " + res.queued_id + ")" : "";
      return {
        outcome: outcome,
        text: "Queued durably for " + (res.target || "the desk") + id + " — it will deliver when the desk can receive." + detail,
        ok: true,
      };
    }
    return { outcome: outcome, text: "NOT accepted: " + outcome + detail, ok: false };
  }
  window.flotillaDash = {
    el: el, escapeHtml: escapeHtml, getJSON: getJSON, postJSON: postJSON,
    onLiveUpdate: onLiveUpdate, routeMessage: routeMessage, routeOutcomeCopy: routeOutcomeCopy,
  };

  /* ── cached read model (combined on refresh) ───────────────────────────── */
  var cache = { status: null, topology: null, history: null, mirror: null };
  // Selection retains both desk and home-channel context: #745 makes the map one-row-per-seat,
  // while the channel still scopes the selected thread header and backlog projection.
  var selectedDesk = null, selectedChannel = null;
  // Session-mirror detail level (design §2.3 UI half). "info" = the readermap body
  // only (clean default); "debug" additionally reveals each entry's collapsible
  // debug tier (reader-map envelope, mirror note, firewall warn-terms). The full
  // debug payload is ALWAYS present in the ledger — this is a live render toggle, so
  // the tier ships ON-demand (no dormant env gate, no restart). Folded into the
  // glance + thread dedup keys so flipping it forces a repaint.
  var mirrorVerbosity = "info";

  function agentMap(status) {
    var map = {};
    var agents = (status && Array.isArray(status.agents)) ? status.agents : [];
    agents.forEach(function (a) { map[String(a.name).toLowerCase()] = a; });
    return map;
  }

  function deskStateClass(state) {
    return "state-" + escapeHtml(String(state || "unknown"));
  }

  function operatorVisualState(state, posture) {
    return posture === "blocked" ? "blocked" : state;
  }

  function usageText(usage) {
    if (!usage || !Number.isFinite(Number(usage.remaining_percent))) return "";
    var text = String(usage.remaining_percent) + "% " + String(usage.window || "usage");
    if (usage.stale_after && Date.parse(usage.stale_after) < Date.now()) text += " stale";
    return text;
  }

  function renderFreshness(data) {
    var fresh = data.freshness || { state: "absent", message: "" };
    var banner = el("freshness");
    banner.className = "freshness show " + escapeHtml(fresh.state);
    banner.textContent = fresh.message || "";
    return fresh;
  }

  function utilizationUnits(status) {
    var u = (status && status.utilization) || {};
    if (!Number.isFinite(Number(u.total))) return [{ kind: "unavailable", text: "Fleet utilization unavailable" }];
    var total = Number(u.total || 0);
    var units = [{
      kind: "working",
      text: Number(u.working || 0) + " of " + total + " " + (total === 1 ? "seat" : "seats") + " working"
    }];
    if (Number(u.blocked || 0) > 0) units.push({ kind: "blocked", text: Number(u.blocked) + " blocked" });
    if (Number(u.awaiting_authority || 0) > 0) {
      var held = Number(u.awaiting_authority);
      units.push({ kind: "held", text: held + " " + (held === 1 ? "seat" : "seats") + " waiting for authority" });
    }
    return units;
  }

  function utilizationText(status) {
    return utilizationUnits(status).map(function (unit) { return unit.text; }).join(" · ");
  }

  function utilizationReadUnits(status) {
    return status && status.utilization && status.utilization.utilization_wall
      ? [
        { kind: "read", text: "Almost no one is working" },
        { kind: "action", text: "Send work or pull the next queue item" }
      ]
      : [];
  }

  function renderUtilization(status) {
    var target = el("fleet-utilization");
    if (target) {
      var units = utilizationUnits(status).concat(utilizationReadUnits(status));
      target.innerHTML = units.map(function (unit) {
        return '<span class="fleet-utilization-unit">' + escapeHtml(unit.text) + "</span>";
      }).join(" ");
    }
  }

  var lastSwarmKey = "";
  function renderLiveSwarm(status) {
    var agents = ((status || {}).agents || []).filter(function (a) { return a.state === "working"; });
    var key = JSON.stringify(agents.map(function (a) { return [a.name, a.last_action || null]; }));
    if (key === lastSwarmKey) return;
    lastSwarmKey = key;
    var rate = el("live-swarm-rate"), items = el("live-swarm-items");
    var u = (status && status.utilization) || {};
    if (rate) {
      var total = Number(u.total || 0);
      rate.textContent = Number(u.working || 0) + " of " + total + " " + (total === 1 ? "seat" : "seats") + " working";
    }
    if (!items) return;
    if (!agents.length) {
      items.innerHTML = '<span class="live-swarm-empty">No seats working — send work or pull the next queue item.</span>';
      return;
    }
    items.innerHTML = agents.map(function (a) {
      var action = a.last_action || {};
      var when = action.at ? relTime(action.at) : "awaiting first update";
      var summary = action.summary || "action awaiting first update";
      return '<button type="button" class="live-swarm-card" data-swarm-desk="' + escapeHtml(a.name) + '">' +
        '<span class="live-swarm-name">' + escapeHtml(a.name) + '</span>' +
        '<span class="live-swarm-action">' + escapeHtml(summary) + '</span>' +
        '<span class="live-swarm-when">' + escapeHtml(when) + '</span></button>';
    }).join("");
    var cards = items.querySelectorAll("[data-swarm-desk]");
    for (var i = 0; i < cards.length; i++) {
      cards[i].addEventListener("click", function () { openConversation(this.getAttribute("data-swarm-desk"), ""); });
    }
  }

  function renderRailMeta(status, fresh) {
    var meta = el("rail-meta");
    var xl = status.xo_liveness || {};
    var units = utilizationUnits(status);
    if (status.xo) {
      var ack = xl.acked ? ("ack " + String(xl.ack_age || "unknown") + " ago") : "never acked";
      var settled = xl.settled_known ? (xl.settled ? "settled" : "active") : "settled unknown";
      units.push({ kind: "liveness", text: String(status.xo) + " · " + ack + " · " + settled });
    }
    if (fresh.state === "stale") units.push({ kind: "stale", text: "snapshot stale" });
    meta.innerHTML = units.map(function (unit) {
      return '<span class="fleet-status-unit ' + unit.kind + '">' + escapeHtml(unit.text) + "</span>";
    }).join(" ");
  }

  // coordinatorNames returns the coordinator agents the rail must always surface — the
  // primary XO and, when distinct, the CoS (from /api/status). The coordinator's session
  // IS mirrored to a ledger like any desk, but an older/incomplete org document may omit that
  // identity. Status pinning keeps the coordinator conversation reachable (F#383 criterion 1).
  function coordinatorNames(status) {
    var st = status || cache.status || {};
    var out = [];
    if (st.xo) out.push(st.xo);
    if (st.cos && String(st.cos).toLowerCase() !== String(st.xo || "").toLowerCase()) out.push(st.cos);
    return out;
  }
  function agentKey(name) {
    return String(name || "").trim().toLowerCase();
  }

  // flotillaLabel turns the owning coordinator identity into a readable group label. Channel
  // snowflakes are routing data, not operator-facing organization names (#745).
  function flotillaLabel(name) {
    var clean = String(name || "").trim().replace(/-(?:xo|adj)$/i, "");
    if (!clean) return "Desks";
    if (/^[a-z]{1,3}$/i.test(clean)) return clean.toUpperCase();
    return clean.replace(/[-_]+/g, " ").replace(/\b[a-z]/g, function (c) { return c.toUpperCase(); });
  }

  function railRoleTag(name, role) {
    var key = agentKey(name);
    var roleKey = String(role || "").toLowerCase();
    var roleInName = roleKey && (key === roleKey || /-(?:xo|adj|cos)$/.test(key));
    return roleKey && roleKey !== "member" && !roleInName
      ? '<span class="conv-role' + (roleKey === "xo" ? " xo" : "") + '">· ' + escapeHtml(roleKey) + "</span>"
      : "";
  }

  // buildRailGroups projects canonical org truth into an operator map:
  //   • coordinators appear exactly once in Fleet Command;
  //   • every container/flotilla appears once in org order;
  //   • every other seat appears exactly once under its nearest container/coordinator;
  //   • seats without a usable org parent remain visible in one honest Desks group.
  // Historical/mirror channels remain available as routing metadata but never become groups.
  function buildRailGroups(topology, status) {
    var channels = (topology && Array.isArray(topology.channels)) ? topology.channels : [];
    var orgNodes = (topology && Array.isArray(topology.org_nodes)) ? topology.org_nodes : [];
    var board = status || cache.status || {};
    // Roster-authoritative on current servers. The fallback preserves compatibility with older
    // topology documents that predate the coordinator set.
    var coordList = (topology && Array.isArray(topology.coordinators) && topology.coordinators.length)
      ? topology.coordinators.slice()
      : coordinatorNames(board).concat(channels.map(function (ch) { return ch.xo_agent; }));
    coordinatorNames(board).forEach(function (name) { coordList.push(name); });
    var coordSet = {};
    var coordinators = [];
    coordList.forEach(function (name) {
      var key = agentKey(name);
      if (!key || coordSet[key]) return;
      coordSet[key] = true;
      coordinators.push(name);
    });
    function isCoord(name) { return !!coordSet[agentKey(name)]; }

    var nodeByID = {};
    orgNodes.forEach(function (node) { if (node && node.id) nodeByID[agentKey(node.id)] = node; });
    var rootKey = agentKey(topology && topology.org_root);
    var orgOrder = [], orgSeen = {};
    function walkOrg(key) {
      key = agentKey(key);
      if (!key || orgSeen[key]) return;
      orgSeen[key] = true;
      orgOrder.push(key);
      var node = nodeByID[key];
      (node && node.children || []).forEach(walkOrg);
    }
    walkOrg(rootKey);
    orgNodes.forEach(function (node) { if (node && node.id) walkOrg(node.id); });
    var seats = [], seatSeen = {};
    function addSeat(name) {
      var key = agentKey(name);
      if (!key || seatSeen[key]) return;
      seatSeen[key] = true;
      seats.push(name);
    }
    coordinators.forEach(addSeat);
    orgNodes.forEach(function (node) { if (node && node.kind !== "container") addSeat(node.id); });
    channels.forEach(function (ch) {
      addSeat(ch.xo_agent);
      (ch.members || []).forEach(addSeat);
    });
    ((board && board.agents) || []).forEach(function (agent) { addSeat(agent.name); });

    // groupAnchor prefers an explicit container anywhere above the seat. Without one, the
    // nearest coordinator supplies the backward-compatible flotilla group.
    function groupAnchor(name) {
      var key = agentKey(name);
      var visited = {};
      var nearestCoord = "";
      for (var steps = 0; key && steps <= orgNodes.length; steps++) {
        var node = nodeByID[key];
        var parent = node ? agentKey(node.parent) : "";
        if (!parent || visited[parent]) break;
        visited[parent] = true;
        var parentNode = nodeByID[parent];
        if (parentNode && parentNode.kind === "container") return { key: parent, kind: "container" };
        if (!nearestCoord && coordSet[parent]) nearestCoord = parent;
        key = parent;
      }
      if (nearestCoord) return { key: nearestCoord, kind: "coordinator" };
      // Compatibility fallback for a topology without org_nodes: prefer an explicit project
      // binding and only then an unannotated binding. Never use fleet-command membership.
      var preferred = channels.filter(function (ch) { return ch.role === "project"; })
        .concat(channels.filter(function (ch) { return !ch.role; }));
      for (var i = 0; i < preferred.length; i++) {
        var ch = preferred[i];
        if (!isCoord(ch.xo_agent)) continue;
        if (agentKey(ch.xo_agent) === agentKey(name)) return { key: agentKey(ch.xo_agent), kind: "coordinator" };
        if ((ch.members || []).some(function (m) { return agentKey(m) === agentKey(name); })) {
          return { key: agentKey(ch.xo_agent), kind: "coordinator" };
        }
      }
      return null;
    }

    function homeChannel(owner) {
      for (var i = 0; i < channels.length; i++) {
        if (channels[i].role === "project" && agentKey(channels[i].xo_agent) === owner) return channels[i].channel_id || "";
      }
      for (var j = 0; j < channels.length; j++) {
        if (channels[j].role !== "fleet-command" && agentKey(channels[j].xo_agent) === owner) return channels[j].channel_id || "";
      }
      return "";
    }

    function seatChannel(name, fallback) {
      var node = nodeByID[agentKey(name)];
      if (node && node.home_channel_id) return node.home_channel_id;
      return homeChannel(agentKey(name)) || fallback || "";
    }

    var fleetChannel = "";
    for (var f = 0; f < channels.length; f++) {
      if (channels[f].role === "fleet-command") { fleetChannel = channels[f].channel_id || ""; break; }
    }
    var cosKey = agentKey(board && board.cos);
    var groups = [];
    if (coordinators.length) {
      groups.push({
        channel_id: fleetChannel,
        role: "fleet-command",
        label: "Fleet Command",
        desks: coordinators.map(function (name) {
          return {
            name: name,
            role: agentKey(name) === cosKey ? "cos" : "xo",
            channel_id: seatChannel(name, fleetChannel),
          };
        }),
      });
    }

    var groupByKey = {}, groupOrder = [];
    function orgDepth(key) {
      var depth = 0, seen = {};
      key = agentKey(key);
      while (key && key !== rootKey && !seen[key] && depth <= orgNodes.length) {
        seen[key] = true;
        var node = nodeByID[key];
        key = node ? agentKey(node.parent) : "";
        depth++;
      }
      return Math.max(0, depth - 1);
    }
    function ensureGroup(key, kind) {
      var mapKey = kind === "desks" ? "__desks__" : agentKey(key);
      if (groupByKey[mapKey]) return groupByKey[mapKey];
      var node = nodeByID[mapKey];
      var group = {
        channel_id: node && node.home_channel_id ? node.home_channel_id : homeChannel(mapKey),
        role: kind === "desks" ? "desks" : (kind === "container" ? "flotilla" : "project"),
        label: kind === "desks" ? "Desks" : flotillaLabel((node && node.id) || key),
        depth: kind === "desks" ? 0 : orgDepth(mapKey),
        desks: [],
      };
      groupByKey[mapKey] = group;
      groupOrder.push(mapKey);
      return group;
    }
    function hasContainerAncestor(key) {
      var seen = {};
      for (var steps = 0; key && steps <= orgNodes.length; steps++) {
        var node = nodeByID[key];
        var parent = node ? agentKey(node.parent) : "";
        if (!parent || seen[parent]) return false;
        seen[parent] = true;
        if (nodeByID[parent] && nodeByID[parent].kind === "container") return true;
        key = parent;
      }
      return false;
    }
    // Pre-create canonical org groups in tree order. A coordinator nested inside a container
    // belongs to that container; a coordinator without one remains a flotilla anchor.
    orgOrder.forEach(function (key) {
      var node = nodeByID[key];
      if (!node) return;
      if (node.kind === "container") ensureGroup(key, "container");
      else if (key !== rootKey && coordSet[key] && !hasContainerAncestor(key)) ensureGroup(key, "coordinator");
    });

    seats.forEach(function (name) {
      if (isCoord(name)) return;
      var anchor = groupAnchor(name);
      var isRootDesks = anchor && anchor.kind === "coordinator" && anchor.key === rootKey;
      var group = anchor && !isRootDesks
        ? ensureGroup(anchor.key, anchor.kind)
        : ensureGroup("", "desks");
      group.desks.push({ name: name, role: "member", channel_id: seatChannel(name, group.channel_id) });
    });
    groupOrder.forEach(function (key) { groups.push(groupByKey[key]); });
    return groups;
  }

  function groupForDesk(groups, name) {
    var want = String(name || "").toLowerCase();
    for (var i = 0; i < groups.length; i++) {
      for (var j = 0; j < groups[i].desks.length; j++) {
        if (String(groups[i].desks[j].name).toLowerCase() === want) return groups[i];
      }
    }
    return null;
  }

  // channelForDesk returns the hidden routing/backlog context of a seat's one map group.
  function channelForDesk(groups, name) {
    var want = agentKey(name);
    for (var i = 0; i < groups.length; i++) {
      for (var j = 0; j < groups[i].desks.length; j++) {
        var desk = groups[i].desks[j];
        if (agentKey(desk.name) === want) return desk.channel_id || groups[i].channel_id || "";
      }
    }
    return "";
  }

  function ensureSelection(status, groups) {
    if (selectedDesk) return;
    // Prefer a distinct CoS on first paint (F#383 criterion 1) — the standalone-
    // conversations test starts with the coordinator thread, not a random desk.
    // When cos is unset or identical to xo, BoardDoc.cos is empty and we fall through.
    if (status && status.cos) {
      selectedDesk = status.cos;
      selectedChannel = channelForDesk(groups, status.cos);
      return;
    }
    if (status && status.xo) {
      selectedDesk = status.xo;
      selectedChannel = channelForDesk(groups, status.xo);
      return;
    }
    for (var i = 0; i < groups.length; i++) {
      if (groups[i].desks.length) {
        selectedDesk = groups[i].desks[0].name;
        selectedChannel = channelForDesk(groups, selectedDesk);
        return;
      }
    }
  }

  function renderConversationRail(status, topology, fresh) {
    var groups = buildRailGroups(topology);
    ensureSelection(status, groups);
    // Canonicalize old channel-scoped links to the seat's one org-map home. Historical channel
    // bindings no longer create alternate copies, so retaining one would leave no row selected.
    if (selectedDesk) selectedChannel = channelForDesk(groups, selectedDesk);
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
        var rowChannel = d.channel_id || grp.channel_id || "";
        var a = agents[key] || {};
        var state = String(a.state || "unknown");
        // #524: show loop_posture beside pane state so officers see parked vs drifted.
        var posture = String(a.loop_posture || "");
        var visualState = operatorVisualState(state, posture);
        var stateLabel = posture ? (state + " · " + posture) : state;
        var usage = usageText(a.usage);
        if (usage) stateLabel += " · " + usage;
        // Keep the channel in selection state for backlog scoping, even though #745 guarantees
        // the map itself contains only one row for this seat.
        var on = selectedDesk && String(selectedDesk).toLowerCase() === key && rowChannel === selectedChannel;
        // Ordinary member chips add no information in a hierarchy. Coordinator chips remain,
        // separated by a visible dot, unless the identity already carries that role suffix —
        // avoiding glued labels such as "alpha-xoxo" in text/screen-reader output (#745).
        var roleTag = railRoleTag(d.name, d.role);
        return (
          '<button type="button" class="conv-item' + (on ? " selected" : "") + (stale ? " desk-stale" : "") + '" ' +
            'data-desk="' + escapeHtml(d.name) + '" data-channel="' + escapeHtml(rowChannel) + '" role="listitem" aria-pressed="' + String(on) + '">' +
            '<span class="conv-rail ' + deskStateClass(visualState) + '" aria-hidden="true"></span>' +
            '<span class="conv-item-body">' +
              '<span class="conv-item-name">' + escapeHtml(d.name) + roleTag + "</span>" +
              '<span class="conv-item-state ' + deskStateClass(visualState) + '" title="pane state · loop posture">' + escapeHtml(stateLabel) + "</span>" +
            "</span>" +
          "</button>"
        );
      }).join("");
      // Every #745 group has an operator-facing org label; the channel id remains only in
      // data-channel for routing/backlog context and never becomes a snowflake header.
      var head = '<span class="chan-id ' + (grp.role === "desks" ? "chan-desks" : "chan-fleet-command") + '">' +
        escapeHtml(grp.label || "Desks") + "</span>" + role;
      var depthCls = grp.depth ? " conv-group-depth-" + Math.min(3, grp.depth) : "";
      var extraCls = (grp.role === "fleet-command" ? " conv-group-fleet-command" : "") + depthCls;
      return (
        '<div class="conv-group' + extraCls + '">' +
          '<div class="conv-group-head">' + head + "</div>" +
          '<div class="conv-group-items" role="list">' + items + "</div>" +
        "</div>"
      );
    }).join("");

    var buttons = rail.querySelectorAll(".conv-item");
    for (var i = 0; i < buttons.length; i++) {
      buttons[i].addEventListener("click", function () {
        selectedDesk = this.getAttribute("data-desk");
        selectedChannel = this.getAttribute("data-channel"); // scope the selection to THIS channel copy (#370)
        resetThreadScroll(); // a freshly selected thread opens at its latest message
        renderConversations();
        fetchSelectedHistory(true);
        fetchMirror();            // load the newly-selected desk's session mirror
        pushNav({ view: "conversations", desk: selectedDesk, channel: selectedChannel }); // reversible (#349 A1)
      });
    }
  }

  // ledgerParticipant normalizes a ledger from/to token to a bare desk name: a relay
  // target may be written "@name" (the mention form the daemon uses for a desk address —
  // see watch.go mirrorRelayToLedger) while an XO/operator is bare. Strip the leading "@"
  // so a desk matches whether it appears as sender OR recipient, in either form. Handling
  // "@" on ONLY the `to` side (the prior bug) dropped a desk's OWN OUTBOUND relay lines
  // (from "@desk") from its thread.
  function ledgerParticipant(tok) {
    return String(tok || "").toLowerCase().replace(/^@/, "");
  }
  // rawHasParticipant reports whether `desk` appears as a WHOLE token in an unparsed
  // ledger line — not as a prefix of a longer agent name. Substring match ("cos" inside
  // "cos-adj") was flooding the cos thread with unrelated unparsed bullets (#518).
  function rawHasParticipant(raw, desk) {
    if (!desk) return false;
    var s = String(raw || "").toLowerCase();
    var d = String(desk).toLowerCase();
    var i = 0;
    while ((i = s.indexOf(d, i)) !== -1) {
      var before = i === 0 ? "" : s.charAt(i - 1);
      var after = s.charAt(i + d.length);
      // Agent tokens are [a-z0-9_-]+ — treat those as the token body so "cos" ≠ "cos-adj".
      var beforeOk = !before || /[^a-z0-9_-]/.test(before);
      var afterOk = !after || /[^a-z0-9_-]/.test(after);
      if (beforeOk && afterOk) return true;
      i += d.length;
    }
    return false;
  }
  function ledgerMatchesDesk(entry, desk) {
    if (!desk) return false;
    var d = ledgerParticipant(desk);
    if (entry.parsed) {
      return ledgerParticipant(entry.from) === d || ledgerParticipant(entry.to) === d;
    }
    return rawHasParticipant(entry.raw, d);
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
    // #524: loop_posture is distinct from pane state (idle ≠ parked).
    var posture = String(a.loop_posture || "");
    var visualState = operatorVisualState(state, posture);
    var usageValue = usageText(a.usage);
    var usage = usageValue
      ? '<span class="desk-usage" title="provider usage remaining">' + escapeHtml(usageValue) + '</span>'
      : "";
    var stale = fresh.state === "stale";
    card.innerHTML =
      '<article class="desk conv-desk-mini' + (stale ? " desk-stale" : "") + '">' +
        '<div class="desk-rail ' + deskStateClass(visualState) + '" aria-hidden="true"></div>' +
        '<div class="desk-body">' +
          '<header class="desk-head">' +
            '<span class="desk-name">' + escapeHtml(selectedDesk) + "</span>" +
            '<span class="desk-state ' + deskStateClass(visualState) + '">' + escapeHtml(state) + "</span>" +
            (posture ? ('<span class="desk-loop-posture" title="loop posture">' + escapeHtml(posture) + "</span>") : "") +
          "</header>" +
          '<span class="desk-surface">' + escapeHtml(a.surface || "—") + "</span>" +
          usage +
        "</div>" +
      "</article>";
  }

  function renderConversationHeader(topology) {
    el("conv-title").textContent = selectedDesk ? selectedDesk : "Conversation";
    // Channel ids remain hidden routing metadata. Echo the same readable org context the map
    // uses instead of replacing a human group label with a snowflake in the detail header.
    var group = selectedDesk ? groupForDesk(buildRailGroups(topology), selectedDesk) : null;
    var context = group ? (group.label || "Desks") : "the fleet";
    el("conv-sub").textContent = selectedDesk
      ? ("Coordination in " + context + " — ledger filtered to this desk")
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
  function renderSessionMirror(force) {
    if (!force && composerComposeActive()) { mirrorRenderDeferred = true; return; }
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
    // Key includes the verbosity so a toggle flip (which changes what renders) forces
    // a repaint instead of dedup-skipping as unchanged.
    var key = selectedDesk + "|" + (latest.ts || "") + "|" + info.length + ":" + cheapHash(info) + "|" + mirrorVerbosity;
    if (key === lastMirrorKey) return;
    lastMirrorKey = key;
    var when = latest.ts
      ? '<time class="mirror-when" datetime="' + escapeHtml(latest.ts) + '" title="' + escapeHtml(latest.ts) + '">' + escapeHtml(relTime(latest.ts)) + "</time>"
      : "";
    var body = escapeHtml(latest.info || "").replace(/\r?\n/g, "<br>");
    el("conv-map").innerHTML =
      '<span class="conv-map-label">session mirror ' + when + "</span>" +
      '<div class="mirror-glance">' + body + "</div>" + debugBlock(latest);
  }

  // debugBlock renders one entry's collapsible debug tier (design §2.3 UI half),
  // shown only when the verbosity toggle is on "debug" AND the entry carries debug
  // detail — otherwise "" (the info tier stays clean). The reader-map envelope is
  // rendered as labeled rows (anchor / delta / decision / audience — the mental-map
  // fields, not a raw JSON blob), then the mirror note and any firewall warn-terms.
  function debugBlock(entry) {
    if (mirrorVerbosity !== "debug") return "";
    var d = entry && entry.debug;
    if (!d) return "";
    var parts = [];
    var env = d.envelope;
    if (env) {
      var rows = [["anchor", env.anchor], ["delta", env.delta], ["decision", env.decision], ["audience", env.audience]]
        .filter(function (r) { return r[1]; })
        .map(function (r) { return '<div class="dbg-row"><span class="dbg-key">' + r[0] + '</span><span class="dbg-val">' + escapeHtml(String(r[1])) + "</span></div>"; })
        .join("");
      if (rows) parts.push('<div class="dbg-group"><div class="dbg-group-h">reader-map envelope</div>' + rows + "</div>");
    }
    if (d.mirror_note) {
      parts.push('<div class="dbg-row"><span class="dbg-key">mirror note</span><span class="dbg-val">' + escapeHtml(d.mirror_note) + "</span></div>");
    }
    if (d.firewall && Array.isArray(d.firewall.warn_terms) && d.firewall.warn_terms.length) {
      parts.push('<div class="dbg-row dbg-firewall"><span class="dbg-key">firewall</span><span class="dbg-val">' + escapeHtml(d.firewall.warn_terms.join(", ")) + "</span></div>");
    }
    if (!parts.length) return "";
    return '<details class="thread-debug"><summary>debug detail</summary><div class="dbg-body">' + parts.join("") + "</div></details>";
  }

  // setMirrorVerbosity flips the detail level and repaints both mirror surfaces.
  function setMirrorVerbosity(level) {
    mirrorVerbosity = level === "debug" ? "debug" : "info";
    var btns = document.querySelectorAll(".mv-btn");
    for (var i = 0; i < btns.length; i++) {
      var on = btns[i].getAttribute("data-verbosity") === mirrorVerbosity;
      btns[i].classList.toggle("active", on);
      btns[i].setAttribute("aria-pressed", String(on));
    }
    paintMirror(true);
  }

  // fetchMirror loads the selected desk's session mirror and re-renders the glance.
  // Guarded on selectedDesk so a slow response for a de-selected desk is dropped.
  // limit=500 is the thread-merge tail (the glance still uses only the last entry).
  // A 100-line cap silently dropped half of a busy coordinator's mirror (#518 live
  // probe: cos had 200 on disk; the thread only saw 100).
  // paintMirror re-renders BOTH mirror-dependent views — the glance (latest entry)
  // and the thread (which now interleaves the full mirror history with the ledger).
  function paintMirror(force) {
    renderSessionMirror(force);
    renderThread(cache.history || {}, force);
  }
  function fetchMirror() {
    var want = selectedDesk;
    if (!want) { cache.mirror = null; paintMirror(); return Promise.resolve(); }
    return getJSON("/api/session-mirror?agent=" + encodeURIComponent(want) + "&limit=500").then(function (d) {
      if (selectedDesk === want) { cache.mirror = d; paintMirror(); }
    }).catch(function (err) {
      if (selectedDesk === want) { cache.mirror = { agent: want, entries: [], error: err.message }; paintMirror(); }
    });
  }

  // speakerHue maps a speaker name to a STABLE hue, so each participant keeps one
  // colour across the thread (turn-by-turn, colour-coded by speaker — #302).
  function speakerHue(name) {
    // Normalize the same way thread identity is matched elsewhere
    // (case-insensitive, whitespace-trimmed) so casing/spacing variants of one
    // speaker resolve to a SINGLE colour, not several.
    var h = 0, s = String(name || "").trim().toLowerCase();
    for (var i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
    return h % 360;
  }

  // mirrorEntriesForSelected returns the selected desk's session-mirror entries
  // (ascending oldest→newest), or [] — guarded on identity so a stale cache.mirror
  // still holding the PREVIOUS desk's doc never leaks into this desk's thread (the
  // same cross-desk guard renderSessionMirror uses, #300 trio finding).
  function mirrorEntriesForSelected() {
    var doc = cache.mirror;
    if (!doc || !selectedDesk) return [];
    if (doc.agent && String(doc.agent).toLowerCase() !== String(selectedDesk).toLowerCase()) return [];
    return Array.isArray(doc.entries) ? doc.entries : [];
  }

  var lastThreadKey = null; // dedup so an unchanged SSE/mirror tick doesn't re-announce the log / reset scroll

  // renderThread interleaves TWO streams for the selected desk into one
  // chronological timeline (design §2.4, Inc 2): the CoS relay ledger (messages
  // to/from the desk) and the desk's OWN session-mirror turn-finals (its session
  // output). Both are RFC3339-stamped — the ledger arrives newest-first, the mirror
  // newest-last — so each is normalized to a parsed sort key and the merged list is
  // ordered newest-first (matching the pre-merge thread order). Relay lines and
  // session output are visually distinguished; the same per-speaker hue ties a
  // desk's session output to its own relay lines. The merge is universal (it applies
  // to coordinator and execution desks alike — both read as one timeline of "what
  // was said to/from me" + "what I turned-final"), so no role branch is needed.
  // isCoordinatorThread reports whether the selected thread is a coordinator's (XO / CoS).
  function isCoordinatorThread() {
    var d = String(selectedDesk || "").toLowerCase();
    return coordinatorNames().some(function (n) { return String(n).toLowerCase() === d; });
  }
  // coordinatorHistoryNote calibrates the coordinator thread honestly (#405/#406, backfill =
  // forward-only): the coordinator's PRE-fix turns were withheld by the firewall bug and were
  // never durably captured, so the thread's recorded history begins at its first entry. Rather
  // than pad a misleadingly-thin past, we mark where the real history starts. Coordinator only.
  function coordinatorHistoryNote(items) {
    if (!isCoordinatorThread() || !items.length) return "";
    var first = items[0]; // ascending order → items[0] is the oldest recorded turn
    var ts = first.kind === "mirror" ? (first.m && first.m.ts) : (first.e && first.e.parsed ? first.e.time : "");
    var when = "";
    if (ts) {
      var t = Date.parse(ts);
      if (!isNaN(t)) when = " " + new Date(t).toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
    }
    return '<div class="thread-calib">History begins' + escapeHtml(when) +
      ' — earlier coordinator turns weren’t recorded (a firewall issue, since fixed). Shown from here down.</div>';
  }
  // #518: web composer delivers via /api/control/route. #432 now appends CosLedger on
  // delivered, but the next /api/history tick is async — keep an in-memory optimistic
  // line per successful deliver so the operator sees their own words immediately;
  // prune when a matching ledger line appears or after a short TTL.
  var optimisticOut = []; // {id, target, body, ts, t}
  var OPTIMISTIC_TTL_MS = 5 * 60 * 1000;
  var MOBILE_THREAD_INITIAL = 3;
  var MOBILE_THREAD_BATCH = 8;
  var mobileThreadVisible = MOBILE_THREAD_INITIAL;
  var mobileThreadHidden = 0;
  var expandedThreadMessages = Object.create(null);
  function mobileThreadWindowActive() {
    return window.matchMedia && window.matchMedia("(max-width: 640px)").matches;
  }
  function threadItemKey(it) {
    if (it.kind === "mirror") {
      return "m:" + (it.m.ts || "") + ":" + cheapHash(it.m.info || "") + ":" + (it.m.suppressed ? "1" : "0");
    }
    if (it.kind === "optimistic") return "o:" + (it.o.id || "") + ":" + cheapHash(it.o.body || "");
    return "l:" + (it.e.parsed ? it.e.time : "") + ":" + cheapHash(it.e.parsed ? (it.e.body || it.e.gist) : it.e.raw);
  }
  function threadItemHTML(it) {
    var key = threadItemKey(it);
    var expanded = !!expandedThreadMessages[key];
    var message = it.kind === "mirror" ? threadMirrorMsg(it.m) :
      (it.kind === "optimistic" ? threadOptimisticMsg(it.o) : threadLedgerMsg(it.e));
    // Every clamp candidate gets a control in the DOM. syncThreadMessageToggles
    // reveals it from measured overflow, never a character-count proxy (#689).
    var toggle = mobileThreadWindowActive()
      ? '<button type="button" class="thread-message-toggle" data-thread-expand="' + escapeHtml(key) + '" aria-expanded="' + String(expanded) + '"' + (expanded ? "" : " hidden") + '>' + (expanded ? "Show less" : "Show full") + "</button>"
      : "";
    return '<div class="thread-window-item' + (expanded ? " is-expanded" : "") + '" data-thread-item-key="' + escapeHtml(key) + '">' + message + toggle + "</div>";
  }
  function syncThreadMessageToggles(thread) {
    if (!thread || !mobileThreadWindowActive()) return;
    thread.querySelectorAll(".thread-window-item").forEach(function (item) {
      var body = item.querySelector(".thread-gist, .thread-mirror-body");
      var toggle = item.querySelector("[data-thread-expand]");
      if (!body || !toggle) return;
      var expanded = item.classList.contains("is-expanded");
      toggle.hidden = !expanded && body.scrollHeight <= body.clientHeight + 1;
    });
  }
  function appendOptimisticOutbound(target, body) {
    optimisticOut.push({
      id: "opt-" + Date.now().toString(36) + "-" + Math.random().toString(36).slice(2, 8),
      target: target,
      body: body,
      ts: new Date().toISOString(),
      t: Date.now(),
    });
  }
  function pruneOptimistic(ledger, desk) {
    var now = Date.now();
    var deskKey = ledgerParticipant(desk);
    optimisticOut = optimisticOut.filter(function (o) {
      if (now - o.t > OPTIMISTIC_TTL_MS) return false;
      if (ledgerParticipant(o.target) !== deskKey) return true; // keep other desks' pending
      // Drop when a real ledger line carries the same operator→desk body (leg 2 parity).
      for (var i = 0; i < ledger.length; i++) {
        var e = ledger[i];
        if (!e || !e.parsed) continue;
        if (ledgerParticipant(e.to) !== deskKey) continue;
        var from = ledgerParticipant(e.from);
        if (from !== "operator" && from.indexOf("operator") !== 0) continue;
        var text = String(e.body || e.gist || "");
        if (text === o.body || text.indexOf(o.body) === 0) return false;
      }
      return true;
    });
  }
  function renderThread(history, force) {
    if (!force && composerComposeActive()) { threadRenderDeferred = true; return; }
    var thread = el("conv-thread");
    if (!selectedDesk) {
      if (lastThreadKey === "@none") return;
      lastThreadKey = "@none";
      thread.innerHTML = '<div class="empty">Pick a desk to read its coordination thread.</div>';
      return;
    }
    if (!historyMatchesSelected(history)) {
      var loadingKey = "@loading:" + selectedDesk;
      if (lastThreadKey !== loadingKey) {
        lastThreadKey = loadingKey;
        thread.innerHTML = '<div class="empty">Loading recent coordination history…</div>';
      }
      return;
    }
    var ledger = (history && Array.isArray(history.ledger)) ? history.ledger : [];
    var loadError = history && history.error ? String(history.error) : "";
    pruneOptimistic(ledger, selectedDesk);
    var items = [];
    ledger.forEach(function (e) {
      if (!ledgerMatchesDesk(e, selectedDesk)) return;
      items.push({ kind: "ledger", t: Date.parse(e.parsed ? e.time : ""), e: e });
    });
    mirrorEntriesForSelected().forEach(function (m) {
      items.push({ kind: "mirror", t: Date.parse(m.ts), m: m });
    });
    // Optimistic operator→desk lines for THIS desk only (#518).
    var deskKey = ledgerParticipant(selectedDesk);
    optimisticOut.forEach(function (o) {
      if (ledgerParticipant(o.target) !== deskKey) return;
      items.push({ kind: "optimistic", t: o.t, o: o });
    });
    if (!items.length) {
      var emptyKey = "@empty:" + selectedDesk + ":" + loadError;
      if (lastThreadKey === emptyKey) return;
      lastThreadKey = emptyKey;
      if (loadError) {
        thread.innerHTML = '<div class="empty thread-load-error" role="alert">Could not load coordination history (' + escapeHtml(loadError) + ").</div>";
        return;
      }
      // Coordinator empty state is honest about the dual feed (relay ledger + session-
      // mirror) so a first open of the CoS thread after #575–#578 doesn't read as a
      // broken surface — "desk" framing was wrong for the coordinator pin (F#383).
      var emptyMsg = isCoordinatorThread()
        ? "No coordination history for this coordinator yet. Relay ledger lines and session-mirror turn-finals appear here once recorded."
        : "No coordination history for this desk yet.";
      thread.innerHTML = '<div class="empty">' + emptyMsg + "</div>";
      return;
    }
    // Oldest-first (ascending) — chat convention: the latest message is at the BOTTOM,
    // with the composer directly beneath it (F#383 criterion 5). Unparseable timestamps
    // (rare malformed/raw ledger lines with no time) sort to the TOP (treated as oldest);
    // Array.sort is stable (ES2019+) so their relative order is preserved.
    items.sort(function (a, b) {
      var at = isNaN(a.t) ? -Infinity : a.t, bt = isNaN(b.t) ? -Infinity : b.t;
      return at - bt;
    });
    // Dedup: skip the innerHTML rewrite (which re-announces via the log's aria-live
    // AND resets the operator's scroll) when the merged timeline is unchanged. The
    // key folds each item's timestamp + a content hash so a same-second new entry
    // still re-renders (mirrors the #300 glance dedup discipline).
    var mobileWindow = mobileThreadWindowActive();
    mobileThreadHidden = mobileWindow ? Math.max(0, items.length - mobileThreadVisible) : 0;
    var visibleItems = mobileThreadHidden ? items.slice(items.length - mobileThreadVisible) : items;
    var sig = selectedDesk + "#" + mirrorVerbosity + "#error:" + cheapHash(loadError) +
      "#window:" + (mobileWindow ? mobileThreadVisible : "all") + "#" + items.map(threadItemKey).join("|");
    if (sig === lastThreadKey) return;
    lastThreadKey = sig;
    var errorNotice = loadError
      ? '<div class="thread-load-error" role="alert">Could not refresh coordination history (' + escapeHtml(loadError) + "). Showing the last loaded messages.</div>"
      : "";
    var windowControl = mobileThreadHidden
      ? '<button type="button" class="thread-window-more" data-thread-window-more>↑ Show ' + Math.min(MOBILE_THREAD_BATCH, mobileThreadHidden) + " earlier · " + mobileThreadHidden + " cached</button>"
      : "";
    thread.innerHTML = errorNotice + coordinatorHistoryNote(items) + windowControl + visibleItems.map(threadItemHTML).join("");
    syncThreadMessageToggles(thread);
    requestAnimationFrame(function () { syncThreadMessageToggles(thread); });
    // Latest-at-bottom scroll discipline (F#383 criterion 5): if the operator is pinned to
    // the bottom (the default, and whenever they scroll back down), keep the newest message
    // in view; if they've scrolled UP into history, don't yank them — surface a jump-to-latest
    // chip instead so a live tick can't steal their place.
    if (threadPinned) scrollThreadToBottom();
    else showThreadJump(true);
  }

  // ── thread composer + latest-at-bottom scroll (F#383 criteria 4 + 5) ──────────────
  // composerComposeActive is true while the operator has a NON-EMPTY draft on the
  // thread composer. Live SSE/mirror ticks must NOT rewrite the thread or the
  // session-mirror glance during compose (aria-live re-announce + scroll reset
  // steals focus and feels like an adjutant interrupt — flotilla#517).
  // #518: empty-focus alone is NOT compose-active. After a successful send the
  // textarea is focused+empty; ticks and refresh() must still paint the optimistic
  // outbound line and the desk's reply. Protecting the draft (the operator's words)
  // is the load-bearing arm; empty focus was blocking post-send flush.
  var mirrorRenderDeferred = false;
  var threadRenderDeferred = false;
  function composerComposeActive() {
    var ta = el("thread-composer-input"), form = el("thread-composer");
    if (!ta || !form || form.hidden) return false;
    return ta.value.length > 0;
  }
  function flushDeferredMirrorPaint() {
    if (!mirrorRenderDeferred && !threadRenderDeferred) return;
    mirrorRenderDeferred = false;
    threadRenderDeferred = false;
    // Force repaint even when a non-empty draft remains after blur — automatic ticks
    // defer, but an explicit flush must not re-enter the compose guard (#517).
    paintMirror(true);
  }
  var threadPinned = true; // true ⇒ keep the newest message in view on each render
  function scrollThreadToBottom() {
    var t = el("conv-thread");
    if (t) t.scrollTop = t.scrollHeight;
  }
  function showThreadJump(on) {
    var j = el("thread-jump");
    if (j) j.hidden = !on;
  }
  // A render for a NEWLY selected desk should always open at the bottom (freshest first-read).
  function resetThreadScroll() {
    threadPinned = true;
    mobileThreadVisible = MOBILE_THREAD_INITIAL;
    mobileThreadHidden = 0;
    expandedThreadMessages = Object.create(null);
    showThreadJump(false);
    // A new selection gives a FRESH composer for that desk — the prior desk's draft, status
    // line, and grown height must not bleed into the newly-selected desk's composer (cubic P3).
    var m = el("thread-composer-msg");
    if (m) { m.textContent = ""; m.className = "form-msg"; }
    var ta = el("thread-composer-input");
    if (ta) { ta.value = ""; ta.style.height = ""; }
  }

  // threadLedgerMsg renders one CoS relay-ledger line (a message to/from the desk).
  function threadLedgerMsg(e) {
    if (!e.parsed) {
      return '<div class="thread-msg thread-raw">' + escapeHtml(e.raw) + "</div>";
    }
    var deskKey = ledgerParticipant(selectedDesk);
    var outbound = ledgerParticipant(e.from) === deskKey;
    var cls = outbound ? "thread-out" : "thread-in";
    var hue = speakerHue(e.from); // colour the turn by its speaker
    return (
      '<div class="thread-msg ' + cls + '" style="--spk:hsl(' + hue + ' 55% 62%)">' +
        '<header class="thread-head">' +
          '<span class="thread-route"><b class="thread-from">' + escapeHtml(e.from) + "</b> &rarr; " + escapeHtml(e.to) + "</span>" +
          '<time class="thread-time">' + escapeHtml(e.time) + "</time>" +
        "</header>" +
        (e.channel && e.channel !== "-"
          ? '<span class="thread-chan muted">#' + escapeHtml(e.channel) + "</span>"
          : "") +
        // Render the FULL body when the companion store hydrated it (#407 — the audit gist
        // is clamped to keep the ledger line atomic; the thread must never show that clamped
        // copy as if it were the whole message). Falls back to the gist for short messages
        // and pre-#407 lines.
        '<p class="thread-gist">' + escapeHtml(e.body || e.gist) + "</p>" +
      "</div>"
    );
  }

  // threadOptimisticMsg — #518 interim operator voice for web-composer delivers.
  // Fills the gap until /api/history reflects the #432 CosLedger append (or TTL).
  function threadOptimisticMsg(o) {
    var hue = speakerHue("operator");
    return (
      '<div class="thread-msg thread-out thread-optimistic" data-optimistic-id="' + escapeHtml(o.id || "") + '" style="--spk:hsl(' + hue + ' 55% 62%)">' +
        '<header class="thread-head">' +
          '<span class="thread-route"><b class="thread-from">operator</b> &rarr; ' + escapeHtml(o.target) +
            ' <span class="thread-kind">dash</span></span>' +
          '<time class="thread-time" datetime="' + escapeHtml(o.ts || "") + '">' + escapeHtml(relTime(o.ts)) + "</time>" +
        "</header>" +
        '<p class="thread-gist">' + escapeHtml(o.body) + "</p>" +
      "</div>"
    );
  }

  // threadMirrorMsg renders a session-mirror entry (the desk's own turn-final at
  // info level) as a distinct "session" turn, hue-matched to the desk's speaker
  // colour so it reads as the same participant's output alongside its relay lines.
  function threadMirrorMsgFor(agent, m) {
    var hue = speakerHue(agent);
    var body = escapeHtml(m.info || "").replace(/\r?\n/g, "<br>");
    // #406 fix-forward: a firewall-refused turn is kept in the PRIVATE dash but was never posted
    // to the public channel — render that honestly so a withheld turn is not mistaken for published.
    var withheld = m.suppressed
      ? ' <span class="thread-withheld" title="Kept in your private dashboard but withheld from the public channel by the partition firewall.">withheld from public</span>'
      : "";
    return (
      '<div class="thread-msg thread-mirror' + (m.suppressed ? " is-withheld" : "") + '" style="--spk:hsl(' + hue + ' 55% 62%)">' +
        '<header class="thread-head">' +
          '<span class="thread-route"><b class="thread-from">' + escapeHtml(agent) + "</b> " +
            '<span class="thread-kind">session</span>' + withheld + "</span>" +
          '<time class="thread-time" datetime="' + escapeHtml(m.ts || "") + '" title="' + escapeHtml(m.ts || "") + '">' + escapeHtml(relTime(m.ts)) + "</time>" +
        "</header>" +
        '<div class="thread-mirror-body">' + (body || '<span class="muted">(no session output)</span>') + "</div>" +
        debugBlock(m) +
      "</div>"
    );
  }
  function threadMirrorMsg(m) { return threadMirrorMsgFor(selectedDesk, m); }
  function renderMirrorEntries(agent, entries) {
    return (Array.isArray(entries) ? entries : []).map(function (m) { return threadMirrorMsgFor(agent, m); }).join("");
  }
  function renderOperatorInject(target, body, ts) {
    return threadOptimisticMsg({ id: "wc-local", target: target, body: body, ts: ts || new Date().toISOString() });
  }
  window.flotillaDash.renderMirrorEntries = renderMirrorEntries;
  window.flotillaDash.renderOperatorInject = renderOperatorInject;
  window.flotillaDash.relativeTime = relTime;

  // queueVisibleForDesk filters the fleet backlog to the selected desk/coordinator (#421).
  // Items with an explicit @desk / →desk scope show only on that desk; unscoped items are
  // coordinator/XO-level and show on the channel hub (xo_agent) or fleet xo.
  function queueVisibleForDesk(item, desk, status, topology) {
    if (!desk || !item) return false;
    var deskL = String(desk).toLowerCase();
    var scope = String(item.scope || "").toLowerCase();
    if (scope) return scope === deskL;
    var xo = ((status || {}).xo || "").toLowerCase();
    if (deskL === xo) return true;
    var groups = buildRailGroups(topology || {});
    for (var i = 0; i < groups.length; i++) {
      var g = groups[i];
      var desks = g.desks || [];
      for (var j = 0; j < desks.length; j++) {
        var channel = desks[j].channel_id || g.channel_id || "";
        if (selectedChannel && channel !== selectedChannel) continue;
        if (desks[j].role === "xo" && String(desks[j].name || "").toLowerCase() === deskL) return true;
      }
    }
    return false;
  }

  function renderBacklogStrip(history, status, topology) {
    var bl = (history && history.backlog) ? history.backlog : {};
    var box = el("conv-backlog");
    var allItems = Array.isArray(bl.unblocked) ? bl.unblocked : [];
    queueItems = allItems.filter(function (item) {
      return queueVisibleForDesk(item, selectedDesk, status, topology);
    });
    var scopeLab = el("conv-queue-scope");
    if (scopeLab) {
      scopeLab.textContent = selectedDesk
        ? ("scoped to " + selectedDesk + (queueItems.length ? " · " + queueItems.length + " item" + (queueItems.length === 1 ? "" : "s") : ""))
        : "";
    }
    var counts =
      '<div class="backlog-counts">' +
        '<span>' + queueItems.length + " shown</span>" +
        (allItems.length !== queueItems.length ? '<span class="muted">' + allItems.length + " fleet-wide</span>" : "") +
        '<span class="count-blocked">' + (bl.blocked || 0) + " blocked</span>" +
        (bl.awaiting_auth ? '<span class="count-awaiting-auth">' + bl.awaiting_auth + " awaiting-auth</span>" : "") +
        '<span class="count-done">' + (bl.done || 0) + " done</span>" +
      "</div>";
    var items = queueItems.length
      ? queueItems.map(function (item, idx) { return backlogItem(item, idx); }).join("")
      : (bl.found
        ? (selectedDesk ? '<div class="empty">No work queued for ' + escapeHtml(selectedDesk) + ".</div>" : '<div class="empty">Select a desk.</div>')
        : '<div class="empty">No backlog section found.</div>');
    box.innerHTML = counts + items;
  }

  // backlogItem renders one structured queue row (#302 list, #405 grid, #419 operator
  // layer): status chip + operator-facing title. Clicking opens the reader-modeled modal.
  function backlogItem(item, idx) {
    if (!item || typeof item !== "object") {
      item = { title: String(item == null ? "" : item), raw: String(item == null ? "" : item) };
    }
    var marker = (item.status || "").toLowerCase();
    var title = item.title || "Work item";
    var markerLabel = marker ? marker.replace(/-/g, " ") : "";
    if (!marker) {
      return '<div class="backlog-item bq-row" role="button" tabindex="0" data-bq-open data-bq-index="' + idx + '">' +
        '<span class="bq-text bq-text-wide">' + escapeHtml(title) + "</span></div>";
    }
    return '<div class="backlog-item bq-row bq-' + escapeHtml(marker) + '" role="button" tabindex="0" data-bq-open data-bq-index="' + idx + '">' +
      '<span class="bq-marker">' + escapeHtml(markerLabel) + "</span>" +
      '<span class="bq-text">' + escapeHtml(title) + "</span></div>";
  }

  function renderConversations() {
    var status = cache.status || {};
    var topology = cache.topology || {};
    var history = cache.history || {};
    var fresh = renderFreshness(status);
    renderUtilization(status);
    renderLiveSwarm(status);
    renderRailMeta(status, fresh);
    renderConversationRail(status, topology, fresh);
    renderConversationHeader(topology);
    renderDeskCard(status, fresh);
    renderSessionMirror();
    renderThread(history);
    renderHistoryPager(history);
    renderBacklogStrip(history, status, topology);
    syncComposer();
  }

  // syncComposer shows the thread composer for the selected desk/coordinator and labels it
  // with that target — so the operator can type + send from the thread they're reading
  // (F#383 criterion 4: presence AND discoverability). Hidden when nothing is selected.
  function syncComposer() {
    var form = el("thread-composer");
    var ta = el("thread-composer-input");
    if (!form || !ta) return;
    if (selectedDesk) {
      form.hidden = false;
      ta.placeholder = "Message " + selectedDesk + "…";
    } else {
      form.hidden = true;
    }
    // #747: Conversations is minimally usable only after its three startup read
    // models have settled (success or honest error) and this paint completed.
    if (cache.status && cache.topology && cache.history && window.flotillaPerf) {
      window.flotillaPerf.viewRendered("conversations");
    }
  }

  /* ── refresh orchestration ───────────────────────────────────────────── */
  var refreshEpoch = 0;
  var historyEpoch = 0;
  var startupWaterfallSignaled = false;

  function historyMatchesSelected(history) {
    return !!(selectedDesk && history && agentKey(history.desk) === agentKey(selectedDesk));
  }

  function historyURL(desk, cursor, meta) {
    var q = meta ? "meta=1" : "desk=" + encodeURIComponent(desk || "") + "&limit=50";
    if (cursor) q += "&cursor=" + encodeURIComponent(cursor);
    return "/api/history?" + q;
  }

  function renderHistoryPager(history) {
    var btn = el("thread-load-earlier");
    if (!btn) return;
    btn.hidden = mobileThreadHidden > 0 || !(historyMatchesSelected(history) && history.has_more);
    if (!btn.disabled) btn.textContent = "↑ Load earlier";
  }

  // Fetch exactly one selected-desk page. Reset replaces the recent window; a
  // cursor fetch extends it toward older entries without ever requesting the
  // fleet-wide ledger. A selection/refresh epoch prevents late pages crossing desks.
  function fetchSelectedHistory(reset) {
    var desk = selectedDesk;
    if (!desk) return Promise.resolve();
    var prior = cache.history;
    var lastGood = reset && historyMatchesSelected(prior) && !prior.error ? prior : null;
    var cursor = reset ? "" : (historyMatchesSelected(prior) ? prior.next_cursor : "");
    if (!reset && (!prior || !prior.has_more || !cursor)) return Promise.resolve();
    var epoch = ++historyEpoch;
    return getJSON(historyURL(desk, cursor, false)).then(function (doc) {
      if (epoch !== historyEpoch || agentKey(selectedDesk) !== agentKey(desk)) return;
      if (!reset && historyMatchesSelected(prior)) {
        doc.ledger = (prior.ledger || []).concat(doc.ledger || []);
      }
      cache.history = doc;
      lastThreadKey = "";
      renderConversations();
      unseenSigs.conversations = computeConvSig();
      resolvePending("conversations");
      refreshDots();
    }).catch(function (err) {
      if (epoch !== historyEpoch || agentKey(selectedDesk) !== agentKey(desk)) return;
      if (reset) {
        cache.history = lastGood || { desk: desk, ledger: [], backlog: { found: false, unblocked: [] } };
        cache.history.error = err.message;
        lastThreadKey = "";
        renderConversations();
      }
    });
  }

  function refreshSelectedHistory() {
    if (!historyMatchesSelected(cache.history)) return fetchSelectedHistory(true);
    var known = cache.history;
    return getJSON(historyURL("", "", true)).then(function (meta) {
      if (!historyMatchesSelected(known)) return fetchSelectedHistory(true);
      if (meta.ledger_signature !== known.ledger_signature) return fetchSelectedHistory(true);
      known.backlog = meta.backlog;
      known.backlog_signature = meta.backlog_signature;
      delete known.error;
      cache.history = known;
    }).catch(function () {
      // Metadata is an optimization. Retain the last honest selected-desk page
      // when it is temporarily unavailable; the next live tick retries it.
    });
  }

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

    return Promise.all([pStatus, pTopo]).then(function () {
      if (!current()) return;
      ensureSelection(cache.status || {}, buildRailGroups(cache.topology || {}));
      return refreshSelectedHistory();
    }).then(function () {
      if (current()) {
        renderConversations();
        var mirrorSettled = fetchMirror(); // keep the selected desk's session-mirror glance current on each tick
        // Update the conversations unseen dot from the freshly-loaded ledger.
        unseenSigs.conversations = computeConvSig();
        resolvePending("conversations");
        refreshDots();
        // Peek the other tabs' data sources to keep their unseen dots current.
        // Fire-and-forget: errors are swallowed inside each peek so a failing
        // endpoint never blocks the conversations render.
        var startupPeeks = [peekGoalsSig(), peekIssuesSig(), peekParadeSig(), mirrorSettled];
        if (!startupWaterfallSignaled && window.flotillaPerf) {
          startupWaterfallSignaled = true;
          Promise.all(startupPeeks).then(function () { window.flotillaPerf.startupSettled(); });
        }
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
      notifyLiveUpdate();
    });
    es.onopen = function () { setConn("live"); stopPolling(); };
    es.onerror = function () {
      setConn("down");
      if (!pollTimer) pollTimer = setInterval(refresh, POLL_FALLBACK_MS);
    };
  }

  connect();

  /* ── unseen-content dots per tab (item 9) ────────────────────────────────
     Per-tab localStorage key: flotilla-tab-sig-{tabname}
     Signature sources:
       conversations — latest ledger ts + count (from cache.history; available each refresh)
       goals         — version + counts from /api/goals (peeked once per refresh cycle)
       issues        — issue count + newest updatedAt from /api/issues (peeked once)
       parade        — newest parade date from /api/parades (peeked once)
     Dot shows when currentSig !== storedSig; clears when operator opens that tab.
     Per-browser client signal — localStorage is not synced across devices.
  ─────────────────────────────────────────────────────────────────────────── */
  var unseenSigs = {};
  // pendingView tracks a tab the operator has OPENED while its signature is not yet
  // loaded (the async peek hasn't landed). markTabViewed can't store an empty sig, so
  // it records the intent here; when the peek resolves, it stores the now-known sig so
  // the dot stays cleared. Fixes the fast-click race on the Parade nav-out (cubic #416 P2).
  var pendingView = {};

  function unseenKey(tab) { return "flotilla-tab-sig-" + tab; }
  function unseenDot(tab) { return document.getElementById("dot-" + tab); }

  function setDot(tab, active) {
    var dot = unseenDot(tab);
    if (!dot) return;
    var val = active ? "true" : "false";
    if (dot.getAttribute("data-active") === val) return;
    dot.setAttribute("data-active", val);
    dot.setAttribute("aria-hidden", active ? "false" : "true");
  }

  function refreshDots() {
    ["conversations", "goals", "issues", "parade"].forEach(function (tab) {
      var sig = unseenSigs[tab] || "";
      if (!sig) return; // no data yet — leave dot hidden

      // If this tab's SPA view is currently visible (not hidden), the operator is
      // already looking at it — silently record as viewed instead of showing a dot.
      var viewEl = el("view-" + tab);
      if (viewEl && !viewEl.classList.contains("hidden")) {
        markTabViewed(tab);
        return;
      }

      var stored = "";
      try { stored = localStorage.getItem(unseenKey(tab)) || ""; } catch (e) {}
      setDot(tab, sig !== stored);
    });
  }

  function markTabViewed(tab) {
    var sig = unseenSigs[tab] || "";
    if (!sig) {
      // The signature hasn't loaded yet — remember that the operator opened this tab
      // and kick the peek so the sig arrives ASAP; the peek stores it on resolution.
      pendingView[tab] = true;
      peekTab(tab);
      return;
    }
    delete pendingView[tab];
    try { localStorage.setItem(unseenKey(tab), sig); } catch (e) {}
    setDot(tab, false);
  }

  // peekTab (re)fetches the signature for one tab. Returns the fetch promise (or a
  // resolved promise for conversations, whose sig comes from the shared refresh cache).
  function peekTab(tab) {
    if (tab === "goals") return peekGoalsSig();
    if (tab === "issues") return peekIssuesSig();
    if (tab === "parade") return peekParadeSig();
    return Promise.resolve();
  }

  // resolvePending stores the now-known signature for a tab the operator already opened
  // while its sig was still loading — so the dot stays cleared once the peek lands.
  function resolvePending(tab) {
    if (pendingView[tab] && unseenSigs[tab]) markTabViewed(tab);
  }

  // Server file signatures cover the full bounded source without loading it into the
  // browser. They also keep backlog-only changes distinct from conversation changes.
  function computeConvSig() {
    var hist = cache.history || {};
    return String(hist.ledger_signature || "") + "|" + String(hist.backlog_signature || "");
  }

  // peekGoalsSig fetches /api/goals to derive the goals tab signature. Errors are
  // swallowed so a missing goals doc never breaks the dot logic. Returns the promise
  // so a caller (a pending tab-view) can act once the sig lands.
  function peekGoalsSig() {
    return getJSON("/api/goals").then(function (g) {
      var v = (g && g.version != null) ? String(g.version) : "";
      var c = (g && g.counts) ? JSON.stringify(g.counts) : "";
      unseenSigs.goals = v + "|" + c;
      resolvePending("goals");
      refreshDots();
    }).catch(function () {});
  }

  // peekIssuesSig fetches /api/issues (open, up to 50) to derive the issues tab signature.
  // The tracker serializes gh's `--json` shape, so the timestamp fields are camelCase
  // (updatedAt / createdAt — see internal/dash/tracker/tracker.go Issue struct), NOT
  // snake_case. Reading updatedAt keeps the dot in sync when an existing issue is edited
  // without the count changing (cubic #416 P2).
  function peekIssuesSig() {
    return getJSON("/api/issues?state=open&limit=50").then(function (d) {
      var items = Array.isArray(d && d.issues) ? d.issues : (Array.isArray(d) ? d : []);
      var newest = items.length ? (items[0].updatedAt || items[0].createdAt || "") : "";
      unseenSigs.issues = String(items.length) + "|" + newest;
      resolvePending("issues");
      refreshDots();
    }).catch(function () {});
  }

  // peekParadeSig fetches /api/parades to derive the parade tab signature (newest date).
  function peekParadeSig() {
    return getJSON("/api/parades").then(function (d) {
      var items = Array.isArray(d && d.parades) ? d.parades : [];
      var newest = items.length ? (items[0].date || "") : "";
      unseenSigs.parade = newest;
      resolvePending("parade");
      refreshDots();
    }).catch(function () {});
  }

  /* ── tab nav: Conversations ⇄ Goals ⇄ Issues · Parade/R&D (nav-out) ─────────────── */
  var VIEWS = ["conversations", "goals", "issues"];
  // #516: the brand subtitle tracks the active SPA tab. Parade and R&D are separate
  // pages; only the three SPA views land here.
  function setBrandDash(view) {
    var b = document.querySelector(".brand-dash");
    if (b) b.textContent = view;
  }
  function showView(view) {
    if (window.flotillaPerf) window.flotillaPerf.selectView(view);
    VIEWS.forEach(function (v) {
      var on = v === view;
      el("view-" + v).classList.toggle("hidden", !on);
      el("tab-" + v).classList.toggle("active", on);
      el("tab-" + v).setAttribute("aria-selected", String(on));
    });
    setBrandDash(view);
    el("freshness").classList.toggle("hidden", view !== "conversations");
    // Conversations is the fixed single-scroll app-shell (#326): only on this tab
    // does the page itself stop scrolling. Goals/Issues keep natural page scroll.
    document.body.classList.toggle("conv-shell-active", view === "conversations");
    if (view === "conversations") {
      requestAnimationFrame(function () { syncThreadMessageToggles(el("conv-thread")); });
    }
    if (window.flotillaWorkContext) window.flotillaWorkContext.onViewChange(view);
    if (view === "goals" && window.flotillaGoals) window.flotillaGoals.show();
    if (view === "issues" && window.flotillaTracker) window.flotillaTracker.show();
    markTabViewed(view); // clear unseen dot when operator opens this tab
  }
  // #429: goals.js routes a decision card's "Drives" link back into the Goals map.
  window.flotillaDash.showView = showView;
  // Only button tabs (those with data-view) are wired to showView. The Parade <a>
  // tab navigates to /parade naturally — including firing its own click handler below.
  var tabs = document.querySelectorAll(".tab[data-view]");
  for (var i = 0; i < tabs.length; i++) {
    tabs[i].addEventListener("click", function () {
      var view = this.getAttribute("data-view");
      showView(view);
      // Capture the open goals drawer node too, so a tab-level Back/Forward that returns
      // to Goals restores the drawer instead of dropping it (cubic #351 P2).
      var node = (view === "goals" && window.flotillaGoals && window.flotillaGoals.openNode) ? window.flotillaGoals.openNode() : null;
      pushNav({ view: view, desk: selectedDesk || null, node: node });
    });
  }
  // Parade tab: mark it viewed on click so the dot clears as the operator navigates out.
  // The Parade link navigates the whole page away, unloading this script — so if the
  // signature hasn't loaded yet, a bare markTabViewed would only queue a pending write
  // that never runs (the page is gone). To fully close that race (cubic #416 P2): when
  // the sig is unknown, DEFER navigation, peek the sig, store it, then navigate. When the
  // sig is already known (the common case — the peek runs at load), store synchronously
  // and let the browser navigate normally (no preventDefault).
  var paradeTabEl = el("tab-parade");
  if (paradeTabEl) {
    paradeTabEl.addEventListener("click", function (e) {
      // A modified click (⌘/Ctrl/Shift/Alt, or a non-primary button like middle-click)
      // means "open /parade in a new tab/window" — THIS page stays loaded, so its own
      // load-time peek + resolvePending will clear the dot; never hijack it into a
      // same-tab window.location.href. Only plain left-clicks get the defer treatment
      // (cubic #416 P2 — the defer must not steal new-tab opens).
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return;
      if (unseenSigs.parade) { markTabViewed("parade"); return; } // known → store + navigate
      // Unknown sig, plain click: hold the navigation just long enough to record the view.
      e.preventDefault();
      var href = paradeTabEl.getAttribute("href") || "/parade";
      var go = function () { window.location.href = href; };
      peekParadeSig().then(function () { markTabViewed("parade"); }).then(go, go);
    });
  }

  /* ── drive-queue item modal (#349 Inc 4 E10): a queue chip lives in a narrow column,
     so clicking (or Enter/Space on) it opens the full item here, focused. Read-only —
     the queue is a status surface; acting on an item is the control forms below. ────── */
  var convModalReturn = null; // the drive-queue chip that opened the modal — refocused on close
  function openConvModal(item) {
    if (!item || typeof item !== "object") item = { title: String(item == null ? "" : item), internal: String(item == null ? "" : item) };
    var marker = (item.status || "").replace(/-/g, " ");
    var mk = el("conv-modal-marker");
    mk.textContent = marker || "";
    mk.style.display = marker ? "" : "none";
    el("conv-modal-title").textContent = item.title || "Work item";
    var summary = el("conv-modal-summary");
    var summaryText = (item.summary || "").trim();
    if (summary) {
      // Visibility keys on whether a summary STRING is present, not on the element
      // existing (it always does) — else an item with no summary shows an empty block
      // (cubic #420 P2).
      summary.textContent = summaryText;
      summary.hidden = summaryText === "";
    }
    var internal = (item.internal || item.raw || "").trim();
    var internalWrap = el("conv-modal-internal");
    var internalBody = el("conv-modal-internal-body");
    if (internal && internal !== summaryText && internal !== item.title) {
      internalBody.textContent = internal;
      internalWrap.hidden = false;
      internalWrap.open = false;
    } else {
      internalBody.textContent = "";
      internalWrap.hidden = true;
    }
    var modal = el("conv-modal");
    convModalReturn = document.activeElement; // the chip — restored on close (a11y)
    modal.classList.add("open");
    modal.setAttribute("aria-hidden", "false");
    var x = modal.querySelector(".conv-modal-x");
    if (x) x.focus();
  }
  function closeConvModal() {
    var modal = el("conv-modal");
    if (!modal.classList.contains("open")) return;
    modal.classList.remove("open");
    modal.setAttribute("aria-hidden", "true");
    // Return focus to the chip that opened it (matches the goals-modal a11y contract).
    if (convModalReturn && convModalReturn.focus && document.contains(convModalReturn)) {
      convModalReturn.focus({ preventScroll: true });
    }
    convModalReturn = null;
  }
  (function wireConvModal() {
    var backlog = el("conv-backlog");
    if (backlog) {
      backlog.addEventListener("click", function (e) {
        var item = e.target.closest ? e.target.closest("[data-bq-open]") : null;
        if (item) {
          var idx = parseInt(item.getAttribute("data-bq-index"), 10);
          openConvModal(queueItems[idx]);
        }
      });
      backlog.addEventListener("keydown", function (e) {
        if (e.key !== "Enter" && e.key !== " ") return;
        var item = e.target.closest ? e.target.closest("[data-bq-open]") : null;
        if (item) {
          e.preventDefault();
          var idx = parseInt(item.getAttribute("data-bq-index"), 10);
          openConvModal(queueItems[idx]);
        }
      });
    }
    var modal = el("conv-modal");
    if (modal) {
      modal.addEventListener("click", function (e) {
        if (e.target.hasAttribute && e.target.hasAttribute("data-conv-modal-close")) closeConvModal();
      });
      // Focus trap (aria-modal): keep Tab / Shift+Tab cycling among the modal's focusable
      // controls while it's open — Tab must not escape behind the backdrop onto the page
      // below (cubic #361 P2; mirrors the goals-modal trap). The close × is the only
      // focusable here, so both edges wrap back to it.
      modal.addEventListener("keydown", function (e) {
        if (e.key !== "Tab" || !modal.classList.contains("open")) return;
        var f = modal.querySelectorAll(".conv-modal-x");
        if (!f.length) return;
        var first = f[0], last = f[f.length - 1];
        if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
        else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
      });
    }
    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape") closeConvModal();
    });
  })();

  /* ── browser history: every view / desk / goals-node change is a reversible nav
     entry (#349 A1) — clicking into a conversation from the goals map is no longer a
     one-way trap; Back/Forward restore the prior state. pushState is best-effort
     (wrapped) so the SPA still works where history is unavailable. ─────────────────── */
  var restoringNav = false;
  function navHash(s) {
    if (!s || (s.view || "conversations") === "conversations") return s && s.desk ? "#conv/" + encodeURIComponent(s.desk) : "#conv";
    if (s.view === "goals") return s.node ? "#goals/" + encodeURIComponent(s.node) : "#goals";
    return "#" + s.view;
  }
  function pushNav(state) {
    if (restoringNav) return;
    try { history.pushState(state, "", navHash(state)); } catch (e) { /* history unavailable — nav still works */ }
  }
  window.flotillaDash.pushNav = pushNav;
  function applyNav(state) {
    // try/finally so a restore-time throw (a render/restore error) can NEVER leave the
    // guard stuck true — a stuck guard would suppress every future pushNav and silently
    // kill history for the rest of the session (cubic #354 P2).
    restoringNav = true;
    try {
      var s = state || { view: "conversations" };
      var view = s.view || "conversations";
      // Set the selection to the state's desk — including CLEARING it on a desk:null state
      // (Back to the seed must not leave the thread/header/mirror on the old desk; a null
      // selection lets renderConversations re-pick the default) — cubic #351 P2.
      if (view === "conversations") { selectedDesk = s.desk || null; selectedChannel = s.channel || null; }
      showView(view);
      if (view === "conversations") { renderConversations(); fetchSelectedHistory(true); fetchMirror(); }
      if (view === "goals" && window.flotillaGoals && window.flotillaGoals.restoreNode) {
        if (window.flotillaGoals.show) window.flotillaGoals.show(); // ensure the map is rendered first
        window.flotillaGoals.restoreNode(s.node || null);
      }
    } finally {
      restoringNav = false;
    }
  }
  window.addEventListener("popstate", function (e) { applyNav(e.state); });

  // parseHash maps a location.hash into a nav state for cold-open deep links (#579).
  // Returns null when the fragment is empty or not an SPA view — so default_view can
  // still choose the landing tab. Explicit hashes always win over default_view.
  function parseHash(raw) {
    var h = String(raw || "").replace(/^#/, "");
    if (!h) return null;
    if (h === "conv" || h.indexOf("conv/") === 0) {
      var desk = h.indexOf("conv/") === 0 ? decodeURIComponent(h.slice(5)) : "";
      return { view: "conversations", desk: desk || null, channel: null };
    }
    if (h === "goals" || h.indexOf("goals/") === 0) {
      var node = h.indexOf("goals/") === 0 ? decodeURIComponent(h.slice(6)) : "";
      return { view: "goals", node: node || null };
    }
    if (h === "issues") return { view: h };
    // #863: preserve the old dashboard deep link by routing it into the combined
    // R&D reading room instead of trying to reveal a removed Decisions panel.
    if (h === "decisions") return { view: "rd" };
    return null;
  }
  window.flotillaDash.parseHash = parseHash; // asset-lockable / goja (#579)

  // seedLanding chooses the first tab on cold open (#579):
  //   1. Explicit hash / deep link → that view (always wins).
  //   2. Else GET /api/goals/meta.default_view === true → Goals.
  //   3. Else Conversations (historical default).
  // Must NOT replaceState to #conv before the goals peek — that would mint a
  // synthetic "explicit" hash and steal the default_view branch.
  function seedLanding() {
    var fromHash = parseHash(location.hash);
    if (fromHash) {
      if (fromHash.view === "rd") {
        window.location.replace("/research?focus=decisions");
        return;
      }
      applyNav(fromHash);
      try { history.replaceState(fromHash, "", navHash(fromHash)); } catch (e) { /* ignore */ }
      return;
    }
    getJSON("/api/goals/meta").then(function (g) {
      // Operator (or another path) already chose a view while we waited — do not steal.
      if (parseHash(location.hash)) return;
      var land = (g && g.default_view)
        ? { view: "goals" }
        : { view: "conversations", desk: selectedDesk || null, channel: selectedChannel || null };
      applyNav(land);
      try { history.replaceState(land, "", navHash(land)); } catch (e) { /* ignore */ }
    }).catch(function () {
      if (parseHash(location.hash)) return;
      var land = { view: "conversations", desk: selectedDesk || null, channel: selectedChannel || null };
      applyNav(land);
      try { history.replaceState(land, "", navHash(land)); } catch (e) { /* ignore */ }
    });
  }
  seedLanding();

  // Session-mirror detail toggle (info ⇄ debug) — static chrome, wired once. Flipping
  // it repaints the glance + thread at the new tier (the debug payload is already in
  // the cache, so no fetch is needed).
  var mvBtns = document.querySelectorAll(".mv-btn");
  for (var mv = 0; mv < mvBtns.length; mv++) {
    mvBtns[mv].addEventListener("click", function () { setMirrorVerbosity(this.getAttribute("data-verbosity")); });
  }

  // openConversation is the deep-link the Goals map calls (feedback #3): select a
  // desk's conversation thread and switch to the Conversations view. Mirrors the
  // rail's own desk-click (selectedDesk + render + authoritative control targets).
  function openConversation(desk) {
    if (!desk) return;
    selectedDesk = desk;
    resetThreadScroll(); // open the deep-linked thread at its latest message
    showView("conversations");
    renderConversations();
    fetchSelectedHistory(true);
    fetchMirror(); // load the deep-linked desk's session mirror (the identity guard hides the prior desk's until it lands)
    pushNav({ view: "conversations", desk: desk }); // reversible: Back returns to the goals map (#349 A1)
    // Move focus into the now-visible Conversations view — the deep-link hid the
    // Goals view, so leaving focus on the goals node would strand it on <body>.
    var title = el("conv-title");
    if (title) { title.setAttribute("tabindex", "-1"); title.focus(); }
  }
  window.flotillaDash.openConversation = openConversation;

  // #421/#863: any [data-open-decisions] trigger (the Goals "Awaiting you" tile)
  // routes to the combined R&D reading room, already focused on waiting decisions.
  // Single owner: goals.js deliberately does not handle this attribute.
  function openDecisionsView() {
    window.location.href = "/research?focus=decisions";
  }
  document.addEventListener("click", function (e) {
    var trig = e.target.closest ? e.target.closest("[data-open-decisions]") : null;
    if (!trig) return;
    e.preventDefault();
    openDecisionsView();
  });
  document.addEventListener("keydown", function (e) {
    if (e.key !== "Enter" && e.key !== " ") return;
    var trig = e.target.closest ? e.target.closest("[data-open-decisions]") : null;
    if (!trig) return;
    e.preventDefault();
    openDecisionsView();
  });

  // #689: phone Conversations keeps the full bounded data model in memory while
  // mounting only a small recent window. Each disclosure extends that DOM window;
  // once it is exhausted, the existing cursor-backed history control takes over.
  (function wireMobileConversationDensity() {
    var thread = el("conv-thread");
    if (thread) {
      thread.addEventListener("click", function (e) {
        var more = e.target.closest ? e.target.closest("[data-thread-window-more]") : null;
        if (more) {
          var beforeTop = more.getBoundingClientRect().top;
          mobileThreadVisible += MOBILE_THREAD_BATCH;
          lastThreadKey = "";
          renderThread(cache.history, true);
          renderHistoryPager(cache.history);
          var next = thread.querySelector("[data-thread-window-more]");
          if (next) {
            window.scrollBy(0, next.getBoundingClientRect().top - beforeTop);
            next.focus();
          } else {
            var first = thread.querySelector(".thread-window-item");
            if (first) {
              first.setAttribute("tabindex", "-1");
              first.focus({ preventScroll: true });
            }
          }
          return;
        }
        var toggle = e.target.closest ? e.target.closest("[data-thread-expand]") : null;
        if (!toggle) return;
        var key = toggle.getAttribute("data-thread-expand") || "";
        var item = toggle.closest(".thread-window-item");
        var expanded = toggle.getAttribute("aria-expanded") !== "true";
        if (expanded) expandedThreadMessages[key] = true;
        else delete expandedThreadMessages[key];
        toggle.setAttribute("aria-expanded", String(expanded));
        toggle.textContent = expanded ? "Show less" : "Show full";
        if (item) item.classList.toggle("is-expanded", expanded);
      });
    }
    document.querySelectorAll("[data-conv-disclosure]").forEach(function (button) {
      button.addEventListener("click", function () {
        var panel = button.closest(".conv-nav, .conv-context");
        var expanded = button.getAttribute("aria-expanded") !== "true";
        button.setAttribute("aria-expanded", String(expanded));
        if (panel) panel.classList.toggle("mobile-expanded", expanded);
        button.textContent = button.getAttribute("data-conv-disclosure") === "nav"
          ? (expanded ? "Hide desks" : "Choose desk")
          : (expanded ? "Hide context" : "Show context");
      });
    });
    document.addEventListener("click", function (e) {
      var desk = e.target.closest ? e.target.closest(".conv-item") : null;
      if (!desk || !mobileThreadWindowActive()) return;
      var nav = desk.closest(".conv-nav");
      var button = nav ? nav.querySelector('[data-conv-disclosure="nav"]') : null;
      if (nav) nav.classList.remove("mobile-expanded");
      if (button) { button.setAttribute("aria-expanded", "false"); button.textContent = "Choose desk"; }
    });
    var media = window.matchMedia ? window.matchMedia("(max-width: 640px)") : null;
    if (media && media.addEventListener) {
      media.addEventListener("change", function () {
        mobileThreadVisible = MOBILE_THREAD_INITIAL;
        mobileThreadHidden = 0;
        lastThreadKey = "";
        renderThread(cache.history, true);
        renderHistoryPager(cache.history);
      });
    }
    window.addEventListener("resize", function () { syncThreadMessageToggles(thread); });
  })();

  (function wireHistoryPager() {
    var btn = el("thread-load-earlier");
    var thread = el("conv-thread");
    if (!btn || !thread) return;
    btn.addEventListener("click", function () {
      if (btn.disabled || !historyMatchesSelected(cache.history)) return;
      var beforeHeight = thread.scrollHeight;
      var beforeTop = thread.scrollTop;
      var hadFocus = document.activeElement === btn;
      btn.disabled = true;
      btn.textContent = "Loading…";
      threadPinned = false;
      fetchSelectedHistory(false).then(function () {
        // Older rows are prepended visually after the ascending timeline sort.
        // Offset their height so the line the operator was reading stays put.
        thread.scrollTop = beforeTop + Math.max(0, thread.scrollHeight - beforeHeight);
      }).finally(function () {
        btn.disabled = false;
        renderHistoryPager(cache.history);
        if (hadFocus && btn.hidden) {
          var title = el("conv-title");
          if (title) { title.setAttribute("tabindex", "-1"); title.focus(); }
        }
      });
    });
  })();

  // Thread composer + latest-at-bottom scroll wiring (F#383 criteria 4 + 5). The composer
  // sends to the SELECTED desk/coordinator via the same route-to-pane relay the control
  // column uses (/api/control/route) — the typed outcome is surfaced honestly (the pane
  // lock can report busy/crashed/unconfirmed; never a fake success).
  (function wireThreadComposer() {
    var thread = el("conv-thread");
    if (thread) {
      thread.addEventListener("scroll", function () {
        var nearBottom = thread.scrollHeight - thread.scrollTop - thread.clientHeight < 48;
        threadPinned = nearBottom;
        // Standard chat behavior: the jump-to-latest chip is offered whenever the operator
        // has scrolled up into history, and hides the moment they're back at the bottom.
        showThreadJump(!nearBottom);
      });
    }
    var jump = el("thread-jump");
    if (jump) jump.addEventListener("click", function () { threadPinned = true; showThreadJump(false); scrollThreadToBottom(); });

    var form = el("thread-composer"), ta = el("thread-composer-input"), msg = el("thread-composer-msg");
    function setMsg(text, kind) { if (msg) { msg.className = "form-msg" + (kind ? " " + kind : ""); msg.textContent = text; } }
    function resizeComposer() { if (ta) { ta.style.height = "auto"; ta.style.height = Math.min(ta.scrollHeight, 120) + "px"; } }
    if (form && ta) {
      // inFlight guards against a DOUBLE-SEND: a fast second Enter (or Enter+click) can
      // re-enter submit before the browser reflects btn.disabled — and requestSubmit()
      // fires the submit event regardless of the button's disabled state. Both the submit
      // handler and the Enter path check this flag, so exactly one POST goes out (cubic P2).
      var inFlight = false;
      // sameSel reports whether the currently-selected desk is still the one a send targeted —
      // the operator may have switched desks mid-send, and the shared #thread-composer-msg /
      // textarea belong to the NEW desk now, so an outcome must not land there (cubic P3).
      function sameSel(target) { return String(selectedDesk || "").toLowerCase() === String(target).toLowerCase(); }
      form.addEventListener("submit", function (ev) {
        ev.preventDefault();
        if (inFlight) return;
        var target = selectedDesk, body = ta.value.trim();
        if (!target) { setMsg("Select a desk first.", "err"); return; }
        if (!body) { setMsg("Type a message.", "err"); return; }
        var btn = form.querySelector("button");
        inFlight = true;
        setMsg("Sending…", "");
        if (btn) btn.disabled = true;
        routeMessage(target, body).then(function (res) {
          var copy = routeOutcomeCopy(res);
          var outcome = copy.outcome;
          // #518: record optimistic outbound for the TARGET desk even if the operator
          // switched selection mid-send — the line must appear when they return to that
          // thread. UI mutations (clear draft / paint / status) stay sameSel-guarded.
          // Still useful after #432 ledger append: optimistic fills the gap until refresh.
          if (outcome === "delivered") {
            appendOptimisticOutbound(target, body);
          }
          if (!sameSel(target)) return;
          if (copy.ok) {
            ta.value = "";
            resizeComposer();
          }
          if (outcome === "delivered") {
            threadPinned = true;
            lastThreadKey = null; // force paint even if mirror/ledger unchanged
            flushDeferredMirrorPaint(); // paintMirror(true) → renderThread with optimistic
            scrollThreadToBottom();
            // Re-fetch streams so ledger (#432) + desk reply land without waiting
            // solely on the next SSE tick; empty-focus is no longer compose-active.
            refresh();
          }
          setMsg(copy.text, copy.ok ? "ok" : "");
        }).catch(function (err) { if (sameSel(target)) setMsg(err.message, "err"); }).then(function () {
          inFlight = false;
          if (btn) btn.disabled = false;
        });
      });
      // Enter sends; Shift+Enter is a newline (chat convention). Guarded so a rapid
      // double-Enter can't queue a second send while one is already in flight (cubic P2).
      ta.addEventListener("keydown", function (e) {
        if (e.key === "Enter" && !e.shiftKey) {
          e.preventDefault();
          if (inFlight) return;
          if (form.requestSubmit) form.requestSubmit(); else form.dispatchEvent(new Event("submit", { cancelable: true }));
        }
      });
      ta.addEventListener("input", resizeComposer);
      ta.addEventListener("blur", function () { setTimeout(flushDeferredMirrorPaint, 0); });
    }
  })();
})();
