/* Theme toggle and small page behaviours for the Snikket web portal.
   The server renders data-theme from the theme cookie, so this script only
   has to switch the attribute and remember the new choice. */
(function () {
	"use strict";

	var STORAGE_KEY = "snikket-theme";
	var COOKIE_NAME = "theme";
	var COOKIE_MAX_AGE = 31536000;

	function readStored() {
		try {
			var stored = window.localStorage.getItem(STORAGE_KEY);
			if (stored === "light" || stored === "dark") {
				return stored;
			}
		} catch (err) {
			/* Storage is unavailable in private mode on some browsers. */
		}
		return null;
	}

	function store(theme) {
		try {
			window.localStorage.setItem(STORAGE_KEY, theme);
		} catch (err) {
			/* Losing the preference is acceptable, the cookie still holds it. */
		}
		document.cookie = COOKIE_NAME + "=" + theme + "; path=/; max-age=" +
			COOKIE_MAX_AGE + "; samesite=lax" +
			(window.location.protocol === "https:" ? "; secure" : "");
	}

	function current() {
		return document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light";
	}

	function apply(theme) {
		document.documentElement.setAttribute("data-theme", theme);
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
				} catch (err) {
					/* The value stays selected so it can be copied by hand. */
				}
			});
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
	});
})();
