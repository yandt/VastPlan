import { readFile, realpath, writeFile } from "node:fs/promises";
import { dirname, relative, resolve, sep } from "node:path";

const pluginRoot = await realpath(requiredOption("--plugin-root"));
const manifestPath = resolve(requiredOption("--manifest"));
const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
const catalogs = manifest?.contributes?.frontend?.navigations ?? [];
let totalBytes = 0;

for (const catalog of catalogs) {
  for (const icon of catalog.icons ?? []) {
    if (icon.sources === undefined) continue;
    if (icon.states !== undefined || typeof icon.sources.normal !== "string") fail(`${icon.id}: sources 与 states 不能混用且 normal 必填`);
    const states = {};
    for (const state of ["normal", "active", "loading", "error"]) {
      const source = icon.sources[state];
      if (source === undefined) continue;
      const filename = await confinedSource(source);
      const raw = await readFile(filename, "utf8");
      totalBytes += Buffer.byteLength(raw);
      if (totalBytes > 128 * 1024) fail("导航 SVG source 总量超过 128 KiB");
      states[state] = parseSVG(raw, `${icon.id}/${state}`);
    }
    icon.states = states;
    delete icon.sources;
  }
}

await writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, { mode: 0o600 });

async function confinedSource(source) {
  if (!/^frontend\/icons\/navigation\/[A-Za-z0-9._/-]+\.svg$/.test(source) || source.includes("..") || source.includes("//")) fail(`SVG source 路径无效: ${source}`);
  const candidate = await realpath(resolve(pluginRoot, source));
  const rel = relative(pluginRoot, candidate);
  if (rel === "" || rel === ".." || rel.startsWith(`..${sep}`) || rel.startsWith(sep)) fail(`SVG source 越出插件目录: ${source}`);
  return candidate;
}

function parseSVG(raw, name) {
  if (Buffer.byteLength(raw) > 32 * 1024) fail(`${name}: 单个 SVG 超过 32 KiB`);
  if (/<!|<\?|<!--|&[A-Za-z#]|<\s*(?:script|style|foreignObject|animate|animateTransform|set|use|image|filter|mask|clipPath|defs|symbol|text|circle|rect|line|polyline|polygon|ellipse)\b|\bon[a-z]+\s*=|\b(?:href|xlink:href|filter|style|mask|clip-path)\s*=|url\s*\(/i.test(raw)) {
    fail(`${name}: SVG 包含脚本、外链、样式或不受支持图元`);
  }
  const tokens = raw.match(/<[^>]+>|[^<]+/g) ?? [];
  const stack = [];
  let root;
  for (const token of tokens) {
    if (!token.startsWith("<")) {
      if (token.trim() !== "") fail(`${name}: SVG 不允许文本节点`);
      continue;
    }
    if (/^<\/svg\s*>$/i.test(token)) {
      if (stack.length !== 1 || stack[0].tag !== "svg") fail(`${name}: SVG 层级不完整`);
      stack.pop();
      continue;
    }
    if (/^<\/g\s*>$/i.test(token)) {
      if (stack.length < 2 || stack.at(-1).tag !== "g") fail(`${name}: group 闭合不匹配`);
      stack.pop();
      continue;
    }
    const match = token.match(/^<\s*(svg|g|path)\b([\s\S]*?)(\/?)>$/i);
    if (match === null) fail(`${name}: 只允许 svg/g/path`);
    const tag = match[1].toLowerCase();
    const attrs = parseAttributes(match[2], name);
    const selfClosing = match[3] === "/";
    if (tag === "svg") {
      if (root !== undefined || stack.length !== 0 || selfClosing) fail(`${name}: 必须只有一个非空 svg 根`);
      assertAllowed(attrs, new Set(["viewBox", "fill", "fill-rule", "xmlns", "role", "aria-hidden", "focusable"]), name);
      if (!/^-?[0-9]+(?:\.[0-9]+)? -?[0-9]+(?:\.[0-9]+)? [0-9]+(?:\.[0-9]+)? [0-9]+(?:\.[0-9]+)?$/.test(attrs.viewBox ?? "")) fail(`${name}: viewBox 无效`);
      requireCurrentColor(attrs.fill, name);
      root = { viewBox: attrs.viewBox, ...(attrs["fill-rule"] === undefined ? {} : { fillRule: fillRule(attrs["fill-rule"], name) }), nodes: [] };
      stack.push({ tag: "svg", children: root.nodes });
      continue;
    }
    if (root === undefined || stack.length === 0) fail(`${name}: 图元必须位于 svg 内`);
    if (tag === "g") {
      if (selfClosing) fail(`${name}: group 不能为空`);
      assertAllowed(attrs, new Set(["transform", "fill"]), name);
      requireCurrentColor(attrs.fill, name);
      if (attrs.transform !== undefined && !/^(?:(?:matrix|translate|scale|rotate|skewX|skewY)\([0-9eE.,+\- ]+\)\s*)+$/.test(attrs.transform)) fail(`${name}: transform 无效`);
      const node = { tag: "g", ...(attrs.transform === undefined ? {} : { transform: attrs.transform }), children: [] };
      stack.at(-1).children.push(node);
      stack.push({ tag: "g", children: node.children });
      continue;
    }
    if (!selfClosing || stack.at(-1).children.length >= 32) fail(`${name}: path 必须自闭合且每组最多 32 个节点`);
    assertAllowed(attrs, new Set(["d", "fill", "fill-rule", "opacity", "data-tone", "stroke"]), name);
    requireCurrentColor(attrs.fill, name);
    if (attrs.stroke !== undefined && attrs.stroke !== "none") fail(`${name}: stroke 只允许 none`);
    if (!/^[MmZzLlHhVvCcSsQqTtAa0-9.,+eE \-]+$/.test(attrs.d ?? "") || attrs.d.length > 32768) fail(`${name}: path d 无效`);
    const tone = attrs["data-tone"] ?? "primary";
    if (tone !== "primary" && tone !== "secondary") fail(`${name}: data-tone 无效`);
    const node = { tag: "path", d: attrs.d, tone };
    if (attrs.opacity !== undefined) {
      const opacity = Number(attrs.opacity);
      if (!Number.isFinite(opacity) || opacity < 0 || opacity > 1) fail(`${name}: opacity 无效`);
      node.opacity = opacity;
    }
    if (attrs["fill-rule"] !== undefined) node.fillRule = fillRule(attrs["fill-rule"], name);
    stack.at(-1).children.push(node);
  }
  if (root === undefined || stack.length !== 0 || root.nodes.length === 0) fail(`${name}: SVG 缺少完整的安全图元`);
  return root;
}

function parseAttributes(raw, name) {
  const attrs = {};
  let rest = raw.trim();
  while (rest !== "") {
    const match = rest.match(/^([A-Za-z_:][A-Za-z0-9_.:-]*)\s*=\s*("[^"]*"|'[^']*')\s*/);
    if (match === null) fail(`${name}: SVG 属性必须使用引号且格式合法`);
    const key = match[1];
    if (Object.hasOwn(attrs, key)) fail(`${name}: SVG 属性重复 ${key}`);
    attrs[key] = match[2].slice(1, -1);
    rest = rest.slice(match[0].length);
  }
  return attrs;
}

function assertAllowed(attrs, allowed, name) {
  for (const key of Object.keys(attrs)) if (!allowed.has(key)) fail(`${name}: 不允许 SVG 属性 ${key}`);
}

function requireCurrentColor(value, name) {
  if (value !== undefined && value !== "currentColor") fail(`${name}: fill 只允许 currentColor`);
}

function fillRule(value, name) {
  if (value !== "evenodd" && value !== "nonzero") fail(`${name}: fill-rule 无效`);
  return value;
}

function requiredOption(name) {
  const index = process.argv.indexOf(name);
  const value = index < 0 ? undefined : process.argv[index + 1];
  if (value === undefined || value.startsWith("--")) fail(`${name} 缺少值`);
  return value;
}

function fail(message) { throw new Error(message); }
