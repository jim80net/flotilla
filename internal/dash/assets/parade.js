/* flotilla parade — a POWERPOINT-STYLE deck viewer over the parade archive.
 * /api/parades gives every parade newest-first; each carries slides.md (the deck source:
 * "---"-separated slides, first line per slide = title, image refs render large). The page
 * OPENS as the newest deck; ← / → / tap-halves / swipe / arrow-keys navigate slides; a
 * counter shows position; Escape (or "all parades") drops to the newest-first list — the
 * progression — where tapping a parade opens its deck. report.md content ever reaches the
 * DOM only through the escape-then-markdown pipeline (escape FIRST), so nothing can inject.
 */
(function () {
  "use strict";
  function el(id) { return document.getElementById(id); }
  function esc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
  }

  var PARADES = [], pIdx = 0, sIdx = 0, view = "list";

  // resolve a slide image ref to a served URL: an http(s) ref passes through; anything else
  // is treated as an asset basename under the parade's assets/ dir.
  function imgURL(date, src) {
    if (/^https?:\/\//i.test(src)) return src;
    return "/parade-assets/" + encodeURIComponent(date) + "/" + encodeURIComponent(String(src).replace(/^.*\//, ""));
  }

  // ── GFM pipe-table helpers (pure; operate on RAW lines so the structural chars '|', '-',
  // ':' are seen before escaping — the cell TEXT is escaped-then-inline'd at render). A table
  // is a header row of '|'-separated cells IMMEDIATELY followed by a delimiter row, so prose
  // that merely contains a pipe is never mistaken for a table. ──
  function splitTableRow(raw) {
    var s = raw.trim().replace(/^\|/, "").replace(/\|$/, "");
    var cells = [], cur = "";
    for (var k = 0; k < s.length; k++) {
      if (s[k] === "\\" && s[k + 1] === "|") { cur += "|"; k++; } // \| is a literal pipe in a cell
      else if (s[k] === "|") { cells.push(cur); cur = ""; }
      else cur += s[k];
    }
    cells.push(cur);
    return cells.map(function (c) { return c.trim(); });
  }
  // isTableDelimiter: every cell is dashes with an optional leading/trailing alignment colon.
  function isTableDelimiter(raw) {
    if (raw.indexOf("|") === -1) return false;
    var cells = splitTableRow(raw);
    return cells.length > 0 && cells.every(function (c) { return /^:?-+:?$/.test(c); });
  }
  // tableAligns maps each delimiter cell to a text-align keyword, padded/truncated to n columns.
  function tableAligns(raw, n) {
    var cells = splitTableRow(raw), aligns = [];
    for (var k = 0; k < n; k++) {
      var c = cells[k] || "", l = c.charAt(0) === ":", r = c.charAt(c.length - 1) === ":";
      aligns.push(l && r ? "center" : r ? "right" : l ? "left" : "");
    }
    return aligns;
  }

  // ── HTML-table helpers (#427): a deck may carry a literal <table> block (exporters and
  // authors both emit them). The block is NEVER passed through to the DOM — it is PARSED
  // for its row/cell TEXT and re-emitted via the same escape-then-inline renderTable path
  // as a pipe table, so the escape-first invariant holds: any markup inside a cell is
  // stripped to text, and the text is escaped before insertion. ──
  // decodeEntities maps the common entities an author writes in source cells back to text
  // BEFORE the render-side escape — otherwise "&amp;" would double-escape to "&amp;amp;".
  function decodeEntities(s) {
    return String(s)
      .replace(/&nbsp;/gi, " ")
      .replace(/&lt;/gi, "<").replace(/&gt;/gi, ">")
      .replace(/&quot;/gi, '"').replace(/&#0?39;/g, "'").replace(/&apos;/gi, "'")
      .replace(/&amp;/gi, "&"); // last, so a literal "&amp;lt;" decodes to "&lt;", not "<"
  }
  // cellAlign reads an alignment from a th/td attribute string — a fixed keyword set, so
  // nothing attacker-shaped can reach the style attribute renderTable emits. It matches
  // only a REAL quoted style attribute's whole text-align declaration, or a REAL align
  // attribute whose value is exactly the keyword (cubic #447 P3: a data-align attr, a
  // title mentioning "text-align: left", or align="lefty" must NOT infer alignment).
  function cellAlign(attrs) {
    var style = /(?:^|\s)style\s*=\s*(?:"([^"]*)"|'([^']*)')/i.exec(attrs);
    if (style) {
      var d = /(?:^|;)\s*text-align\s*:\s*(left|center|right)\s*(?:;|$)/i.exec(style[1] || style[2] || "");
      if (d) return d[1].toLowerCase();
    }
    var a = /(?:^|\s)align\s*=\s*(?:"(left|center|right)"|'(left|center|right)'|(left|center|right)(?=[\s>]|$))/i.exec(attrs);
    return a ? (a[1] || a[2] || a[3]).toLowerCase() : "";
  }
  // parseHtmlTable extracts rows of {head, align, text} cells from a raw <table> block.
  // Nested markup inside a cell is stripped to its text (a deck table cell is prose).
  // KNOWN LIMITATION (regex, not a parser): an attribute value containing '>' (e.g.
  // title="a > b") truncates that tag's [^>]* match, which can drop the cell/row. The
  // failure mode is DEGRADATION ONLY — unmatched content falls through to escaped text
  // via the caller's zero-rows path; nothing can inject. Accepted for deck sources,
  // whose tables are exporter/author-plain.
  function parseHtmlTable(src) {
    var rows = [], rowRe = /<tr[^>]*>([\s\S]*?)<\/tr\s*>/gi, m;
    while ((m = rowRe.exec(src))) {
      var cells = [], cellRe = /<t([hd])\b([^>]*)>([\s\S]*?)<\/t\1\s*>/gi, c;
      while ((c = cellRe.exec(m[1]))) {
        cells.push({
          head: c[1].toLowerCase() === "h",
          align: cellAlign(c[2]),
          text: decodeEntities(c[3].replace(/<[^>]*>/g, " ")).replace(/\s+/g, " ").trim(),
        });
      }
      if (cells.length) rows.push(cells);
    }
    return rows;
  }

  // escape-then-markdown for a slide body (mirrors goals.js renderBrief). Escapes FIRST, then
  // a fixed subset: a block image line ![alt](src) renders LARGE; #.. headings; -/* bullets;
  // GFM pipe-tables; blank-line paragraphs; inline **bold**, `code`, [text](http…). Images
  // resolve via date.
  function renderMd(date, md) {
    var lines = String(md == null ? "" : md).replace(/\r\n/g, "\n").split("\n");
    // inline() takes ALREADY-ESCAPED text and layers inline markdown on top. Its link href
    // ($2) is already-escaped and valid in an attribute, so nothing is re-escaped here.
    function inline(escd) {
      return escd
        .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
        .replace(/`([^`]+)`/g, "<code>$1</code>")
        .replace(/\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');
    }
    // renderTable emits a <table> from a header row (null for a headerless HTML table),
    // body rows, and per-column alignments. Each cell is escaped THEN inline-marked (the
    // escape-first invariant), so no cell text can inject.
    function renderTable(head, rows, aligns) {
      var nCols = head ? head.length
        : rows.reduce(function (n, r) { return Math.max(n, r.length); }, 0);
      function cellHtml(tag, text, k) {
        var a = aligns[k] ? ' style="text-align:' + aligns[k] + '"' : ""; // aligns ∈ {left,center,right} — a fixed, non-injectable set
        return "<" + tag + a + ">" + inline(esc(text)) + "</" + tag + ">";
      }
      function rowHtml(cells, tag) {
        var cs = [];
        for (var k = 0; k < nCols; k++) cs.push(cellHtml(tag, cells[k] || "", k));
        return "<tr>" + cs.join("") + "</tr>";
      }
      var thead = head ? "<thead>" + rowHtml(head, "th") + "</thead>" : "";
      var tbody = rows.length ? "<tbody>" + rows.map(function (r) { return rowHtml(r, "td"); }).join("") + "</tbody>" : "";
      return '<table class="pd-table">' + thead + tbody + "</table>";
    }
    var out = [], list = null, quote = null;
    function flushList() { if (list) { out.push("<ul>" + list.join("") + "</ul>"); list = null; } }
    // A run of "> " lines renders as a blockquote callout — the parade's DECISION-BRIEF block
    // (parade v3): the operator wanted open decisions presented as their brief, and a distinct
    // callout gives the 6-element brief its own visual weight on a slide (#380 / deck v3).
    function flushQuote() { if (quote) { out.push('<blockquote class="pd-quote">' + quote.join("") + "</blockquote>"); quote = null; } }
    function flush() { flushList(); flushQuote(); }
    for (var i = 0; i < lines.length; i++) {
      var ln = lines[i];
      // Parse an image ref from the RAW line so its src (a real basename / URL) is NOT
      // HTML-escaped before imgURL builds the asset URL — escaping first would corrupt an
      // '&' in the name and break the load. The FINAL url + alt are escaped for the attrs
      // (cubic #373 P5).
      var img = /^!\[([^\]]*)\]\(([^)\s]+)\)\s*$/.exec(ln);
      if (img) { flush(); out.push('<img class="pd-slide-img" loading="lazy" src="' + esc(imgURL(date, img[2])) + '" alt="' + esc(img[1]) + '" />'); continue; }
      // HTML <table> block (#427): consumed only when a closing tag exists ahead — an
      // unterminated block falls through to text rendering (honest degradation, escape-first).
      // The parsed cell TEXT re-renders through the same renderTable path as a pipe table.
      if (/^\s*<table\b/i.test(ln)) {
        var end = -1, buf = [];
        for (var t = i; t < lines.length; t++) {
          buf.push(lines[t]);
          if (/<\/table\s*>/i.test(lines[t])) { end = t; break; }
        }
        var hrows = end === -1 ? [] : parseHtmlTable(buf.join("\n"));
        if (hrows.length) {
          flush();
          var headIsTh = hrows[0].every(function (c) { return c.head; });
          var hhead = headIsTh ? hrows[0].map(function (c) { return c.text; }) : null;
          var hbody = (headIsTh ? hrows.slice(1) : hrows).map(function (r) {
            return r.map(function (c) { return c.text; });
          });
          // A rebuilt HTML table keeps EVERY authored cell: pad a short <th> row out to
          // the widest body row (cubic #447 P2) — unlike the pipe path, where GFM says
          // extra body cells are ignored. Padded columns get blank headers, no alignment.
          if (hhead) {
            var maxW = hbody.reduce(function (n, r) { return Math.max(n, r.length); }, hhead.length);
            while (hhead.length < maxW) hhead.push("");
          }
          out.push(renderTable(hhead, hbody, hrows[0].map(function (c) { return c.align; })));
          i = end; // the for-loop's i++ advances past the closing-tag line
          continue;
        }
        // no closing tag / no parsable rows: fall through — the lines render as escaped text
      }
      // GFM pipe-table: a header row with a '|' IMMEDIATELY followed by a delimiter row. The
      // block runs until a blank or pipe-less line. Detected on RAW lines (before escaping) so
      // the structural pipes are visible; cell text is escaped-then-inline'd in renderTable.
      if (ln.indexOf("|") !== -1 && i + 1 < lines.length && isTableDelimiter(lines[i + 1])) {
        flush();
        var headCells = splitTableRow(ln);
        var aligns = tableAligns(lines[i + 1], headCells.length);
        var bodyRows = [];
        var j = i + 2;
        for (; j < lines.length && lines[j].trim() !== "" && lines[j].indexOf("|") !== -1; j++) {
          bodyRows.push(splitTableRow(lines[j]));
        }
        out.push(renderTable(headCells, bodyRows, aligns));
        i = j - 1; // the for-loop's i++ advances past the last consumed row
        continue;
      }
      // Detect a "> " blockquote on the RAW line: '>' would be escaped to '&gt;' before a
      // regex on the escaped text, so it must be matched pre-esc (like the image src). The
      // captured content is then escaped + inline-marked for insertion.
      var q = /^>\s?(.*)$/.exec(ln);
      if (q) { flushList(); (quote = quote || []).push("<p>" + inline(esc(q[1])) + "</p>"); continue; }
      var e = esc(ln); // escape the remaining text FIRST, then layer markdown
      var h = /^(#{1,6})\s+(.*)$/.exec(e), li = /^\s*[-*]\s+(.*)$/.exec(e);
      if (h) { flush(); out.push('<div class="pd-h pd-h' + Math.min(h[1].length, 4) + '">' + inline(h[2]) + "</div>"); }
      else if (li) { flushQuote(); (list = list || []).push("<li>" + inline(li[1]) + "</li>"); }
      else if (e.trim() === "") { flush(); }
      else { flush(); out.push("<p>" + inline(e) + "</p>"); }
    }
    flush();
    return out.join("");
  }

  // parseSlides splits slides.md on a line that is exactly "---"; each slide's FIRST non-empty
  // line is its title (a leading # is stripped), the rest is the body.
  function parseSlides(md) {
    var lines = String(md == null ? "" : md).replace(/\r\n/g, "\n").split("\n");
    var chunks = [], cur = [];
    for (var i = 0; i < lines.length; i++) {
      if (/^---\s*$/.test(lines[i])) { chunks.push(cur.join("\n")); cur = []; }
      else cur.push(lines[i]);
    }
    chunks.push(cur.join("\n"));
    return chunks.map(function (c) { return c.trim(); }).filter(Boolean).map(function (chunk) {
      var ls = chunk.split("\n");
      var title = (ls.shift() || "").replace(/^#+\s*/, "").trim();
      return { title: title, body: ls.join("\n").trim() };
    });
  }

  function curSlides() { return parseSlides((PARADES[pIdx] || {}).slides || ""); }

  /* ── deck view ─────────────────────────────────────────────────────────── */
  function renderDeck() {
    var par = PARADES[pIdx];
    if (!par) { showList(); return; }
    var slides = curSlides();
    if (!slides.length) slides = [{ title: par.date, body: "*No slides yet for this parade.*" }];
    if (sIdx < 0) sIdx = 0;
    if (sIdx > slides.length - 1) sIdx = slides.length - 1;
    var s = slides[sIdx];
    el("pd-deck-date").textContent = par.date;
    el("pd-counter").textContent = (sIdx + 1) + " / " + slides.length;
    el("pd-slide").innerHTML =
      (s.title ? '<h1 class="pd-slide-title">' + esc(s.title) + "</h1>" : "") +
      '<div class="pd-slide-body">' + renderMd(par.date, s.body) + "</div>";
    el("pd-prev").disabled = sIdx === 0;
    el("pd-next").disabled = sIdx >= slides.length - 1;
    var slideEl = el("pd-slide"); if (slideEl) slideEl.focus({ preventScroll: true });
    el("pd-deck").hidden = false;
    el("pd-list-view").hidden = true;
    view = "deck";
  }
  function next() { if (sIdx < curSlides().length - 1) { sIdx++; renderDeck(); } }
  function prev() { if (sIdx > 0) { sIdx--; renderDeck(); } }
  function openDeck(i) { pIdx = i; sIdx = 0; renderDeck(); }

  /* ── list view (the progression) ───────────────────────────────────────── */
  function showList() {
    el("pd-deck").hidden = true;
    el("pd-list-view").hidden = false;
    view = "list";
  }
  function renderList() {
    var box = el("pd-list");
    if (!box) return;
    if (!PARADES.length) { box.innerHTML = '<div class="empty">No parades yet — the first one will appear here.</div>'; return; }
    box.innerHTML = PARADES.map(function (p, i) {
      var slides = parseSlides(p.slides || "");
      var first = slides.length ? slides[0].title : "(empty)";
      return '<button class="pd-listcard" type="button" data-i="' + i + '">' +
        '<span class="pd-listcard-date">' + esc(p.date) + "</span>" +
        '<span class="pd-listcard-meta">' + slides.length + " slide" + (slides.length === 1 ? "" : "s") +
        " · " + esc(first) + "</span></button>";
    }).join("");
  }

  /* ── wiring: arrows, tap-halves, swipe, keyboard, list clicks ──────────── */
  function wire() {
    el("pd-prev").addEventListener("click", function (e) { e.stopPropagation(); prev(); });
    el("pd-next").addEventListener("click", function (e) { e.stopPropagation(); next(); });
    el("pd-close").addEventListener("click", showList);
    // tap the left/right half of the slide to page (ignore taps on links/images).
    el("pd-stage").addEventListener("click", function (e) {
      if (e.target.closest("a") || e.target.closest("button")) return;
      var r = el("pd-stage").getBoundingClientRect();
      if (e.clientX - r.left < r.width / 2) prev(); else next();
    });
    // swipe.
    var x0 = null;
    el("pd-stage").addEventListener("touchstart", function (e) { x0 = e.touches[0].clientX; }, { passive: true });
    el("pd-stage").addEventListener("touchend", function (e) {
      if (x0 == null) return;
      var dx = e.changedTouches[0].clientX - x0; x0 = null;
      if (Math.abs(dx) > 40) { if (dx < 0) next(); else prev(); }
    }, { passive: true });
    // keyboard: arrows page, Escape drops to the list.
    document.addEventListener("keydown", function (e) {
      if (view === "deck") {
        if (e.key === "ArrowRight" || e.key === "PageDown" || e.key === " ") { e.preventDefault(); next(); }
        else if (e.key === "ArrowLeft" || e.key === "PageUp") { e.preventDefault(); prev(); }
        else if (e.key === "Escape") { e.preventDefault(); showList(); }
      }
    });
    // list card → open that deck.
    el("pd-list").addEventListener("click", function (e) {
      var card = e.target.closest("[data-i]");
      if (card) openDeck(parseInt(card.getAttribute("data-i"), 10) || 0);
    });
  }

  wire();
  fetch("/api/parades", { cache: "no-store" })
    .then(function (r) { if (!r.ok) throw new Error("/api/parades → " + r.status); return r.json(); })
    .then(function (d) {
      if (d && d.error) { // an archive read error is an ERROR state, not a false "no parades"
        showList();
        var b = el("pd-list");
        if (b) b.innerHTML = '<div class="error">' + esc(d.error) + "</div>";
        return;
      }
      PARADES = (d && d.parades) || [];
      renderList();
      if (PARADES.length) openDeck(0); else showList(); // open the newest deck; else the (empty) list
    })
    .catch(function (e) {
      showList();
      var box = el("pd-list");
      if (box) box.innerHTML = '<div class="error">Could not load parades: ' + esc(e.message) + "</div>";
    });
})();
