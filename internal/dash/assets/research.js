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
  var lastDocumentID = "", lastDocumentPush = false, currentDocument = null, currentRendered = null;
  var documentRequestEpoch = 0, annotationSession = 0;
  var annotationState = null, pendingAnchor = null, selectionDraft = null, annotationReturnFocus = null;
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
    document.body.classList.remove("research-annotations-open");
    if (annotationReturnFocus && annotationReturnFocus.isConnected) annotationReturnFocus.focus();
    annotationReturnFocus = null;
  }
  function openAnnotationPanel(trigger) {
    hideSelectionAction();
    annotationReturnFocus = trigger || document.activeElement;
    el("research-annotation-panel").hidden = false;
    document.body.classList.add("research-annotations-open");
  }
  function annotationLabel(annotation) {
    return annotation.anchor ? annotation.anchor.quote : "Document comment";
  }
  function annotationStateLabel(annotation) {
    var resolution = annotation.anchor_resolution;
    if (resolution && resolution.state === "needs_review") return "Needs review";
    return annotation.resolved ? "Resolved" : "Open";
  }
  function annotationByID(id) {
    var annotations = annotationState && Array.isArray(annotationState.annotations) ? annotationState.annotations : [];
    return annotations.find(function (annotation) { return annotation.id === id; });
  }
  function showAnnotationThread(annotation, trigger) {
    if (!annotation) return;
    openAnnotationPanel(trigger);
    el("research-annotation-form").hidden = true;
    el("research-annotation-thread").hidden = false;
    el("research-annotation-thread-title").textContent = annotation.anchor ? "Passage thread" : "Document comment";
    el("research-annotation-thread-state").textContent = annotationStateLabel(annotation);
    var quote = el("research-annotation-quote");
    quote.hidden = !annotation.anchor;
    quote.textContent = annotation.anchor ? annotation.anchor.quote : "";
    var comments = Array.isArray(annotation.comments) ? annotation.comments : [];
    el("research-annotation-comments").replaceChildren.apply(el("research-annotation-comments"), comments.map(function (comment) {
      var card = document.createElement("article"); card.className = "research-annotation-comment";
      var text = document.createElement("p"); text.textContent = comment.text || "";
      var footer = document.createElement("footer"); footer.textContent = (comment.author || "operator") + " · " + formatDate(comment.created_at);
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
    el("research-annotation-draft").focus();
  }
  function renderAnnotationList() {
    var annotations = annotationState && Array.isArray(annotationState.annotations) ? annotationState.annotations : [];
    var list = el("research-annotation-list");
    list.replaceChildren.apply(list, annotations.map(function (annotation) {
      var button = document.createElement("button"); button.type = "button"; button.className = "research-annotation-card";
      if (annotationStateLabel(annotation) === "Needs review") button.classList.add("is-stale");
      button.dataset.annotationOpen = annotation.id;
      var title = document.createElement("strong"); title.textContent = annotationStateLabel(annotation) + " · " + (annotation.anchor ? "Passage" : "Document");
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
    el("research-annotation-count").textContent = annotations.length + (annotations.length === 1 ? " annotation" : " annotations");
    el("research-annotation-summary").textContent = stale
      ? stale + (stale === 1 ? " passage needs review; no uncertain highlight is shown." : " passages need review; no uncertain highlights are shown.")
      : "Passage highlights and document comments stay private to this host.";
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
    var rendered = renderMarkdown(documentWithoutDuplicateTitle(doc.markdown, doc.title), doc.id);
    annotationSession++;
    currentDocument = doc; currentRendered = rendered; annotationState = null; pendingAnchor = null;
    el("research-annotation-draft").value = "";
    el("research-annotation-save").disabled = false;
    el("research-annotation-save-status").textContent = "";
    el("research-annotation-save-status").classList.remove("error");
    el("research-annotation-panel").hidden = true;
    document.body.classList.remove("research-annotations-open");
    hideSelectionAction();
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
    documentRequestEpoch++;
    annotationSession++;
    currentDocument = null; currentRendered = null; annotationState = null;
    el("research-annotation-panel").hidden = true;
    document.body.classList.remove("research-annotations-open");
    hideSelectionAction();
    document.body.classList.remove("research-has-document");
    if (push) history.pushState({}, "", "/research");
    document.title = "flotilla — research";
  }
  function openDocument(id, push) {
    var requestEpoch = ++documentRequestEpoch;
    lastDocumentID = id;
    lastDocumentPush = !!push;
    el("research-reader").classList.add("is-loading");
    el("research-document").hidden = true;
    setReaderState("Loading document…", "Fetching the latest private-LAN copy.", false);
    document.body.classList.add("research-has-document");
    fetchJSON(apiPath(id)).then(function (doc) {
      if (requestEpoch !== documentRequestEpoch) return;
      renderDocument(doc);
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
      draft.value = ""; pendingAnchor = null;
      status.textContent = "Saved.";
      renderAnnotations();
      var created = state.created || (state.annotations || [])[state.annotations.length - 1];
      showAnnotationThread(created, save);
    }).catch(function (error) {
      if (session !== annotationSession || !currentDocument || currentDocument.id !== documentID) return;
      status.textContent = "Not saved — " + error.message + ". Your draft is still here.";
      status.classList.add("error");
    }).finally(function () {
      if (session === annotationSession && currentDocument && currentDocument.id === documentID) save.disabled = false;
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
  window.addEventListener("popstate", function () { var id = pathID(); if (id) openDocument(id, false); else showLibrary(false); });
  loadIndex();
}());
