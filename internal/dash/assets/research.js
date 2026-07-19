(function () {
  "use strict";

  var DOCUMENTS = [];
  var ACTIVE_ID = "";

  function el(id) { return document.getElementById(id); }
  function esc(value) {
    return String(value == null ? "" : value)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
  }
  function safeHref(value) {
    var href = String(value || "").trim();
    return /^(https?:\/\/|mailto:)/i.test(href) ? href : "";
  }
  function inline(raw) {
    var escaped = esc(raw);
    escaped = escaped.replace(/`([^`]+)`/g, "<code>$1</code>");
    escaped = escaped.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
    escaped = escaped.replace(/\*([^*]+)\*/g, "<em>$1</em>");
    escaped = escaped.replace(/\[([^\]]+)\]\(([^)]+)\)/g, function (_, label, href) {
      var safe = safeHref(href);
      return safe ? '<a href="' + esc(safe) + '" target="_blank" rel="noopener noreferrer">' + label + "</a>" : label;
    });
    return escaped;
  }
  function tableDivider(line) {
    var cells = line.replace(/^\||\|$/g, "").split("|");
    return cells.length > 1 && cells.every(function (cell) { return /^\s*:?-{3,}:?\s*$/.test(cell); });
  }
  function tableCells(line) { return line.replace(/^\||\|$/g, "").split("|").map(function (cell) { return cell.trim(); }); }
  function renderTable(lines, start) {
    if (start + 1 >= lines.length || lines[start].indexOf("|") < 0 || !tableDivider(lines[start + 1])) return null;
    var headers = tableCells(lines[start]);
    var rows = [], i = start + 2;
    while (i < lines.length && lines[i].indexOf("|") >= 0 && lines[i].trim()) { rows.push(tableCells(lines[i])); i++; }
    var html = '<div class="research-table-wrap"><table><thead><tr>' + headers.map(function (cell) { return "<th>" + inline(cell) + "</th>"; }).join("") + "</tr></thead><tbody>";
    html += rows.map(function (row) { return "<tr>" + headers.map(function (_, idx) { return "<td>" + inline(row[idx] || "") + "</td>"; }).join("") + "</tr>"; }).join("");
    return { html: html + "</tbody></table></div>", next: i };
  }
  function renderMarkdown(markdown) {
    var lines = String(markdown || "").replace(/\r\n/g, "\n").split("\n");
    var out = [], list = "", quote = [], code = [], inCode = false;
    function flushList() { if (list) { out.push("</" + list + ">"); list = ""; } }
    function flushQuote() { if (quote.length) { out.push("<blockquote>" + quote.map(inline).join("<br>") + "</blockquote>"); quote = []; } }
    for (var i = 0; i < lines.length;) {
      var line = lines[i];
      if (/^```/.test(line)) {
        flushList(); flushQuote();
        if (inCode) { out.push("<pre><code>" + esc(code.join("\n")) + "</code></pre>"); code = []; inCode = false; } else { inCode = true; }
        i++; continue;
      }
      if (inCode) { code.push(line); i++; continue; }
      var table = renderTable(lines, i);
      if (table) { flushList(); flushQuote(); out.push(table.html); i = table.next; continue; }
      var heading = line.match(/^(#{1,4})\s+(.+)$/);
      var bullet = line.match(/^\s*[-*]\s+(.+)$/);
      var numbered = line.match(/^\s*\d+[.)]\s+(.+)$/);
      var quoted = line.match(/^>\s?(.*)$/);
      if (quoted) { flushList(); quote.push(quoted[1]); i++; continue; }
      flushQuote();
      if (heading) { flushList(); out.push("<h" + heading[1].length + ">" + inline(heading[2]) + "</h" + heading[1].length + ">"); }
      else if (bullet || numbered) {
        var kind = bullet ? "ul" : "ol";
        if (list !== kind) { flushList(); list = kind; out.push("<" + kind + ">"); }
        out.push("<li>" + inline((bullet || numbered)[1]) + "</li>");
      } else if (!line.trim()) { flushList(); }
      else { flushList(); out.push("<p>" + inline(line) + "</p>"); }
      i++;
    }
    flushList(); flushQuote();
    if (inCode) out.push("<pre><code>" + esc(code.join("\n")) + "</code></pre>");
    return out.join("");
  }
  function stateLabel(state) {
    if (state === "design_only") return "Design only";
    if (state === "awaiting_authority") return "Awaiting authority";
    return "Reference";
  }
  function renderList() {
    var query = el("research-filter").value.toLowerCase().trim();
    var docs = DOCUMENTS.filter(function (doc) { return !query || (doc.title + " " + doc.path).toLowerCase().indexOf(query) >= 0; });
    el("research-count").textContent = docs.length + " of " + DOCUMENTS.length;
    if (!docs.length) { el("research-list").innerHTML = '<p class="empty">No research matches this filter.</p>'; return; }
    el("research-list").innerHTML = docs.map(function (doc) {
      return '<button type="button" class="research-card' + (doc.id === ACTIVE_ID ? " active" : "") + '" data-research-id="' + esc(doc.id) + '">' +
        '<span class="research-card-state state-' + esc(doc.state) + '">' + esc(stateLabel(doc.state)) + "</span>" +
        '<strong>' + esc(doc.title) + '</strong><span class="research-card-path">' + esc(doc.path) + "</span></button>";
    }).join("");
  }
  function loadDocument(id, focus) {
    ACTIVE_ID = id; renderList();
    el("research-reader").innerHTML = '<p class="muted">Loading document…</p>';
    fetch("/api/research/" + encodeURIComponent(id), { cache: "no-store" })
      .then(function (response) { return response.json().then(function (body) { if (!response.ok) throw new Error(body.error || "Research document unavailable"); return body; }); })
      .then(function (body) {
        var doc = body.document;
        el("research-reader").innerHTML = '<header class="research-reader-head"><span class="research-card-state state-' + esc(doc.state) + '">' + esc(stateLabel(doc.state)) + '</span><span class="research-reader-path">' + esc(doc.path) + "</span></header>" +
          '<div class="research-markdown">' + renderMarkdown(doc.body) + "</div>";
        if (focus) el("research-reader").focus({ preventScroll: true });
        if (window.history && window.history.replaceState) window.history.replaceState(null, "", "/research?id=" + encodeURIComponent(id));
      })
      .catch(function (error) { el("research-reader").innerHTML = '<div class="error">' + esc(error.message) + "</div>"; });
  }
  el("research-list").addEventListener("click", function (event) {
    var card = event.target.closest("[data-research-id]");
    if (card) loadDocument(card.getAttribute("data-research-id"), true);
  });
  el("research-filter").addEventListener("input", renderList);
  fetch("/api/research", { cache: "no-store" })
    .then(function (response) { return response.json().then(function (body) { if (!response.ok) throw new Error(body.error || "Research library unavailable"); return body; }); })
    .then(function (body) {
      DOCUMENTS = Array.isArray(body.documents) ? body.documents : [];
      renderList();
      if (!DOCUMENTS.length) { el("research-list").innerHTML = '<p class="empty">No research documents yet.</p>'; return; }
      var requested = new URLSearchParams(window.location.search).get("id");
      var initial = DOCUMENTS.some(function (doc) { return doc.id === requested; }) ? requested : DOCUMENTS[0].id;
      loadDocument(initial, false);
    })
    .catch(function (error) { el("research-list").innerHTML = '<div class="error">' + esc(error.message) + "</div>"; el("research-count").textContent = "unavailable"; });
})();
