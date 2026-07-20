(function () {
  "use strict";

  var storageKey = "flotilla-theme-v1";
  var media = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;

  function storedTheme() {
    try {
      var value = window.localStorage.getItem(storageKey);
      return value === "light" || value === "dark" ? value : "";
    } catch (_) {
      return "";
    }
  }

  function systemTheme() {
    return media && media.matches ? "dark" : "light";
  }

  function apply(theme) {
    document.documentElement.dataset.theme = theme;
    document.documentElement.style.colorScheme = theme;
    document.querySelectorAll("[data-theme-toggle]").forEach(function (button) {
      var dark = theme === "dark";
      button.setAttribute("aria-pressed", dark ? "true" : "false");
      button.setAttribute("aria-label", dark ? "Dark theme active. Switch to light theme" : "Light theme active. Switch to dark theme");
      button.setAttribute("title", dark ? "Switch to light theme" : "Switch to dark theme");
      var label = button.querySelector("[data-theme-label]");
      if (label) label.textContent = dark ? "Dark" : "Light";
    });
  }

  function choose(theme) {
    try {
      window.localStorage.setItem(storageKey, theme);
    } catch (_) {}
    apply(theme);
  }

  // This script is deliberately loaded before the stylesheet. Resolving the
  // attribute synchronously keeps first paint in the operator's chosen theme.
  apply(storedTheme() || systemTheme());

  document.addEventListener("DOMContentLoaded", function () {
    apply(storedTheme() || systemTheme());
    document.querySelectorAll("[data-theme-toggle]").forEach(function (button) {
      button.addEventListener("click", function () {
        choose(document.documentElement.dataset.theme === "dark" ? "light" : "dark");
      });
    });
  });

  if (media) {
    var followSystem = function () {
      if (!storedTheme()) apply(systemTheme());
    };
    if (media.addEventListener) media.addEventListener("change", followSystem);
    else if (media.addListener) media.addListener(followSystem);
  }
})();
