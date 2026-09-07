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
		initSidebar();
		initAdminSearch();
		initFilePickers();
		initBusyForms();
		initUpdaterPoll();
	});

	function initFilePickers() {
		var pickers = document.querySelectorAll("[data-file-picker]");
		for (var i = 0; i < pickers.length; i++) {
			bindFilePicker(pickers[i]);
		}
	}

	function bindFilePicker(root) {
		var input = root.querySelector("[data-file-input]");
		var name = root.querySelector("[data-file-name]");
		if (!input || !name) {
			return;
		}
		var empty = name.getAttribute("data-empty") || "No file chosen";

		function render() {
			if (input.files && input.files.length > 0) {
				name.textContent = input.files.length === 1 ? input.files[0].name : input.files.length + " files";
				root.classList.add("has-file");
				return;
			}
			name.textContent = empty;
			root.classList.remove("has-file");
		}

		input.addEventListener("change", render);
		render();
	}

	function initBusyForms() {
		var forms = document.querySelectorAll("[data-busy-form]");
		for (var i = 0; i < forms.length; i++) {
			forms[i].addEventListener("submit", function () {
				this.classList.add("is-busy");
				var buttons = this.querySelectorAll("button[type='submit']");
				for (var j = 0; j < buttons.length; j++) {
					buttons[j].disabled = true;
				}
			});
		}
	}

	function initUpdaterPoll() {
		if (!document.querySelector("[data-updater-poll]")) {
			return;
		}
		window.setTimeout(function () {
			window.location.reload();
		}, 4000);
	}

	function initSidebar() {
		var shell = document.querySelector("[data-admin-shell]");
		if (!shell) {
			return;
		}
		var sidebar = shell.querySelector("[data-sidebar]");
		var backdrop = shell.querySelector("[data-sidebar-backdrop]");
		var toggles = shell.querySelectorAll("[data-sidebar-toggle]");
		var closes = shell.querySelectorAll("[data-sidebar-close]");
		var storageKey = "snikketx-sidebar-collapsed";
		var mq = window.matchMedia ? window.matchMedia("(max-width: 900px)") : null;

		function isMobile() {
			return mq ? mq.matches : window.innerWidth <= 900;
		}

		function setExpanded(expanded) {
			for (var i = 0; i < toggles.length; i++) {
				toggles[i].setAttribute("aria-expanded", expanded ? "true" : "false");
			}
		}

		function closeMobile() {
			if (!sidebar) {
				return;
			}
			sidebar.classList.remove("is-open");
			if (backdrop) {
				backdrop.classList.remove("is-open");
				backdrop.hidden = true;
			}
			document.documentElement.style.overflow = "";
			setExpanded(false);
		}

		function openMobile() {
			if (!sidebar) {
				return;
			}
			sidebar.classList.add("is-open");
			if (backdrop) {
				backdrop.hidden = false;
				backdrop.classList.add("is-open");
			}
			document.documentElement.style.overflow = "hidden";
			setExpanded(true);
		}

		function applyDesktopCollapsed(collapsed) {
			if (collapsed) {
				shell.classList.add("is-sidebar-collapsed");
			} else {
				shell.classList.remove("is-sidebar-collapsed");
			}
			try {
				window.localStorage.setItem(storageKey, collapsed ? "1" : "0");
			} catch (err) {}
			setExpanded(!collapsed);
		}

		function readDesktopCollapsed() {
			try {
				return window.localStorage.getItem(storageKey) === "1";
			} catch (err) {
				return false;
			}
		}

		function syncMode() {
			if (isMobile()) {
				shell.classList.remove("is-sidebar-collapsed");
				closeMobile();
				return;
			}
			closeMobile();
			applyDesktopCollapsed(readDesktopCollapsed());
		}

		for (var i = 0; i < toggles.length; i++) {
			toggles[i].addEventListener("click", function () {
				if (isMobile()) {
					if (sidebar && sidebar.classList.contains("is-open")) {
						closeMobile();
					} else {
						openMobile();
					}
					return;
				}
				applyDesktopCollapsed(!shell.classList.contains("is-sidebar-collapsed"));
			});
		}

		for (var j = 0; j < closes.length; j++) {
			closes[j].addEventListener("click", function (event) {
				event.preventDefault();
				closeMobile();
			});
		}

		if (backdrop) {
			backdrop.addEventListener("click", closeMobile);
		}

		document.addEventListener("keydown", function (event) {
			if (event.key === "Escape") {
				closeMobile();
			}
		});

		if (mq && mq.addEventListener) {
			mq.addEventListener("change", syncMode);
		} else if (mq && mq.addListener) {
			mq.addListener(syncMode);
		}

		syncMode();
	}

	function initAdminSearch() {
		var root = document.querySelector("[data-admin-search]");
		if (!root) {
			return;
		}
		var input = root.querySelector("[data-admin-search-input]");
		var results = root.querySelector("[data-admin-search-results]");
		var kbd = root.querySelector("[data-admin-search-kbd]");
		if (!input || !results) {
			return;
		}
		var items = [
			{ title: "Home", href: "/admin/", group: "Manage", keywords: "dashboard overview slo" },
			{ title: "Users", href: "/admin/users", group: "Manage", keywords: "accounts localpart disable admin" },
			{ title: "Circles", href: "/admin/circles", group: "Manage", keywords: "groups contacts roster" },
			{ title: "Group chats", href: "/admin/mucs", group: "Manage", keywords: "muc rooms chat" },
			{ title: "Invitations", href: "/admin/invitations", group: "Manage", keywords: "invite link create" },
			{ title: "Create invitation", href: "/admin/invitation/-/new", group: "Manage", keywords: "invite new user" },
			{ title: "Devices", href: "/admin/devices", group: "Operations", keywords: "clients push sessions revoke" },
			{ title: "Storage", href: "/admin/storage", group: "Operations", keywords: "uploads http files quota" },
			{ title: "Archives", href: "/admin/archives", group: "Operations", keywords: "mam message history" },
			{ title: "Certs and DNS", href: "/admin/certs/", group: "Operations", keywords: "tls acme srv certificates dns" },
			{ title: "Rate limits", href: "/admin/limits/", group: "Operations", keywords: "lockout login throttle" },
			{ title: "Updates", href: "/admin/updates", group: "Operations", keywords: "cosign signatures pin digests apply" },
			{ title: "Apps", href: "/admin/apps", group: "Operations", keywords: "android apk download" },
			{ title: "Backup", href: "/admin/backup/", group: "Operations", keywords: "restic restore retention schedule disaster" },
			{ title: "Logs", href: "/admin/logs/", group: "System", keywords: "prosody portal docker container ravenguard" },
			{ title: "System", href: "/admin/system/", group: "System", keywords: "announce announcement metrics version" },
			{ title: "Health", href: "/admin/health/", group: "System", keywords: "probes errors status" },
			{ title: "Audit log", href: "/admin/audit/", group: "System", keywords: "events security trail" }
		];
		var active = -1;
		var matches = [];

		if (kbd) {
			var isMac = /Mac|iPhone|iPad/.test(navigator.platform || "");
			kbd.textContent = isMac ? "⌘K" : "Ctrl K";
		}

		function normalize(value) {
			return String(value || "").toLowerCase();
		}

		function scoreItem(item, query) {
			var hay = normalize(item.title + " " + item.group + " " + item.keywords + " " + item.href);
			if (!query) {
				return 1;
			}
			if (normalize(item.title).indexOf(query) === 0) {
				return 100;
			}
			if (hay.indexOf(query) >= 0) {
				return 50;
			}
			var parts = query.split(/\s+/);
			for (var i = 0; i < parts.length; i++) {
				if (parts[i] && hay.indexOf(parts[i]) < 0) {
					return 0;
				}
			}
			return parts.length ? 25 : 0;
		}

		function closeResults() {
			results.hidden = true;
			results.innerHTML = "";
			input.setAttribute("aria-expanded", "false");
			active = -1;
			matches = [];
		}

		function render() {
			var query = normalize(input.value).trim();
			matches = [];
			for (var i = 0; i < items.length; i++) {
				var scored = scoreItem(items[i], query);
				if (scored > 0) {
					matches.push({ item: items[i], score: scored });
				}
			}
			matches.sort(function (a, b) {
				if (b.score !== a.score) {
					return b.score - a.score;
				}
				return a.item.title.localeCompare(b.item.title);
			});
			if (!query) {
				matches = matches.slice(0, 8);
			} else {
				matches = matches.slice(0, 12);
			}
			results.innerHTML = "";
			if (!matches.length) {
				var empty = document.createElement("li");
				empty.className = "admin-search-empty";
				empty.textContent = "No matching pages or settings.";
				results.appendChild(empty);
				results.hidden = false;
				input.setAttribute("aria-expanded", "true");
				active = -1;
				return;
			}
			for (var j = 0; j < matches.length; j++) {
				var li = document.createElement("li");
				li.setAttribute("role", "presentation");
				var link = document.createElement("a");
				link.className = "admin-search-option";
				link.href = matches[j].item.href;
				link.setAttribute("role", "option");
				link.setAttribute("id", "admin-search-option-" + j);
				link.innerHTML = "<span class=\"admin-search-option-title\"></span><span class=\"admin-search-option-meta\"></span>";
				link.querySelector(".admin-search-option-title").textContent = matches[j].item.title;
				link.querySelector(".admin-search-option-meta").textContent = matches[j].item.group + " · " + matches[j].item.href;
				li.appendChild(link);
				results.appendChild(li);
			}
			results.hidden = false;
			input.setAttribute("aria-expanded", "true");
			active = 0;
			syncActive();
		}

		function syncActive() {
			var options = results.querySelectorAll(".admin-search-option");
			for (var i = 0; i < options.length; i++) {
				if (i === active) {
					options[i].classList.add("is-active");
					input.setAttribute("aria-activedescendant", options[i].id);
				} else {
					options[i].classList.remove("is-active");
				}
			}
		}

		function openSelected() {
			var options = results.querySelectorAll(".admin-search-option");
			if (active >= 0 && options[active]) {
				window.location.href = options[active].getAttribute("href");
			}
		}

		input.addEventListener("focus", function () {
			render();
		});
		input.addEventListener("input", function () {
			render();
		});
		input.addEventListener("keydown", function (event) {
			if (event.key === "ArrowDown") {
				event.preventDefault();
				if (!matches.length) {
					render();
				}
				if (matches.length) {
					active = (active + 1) % matches.length;
					syncActive();
				}
				return;
			}
			if (event.key === "ArrowUp") {
				event.preventDefault();
				if (matches.length) {
					active = (active - 1 + matches.length) % matches.length;
					syncActive();
				}
				return;
			}
			if (event.key === "Enter") {
				if (!results.hidden && matches.length) {
					event.preventDefault();
					openSelected();
				}
				return;
			}
			if (event.key === "Escape") {
				event.preventDefault();
				closeResults();
				input.blur();
			}
		});

		document.addEventListener("keydown", function (event) {
			if ((event.ctrlKey || event.metaKey) && !event.altKey && String(event.key).toLowerCase() === "k") {
				event.preventDefault();
				input.focus();
				input.select();
				render();
			}
		});

		document.addEventListener("click", function (event) {
			if (!root.contains(event.target)) {
				closeResults();
			}
		});
	}

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
