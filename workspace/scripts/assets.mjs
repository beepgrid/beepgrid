import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";
import { createHash } from "node:crypto";
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const dist = path.join(root, "dist");
const assets = await fs.readdir(path.join(dist, "assets"));
const fonts = JSON.parse(
  await fs.readFile(path.join(dist, "fonts/manifest.json"), "utf8"),
);
let fontBytes = 0,
  largestFont = 0;
for (const font of fonts) {
  if (!/^[a-z0-9.-]+\.woff2$/.test(font.file))
    throw Error("Invalid font asset path");
  const bytes = await fs.readFile(path.join(dist, "fonts", font.file));
  if (
    bytes.subarray(0, 4).toString() !== "wOF2" ||
    createHash("sha256").update(bytes).digest("hex") !== font.sha256
  )
    throw Error("Font integrity mismatch: " + font.file);
  fontBytes += bytes.length;
  largestFont = Math.max(largestFont, bytes.length);
}
let total = 0,
  css = 0,
  entry = 0;
for (const name of assets) {
  if (!/\.(js|css)$/.test(name)) continue;
  const compressed = gzipSync(
    await fs.readFile(path.join(dist, "assets", name)),
  ).length;
  if (name.endsWith(".css")) css += compressed;
  else {
    total += compressed;
    if (name.startsWith("index-")) entry += compressed;
  }
}
const budgets = {
  entry_js_gzip_kib: 96,
  all_js_gzip_kib: 145,
  css_gzip_kib: 12.5,
  all_fonts_kib: 320,
  largest_font_kib: 96,
};
const measured = {
  entry_js_gzip_kib: entry / 1024,
  all_js_gzip_kib: total / 1024,
  css_gzip_kib: css / 1024,
  all_fonts_kib: fontBytes / 1024,
  largest_font_kib: largestFont / 1024,
};
const icons = JSON.parse(
  await fs.readFile(path.join(dist, "agent-icons/manifest.json"), "utf8"),
);
const sprite = await fs.readFile(path.join(dist, "agent-icons/marks.svg"));
if (createHash("sha256").update(sprite).digest("hex") !== icons.sprite_sha256)
  throw Error("Agent icon sprite integrity mismatch");
if (gzipSync(sprite).length > 5 * 1024)
  throw Error("Agent icon sprite exceeds 5 KiB gzip");
for (const key of Object.keys(budgets))
  if (measured[key] > budgets[key])
    throw Error(`${key}: ${measured[key].toFixed(2)} exceeds ${budgets[key]}`);
console.log(JSON.stringify({ measured, budgets }, null, 2));
if (process.argv.includes("--embed")) {
  const parent = path.resolve(root, "../internal/web");
  await fs.access(path.join(parent, "web.go"));
  const stage = await fs.mkdtemp(path.join(parent, ".workspace-build-"));
  try {
    for (const name of [
      "index.html",
      "theme.js",
      "icon.svg",
      "agent-icons/marks.svg",
      "agent-icons/manifest.json",
      "agent-icons/lucide-LICENSE.txt",
      "agent-icons/phosphor-LICENSE.txt",
      "fonts/manifest.json",
      ...fonts.flatMap((f) => [
        "fonts/" + f.file,
        "fonts/" + f.name + "-OFL.txt",
      ]),
      ...assets.filter((n) => /\.(js|css)$/.test(n)).map((n) => "assets/" + n),
    ]) {
      const bytes = await fs.readFile(path.join(dist, name));
      await fs.mkdir(path.dirname(path.join(stage, name)), { recursive: true });
      await fs.writeFile(path.join(stage, name), bytes);
      if (!name.endsWith(".woff2"))
        await fs.writeFile(
          path.join(stage, name + ".gz"),
          gzipSync(bytes, { level: 9 }),
        );
    }
    await fs.rm(path.join(parent, "dist"), { recursive: true, force: true });
    await fs.rename(stage, path.join(parent, "dist"));
  } finally {
    await fs.rm(stage, { recursive: true, force: true });
  }
  console.log("Embedded the independent workspace build, without source maps.");
}
