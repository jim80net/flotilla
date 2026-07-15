/* flotilla dash — native GitHub-backed issue tracker (vanilla JS, no build).
 *
 * Reuses dash.js's single-sourced helpers (window.flotillaDash) so escapeHtml /
 * getJSON are not duplicated. Every dynamic value (titles, bodies, comments,
 * labels) is inserted via escapeHtml — a malicious issue title can never become
 * stored XSS. Writes carry the X-Flotilla-Dash anti-CSRF header (the dash's own
 * fetch sets it; a cross-origin page cannot, so a forged POST is rejected).
 *
 * gh calls have latency and can fail (unauth / rate-limit / network); every view
 * shows an explicit loading state and surfaces the server's TYPED error verbatim
 * — never a blank list that looks like "no issues".
 */
(function () {
  "use strict";

  var D = window.flotillaDash;
  var el = D.el, escapeHtml = D.escapeHtml, getJSON = D.getJSON, postJSON = D.postJSON;

  var loaded = false; // lazy first-load when the Issues tab is first shown
  var workRowContexts = {};
  var lastLedgerDoc = null;
  function isMobileLedger() { return !!(window.matchMedia && window.matchMedia("(max-width: 640px)").matches); }
  var mobileLedger = isMobileLedger();
  var shippedOpen = {};
  var shippedWindows = {};

  // A monotonic epoch guards against out-of-order renders: rapid filter toggles
  // or list⇄detail navigation launch overlapping fetches, and an older response
  // could land after a newer one. Each fetch stamps an epoch; a response renders
  // only if it is still the latest (same pattern as dash.js's board refresh).
  var viewEpoch = 0;

  // safeHref returns an escaped https?:// URL or "" — a scheme allowlist so a
  // non-http(s) URL (e.g. a javascript: scheme, which escapeHtml does NOT
  // neutralize in an href) can never become a clickable link. The issue URL is
  // gh-minted today; this makes the safe-scheme invariant explicit, not implicit.
  function safeHref(u) {
    return /^https?:\/\//i.test(String(u || "")) ? escapeHtml(u) : "";
  }

  /* ── label chips ─────────────────────────────────────────────────────── */
  function labelChips(labels) {
    return (labels || []).map(function (l) {
      // Pass GitHub's raw label hue as --label; the CSS derives a legible chip
      // (dark-ink text on a faint tint) so a light GitHub color (e.g. #a2eeef)
      // never washes out on the warm-light surface. See dash.css .issue-label.
      var color = /^[0-9a-fA-F]{6}$/.test(l.color || "") ? l.color : "888888";
      return '<span class="issue-label" style="--label:#' + color + '">' +
        escapeHtml(l.name) + "</span>";
    }).join("");
  }

  /* ── list view ───────────────────────────────────────────────────────── */
  function workLedgerURL() {
    var q = "?state=" + encodeURIComponent(el("filter-state").value);
    if (el("filter-idea").checked) q += "&label=operator-idea";
    return "/api/work-ledger" + q;
  }

  function loadIssues() {
    showOnly("issues-listpanel");
    var epoch = ++viewEpoch;
    var list = el("issues-list");
    list.innerHTML = '<div class="empty">Loading fleet work ledger…</div>';
    getJSON(workLedgerURL()).then(function (doc) {
      if (epoch === viewEpoch) renderIssueList(doc);
    }).catch(function (err) {
      if (epoch === viewEpoch) list.innerHTML = '<div class="error">Could not load the work ledger: ' + escapeHtml(err.message) + "</div>";
    });
  }

  function relativeWhen(stamp, verb) {
    var at = Date.parse(stamp || "");
    if (!Number.isFinite(at)) return verb + " recently";
    var mins = Math.max(0, Math.floor((Date.now() - at) / 60000));
    if (mins < 60) return verb + " " + mins + "m ago";
    var hours = Math.floor(mins / 60);
    if (hours < 48) return verb + " " + hours + "h ago";
    return verb + " " + Math.floor(hours / 24) + "d ago";
  }

  function workRow(item, posture, flotilla, desk, compact) {
    var it = item.issue || {};
    var number = Number(it.number);
    workRowContexts[number] = {
      item: item, posture: posture, flotilla: flotilla, desk: desk,
      seats: [desk, it.desk].filter(Boolean),
    };
    var contextLine = item.goal_title
      ? '<span class="issue-context">Drives ' + escapeHtml(item.goal_title) + " · " + escapeHtml(item.goal_detail || "in flight") + "</span>"
      : '<span class="issue-context">' + escapeHtml(relativeWhen(it.closedAt, "closed")) + "</span>";
    return (
      '<div class="issue-row issue-row-' + posture + (compact ? " issue-row-compact" : "") + '" data-number="' + number + '" role="button" tabindex="0" aria-label="Open work context for issue ' + number + '">' +
        (compact
          ? '<span class="issue-state-dot ' + posture + '" aria-hidden="true"></span><span class="sr-only">shipped</span>'
          : '<span class="issue-state ' + posture + '">' + (posture === "in-flight" ? "in flight" : "shipped") + "</span>") +
        '<span class="issue-num">#' + number + "</span>" +
        '<span class="issue-copy"><span class="issue-title">' + escapeHtml(it.title) + "</span>" + (compact ? "" : contextLine) + "</span>" +
        (compact ? "" : '<span class="issue-labels">' + labelChips(it.labels) + "</span>") +
      "</div>"
    );
  }

  function renderDesk(desk, flotilla) {
    var moving = Array.isArray(desk.in_flight) ? desk.in_flight : [];
    var shipped = Array.isArray(desk.shipped) ? desk.shipped : [];
    var shippedPreview = shipped.slice(0, 10);
    var shippedRest = shipped.slice(10);
    var shippedMore = shippedRest.length
      ? '<details class="issue-shipped-more"><summary><span class="when-closed">show all ' + shipped.length +
          ' shipped</span><span class="when-open">hide ' + shippedRest.length + " older shipped</span></summary>" +
          shippedRest.map(function (it) { return workRow(it, "shipped", flotilla, desk.name); }).join("") + "</details>"
      : "";
    return '<section class="issue-desk">' +
      '<div class="issue-desk-head"><h4>' + escapeHtml(desk.name || "Unassigned") + '</h4><span>' +
        moving.length + " moving · " + shipped.length + " shipped</span></div>" +
      moving.map(function (it) { return workRow(it, "in-flight", flotilla, desk.name); }).join("") +
      shippedPreview.map(function (it) { return workRow(it, "shipped", flotilla, desk.name); }).join("") +
      shippedMore +
      "</section>";
  }

  function groupKey(flotilla, desk) {
    return String(flotilla || "Unassigned") + "\u001f" + String(desk || "Unassigned");
  }

  function renderMobileDesk(desk, flotilla) {
    var moving = Array.isArray(desk.in_flight) ? desk.in_flight : [];
    var shipped = Array.isArray(desk.shipped) ? desk.shipped : [];
    var key = groupKey(flotilla, desk.name);
    var limit = Math.max(10, shippedWindows[key] || 10);
    var shown = shipped.slice(0, limit);
    var remaining = Math.max(0, shipped.length - shown.length);
    var next = Math.min(20, remaining);
    var movingBlock = moving.length
      ? '<div class="issue-desk-head"><h4>' + escapeHtml(desk.name || "Unassigned") + '</h4><span>' +
          moving.length + " moving · " + shipped.length + " shipped</span></div>" +
        moving.map(function (it) { return workRow(it, "in-flight", flotilla, desk.name); }).join("")
      : "";
    var shippedBlock = shipped.length
      ? '<details class="issue-shipped-group" data-shipped-key="' + escapeHtml(key) + '"' + (shippedOpen[key] ? " open" : "") + ">" +
          '<summary><span class="issue-shipped-identity">' + escapeHtml(desk.name || "Unassigned") + '</span><span>' +
            shipped.length + " shipped in the last 14 days — tap to expand</span></summary>" +
          (shippedOpen[key] ? '<div class="issue-shipped-window">' + shown.map(function (it) {
              return workRow(it, "shipped", flotilla, desk.name, true);
            }).join("") +
            (remaining ? '<button type="button" class="issue-window-more" data-issue-more="' + escapeHtml(key) + '">show ' +
              next + " more of " + remaining + " ▸</button>" : "") + "</div>" : "") + "</details>"
      : "";
    return '<section class="issue-desk issue-desk-mobile">' + movingBlock + shippedBlock + "</section>";
  }

  function renderIssueList(doc) {
    lastLedgerDoc = doc;
    workRowContexts = {};
    el("issues-repo").textContent = doc.repo ? doc.repo : "";
    var flotillas = Array.isArray(doc.flotillas) ? doc.flotillas : [];
    var list = el("issues-list");
    var scopeNote = '<div class="issue-scope-note" role="note"><strong>Moving is goal-linked only</strong>' +
      "<span>Other open issues are omitted.</span></div>";
    if (!flotillas.length) {
      list.innerHTML = scopeNote + '<div class="empty">No fleet work matches this view.</div>';
      return;
    }
    list.innerHTML = scopeNote + flotillas.map(function (flotilla) {
      var desks = Array.isArray(flotilla.desks) ? flotilla.desks : [];
      return '<section class="issue-ledger-section"><div class="issue-ledger-head"><div><span class="issue-ledger-kicker">Flotilla</span>' +
        '<h3>' + escapeHtml(flotilla.name || "Unassigned") + '</h3></div><span class="issue-ledger-count">' +
        desks.length + " desk" + (desks.length === 1 ? "" : "s") + "</span></div>" +
        desks.map(function (desk) {
          return mobileLedger
            ? renderMobileDesk(desk, flotilla.name || "Unassigned")
            : renderDesk(desk, flotilla.name || "Unassigned");
        }).join("") + "</section>";
    }).join("");
    var rows = list.querySelectorAll(".issue-row");
    for (var i = 0; i < rows.length; i++) {
      rows[i].addEventListener("click", function () { openWorkContext(this); });
      rows[i].addEventListener("keydown", function (event) {
        if (event.key === "Enter" || event.key === " ") { event.preventDefault(); openWorkContext(this); }
      });
    }
    var groups = list.querySelectorAll("[data-shipped-key]");
    for (var j = 0; j < groups.length; j++) {
      groups[j].addEventListener("toggle", function () {
        var key = this.getAttribute("data-shipped-key"), open = this.open;
        if (!!shippedOpen[key] === open) return;
        var top = this.querySelector("summary").getBoundingClientRect().top;
        shippedOpen[key] = open;
        renderIssueList(lastLedgerDoc);
        requestAnimationFrame(function () {
          var nextGroups = list.querySelectorAll("[data-shipped-key]"), summary = null;
          for (var i = 0; i < nextGroups.length; i++) {
            if (nextGroups[i].getAttribute("data-shipped-key") === key) { summary = nextGroups[i].querySelector("summary"); break; }
          }
          if (!summary) return;
          window.scrollBy(0, summary.getBoundingClientRect().top - top);
          summary.focus();
        });
      });
    }
    var more = list.querySelectorAll("[data-issue-more]");
    for (var k = 0; k < more.length; k++) {
      more[k].addEventListener("click", function () {
        var key = this.getAttribute("data-issue-more");
        var top = this.getBoundingClientRect().top;
        var previous = shippedWindows[key] || 10;
        shippedWindows[key] = previous + 20;
        renderIssueList(lastLedgerDoc);
        requestAnimationFrame(function () {
          var groups = list.querySelectorAll("[data-shipped-key]"), group = null;
          for (var i = 0; i < groups.length; i++) {
            if (groups[i].getAttribute("data-shipped-key") === key) { group = groups[i]; break; }
          }
          if (!group) return;
          var rows = group.querySelectorAll(".issue-row");
          var anchor = rows[Math.min(previous, rows.length - 1)] || group.querySelector("summary");
          if (!anchor) return;
          window.scrollBy(0, anchor.getBoundingClientRect().top - top);
          anchor.focus();
        });
      });
    }
  }

  function syncMobileLedger() {
    var next = isMobileLedger();
    var overflow = el("tracker-overflow");
    var menu = el("tracker-overflow-menu");
    var controls = overflow && overflow.parentNode;
    var idea = el("filter-idea").closest(".filter-idea");
    var state = el("filter-state"), refresh = el("issues-refresh"), create = el("issues-new");
    if (overflow && menu && controls) {
      overflow.open = false;
      if (next) {
        menu.appendChild(idea);
        menu.appendChild(refresh);
        menu.appendChild(create);
      } else {
        controls.insertBefore(idea, state);
        controls.insertBefore(refresh, overflow);
        controls.insertBefore(create, overflow);
      }
    }
    if (next === mobileLedger) return;
    mobileLedger = next;
    if (lastLedgerDoc) renderIssueList(lastLedgerDoc);
  }

  function openWorkContext(row) {
    var context = workRowContexts[Number(row.getAttribute("data-number"))];
    if (context && window.flotillaWorkContext) window.flotillaWorkContext.open(context, row);
  }

  /* ── detail view ─────────────────────────────────────────────────────── */
  // openGoalFromIssue jumps to the Goals map focused on the goal-id trailer slug
  // (#580). Reuses the same restoreNode + pushNav path the Decisions "Drives"
  // link uses so a cold Goals tab still focuses the node once the map renders.
  function openGoalFromIssue(goalId) {
    var id = String(goalId || "").trim();
    if (!id) return;
    if (D.showView) D.showView("goals");
    if (window.flotillaGoals && window.flotillaGoals.restoreNode) {
      window.flotillaGoals.restoreNode(id);
    }
    if (D.pushNav) D.pushNav({ view: "goals", node: id });
  }

  function openIssue(number) {
    showOnly("issues-detail");
    var epoch = ++viewEpoch;
    el("detail-title").textContent = "#" + number;
    var body = el("detail-body");
    body.innerHTML = '<div class="empty">Loading issue #' + number + "…</div>";
    getJSON("/api/issues/" + number).then(function (it) {
      if (epoch === viewEpoch) renderIssueDetail(it);
    }).catch(function (err) {
      if (epoch === viewEpoch) body.innerHTML = '<div class="error">Could not load issue #' + number + ": " + escapeHtml(err.message) + "</div>";
    });
  }

  function renderIssueDetail(it) {
    var number = Number(it.number);
    el("detail-title").textContent = "#" + number + " " + (it.title || "");
    var state = String(it.state || "").toLowerCase();
    var comments = Array.isArray(it.comments) ? it.comments : [];
    // #580: goal-id trailer (server EnrichIssue → goal_id) surfaces as a Drives
    // chip that opens the Goals map on that node. Absent trailer → no chip.
    var goalId = String(it.goal_id || "").trim();
    var goalChip = goalId
      ? ('<div class="issue-goal">' +
          '<span class="issue-goal-lab">Drives</span> ' +
          '<button type="button" class="issue-goal-link" data-goal-goto="' + escapeHtml(goalId) +
            '" title="Open goal ' + escapeHtml(goalId) + ' on the Goals map">' +
            escapeHtml(goalId) + "</button></div>")
      : "";

    var html =
      '<div class="detail-meta">' +
        '<span class="issue-state ' + escapeHtml(state) + '">' + escapeHtml(state || "?") + "</span> " +
        '<span class="muted">by ' + escapeHtml(it.author && it.author.login) + "</span> " +
        '<span class="issue-labels">' + labelChips(it.labels) + "</span>" +
        (safeHref(it.url) ? ' <a class="issue-link" href="' + safeHref(it.url) + '" target="_blank" rel="noopener">view on GitHub ↗</a>' : "") +
      "</div>" +
      goalChip +
      '<div class="issue-body">' + escapeHtml(it.body || "") + "</div>" +
      '<h3 class="sub">Comments (' + comments.length + ")</h3>" +
      '<div class="comments">' + (
        comments.length
          ? comments.map(function (c) {
              return '<div class="comment"><div class="comment-head muted">' +
                escapeHtml(c.author && c.author.login) + " · " + escapeHtml(c.createdAt) +
                '</div><div class="comment-body">' + escapeHtml(c.body) + "</div></div>";
            }).join("")
          : '<div class="empty">No comments.</div>'
      ) + "</div>";

    // Action forms — comment, label, and the DESTRUCTIVE close (confirmed).
    if (state === "open") {
      html +=
        '<div class="detail-actions">' +
          '<textarea id="comment-body" placeholder="Add a comment…" rows="3"></textarea>' +
          '<div class="form-actions">' +
            '<span id="detail-msg" class="form-msg"></span>' +
            '<button id="comment-submit" class="btn btn-primary">Comment</button>' +
          "</div>" +
          '<div class="label-actions">' +
            '<input type="text" id="label-add" placeholder="add labels (comma-separated)" />' +
            '<input type="text" id="label-remove" placeholder="remove labels (comma-separated)" />' +
            '<button id="label-submit" class="btn">Apply labels</button>' +
          "</div>" +
          '<button id="close-submit" class="btn btn-danger">Close issue</button>' +
        "</div>";
    } else {
      html += '<div class="detail-actions"><span class="muted">This issue is ' + escapeHtml(state) + ".</span></div>";
    }

    var body = el("detail-body");
    body.innerHTML = html;
    // #580: Drives chip → Goals map (stopPropagation so it never bubbles as a row click).
    var goalBtn = body.querySelector("[data-goal-goto]");
    if (goalBtn) {
      goalBtn.addEventListener("click", function (e) {
        e.preventDefault();
        e.stopPropagation();
        openGoalFromIssue(this.getAttribute("data-goal-goto"));
      });
    }
    wireDetailActions(number, state);
  }

  function wireDetailActions(number, state) {
    if (state !== "open") return;
    var msg = el("detail-msg");
    function fail(err) { msg.className = "form-msg err"; msg.textContent = err.message; }
    function busy(on) {
      el("comment-submit").disabled = on;
      el("label-submit").disabled = on;
      el("close-submit").disabled = on;
    }

    el("comment-submit").addEventListener("click", function () {
      var b = el("comment-body").value.trim();
      if (!b) { fail(new Error("comment body is required")); return; }
      msg.className = "form-msg"; msg.textContent = "Posting…"; busy(true);
      postJSON("/api/issues/" + number + "/comments", { body: b })
        .then(function () { openIssue(number); })
        .catch(function (err) { busy(false); fail(err); });
    });

    el("label-submit").addEventListener("click", function () {
      var add = splitCsv(el("label-add").value);
      var remove = splitCsv(el("label-remove").value);
      if (!add.length && !remove.length) { fail(new Error("enter at least one label to add or remove")); return; }
      msg.className = "form-msg"; msg.textContent = "Applying…"; busy(true);
      postJSON("/api/issues/" + number + "/labels", { add: add, remove: remove })
        .then(function () { openIssue(number); })
        .catch(function (err) { busy(false); fail(err); });
    });

    // Close is DESTRUCTIVE — confirm explicitly before the POST.
    el("close-submit").addEventListener("click", function () {
      if (!window.confirm("Close issue #" + number + " on GitHub? This is a state change to the repository.")) return;
      msg.className = "form-msg"; msg.textContent = "Closing…"; busy(true);
      postJSON("/api/issues/" + number + "/close")
        .then(function () { openIssue(number); })
        .catch(function (err) { busy(false); fail(err); });
    });
  }

  /* ── create view ─────────────────────────────────────────────────────── */
  function showCreate() {
    showOnly("issues-create");
    el("create-title").value = "";
    el("create-body").value = "";
    el("create-labels").value = "";
    el("create-msg").textContent = "";
    el("create-title").focus();
  }

  function submitCreate(ev) {
    ev.preventDefault();
    var title = el("create-title").value.trim();
    var msg = el("create-msg");
    if (!title) { msg.className = "form-msg err"; msg.textContent = "title is required"; return; }
    msg.className = "form-msg"; msg.textContent = "Creating…";
    var payload = {
      title: title,
      body: el("create-body").value,
      labels: splitCsv(el("create-labels").value),
    };
    postJSON("/api/issues", payload).then(function (issue) {
      if (issue && issue.number) { openIssue(Number(issue.number)); }
      else { loadIssues(); }
    }).catch(function (err) {
      msg.className = "form-msg err"; msg.textContent = err.message;
    });
  }

  /* ── view toggles within the tracker ─────────────────────────────────── */
  // Exactly one of the three tracker panels (list / create / detail) is visible.
  function showOnly(id) {
    if (id !== "issues-listpanel" && window.flotillaWorkContext) window.flotillaWorkContext.close();
    ["issues-listpanel", "issues-create", "issues-detail"].forEach(function (s) {
      el(s).classList.toggle("hidden", s !== id);
    });
  }

  function splitCsv(s) {
    return String(s || "").split(",").map(function (x) { return x.trim(); }).filter(Boolean);
  }

  /* ── wiring ──────────────────────────────────────────────────────────── */
  el("issues-refresh").addEventListener("click", loadIssues);
  el("filter-idea").addEventListener("change", loadIssues);
  el("filter-state").addEventListener("change", loadIssues);
  el("issues-new").addEventListener("click", showCreate);
  el("create-cancel").addEventListener("click", loadIssues);
  el("create-form").addEventListener("submit", submitCreate);
  el("detail-back").addEventListener("click", loadIssues);
  var mobileResizeTimer = null;
  window.addEventListener("resize", function () {
    clearTimeout(mobileResizeTimer);
    mobileResizeTimer = setTimeout(syncMobileLedger, 120);
  });
  syncMobileLedger();

  // Exposed to dash.js: load issues the first time the Issues tab is shown.
  window.flotillaTracker = {
    show: function () { if (!loaded) { loaded = true; loadIssues(); } },
    openIssue: openIssue,
  };
})();
