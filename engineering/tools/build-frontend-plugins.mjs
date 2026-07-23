import { build } from "esbuild";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, readFile, readdir, rm, stat, writeFile } from "node:fs/promises";
import { basename, dirname, extname, resolve } from "node:path";
import { createFrontendModuleGraph } from "./frontend-module-graph.mjs";
import { isDeferredFrontendContribution } from "./frontend-plugin-contribution.mjs";
import { buildFrontendServerGraph } from "./frontend-server-build.mjs";

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
  external: ["react", "react-dom", "react/jsx-runtime", "@vastplan/rjsf-csp-validator", "@vastplan/ui-primitives", "@vastplan/ui-contract", "@vastplan/workbench-sdk"],
};
const allowedExternals = new Set(common.external);

const plugins = await discoverFrontendPlugins();

const modules = [];
for (const { id, entry, source, serverEntry, serverSource, deferred, pluginRoot } of plugins) {
  await enforceFunctionalPluginBoundary(id, dirname(dirname(source)));
  const buildRoot = outputRoot === undefined ? pluginRoot : resolve(outputRoot, id);
  const outfile = resolve(buildRoot, entry);
  const outdir = dirname(outfile);
  await rm(outdir, { recursive: true, force: true });
  await mkdir(outdir, { recursive: true });
  const entryName = basename(entry, extname(entry));
  const result = await build({
    ...common,
    // UI adapters may export their framework styles as module text. Applying
    // this loader uniformly keeps discovery independent from plugin identity.
    loader: { ".css": "text" },
    entryPoints: { [entryName]: source },
    outdir,
    entryNames: entryName,
    chunkNames: "chunks/[name]-[hash]",
    assetNames: "assets/[name]-[hash]",
    splitting: true,
    metafile: true,
    outExtension: { ".js": extname(entry) },
  });
  const graph = await createFrontendModuleGraph({ target: "browser", pluginRoot: buildRoot, entry, metafile: result.metafile, allowedExternals });
  const graphFile = resolve(outdir, "vastplan.browser-graph.json");
  await writeFile(graphFile, `${JSON.stringify(graph, null, 2)}\n`);
  await writeFile(resolve(outdir, "vastplan.browser-metafile.json"), `${JSON.stringify(result.metafile, null, 2)}\n`);
  let serverGraph;
  let serverGraphFile;
  if (serverEntry !== undefined && serverSource !== undefined) {
		({ graph: serverGraph, graphFile: serverGraphFile } = await buildFrontendServerGraph({ buildRoot, serverEntry, serverSource }));
  }
  if (id === "cn.vastplan.foundation.frontend.render.adapter.arco") {
    const result = spawnSync(process.execPath, ["engineering/tools/check-arco-on-demand.mjs"], { stdio: "inherit", env: { ...process.env, ARCO_BUNDLE_FILE: outfile } });
    if (result.status !== 0) process.exit(result.status ?? 1);
  }
  if (id === "cn.vastplan.foundation.frontend.render.adapter.mui") {
    const result = spawnSync(process.execPath, ["engineering/tools/check-mui-icons-on-demand.mjs"], { stdio: "inherit", env: { ...process.env, MUI_BUNDLE_FILE: outfile } });
    if (result.status !== 0) process.exit(result.status ?? 1);
  }
  if (outputRoot !== undefined) {
    const bytes = await readFile(outfile);
    modules.push({ id, entry, file: outfile, sha256: createHash("sha256").update(bytes).digest("hex"), graphFile, graph, deferred,
      ...(serverGraph === undefined ? {} : { serverEntry, serverGraphFile, serverGraph }) });
  }
}

/**
 * Functional UI plugins declare Workbench pages; they do not own a component
 * tree. Foundation frontend plugins remain the only layer allowed to import a
 * rendering framework or the primitive UI surface.
 */
async function enforceFunctionalPluginBoundary(id, frontendRoot) {
  if (id.startsWith("cn.vastplan.foundation.frontend.")) return;
  const sourceRoot = resolve(frontendRoot, "src");
  const files = await sourceFiles(sourceRoot);
  let importsWorkbench = false;
  for (const file of files) {
    const content = await readFile(file, "utf8");
    importsWorkbench ||= /from\s+["']@vastplan\/workbench-sdk["']/.test(content);
    if (/from\s+["'](?:react|react-dom(?:\/[^"']*)?|@arco-design\/[^"']+|@mui\/[^"']+)["']/.test(content)) {
      throw new Error(`${id}: 功能插件不得直接导入 React 或 UI 框架 (${file})`);
    }
    if (/\bcontext\.addPage\s*\(/.test(content)) {
      throw new Error(`${id}: 功能插件必须通过 Workbench 注册页面，禁止 context.addPage (${file})`);
    }
    for (const match of content.matchAll(/import\s+(?:type\s+)?\{([\s\S]*?)\}\s+from\s+["']@vastplan\/ui-primitives["']/g)) {
      const names = match[1].split(",").map((entry) => entry.trim().replace(/^type\s+/, "").split(/\s+as\s+/)[0]).filter(Boolean);
      const forbidden = names.filter((name) => !name.startsWith("Portal"));
      if (forbidden.length > 0) throw new Error(`${id}: 功能插件只能从 ui-primitives 使用非视觉 Portal 客户端契约，禁止 ${forbidden.join(", ")} (${file})`);
    }
    if (/from\s+["']@vastplan\/ui-primitives["']/.test(content) && !/import\s+(?:type\s+)?\{/.test(content)) {
      throw new Error(`${id}: 功能插件不得整体导入 ui-primitives (${file})`);
    }
  }
  if (!importsWorkbench) throw new Error(`${id}: 功能前端插件必须使用 @vastplan/workbench-sdk`);
}

async function sourceFiles(root) {
  const result = [];
  for (const entry of await readdir(root, { withFileTypes: true })) {
    const path = resolve(root, entry.name);
    if (entry.isDirectory()) result.push(...await sourceFiles(path));
    else if (/\.(?:[cm]?[jt]sx?)$/.test(entry.name) && !/\.test\.[cm]?[jt]sx?$/.test(entry.name) && !/\.d\.ts$/.test(entry.name)) result.push(path);
  }
  return result;
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
    const deferred = isDeferredFrontendContribution(manifest.contributes?.frontend);
    const serverEntry = typeof manifest.entry?.frontendServer === "string" ? manifest.entry.frontendServer.trim() : undefined;
    if (serverEntry !== undefined && (!/^frontend\/dist\/[A-Za-z0-9._/-]+\.(?:m?js)$/.test(serverEntry) || serverEntry.includes(".."))) {
      throw new Error(`${manifestPath} 的 entry.frontendServer 必须是 frontend/dist/ 下的 JavaScript 文件`);
    }
    plugins.push({ id, entry, serverEntry, deferred, pluginRoot, source: await findFrontendSource(pluginRoot, id),
      ...(serverEntry === undefined ? {} : { serverSource: await findFrontendServerSource(pluginRoot, id) }) });
  }
  return plugins;
}

async function findFrontendServerSource(pluginRoot, id) {
  for (const suffix of ["tsx", "ts", "jsx", "js"]) {
    const source = resolve(pluginRoot, `frontend/src/server.${suffix}`);
    try { if ((await stat(source)).isFile()) return source; }
    catch (error) { if (error?.code !== "ENOENT") throw error; }
  }
  throw new Error(`前端插件 ${id} 声明了 entry.frontendServer，但缺少 frontend/src/server.(tsx|ts|jsx|js)`);
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
