try {
  const t = localStorage.getItem("beepgrid:workspace:theme");
  document.documentElement.dataset.theme =
    t === "light" || t === "dark"
      ? t
      : matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light";
} catch {
  document.documentElement.dataset.theme = "dark";
}
