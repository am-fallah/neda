(function () {
  try {
    var stored = localStorage.getItem("neda-theme");
    var dark =
      stored === "dark" ||
      (stored !== "light" &&
        window.matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.classList.toggle("dark", dark);
  } catch (_e) {
    // Storage may be unavailable; the system preference still applies.
  }
})();
