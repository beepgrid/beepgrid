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
// Typography preference
try {
  const id = localStorage.getItem("beepgrid:workspace:type-preset:v1");
  document.documentElement.dataset.typography = ["inter-reference","inter-strong","system-reference","geist-reference","source-readable","inter-roomy","dm-balanced","plex-clear","manrope-soft","jakarta-modern"].includes(id) ? id : "inter-reference";
} catch {
  document.documentElement.dataset.typography = "inter-reference";
}
// Agent icon preference
try {
  const id = localStorage.getItem("beepgrid:workspace:agent-icons:v1");
  document.documentElement.dataset.agentIcons = ["open-tabs","lucide-tint","phosphor-duotone","geometric-flat","dimensional"].includes(id) ? id : "open-tabs";
} catch {
  document.documentElement.dataset.agentIcons = "open-tabs";
}
