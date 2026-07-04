/* flotilla parade page — a read-only archive of the fleet's parade reports, newest first.
 * Data via fetch(/api/parades); each report.md renders through the SAME escape-then-markdown
 * pipeline the decision brief uses (escape FIRST, then a fixed markdown subset) so no raw HTML
 * from a report ever reaches the DOM. Images come from /parade-assets/<date>/<file>.
 */
(function () {
  "use strict";
  function el(id) { return document.getElementById(id); }
  function esc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
  }

  // renderMd: escape-then-markdown (mirrors goals.js renderBrief). Escapes first, then a small
  // fixed subset — #..#### headings, -/* bullet lists, blank-line paragraphs, inline **bold**,
  // `code`, and [text](http(s)://…) links. Nothing here can inject markup.
  function renderMd(md) {
    var lines = esc(String(md == null ? "" : md)).split(/\r?\n/);
    function inline(s) {
      return s
        .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
        .replace(/`([^`]+)`/g, "<code>$1</code>")
        .replace(/\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');
    }
    var out = [], list = null;
    function flush() { if (list) { out.push("<ul>" + list.join("") + "</ul>"); list = null; } }
    for (var i = 0; i < lines.length; i++) {
      var ln = lines[i], h = /^(#{1,6})\s+(.*)$/.exec(ln), li = /^\s*[-*]\s+(.*)$/.exec(ln);
      if (h) { flush(); out.push('<div class="pd-h pd-h' + Math.min(h[1].length, 4) + '">' + inline(h[2]) + "</div>"); }
      else if (li) { (list = list || []).push("<li>" + inline(li[1]) + "</li>"); }
      else if (ln.trim() === "") { flush(); }
      else { flush(); out.push("<p>" + inline(ln) + "</p>"); }
    }
    flush();
    return out.join("");
  }

  function gallery(date, assets) {
    if (!assets || !assets.length) return "";
    var imgs = assets.map(function (a) {
      var src = "/parade-assets/" + encodeURIComponent(date) + "/" + encodeURIComponent(a);
      return '<a class="pd-shot" href="' + esc(src) + '" target="_blank" rel="noopener" title="' + esc(a) + '">' +
        '<img loading="lazy" src="' + esc(src) + '" alt="' + esc(a) + '" /></a>';
    }).join("");
    return '<div class="pd-gallery">' + imgs + "</div>";
  }

  function render(parades) {
    var box = el("parade-list");
    if (!box) return;
    if (!parades || !parades.length) {
      box.innerHTML = '<div class="empty">No parades yet — the first one will appear here.</div>';
      return;
    }
    box.innerHTML = parades.map(function (p) {
      var report = (p.report && p.report.trim())
        ? renderMd(p.report)
        : '<p class="muted">No report yet for this parade.</p>';
      return '<section class="pd-card">' +
        '<header class="pd-date"><span class="pd-date-dot" aria-hidden="true"></span>' + esc(p.date) + "</header>" +
        '<div class="pd-report">' + report + "</div>" +
        gallery(p.date, p.assets) +
        "</section>";
    }).join("");
  }

  fetch("/api/parades", { cache: "no-store" })
    .then(function (r) { if (!r.ok) throw new Error("/api/parades → " + r.status); return r.json(); })
    .then(function (d) { render((d && d.parades) || []); })
    .catch(function (e) {
      var box = el("parade-list");
      if (box) box.innerHTML = '<div class="error">Could not load parades: ' + esc(e.message) + "</div>";
    });
})();
