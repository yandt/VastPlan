import { constants } from "node:fs";
import { open } from "node:fs/promises";
import type { APIContractCatalog, APIContractCatalogPort, APIContractContribution, APIContractReference } from "./api-exposure-contract";
import { parseAPIContractCatalog } from "./api-exposure-schema";

const maximumCatalogBytes = 64 << 20;
const refreshIntervalMilliseconds = 1_000;

export class FileAPIContractCatalog implements APIContractCatalogPort {
  private catalog!: APIContractCatalog;
  private signature = "";
  private nextRefreshAt = 0;

  private constructor(private readonly path: string, private readonly now: () => number) {}

  public static async open(path: string, now: () => number = Date.now): Promise<FileAPIContractCatalog> {
    const result = new FileAPIContractCatalog(path, now);
    await result.reload(true);
    return result;
  }

  public async resolveContract(reference: APIContractReference): Promise<APIContractContribution | undefined> {
    await this.reload(false);
    return this.catalog.contracts.find((candidate) => candidate.reference.contractId === reference.contractId
      && candidate.reference.contractVersion === reference.contractVersion
      && candidate.reference.contractDigest === reference.contractDigest)?.contract;
  }

  private async reload(required: boolean): Promise<void> {
    const now = this.now();
    if (!required && now < this.nextRefreshAt) return;
    this.nextRefreshAt = now + refreshIntervalMilliseconds;
    try {
      const handle = await open(this.path, constants.O_RDONLY | constants.O_NOFOLLOW);
      try {
        const stat = await handle.stat({ bigint: true });
        if (!stat.isFile() || (stat.mode & 0o022n) !== 0n) throw new Error("API Contract Catalog 必须是不可由组或其他用户写入的普通文件");
        if (stat.size > BigInt(maximumCatalogBytes)) throw new Error("API Contract Catalog 超过大小上限");
        const signature = `${stat.dev}:${stat.ino}:${stat.size}:${stat.mtimeNs}`;
        if (!required && signature === this.signature) return;
        const next = parseAPIContractCatalog(await handle.readFile("utf8"));
        if (!required && next.generation < this.catalog.generation) throw new Error("API Contract Catalog generation 不得回退");
        this.catalog = next;
        this.signature = signature;
      } finally { await handle.close(); }
    } catch (error) {
      if (required) throw error;
      process.stderr.write(`${JSON.stringify({ level: "error", message: "api contract catalog reload rejected", error: error instanceof Error ? error.message : String(error) })}\n`);
    }
  }
}
