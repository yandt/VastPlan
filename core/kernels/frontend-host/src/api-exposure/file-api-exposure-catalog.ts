import { constants } from "node:fs";
import { open } from "node:fs/promises";
import type { APIExposureCatalog, APIExposureCatalogPort, ResolvedAPIExposure } from "./api-exposure-contract";
import { parseAPIExposureCatalog } from "./api-exposure-schema";

const maximumCatalogBytes = 64 << 20;
const refreshIntervalMilliseconds = 1_000;

export class FileAPIExposureCatalog implements APIExposureCatalogPort {
  private catalog!: APIExposureCatalog;
  private signature = "";
  private nextRefreshAt = 0;

  private constructor(private readonly path: string, private readonly now: () => number) {}

  public static async open(path: string, now: () => number = Date.now): Promise<FileAPIExposureCatalog> {
    const result = new FileAPIExposureCatalog(path, now);
    await result.reload(true);
    return result;
  }

  public async resolve(host: string, routeKey: string, majorVersion: number): Promise<ResolvedAPIExposure | undefined> {
    await this.reload(false);
    const normalizedHost = normalizeHost(host);
    return this.catalog.exposures.find((resolved) => resolved.exposure.routeKey === routeKey
      && contractMajor(resolved.contract.contractVersion) === majorVersion
      && resolved.exposure.hosts.includes(normalizedHost));
  }

  private async reload(required: boolean): Promise<void> {
    const now = this.now();
    if (!required && now < this.nextRefreshAt) return;
    this.nextRefreshAt = now + refreshIntervalMilliseconds;
    try {
      const handle = await open(this.path, constants.O_RDONLY | constants.O_NOFOLLOW);
      try {
        const stat = await handle.stat({ bigint: true });
        if (!stat.isFile() || (stat.mode & 0o022n) !== 0n) throw new Error("API Exposure Catalog 必须是不可由组或其他用户写入的普通文件");
        if (stat.size > BigInt(maximumCatalogBytes)) throw new Error("API Exposure Catalog 超过大小上限");
        const signature = `${stat.dev}:${stat.ino}:${stat.size}:${stat.mtimeNs}`;
        if (!required && signature === this.signature) return;
        const next = parseAPIExposureCatalog(await handle.readFile("utf8"));
        if (!required && next.generation < this.catalog.generation) throw new Error("API Exposure Catalog generation 不得回退");
        this.catalog = next;
        this.signature = signature;
      } finally {
        await handle.close();
      }
    } catch (error) {
      if (required) throw error;
      process.stderr.write(`${JSON.stringify({ level: "error", message: "api exposure catalog reload rejected", error: error instanceof Error ? error.message : String(error) })}\n`);
    }
  }
}

function normalizeHost(value: string): string {
  const host = value.trim().toLowerCase();
  if (host.startsWith("[")) return host;
  return host.replace(/:\d+$/, "").replace(/\.$/, "");
}

function contractMajor(version: string): number {
  return Number(version.split(".", 1)[0]);
}
