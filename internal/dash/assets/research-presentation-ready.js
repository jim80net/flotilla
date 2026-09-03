(function () {
  "use strict";

  var expectedParentOrigin = "";
  try { expectedParentOrigin = new URL(document.referrer).origin; } catch (_) {}
  var stopImmediatePropagation = Event.prototype.stopImmediatePropagation;
  var postReply = MessagePort.prototype.postMessage;
  var closeReply = MessagePort.prototype.close;
  var requestFrame = window.requestAnimationFrame.bind(window);

  function elementPaints(element) {
	for (var current = element; current; current = current.parentElement) {
	  var style = getComputedStyle(current);
	  if (style.display === "none" || style.visibility === "hidden" || style.visibility === "collapse" ||
		  style.contentVisibility === "hidden" || Number(style.opacity) === 0) return false;
	}
	return true;
  }

  function textPaints(node, boundary) {
	if (!node.nodeValue || !node.nodeValue.replace(/\s+/g, " ").trim()) return false;
	if (!node.parentElement || !elementPaints(node.parentElement)) return false;
	var range = document.createRange();
	range.selectNodeContents(node);
	var paints = Array.prototype.some.call(range.getClientRects(), function (rect) {
	  return rect.width > 0 && rect.height > 0;
	});
	range.detach();
	return paints && boundary.contains(node);
  }

  function visibleCopyLength(root) {
	var walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
	var copy = "";
	while (walker.nextNode()) {
	  if (textPaints(walker.currentNode, root)) copy += " " + walker.currentNode.nodeValue;
	}
	return copy.replace(/\s+/g, " ").trim().length;
  }

  function presentationReady() {
	return Array.prototype.some.call(document.querySelectorAll("main, section, article"), function (root) {
	  if (!elementPaints(root)) return false;
	  var rect = root.getBoundingClientRect();
	  return rect.width > 0 && rect.height > 0 && visibleCopyLength(root) >= 8;
	});
  }

  addEventListener("message", function (event) {
	if (event.source !== parent || !expectedParentOrigin || event.origin !== expectedParentOrigin ||
		!event.data || event.data.type !== "flotilla-presentation-probe" || event.ports.length !== 1) return;
	// This script is injected before package code. Stop same-window listeners
	// before exposing the capability-bearing port to presentation-authored JS.
	stopImmediatePropagation.call(event);
	var reply = event.ports[0];
	requestFrame(function () {
	  requestFrame(function () {
		postReply.call(reply, { type: "flotilla-presentation-ready", ready: presentationReady() });
		closeReply.call(reply);
	  });
	});
  }, true);
}());
