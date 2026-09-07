/* Apply stored theme before first paint to avoid a light/dark flash. */
(function () {
	try {
		var t = localStorage.getItem("snikketx-theme");
		if (t !== "light" && t !== "dark") {
			t = window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
		}
		document.documentElement.setAttribute("data-theme", t);
	} catch (e) {}
})();
