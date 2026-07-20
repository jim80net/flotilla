/* Private-LAN Research library. Markdown is escaped before a deliberately small,
 * fixed render layer is applied; authored HTML never enters the DOM as markup. */
(function () {
  "use strict";

  function el(id) { return document.getElementById(id); }
  function esc(value) {
    return String(value == null ? "" : value)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
  }
  function inline(raw) {
    var safe = esc(raw);
    return safe
      .replace(/`([^`]+)`/g, "<code>$1</code>")
      .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
      .replace(/\*([^*]+)\*/g, "<em>$1</em>")
      .replace(/\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>')
      .replace(/\[([^\]]+)\]\((#[a-zA-Z0-9_-]+)\)/g, '<a href="$2">$1</a>');
  }
  function slug(text, used) {
    var base = String(text).toLowerCase().replace(/<[^>]*>/g, "")
      .replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "") || "section";
    var candidate = base, n = 2;
    while (used[candidate]) { candidate = base + "-" + n; n++; }
    used[candidate] = true;
    return candidate;
  }
  function splitTableRow(line) {
    return line.trim().replace(/^\|/, "").replace(/\|$/, "").split("|").map(function (cell) { return cell.trim(); });
  }
  function isTableDelimiter(line) {
    var cells = splitTableRow(line);
    return line.indexOf("|") >= 0 && cells.length > 0 && cells.every(function (cell) { return /^:?-{3,}:?$/.test(cell); });
  }
  function renderTable(header, rows) {
    function row(cells, tag) {
      return "<tr>" + cells.map(function (cell) { return "<" + tag + ">" + inline(cell) + "</" + tag + ">"; }).join("") + "</tr>";
    }
    return '<div class="research-table-wrap"><table><thead>' + row(header, "th") + "</thead><tbody>" +
      rows.map(function (cells) { return row(cells, "td"); }).join("") + "</tbody></table></div>";
  }

  function renderMarkdown(markdown) {
    var lines = String(markdown || "").replace(/\r\n/g, "\n").split("\n");
    var html = [], toc = [], used = {}, paragraph = [], list = null, quote = [], code = null;
    function flushParagraph() {
      if (paragraph.length) { html.push("<p>" + inline(paragraph.join(" ")) + "</p>"); paragraph = []; }
    }
    function flushList() {
      if (list) { html.push("<" + list.tag + ">" + list.items.map(function (item) { return "<li>" + inline(item) + "</li>"; }).join("") + "</" + list.tag + ">"); list = null; }
    }
    function flushQuote() {
      if (quote.length) { html.push("<blockquote>" + quote.map(function (line) { return "<p>" + inline(line) + "</p>"; }).join("") + "</blockquote>"); quote = []; }
    }
    function flushFlow() { flushParagraph(); flushList(); flushQuote(); }

    for (var i = 0; i < lines.length; i++) {
      var line = lines[i], trimmed = line.trim();
      var fence = /^```\s*([a-zA-Z0-9_-]*)/.exec(trimmed);
      if (fence) {
        if (code) {
          html.push('<pre><code' + (code.lang ? ' data-language="' + esc(code.lang) + '"' : "") + ">" + esc(code.lines.join("\n")) + "</code></pre>");
          code = null;
        } else {
          flushFlow(); code = { lang: fence[1] || "", lines: [] };
        }
        continue;
      }
      if (code) { code.lines.push(line); continue; }
      if (trimmed.indexOf("|") >= 0 && i + 1 < lines.length && isTableDelimiter(lines[i + 1])) {
        flushFlow();
        var header = splitTableRow(line), rows = [];
        i += 2;
        while (i < lines.length && lines[i].trim() && lines[i].indexOf("|") >= 0) { rows.push(splitTableRow(lines[i])); i++; }
        i--;
        html.push(renderTable(header, rows));
        continue;
      }
      var heading = /^(#{1,4})\s+(.+)$/.exec(trimmed);
      if (heading) {
        flushFlow();
        var level = heading[1].length, text = heading[2].trim(), id = slug(text, used);
        html.push("<h" + level + ' id="' + id + '">' + inline(text) + "</h" + level + ">");
        if (level >= 2) toc.push({ level: level, text: text.replace(/[*_`]/g, ""), id: id });
        continue;
      }
      if (/^(-{3,}|\*{3,})$/.test(trimmed)) { flushFlow(); html.push("<hr>"); continue; }
      var unordered = /^[-*]\s+(.+)$/.exec(trimmed), ordered = /^\d+[.)]\s+(.+)$/.exec(trimmed);
      if (unordered || ordered) {
        flushParagraph(); flushQuote();
        var tag = unordered ? "ul" : "ol";
        if (!list || list.tag !== tag) { flushList(); list = { tag: tag, items: [] }; }
        list.items.push((unordered || ordered)[1]);
        continue;
      }
      var quoted = /^>\s?(.*)$/.exec(trimmed);
      if (quoted) { flushParagraph(); flushList(); quote.push(quoted[1]); continue; }
      if (!trimmed) { flushFlow(); continue; }
      flushList(); flushQuote(); paragraph.push(trimmed);
    }
    if (code) html.push("<pre><code>" + esc(code.lines.join("\n")) + "</code></pre>");
    flushFlow();
    return { html: html.join(""), toc: toc };
  }

  function apiPath(id) {
    return "/api/research/" + id.split("/").map(encodeURIComponent).join("/");
  }
  function pagePath(id) {
    return "/research/" + id.split("/").map(encodeURIComponent).join("/");
  }
  function pathID() {
    var prefix = "/research/";
    if (location.pathname.indexOf(prefix) !== 0) return "";
    try { return location.pathname.slice(prefix.length).split("/").map(decodeURIComponent).join("/"); }
    catch (_) { return ""; }
  }
  function fetchJSON(url) {
    return fetch(url, { cache: "no-store" }).then(function (response) {
      return response.json().catch(function () { return {}; }).then(function (body) {
        if (!response.ok) throw new Error(body.error || (url + " → " + response.status));
        return body;
      });
    });
  }
  function statusLabel(status) {
    return ({ "design-only": "Design only", "awaiting-auth": "Awaiting authorization", "operator-review": "Operator review", research: "Research" })[status] || status;
  }
  function formatDate(value) {
    var date = new Date(value);
    return isNaN(date.getTime()) ? "" : date.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
  }
  function documentWithoutDuplicateTitle(markdown, title) {
    var lines = String(markdown || "").split("\n"), first = -1;
    for (var i = 0; i < lines.length; i++) { if (lines[i].trim()) { first = i; break; } }
    if (first >= 0 && lines[first].replace(/^#\s+/, "").trim() === title) lines.splice(first, 1);
    return lines.join("\n");
  }

  var entries = [], collectionWindow = 6, decisionVisible = collectionWindow, libraryVisible = collectionWindow;
  var lastDocumentID = "", lastDocumentPush = false;
  function setIndexState(title, detail, retry) {
    var status = el("research-status");
    status.hidden = false;
    status.classList.toggle("error", retry);
    el("research-status-title").textContent = title;
    el("research-status-detail").textContent = detail;
    el("research-index-retry").hidden = !retry;
  }
  function setReaderState(title, detail, retry) {
    el("research-reader-empty").hidden = false;
    el("research-reader-state-title").textContent = title;
    el("research-reader-state-detail").textContent = detail;
    el("research-document-retry").hidden = !retry;
  }
  function card(entry) {
    var link = document.createElement("a");
    link.className = "research-card" + (entry.decision ? " is-decision" : "");
    link.href = pagePath(entry.id);
    link.dataset.researchId = entry.id;
    var top = document.createElement("span"); top.className = "research-card-top";
    var badge = document.createElement("span"); badge.className = "research-badge"; badge.textContent = statusLabel(entry.status);
    var date = document.createElement("time"); date.textContent = formatDate(entry.updated_at);
    top.appendChild(badge); top.appendChild(date);
    var title = document.createElement("strong"); title.textContent = entry.title;
    link.appendChild(top); link.appendChild(title);
    if (entry.summary) { var summary = document.createElement("span"); summary.className = "research-card-summary"; summary.textContent = entry.summary; link.appendChild(summary); }
    link.addEventListener("click", function (event) { event.preventDefault(); openDocument(entry.id, true); });
    return link;
  }
  function renderCollection(listID, moreID, collection, visible) {
    var mounted = collection.slice(0, visible), remaining = Math.max(0, collection.length - mounted.length);
    el(listID).replaceChildren.apply(el(listID), mounted.map(card));
    var more = el(moreID);
    more.hidden = remaining === 0;
    more.textContent = remaining ? "Show " + Math.min(collectionWindow, remaining) + " more · " + remaining + " remaining" : "";
  }
  function renderIndex() {
    var decisions = entries.filter(function (entry) { return entry.decision; });
    var library = entries.filter(function (entry) { return !entry.decision; });
    el("research-status").hidden = true;
    el("research-all").hidden = library.length === 0;
    el("research-count").textContent = library.length + (library.length === 1 ? " document" : " documents");
    renderCollection("research-list", "research-library-more", library, libraryVisible);
    if (decisions.length) {
      el("research-decisions").hidden = false;
      el("research-decision-count").textContent = decisions.length + " waiting";
      renderCollection("research-decision-list", "research-decision-more", decisions, decisionVisible);
    }
    if (!entries.length) {
      setIndexState("No research documents", "The configured research collection is empty.", false);
    }
  }
  function renderTOC(items) {
    var list = el("research-toc-list"); list.replaceChildren();
    items.forEach(function (item) {
      var li = document.createElement("li"); li.className = "toc-level-" + item.level;
      var link = document.createElement("a"); link.href = "#" + item.id; link.textContent = item.text;
      li.appendChild(link); list.appendChild(li);
    });
    var toc = el("research-toc");
    el("research-toc-count").textContent = items.length + (items.length === 1 ? " section" : " sections");
    toc.hidden = items.length < 2;
    toc.open = items.length >= 2 && window.matchMedia("(min-width: 761px)").matches;
    document.documentElement.classList.remove("research-toc-open");
    document.body.classList.remove("research-toc-open");
  }
  function renderDocument(doc) {
    var rendered = renderMarkdown(documentWithoutDuplicateTitle(doc.markdown, doc.title));
    el("research-reader-empty").hidden = true;
    el("research-document").hidden = false;
    el("research-title").textContent = doc.title;
    el("research-path").textContent = doc.id;
    el("research-document-status").textContent = statusLabel(doc.status);
    el("research-updated").textContent = formatDate(doc.updated_at);
    el("research-updated").dateTime = doc.updated_at;
    el("research-body").innerHTML = rendered.html;
    renderTOC(rendered.toc);
    el("research-decision-strip").hidden = !doc.decision;
    var target = rendered.toc.find(function (item) { return /checklist|operator go|decision|recommendation/i.test(item.text); });
    el("research-decision-jump").hidden = !target;
    if (target) el("research-decision-jump").href = "#" + target.id;
    document.body.classList.add("research-has-document");
    document.title = doc.title + " — flotilla research";
    window.scrollTo(0, 0);
  }
  function showLibrary(push) {
    document.body.classList.remove("research-has-document");
    if (push) history.pushState({}, "", "/research");
    document.title = "flotilla — research";
  }
  function openDocument(id, push) {
    lastDocumentID = id;
    lastDocumentPush = !!push;
    el("research-reader").classList.add("is-loading");
    el("research-document").hidden = true;
    setReaderState("Loading document…", "Fetching the latest private-LAN copy.", false);
    document.body.classList.add("research-has-document");
    fetchJSON(apiPath(id)).then(function (doc) {
      renderDocument(doc);
      if (push) history.pushState({ research: id }, "", pagePath(id));
      lastDocumentPush = false;
    }).catch(function (error) {
      setReaderState("Document unavailable", "The document could not be loaded: " + error.message, true);
    }).finally(function () { el("research-reader").classList.remove("is-loading"); });
  }

  function loadIndex() {
    setIndexState("Loading research…", "Reading the private-LAN collection.", false);
    fetchJSON("/api/research").then(function (body) {
      entries = Array.isArray(body.research) ? body.research : [];
      renderIndex();
      var id = pathID(); if (id) openDocument(id, false);
    }).catch(function (error) {
      setIndexState("Research library unavailable", "The collection could not be loaded: " + error.message, true);
    });
  }

  el("research-back").addEventListener("click", function () { showLibrary(true); });
  el("research-decision-more").addEventListener("click", function () { decisionVisible += collectionWindow; renderIndex(); });
  el("research-library-more").addEventListener("click", function () { libraryVisible += collectionWindow; renderIndex(); });
  el("research-index-retry").addEventListener("click", loadIndex);
  el("research-document-retry").addEventListener("click", function () { if (lastDocumentID) openDocument(lastDocumentID, lastDocumentPush); });
  var tocRestoreY = 0;
  var tocLinkClosing = false;
  var toc = el("research-toc"), tocSummary = toc.querySelector("summary");
  toc.addEventListener("toggle", function () {
    if (!window.matchMedia("(max-width: 760px)").matches) return;
    if (toc.open) {
      tocRestoreY = window.scrollY;
      document.documentElement.classList.add("research-toc-open");
      document.body.classList.add("research-toc-open");
      return;
    }
    document.documentElement.classList.remove("research-toc-open");
    document.body.classList.remove("research-toc-open");
    if (!tocLinkClosing) window.scrollTo(0, tocRestoreY);
    tocLinkClosing = false;
  });
  el("research-toc-list").addEventListener("click", function (event) {
    var link = event.target.closest("a");
    if (!link || !toc.open) return;
    var target = document.getElementById(link.getAttribute("href").slice(1));
    if (!target) return;
    event.preventDefault();
    tocLinkClosing = true;
    toc.open = false;
    requestAnimationFrame(function () {
      target.scrollIntoView({ block: "start" });
      target.setAttribute("tabindex", "-1");
      target.focus({ preventScroll: true });
      history.replaceState(history.state, "", "#" + target.id);
    });
  });
  document.addEventListener("keydown", function (event) {
    if (event.key !== "Escape" || !toc.open || !window.matchMedia("(max-width: 760px)").matches) return;
    event.preventDefault();
    toc.open = false;
    tocSummary.focus();
  });
  window.addEventListener("popstate", function () { var id = pathID(); if (id) openDocument(id, false); else showLibrary(false); });
  loadIndex();
}());
