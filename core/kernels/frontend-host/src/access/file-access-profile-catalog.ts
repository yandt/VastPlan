import { constants } from "node:fs";
import { open } from "node:fs/promises";
import type { AccessCatalogPort } from "./access-catalog-port";
import { createAccessGeneration, type AccessGeneration } from "./access-generation";
import { parseAccessProfileCatalog, type AccessProfile } from "./access-profile-contract";

const maxCatalogBytes = 16 << 20;

export class FileAccessProfileCatalog implements AccessCatalogPort {
  private constructor(private readonly path: string) {}

  public static async open(path: string): Promise<FileAccessProfileCatalog> {
    const catalog = new FileAccessProfileCatalog(path);
    await catalog.readProfiles();
    return catalog;
  }

  public async resolve(host: string, path: string): Promise<AccessGeneration | undefined> {
    const normalizedHost = host.toLowerCase().replace(/\.$/, "");
    let selected: AccessProfile | undefined;
    for (const profile of await this.readProfiles()) {
      if (!profile.domains.includes(normalizedHost) || !routeMatches(profile.route, path)) continue;
      if (selected === undefined || profile.route.length > selected.route.length) selected = profile;
    }
    return selected === undefined ? undefined : createAccessGeneration(selected);
  }

  private async readProfiles(): Promise<readonly AccessProfile[]> {
    const handle = await open(this.path, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const file = await handle.stat();
      if (!file.isFile() || (file.mode & 0o022) !== 0) {
        throw new Error("Access Profile Catalog 必须是不可由组或其他用户写入的普通文件");
      }
      if (file.size > maxCatalogBytes) throw new Error("Access Profile Catalog 超过大小上限");
      const raw = await handle.readFile("utf8");
      let document: unknown;
      try { document = JSON.parse(raw) as unknown; } catch { throw new Error("Access Profile Catalog 不是有效 JSON"); }
      const candidate = typeof document === "object" && document !== null && !Array.isArray(document) && "accessCatalog" in document
        ? (document as { accessCatalog?: unknown }).accessCatalog : document;
      if (candidate === undefined) throw new Error("Provider Management publication 尚未包含 Access Catalog");
      return parseAccessProfileCatalog(JSON.stringify(candidate)).profiles;
    } finally {
      await handle.close();
    }
  }
}

function routeMatches(route: string, path: string): boolean {
  return route === "/" || path === route || path.startsWith(`${route}/`);
}
