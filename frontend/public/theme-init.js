// Roadmap Phase 56: runs in <head>, before anything is painted, so the page never flashes the
// wrong theme. Reads the saved choice (cookie vinaire_theme = auto | dark | light) and falls back
// to the system preference. An external file, not an inline script: the CSP allows 'self' scripts.
(function () {
  try {
    var m = document.cookie.match(/(?:^|; )vinaire_theme=(auto|dark|light)/);
    var mode = m ? m[1] : "auto";
    var dark = mode === "dark" || (mode === "auto" && !(window.matchMedia && window.matchMedia("(prefers-color-scheme: light)").matches));
    document.documentElement.setAttribute("data-theme", dark ? "dark" : "light");
  } catch {
    document.documentElement.setAttribute("data-theme", "dark");
  }
})();
