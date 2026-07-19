import { copyFile, mkdir, readFile, writeFile } from "node:fs/promises";
import { basename, join, resolve } from "node:path";

const assets = resolve(process.argv[2] ?? "bin/portal/assets");
const fonts = join(assets, "fonts");
await mkdir(fonts, { recursive: true });

const sources = [
  ["node_modules/@fontsource/noto-sans", "latin-400.css"],
  ["node_modules/@fontsource/noto-sans", "latin-600.css"],
  ["node_modules/@fontsource/noto-sans-sc", "400.css"],
  ["node_modules/@fontsource/noto-sans-sc", "600.css"],
];
const css = [];
const files = new Map();
for (const [root, stylesheet] of sources) {
  const raw = await readFile(join(root, stylesheet), "utf8");
  for (const match of raw.matchAll(/\.\/files\/([^)'\"]+\.woff2)/g)) files.set(match[1], join(root, "files", match[1]));
  css.push(raw
    .replace(/,\s*url\(\.\/files\/[^)]+\.woff\)\s*format\(['\"]woff['\"]\)/g, "")
    .replaceAll("./files/", "./fonts/"));
}
await Promise.all([...files.entries()].map(([name, source]) => copyFile(source, join(fonts, basename(name)))));
await writeFile(join(assets, "portal-fonts.css"), `${css.join("\n")}\n`);
console.log(`已构建 Portal 字体子集: ${files.size} 个 woff2`);
