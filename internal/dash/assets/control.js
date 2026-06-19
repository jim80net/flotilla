/* flotilla dash — cnc control (vanilla JS, no build).
 *
 * Three actions, each a thin POST to the control library: post an operator note
 * (live), route an instruction, resume a crashed desk. Reuses dash.js's shared
 * postJSON (anti-CSRF X-Flotilla-Dash header + typed-error surfacing). Route +
 * resume drive panes and are GATED on the cross-process pane lock (the server
 * returns 503 with a clear message until it lands); the UI surfaces that honestly
 * rather than pretending the action happened.
 */
(function () {
  "use strict";

  var D = window.flotillaDash;
  var el = D.el, postJSON = D.postJSON;

  // wire(formId, msgId, button-busy, submit-fn) binds a control form: prevent
  // default, disable while in-flight, show a typed result/error honestly.
  function wire(formId, msgId, submit) {
    var form = el(formId), msg = el(msgId);
    function setMsg(text, kind) { msg.className = "form-msg" + (kind ? " " + kind : ""); msg.textContent = text; }
    var btn = form.querySelector("button");
    form.addEventListener("submit", function (ev) {
      ev.preventDefault();
      setMsg("Working…", "");
      btn.disabled = true;
      submit().then(function (okText) {
        setMsg(okText, "ok");
      }).catch(function (err) {
        setMsg(err.message, "err");
      }).then(function () {
        btn.disabled = false;
      });
    });
  }

  // Operator note (live — no pane driven).
  wire("notify-form", "notify-msg", function () {
    var body = el("notify-body").value.trim();
    if (!body) return Promise.reject(new Error("note is required"));
    return postJSON("/api/control/notify", { message: body }).then(function () {
      el("notify-body").value = "";
      return "Note posted to the fleet channel.";
    });
  });

  // Route an instruction (gated on the pane lock → reports 503 honestly).
  wire("route-form", "route-msg", function () {
    var target = el("route-target").value.trim();
    var body = el("route-body").value.trim();
    if (!target) return Promise.reject(new Error("target is required"));
    if (!body) return Promise.reject(new Error("instruction is required"));
    return postJSON("/api/control/route", { target: target, message: body }).then(function (res) {
      // The typed outcome (delivered/busy/crashed/transient/unconfirmed) is the
      // truth — surface it distinctly, never a bare "ok" and never assume success
      // on a missing outcome.
      var outcome = (res && res.outcome) || "(no outcome reported)";
      var detail = res && res.detail ? " — " + res.detail : "";
      if (outcome === "delivered") { el("route-body").value = ""; }
      return "Outcome: " + outcome + detail;
    });
  });

  // Resume a crashed desk (destructive-ish: restarts a process; confirm) — gated.
  wire("resume-form", "resume-msg", function () {
    var agent = el("resume-agent").value.trim();
    if (!agent) return Promise.reject(new Error("agent is required"));
    if (!window.confirm("Resume desk '" + agent + "'? This restarts its agent process from the launch recipe.")) {
      return Promise.reject(new Error("cancelled"));
    }
    return postJSON("/api/control/resume", { agent: agent }).then(function (res) {
      var outcome = (res && res.outcome) || "(no outcome reported)";
      var detail = res && res.detail ? " — " + res.detail : "";
      return "Outcome: " + outcome + detail;
    });
  });
})();
