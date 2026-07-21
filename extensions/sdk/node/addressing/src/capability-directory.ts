import { Kvm, KvWatchInclude, type KV, type KvWatchEntry } from "@nats-io/kv";
import type { NatsConnection, QueuedIterator } from "@nats-io/transport-node";
import { authorizeCapability, NodeTransportSecurity } from "./transport-security.js";
import type { CapabilityAnnouncement, CapabilityDirectoryPort, DirectoryQuery } from "./types.js";

const capabilitiesBucket = "VASTPLAN_CAPABILITIES_V1";

export class CapabilityDirectoryIndex implements CapabilityDirectoryPort {
  private readonly byCapability = new Map<string, Map<string, CapabilityAnnouncement>>();

  public constructor(private readonly security: NodeTransportSecurity, private readonly now: () => number = Date.now) {}

  public apply(key: string, operation: "PUT" | "DEL" | "PURGE", bytes?: Uint8Array): void {
    if (operation !== "PUT") {
      for (const [capability, instances] of this.byCapability) {
        instances.delete(key);
        if (instances.size === 0) this.byCapability.delete(capability);
      }
      return;
    }
    if (bytes === undefined) throw new Error(`能力目录 PUT 缺少值: ${key}`);
    const announcement = parseAnnouncement(bytes);
    validateAnnouncementShape(key, announcement);
    this.security.verifyAnnouncement(key, announcement);
    let instances = this.byCapability.get(announcement.capability);
    if (instances === undefined) {
      instances = new Map();
      this.byCapability.set(announcement.capability, instances);
    }
    instances.set(key, announcement);
  }

  public instances(query: DirectoryQuery): readonly CapabilityAnnouncement[] {
    const entries = this.byCapability.get(query.capability);
    if (entries === undefined) return [];
    const now = this.now();
    return [...entries.values()].filter((entry) => {
      if (entry.health !== "healthy" || ![undefined, "", "ready", "degraded"].includes(entry.readiness)) return false;
      if (entry.lease_expires_at !== undefined && Date.parse(entry.lease_expires_at) <= now) return false;
      if (query.logicalService !== undefined && entry.logical_service !== query.logicalService) return false;
      if (query.routingDomain !== undefined && entry.routing_domain !== query.routingDomain) return false;
      if (query.partitionKey !== undefined && entry.partition_key !== query.partitionKey) return false;
      if (query.instanceId !== undefined && entry.instance_id !== query.instanceId) return false;
      try { authorizeCapability(this.security.self, entry); return true; } catch { return false; }
    }).sort((left, right) => left.instance_id.localeCompare(right.instance_id));
  }
}

export class NatsCapabilityDirectory implements CapabilityDirectoryPort {
  private watcher?: QueuedIterator<KvWatchEntry>;
  private readonly index: CapabilityDirectoryIndex;

  private constructor(private readonly bucket: KV, security: NodeTransportSecurity) {
    this.index = new CapabilityDirectoryIndex(security);
  }

  public static async open(connection: NatsConnection, security: NodeTransportSecurity): Promise<NatsCapabilityDirectory> {
    const bucket = await new Kvm(connection).open(capabilitiesBucket, { bindOnly: true });
    const directory = new NatsCapabilityDirectory(bucket, security);
    await directory.start();
    return directory;
  }

  public instances(query: DirectoryQuery): readonly CapabilityAnnouncement[] { return this.index.instances(query); }

  public async close(): Promise<void> {
    this.watcher?.stop();
  }

  private async start(): Promise<void> {
    this.watcher = await this.bucket.watch({ include: KvWatchInclude.LastValue });
    void this.consume(this.watcher);
    const keys = await this.bucket.keys();
    for await (const key of keys) {
      const entry = await this.bucket.get(key);
      if (entry !== null) this.applyEntry(entry);
    }
  }

  private async consume(watcher: AsyncIterable<KvWatchEntry>): Promise<void> {
    try { for await (const entry of watcher) this.applyEntry(entry); }
    catch { /* reconnect and watcher failures are surfaced by the next empty resolution */ }
  }

  private applyEntry(entry: Pick<KvWatchEntry, "key" | "operation" | "value">): void {
    try { this.index.apply(entry.key, entry.operation, entry.value); }
    catch { this.index.apply(entry.key, "DEL"); }
  }
}

function parseAnnouncement(bytes: Uint8Array): CapabilityAnnouncement {
  const value = JSON.parse(new TextDecoder().decode(bytes)) as unknown;
  if (!isRecord(value)) throw new Error("能力目录记录不是对象");
  return value as unknown as CapabilityAnnouncement;
}

function validateAnnouncementShape(key: string, record: CapabilityAnnouncement): void {
  if (record.schema_version !== 1 || !requiredStrings(record.capability, record.extension_point, record.service_role, record.instance_id, record.node_id, record.unit_id, record.subject, record.health, record.updated_at)) {
    throw new Error("能力目录记录缺少必填字段");
  }
  for (const value of [record.logical_service, record.routing_domain, record.partition_key, record.instance_policy, record.state_model, record.visibility, record.routing, record.stream_endpoint, record.version, record.readiness, record.readiness_reason, record.fencing_token, record.lease_expires_at, record.transport_public_key, record.transport_timestamp, record.transport_nonce, record.transport_signature]) {
    if (value !== undefined && typeof value !== "string") throw new Error("能力目录可选字段类型无效");
  }
  if (record.generation !== undefined && (!Number.isSafeInteger(record.generation) || record.generation < 0)) throw new Error("能力目录 generation 无效");
  if (!Number.isFinite(Date.parse(record.updated_at)) || (record.lease_expires_at !== undefined && !Number.isFinite(Date.parse(record.lease_expires_at)))) throw new Error("能力目录时间字段无效");
  if (key !== capabilityKey(record.capability, record.instance_id)) throw new Error("能力目录 key 与记录身份不一致");
  if (record.subject !== rpcSubject(record.capability, record.logical_service, record.routing_domain, record.partition_key)) throw new Error("能力目录 subject 与 capability 不一致");
  if (![undefined, "", "ready", "degraded", "draining"].includes(record.readiness)) throw new Error("能力目录 readiness 无效");
  validateServicePolicy(record);
}

function validateServicePolicy(record: CapabilityAnnouncement): void {
  const policy = record.instance_policy || "active-active";
  const state = record.state_model || (policy === "leader" ? "leader-owned" : policy === "partitioned" ? "partition-owned" : policy === "per-kernel" ? "local-ephemeral" : "external-shared");
  const visibility = record.visibility || (policy === "per-kernel" ? "local" : "cluster");
  const routing = record.routing || (policy === "leader" ? "leader" : policy === "partitioned" ? "shard" : policy === "per-kernel" ? "direct" : "queue");
  const valid = policy === "active-active" ? state === "external-shared" && visibility !== "local" && routing === "queue"
    : policy === "leader" ? state === "leader-owned" && visibility !== "local" && routing === "leader"
    : policy === "partitioned" ? state === "partition-owned" && visibility !== "local" && routing === "shard"
    : policy === "per-kernel" && state === "local-ephemeral" && visibility === "local" && routing === "direct";
  if (!valid || visibility === "local") throw new Error("能力目录运行策略无效或暴露了 local capability");
  if ((policy === "leader" || policy === "partitioned") && !record.fencing_token) throw new Error("leader/partitioned capability 缺少 fencing token");
}

export function capabilityKey(capability: string, instanceID: string): string {
  return `capabilities.${token(capability)}.${token(instanceID)}`;
}

export function rpcSubject(capability: string, logicalService = "", routingDomain = "", partitionKey = ""): string {
  const base = logicalService === "" && routingDomain === ""
    ? `vp.rpc.v1.${token(capability)}`
    : `vp.rpc.v1.${subjectToken(logicalService)}.${token(capability)}.${subjectToken(routingDomain)}`;
  return partitionKey === "" ? base : rpcSubject(capability, logicalService, `${routingDomain}/partition/${partitionKey}`);
}

export function instanceSubject(record: CapabilityAnnouncement): string {
  return `${record.subject}.instance.${token(record.instance_id)}`;
}

function token(value: string): string { return Buffer.from(value).toString("base64url"); }
function subjectToken(value: string): string { return value === "" ? "_" : token(value); }
function requiredStrings(...values: unknown[]): boolean { return values.every((value) => typeof value === "string" && value !== ""); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null; }
