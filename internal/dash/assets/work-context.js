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
  var streamVisible = 20;
  var expandedBodies = {};
  var savedMobileScrollY = null;

  function el(id) { return document.getElementById(id); }
  function esc(value) { return D.escapeHtml(value); }
  function panelOpen() { return selected && !el("work-context").hidden; }
  function streamViewport() {
    return window.matchMedia && window.matchMedia("(max-width: 740px)").matches
      ? el("wc-stream").parentNode : el("wc-stream");
  }
  function subject() {
    return selected && selected.item ? (selected.item.goal || selected.item.issue || {}) : {};
  }
  function goalContext() { return !!(selected && selected.item && selected.item.goal); }
  function contextView() { return goalContext() ? "goals" : "issues"; }

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
    return !goalContext() && flotilla && flotilla.toLowerCase() !== "unassigned" && flotilla.toLowerCase() !== String(seat).toLowerCase()
      ? flotilla : "";
  }

  function workAge() {
    var item = subject();
    if (goalContext()) {
      return item.achieved_at && !item.achieved_seed
        ? "realized " + D.relativeTime(item.achieved_at) : "age unavailable";
    }
    var closed = String(item.state || "").toLowerCase() === "closed";
    var stamp = closed ? item.closedAt : item.createdAt;
    return stamp ? (closed ? "closed " : "opened ") + D.relativeTime(stamp) : "age unavailable";
  }

  function renderOwnership() {
    if (!selected) return;
    var item = subject();
    var scope = goalContext() ? "GOAL" : "ISSUE";
    var posture = String(selected.posture || "unknown").replace(/-/g, " ");
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
      '<div class="wc-eyebrow">' + scope + ' · <span>' + esc(posture) + "</span> · " + esc(workAge()) + "</div>" +
      '<h2 id="wc-title">' + esc(item.title || "Work context") + "</h2>" +
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

  function humanAge(seconds) {
    if (seconds < 60) return seconds + "s";
    var minutes = Math.floor(seconds / 60);
    if (minutes < 60) return minutes + "m";
    var hours = Math.floor(minutes / 60), remMinutes = minutes % 60;
    if (hours < 24) return hours + "h" + (remMinutes ? " " + remMinutes + "m" : "");
    var days = Math.floor(hours / 24), remHours = hours % 24;
    return days + "d" + (remHours ? " " + remHours + "h" : "");
  }

  function updateContract() {
    if (!activeSeat) return;
    var seconds = mirrorAgeSeconds();
    var contract = el("wc-live-contract");
    var idle = seconds !== null && seconds >= 300;
    contract.classList.toggle("is-idle", idle);
    contract.textContent = seconds === null
      ? "● live — mirror awaiting first update · SSE push, 15s poll fallback"
      : (idle ? "◐ stream idle" : "● live") + " — mirror updated " + humanAge(seconds) +
        " ago · SSE push, 15s poll fallback";
  }

  function decorateMessageBodies(keys) {
    var stream = el("wc-stream");
    var bodies = stream.querySelectorAll(".thread-mirror-body, .thread-gist");
    for (var i = 0; i < bodies.length; i++) {
      var body = bodies[i];
      // Two live refreshes can queue decoration frames against the same rendered
      // DOM. The first frame owns the control; later frames must be idempotent.
      if (body.classList.contains("wc-message-clamped")) continue;
      var key = keys[i] || (activeSeat + "|body|" + i);
      var style = window.getComputedStyle(body);
      var lineHeight = parseFloat(style.lineHeight) || (parseFloat(style.fontSize) || 12) * 1.4;
      var lines = Math.max(1, Math.ceil(body.scrollHeight / lineHeight));
      if (lines <= 8) continue;
      var more = lines - 8;
      body.classList.add("wc-message-clamped");
      body.classList.toggle("is-expanded", !!expandedBodies[key]);
      body.setAttribute("data-wc-body-key", key);
      body.id = "wc-message-body-" + i;
      var button = document.createElement("button");
      button.type = "button";
      button.className = "wc-message-toggle";
      button.setAttribute("data-wc-body-toggle", key);
      button.setAttribute("data-wc-more-lines", String(more));
      button.setAttribute("aria-controls", body.id);
      button.setAttribute("aria-expanded", String(!!expandedBodies[key]));
      button.textContent = expandedBodies[key]
        ? "collapse turn-final ▴"
        : "show full turn-final ▾ (" + more + " more lines)";
      body.parentNode.insertBefore(button, body.nextSibling);
    }
  }

  function updateLoadEarlier(entries, mirrorLimit) {
    var button = el("wc-load-earlier");
    var hidden = Math.max(0, entries.length - mirrorLimit);
    if (hidden) {
      button.disabled = false;
      button.textContent = "↑ show " + Math.min(20, hidden) + " more of " + hidden;
    } else if (!loadedAll) {
      button.disabled = false;
      button.textContent = "↑ load earlier";
    } else {
      button.disabled = true;
      button.textContent = "full session scrollback loaded";
    }
  }

  function renderMirror(preserve) {
    if (!activeSeat) return;
    var entries = mirrorDoc && Array.isArray(mirrorDoc.entries) ? mirrorDoc.entries : [];
    var stream = el("wc-stream");
    var activeInjects = localInjects.filter(function (item) { return item.target === activeSeat; });
    var mirrorLimit = Math.max(0, streamVisible - activeInjects.length);
    var start = Math.max(0, entries.length - mirrorLimit);
    var visible = entries.slice(start);
    var bodyKeys = visible.map(function (entry) {
      return activeSeat + "|mirror|" + String(entry.ts || "") + "|" + String(entry.info || "").slice(0, 80);
    });
    var html = visible.length
      ? D.renderMirrorEntries(activeSeat, visible)
      : '<div class="empty">' + (mirrorDoc && mirrorDoc.error ? "Session mirror unavailable." : "No session mirror yet for this seat.") + "</div>";
    activeInjects.forEach(function (item, index) {
      html += D.renderOperatorInject(item.target, item.body, item.ts);
      bodyKeys.push(activeSeat + "|local|" + String(item.ts || "") + "|" + index);
    });
    stream.innerHTML = html;
    updateLoadEarlier(entries, mirrorLimit);
    updateContract();
    requestAnimationFrame(function () {
      decorateMessageBodies(bodyKeys);
      var viewport = streamViewport();
      if (preserve) viewport.scrollTop = preserve.top + (viewport.scrollHeight - preserve.height);
      else if (streamPinned) viewport.scrollTop = viewport.scrollHeight;
    });
  }

  function fetchMirror(all, preserveOlder) {
    if (!panelOpen() || !activeSeat) return Promise.resolve();
    var seat = activeSeat;
    var epoch = ++mirrorEpoch;
    var viewport = streamViewport();
    var preserve = preserveOlder ? { top: viewport.scrollTop, height: viewport.scrollHeight } : null;
    var path = "/api/session-mirror?agent=" + encodeURIComponent(seat) + (all ? "&limit=0" : "&limit=500");
    return D.getJSON(path).then(function (doc) {
      if (!panelOpen() || activeSeat !== seat || epoch !== mirrorEpoch) return;
      mirrorDoc = doc;
      loadedAll = !!all;
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
    streamVisible = 20;
    expandedBodies = {};
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

  function mountPanel() {
    var panel = el("work-context");
    var host = goalContext() ? el("goals-graph") : el("issues-workspace");
    if (host && panel.parentNode !== host) host.appendChild(panel);
    el("issues-workspace").classList.toggle("has-context", !goalContext());
    el("goals-graph").classList.toggle("has-context", goalContext());
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
    streamVisible = 20;
    expandedBodies = {};
    resetComposer();
    returnFocus = source || document.activeElement;
    var panel = el("work-context");
    mountPanel();
    panel.hidden = false;
    savedMobileScrollY = window.matchMedia && window.matchMedia("(max-width: 740px)").matches
      ? window.scrollY : null;
    document.body.classList.add("work-context-open");
    document.documentElement.classList.add("work-context-open");
    el("wc-header").innerHTML = '<div class="wc-eyebrow">' + (goalContext() ? "GOAL" : "ISSUE") +
      ' · loading live context</div><h2 id="wc-title">' +
      esc(subject().title || "Work context") + "</h2>";
    el("wc-github").hidden = !context.item.issue || !context.item.issue.number;
    el("wc-live-contract").textContent = "Loading live seat…";
    el("wc-stream").innerHTML = '<div class="empty">Loading session mirror…</div>';
    el("wc-composer").hidden = true;
    startRefresh();
    fetchLiveContext();
    fetchIssueOnce(); // GitHub is external/subprocess-backed: once per explicit panel open.
    el("wc-close").focus();
  }

  function update(context) {
    if (!panelOpen() || !context || goalContext() !== !!(context.item && context.item.goal)) return;
    selected = context;
    renderOwnership();
  }

  function close() {
    if (!panelOpen()) return;
    contextEpoch++;
    mirrorEpoch++;
    issueEpoch++;
    stopRefresh();
    var closingGoal = goalContext();
    selected = null;
    activeSeat = "";
    el("work-context").hidden = true;
    el("issues-workspace").classList.remove("has-context");
    el("goals-graph").classList.remove("has-context");
    document.body.classList.remove("work-context-open");
    document.documentElement.classList.remove("work-context-open");
    if (returnFocus && returnFocus.focus) returnFocus.focus();
    if (savedMobileScrollY !== null) {
      var restoreY = savedMobileScrollY;
      savedMobileScrollY = null;
      requestAnimationFrame(function () { window.scrollTo(0, restoreY); });
    }
    if (closingGoal && window.flotillaGoals && window.flotillaGoals.contextClosed) {
      window.flotillaGoals.contextClosed();
    }
  }

  el("wc-close").addEventListener("click", close);
  el("wc-load-earlier").addEventListener("click", function () {
    var entries = mirrorDoc && Array.isArray(mirrorDoc.entries) ? mirrorDoc.entries : [];
    streamPinned = false;
    streamVisible += 20;
    var mirrorLimit = Math.max(0, streamVisible - localInjects.filter(function (item) { return item.target === activeSeat; }).length);
    if (mirrorLimit <= entries.length || loadedAll) {
      var viewport = streamViewport();
      renderMirror({ top: viewport.scrollTop, height: viewport.scrollHeight });
    } else fetchMirror(true, true);
  });
  function trackStreamPin(event) {
    var viewport = streamViewport();
    if (event && event.currentTarget !== viewport) return;
    streamPinned = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight < 48;
  }
  el("wc-stream").addEventListener("scroll", trackStreamPin);
  el("wc-stream").parentNode.addEventListener("scroll", trackStreamPin);
  el("wc-stream").addEventListener("click", function (event) {
    var button = event.target.closest("[data-wc-body-toggle]");
    if (!button) return;
    var key = button.getAttribute("data-wc-body-toggle");
    var body = button.previousElementSibling;
    var viewport = streamViewport();
    var message = button.closest(".thread-msg");
    var top = message ? message.getBoundingClientRect().top : 0;
    expandedBodies[key] = !expandedBodies[key];
    body.classList.toggle("is-expanded", expandedBodies[key]);
    button.setAttribute("aria-expanded", String(expandedBodies[key]));
    button.textContent = expandedBodies[key]
      ? "collapse turn-final ▴"
      : "show full turn-final ▾ (" + button.getAttribute("data-wc-more-lines") + " more lines)";
    if (!expandedBodies[key] && message) {
      requestAnimationFrame(function () { viewport.scrollTop += message.getBoundingClientRect().top - top; });
    }
  });
  window.addEventListener("resize", function () { if (panelOpen()) renderSeatState(); });
  document.addEventListener("keydown", function (event) { if (event.key === "Escape" && panelOpen()) close(); });
  D.onLiveUpdate(function () {
    if (!panelOpen()) return;
    fetchLiveContext();
    scheduleFallback();
  });

  function onViewChange(view) { if (view !== contextView() && panelOpen()) close(); }

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

  window.flotillaWorkContext = {
    open: open, update: update, close: close, refresh: fetchLiveContext, onViewChange: onViewChange,
  };
})();
