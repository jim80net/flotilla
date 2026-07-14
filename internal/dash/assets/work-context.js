/* Work Context panel (#716): one Issues-hosted view over the existing mirror + route primitives. */
(function () {
  "use strict";

  var D = window.flotillaDash;
  if (!D) return;

  var selected = null;
  var statusDoc = null;
  var topologyDoc = null;
  var activeSeat = "";
  var mirrorDoc = null;
  var localInjects = [];
  var contextEpoch = 0;
  var mirrorEpoch = 0;
  var issueEpoch = 0;
  var pollTimer = null;
  var ageTimer = null;
  var returnFocus = null;
  var composeInFlight = false;
  var loadedAll = false;
  var streamPinned = true;

  function el(id) { return document.getElementById(id); }
  function esc(value) { return D.escapeHtml(value); }
  function panelOpen() { return selected && !el("work-context").hidden; }

  function statusAgent(name) {
    var agents = statusDoc && Array.isArray(statusDoc.agents) ? statusDoc.agents : [];
    var want = String(name || "").toLowerCase();
    for (var i = 0; i < agents.length; i++) {
      if (String(agents[i].name || "").toLowerCase() === want) return agents[i];
    }
    return null;
  }

  function seatsForSelection() {
    var raw = selected && Array.isArray(selected.seats) ? selected.seats : [];
    var seen = {};
    return raw.filter(function (seat) {
      var key = String(seat || "").trim().toLowerCase();
      if (!key || key === "unassigned" || seen[key] || !statusAgent(seat)) return false;
      seen[key] = true;
      return true;
    });
  }

  function owningXO(seat) {
    var nodes = topologyDoc && Array.isArray(topologyDoc.org_nodes) ? topologyDoc.org_nodes : [];
    var byID = {};
    nodes.forEach(function (node) { byID[String(node.id || "").toLowerCase()] = node; });
    var node = byID[String(seat || "").toLowerCase()];
    for (var depth = 0; node && node.parent && depth < 12; depth++) {
      var parent = String(node.parent);
      if (statusAgent(parent)) return parent;
      node = byID[parent.toLowerCase()];
    }
    var flotilla = String((selected && selected.flotilla) || "");
    return flotilla && flotilla.toLowerCase() !== "unassigned" && flotilla.toLowerCase() !== String(seat).toLowerCase()
      ? flotilla : "";
  }

  function workAge() {
    var issue = selected && selected.item && selected.item.issue ? selected.item.issue : {};
    var stamp = String(issue.state || "").toLowerCase() === "closed" ? issue.closedAt : issue.createdAt;
    return stamp ? D.relativeTime(stamp) : "age unavailable";
  }

  function renderOwnership() {
    if (!selected) return;
    var issue = selected.item.issue || {};
    var posture = selected.posture === "in-flight" ? "in flight" : "shipped";
    var seats = seatsForSelection();
    if (activeSeat && seats.indexOf(activeSeat) < 0) activeSeat = "";
    if (!activeSeat && seats.length) activeSeat = seats[0];
    var xo = activeSeat ? owningXO(activeSeat) : owningXO(selected.desk);
    var chips = '<span class="wc-chip wc-flotilla">' + esc(selected.flotilla || "Unassigned") + "</span>" +
      '<span class="wc-chip">desk · ' + esc(selected.desk || "Unassigned") + "</span>" +
      (xo ? '<span class="wc-chip">XO · ' + esc(xo) + "</span>" : "");
    var switcher = "";
    if (seats.length > 1) {
      switcher = '<div class="wc-seat-switcher" role="group" aria-label="Active seat">' + seats.map(function (seat) {
        var agent = statusAgent(seat) || {};
        return '<button type="button" data-wc-seat="' + esc(seat) + '" class="wc-seat' + (seat === activeSeat ? " active" : "") +
          '" aria-pressed="' + (seat === activeSeat ? "true" : "false") + '">' + esc(seat) +
          '<span aria-hidden="true">' + (agent.state === "idle" || agent.state === "working" ? " ●" : "") + "</span></button>";
      }).join("") + "</div>";
    } else if (seats.length === 1) {
      var one = statusAgent(seats[0]) || {};
      switcher = '<span class="wc-seat-static">' + esc(seats[0]) + " · " + esc(one.state || "unknown") + "</span>";
    }
    el("wc-header").innerHTML =
      '<div class="wc-eyebrow">ISSUE · <span>' + esc(posture) + "</span> · " + esc(workAge()) + "</div>" +
      '<h2 id="wc-title">' + esc(issue.title || "Work context") + "</h2>" +
      '<div class="wc-ownership">' + chips + switcher + "</div>";
    var buttons = el("wc-header").querySelectorAll("[data-wc-seat]");
    for (var i = 0; i < buttons.length; i++) {
      buttons[i].addEventListener("click", function () { selectSeat(this.getAttribute("data-wc-seat")); });
    }
    renderSeatState();
  }

  function renderSeatState() {
    var composer = el("wc-composer");
    var input = el("wc-composer-input");
    var loadEarlier = el("wc-load-earlier");
    if (!activeSeat) {
      composer.hidden = true;
      loadEarlier.hidden = true;
      var unavailable = statusDoc && statusDoc.error;
      el("wc-live-contract").textContent = unavailable
        ? "live seat status unavailable — stream unavailable"
        : "no live seat mapped — stream unavailable";
      el("wc-stream").innerHTML = '<div class="wc-seatless">Ownership and GitHub context remain available for this work.</div>';
      return;
    }
    composer.hidden = false;
    loadEarlier.hidden = false;
    var fullPlaceholder = "Message " + activeSeat + " — delivered into its pane via the control route…";
    input.placeholder = window.innerWidth <= 740 && fullPlaceholder.length > 32
      ? fullPlaceholder.slice(0, 31) + "…" : fullPlaceholder;
    input.title = fullPlaceholder;
    input.setAttribute("aria-label", "Message " + activeSeat);
    renderMirror();
  }

  function mirrorAgeSeconds() {
    var entries = mirrorDoc && Array.isArray(mirrorDoc.entries) ? mirrorDoc.entries : [];
    if (!entries.length || !entries[entries.length - 1].ts) return null;
    var time = Date.parse(entries[entries.length - 1].ts);
    return isNaN(time) ? null : Math.max(0, Math.floor((Date.now() - time) / 1000));
  }

  function updateContract() {
    if (!activeSeat) return;
    var seconds = mirrorAgeSeconds();
    el("wc-live-contract").textContent = seconds === null
      ? "● live — mirror awaiting first update · SSE push, 15s poll fallback"
      : "● live — mirror updated " + seconds + "s ago · SSE push, 15s poll fallback";
  }

  function renderMirror(preserve) {
    if (!activeSeat) return;
    var entries = mirrorDoc && Array.isArray(mirrorDoc.entries) ? mirrorDoc.entries : [];
    var stream = el("wc-stream");
    var html = entries.length
      ? D.renderMirrorEntries(activeSeat, entries)
      : '<div class="empty">' + (mirrorDoc && mirrorDoc.error ? "Session mirror unavailable." : "No session mirror yet for this seat.") + "</div>";
    localInjects.filter(function (item) { return item.target === activeSeat; }).forEach(function (item) {
      html += D.renderOperatorInject(item.target, item.body, item.ts);
    });
    stream.innerHTML = html;
    updateContract();
    requestAnimationFrame(function () {
      if (preserve) stream.scrollTop = preserve.top + (stream.scrollHeight - preserve.height);
      else if (streamPinned) stream.scrollTop = stream.scrollHeight;
    });
  }

  function fetchMirror(all, preserveOlder) {
    if (!panelOpen() || !activeSeat) return Promise.resolve();
    var seat = activeSeat;
    var epoch = ++mirrorEpoch;
    var stream = el("wc-stream");
    var preserve = preserveOlder ? { top: stream.scrollTop, height: stream.scrollHeight } : null;
    var path = "/api/session-mirror?agent=" + encodeURIComponent(seat) + (all ? "&limit=0" : "&limit=500");
    return D.getJSON(path).then(function (doc) {
      if (!panelOpen() || activeSeat !== seat || epoch !== mirrorEpoch) return;
      mirrorDoc = doc;
      loadedAll = !!all;
      el("wc-load-earlier").disabled = loadedAll;
      el("wc-load-earlier").textContent = loadedAll ? "full session scrollback loaded" : "↑ load earlier";
      renderMirror(preserve);
    }).catch(function (err) {
      if (!panelOpen() || activeSeat !== seat || epoch !== mirrorEpoch) return;
      mirrorDoc = { agent: seat, entries: [], error: err.message };
      renderMirror();
    });
  }

  function selectSeat(seat) {
    if (!seat || seat === activeSeat) return;
    activeSeat = seat;
    mirrorDoc = null;
    loadedAll = false;
    streamPinned = true;
    resetComposer();
    renderOwnership();
    fetchMirror(false);
  }

  function resetComposer() {
    var input = el("wc-composer-input");
    input.value = "";
    input.style.height = "";
    el("wc-composer-msg").textContent = "";
    el("wc-composer-msg").className = "form-msg";
  }

  function renderGitHub(issue) {
    var details = el("wc-github");
    if (!selected || !selected.item.issue || !selected.item.issue.number) {
      details.hidden = true;
      return;
    }
    details.hidden = false;
    var number = selected.item.issue.number;
    var comments = issue && Array.isArray(issue.comments) ? issue.comments : [];
    el("wc-github-summary").textContent = "GitHub #" + number + " — issue body · " + comments.length +
      " comment" + (comments.length === 1 ? "" : "s") + " (read-only mirror)";
    var body = issue && issue.body ? issue.body : "No issue body.";
    el("wc-github-body").innerHTML = '<button type="button" class="btn wc-open-full-issue">Open full issue</button>' +
      '<div class="wc-github-copy">' + esc(body) + "</div>" +
      (comments.length ? '<div class="wc-comments">' + comments.map(function (comment) {
        return '<article class="comment"><div class="comment-head muted">' + esc(comment.author && comment.author.login) +
          " · " + esc(comment.createdAt || "") + '</div><div class="comment-body">' + esc(comment.body || "") + "</div></article>";
      }).join("") + "</div>" : "");
    el("wc-github-body").querySelector(".wc-open-full-issue").addEventListener("click", function () {
      if (window.flotillaTracker && window.flotillaTracker.openIssue) window.flotillaTracker.openIssue(number);
    });
  }

  function fetchLiveContext() {
    var epoch = ++contextEpoch;
    var statusRequest = D.getJSON("/api/status").catch(function (err) { return { agents: [], error: err.message }; });
    var topologyRequest = D.getJSON("/api/topology").catch(function () { return { org_nodes: [] }; });
    return Promise.all([statusRequest, topologyRequest]).then(function (docs) {
      if (!panelOpen() || epoch !== contextEpoch) return;
      statusDoc = docs[0];
      topologyDoc = docs[1];
      renderOwnership();
      if (activeSeat) fetchMirror(loadedAll, false);
    });
  }

  function fetchIssueOnce() {
    var issue = selected && selected.item ? selected.item.issue || {} : {};
    if (!issue.number) { renderGitHub(null); return Promise.resolve(); }
    var epoch = issueEpoch;
    return D.getJSON("/api/issues/" + issue.number).then(function (doc) {
      if (panelOpen() && epoch === issueEpoch) renderGitHub(doc);
    }).catch(function () {
      if (panelOpen() && epoch === issueEpoch) renderGitHub(null);
    });
  }

  function startRefresh() {
    stopRefresh();
    scheduleFallback();
    ageTimer = setInterval(updateContract, 1000);
  }
  function scheduleFallback() {
    if (pollTimer) clearTimeout(pollTimer);
    pollTimer = setTimeout(function () {
      fetchLiveContext();
      scheduleFallback();
    }, 15000);
  }
  function stopRefresh() {
    if (pollTimer) clearTimeout(pollTimer);
    if (ageTimer) clearInterval(ageTimer);
    pollTimer = ageTimer = null;
  }

  function open(context, source) {
    contextEpoch++;
    mirrorEpoch++;
    issueEpoch++;
    selected = context;
    activeSeat = "";
    mirrorDoc = null;
    localInjects = [];
    loadedAll = false;
    streamPinned = true;
    resetComposer();
    returnFocus = source || document.activeElement;
    var panel = el("work-context");
    panel.hidden = false;
    el("issues-workspace").classList.add("has-context");
    document.body.classList.add("work-context-open");
    el("wc-header").innerHTML = '<div class="wc-eyebrow">ISSUE · loading live context</div><h2 id="wc-title">' +
      esc(context.item.issue.title || "Work context") + "</h2>";
    el("wc-github").hidden = !context.item.issue.number;
    el("wc-live-contract").textContent = "Loading live seat…";
    el("wc-stream").innerHTML = '<div class="empty">Loading session mirror…</div>';
    el("wc-composer").hidden = true;
    startRefresh();
    fetchLiveContext();
    fetchIssueOnce(); // GitHub is external/subprocess-backed: once per explicit panel open.
    el("wc-close").focus();
  }

  function close() {
    if (!panelOpen()) return;
    contextEpoch++;
    mirrorEpoch++;
    issueEpoch++;
    stopRefresh();
    selected = null;
    activeSeat = "";
    el("work-context").hidden = true;
    el("issues-workspace").classList.remove("has-context");
    document.body.classList.remove("work-context-open");
    if (returnFocus && returnFocus.focus) returnFocus.focus();
  }

  el("wc-close").addEventListener("click", close);
  el("wc-load-earlier").addEventListener("click", function () { streamPinned = false; fetchMirror(true, true); });
  el("wc-stream").addEventListener("scroll", function () {
    var stream = el("wc-stream");
    streamPinned = stream.scrollHeight - stream.scrollTop - stream.clientHeight < 48;
  });
  window.addEventListener("resize", function () { if (panelOpen()) renderSeatState(); });
  document.addEventListener("keydown", function (event) { if (event.key === "Escape" && panelOpen()) close(); });
  D.onLiveUpdate(function () {
    if (!panelOpen()) return;
    fetchLiveContext();
    scheduleFallback();
  });

  function onViewChange(view) { if (view !== "issues" && panelOpen()) close(); }

  (function wireComposer() {
    var form = el("wc-composer");
    var input = el("wc-composer-input");
    var msg = el("wc-composer-msg");
    function setMessage(text, kind) {
      msg.className = "form-msg" + (kind ? " " + kind : "");
      msg.textContent = text;
    }
    function resize() {
      input.style.height = "auto";
      input.style.height = Math.min(input.scrollHeight, 120) + "px";
    }
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      if (composeInFlight) return;
      var target = activeSeat;
      var body = input.value.trim();
      if (!target) { setMessage("No live seat is mapped to this work.", "err"); return; }
      if (!body) { setMessage("Type a message.", "err"); return; }
      var button = form.querySelector("button[type=submit]");
      composeInFlight = true;
      button.disabled = true;
      setMessage("Sending…", "");
      D.routeMessage(target, body).then(function (res) {
        var copy = D.routeOutcomeCopy(res);
        if (!panelOpen() || target !== activeSeat) return;
        if (copy.ok) {
          localInjects.push({ target: target, body: body, ts: new Date().toISOString() });
          input.value = "";
          resize();
          renderMirror();
          fetchMirror(loadedAll, false);
        }
        setMessage(copy.text, copy.ok ? "ok" : "");
      }).catch(function (err) {
        if (panelOpen() && target === activeSeat) setMessage(err.message, "err");
      }).then(function () {
        composeInFlight = false;
        button.disabled = false;
      });
    });
    input.addEventListener("keydown", function (event) {
      if (event.key === "Enter" && !event.shiftKey) {
        event.preventDefault();
        if (!composeInFlight) form.requestSubmit();
      }
    });
    input.addEventListener("input", resize);
  })();

  window.flotillaWorkContext = { open: open, close: close, refresh: fetchLiveContext, onViewChange: onViewChange };
})();
