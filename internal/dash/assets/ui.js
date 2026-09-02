(function () {
  "use strict";
  function escapeHtml(value) {
    return String(value == null ? "" : value)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
  }
  function renderInlineMarkdown(value) {
    var links = [];
    var text = String(value == null ? "" : value)
      .replace(/^\s{0,3}#{1,6}\s+/gm, "")
      .replace(/\[([^\]]+)\]\(([^)]+)\)/g, function (_, label, href) {
        href = String(href || "").trim();
        if (!/^(?:https?:\/\/|\/|#)[^\s]+$/i.test(href)) return label;
        var token = "\u0000FLOTILLA_LINK_" + links.length + "\u0000";
        links.push('<a href="' + escapeHtml(href) + '"' + (/^https?:\/\//i.test(href) ? ' target="_blank" rel="noopener noreferrer"' : "") + '>' + escapeHtml(label) + "</a>");
        return token;
      });
    var html = escapeHtml(text)
      .replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>")
      .replace(/__([^_\n]+)__/g, "<strong>$1</strong>")
      .replace(/\*([^*\n]+)\*/g, "<em>$1</em>")
      .replace(/`([^`\n]+)`/g, "<code>$1</code>")
      .replace(/\r?\n/g, "<br>");
    links.forEach(function (link, i) { html = html.replace("\u0000FLOTILLA_LINK_" + i + "\u0000", link); });
    return html;
  }
  function failurePanel(message, retryLabel, detail, retryAttr) {
    var attr = retryAttr ? " " + retryAttr : "";
    return '<div class="error recoverable-error" role="alert"><p>' + escapeHtml(message) + '</p>' +
      '<button type="button" class="btn"' + attr + '>' + escapeHtml(retryLabel || "Try again") + '</button>' +
      '<details><summary>Technical details</summary><code>' + escapeHtml(detail || "No additional detail was provided.") + "</code></details></div>";
  }
  window.flotillaUI = { escapeHtml: escapeHtml, renderInlineMarkdown: renderInlineMarkdown, failurePanel: failurePanel };
})();
