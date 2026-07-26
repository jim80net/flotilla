// Reference navigation contract for self-contained R&D showpieces.
//
// Copy this file into the presentation package. The package remains local and
// does not depend on a CDN or on the dash's installed static assets.
const deck =
  document.querySelector("[data-showpiece-deck]") ||
  document.querySelector("main");
const slides = [...document.querySelectorAll(".slide")];
const progress = document.querySelector("[data-progress]");
const currentLabel = document.querySelector("[data-current]");
const totalLabel = document.querySelector("[data-total]");
const previous = document.querySelector("[data-prev]");
const next = document.querySelector("[data-next]");
const motionQuery = window.matchMedia("(prefers-reduced-motion: reduce)");

let current = 0;
let scrollFrame = 0;

function clamp(index) {
  return Math.max(0, Math.min(slides.length - 1, index));
}

function setCurrent(index) {
  current = clamp(index);
  if (currentLabel) currentLabel.textContent = String(current + 1).padStart(2, "0");
  if (totalLabel) totalLabel.textContent = String(slides.length).padStart(2, "0");
  if (progress) progress.style.width = `${((current + 1) / slides.length) * 100}%`;
  if (previous) previous.disabled = current === 0;
  if (next) next.disabled = current === slides.length - 1;
  const label = slides[current]?.dataset.label || `Section ${current + 1}`;
  document.title = `${label} · ${document.documentElement.dataset.showpieceTitle || "Research"}`;
}

function nearestSlide() {
  const top = deck.scrollTop;
  let winner = 0;
  let distance = Number.POSITIVE_INFINITY;
  slides.forEach((slide, index) => {
    const candidate = Math.abs(slide.offsetTop - top);
    if (candidate < distance) {
      winner = index;
      distance = candidate;
    }
  });
  return winner;
}

function syncFromScroll() {
  if (scrollFrame) return;
  scrollFrame = requestAnimationFrame(() => {
    scrollFrame = 0;
    setCurrent(nearestSlide());
  });
}

function go(index) {
  const target = clamp(index);
  // Explicit navigation owns state immediately. It must not wait for an
  // IntersectionObserver threshold that a long mobile section may never meet.
  setCurrent(target);
  deck.scrollTo({
    top: slides[target].offsetTop,
    behavior: motionQuery.matches ? "auto" : "smooth",
  });
}

deck.addEventListener("scroll", syncFromScroll, { passive: true });
previous?.addEventListener("click", () => go(current - 1));
next?.addEventListener("click", () => go(current + 1));

document.addEventListener("keydown", (event) => {
  if (["ArrowDown", "ArrowRight", "PageDown", " "].includes(event.key)) {
    event.preventDefault();
    go(current + 1);
  } else if (["ArrowUp", "ArrowLeft", "PageUp"].includes(event.key)) {
    event.preventDefault();
    go(current - 1);
  } else if (event.key === "Home") {
    event.preventDefault();
    go(0);
  } else if (event.key === "End") {
    event.preventDefault();
    go(slides.length - 1);
  }
});

setCurrent(0);
document.body.dataset.showpieceNavigationReady = "true";
