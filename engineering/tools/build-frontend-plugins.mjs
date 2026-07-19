import { build } from "esbuild";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, readFile, readdir, stat, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";

const outputRoot = option("--out-dir");
const manifestPath = option("--manifest");

const common = {
  bundle: true,
  format: "esm",
  platform: "browser",
  target: "es2022",
  legalComments: "none",
  minify: true,
  define: { "process.env.NODE_ENV": '"production"' },
  external: ["react", "react-dom", "react/jsx-runtime", "@vastplan/ui-primitives", "@vastplan/ui-contract", "@vastplan/workbench-sdk"],
};

const plugins = await discoverFrontendPlugins();

const modules = [];
for (const { id, entry, source, deferred } of plugins) {
  const outfile = outputRoot === undefined
    ? resolve("extensions/plugins", id, entry)
    : resolve(outputRoot, `${id}.js`);
  await mkdir(dirname(outfile), { recursive: true });
  await build({
    ...common,
    // UI adapters may export their framework styles as module text. Applying
    // this loader uniformly keeps discovery independent from plugin identity.
    loader: { ".css": "text" },
    entryPoints: [source],
    outfile,
  });
  if (id === "cn.vastplan.foundation.frontend.render.adapter.arco") {
    const result = spawnSync(process.execPath, ["engineering/tools/check-arco-on-demand.mjs"], { stdio: "inherit", env: { ...process.env, ARCO_BUNDLE_FILE: outfile } });
    if (result.status !== 0) process.exit(result.status ?? 1);
  }
  if (outputRoot !== undefined) {
    const bytes = await readFile(outfile);
    modules.push({ id, entry, file: outfile, sha256: createHash("sha256").update(bytes).digest("hex"), deferred });
  }
}

async function discoverFrontendPlugins() {
  const root = resolve("extensions/plugins");
  const entries = await readdir(root, { withFileTypes: true });
  const plugins = [];
  for (const directory of entries.filter((entry) => entry.isDirectory()).sort((left, right) => left.name.localeCompare(right.name))) {
    const pluginRoot = resolve(root, directory.name);
    const manifestPath = resolve(pluginRoot, "vastplan.plugin.json");
    const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
    const id = typeof manifest.id === "string" ? manifest.id.trim() : "";
    const entry = typeof manifest.entry?.frontend === "string" ? manifest.entry.frontend.trim() : "";
    if (entry === "") continue;
    if (id === "" || id !== directory.name) throw new Error(`${manifestPath} 的 id 必须等于插件目录名`);
    if (!/^frontend\/dist\/[A-Za-z0-9._/-]+\.(?:m?js)$/.test(entry) || entry.includes("..")) {
      throw new Error(`${manifestPath} 的 entry.frontend 必须是 frontend/dist/ 下的 JavaScript 文件`);
    }
    const rendererModules = manifest.contributes?.frontend?.rendererModules;
    const deferred = Array.isArray(rendererModules) && rendererModules.length === 1;
    plugins.push({ id, entry, deferred, source: await findFrontendSource(pluginRoot, id) });
  }
  return plugins;
}

async function findFrontendSource(pluginRoot, id) {
  for (const suffix of ["tsx", "ts", "jsx", "js"]) {
    const source = resolve(pluginRoot, `frontend/src/index.${suffix}`);
    try {
      if ((await stat(source)).isFile()) return source;
    } catch (error) {
      if (error?.code !== "ENOENT") throw error;
    }
  }
  throw new Error(`前端插件 ${id} 声明了 entry.frontend，但缺少 frontend/src/index.(tsx|ts|jsx|js)`);
}

if (manifestPath !== undefined) {
  if (outputRoot === undefined) throw new Error("--manifest 必须与 --out-dir 一起使用");
  await mkdir(dirname(resolve(manifestPath)), { recursive: true });
  await writeFile(manifestPath, `${JSON.stringify({ version: 1, modules }, null, 2)}\n`, { mode: 0o600 });
}

function option(name) {
  const index = process.argv.indexOf(name);
  if (index === -1) return undefined;
  const value = process.argv[index + 1];
  if (value === undefined || value.startsWith("--")) throw new Error(`${name} 缺少值`);
  return value;
}
