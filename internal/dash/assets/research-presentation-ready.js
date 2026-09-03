(function () {
  "use strict";

  var expectedParentOrigin = "";
  try { expectedParentOrigin = new URL(document.referrer).origin; } catch (_) {}
  // Bind receiver-sensitive platform methods once so the cooperative probe
  // uses one stable response path throughout its short lifetime.
  var stopEvent = Function.prototype.call.bind(Event.prototype.stopImmediatePropagation);
  var postReply = Function.prototype.call.bind(MessagePort.prototype.postMessage);
  var closeReply = Function.prototype.call.bind(MessagePort.prototype.close);
  var some = Function.prototype.call.bind(Array.prototype.some);
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
	var paints = some(range.getClientRects(), function (rect) {
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
	return some(document.querySelectorAll("main, section, article"), function (root) {
	  if (!elementPaints(root)) return false;
	  var rect = root.getBoundingClientRect();
	  return rect.width > 0 && rect.height > 0 && visibleCopyLength(root) >= 8;
	});
  }

  addEventListener("message", function (event) {
	if (event.source !== parent || !expectedParentOrigin || event.origin !== expectedParentOrigin ||
		!event.data || event.data.type !== "flotilla-presentation-probe" || event.ports.length !== 1) return;
	// The injected bridge owns the viewer's one-use response port.
	stopEvent(event);
	var reply = event.ports[0];
	requestFrame(function () {
	  requestFrame(function () {
		postReply(reply, { type: "flotilla-presentation-ready", ready: presentationReady() });
		closeReply(reply);
	  });
	});
  }, true);
}());
