/* =========================================================================
   flotilla landing — minimal vanilla JS.
   Two jobs only:
     1. copy-to-clipboard on the install one-liner;
     2. the LIVE FLEET STATUS widget: fetch ./status.json and render a table.

   TODO: point the widget at real `flotilla status --json` output once that
   command ships. The committed ./status.json is a generic SAMPLE so the widget
   renders live on this static page today. Its shape (an `agents` array of
   { name, role?, surface?, state, task? }) is the contract a real
   `flotilla status --json` should emit; adjust render() if the real shape
   differs.
   ========================================================================= */
(function () {
  "use strict";

  /* ── 1. copy buttons ─────────────────────────────────────────────────── */
  document.querySelectorAll(".copy-btn").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var target = document.querySelector(btn.dataset.copy);
      if (!target) return;
      var text = target.textContent.trim();
      var done = function () {
        var label = btn.querySelector(".copy-label");
        var prev = label.textContent;
        label.textContent = "copied";
        btn.classList.add("copied");
        setTimeout(function () {
          label.textContent = prev;
          btn.classList.remove("copied");
        }, 1400);
      };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(done).catch(fallbackCopy);
      } else {
        fallbackCopy();
      }
      function fallbackCopy() {
        var ta = document.createElement("textarea");
        ta.value = text;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        try { document.execCommand("copy"); done(); } catch (e) { /* no-op */ }
        document.body.removeChild(ta);
      }
    });
  });

  /* ── 2. fleet-status widget ──────────────────────────────────────────── */
  var KNOWN_STATES = ["working", "idle", "awaiting", "errored", "offline"];

  var mount = document.getElementById("fleet-status");
  var meta = document.getElementById("fleet-meta");
  if (!mount) return;

  var src = mount.getAttribute("data-src") || "./status.json";

  fetch(src, { cache: "no-store" })
    .then(function (res) {
      if (!res.ok) throw new Error("status " + res.status);
      return res.json();
    })
    .then(function (data) {
      render(data);
    })
    .catch(function (err) {
      // Honest failure: don't fabricate a fleet. Show the error.
      mount.innerHTML =
        '<p class="fleet-fallback">Could not load fleet status (' +
        escapeHtml(String(err.message || err)) +
        ").</p>";
      if (meta) meta.textContent = "unavailable";
    });

  function render(data) {
    var agents = (data && Array.isArray(data.agents)) ? data.agents : [];
    if (!agents.length) {
      mount.innerHTML = '<p class="fleet-fallback">No agents in the fleet.</p>';
      if (meta) meta.textContent = "0 agents";
      return;
    }

    var html = agents.map(function (a, i) {
      var state = normalizeState(a.state);
      var label = stateLabel(a.state, state);
      var roleBadge = a.role
        ? '<span class="role">' + escapeHtml(a.role) + "</span>"
        : "";
      var surface = a.surface
        ? '<span class="fleet-surface">' + escapeHtml(a.surface) + "</span>"
        : '<span class="fleet-surface"></span>';
      var task = a.task ? escapeHtml(a.task) : "";

      return (
        '<div class="fleet-row" style="animation-delay:' + (i * 60) + 'ms">' +
          '<span class="fleet-name">' + escapeHtml(a.name || "—") + roleBadge + "</span>" +
          '<span class="state ' + state + '"><span class="glyph"></span>' + escapeHtml(label) + "</span>" +
          '<span class="fleet-task">' + task + "</span>" +
          surface +
        "</div>"
      );
    }).join("");

    mount.innerHTML = html;

    if (meta) {
      var working = agents.filter(function (a) { return normalizeState(a.state) === "working"; }).length;
      var stamp = data.generated_at ? " · " + escapeHtml(data.generated_at) : "";
      meta.textContent = agents.length + " agents · " + working + " working" + stamp;
    }
  }

  function normalizeState(s) {
    s = String(s || "").toLowerCase().trim();
    // map a few synonyms onto the canonical class set
    if (s === "awaiting-approval" || s === "awaiting_approval" || s === "blocked") return "awaiting";
    if (s === "error" || s === "crashed") return "errored";
    if (s === "down" || s === "dead") return "offline";
    return KNOWN_STATES.indexOf(s) >= 0 ? s : "idle";
  }

  function stateLabel(raw, normalized) {
    var r = String(raw || "").trim();
    return r || normalized;
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }
})();
