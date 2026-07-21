import { createHash, randomUUID } from "node:crypto";
import { constants } from "node:fs";
import { access, link, mkdir, readFile, stat, unlink, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { gunzipSync } from "node:zlib";
import { objectPath, snapshotPath } from "./portal-delivery-paths";
import { parseSealedDeliverySnapshot, runtimeObjects, serverRuntimeObjects, type FrontendObjectDescriptor, type PortalRuntimeSpec, type PortalSpec, type SealedDeliverySnapshot, type ServerRuntimeSpec } from "./portal-runtime-contract";

const maximumSnapshotBytes = 8 << 20;
const maximumObjectBytes = 64 << 20;

export interface PortalDeliveryObject {
  readonly descriptor: FrontendObjectDescriptor;
  readonly content: Uint8Array;
  readonly gzipContent?: Uint8Array;
}

export class PortalDeliveryStore {
  private constructor(private readonly cacheRoot: string, private readonly originRoot?: string) {}

  public static async open(cacheRoot: string, originRoot?: string): Promise<PortalDeliveryStore> {
    await Promise.all([mkdir(`${cacheRoot}/objects`, { recursive: true, mode: 0o700 }), mkdir(`${cacheRoot}/snapshots`, { recursive: true, mode: 0o700 })]);
    if (originRoot !== undefined) await access(originRoot, constants.R_OK);
    return new PortalDeliveryStore(cacheRoot, originRoot);
  }

  public async runtime(tenantId: string, spec: PortalSpec): Promise<PortalRuntimeSpec> {
	try { return (await this.readSnapshot(this.cacheRoot, tenantId, spec)).runtime; }
    catch (cacheError) {
      if (this.originRoot === undefined || this.originRoot === this.cacheRoot) throw cacheError;
      await this.prefetch(tenantId, spec);
		return (await this.readSnapshot(this.cacheRoot, tenantId, spec)).runtime;
	}
  }

	public async serverRuntime(tenantId: string, spec: PortalSpec): Promise<ServerRuntimeSpec> {
		try { return (await this.readSnapshot(this.cacheRoot, tenantId, spec)).server; }
		catch (cacheError) {
			if (this.originRoot === undefined || this.originRoot === this.cacheRoot) throw cacheError;
			await this.prefetch(tenantId, spec);
			return (await this.readSnapshot(this.cacheRoot, tenantId, spec)).server;
		}
	}

  public async object(tenantId: string, spec: PortalSpec, digest: string): Promise<PortalDeliveryObject> {
    let runtime = await this.runtime(tenantId, spec);
    let descriptor = runtimeObjects(runtime).find((candidate) => candidate.sha256 === digest);
    if (descriptor === undefined) throw new Error("Portal 快照未授权该内容对象");
    try { return await this.readObject(this.cacheRoot, descriptor); }
    catch (cacheError) {
      if (this.originRoot === undefined || this.originRoot === this.cacheRoot) throw cacheError;
      await this.prefetch(tenantId, spec);
			runtime = (await this.readSnapshot(this.cacheRoot, tenantId, spec)).runtime;
      descriptor = runtimeObjects(runtime).find((candidate) => candidate.sha256 === digest);
      if (descriptor === undefined) throw new Error("Portal 快照未授权该内容对象");
      return this.readObject(this.cacheRoot, descriptor);
    }
  }

	public async serverObject(tenantId: string, spec: PortalSpec, digest: string): Promise<PortalDeliveryObject> {
		let server = await this.serverRuntime(tenantId, spec);
		let descriptor = serverRuntimeObjects(server).find((candidate) => candidate.sha256 === digest);
		if (descriptor === undefined) throw new Error("Portal Server 快照未授权该内容对象");
		try { return await this.readObject(this.cacheRoot, descriptor); }
		catch (cacheError) {
			if (this.originRoot === undefined || this.originRoot === this.cacheRoot) throw cacheError;
			await this.prefetch(tenantId, spec);
			server = await this.serverRuntime(tenantId, spec);
			descriptor = serverRuntimeObjects(server).find((candidate) => candidate.sha256 === digest);
			if (descriptor === undefined) throw new Error("Portal Server 快照未授权该内容对象");
			return this.readObject(this.cacheRoot, descriptor);
		}
	}

  private async prefetch(tenantId: string, spec: PortalSpec): Promise<void> {
    const origin = this.originRoot;
    if (origin === undefined) throw new Error("Portal 中央交付 origin 未配置");
    const rawSnapshot = await boundedRead(snapshotPath(origin, tenantId, spec.id, spec.revision), maximumSnapshotBytes);
		const snapshot = parseSealedDeliverySnapshot(rawSnapshot, spec);
		const objects = new Map<string, PortalDeliveryObject>();
		for (const descriptor of [...runtimeObjects(snapshot.runtime), ...serverRuntimeObjects(snapshot.server)]) {
      if (!objects.has(descriptor.sha256)) objects.set(descriptor.sha256, await this.readObject(origin, descriptor));
    }
    for (const object of objects.values()) {
      await writeAtomicImmutable(objectPath(this.cacheRoot, object.descriptor.sha256), object.content);
      if (object.gzipContent !== undefined) await writeAtomicImmutable(objectPath(this.cacheRoot, object.descriptor.sha256, true), object.gzipContent);
    }
    await writeAtomicImmutable(snapshotPath(this.cacheRoot, tenantId, spec.id, spec.revision), rawSnapshot);
  }

	private async readSnapshot(root: string, tenantId: string, spec: PortalSpec): Promise<SealedDeliverySnapshot> {
		const raw = await boundedRead(snapshotPath(root, tenantId, spec.id, spec.revision), maximumSnapshotBytes);
		return parseSealedDeliverySnapshot(raw, spec);
	}

  private async readObject(root: string, descriptor: FrontendObjectDescriptor): Promise<PortalDeliveryObject> {
    const content = await boundedRead(objectPath(root, descriptor.sha256), maximumObjectBytes);
    if (sha256(content) !== descriptor.sha256) throw new Error("Portal 内容寻址对象摘要失配");
    let gzipContent: Uint8Array | undefined;
    try {
      gzipContent = await boundedRead(objectPath(root, descriptor.sha256, true), maximumObjectBytes);
      if (sha256(gunzipSync(gzipContent, { maxOutputLength: maximumObjectBytes })) !== descriptor.sha256) throw new Error("Portal gzip 内容对象摘要失配");
    } catch (error) {
      if (!isMissing(error)) throw error;
    }
    return { descriptor, content, ...(gzipContent === undefined ? {} : { gzipContent }) };
  }
}

async function boundedRead(path: string, maximum: number): Promise<Uint8Array> {
  const info = await stat(path);
  if (!info.isFile() || info.size > maximum) throw new Error("Portal 交付文件超过安全上限");
  return readFile(path);
}

async function writeAtomicImmutable(path: string, content: Uint8Array): Promise<void> {
  await mkdir(dirname(path), { recursive: true, mode: 0o700 });
  try {
    const existing = await readFile(path);
    if (!existing.equals(content)) throw new Error("Portal 不可变交付对象发生内容冲突");
    return;
  } catch (error) {
    if (!isMissing(error)) throw error;
  }
  const temporary = `${path}.delivery-${randomUUID()}`;
  try {
    await writeFile(temporary, content, { mode: 0o600, flag: "wx" });
    try { await link(temporary, path); }
    catch (error) {
      if (!isExists(error)) throw error;
      const existing = await readFile(path);
      if (!existing.equals(content)) throw new Error("Portal 不可变交付对象发生内容冲突");
    }
  } finally {
    await unlink(temporary).catch(() => undefined);
  }
}

function sha256(content: Uint8Array): string { return createHash("sha256").update(content).digest("hex"); }
function isMissing(error: unknown): boolean { return typeof error === "object" && error !== null && "code" in error && (error as { code?: string }).code === "ENOENT"; }
function isExists(error: unknown): boolean { return typeof error === "object" && error !== null && "code" in error && (error as { code?: string }).code === "EEXIST"; }
