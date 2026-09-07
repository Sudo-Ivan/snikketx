/* Renders the QR code of an invitation link. Loaded only by invitation pages,
   after the vendored qrcode library. */
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
			new QRCode(target, {
				text: target.dataset.qrdata,
				width: 160,
				height: 160,
				correctLevel: QRCode.CorrectLevel.M
			});
		}
		bindDownloads();
	}

	function bindDownloads() {
		var buttons = document.querySelectorAll("[data-qr-download-btn]");
		for (var i = 0; i < buttons.length; i++) {
			buttons[i].addEventListener("click", function () {
				var panel = this.closest(".invite-qr-panel") || document;
				var host = panel.querySelector("[data-qr-download]");
				if (!host) {
					return;
				}
				var canvas = host.querySelector("canvas");
				var img = host.querySelector("img");
				var href = "";
				if (canvas && canvas.toDataURL) {
					href = canvas.toDataURL("image/png");
				} else if (img && img.src) {
					href = img.src;
				}
				if (!href) {
					return;
				}
				var link = document.createElement("a");
				link.href = href;
				link.download = "snikketx-invite-qr.png";
				document.body.appendChild(link);
				link.click();
				document.body.removeChild(link);
			});
		}
	}

	if (document.readyState === "loading") {
		document.addEventListener("DOMContentLoaded", render);
		return;
	}
	render();
})();
