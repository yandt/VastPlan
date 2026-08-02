import { readFile, readdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";

const vendorRoot = resolve(process.argv[2] ?? "bin/portal/assets/vendor");
const metafilePath = resolve(process.argv[3] ?? resolve(vendorRoot, "icon-catalog.metafile.json"));
const manifest = JSON.parse(await readFile("extensions/sdk/ts/icon-catalog/src/generated/manifest.json", "utf8"));
const metafile = JSON.parse(await readFile(metafilePath, "utf8"));
const semanticIconContract = await readFile("extensions/sdk/ts/ui-contract/src/icons.ts", "utf8");
const semanticIconCount = (semanticIconContract.match(/^\s+"[a-zA-Z][a-zA-Z0-9]*",$/gm) ?? []).length;
if (manifest.iconCount !== 846 || manifest.shardCount !== 27 || manifest.license !== "MIT") throw new Error("Ant 图标目录清单无效");

const semanticEntry = resolve(vendorRoot, "icon-catalog-semantic.js");
const catalogEntry = resolve(vendorRoot, "icon-catalog.js");
const semanticClosure = staticClosure(semanticEntry, metafile.outputs);
const catalogClosure = staticClosure(catalogEntry, metafile.outputs);
const semanticIcons = iconInputs(semanticClosure, metafile.outputs);
const catalogStartupIcons = iconInputs(catalogClosure, metafile.outputs);
const allIcons = iconInputs(new Set(Object.keys(metafile.outputs).map((file) => resolve(file))), metafile.outputs);
const delayedShards = dynamicDependencies(catalogClosure, metafile.outputs);

if (semanticIcons.size !== semanticIconCount) throw new Error(`语义入口必须只包含契约声明的 ${semanticIconCount} 个图标，实际 ${semanticIcons.size}`);
if (catalogStartupIcons.size !== 0) throw new Error(`完整目录入口不得静态内联 SVG，实际 ${catalogStartupIcons.size}`);
if (allIcons.size !== 846) throw new Error(`构建闭包必须包含完整 846 个图标，实际 ${allIcons.size}`);
if (delayedShards.size !== 27) throw new Error(`完整目录必须生成 27 个延迟分片，实际 ${delayedShards.size}`);

const files = [semanticEntry, catalogEntry, ...await javascriptFiles(resolve(vendorRoot, "icon-catalog"))];
const semanticBytes = await closureBytes(semanticClosure);
if (files.length > 96) throw new Error(`图标目录输出文件过多: ${files.length}`);
if (semanticBytes > 96 * 1024) throw new Error(`语义图标初始闭包过大: ${semanticBytes} bytes`);
console.log(`Ant 图标按需加载校验通过: 846 个图标，27 个延迟分片，语义闭包 ${semanticBytes} 字节/${semanticClosure.size} 个模块，总输出 ${files.length} 个文件`);

function outputMap(outputs) { return new Map(Object.entries(outputs).map(([file, metadata]) => [resolve(file), metadata])); }

function staticClosure(entry, outputs) {
  const byAbsolute = outputMap(outputs), visited = new Set();
  const visit = (file) => {
    if (visited.has(file)) return;
    const metadata = byAbsolute.get(file);
    if (metadata === undefined) throw new Error(`metafile 缺少输出: ${file}`);
    visited.add(file);
    for (const dependency of metadata.imports ?? []) {
      if (dependency.external || dependency.kind === "dynamic-import") continue;
      visit(resolveDependency(file, dependency.path, byAbsolute));
    }
  };
  visit(entry);
  return visited;
}

function dynamicDependencies(closure, outputs) {
  const byAbsolute = outputMap(outputs), result = new Set();
  for (const file of closure) {
    for (const dependency of byAbsolute.get(file)?.imports ?? []) {
      if (!dependency.external && dependency.kind === "dynamic-import") result.add(resolveDependency(file, dependency.path, byAbsolute));
    }
  }
  return result;
}

function iconInputs(outputFiles, outputs) {
  const byAbsolute = outputMap(outputs), result = new Set();
  for (const file of outputFiles) {
    for (const input of Object.keys(byAbsolute.get(file)?.inputs ?? {})) {
      if (/[/\\]@ant-design[/\\]icons-svg[/\\]es[/\\]asn[/\\].+\.js$/.test(input)) result.add(resolve(input));
    }
  }
  return result;
}

function resolveDependency(importer, dependency, outputs) {
  const path = [resolve(dirname(importer), dependency), resolve(dependency)].find((candidate) => outputs.has(candidate));
  if (path === undefined) throw new Error(`metafile 依赖未闭合: ${importer} -> ${dependency}`);
  return path;
}

async function javascriptFiles(root) {
  const result = [];
  for (const entry of await readdir(root, { withFileTypes: true })) {
    const path = resolve(root, entry.name);
    if (entry.isDirectory()) result.push(...await javascriptFiles(path));
    else if (entry.name.endsWith(".js")) result.push(path);
  }
  return result.sort();
}

async function closureBytes(files) {
  let total = 0;
  for (const file of files) total += (await readFile(file)).byteLength;
  return total;
}
