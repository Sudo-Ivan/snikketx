/* Renders the QR code of an invitation link. Loaded only by the invitation
   landing page, after the vendored qrcode library. */
(function () {
	"use strict";

	function render() {
		if (typeof QRCode === "undefined") {
			return;
		}
		var targets = document.querySelectorAll("[data-qrdata]");
		for (var i = 0; i < targets.length; i++) {
			var target = targets[i];
			if (target.getAttribute("data-qr-rendered") === "1") {
				continue;
			}
			target.setAttribute("data-qr-rendered", "1");
			new QRCode(target, target.dataset.qrdata);
		}
	}

	if (document.readyState === "loading") {
		document.addEventListener("DOMContentLoaded", render);
		return;
	}
	render();
})();
