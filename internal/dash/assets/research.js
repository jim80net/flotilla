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

  function researchVideoURL(documentID, source) {
    var src = String(source || "");
    if (!/\.(mp4|webm|ogv)$/i.test(src) || src.indexOf("\\") >= 0 || src.charAt(0) === "/") return "";
    var parts = String(documentID || "").split("/"); parts.pop();
    src.split("/").forEach(function (part) { parts.push(part); });
    if (parts.some(function (part) { return !part || part === "." || part === ".." || part.charAt(0) === "."; })) return "";
    return "/research-assets/" + parts.map(encodeURIComponent).join("/");
  }
  function researchVideoType(source) {
    if (/\.webm$/i.test(source)) return "video/webm";
    if (/\.ogv$/i.test(source)) return "video/ogg";
    return "video/mp4";
  }
  function renderVideo(match, documentID) {
    var source = researchVideoURL(documentID, match[2]);
    if (!source) return "";
    var caption = (match[1] || match[3] || "Research briefing video").trim();
    var safeCaption = esc(caption);
    return '<figure class="research-video"><video controls playsinline preload="metadata" aria-label="' + safeCaption + '">' +
      '<source src="' + esc(source) + '" type="' + researchVideoType(match[2]) + '">' +
      '<a href="' + esc(source) + '">Open the video</a></video>' +
      '<figcaption><span>' + safeCaption + '</span><button type="button" data-research-video-fullscreen aria-label="Full screen: ' + safeCaption + '">Full screen</button></figcaption></figure>';
  }

  function renderMarkdown(markdown, documentID) {
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
      var video = /^!\[Video(?::\s*([^\]]+))?\]\(([^)\s]+)(?:\s+"([^"]+)")?\)$/i.exec(trimmed);
      if (video) {
        var videoHTML = renderVideo(video, documentID);
        if (videoHTML) { flushFlow(); html.push(videoHTML); continue; }
      }
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
  function annotationPath(id) {
    return "/api/research-annotations/" + id.split("/").map(encodeURIComponent).join("/");
  }
  function annotationRoutePath(id) {
    return "/api/research-annotation-routes/" + id.split("/").map(encodeURIComponent).join("/");
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
        if (!response.ok) {
          var error = new Error(body.error || (url + " → " + response.status));
          error.status = response.status; error.body = body;
          throw error;
        }
        return body;
      });
    });
  }
  function postJSON(url, body) {
    return fetch(url, {
      method: "POST", cache: "no-store",
      headers: { "Content-Type": "application/json", "X-Flotilla-Dash": "1" },
      body: JSON.stringify(body)
    }).then(function (response) {
      return response.json().catch(function () { return {}; }).then(function (payload) {
        if (!response.ok) {
          var error = new Error(payload.error || ("save failed → " + response.status));
          error.status = response.status; error.body = payload;
          throw error;
        }
        return payload;
      });
    });
  }
  function statusLabel(status) {
    return ({ "design-only": "Design only", "awaiting-auth": "Waiting on you", "operator-review": "Your review", decision: "Decision", archival: "Archival", research: "Research" })[status] || status;
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
  function documentWithoutPublicationDirective(markdown) {
    var value = String(markdown || ""), opener = "<!-- flotilla-publication";
    var trimmed = value.replace(/^[\s\uFEFF]+/, "");
    if (trimmed.indexOf(opener) !== 0) return value;
    var start = value.length - trimmed.length;
    var end = value.indexOf("-->", start + opener.length);
    return end < 0 ? value.slice(0, start) : value.slice(0, start) + value.slice(end + 3);
  }
  function diagnosticLabel(code) {
    return ({
      "content.empty": "Empty",
      "content.title_only": "Title only",
      "content.boilerplate": "Boilerplate",
      "action.missing": "Missing reader action",
      "support.missing": "Missing support or text-only rationale",
      "presentation.missing": "Missing HTML5 showpiece",
      "metadata.malformed": "Malformed metadata",
      "metadata.unknown": "Unknown directive",
      "metadata.classification": "Invalid classification",
      "metadata.support": "Invalid support value"
    })[code] || code;
  }

  function hasDecisionBrief(value) { return String(value || "").trim().length > 0; }
  function paperIDFromBrief(brief) {
    var match = String(brief || "").match(/\[[^\]]+\]\(\/(?:api\/)?research\/([^\s)#]+)(?:#[^)\s]+)?\)/i);
    if (!match) return "";
    try { return match[1].split("/").map(decodeURIComponent).join("/"); }
    catch (_) { return ""; }
  }
  function decisionBriefField(brief, names) {
    var wanted = names.map(function (name) { return name.toLowerCase(); });
    function fieldName(value) {
      return String(value || "").replace(/\*+/g, "").replace(/^\d+[.)]\s*/, "").trim().toLowerCase();
    }
    var lines = String(brief || "").split(/\r?\n/);
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].trim();
      var heading = line.match(/^#{1,6}\s+(.+?)\s*$/);
      var labeled = line.match(/^(?:[-*]\s*)?(?:\*\*)?([^:*—]+)(?:\*\*)?\s*[:—-]\s*(.+)$/);
      if (heading && wanted.indexOf(fieldName(heading[1])) !== -1) {
        var paragraph = [];
        for (var j = i + 1; j < lines.length; j++) {
          var value = lines[j].replace(/^[-*]\s+/, "").trim();
          if (!value) {
            if (paragraph.length) break;
            continue;
          }
          if (/^#{1,6}\s+/.test(value)) break;
          paragraph.push(value);
        }
        return paragraph.join(" ");
      }
      if (labeled && wanted.indexOf(fieldName(labeled[1])) !== -1) return labeled[2].trim();
    }
    return "";
  }
  function decisionCardProse(value) {
    var text = String(value || "")
      .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
      .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
      .replace(/`+([^`]+)`+/g, "$1")
      .replace(/<\/?[a-z][^>]*>/gi, " ")
      .replace(/[*_~]+/g, "")
      .replace(/\s+/g, " ")
      .trim();
    var limit = 180;
    if (text.length <= limit) return text;
    var cut = text.slice(0, limit + 1);
    var lastSpace = cut.lastIndexOf(" ");
    if (lastSpace >= Math.floor(limit * 0.65)) cut = cut.slice(0, lastSpace);
    return cut.trim() + "…";
  }
  function decisionTitle(decision) {
    var base = String(decision.node.title || decision.node.id || decision.label || "Decision")
      .replace(/\s+public release and installed marketplace rollout$/i, " marketplace release")
      .replace(/\s+public release and installed rollout$/i, " release")
      .replace(/\s+ledger$/i, "")
      .trim();
    var label = decisionCardProse(decision.label).replace(/\s*\([^)]*\)\s*$/, "").trim();
    var firstLabelWord = (label.toLowerCase().match(/[a-z0-9]+/) || [""])[0];
    if (label && label.length <= 48 && firstLabelWord && base.toLowerCase().indexOf(firstLabelWord) === -1) {
      return base + " · " + label;
    }
    return base;
  }
  function gatherRDDecisions(doc) {
    if (!doc || !Array.isArray(doc.goals)) return [];
    var out = [];
    doc.goals.forEach(function (node) {
      if (!node) return;
      var workItems = node.work_items || [];
      var gatedItems = workItems.filter(function (item) {
        if (!item) return false;
        var detail = String(item.detail || "").toLowerCase().replace(/_/g, "-");
        // Exact awaiting-auth is operator authority. A blocked item is only a
        // provisional candidate: actionableDecisions additionally requires its
        // resolved paper to be explicitly decision-class.
        return (item.class === "awaiting" && detail === "awaiting-auth") || item.class === "blocked";
      });
      gatedItems.forEach(function (item) {
        if (hasDecisionBrief(item.brief)) {
          out.push({
            node: node,
            label: item.label || item.detail || item.kind || "",
            brief: item.brief,
            paperID: item.paper_id || paperIDFromBrief(item.brief),
            authority: item.class === "awaiting" ? "awaiting-auth" : "decision-paper",
            reason: decisionBriefField(item.brief, ["why you care", "why blocked", "decision", "question", "what it is", "blocker"]) ||
              item.label || item.detail || "Your fleet is waiting for your decision"
          });
        }
      });
    });
    return out;
  }

  function focusFromURL() {
    var value = new URLSearchParams(location.search).get("focus") || "decisions";
    // Old Library/All links now land in the curated teaching lane instead of
    // reopening the raw publication queue on an operator surface.
    return value === "learn" || value === "library" || value === "all" ? "learn" : "decisions";
  }
  function indexPath() {
    return "/research?focus=" + encodeURIComponent(currentFocus);
  }
  var entries = [], goalDecisions = [], decisionsAvailable = true, collectionWindow = 6, decisionWindow = 3;
  var decisionVisible = decisionWindow, learnVisible = collectionWindow;
  var currentFocus = focusFromURL(), searchQuery = "";
  var lastDocumentID = "", lastDocumentPush = false, currentDocument = null, currentRendered = null, currentDecision = null;
  var documentRequestEpoch = 0, annotationSession = 0;
  var annotationState = null, pendingAnchor = null, pendingRoute = null, selectionDraft = null, annotationReturnFocus = null;
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
    var diagnostics = Array.isArray(entry.diagnostics) ? entry.diagnostics : [];
    link.className = "research-card" + (entry.decision ? " is-decision" : "") + (entry.archival ? " is-archival" : "") + (diagnostics.length ? " has-diagnostics" : "");
    link.href = pagePath(entry.id);
    link.dataset.researchId = entry.id;
    var top = document.createElement("span"); top.className = "research-card-top";
    var badge = document.createElement("span"); badge.className = "research-badge"; badge.textContent = statusLabel(entry.status);
    var date = document.createElement("time"); date.textContent = formatDate(entry.updated_at);
    var publication = document.createElement("span");
    publication.className = "research-publication-state";
    publication.textContent = entry.presentation_ready ? "HTML5 showpiece" : "Source only · not ready";
    top.appendChild(badge); top.appendChild(publication); top.appendChild(date);
    var title = document.createElement("strong"); title.textContent = entry.title;
    link.appendChild(top); link.appendChild(title);
    if (entry.summary) { var summary = document.createElement("span"); summary.className = "research-card-summary"; summary.textContent = entry.summary; link.appendChild(summary); }
    if (diagnostics.length) {
      var warning = document.createElement("span");
      warning.className = "research-card-diagnostics";
      warning.textContent = diagnostics.length + (diagnostics.length === 1 ? " publication check" : " publication checks");
      link.appendChild(warning);
    }
    link.addEventListener("click", function (event) { event.preventDefault(); openDocument(entry.id, true, entry); });
    return link;
  }
  function decisionCard(decision) {
    var node = decision.node;
    var paperEntry = decision.paperID && entries.find(function (entry) { return entry.id === decision.paperID; });
    var item = document.createElement("a");
    item.className = "research-card is-decision";
    item.href = pagePath(decision.paperID);
    item.dataset.researchId = decision.paperID;
    item.addEventListener("click", function (event) {
      event.preventDefault();
      openDocument(decision.paperID, true, paperEntry);
    });
    var top = document.createElement("span"); top.className = "research-card-top";
    var badge = document.createElement("span"); badge.className = "research-badge"; badge.textContent = "Decision";
    var state = document.createElement("span"); state.className = "research-decision-state"; state.textContent = node.state || node.status_display || "awaiting";
    top.appendChild(badge); top.appendChild(state);
    var title = document.createElement("strong");
    title.textContent = decisionTitle(decision);
    var reason = document.createElement("span"); reason.className = "research-card-blocker";
    reason.textContent = "Why you care · " + decisionCardProse(decision.reason);
    var action = document.createElement("span"); action.className = "research-card-summary";
    action.textContent = "Your next move · " + (decisionCardProse(
      decisionBriefField(decision.brief, ["recommendation", "recommended"])
    ) || "Open the decision and tell your fleet what you decide.");
    var next = document.createElement("span"); next.className = "research-card-next";
    next.textContent = "Open working paper →";
    item.appendChild(top); item.appendChild(title); item.appendChild(reason); item.appendChild(action); item.appendChild(next);
    return item;
  }
  function renderCollection(listID, moreID, collection, visible, renderer) {
    var mounted = collection.slice(0, visible), remaining = Math.max(0, collection.length - mounted.length);
    el(listID).replaceChildren.apply(el(listID), mounted.map(renderer || card));
    var more = el(moreID);
    more.hidden = remaining === 0;
    more.textContent = remaining ? "Show " + Math.min(collectionWindow, remaining) + " more · " + remaining + " remaining" : "";
  }
  function searchable(entry) {
    return [entry.title, entry.summary, entry.id, statusLabel(entry.status)]
      .join(" ").toLowerCase();
  }
  function searchableDecision(decision) {
    return [
      decision.node.title, decision.node.id, decision.node.state, decision.node.status_display,
      decision.label, decision.brief, decision.paperID
    ].join(" ").toLowerCase();
  }
  function filteredEntries() {
    var needle = searchQuery.trim().toLowerCase();
    return needle ? entries.filter(function (entry) { return searchable(entry).indexOf(needle) !== -1; }) : entries.slice();
  }
  function educationalResearch() {
    return filteredEntries().filter(function (entry) { return entry.learn_ready === true; });
  }
  function actionableDecisions() {
    return goalDecisions.filter(function (decision) {
      var paper = decision.paperID && entries.find(function (entry) { return entry.id === decision.paperID; });
      // The shelf is authority plus publication truth, never a generic blocked
      // roll-up. Both exact awaiting-auth and blocked safety forks need a real,
      // explicitly decision-class paper before they reach Jim.
      return !!paper && paper.decision === true &&
        paper.publication && paper.publication.explicit === true &&
        paper.publication.classification === "decision";
    });
  }
  function syncFocusControls() {
    document.querySelectorAll("[data-research-focus]").forEach(function (button) {
      button.setAttribute("aria-pressed", String(button.dataset.researchFocus === currentFocus));
    });
  }
  function renderIndex() {
    var needle = searchQuery.trim().toLowerCase();
    var actionable = actionableDecisions();
    var decisions = needle
      ? actionable.filter(function (decision) { return searchableDecision(decision).indexOf(needle) !== -1; })
      : actionable;
    var learning = educationalResearch();
    var showDecisions = currentFocus === "decisions";
    var showLearning = currentFocus === "learn";
    syncFocusControls();
    el("research-status").hidden = true;
    el("research-decisions").hidden = !showDecisions || decisions.length === 0;
    el("research-learn").hidden = !showLearning || learning.length === 0;
    el("research-learn-count").textContent = learning.length + (learning.length === 1 ? " showpiece" : " showpieces");
    renderCollection("research-learn-list", "research-learn-more", learning, learnVisible, card);
    if (showDecisions && decisions.length) {
      el("research-decision-count").textContent = decisions.length + " waiting";
      renderCollection("research-decision-list", "research-decision-more", decisions, decisionVisible, decisionCard);
    }
    var visibleCount = showDecisions ? decisions.length : learning.length;
    var kind = currentFocus === "decisions" ? "waiting decisions" : "educational showpieces";
    el("research-filter-status").textContent = visibleCount + " " + kind + (searchQuery ? " match “" + searchQuery + "”" : "");
    if (!entries.length) {
      setIndexState("No research documents", "The configured research collection is empty.", false);
    } else if (showDecisions && !decisionsAvailable) {
      setIndexState("Decisions unavailable", "Your decision queue could not be loaded. Learn remains available.", true);
    } else if (!visibleCount) {
      setIndexState("No matching material", searchQuery
        ? "No " + kind + " match “" + searchQuery + "”. Clear the search or choose another focus."
        : currentFocus === "learn"
          ? "No educational showpieces are publication-ready yet. Raw notes stay off your R&D shelf until they teach something clearly."
          : "There are no " + kind + " right now. Choose Learn to go deeper.", false);
    }
  }
  function setFocus(focus, push) {
    currentFocus = focus === "learn" ? "learn" : "decisions";
    decisionVisible = decisionWindow;
    learnVisible = collectionWindow;
    renderIndex();
    if (push) history.pushState({ focus: currentFocus }, "", indexPath());
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

  function runeCount(value) { return Array.from(String(value || "")).length; }
  function boundedRunes(value, fromEnd) {
    var chars = Array.from(String(value || ""));
    return (fromEnd ? chars.slice(-64) : chars.slice(0, 64)).join("");
  }
  function anchorForQuote(quote) {
    var markdown = currentDocument ? String(currentDocument.markdown || "") : "";
    if (!quote || runeCount(quote) > 2000) return { error: "Select between 1 and 2,000 characters." };
    var first = markdown.indexOf(quote);
    if (first < 0) return { error: "That selection crosses rendered formatting. Select a plain-text passage." };
    if (markdown.indexOf(quote, first + quote.length) >= 0) return { error: "That passage appears more than once. Select a longer, unique passage." };
    return { anchor: {
      quote: quote,
      prefix: boundedRunes(markdown.slice(0, first), true),
      suffix: boundedRunes(markdown.slice(first + quote.length), false),
      start: runeCount(markdown.slice(0, first)),
      end: runeCount(markdown.slice(0, first)) + runeCount(quote)
    } };
  }
  function hideSelectionAction() {
    el("research-selection-action").hidden = true;
    el("research-selection-status").textContent = "";
    selectionDraft = null;
  }
  function updateSelectionAction() {
    if (!currentDocument || el("research-annotation-panel").hidden === false) return;
    var selection = window.getSelection();
    if (!selection || selection.isCollapsed || !selection.rangeCount) { hideSelectionAction(); return; }
    var range = selection.getRangeAt(0), body = el("research-body");
    if (!body.contains(range.commonAncestorContainer)) { hideSelectionAction(); return; }
    var quote = selection.toString();
    if (!quote.trim()) { hideSelectionAction(); return; }
    selectionDraft = { quote: quote, result: anchorForQuote(quote) };
    var action = el("research-selection-action"), rect = range.getBoundingClientRect();
    action.hidden = false;
    el("research-selection-status").textContent = "";
    var box = action.getBoundingClientRect();
    var left = Math.max(8, Math.min(window.innerWidth - box.width - 8, rect.left));
    var top = rect.bottom + 8;
    if (top + box.height > window.innerHeight - 8) top = Math.max(8, rect.top - box.height - 8);
    top = Math.max(8, Math.min(window.innerHeight - box.height - 8, top));
    action.style.left = left + "px"; action.style.top = top + "px";
  }
  function closeAnnotationPanel() {
    el("research-annotation-panel").hidden = true;
    el("research-annotation-backdrop").hidden = true;
    document.body.classList.remove("research-annotations-open");
    if (annotationReturnFocus && annotationReturnFocus.isConnected) annotationReturnFocus.focus();
    annotationReturnFocus = null;
  }
  function openAnnotationPanel(trigger) {
    hideSelectionAction();
    annotationReturnFocus = trigger || document.activeElement;
    el("research-annotation-backdrop").hidden = false;
    el("research-annotation-panel").hidden = false;
    document.body.classList.add("research-annotations-open");
  }
  function annotationLabel(annotation) {
    return annotation.anchor ? annotation.anchor.quote : "Document comment";
  }
  function annotationResponseAuthor(annotation) {
    var comments = annotation && Array.isArray(annotation.comments) ? annotation.comments : [];
    for (var i = comments.length - 1; i >= 0; i--) {
      var author = String(comments[i].author || "").trim();
      if (author && author.toLowerCase() !== "operator") return author;
    }
    return "";
  }
  function annotationStateLabel(annotation) {
    var responder = annotationResponseAuthor(annotation);
    if (annotation.resolved) return responder ? "Addressed by " + responder : "Resolved";
    if (responder) return "Answered by " + responder;
    var resolution = annotation.anchor_resolution;
    if (resolution && resolution.state === "needs_review") return "Needs review";
    return "Awaiting owner response";
  }
  function annotationRoutingLabel(annotation) {
    var routing = annotation && annotation.routing ? annotation.routing : {};
    var state = routing.state, target = String(routing.target || "").trim();
    if (state === "delivered") return target ? "Delivered to " + target : "Delivered for review";
    if (state === "queued") return target ? "Queued to " + target : "Queued for review";
    return "Saved locally · not routed";
  }
  function annotationByID(id) {
    var annotations = annotationState && Array.isArray(annotationState.annotations) ? annotationState.annotations : [];
    return annotations.find(function (annotation) { return annotation.id === id; });
  }
  function annotationReturnTarget(id) {
    return el("research-body").querySelector('[data-annotation-id="' + id + '"]') || el("research-document-comment");
  }
  function showAnnotationThread(annotation, trigger) {
    if (!annotation) return;
    openAnnotationPanel(trigger);
    el("research-annotation-form").hidden = true;
    el("research-annotation-thread").hidden = false;
    el("research-annotation-thread-title").textContent = annotation.anchor ? "Passage thread" : "Document comment";
    el("research-annotation-thread-state").textContent = annotationStateLabel(annotation) + " · " + annotationRoutingLabel(annotation);
    var quote = el("research-annotation-quote");
    quote.hidden = !annotation.anchor;
    quote.textContent = annotation.anchor ? annotation.anchor.quote : "";
    var comments = Array.isArray(annotation.comments) ? annotation.comments : [];
    el("research-annotation-comments").replaceChildren.apply(el("research-annotation-comments"), comments.map(function (comment) {
      var card = document.createElement("article"); card.className = "research-annotation-comment";
      var text = document.createElement("p"); text.textContent = comment.text || "";
      var footer = document.createElement("footer");
      footer.textContent = (comment.author === "operator" || !comment.author ? "You" : comment.author) + " · " + formatDate(comment.created_at);
      card.appendChild(text); card.appendChild(footer); return card;
    }));
    el("research-annotation-close").focus();
  }
  function openAnnotationComposer(anchor, trigger) {
    pendingAnchor = anchor || null;
    openAnnotationPanel(trigger);
    el("research-annotation-thread").hidden = true;
    el("research-annotation-form").hidden = false;
    el("research-annotation-form-title").textContent = anchor ? "Comment on passage" : "Comment on this document";
    var quote = el("research-annotation-draft-quote");
    quote.hidden = !anchor; quote.textContent = anchor ? anchor.quote : "";
    var status = el("research-annotation-save-status");
    status.textContent = ""; status.classList.remove("error");
    pendingRoute = null;
    el("research-annotation-route-retry").hidden = true;
    el("research-annotation-draft").focus();
  }
  function renderAnnotationList() {
    var annotations = annotationState && Array.isArray(annotationState.annotations) ? annotationState.annotations : [];
    var list = el("research-annotation-list");
    list.replaceChildren.apply(list, annotations.map(function (annotation) {
      var button = document.createElement("button"); button.type = "button"; button.className = "research-annotation-card";
      if (annotationStateLabel(annotation) === "Needs review") button.classList.add("is-stale");
      button.dataset.annotationOpen = annotation.id;
      var title = document.createElement("strong"); title.textContent = annotationStateLabel(annotation) + " · " + annotationRoutingLabel(annotation) + " · " + (annotation.anchor ? "Passage" : "Document");
      var summary = document.createElement("span"); summary.textContent = annotationLabel(annotation);
      button.appendChild(title); button.appendChild(summary); return button;
    }));
    el("research-annotation-empty").hidden = !annotationState || annotations.length !== 0;
  }
  function visibleTextSegments(root) {
    var walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT), segments = [], text = "", node;
    while ((node = walker.nextNode())) {
      var start = text.length; text += node.nodeValue;
      segments.push({ node: node, start: start, end: text.length });
    }
    return { text: text, segments: segments };
  }
  function highlightVisibleQuote(annotation) {
    if (!annotation.anchor || !annotation.anchor_resolution || annotation.anchor_resolution.state !== "attached") return;
    var model = visibleTextSegments(el("research-body")), quote = String(annotation.anchor.quote || "");
    var start = model.text.indexOf(quote);
    if (start < 0 || model.text.indexOf(quote, start + quote.length) >= 0) return;
    var end = start + quote.length;
    model.segments.filter(function (segment) { return segment.end > start && segment.start < end; }).reverse().forEach(function (segment) {
      var localStart = Math.max(0, start - segment.start), localEnd = Math.min(segment.node.nodeValue.length, end - segment.start);
      if (localStart >= localEnd) return;
      segment.node.splitText(localEnd);
      var selected = segment.node.splitText(localStart);
      var mark = document.createElement("mark");
      mark.className = "research-highlight"; mark.dataset.annotationId = annotation.id;
      mark.tabIndex = 0; mark.setAttribute("role", "button");
      mark.setAttribute("aria-label", "Open passage annotation: " + quote.slice(0, 100));
      selected.parentNode.insertBefore(mark, selected); mark.appendChild(selected);
    });
  }
  function applyAnnotationHighlights() {
    if (!currentRendered) return;
    el("research-body").innerHTML = currentRendered.html;
    var annotations = annotationState && Array.isArray(annotationState.annotations) ? annotationState.annotations : [];
    annotations.forEach(highlightVisibleQuote);
  }
  function renderAnnotations() {
    var annotations = annotationState && Array.isArray(annotationState.annotations) ? annotationState.annotations : [];
    var stale = annotations.filter(function (annotation) {
      return annotation.anchor_resolution && annotation.anchor_resolution.state === "needs_review";
    }).length;
    var awaiting = annotations.filter(function (annotation) {
      return !annotation.resolved && !annotationResponseAuthor(annotation);
    }).length;
    el("research-annotation-count").textContent = annotations.length + (annotations.length === 1 ? " annotation" : " annotations");
    var summary = "";
    if (awaiting) summary = awaiting + (awaiting === 1 ? " annotation awaits an owner response." : " annotations await owner responses.");
    else if (annotations.length) summary = "Every annotation has an owner response.";
    else summary = "Passage highlights and document comments stay private to this host.";
    if (stale) summary += " " + stale + (stale === 1
      ? " passage needs review; no uncertain highlight is shown."
      : " passages need review; no uncertain highlights are shown.");
    el("research-annotation-summary").textContent = summary;
    el("research-annotations-retry").hidden = true;
    renderAnnotationList();
    applyAnnotationHighlights();
  }
  function loadAnnotations() {
    if (!currentDocument) return;
    var session = annotationSession, documentID = currentDocument.id;
    el("research-annotation-count").textContent = "Loading annotations…";
    el("research-annotation-summary").textContent = "Reading the private host store.";
    el("research-annotations-retry").hidden = true;
    annotationState = null; renderAnnotationList();
    fetchJSON(annotationPath(documentID)).then(function (state) {
      if (session !== annotationSession || !currentDocument || currentDocument.id !== documentID || state.document_id !== documentID) return;
      annotationState = state; renderAnnotations();
    }).catch(function (error) {
      if (session !== annotationSession || !currentDocument || currentDocument.id !== documentID) return;
      annotationState = null;
      el("research-annotation-count").textContent = "Annotations unavailable";
      el("research-annotation-summary").textContent = "Could not read annotations: " + error.message;
      el("research-annotations-retry").hidden = false;
      renderAnnotationList();
    });
  }
  function renderDocument(doc) {
    var markdown = documentWithoutPublicationDirective(doc.markdown);
    var rendered = renderMarkdown(documentWithoutDuplicateTitle(markdown, doc.title), doc.id);
    annotationSession++;
    currentDocument = doc; currentRendered = rendered; annotationState = null; pendingAnchor = null; pendingRoute = null;
    currentDecision = goalDecisions.find(function (decision) { return decision.paperID === doc.id; }) || null;
    el("research-annotation-draft").value = "";
    el("research-annotation-save").disabled = false;
    el("research-annotation-route-retry").hidden = true;
    el("research-annotation-save-status").textContent = "";
    el("research-annotation-save-status").classList.remove("error");
    el("research-annotation-panel").hidden = true;
    el("research-annotation-backdrop").hidden = true;
    document.body.classList.remove("research-annotations-open");
    hideSelectionAction();
    el("research-reader-empty").hidden = true;
    el("research-document").hidden = false;
    setPresentationLinks(null);
    el("research-presentation-stage").hidden = true;
    el("research-presentation").hidden = true;
    el("research-presentation").removeAttribute("src");
    el("research-body").hidden = false;
    el("research-title").textContent = doc.title;
    el("research-path").textContent = doc.id;
    el("research-document-status").textContent = statusLabel(doc.status) + (doc.presentation_ready ? " · HTML5 showpiece" : " · Source only");
    el("research-updated").textContent = formatDate(doc.updated_at);
    el("research-updated").dateTime = doc.updated_at;
    var publication = doc.publication || {}, diagnostics = Array.isArray(doc.diagnostics) ? doc.diagnostics : [];
    var publicationState = el("research-publication-state");
    publicationState.classList.toggle("is-valid", diagnostics.length === 0);
    publicationState.classList.toggle("has-diagnostics", diagnostics.length > 0);
    el("research-publication-result").textContent = diagnostics.length ? diagnostics.length + (diagnostics.length === 1 ? " check" : " checks") : "Valid";
    el("research-reader-action").textContent = publication.reader_action
      ? "Reader action · " + publication.reader_action
      : "Reader action not declared.";
    var diagnosticList = el("research-document-diagnostics");
    diagnosticList.replaceChildren();
    if (diagnostics.length) {
      diagnostics.forEach(function (diagnostic) {
        var item = document.createElement("li"), label = document.createElement("strong"), message = document.createElement("span");
        label.textContent = diagnosticLabel(diagnostic.code); message.textContent = diagnostic.message;
        item.appendChild(label); item.appendChild(message); diagnosticList.appendChild(item);
      });
    } else {
      var validItem = document.createElement("li"); validItem.textContent = doc.archival
        ? "Archival reason and publication support are declared."
        : "Reader action and publication support are declared.";
      diagnosticList.appendChild(validItem);
    }
    el("research-body").innerHTML = rendered.html;
    renderTOC(rendered.toc);
    var decisionStrip = el("research-decision-strip");
    decisionStrip.hidden = !doc.decision && !currentDecision;
    el("research-decision-title").textContent = currentDecision
      ? decisionTitle(currentDecision)
      : "Waiting on you";
    el("research-decision-summary").textContent = currentDecision
      ? ("What you decide · " + (decisionBriefField(currentDecision.brief, ["decision", "question", "recommendation", "recommended"]) ||
          "Read the paper, then tell your fleet what you decide."))
      : "This paper is waiting for what you decide.";
    el("research-decision-respond").hidden = !currentDecision;
    el("research-decision-response").hidden = true;
    el("research-decision-response-input").value = "";
    el("research-decision-response-status").textContent = "";
    el("research-decision-response-close").hidden = true;
    var target = rendered.toc.find(function (item) { return /checklist|operator go|decision|recommendation/i.test(item.text); });
    el("research-decision-jump").hidden = !target;
    if (target) el("research-decision-jump").href = "#" + target.id;
    document.body.classList.add("research-has-document");
    document.title = doc.title + " — flotilla R&D";
    window.scrollTo(0, 0);
  }
  function setPresentationLinks(entry) {
    var available = !!(entry && entry.presentation_ready && entry.presentation_url);
    [el("research-full-screen-header"), el("research-full-screen-canvas")].forEach(function (link) {
      link.hidden = !available;
      if (available) link.href = entry.presentation_url;
      else link.removeAttribute("href");
    });
  }
  function renderPresentation(doc, entry) {
    renderDocument(doc);
    currentRendered = null;
    el("research-body").hidden = true;
    el("research-toc").hidden = true;
    var frame = el("research-presentation");
    frame.title = doc.title + " presentation";
    frame.src = entry.presentation_url;
    frame.hidden = false;
    setPresentationLinks(entry);
    el("research-presentation-stage").hidden = false;
    el("research-annotation-summary").textContent = "Document comments stay private to this host; passage highlights remain on the source.";
  }
  function showLibrary(push) {
    documentRequestEpoch++;
    annotationSession++;
    currentDocument = null; currentRendered = null; currentDecision = null; annotationState = null;
    el("research-presentation").removeAttribute("src");
    el("research-presentation").hidden = true;
    el("research-presentation-stage").hidden = true;
    setPresentationLinks(null);
    el("research-annotation-panel").hidden = true;
    el("research-annotation-backdrop").hidden = true;
    document.body.classList.remove("research-annotations-open");
    hideSelectionAction();
    document.body.classList.remove("research-has-document");
    if (push) history.pushState({ focus: currentFocus }, "", indexPath());
    document.title = "flotilla — R&D";
  }
  function openDocument(id, push, entry) {
    var requestEpoch = ++documentRequestEpoch;
    lastDocumentID = id;
    lastDocumentPush = !!push;
    el("research-reader").classList.add("is-loading");
    el("research-document").hidden = true;
    setReaderState("Loading document…", "Fetching the latest private-LAN copy.", false);
    document.body.classList.add("research-has-document");
    fetchJSON(apiPath(id)).then(function (doc) {
      if (requestEpoch !== documentRequestEpoch) return;
      var indexed = entry || entries.find(function (candidate) { return candidate.id === id; });
      if (indexed && indexed.presentation_ready && indexed.presentation_url) renderPresentation(doc, indexed);
      else renderDocument(doc);
      loadAnnotations();
      if (push) history.pushState({ research: id }, "", pagePath(id));
      lastDocumentPush = false;
    }).catch(function (error) {
      if (requestEpoch !== documentRequestEpoch) return;
      setReaderState("Document unavailable", "The document could not be loaded: " + error.message, true);
    }).finally(function () {
      if (requestEpoch === documentRequestEpoch) el("research-reader").classList.remove("is-loading");
    });
  }

  function loadIndex() {
    setIndexState("Loading R&D…", "Reading your decisions and educational showpieces.", false);
    var research = fetchJSON("/api/research");
    var decisions = fetchJSON("/api/goals").then(function (body) {
      decisionsAvailable = body && body.found !== false;
      return decisionsAvailable ? gatherRDDecisions(body) : [];
    }).catch(function () {
      decisionsAvailable = false;
      return [];
    });
    Promise.all([research, decisions]).then(function (values) {
      entries = Array.isArray(values[0].research) ? values[0].research : [];
      goalDecisions = values[1];
      renderIndex();
      var id = pathID(); if (id) openDocument(id, false, entries.find(function (entry) { return entry.id === id; }));
    }).catch(function (error) {
      setIndexState("R&D library unavailable", "The evidence collection could not be loaded: " + error.message, true);
    });
  }

  el("research-back").addEventListener("click", function () { showLibrary(true); });
  el("research-decision-more").addEventListener("click", function () { decisionVisible += decisionWindow; renderIndex(); });
  el("research-learn-more").addEventListener("click", function () { learnVisible += collectionWindow; renderIndex(); });
  document.querySelector(".research-focus-tabs").addEventListener("click", function (event) {
    var button = event.target.closest("[data-research-focus]");
    if (button) setFocus(button.dataset.researchFocus, true);
  });
  el("research-search").addEventListener("input", function () {
    searchQuery = this.value;
    decisionVisible = decisionWindow;
    learnVisible = collectionWindow;
    renderIndex();
  });
  el("research-decision-respond").addEventListener("click", function () {
    var form = el("research-decision-response");
    form.hidden = false;
    el("research-decision-response-close").hidden = true;
    el("research-decision-response-input").focus();
  });
  el("research-decision-response-close").addEventListener("click", function () {
    el("research-decision-response").hidden = true;
    el("research-decision-respond").focus();
  });
  el("research-decision-response").addEventListener("submit", function (event) {
    event.preventDefault();
    var input = el("research-decision-response-input");
    var status = el("research-decision-response-status");
    var send = el("research-decision-response-send");
    var text = input.value.trim();
    if (!text) { status.textContent = "Type a response first."; input.focus(); return; }
    if (!currentDecision || !currentDocument) { status.textContent = "Decision context is unavailable. Your draft is still here."; return; }
    var decision = currentDecision;
    var documentID = currentDocument.id;
    var target = String(decision.node.conversation_agent || decision.node.owner || "").trim();
    if (!target) { status.textContent = "No desk owns this decision. Your draft is still here."; return; }
    send.disabled = true;
    status.textContent = "Sending…";
    postJSON("/api/control/respond", {
      target: target,
      goal_id: decision.node.id || "",
      item: decision.label || "",
      message: text
    }).then(function (result) {
      if (!currentDocument || currentDocument.id !== documentID || currentDecision !== decision) return;
      if (result.outcome === "delivered") {
        status.textContent = "Delivered to your fleet at " + result.target + " — receipt confirmed.";
      } else if (result.outcome === "queued") {
        status.textContent = "Queued for your fleet at " + result.target + " — not delivered yet.";
      } else {
        status.textContent = "Delivery state is unclear — check your fleet's Work Context.";
      }
      input.value = "";
      el("research-decision-response-close").hidden = false;
      el("research-decision-response-close").focus();
    }).catch(function (error) {
      if (!currentDocument || currentDocument.id !== documentID || currentDecision !== decision) return;
      status.textContent = "NOT sent: " + error.message + ". Your draft is still here.";
    }).finally(function () {
      if (currentDocument && currentDocument.id === documentID && currentDecision === decision) send.disabled = false;
    });
  });
  el("research-index-retry").addEventListener("click", loadIndex);
  el("research-document-retry").addEventListener("click", function () { if (lastDocumentID) openDocument(lastDocumentID, lastDocumentPush); });
  el("research-body").addEventListener("click", function (event) {
    var highlight = event.target.closest("[data-annotation-id]");
    if (highlight) {
      showAnnotationThread(annotationByID(highlight.dataset.annotationId), highlight);
      return;
    }
    var button = event.target.closest("[data-research-video-fullscreen]");
    if (!button) return;
    var video = button.closest(".research-video").querySelector("video");
    if (video.requestFullscreen) {
      var request = video.requestFullscreen();
      if (request && request.catch) request.catch(function () {});
    } else if (video.webkitEnterFullscreen) {
      video.webkitEnterFullscreen();
    }
  });
  el("research-body").addEventListener("keydown", function (event) {
    var highlight = event.target.closest("[data-annotation-id]");
    if (!highlight || (event.key !== "Enter" && event.key !== " ")) return;
    event.preventDefault();
    showAnnotationThread(annotationByID(highlight.dataset.annotationId), highlight);
  });
  el("research-selection-action").querySelector("button").addEventListener("click", function () {
    if (!selectionDraft) return;
    if (selectionDraft.result.error) {
      el("research-selection-status").textContent = selectionDraft.result.error;
      return;
    }
    openAnnotationComposer(selectionDraft.result.anchor, this);
    window.getSelection().removeAllRanges();
  });
  el("research-document-comment").addEventListener("click", function () { openAnnotationComposer(null, this); });
  el("research-annotations-retry").addEventListener("click", loadAnnotations);
  el("research-annotation-close").addEventListener("click", closeAnnotationPanel);
  el("research-annotation-backdrop").addEventListener("click", closeAnnotationPanel);
  el("research-annotation-list").addEventListener("click", function (event) {
    var button = event.target.closest("[data-annotation-open]");
    if (button) showAnnotationThread(annotationByID(button.dataset.annotationOpen), button);
  });
  el("research-annotation-form").addEventListener("submit", function (event) {
    event.preventDefault();
    var draft = el("research-annotation-draft"), comment = draft.value;
    var status = el("research-annotation-save-status"), save = el("research-annotation-save");
    status.classList.remove("error");
    if (!comment.trim()) { status.textContent = "Write a comment before saving."; status.classList.add("error"); draft.focus(); return; }
    if (!annotationState || !currentDocument) {
      status.textContent = "Not saved — annotation state is unavailable. Your draft is still here.";
      status.classList.add("error"); return;
    }
    var session = annotationSession, documentID = currentDocument.id, documentDigest = currentDocument.digest;
    save.disabled = true; status.textContent = "Saving…";
    postJSON(annotationPath(documentID), {
      generation: annotationState.generation,
      document_digest: documentDigest,
      anchor: pendingAnchor,
      comment: comment
    }).then(function (state) {
      if (session !== annotationSession || !currentDocument || currentDocument.id !== documentID || state.document_id !== documentID) return;
      annotationState = state;
      renderAnnotations();
      var created = state.created || (state.annotations || [])[state.annotations.length - 1];
      if (!created || !created.routing || created.routing.state === "saved") {
        pendingRoute = created || null;
        status.textContent = "Saved locally — not routed. Your draft is still here.";
        status.classList.add("error");
        el("research-annotation-route-retry").hidden = !pendingRoute;
        return;
      }
      draft.value = ""; pendingAnchor = null; pendingRoute = null;
      el("research-annotation-route-retry").hidden = true;
      status.textContent = created.routing.state === "delivered"
        ? "Saved and delivered for review."
        : "Saved and queued for review.";
      showAnnotationThread(created, annotationReturnTarget(created.id));
    }).catch(function (error) {
      if (session !== annotationSession || !currentDocument || currentDocument.id !== documentID) return;
      status.textContent = "Not saved — " + error.message + ". Your draft is still here.";
      status.classList.add("error");
    }).finally(function () {
      if (session === annotationSession && currentDocument && currentDocument.id === documentID) save.disabled = false;
    });
  });
  el("research-annotation-route-retry").addEventListener("click", function () {
    if (!pendingRoute || !currentDocument) return;
    var session = annotationSession, documentID = currentDocument.id;
    var retry = this, status = el("research-annotation-save-status");
    retry.disabled = true;
    status.classList.remove("error");
    status.textContent = "Retrying routing…";
    postJSON(annotationRoutePath(documentID), {
      annotation_id: pendingRoute.id,
      generation: pendingRoute.routing.generation
    }).then(function (state) {
      if (session !== annotationSession || !currentDocument || currentDocument.id !== documentID || state.document_id !== documentID) return;
      annotationState = state;
      var routed = state.created || annotationByID(pendingRoute.id);
      renderAnnotations();
      if (!routed || !routed.routing || routed.routing.state === "saved") {
        status.textContent = "Saved locally — not routed. Retry when coordination is available.";
        status.classList.add("error");
        return;
      }
      el("research-annotation-draft").value = "";
      pendingAnchor = null; pendingRoute = null;
      retry.hidden = true;
      status.textContent = routed.routing.state === "delivered"
        ? "Delivered for review."
        : "Queued durably for review.";
      showAnnotationThread(routed, annotationReturnTarget(routed.id));
    }).catch(function (error) {
      if (session !== annotationSession || !currentDocument || currentDocument.id !== documentID) return;
      status.textContent = "Saved locally — routing retry failed: " + error.message + ". Your draft is still here.";
      status.classList.add("error");
    }).finally(function () {
      if (session === annotationSession && currentDocument && currentDocument.id === documentID) retry.disabled = false;
    });
  });
  ["mouseup", "keyup"].forEach(function (name) {
    el("research-body").addEventListener(name, function () { setTimeout(updateSelectionAction, 0); });
  });
  var selectionTimer = 0;
  document.addEventListener("selectionchange", function () {
    clearTimeout(selectionTimer);
    selectionTimer = setTimeout(function () {
      if (!el("research-selection-action").contains(document.activeElement)) updateSelectionAction();
    }, 50);
  });
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
    if (event.key === "Escape" && !el("research-annotation-panel").hidden) {
      event.preventDefault(); closeAnnotationPanel(); return;
    }
    if (event.key === "Tab" && !el("research-annotation-panel").hidden) {
      var focusable = Array.from(el("research-annotation-panel").querySelectorAll('button:not([disabled]), textarea:not([disabled]), [href], [tabindex]:not([tabindex="-1"])'))
        .filter(function (node) { return node.getClientRects().length > 0; });
      if (focusable.length) {
        var first = focusable[0], last = focusable[focusable.length - 1];
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
      }
    }
    if (event.key !== "Escape" || !toc.open || !window.matchMedia("(max-width: 760px)").matches) return;
    event.preventDefault();
    toc.open = false;
    tocSummary.focus();
  });
  window.addEventListener("popstate", function () {
    var id = pathID();
    if (id) { openDocument(id, false); return; }
    currentFocus = focusFromURL();
    decisionVisible = decisionWindow;
    learnVisible = collectionWindow;
    showLibrary(false);
    renderIndex();
  });
  loadIndex();
}());
