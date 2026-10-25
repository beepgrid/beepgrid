// Local diagnostics only: no network reporting or retained user content.
let navigationStarted = performance.now();
let navigationPath = location.pathname;
let recorded = false;
export function beginNavigation(path: string) {
  navigationStarted = performance.now();
  navigationPath = path;
  recorded = false;
}
export function recordNavigationPaint() {
  if (recorded || location.pathname !== navigationPath) return;
  recorded = true;
  requestAnimationFrame(() => {
    const main = document.querySelector("main");
    if (main && location.pathname === navigationPath) {
      main.dataset.navigationMs = (
        performance.now() - navigationStarted
      ).toFixed(1);
      main.dataset.navigationPath = navigationPath;
    }
  });
}
