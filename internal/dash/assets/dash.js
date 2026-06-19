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
      .replace(/"/g, "&quot;");
  }

  function getJSON(path) {
    return fetch(path, { cache: "no-store" }).then(function (res) {
      if (!res.ok) throw new Error(path + " → " + res.status);
      return res.json();
    });
  }

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
        var role = a.role ? '<span class="role">' + escapeHtml(a.role) + "</span>" : "";
        var staleTag = stale ? '<span class="stale-tag">stale</span>' : "";
        return (
          '<div class="row' + (stale ? " stale" : "") + '">' +
            '<span class="name">' + escapeHtml(a.name) + role + "</span>" +
            '<span class="surface">' + escapeHtml(a.surface || "") + "</span>" +
            '<span class="state ' + escapeHtml(state) + '">' + escapeHtml(state) + staleTag + "</span>" +
          "</div>"
        );
      }).join("");
    }

    var meta = el("board-meta");
    var xl = data.xo_liveness || {};
    var bits = [];
    if (data.xo) {
      var ack = xl.acked ? ("ack " + escapeHtml(xl.ack_age) + " ago") : "never acked";
      var settled = xl.settled_known ? (xl.settled ? "settled" : "active") : "settled?";
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
  function refresh() {
    getJSON("/api/status").then(renderBoard).catch(function (err) {
      el("board").innerHTML = '<div class="error">Could not load fleet status (' + escapeHtml(err.message) + ").</div>";
    });
    getJSON("/api/topology").then(renderTopology).catch(function (err) {
      el("topology").innerHTML = '<div class="error">' + escapeHtml(err.message) + "</div>";
    });
    getJSON("/api/history").then(renderHistory).catch(function (err) {
      el("ledger").innerHTML = '<div class="error">' + escapeHtml(err.message) + "</div>";
    });
  }

  /* ── live link: SSE with a polling fallback ──────────────────────────── */
  function setConn(state) {
    var c = el("conn");
    c.className = "conn " + state;
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
    es.addEventListener("update", function () {
      setConn("live");
      if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
      refresh();
    });
    es.onopen = function () { setConn("live"); };
    es.onerror = function () {
      // EventSource auto-reconnects; meanwhile fall back to polling so the board
      // never goes silently stale.
      setConn("down");
      if (!pollTimer) pollTimer = setInterval(refresh, POLL_FALLBACK_MS);
    };
  }

  connect();
})();
