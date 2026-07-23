import { build } from "esbuild";
import { mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";

const outputRoot = option("--out-dir");
if (outputRoot === undefined) throw new Error("必须提供 --out-dir");

const pluginsRoot = resolve("extensions/plugins");
const directories = (await readdir(pluginsRoot, { withFileTypes: true }))
  .filter((entry) => entry.isDirectory())
  .sort((left, right) => left.name.localeCompare(right.name));

for (const directory of directories) {
  const pluginRoot = resolve(pluginsRoot, directory.name);
  const manifest = JSON.parse(await readFile(resolve(pluginRoot, "vastplan.plugin.json"), "utf8"));
  if (manifest.execution?.backend?.driver !== "node-worker") continue;
  const id = typeof manifest.id === "string" ? manifest.id.trim() : "";
  const entry = typeof manifest.entry?.backend === "string" ? manifest.entry.backend.trim() : "";
  if (id !== directory.name) throw new Error(`${directory.name}: Manifest id 与目录不一致`);
  if (!/^backend\/[A-Za-z0-9._/-]+\.m?js$/.test(entry) || entry.includes("..")) {
    throw new Error(`${id}: node-worker entry.backend 必须是 backend/ 下的 JavaScript ESM`);
  }
  const source = resolve(pluginRoot, entry);
  const outfile = resolve(outputRoot, id, entry);
  await rm(dirname(outfile), { recursive: true, force: true });
  await mkdir(dirname(outfile), { recursive: true });
  const result = await build({
    entryPoints: [source],
    outfile,
    bundle: true,
    platform: "node",
    format: "esm",
    target: "node20",
    legalComments: "none",
    minify: true,
    sourcemap: false,
    metafile: true,
    banner: { js: 'import { createRequire as __vastplanCreateRequire } from "node:module"; const require = __vastplanCreateRequire(import.meta.url);' },
  });
  await writeFile(resolve(dirname(outfile), "vastplan.node-metafile.json"), `${JSON.stringify(result.metafile, null, 2)}\n`);
}

function option(name) {
  const index = process.argv.indexOf(name);
  if (index === -1) return undefined;
  const value = process.argv[index + 1];
  if (value === undefined || value.startsWith("--")) throw new Error(`${name} 缺少值`);
  return resolve(value);
}
