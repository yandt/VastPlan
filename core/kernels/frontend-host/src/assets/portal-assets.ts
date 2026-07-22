import { createHash, randomBytes } from "node:crypto";
import { lstat, opendir, readFile, realpath, stat } from "node:fs/promises";
import { extname, relative, resolve, sep } from "node:path";

export const portalNoncePlaceholder = "__VASTPLAN_CSP_NONCE__";
const portalRootMarker = '<div id="vastplan-portal" aria-live="polite"></div>';
const maxAssetFiles = 512;
const maxAssetBytes = 64 * 1024 * 1024;

export interface PortalAsset {
  content: Buffer;
  contentType: string;
  etag: string;
}

export class PortalAssets {
  private constructor(
    private readonly index: Buffer,
    private readonly assets: ReadonlyMap<string, PortalAsset>,
  ) {}

  public static async load(root: string): Promise<PortalAssets> {
    const canonicalRoot = await realpath(root);
    const rootStats = await stat(canonicalRoot);
    if (!rootStats.isDirectory()) throw new Error("Portal 静态产物根路径必须是目录");
    const indexPath = resolveContained(canonicalRoot, "index.html");
    const indexStats = await lstat(indexPath);
    if (!indexStats.isFile() || indexStats.isSymbolicLink()) throw new Error("Portal index.html 必须是普通文件");
    const index = await readFile(indexPath);
    if (!index.includes(portalNoncePlaceholder)) throw new Error("Portal index.html 缺少 CSP nonce 占位符");
		if (!index.includes(portalRootMarker)) throw new Error("Portal index.html 缺少 SSR 宿主标记");

    const assets = new Map<string, PortalAsset>();
    let totalBytes = index.byteLength;
    const assetsRoot = resolveContained(canonicalRoot, "assets");
    const assetsRootStats = await lstat(assetsRoot);
    if (!assetsRootStats.isDirectory() || assetsRootStats.isSymbolicLink()) throw new Error("Portal assets 必须是普通目录");
    for await (const file of walkRegularFiles(assetsRoot)) {
      if (assets.size >= maxAssetFiles) throw new Error("Portal assets 文件数超过上限");
      const content = await readFile(file.absolutePath);
      totalBytes += content.byteLength;
      if (totalBytes > maxAssetBytes) throw new Error("Portal assets 总大小超过上限");
      const sha256 = createHash("sha256").update(content).digest("hex");
      assets.set(file.relativePath, Object.freeze({ content, contentType: contentType(file.relativePath), etag: `"sha256-${sha256}"` }));
    }
    if (assets.size === 0) throw new Error("Portal assets 目录不能为空");
    return new PortalAssets(Buffer.from(index), assets);
  }

  public renderIndex(ssrHTML?: string): { body: Buffer; nonce: string } {
    const nonce = randomBytes(24).toString("base64url");
		let html = this.index.toString("utf8").replaceAll(portalNoncePlaceholder, nonce);
		if (ssrHTML !== undefined) {
			if (!html.includes(portalRootMarker)) throw new Error("Portal index.html 缺少 SSR 宿主标记");
			html = html.replace(portalRootMarker, `<div id="vastplan-portal" aria-live="polite"><template shadowrootmode="open"><div id="vastplan-portal-root">${ssrHTML}</div></template></div>`);
		}
		return { body: Buffer.from(html), nonce };
  }

  public get(name: string): PortalAsset | undefined {
    return this.assets.get(name);
  }

  public getVerified(name: string, sha256: string): PortalAsset | undefined {
    const asset = this.assets.get(name);
    return asset?.etag === `"sha256-${sha256}"` ? asset : undefined;
  }
}

async function* walkRegularFiles(root: string): AsyncGenerator<{ absolutePath: string; relativePath: string }> {
  const canonicalRoot = await realpath(root);
  const pending = [canonicalRoot];
  while (pending.length > 0) {
    const directory = pending.pop()!;
    const handle = await opendir(directory);
    for await (const entry of handle) {
      if (entry.isSymbolicLink()) throw new Error("Portal assets 不能包含符号链接");
      const absolutePath = resolveContained(canonicalRoot, relative(canonicalRoot, resolve(directory, entry.name)));
      if (entry.isDirectory()) pending.push(absolutePath);
      else if (entry.isFile()) yield { absolutePath, relativePath: relative(canonicalRoot, absolutePath).split(sep).join("/") };
      else throw new Error("Portal assets 只能包含普通文件");
    }
  }
}

function resolveContained(root: string, name: string): string {
  const path = resolve(root, name);
  if (path !== root && !path.startsWith(`${root}${sep}`)) throw new Error("Portal 静态产物路径越界");
  return path;
}

function contentType(name: string): string {
  switch (extname(name).toLowerCase()) {
    case ".html": return "text/html; charset=utf-8";
    case ".js": case ".mjs": return "text/javascript; charset=utf-8";
    case ".css": return "text/css; charset=utf-8";
    case ".json": return "application/json; charset=utf-8";
    case ".svg": return "image/svg+xml";
    case ".png": return "image/png";
    case ".jpg": case ".jpeg": return "image/jpeg";
    case ".webp": return "image/webp";
    case ".woff2": return "font/woff2";
    default: return "application/octet-stream";
  }
}
