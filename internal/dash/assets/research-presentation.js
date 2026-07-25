/* Shared R&D showpiece navigation. The server injects this into presentation
   HTML, but it activates only for the explicit slide-controller contract below.
   Scrolling remains free: there is no snap or wheel prevention. */
(function () {
  "use strict";

  var deck = document.querySelector("main");
  var slides = Array.prototype.slice.call(document.querySelectorAll(".slide"));
  var currentLabel = document.querySelector("[data-current]");
  var totalLabel = document.querySelector("[data-total]");
  var previous = document.querySelector("[data-prev]");
  var next = document.querySelector("[data-next]");
  if (!deck || !slides.length || !currentLabel || !previous || !next) return;

  var progress = document.querySelector(".progress");
  var visibleLabel = document.querySelector("[data-visible-label]");
  var reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
  var firstLabel = slides[0].getAttribute("data-label") || "";
  var titleSuffix = firstLabel && document.title.indexOf(firstLabel) === 0 ? document.title.slice(firstLabel.length) : "";
  var current = 0;
  var navigationTarget = null;
  var navigationTimer = 0;
  var scrollFrame = 0;

  function clamp(index) {
    return Math.max(0, Math.min(slides.length - 1, index));
  }

  function setCurrent(index) {
    current = clamp(index);
    var slide = slides[current];
    var label = slide.getAttribute("data-label") || slide.getAttribute("aria-label") ||
      (slide.querySelector("h1, h2") && slide.querySelector("h1, h2").textContent.trim()) ||
      "Section " + (current + 1);
    currentLabel.textContent = String(current + 1).padStart(2, "0");
    if (totalLabel) totalLabel.textContent = String(slides.length).padStart(2, "0");
    if (visibleLabel) visibleLabel.textContent = label;
    if (progress) progress.style.width = ((current + 1) / slides.length * 100) + "%";
    previous.disabled = current === 0;
    next.disabled = current === slides.length - 1;
    document.title = label + titleSuffix;
  }

  function visibleIndex() {
    var top = deck.scrollTop;
    var bottom = top + deck.clientHeight;
    var bestIndex = 0;
    var bestOverlap = -1;
    slides.forEach(function (slide, index) {
      var slideTop = slide.offsetTop;
      var slideBottom = slideTop + slide.offsetHeight;
      var overlap = Math.max(0, Math.min(bottom, slideBottom) - Math.max(top, slideTop));
      if (overlap > bestOverlap || (overlap === bestOverlap && slideTop <= top + deck.clientHeight * 0.35)) {
        bestOverlap = overlap;
        bestIndex = index;
      }
    });
    return bestIndex;
  }

  function settleNavigation() {
    if (navigationTarget === null) return;
    var targetTop = slides[navigationTarget].offsetTop;
    if (Math.abs(deck.scrollTop - targetTop) <= 2 ||
        (navigationTarget === slides.length - 1 && Math.abs(deck.scrollTop + deck.clientHeight - deck.scrollHeight) <= 2)) {
      navigationTarget = null;
      window.clearTimeout(navigationTimer);
    }
  }

  function syncFromScroll() {
    scrollFrame = 0;
    settleNavigation();
    if (navigationTarget === null) setCurrent(visibleIndex());
  }

  function scheduleSync() {
    if (!scrollFrame) scrollFrame = window.requestAnimationFrame(syncFromScroll);
  }

  function cancelNavigation() {
    navigationTarget = null;
    window.clearTimeout(navigationTimer);
    scheduleSync();
  }

  function go(index) {
    var target = clamp(index);
    navigationTarget = target;
    setCurrent(target);
    window.clearTimeout(navigationTimer);
    deck.scrollTo({
      top: slides[target].offsetTop,
      behavior: reducedMotion.matches ? "auto" : "smooth"
    });
    navigationTimer = window.setTimeout(function () {
      navigationTarget = null;
      setCurrent(visibleIndex());
    }, 900);
  }

  document.addEventListener("click", function (event) {
    var control = event.target.closest("[data-prev], [data-next]");
    if (!control) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    go(current + (control.hasAttribute("data-next") ? 1 : -1));
  }, true);

  document.addEventListener("keydown", function (event) {
    var target = null;
    if (["ArrowDown", "ArrowRight", "PageDown", " "].indexOf(event.key) >= 0) target = current + 1;
    else if (["ArrowUp", "ArrowLeft", "PageUp"].indexOf(event.key) >= 0) target = current - 1;
    else if (event.key === "Home") target = 0;
    else if (event.key === "End") target = slides.length - 1;
    if (target === null) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    go(target);
  }, true);

  deck.addEventListener("scroll", scheduleSync, { passive: true });
  deck.addEventListener("wheel", cancelNavigation, { passive: true });
  deck.addEventListener("touchstart", cancelNavigation, { passive: true });
  window.addEventListener("resize", scheduleSync);
  setCurrent(visibleIndex());
  document.documentElement.dataset.flotillaPresentationController = "ready";
})();
