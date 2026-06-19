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
      var color = /^[0-9a-fA-F]{6}$/.test(l.color || "") ? l.color : "888888";
      return '<span class="issue-label" style="border-color:#' + color + ';color:#' + color + '">' +
        escapeHtml(l.name) + "</span>";
    }).join("");
  }

  /* ── list view ───────────────────────────────────────────────────────── */
  function currentFilter() {
    var q = "?state=" + encodeURIComponent(el("filter-state").value);
    if (el("filter-idea").checked) q += "&label=operator-idea";
    return "/api/issues" + q;
  }

  function loadIssues() {
    showOnly("issues-listpanel");
    var epoch = ++viewEpoch;
    var list = el("issues-list");
    list.innerHTML = '<div class="empty">Loading issues…</div>';
    getJSON(currentFilter()).then(function (doc) {
      if (epoch === viewEpoch) renderIssueList(doc);
    }).catch(function (err) {
      if (epoch === viewEpoch) list.innerHTML = '<div class="error">Could not load issues: ' + escapeHtml(err.message) + "</div>";
    });
  }

  function renderIssueList(doc) {
    el("issues-repo").textContent = doc.repo ? doc.repo : "";
    var issues = Array.isArray(doc.issues) ? doc.issues : [];
    var list = el("issues-list");
    if (!issues.length) {
      list.innerHTML = '<div class="empty">No issues match this filter.</div>';
      return;
    }
    list.innerHTML = issues.map(function (it) {
      var state = String(it.state || "").toLowerCase();
      return (
        '<div class="issue-row" data-number="' + Number(it.number) + '">' +
          '<span class="issue-state ' + escapeHtml(state) + '">' + escapeHtml(state || "?") + "</span>" +
          '<span class="issue-num">#' + Number(it.number) + "</span>" +
          '<span class="issue-title">' + escapeHtml(it.title) + "</span>" +
          '<span class="issue-labels">' + labelChips(it.labels) + "</span>" +
        "</div>"
      );
    }).join("");
    var rows = list.querySelectorAll(".issue-row");
    for (var i = 0; i < rows.length; i++) {
      rows[i].addEventListener("click", function () { openIssue(Number(this.getAttribute("data-number"))); });
    }
  }

  /* ── detail view ─────────────────────────────────────────────────────── */
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

    var html =
      '<div class="detail-meta">' +
        '<span class="issue-state ' + escapeHtml(state) + '">' + escapeHtml(state || "?") + "</span> " +
        '<span class="muted">by ' + escapeHtml(it.author && it.author.login) + "</span> " +
        '<span class="issue-labels">' + labelChips(it.labels) + "</span>" +
        (safeHref(it.url) ? ' <a class="issue-link" href="' + safeHref(it.url) + '" target="_blank" rel="noopener">view on GitHub ↗</a>' : "") +
      "</div>" +
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

  // Exposed to dash.js: load issues the first time the Issues tab is shown.
  window.flotillaTracker = {
    show: function () { if (!loaded) { loaded = true; loadIssues(); } },
  };
})();
