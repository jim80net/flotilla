/* Static-site parade viewer — loads manifest.json + per-date slides.md (no backend). */
(function () {
  "use strict";
  function el(id) { return document.getElementById(id); }
  function esc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
  }

  var PARADES = [], pIdx = 0, sIdx = 0, view = "list";

  function imgURL(par, src) {
    if (/^https?:\/\//i.test(src)) return src;
    var base = String(src).replace(/^.*\//, "");
    return par.date + "/assets/" + encodeURIComponent(base);
  }

  function renderMd(par, md) {
    var lines = String(md == null ? "" : md).replace(/\r\n/g, "\n").split("\n");
    function inline(escd) {
      return escd
        .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
        .replace(/`([^`]+)`/g, "<code>$1</code>")
        .replace(/\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>')
        .replace(/\[([^\]]+)\]\((\.\.\/[^)\s]+)\)/g, '<a href="$2">$1</a>')
        .replace(/\[([^\]]+)\]\((#[^)\s]+)\)/g, '<a href="$2">$1</a>');
    }
    var out = [], list = null, quote = null;
    function flushList() { if (list) { out.push("<ul>" + list.join("") + "</ul>"); list = null; } }
    function flushQuote() {
      if (quote) { out.push('<blockquote class="pd-quote">' + quote.join("") + "</blockquote>"); quote = null; }
    }
    function flush() { flushList(); flushQuote(); }
    for (var i = 0; i < lines.length; i++) {
      var ln = lines[i];
      var img = /^!\[([^\]]*)\]\(([^)\s]+)\)\s*$/.exec(ln);
      if (img) {
        flush();
        out.push('<img class="pd-slide-img" loading="lazy" src="' + esc(imgURL(par, img[2])) + '" alt="' + esc(img[1]) + '" />');
        continue;
      }
      var q = /^>\s?(.*)$/.exec(ln);
      if (q) { flushList(); (quote = quote || []).push("<p>" + inline(esc(q[1])) + "</p>"); continue; }
      var e = esc(ln);
      var h = /^(#{1,6})\s+(.*)$/.exec(e), li = /^\s*[-*]\s+(.*)$/.exec(e);
      if (h) { flush(); out.push('<div class="pd-h pd-h' + Math.min(h[1].length, 4) + '">' + inline(h[2]) + "</div>"); }
      else if (li) { flushQuote(); (list = list || []).push("<li>" + inline(li[1]) + "</li>"); }
      else if (e.trim() === "") { flush(); }
      else { flush(); out.push("<p>" + inline(e) + "</p>"); }
    }
    flush();
    return out.join("");
  }

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

  // Presenter chrome: "XO Name · claim" title → avatar badge + claim title.
  // Avatar at <date>/assets/presenter-<slug>.png; missing → initials fallback.
  function parsePresenter(title) {
    var raw = String(title == null ? "" : title).trim();
    if (!raw) return { presenter: "", claim: "", slug: "", initials: "" };
    var sep = raw.indexOf(" · ");
    if (sep < 1) return { presenter: "", claim: raw, slug: "", initials: "" };
    var presenter = raw.slice(0, sep).trim();
    var claim = raw.slice(sep + 3).trim() || raw;
    var slug = presenter.toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "");
    var parts = presenter.split(/\s+/).filter(Boolean);
    var initials = parts.length >= 2
      ? (parts[0].charAt(0) + parts[parts.length - 1].charAt(0)).toUpperCase()
      : presenter.slice(0, 2).toUpperCase();
    return { presenter: presenter, claim: claim, slug: slug, initials: initials };
  }

  function presenterHtml(par, p) {
    if (!p.presenter) return "";
    var avatar = p.slug
      ? '<img class="pd-presenter-avatar" src="' + esc(imgURL(par, "presenter-" + p.slug + ".png")) +
        '" alt="" width="52" height="52" data-fallback="' + esc(p.initials) + '" />'
      : '<span class="pd-presenter-avatar pd-presenter-fallback">' + esc(p.initials) + "</span>";
    return (
      '<div class="pd-presenter" data-presenter="' + esc(p.slug || p.presenter) + '">' +
      avatar +
      '<div class="pd-presenter-meta">' +
      '<span class="pd-presenter-name">' + esc(p.presenter) + "</span>" +
      '<span class="pd-presenter-role">presenting</span>' +
      "</div></div>"
    );
  }

  function wirePresenterFallback(root) {
    if (!root) return;
    var imgs = root.querySelectorAll("img.pd-presenter-avatar[data-fallback]");
    for (var i = 0; i < imgs.length; i++) {
      (function (img) {
        img.addEventListener("error", function () {
          var span = document.createElement("span");
          span.className = "pd-presenter-avatar pd-presenter-fallback";
          span.textContent = img.getAttribute("data-fallback") || "?";
          if (img.parentNode) img.parentNode.replaceChild(span, img);
        });
      })(imgs[i]);
    }
  }

  function renderDeck() {
    var par = PARADES[pIdx];
    if (!par) { showList(); return; }
    var slides = curSlides();
    if (!slides.length) slides = [{ title: par.date, body: "*No slides yet for this parade.*" }];
    if (sIdx < 0) sIdx = 0;
    if (sIdx > slides.length - 1) sIdx = slides.length - 1;
    var s = slides[sIdx];
    var p = parsePresenter(s.title);
    var titleText = p.presenter ? p.claim : s.title;
    el("pd-deck-date").textContent = par.date;
    el("pd-counter").textContent = (sIdx + 1) + " / " + slides.length;
    el("pd-slide").innerHTML =
      presenterHtml(par, p) +
      (titleText ? '<h1 class="pd-slide-title">' + esc(titleText) + "</h1>" : "") +
      '<div class="pd-slide-body">' + renderMd(par, s.body) + "</div>";
    wirePresenterFallback(el("pd-slide"));
    el("pd-prev").disabled = sIdx === 0;
    el("pd-next").disabled = sIdx >= slides.length - 1;
    var slideEl = el("pd-slide"); if (slideEl) slideEl.focus({ preventScroll: true });
    el("pd-deck").hidden = false;
    el("pd-list-view").hidden = true;
    view = "deck";
  }
  function next() { if (sIdx < curSlides().length - 1) { sIdx++; renderDeck(); } }
  function prev() { if (sIdx > 0) { sIdx--; renderDeck(); } }
  function openDeck(i) {
    var par = PARADES[i];
    if (!par || par.error) { showList(); return; }
    pIdx = i; sIdx = 0; renderDeck();
  }

  function showList() {
    el("pd-deck").hidden = true;
    el("pd-list-view").hidden = false;
    view = "list";
  }
  function renderList() {
    var box = el("pd-list");
    if (!box) return;
    if (!PARADES.length) {
      box.innerHTML = '<div class="empty">No parades yet — the first one will appear here.</div>';
      return;
    }
    box.innerHTML = PARADES.map(function (p, i) {
      if (p.error) {
        return '<div class="pd-listcard pd-listcard-error">' +
          '<span class="pd-listcard-date">' + esc(p.date) + "</span>" +
          '<span class="pd-listcard-meta error">Could not load: ' + esc(p.error) + "</span></div>";
      }
      var slides = parseSlides(p.slides || "");
      var first = slides.length ? slides[0].title : "(empty)";
      return '<button class="pd-listcard" type="button" data-i="' + i + '">' +
        '<span class="pd-listcard-date">' + esc(p.date) + "</span>" +
        '<span class="pd-listcard-meta">' + slides.length + " slide" + (slides.length === 1 ? "" : "s") +
        " · " + esc(first) + "</span></button>";
    }).join("");
  }

  function wire() {
    el("pd-prev").addEventListener("click", function (e) { e.stopPropagation(); prev(); });
    el("pd-next").addEventListener("click", function (e) { e.stopPropagation(); next(); });
    el("pd-close").addEventListener("click", showList);
    el("pd-stage").addEventListener("click", function (e) {
      if (e.target.closest("a") || e.target.closest("button")) return;
      var r = el("pd-stage").getBoundingClientRect();
      if (e.clientX - r.left < r.width / 2) prev(); else next();
    });
    var x0 = null;
    el("pd-stage").addEventListener("touchstart", function (e) { x0 = e.touches[0].clientX; }, { passive: true });
    el("pd-stage").addEventListener("touchend", function (e) {
      if (x0 == null) return;
      var dx = e.changedTouches[0].clientX - x0; x0 = null;
      if (Math.abs(dx) > 40) { if (dx < 0) next(); else prev(); }
    }, { passive: true });
    document.addEventListener("keydown", function (e) {
      if (view === "deck") {
        if (e.key === "ArrowRight" || e.key === "PageDown" || e.key === " ") { e.preventDefault(); next(); }
        else if (e.key === "ArrowLeft" || e.key === "PageUp") { e.preventDefault(); prev(); }
        else if (e.key === "Escape") { e.preventDefault(); showList(); }
      }
    });
    el("pd-list").addEventListener("click", function (e) {
      var card = e.target.closest("[data-i]");
      if (card) openDeck(parseInt(card.getAttribute("data-i"), 10) || 0);
    });
  }

  function loadSlides(entry) {
    var path = entry.slidesPath || (entry.date + "/slides.md");
    return fetch(path, { cache: "no-store" })
      .then(function (r) {
        if (!r.ok) throw new Error(path + " → " + r.status);
        return r.text();
      })
      .then(function (text) {
        return { date: entry.date, slides: text, assets: entry.assets || [], error: null };
      });
  }

  function paradeFromSettled(entry, res) {
    if (res.status === "fulfilled") return res.value;
    var msg = (res.reason && res.reason.message) || "load failed";
    return { date: entry.date, slides: "", assets: [], error: msg };
  }

  function firstOpenDeckIndex() {
    for (var i = 0; i < PARADES.length; i++) {
      if (!PARADES[i].error) return i;
    }
    return -1;
  }

  wire();
  fetch("./manifest.json", { cache: "no-store" })
    .then(function (r) { if (!r.ok) throw new Error("manifest.json → " + r.status); return r.json(); })
    .then(function (d) {
      var entries = (d && d.parades) || [];
      if (!entries.length) { renderList(); showList(); return; }
      return Promise.allSettled(entries.map(loadSlides)).then(function (results) {
        PARADES = results.map(function (res, i) { return paradeFromSettled(entries[i], res); })
          .sort(function (a, b) { return a.date < b.date ? 1 : a.date > b.date ? -1 : 0; });
        renderList();
        var open = firstOpenDeckIndex();
        if (open >= 0) openDeck(open); else showList();
      });
    })
    .catch(function (e) {
      showList();
      var box = el("pd-list");
      if (box) box.innerHTML = '<div class="error">Could not load parades: ' + esc(e.message) + "</div>";
    });
})();