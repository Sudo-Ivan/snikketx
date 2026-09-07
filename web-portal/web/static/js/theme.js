/* Theme toggle, copy buttons, and password strength for the portal. */
(function () {
	"use strict";

	var STORAGE_KEY = "snikketx-theme";
	var COOKIE_NAME = "theme";
	var COOKIE_MAX_AGE = 31536000;
	var MIN_LEN = 10;

	function readStored() {
		try {
			var stored = window.localStorage.getItem(STORAGE_KEY);
			if (stored === "light" || stored === "dark") {
				return stored;
			}
		} catch (err) {}
		return null;
	}

	function store(theme) {
		try {
			window.localStorage.setItem(STORAGE_KEY, theme);
		} catch (err) {}
		document.cookie = COOKIE_NAME + "=" + theme + "; path=/; max-age=" +
			COOKIE_MAX_AGE + "; samesite=lax" +
			(window.location.protocol === "https:" ? "; secure" : "");
	}

	function current() {
		return document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light";
	}

	function apply(theme) {
		document.documentElement.setAttribute("data-theme", theme);
		var meta = document.querySelector('meta[name="theme-color"]:not([media])');
		if (!meta) {
			meta = document.querySelector('meta[name="theme-color"]');
		}
		if (meta) {
			meta.setAttribute("content", theme === "dark" ? "#09090b" : "#fafafa");
		}
	}

	function preferred() {
		var stored = readStored();
		if (stored) {
			return stored;
		}
		if (window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches) {
			return "dark";
		}
		return null;
	}

	function initTheme() {
		var wanted = preferred();
		if (wanted && wanted !== current()) {
			apply(wanted);
			store(wanted);
		}

		var toggles = document.querySelectorAll("[data-theme-toggle]");
		for (var i = 0; i < toggles.length; i++) {
			toggles[i].addEventListener("click", function () {
				var next = current() === "dark" ? "light" : "dark";
				apply(next);
				store(next);
			});
		}
	}

	function initCopyButtons() {
		var buttons = document.querySelectorAll("[data-copy-for]");
		for (var i = 0; i < buttons.length; i++) {
			buttons[i].addEventListener("click", function () {
				var button = this;
				var source = document.getElementById(button.getAttribute("data-copy-for"));
				if (!source) {
					return;
				}

				source.focus();
				source.select();

				var done = function () {
					var label = button.getAttribute("aria-label");
					button.setAttribute("aria-label", "Copied");
					window.setTimeout(function () {
						button.setAttribute("aria-label", label);
					}, 1500);
				};

				if (navigator.clipboard && navigator.clipboard.writeText) {
					navigator.clipboard.writeText(source.value).then(done, function () {});
					return;
				}
				try {
					document.execCommand("copy");
					done();
				} catch (err) {}
			});
		}
	}

	function scorePassword(value) {
		var len = value.length;
		if (len === 0) {
			return { score: 0, label: "", empty: true };
		}
		if (len < MIN_LEN) {
			return {
				score: 0,
				label: (MIN_LEN - len) + " more character" + (MIN_LEN - len === 1 ? "" : "s") + " needed",
				empty: false
			};
		}

		var classes = 0;
		if (/[a-z]/.test(value)) {
			classes++;
		}
		if (/[A-Z]/.test(value)) {
			classes++;
		}
		if (/[0-9]/.test(value)) {
			classes++;
		}
		if (/[^A-Za-z0-9]/.test(value)) {
			classes++;
		}

		var score = 1;
		if (classes >= 3) {
			score++;
		}
		if (classes >= 4 || len >= 14) {
			score++;
		}
		if (len >= 18 && classes >= 3) {
			score++;
		}

		var labels = ["", "Weak", "Fair", "Good", "Strong"];
		return { score: score, label: labels[score], empty: false };
	}

	function bindMeter(input) {
		var wrap = document.createElement("div");
		wrap.className = "pw-meter";
		wrap.hidden = true;

		var track = document.createElement("div");
		track.className = "pw-meter-track";
		track.setAttribute("aria-hidden", "true");
		for (var i = 0; i < 4; i++) {
			track.appendChild(document.createElement("span"));
		}

		var label = document.createElement("p");
		label.className = "pw-meter-label";
		label.setAttribute("role", "status");
		label.setAttribute("aria-live", "polite");

		wrap.appendChild(track);
		wrap.appendChild(label);
		input.insertAdjacentElement("afterend", wrap);

		var segments = track.children;

		function render() {
			var result = scorePassword(input.value);
			if (result.empty) {
				wrap.hidden = true;
				wrap.removeAttribute("data-score");
				label.textContent = "";
				return;
			}

			wrap.hidden = false;
			wrap.setAttribute("data-score", String(result.score));
			label.textContent = result.label;

			for (var i = 0; i < segments.length; i++) {
				if (i < result.score) {
					segments[i].className = "is-on";
				} else {
					segments[i].className = "";
				}
			}
		}

		input.addEventListener("input", render);
		input.addEventListener("focus", render);
		render();
	}

	function initPasswordMeters() {
		var inputs = document.querySelectorAll("input[data-pw-meter]");
		for (var i = 0; i < inputs.length; i++) {
			bindMeter(inputs[i]);
		}
	}

	function ready(fn) {
		if (document.readyState === "loading") {
			document.addEventListener("DOMContentLoaded", fn);
			return;
		}
		fn();
	}

	ready(function () {
		initTheme();
		initCopyButtons();
		initPasswordMeters();
		initToasts();
	});

	function initToasts() {
		var toasts = document.querySelectorAll("[data-toast]");
		for (var i = 0; i < toasts.length; i++) {
			bindToast(toasts[i]);
		}
	}

	function bindToast(toast) {
		var dismiss = toast.querySelector("[data-toast-dismiss]");
		if (dismiss) {
			dismiss.addEventListener("click", function () {
				hideToast(toast);
			});
		}
		var category = toast.className || "";
		var ttl = category.indexOf("toast-alert") >= 0 || category.indexOf("toast-danger") >= 0 ? 12000 : 6000;
		window.setTimeout(function () {
			hideToast(toast);
		}, ttl);
	}

	function hideToast(toast) {
		if (!toast || toast.getAttribute("data-leaving") === "1") {
			return;
		}
		toast.setAttribute("data-leaving", "1");
		toast.classList.add("is-leaving");
		window.setTimeout(function () {
			if (toast.parentNode) {
				toast.parentNode.removeChild(toast);
			}
		}, 220);
	}
})();
