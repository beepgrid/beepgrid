import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const dist = path.join(root, "dist");
const assets = await fs.readdir(path.join(dist, "assets"));
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
  css_gzip_kib: 8,
};
const measured = {
  entry_js_gzip_kib: entry / 1024,
  all_js_gzip_kib: total / 1024,
  css_gzip_kib: css / 1024,
};
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
      ...assets.filter((n) => /\.(js|css)$/.test(n)).map((n) => "assets/" + n),
    ]) {
      const bytes = await fs.readFile(path.join(dist, name));
      await fs.mkdir(path.dirname(path.join(stage, name)), { recursive: true });
      await fs.writeFile(path.join(stage, name), bytes);
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
