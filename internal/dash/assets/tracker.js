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
      // Pass GitHub's raw label hue as --label; the CSS derives a legible chip
      // (dark-ink text on a faint tint) so a light GitHub color (e.g. #a2eeef)
      // never washes out on the warm-light surface. See dash.css .issue-label.
      var color = /^[0-9a-fA-F]{6}$/.test(l.color || "") ? l.color : "888888";
      return '<span class="issue-label" style="--label:#' + color + '">' +
        escapeHtml(l.name) + "</span>";
    }).join("");
  }

  /* ── list view ───────────────────────────────────────────────────────── */
  var RECENT_SHIPPED_DAYS = 14;

  function workLedgerURL() {
    // One bounded read supplies both sides of the operator ledger. "In flight" is
    // derived from /api/goals below — never equated with every open GitHub issue.
    var q = "?state=all&limit=200";
    if (el("filter-idea").checked) q += "&label=operator-idea";
    return "/api/issues" + q;
  }

  function loadIssues() {
    showOnly("issues-listpanel");
    var epoch = ++viewEpoch;
    var list = el("issues-list");
    list.innerHTML = '<div class="empty">Loading fleet work ledger…</div>';
    Promise.all([
      getJSON(workLedgerURL()),
      getJSON("/api/goals").catch(function (err) {
        return { found: false, error: err.message || "goals context unavailable" };
      }),
    ]).then(function (docs) {
      if (epoch === viewEpoch) renderIssueList(docs[0], docs[1]);
    }).catch(function (err) {
      if (epoch === viewEpoch) list.innerHTML = '<div class="error">Could not load the work ledger: ' + escapeHtml(err.message) + "</div>";
    });
  }

  function issueNumberFromRef(ref) {
    var m = String(ref || "").match(/#([1-9][0-9]*)$/);
    return m ? Number(m[1]) : 0;
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

  function inFlightContext(goalsDoc) {
    var byIssue = {};
    var goals = goalsDoc && Array.isArray(goalsDoc.goals) ? goalsDoc.goals : [];
    goals.forEach(function (goal) {
      (goal.work_items || []).forEach(function (wi) {
        if (String(wi.kind || "").toLowerCase() !== "issue" || wi.class !== "in-flight") return;
        var number = issueNumberFromRef(wi.ref);
        if (!number || byIssue[number]) return;
        byIssue[number] = {
          goal: goal.title || goal.id || "fleet goal",
          goalId: goal.id || "",
          detail: wi.detail || "in flight",
        };
      });
    });
    return byIssue;
  }

  function workRow(it, posture, context) {
    var number = Number(it.number);
    var contextLine = context
      ? '<span class="issue-context">Drives ' + escapeHtml(context.goal) + " · " + escapeHtml(context.detail) + "</span>"
      : '<span class="issue-context">' + escapeHtml(relativeWhen(it.closedAt, "closed")) + "</span>";
    return (
      '<div class="issue-row issue-row-' + posture + '" data-number="' + number + '">' +
        '<span class="issue-state ' + posture + '">' + (posture === "in-flight" ? "in flight" : "shipped") + "</span>" +
        '<span class="issue-num">#' + number + "</span>" +
        '<span class="issue-copy"><span class="issue-title">' + escapeHtml(it.title) + "</span>" + contextLine + "</span>" +
        '<span class="issue-labels">' + labelChips(it.labels) + "</span>" +
      "</div>"
    );
  }

  function ledgerSection(title, description, issues, posture, contexts, empty) {
    var rows = issues.length
      ? issues.map(function (it) { return workRow(it, posture, contexts && contexts[it.number]); }).join("")
      : '<div class="issue-ledger-empty">' + escapeHtml(empty) + "</div>";
    return '<section class="issue-ledger-section issue-ledger-' + posture + '">' +
      '<div class="issue-ledger-head"><div><h3>' + escapeHtml(title) + '</h3><p>' + escapeHtml(description) +
      '</p></div><span class="issue-ledger-count">' + issues.length + "</span></div>" + rows + "</section>";
  }

  function renderIssueList(doc, goalsDoc) {
    el("issues-repo").textContent = doc.repo ? doc.repo : "";
    var issues = Array.isArray(doc.issues) ? doc.issues.slice() : [];
    issues.sort(function (a, b) { return Date.parse(b.updatedAt || "") - Date.parse(a.updatedAt || ""); });
    var list = el("issues-list");
    if (!issues.length) {
      list.innerHTML = '<div class="empty">No fleet work matches this view.</div>';
      return;
    }
    var contexts = inFlightContext(goalsDoc);
    var inFlight = issues.filter(function (it) {
      return String(it.state || "").toLowerCase() === "open" && !!contexts[Number(it.number)];
    });
    var cutoff = Date.now() - RECENT_SHIPPED_DAYS * 24 * 60 * 60 * 1000;
    var shipped = issues.filter(function (it) {
      return String(it.state || "").toLowerCase() === "closed" && Date.parse(it.closedAt || "") >= cutoff;
    }).slice(0, 12);
    var inFlightEmpty = goalsDoc && goalsDoc.found !== false
      ? "No issue-linked work is currently in flight."
      : "Goals context is unavailable, so in-flight work cannot be classified safely.";
    list.innerHTML =
      ledgerSection("In flight", "Issue-linked work the goals graph says is moving now.", inFlight, "in-flight", contexts, inFlightEmpty) +
      ledgerSection("Recently shipped", "Issues closed in the last 14 days.", shipped, "shipped", null, "Nothing closed in this window.");
    var rows = list.querySelectorAll(".issue-row");
    for (var i = 0; i < rows.length; i++) {
      rows[i].addEventListener("click", function () { openIssue(Number(this.getAttribute("data-number"))); });
    }
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
  el("issues-new").addEventListener("click", showCreate);
  el("create-cancel").addEventListener("click", loadIssues);
  el("create-form").addEventListener("submit", submitCreate);
  el("detail-back").addEventListener("click", loadIssues);

  // Exposed to dash.js: load issues the first time the Issues tab is shown.
  window.flotillaTracker = {
    show: function () { if (!loaded) { loaded = true; loadIssues(); } },
  };
})();
