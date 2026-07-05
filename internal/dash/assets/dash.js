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
  // Selection is COMPOSITE — a desk name + the channel it was picked in. The SAME desk name
  // can appear in several channel groups (e.g. the CoS is the XO of fleet-command AND a member
  // of every project channel), so keying the rail highlight on the name alone lit up every
  // copy at once (#370). selectedChannel scopes the highlight (and the header context) to the
  // one row the operator picked; selectedDesk still drives the per-desk mirror/thread/controls.
  var selectedDesk = null, selectedChannel = null;
  // Session-mirror detail level (design §2.3 UI half). "info" = the readermap body
  // only (clean default); "debug" additionally reveals each entry's collapsible
  // debug tier (reader-map envelope, mirror note, firewall warn-terms). The full
  // debug payload is ALWAYS present in the ledger — this is a live render toggle, so
  // the tier ships ON-demand (no dormant env gate, no restart). Folded into the
  // glance + thread dedup keys so flipping it forces a repaint.
  var mirrorVerbosity = "info";
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

  // coordinatorNames returns the coordinator agents the rail must always surface — the
  // primary XO and, when distinct, the CoS (from /api/status). The coordinator's session
  // IS mirrored to a ledger like any desk, but the rail is built from channel bindings, so
  // a coordinator that isn't a channel xo_agent/member would be unreachable — the operator
  // "can't even see the CoS's conversation" (F#383 criterion 1). Pinning them fixes that.
  function coordinatorNames() {
    var st = cache.status || {};
    var out = [];
    if (st.xo) out.push(st.xo);
    if (st.cos && String(st.cos).toLowerCase() !== String(st.xo || "").toLowerCase()) out.push(st.cos);
    return out;
  }
  function buildRailGroups(topology) {
    var channels = (topology && Array.isArray(topology.channels)) ? topology.channels : [];
    var groups = channels.map(function (ch) {
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
    // Pin any coordinator missing from every channel as a first-class "coordinator" group
    // at the TOP of the rail, so the CoS thread is always followable regardless of topology.
    var listed = {};
    groups.forEach(function (g) { g.desks.forEach(function (d) { listed[String(d.name).toLowerCase()] = true; }); });
    var pinned = [];
    coordinatorNames().forEach(function (name) {
      if (listed[String(name).toLowerCase()]) return; // already reachable via a channel
      listed[String(name).toLowerCase()] = true;
      pinned.push({ channel_id: "", role: "coordinator", coordinator: true, desks: [{ name: name, role: "xo" }] });
    });
    return pinned.concat(groups);
  }

  // channelForDesk returns the channel_id of the FIRST group listing a desk by name (its home
  // context for an auto-selection), or "" if it's in no group.
  function channelForDesk(groups, name) {
    var want = String(name || "").toLowerCase();
    for (var i = 0; i < groups.length; i++) {
      for (var j = 0; j < groups[i].desks.length; j++) {
        if (String(groups[i].desks[j].name).toLowerCase() === want) return groups[i].channel_id;
      }
    }
    return "";
  }

  function ensureSelection(status, groups) {
    if (selectedDesk) return;
    if (status && status.xo) {
      selectedDesk = status.xo;
      selectedChannel = channelForDesk(groups, status.xo);
      return;
    }
    for (var i = 0; i < groups.length; i++) {
      if (groups[i].desks.length) {
        selectedDesk = groups[i].desks[0].name;
        selectedChannel = groups[i].channel_id;
        return;
      }
    }
  }

  function renderConversationRail(status, topology, fresh) {
    var groups = buildRailGroups(topology);
    ensureSelection(status, groups);
    // A desk-ONLY selection (a goals "→ desk" jump, an old #conv/<desk> hash, any deep-link
    // that carries no channel) would match no row under the composite key — backfill the
    // desk's home channel so its row still highlights (cubic #378 P2).
    if (selectedDesk && !selectedChannel) selectedChannel = channelForDesk(groups, selectedDesk);
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
        // composite match: this row lights up ONLY when both the desk name AND its channel
        // match the selection — so a desk that appears in several channels highlights just the
        // picked copy, not all of them (#370).
        var on = selectedDesk && String(selectedDesk).toLowerCase() === key && grp.channel_id === selectedChannel;
        var roleTag = d.role === "xo"
          ? '<span class="conv-role xo">xo</span>'
          : (d.role ? '<span class="conv-role">' + escapeHtml(d.role) + "</span>" : "");
        return (
          '<button type="button" class="conv-item' + (on ? " selected" : "") + (stale ? " desk-stale" : "") + '" ' +
            'data-desk="' + escapeHtml(d.name) + '" data-channel="' + escapeHtml(grp.channel_id) + '" role="listitem" aria-pressed="' + String(on) + '">' +
            '<span class="conv-rail ' + deskStateClass(state) + '" aria-hidden="true"></span>' +
            '<span class="conv-item-body">' +
              '<span class="conv-item-name">' + escapeHtml(d.name) + roleTag + "</span>" +
              '<span class="conv-item-state ' + deskStateClass(state) + '">' + escapeHtml(state) + "</span>" +
            "</span>" +
          "</button>"
        );
      }).join("");
      // The pinned coordinator group has no channel — label it "coordinator" rather than
      // rendering a bare "#". A real channel shows its "#id".
      var head = grp.channel_id
        ? '<span class="chan-id">#' + escapeHtml(grp.channel_id) + "</span>" + role
        : '<span class="chan-id chan-coordinator">coordinator</span>';
      return (
        '<div class="conv-group' + (grp.coordinator ? " conv-group-coordinator" : "") + '">' +
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
        syncControlTargets(true); // explicit desk-selection: set the targets authoritatively
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
  function ledgerMatchesDesk(entry, desk) {
    if (!desk) return false;
    var d = ledgerParticipant(desk);
    if (entry.parsed) {
      return ledgerParticipant(entry.from) === d || ledgerParticipant(entry.to) === d;
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
    // Show the SELECTED channel context (#370) — a desk can live in several channels, and the
    // header must name the one that's active, not just the first group the name appears in. Fall
    // back to resolving the desk's home channel if the selection has no channel yet.
    var channel = selectedChannel ? "#" + selectedChannel : "—";
    if (!selectedChannel && selectedDesk) {
      var ch = channelForDesk(buildRailGroups(topology), selectedDesk);
      if (ch) channel = "#" + ch;
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
    paintMirror();
  }

  // fetchMirror loads the selected desk's session mirror and re-renders the glance.
  // Guarded on selectedDesk so a slow response for a de-selected desk is dropped.
  // limit=100 fetches the full recent tail (the glance uses only the last entry) —
  // sized for the Inc 2 thread-merge, which reuses cache.mirror.entries in full.
  // paintMirror re-renders BOTH mirror-dependent views — the glance (latest entry)
  // and the thread (which now interleaves the full mirror history with the ledger).
  function paintMirror() { renderSessionMirror(); renderThread(cache.history || {}); }
  function fetchMirror() {
    var want = selectedDesk;
    if (!want) { cache.mirror = null; paintMirror(); return; }
    getJSON("/api/session-mirror?agent=" + encodeURIComponent(want) + "&limit=100").then(function (d) {
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
  function renderThread(history) {
    var thread = el("conv-thread");
    if (!selectedDesk) {
      if (lastThreadKey === "@none") return;
      lastThreadKey = "@none";
      thread.innerHTML = '<div class="empty">Pick a desk to read its coordination thread.</div>';
      return;
    }
    var ledger = (history && Array.isArray(history.ledger)) ? history.ledger : [];
    var items = [];
    ledger.forEach(function (e) {
      if (!ledgerMatchesDesk(e, selectedDesk)) return;
      items.push({ kind: "ledger", t: Date.parse(e.parsed ? e.time : ""), e: e });
    });
    mirrorEntriesForSelected().forEach(function (m) {
      items.push({ kind: "mirror", t: Date.parse(m.ts), m: m });
    });
    if (!items.length) {
      var emptyKey = "@empty:" + selectedDesk;
      if (lastThreadKey === emptyKey) return;
      lastThreadKey = emptyKey;
      thread.innerHTML = '<div class="empty">No coordination history for this desk yet.</div>';
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
    var sig = selectedDesk + "#" + mirrorVerbosity + "#" + items.map(function (it) {
      return it.kind === "mirror"
        ? "m:" + (it.m.ts || "") + ":" + cheapHash(it.m.info || "") + ":" + (it.m.suppressed ? "1" : "0")
        : "l:" + (it.e.parsed ? it.e.time : "") + ":" + cheapHash(it.e.parsed ? it.e.gist : it.e.raw);
    }).join("|");
    if (sig === lastThreadKey) return;
    lastThreadKey = sig;
    thread.innerHTML = coordinatorHistoryNote(items) + items.map(function (it) {
      return it.kind === "mirror" ? threadMirrorMsg(it.m) : threadLedgerMsg(it.e);
    }).join("");
    // Latest-at-bottom scroll discipline (F#383 criterion 5): if the operator is pinned to
    // the bottom (the default, and whenever they scroll back down), keep the newest message
    // in view; if they've scrolled UP into history, don't yank them — surface a jump-to-latest
    // chip instead so a live tick can't steal their place.
    if (threadPinned) scrollThreadToBottom();
    else showThreadJump(true);
  }

  // ── thread composer + latest-at-bottom scroll (F#383 criteria 4 + 5) ──────────────
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
        '<p class="thread-gist">' + escapeHtml(e.gist) + "</p>" +
      "</div>"
    );
  }

  // threadMirrorMsg renders a session-mirror entry (the desk's own turn-final at
  // info level) as a distinct "session" turn, hue-matched to the desk's speaker
  // colour so it reads as the same participant's output alongside its relay lines.
  function threadMirrorMsg(m) {
    var hue = speakerHue(selectedDesk);
    var body = escapeHtml(m.info || "").replace(/\r?\n/g, "<br>");
    // #406 fix-forward: a firewall-refused turn is kept in the PRIVATE dash but was never posted
    // to the public channel — render that honestly so a withheld turn is not mistaken for published.
    var withheld = m.suppressed
      ? ' <span class="thread-withheld" title="Kept in your private dashboard but withheld from the public channel by the partition firewall.">withheld from public</span>'
      : "";
    return (
      '<div class="thread-msg thread-mirror' + (m.suppressed ? " is-withheld" : "") + '" style="--spk:hsl(' + hue + ' 55% 62%)">' +
        '<header class="thread-head">' +
          '<span class="thread-route"><b class="thread-from">' + escapeHtml(selectedDesk) + "</b> " +
            '<span class="thread-kind">session</span>' + withheld + "</span>" +
          '<time class="thread-time" datetime="' + escapeHtml(m.ts || "") + '" title="' + escapeHtml(m.ts || "") + '">' + escapeHtml(relTime(m.ts)) + "</time>" +
        "</header>" +
        '<div class="thread-mirror-body">' + (body || '<span class="muted">(no session output)</span>') + "</div>" +
        debugBlock(m) +
      "</div>"
    );
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
      ? unblocked.map(backlogItem).join("")
      : (bl.found ? '<div class="empty">No unblocked items.</div>' : '<div class="empty">No backlog section found.</div>');
    box.innerHTML = counts + items;
  }

  // backlogItem formats a raw backlog line into a status chip + clean text (#302):
  // it strips the leading bullet + the [marker] token and renders the marker as a
  // coloured chip, instead of dumping the raw markdown line. Each item is a button
  // (#349 Inc 4 E10): clicking opens the full item in a focused modal, since the drive
  // queue lives in a narrow column where a long item is otherwise cramped. The marker +
  // text are carried on data-* so the handler needs no re-parse.
  function backlogItem(line) {
    var raw = String(line == null ? "" : line);
    var m = /^\s*[-*]?\s*\[([a-z][a-z0-9-]*)\]\s*(.*)$/i.exec(raw);
    if (!m) {
      var text = raw.replace(/^\s*[-*]\s*/, "");
      return '<div class="backlog-item" role="button" tabindex="0" data-bq-open data-bq-text="' + escapeHtml(text) + '">' +
        '<span class="bq-text">' + escapeHtml(text) + "</span></div>";
    }
    var marker = m[1].toLowerCase();
    return '<div class="backlog-item bq-' + escapeHtml(marker) + '" role="button" tabindex="0" data-bq-open' +
      ' data-bq-marker="' + escapeHtml(marker.replace(/-/g, " ")) + '" data-bq-text="' + escapeHtml(m[2]) + '">' +
      '<span class="bq-marker">' + escapeHtml(marker.replace(/-/g, " ")) + "</span>" +
      '<span class="bq-text">' + escapeHtml(m[2]) + "</span>" +
      "</div>";
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
    // #405 Inc 4: the route/resume control fields were dropped — guard so their absence is a
    // no-op (the operator now targets a desk via the thread composer, not a control column).
    var rt = el("route-target"), ra = el("resume-agent");
    if (rt) rt.value = selectedDesk;
    if (ra) ra.value = selectedDesk;
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
    // Conversations is the fixed single-scroll app-shell (#326): only on this tab
    // does the page itself stop scrolling. Goals/Issues keep natural page scroll.
    document.body.classList.toggle("conv-shell-active", view === "conversations");
    if (view === "goals" && window.flotillaGoals) window.flotillaGoals.show();
    if (view === "issues" && window.flotillaTracker) window.flotillaTracker.show();
  }
  var tabs = document.querySelectorAll(".tab");
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

  /* ── drive-queue item modal (#349 Inc 4 E10): a queue chip lives in a narrow column,
     so clicking (or Enter/Space on) it opens the full item here, focused. Read-only —
     the queue is a status surface; acting on an item is the control forms below. ────── */
  var convModalReturn = null; // the drive-queue chip that opened the modal — refocused on close
  function openConvModal(marker, text) {
    var mk = el("conv-modal-marker");
    mk.textContent = marker || "";
    mk.style.display = marker ? "" : "none";
    el("conv-modal-title").textContent = text || "";
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
        if (item) openConvModal(item.getAttribute("data-bq-marker"), item.getAttribute("data-bq-text"));
      });
      backlog.addEventListener("keydown", function (e) {
        if (e.key !== "Enter" && e.key !== " ") return;
        var item = e.target.closest ? e.target.closest("[data-bq-open]") : null;
        if (item) { e.preventDefault(); openConvModal(item.getAttribute("data-bq-marker"), item.getAttribute("data-bq-text")); }
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
      if (view === "conversations") { renderConversations(); syncControlTargets(true); fetchMirror(); }
      if (view === "goals" && window.flotillaGoals && window.flotillaGoals.restoreNode) {
        if (window.flotillaGoals.show) window.flotillaGoals.show(); // ensure the map is rendered first
        window.flotillaGoals.restoreNode(s.node || null);
      }
    } finally {
      restoringNav = false;
    }
  }
  window.addEventListener("popstate", function (e) { applyNav(e.state); });
  // Seed the initial entry so the very first Back has a target.
  try { history.replaceState({ view: "conversations", desk: selectedDesk || null, channel: selectedChannel || null }, "", navHash({ view: "conversations", desk: selectedDesk })); } catch (e) { /* ignore */ }
  // Conversations is the default active tab — arm the fixed app-shell at startup
  // so the page doesn't scroll before the first tab interaction (#326).
  document.body.classList.add("conv-shell-active");

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
    syncControlTargets(true);
    fetchMirror(); // load the deep-linked desk's session mirror (the identity guard hides the prior desk's until it lands)
    pushNav({ view: "conversations", desk: desk }); // reversible: Back returns to the goals map (#349 A1)
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
        postJSON("/api/control/route", { target: target, message: body }).then(function (res) {
          var outcome = (res && res.outcome) || "(no outcome reported)";
          var detail = res && res.detail ? " — " + res.detail : "";
          // Bind the result to the desk the send TARGETED — if the operator moved on, don't
          // clear the new desk's draft or mislabel its composer; the send still happened.
          if (!sameSel(target)) return;
          if (outcome === "delivered") { ta.value = ""; resizeComposer(); threadPinned = true; scrollThreadToBottom(); }
          setMsg("Outcome: " + outcome + detail, outcome === "delivered" ? "ok" : "");
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
    }
  })();
})();