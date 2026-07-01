/* flotilla dash — read surface (vanilla JS, no build step).
 *
 * All dynamic data arrives via fetch() of the JSON endpoints — NEVER rendered
 * server-side into a <script> literal — so a desk name, ledger gist, or backlog
 * line can never become stored XSS. Everything inserted into the DOM goes
 * through textContent / escapeHtml.
 *
 * Live updates: an EventSource on /events; each "update" event triggers a
 * refetch of all three endpoints. /api/status is the poll fallback + the
 * reconcile-on-(re)connect read, so a dropped SSE link degrades to polling.
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
      .replace(/'/g, "&#39;"); // defense-in-depth for any single-quoted context
  }

  function getJSON(path) {
    return fetch(path, { cache: "no-store" }).then(function (res) {
      if (!res.ok) throw new Error(path + " → " + res.status);
      return res.json();
    });
  }

  // postJSON issues a state-changing request with the anti-CSRF custom header and
  // surfaces the server's typed error message (data.error) on failure. Shared by
  // the tracker + control views so the header + error handling are single-sourced.
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

  // Expose the small, single-sourced helpers so tracker.js + control.js reuse the
  // SAME escapeHtml/getJSON/postJSON (no duplicated XSS-escaping or fetch logic).
  window.flotillaDash = { el: el, escapeHtml: escapeHtml, getJSON: getJSON, postJSON: postJSON };

  /* ── fleet board + freshness ─────────────────────────────────────────── */
  function renderBoard(data) {
    var fresh = data.freshness || { state: "absent", message: "" };
    var banner = el("freshness");
    banner.className = "freshness show " + escapeHtml(fresh.state);
    banner.textContent = fresh.message || "";

    var agents = Array.isArray(data.agents) ? data.agents : [];
    var stale = fresh.state === "stale";
    var board = el("board");
    if (!agents.length) {
      board.innerHTML = '<div class="empty">No agents in the roster.</div>';
    } else {
      board.innerHTML = agents.map(function (a) {
        var state = String(a.state || "unknown");
        var stateClass = escapeHtml(state);
        var role = a.role ? '<span class="role">' + escapeHtml(a.role) + "</span>" : "";
        var staleTag = stale ? '<span class="stale-tag">stale</span>' : "";
        return (
          '<article class="desk' + (stale ? " desk-stale" : "") + '" role="listitem">' +
            '<div class="desk-rail state-' + stateClass + '" aria-hidden="true"></div>' +
            '<div class="desk-body">' +
              '<header class="desk-head">' +
                '<span class="desk-name">' + escapeHtml(a.name) + role + staleTag + "</span>" +
                '<span class="desk-state state-' + stateClass + '">' + stateClass + "</span>" +
              "</header>" +
              '<span class="desk-surface">' + escapeHtml(a.surface || "—") + "</span>" +
            "</div>" +
          "</article>"
        );
      }).join("");
    }

    var meta = el("board-meta");
    var xl = data.xo_liveness || {};
    var bits = [];
    if (data.xo) {
      var ack = xl.acked ? ("ack " + escapeHtml(xl.ack_age) + " ago") : "never acked";
      var settled = xl.settled_known ? (xl.settled ? "settled" : "active") : "settled unknown";
      bits.push("XO " + escapeHtml(data.xo) + " · " + ack + " · " + settled);
    }
    meta.innerHTML = bits.join("");
  }

  /* ── federation topology ─────────────────────────────────────────────── */
  function renderTopology(data) {
    var topo = el("topology");
    var channels = Array.isArray(data.channels) ? data.channels : [];
    if (!channels.length) {
      topo.innerHTML = '<div class="topo-note">' + escapeHtml(data.note || "no topology") + "</div>";
      return;
    }
    topo.innerHTML = channels.map(function (ch) {
      var role = ch.role ? '<span class="chan-role">' + escapeHtml(ch.role) + "</span>" : "";
      var members = (ch.members || []).map(function (m) {
        return '<span class="member">' + escapeHtml(m) + "</span>";
      }).join("");
      return (
        '<div class="chan">' +
          '<div class="chan-head">' +
            '<span class="chan-xo">' + escapeHtml(ch.xo_agent) + "</span>" +
            '<span class="chan-id">#' + escapeHtml(ch.channel_id) + "</span>" +
            role +
          "</div>" +
          '<div class="members">' + (members || '<span class="muted">no members</span>') + "</div>" +
        "</div>"
      );
    }).join("");
  }

  /* ── coordination history ────────────────────────────────────────────── */
  function renderHistory(data) {
    var bl = data.backlog || {};
    var backlog = el("backlog");
    var counts =
      '<div class="backlog-counts">' +
        '<span>' + (bl.items || 0) + " items</span>" +
        '<span class="count-blocked">' + (bl.blocked || 0) + " blocked</span>" +
        (bl.awaiting_auth ? '<span class="count-awaiting-auth">' + bl.awaiting_auth + " awaiting-auth</span>" : "") +
        '<span class="count-done">' + (bl.done || 0) + " done</span>" +
        (bl.malformed ? '<span class="count-malformed">' + bl.malformed + " malformed</span>" : "") +
      "</div>";
    var unblocked = Array.isArray(bl.unblocked) ? bl.unblocked : [];
    var items = unblocked.length
      ? unblocked.map(function (line) { return '<div class="backlog-item">' + escapeHtml(line) + "</div>"; }).join("")
      : (bl.found ? '<div class="empty">No unblocked items.</div>' : '<div class="empty">No backlog section found.</div>');
    backlog.innerHTML = counts + items;

    var ledger = el("ledger");
    var entries = Array.isArray(data.ledger) ? data.ledger : [];
    if (!entries.length) {
      ledger.innerHTML = '<div class="empty">No coordination entries yet.</div>';
      return;
    }
    ledger.innerHTML = entries.map(function (e) {
      if (e.parsed) {
        return (
          '<div class="ledger-entry">' +
            '<span class="ledger-time">' + escapeHtml(e.time) + "</span> " +
            '<span class="ledger-route">' + escapeHtml(e.from) + " → " + escapeHtml(e.to) + "</span>" +
            (e.channel && e.channel !== "-" ? ' <span class="muted">#' + escapeHtml(e.channel) + "</span>" : "") +
            '<span class="ledger-gist">' + escapeHtml(e.gist) + "</span>" +
          "</div>"
        );
      }
      return '<div class="ledger-entry ledger-raw">' + escapeHtml(e.raw) + "</div>";
    }).join("");
  }

  /* ── refresh orchestration ───────────────────────────────────────────── */
  // A monotonic epoch guards against out-of-order renders: SSE bursts + the poll
  // fallback can launch overlapping refreshes, and an older response could land
  // after a newer one. Each refresh stamps an epoch; a response only renders if
  // it is still the latest, so a slow earlier fetch can never clobber the board
  // with a stale snapshot.
  var refreshEpoch = 0;
  function refresh() {
    var epoch = ++refreshEpoch;
    function current() { return epoch === refreshEpoch; }
    function errorIn(id, err) { if (current()) el(id).innerHTML = '<div class="error">' + escapeHtml(err.message) + "</div>"; }

    getJSON("/api/status").then(function (d) { if (current()) renderBoard(d); }).catch(function (err) {
      if (current()) el("board").innerHTML = '<div class="error">Could not load fleet status (' + escapeHtml(err.message) + ").</div>";
    });
    getJSON("/api/topology").then(function (d) { if (current()) renderTopology(d); }).catch(function (err) {
      errorIn("topology", err);
    });
    getJSON("/api/history").then(function (d) { if (current()) renderHistory(d); }).catch(function (err) {
      // A history failure must mark BOTH panes — leaving the backlog showing its
      // previous (now-stale) content while the ledger shows an error would be
      // inconsistent and misleading.
      errorIn("ledger", err);
      errorIn("backlog", err);
    });
  }

  /* ── live link: SSE with a polling fallback ──────────────────────────── */
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
    // Stop the fallback poller as soon as the live link is (re)established — not
    // only when an `update` event arrives — so a reconnect doesn't keep the
    // redundant 5s refetch running.
    es.onopen = function () { setConn("live"); stopPolling(); };
    es.onerror = function () {
      // EventSource auto-reconnects; meanwhile fall back to polling so the board
      // never goes silently stale.
      setConn("down");
      if (!pollTimer) pollTimer = setInterval(refresh, POLL_FALLBACK_MS);
    };
  }

  connect();

  /* ── tab nav: Fleet ⇄ Issues ─────────────────────────────────────────── */
  // The fleet view is the live, SSE-driven board; the issues view is the
  // gh-backed tracker (tracker.js), which fetches on demand. Switching only
  // toggles visibility — the fleet's live link keeps running underneath so the
  // board is current the instant the operator switches back.
  var VIEWS = ["fleet", "issues", "control"];
  function showView(view) {
    VIEWS.forEach(function (v) {
      var on = v === view;
      el("view-" + v).classList.toggle("hidden", !on);
      el("tab-" + v).classList.toggle("active", on);
      el("tab-" + v).setAttribute("aria-selected", String(on));
    });
    // The freshness banner belongs to the live fleet board only.
    el("freshness").classList.toggle("hidden", view !== "fleet");
    if (view === "issues" && window.flotillaTracker) window.flotillaTracker.show();
  }
  var tabs = document.querySelectorAll(".tab");
  for (var i = 0; i < tabs.length; i++) {
    tabs[i].addEventListener("click", function () { showView(this.getAttribute("data-view")); });
  }
})();
