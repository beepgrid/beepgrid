import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const presets = JSON.parse(
  await fs.readFile(path.join(root, "src/typography-presets.json"), "utf8"),
);
const icons = JSON.parse(
  await fs.readFile(path.join(root, "src/icon-presets.json"), "utf8"),
);
const fonts = JSON.parse(
  await fs.readFile(path.join(root, "public/fonts/manifest.json"), "utf8"),
);
const families = new Map(
  presets.filter((p) => p.font).map((p) => [p.font, p.family]),
);
const weightRanges = {
  inter: "100 900",
  geist: "100 900",
  "source-sans-3": "200 900",
  "dm-sans": "100 1000",
  "ibm-plex-sans": "100 700",
  manrope: "200 800",
  "plus-jakarta-sans": "200 800",
};
function tokens(p) {
  return `--app-font:${p.font ? '"' + p.family + '",' : ""}system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;--type-body:${p.body}px;--type-secondary:${p.body - 1}px;--type-small:${p.body - 2}px;--type-caption:${p.body - 2.5}px;--type-micro:${Math.max(12, p.body - 3)}px;--type-label:${p.body - 1}px;--type-name:${p.body + 1.5}px;--type-h1:${p.body + 13}px;--type-h2:${p.body + 5}px;--type-h3:${p.body + 1}px;--type-weight:${p.weight};--type-medium:${Math.min(575, p.weight + 75)};--type-strong:${p.strong};--type-heading-weight:${Math.min(700, p.strong + 25)};--type-line:1.55;--type-tracking:-0.01em;`;
}
const faces = fonts
  .map(
    (f) =>
      `@font-face{font-family:"${families.get(f.name)}";src:url("/fonts/${f.file}") format("woff2");font-style:normal;font-weight:${weightRanges[f.name]};font-display:swap;}`,
  )
  .join("\n");
const css =
  `/* Generated from typography-presets.json. */\n${faces}\n:root{${tokens(presets[0])}}\n` +
  presets
    .map((p) => `:root[data-typography="${p.id}"]{${tokens(p)}}`)
    .join("\n") +
  `\n:root[data-typography="original"]{--app-font:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;--type-body:13px;--type-secondary:12px;--type-small:11px;--type-caption:10px;--type-micro:9px;--type-label:12px;--type-name:12px;--type-h1:25px;--type-h2:17px;--type-h3:14px;--type-weight:400;--type-medium:500;--type-strong:560;--type-heading-weight:580;--type-line:1.7;--type-tracking:0;}\n`;
await fs.writeFile(path.join(root, "src/typography-presets.css"), css);
const themePath = path.join(root, "public/theme.js");
let theme = (await fs.readFile(themePath, "utf8"))
  .split("// Typography preference")[0]
  .trimEnd();
theme += `\n// Typography preference\ntry {\n  const id = localStorage.getItem("beepgrid:workspace:type-preset:v1");\n  document.documentElement.dataset.typography = ${JSON.stringify(presets.map((p) => p.id))}.includes(id) ? id : "${presets[0].id}";\n} catch {\n  document.documentElement.dataset.typography = "${presets[0].id}";\n}\n`;
theme += `// Agent icon preference\ntry {\n  const id = localStorage.getItem("beepgrid:workspace:agent-icons:v1");\n  document.documentElement.dataset.agentIcons = ${JSON.stringify(icons.map((p) => p.id))}.includes(id) ? id : "${icons[0].id}";\n} catch {\n  document.documentElement.dataset.agentIcons = "${icons[0].id}";\n}\n`;
await fs.writeFile(themePath, theme);
