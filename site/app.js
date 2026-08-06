/* flotilla landing — copy-to-clipboard controls only. */
(function () {
  "use strict";

  document.querySelectorAll(".copy-btn").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var target = document.querySelector(btn.dataset.copy);
      if (!target) return;
      var text = target.textContent.trim();
      var done = function () {
        var label = btn.querySelector(".copy-label");
        var prev = label.textContent;
        label.textContent = "copied";
        btn.classList.add("copied");
        setTimeout(function () {
          label.textContent = prev;
          btn.classList.remove("copied");
        }, 1400);
      };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(done).catch(fallbackCopy);
      } else {
        fallbackCopy();
      }
      function fallbackCopy() {
        var ta = document.createElement("textarea");
        ta.value = text;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        try { document.execCommand("copy"); done(); } catch (e) { /* no-op */ }
        document.body.removeChild(ta);
      }
    });
  });
})();
