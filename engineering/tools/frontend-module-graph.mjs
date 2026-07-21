import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { dirname, extname, relative, resolve, sep } from "node:path";

export async function createFrontendModuleGraph({ target, pluginRoot, entry, metafile, allowedExternals }) {
  if (target !== "browser" && target !== "server") throw new Error(`Module Graph target 无效: ${target}`);
  const outputs = Object.entries(metafile.outputs ?? {});
  const outputByAbsolutePath = new Map(outputs.map(([path, metadata]) => [resolve(path), metadata]));
  const externals = new Set();
  const nodes = [];
  for (const [outputPath, metadata] of outputs) {
    const absolutePath = resolve(outputPath);
    const path = packagePath(pluginRoot, absolutePath);
    const dependencies = [];
    for (const dependency of metadata.imports ?? []) {
      if (dependency.external) {
        if (!allowedExternals.has(dependency.path)) throw new Error(`Module Graph 包含未允许的外部依赖: ${dependency.path}`);
        externals.add(dependency.path);
        continue;
      }
      const dependencyAbsolute = resolve(dirname(absolutePath), dependency.path);
      if (!outputByAbsolutePath.has(dependencyAbsolute)) throw new Error(`Module Graph 依赖不在构建闭包中: ${path} -> ${dependency.path}`);
      dependencies.push({ specifier: dependency.path, path: packagePath(pluginRoot, dependencyAbsolute), kind: dependencyKind(dependency.kind) });
    }
    const content = await readFile(absolutePath);
    if (content.byteLength !== metadata.bytes) throw new Error(`Module Graph 输出大小漂移: ${path}`);
    nodes.push({
      path,
      sha256: createHash("sha256").update(content).digest("hex"),
      size: content.byteLength,
      mediaType: mediaType(path),
      purpose: path === entry ? "entry" : purpose(path),
      dependencies: dependencies.sort(compareDependency),
    });
  }
  nodes.sort((left, right) => left.path.localeCompare(right.path));
  if (!nodes.some((node) => node.path === entry && node.purpose === "entry")) throw new Error(`Module Graph 入口未生成: ${entry}`);
  const graph = { schemaVersion: "v1", target, entry, externals: [...externals].sort(), nodes };
  return { ...graph, digest: computeFrontendModuleGraphDigest(graph) };
}

export function computeFrontendModuleGraphDigest(graph) {
  const canonical = {
    schemaVersion: graph.schemaVersion,
    target: graph.target,
    entry: graph.entry,
    externals: [...graph.externals].sort(),
    nodes: graph.nodes.map((node) => ({ ...node, dependencies: [...node.dependencies].sort(compareDependency) })).sort((left, right) => left.path.localeCompare(right.path)),
  };
  return createHash("sha256").update(JSON.stringify(canonical)).digest("hex");
}

function packagePath(root, absolutePath) {
  const path = relative(resolve(root), absolutePath);
  if (path === "" || path === ".." || path.startsWith(`..${sep}`)) throw new Error(`Module Graph 输出逃逸插件目录: ${absolutePath}`);
  return path.split(sep).join("/");
}

function dependencyKind(kind) {
  if (kind === "dynamic-import") return "dynamic";
  if (kind === "file-loader" || kind === "url-token") return "asset";
  return "static";
}

function mediaType(path) {
  switch (extname(path).toLowerCase()) {
    case ".js": case ".mjs": return "text/javascript";
    case ".css": return "text/css";
    case ".json": case ".map": return "application/json";
    case ".wasm": return "application/wasm";
    case ".svg": return "image/svg+xml";
    case ".woff2": return "font/woff2";
    default: return "application/octet-stream";
  }
}

function purpose(path) {
  switch (extname(path).toLowerCase()) {
    case ".css": return "style";
    case ".map": return "source-map";
    case ".json": return "locale";
    case ".js": case ".mjs": return "chunk";
    default: return "asset";
  }
}

function compareDependency(left, right) {
  if (left.specifier !== right.specifier) return left.specifier.localeCompare(right.specifier);
  return left.path === right.path ? left.kind.localeCompare(right.kind) : left.path.localeCompare(right.path);
}
