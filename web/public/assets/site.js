(function () {
  "use strict";

  var storageKey = "mywebsite-theme";

  function isTheme(value) {
    return value === "light" || value === "dark";
  }

  function systemTheme() {
    return typeof window.matchMedia === "function" && window.matchMedia("(prefers-color-scheme: dark)").matches
      ? "dark"
      : "light";
  }

  function storedTheme() {
    try {
      var value = window.localStorage.getItem(storageKey);
      return isTheme(value) ? value : null;
    } catch (_error) {
      return null;
    }
  }

  function currentTheme() {
    var explicitTheme = document.documentElement.getAttribute("data-theme");
    return isTheme(explicitTheme) ? explicitTheme : systemTheme();
  }

  function applyTheme(theme) {
    document.documentElement.setAttribute("data-theme", theme);
  }

  var savedTheme = storedTheme();
  if (savedTheme) {
    applyTheme(savedTheme);
  }

  function initThemeToggle() {
    var button = document.querySelector("[data-theme-toggle]");
    if (!button) return;

    function syncButton() {
      var theme = currentTheme();
      var nextTheme = theme === "dark" ? "light" : "dark";
      var themeLabel = theme === "dark" ? "深色" : "浅色";
      var nextLabel = nextTheme === "dark" ? "深色" : "浅色";
      button.hidden = false;
      button.textContent = "切换到" + nextLabel;
      button.setAttribute("aria-label", "当前为" + themeLabel + "主题，点击切换到" + nextLabel + "主题");
      button.setAttribute("aria-pressed", String(theme === "dark"));
    }

    syncButton();
    button.addEventListener("click", function () {
      var nextTheme = currentTheme() === "dark" ? "light" : "dark";
      applyTheme(nextTheme);
      try {
        window.localStorage.setItem(storageKey, nextTheme);
      } catch (_error) {
        // Private browsing or a blocked storage area should not disable the toggle.
      }
      syncButton();
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initThemeToggle, { once: true });
  } else {
    initThemeToggle();
  }
})();
