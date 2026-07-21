import { createHash, randomBytes } from "node:crypto";
import { readFile, stat } from "node:fs/promises";
import { fromPublic, fromSeed, type KeyPair } from "@nats-io/nkeys";
import type { CapabilityAnnouncement, TransportIdentity, TransportTrustDocument } from "./types.js";

const encoder = new TextEncoder();
const maxClockSkewMs = 5 * 60 * 1_000;

export const transportHeaders = Object.freeze({
  publicKey: "VastPlan-Identity",
  timestamp: "VastPlan-Timestamp",
  nonce: "VastPlan-Nonce",
  signature: "VastPlan-Signature",
});

export interface SignedHeaders {
  publicKey: string;
  timestamp: string;
  nonce: string;
  signature: string;
}

export class NodeTransportSecurity {
  private readonly replay = new Map<string, number>();

  private constructor(
    private readonly pair: KeyPair,
    private readonly trusted: ReadonlyMap<string, TransportIdentity>,
    public readonly self: TransportIdentity,
    private readonly now: () => number = Date.now,
  ) {}

  public static async open(seedFile: string, trustFile: string): Promise<NodeTransportSecurity> {
    const info = await stat(seedFile);
    if ((info.mode & 0o077) !== 0) throw new Error(`传输 NKey seed 权限过宽 ${(info.mode & 0o777).toString(8)}，要求 0600 或更严格`);
    const [seed, rawTrust] = await Promise.all([readFile(seedFile), readFile(trustFile, "utf8")]);
    return NodeTransportSecurity.fromBytes(seed, JSON.parse(rawTrust) as unknown);
  }

  public static fromBytes(seed: Uint8Array, rawDocument: unknown, now: () => number = Date.now): NodeTransportSecurity {
    const document = parseTrustDocument(rawDocument);
    const pair = fromSeed(encoder.encode(new TextDecoder().decode(seed).trim()));
    const publicKey = pair.getPublicKey();
    const trusted = new Map(document.identities.map((identity) => [identity.publicKey, identity]));
    const self = trusted.get(publicKey);
    if (self === undefined) {
      pair.clear();
      throw new Error("当前 NKey 公钥不在传输身份信任文档中");
    }
    return new NodeTransportSecurity(pair, trusted, self, now);
  }

  public sign(subject: string, payload: Uint8Array): SignedHeaders {
    if (!subject) throw new Error("传输签名 subject 不能为空");
    const timestamp = String(this.now());
    const nonce = randomBytes(12).toString("hex");
    const signature = Buffer.from(this.pair.sign(signingBytes(subject, timestamp, nonce, payload))).toString("base64url");
    return { publicKey: this.self.publicKey, timestamp, nonce, signature };
  }

  public verify(subject: string, payload: Uint8Array, values: SignedHeaders, consumeReplay = true): TransportIdentity {
    const identity = this.trusted.get(values.publicKey);
    if (identity === undefined) throw new Error("传输身份不受信任");
    const timestamp = Number(values.timestamp);
    if (!Number.isSafeInteger(timestamp) || Math.abs(this.now() - timestamp) > maxClockSkewMs) throw new Error("传输签名超出允许时间窗");
    if (!values.nonce) throw new Error("传输签名缺少 nonce");
    let signature: Uint8Array;
    try { signature = Buffer.from(values.signature, "base64url"); }
    catch { throw new Error("传输签名编码非法"); }
    const pair = fromPublic(values.publicKey);
    try {
      if (!pair.verify(signingBytes(subject, values.timestamp, values.nonce, payload), signature)) throw new Error("传输签名校验失败");
    } finally {
      pair.clear();
    }
    if (consumeReplay) this.markNonce(`${values.publicKey}:${values.nonce}`, timestamp);
    return identity;
  }

  public verifyAnnouncement(key: string, announcement: CapabilityAnnouncement): TransportIdentity {
    const values = announcementHeaders(announcement);
    const identity = this.verify(key, canonicalAnnouncementBytes(announcement), values, false);
    if (identity.publicKey !== announcement.transport_public_key || !identity.nodeId || identity.nodeId !== announcement.node_id) throw new Error("能力目录签名身份与公告身份不一致");
    return identity;
  }

  public close(): void {
    this.pair.clear();
    this.replay.clear();
  }

  private markNonce(key: string, timestamp: number): void {
    const now = this.now();
    for (const [existing, seenAt] of this.replay) if (now - seenAt > maxClockSkewMs) this.replay.delete(existing);
    if (this.replay.has(key)) throw new Error("检测到传输信封重放");
    this.replay.set(key, timestamp);
  }
}

export function authorizeCapability(identity: TransportIdentity, record: CapabilityAnnouncement): void {
  if (!matches(identity.allowedCapabilities, record.capability)) throw new Error(`身份 ${identity.name} 未获 capability ${record.capability} 调用授权`);
  switch (record.visibility) {
    case "service":
      if (!matches(identity.serviceRoles, record.service_role)) throw new Error(`身份 ${identity.name} 不属于 service role ${record.service_role}`);
      break;
    case "cluster":
      if (!matches(identity.logicalServices, record.logical_service ?? "")) throw new Error(`身份 ${identity.name} 不属于 logical service ${record.logical_service ?? ""}`);
      break;
    case "global":
      if (!identity.allowGlobal) throw new Error(`身份 ${identity.name} 未获 global capability 授权`);
      break;
    case "local":
      if (!identity.nodeId || identity.nodeId !== record.node_id) throw new Error("local capability 只允许同一内核调用");
      break;
    default:
      throw new Error(`capability visibility 非法: ${record.visibility ?? ""}`);
  }
}

export function canonicalAnnouncementBytes(record: CapabilityAnnouncement): Uint8Array {
  const value: Record<string, unknown> = {
    schema_version: record.schema_version,
    capability: record.capability,
    extension_point: record.extension_point,
    service_role: record.service_role,
  };
  add(value, "logical_service", record.logical_service);
  add(value, "routing_domain", record.routing_domain);
  add(value, "partition_key", record.partition_key);
  add(value, "instance_policy", record.instance_policy);
  add(value, "state_model", record.state_model);
  add(value, "visibility", record.visibility);
  add(value, "routing", record.routing);
  value.instance_id = record.instance_id;
  value.node_id = record.node_id;
  value.unit_id = record.unit_id;
  value.subject = record.subject;
  add(value, "stream_endpoint", record.stream_endpoint);
  add(value, "version", record.version);
  value.health = record.health;
  add(value, "readiness", record.readiness);
  add(value, "readiness_reason", record.readiness_reason);
  if (record.generation !== undefined && record.generation !== 0) value.generation = record.generation;
  add(value, "fencing_token", record.fencing_token);
  add(value, "lease_expires_at", record.lease_expires_at);
  value.updated_at = record.updated_at;
  return encoder.encode(JSON.stringify(value));
}

function signingBytes(subject: string, timestamp: string, nonce: string, payload: Uint8Array): Uint8Array {
  const digest = createHash("sha256").update(payload).digest("base64url");
  return encoder.encode(`${subject}\n${timestamp}\n${nonce}\n${digest}`);
}

function announcementHeaders(record: CapabilityAnnouncement): SignedHeaders {
  if (!record.transport_public_key || !record.transport_timestamp || !record.transport_nonce || !record.transport_signature) throw new Error("能力目录缺少传输签名");
  return { publicKey: record.transport_public_key, timestamp: record.transport_timestamp, nonce: record.transport_nonce, signature: record.transport_signature };
}

function parseTrustDocument(value: unknown): TransportTrustDocument {
  if (!isRecord(value) || value.version !== 1 || !Array.isArray(value.identities) || value.identities.length === 0) throw new Error("传输身份信任文档必须是 version=1 且至少包含一个身份");
  const identities = value.identities.map((entry) => parseIdentity(entry));
  if (new Set(identities.map((identity) => identity.publicKey)).size !== identities.length) throw new Error("传输身份信任文档包含重复公钥");
  return { version: 1, identities };
}

function parseIdentity(value: unknown): TransportIdentity {
  if (!isRecord(value) || typeof value.name !== "string" || !value.name || typeof value.role !== "string" || !value.role || typeof value.publicKey !== "string" || !Array.isArray(value.allowedCapabilities) || value.allowedCapabilities.length === 0) {
    throw new Error("传输信任身份字段非法");
  }
  const arrays = [value.allowedCapabilities, value.serviceRoles ?? [], value.logicalServices ?? []];
  if (arrays.some((items) => !Array.isArray(items) || items.some((item) => typeof item !== "string" || item === ""))) throw new Error(`传输信任身份 ${value.name} 的授权列表非法`);
  if ((value.tenantId !== undefined && typeof value.tenantId !== "string") || (value.nodeId !== undefined && typeof value.nodeId !== "string") ||
      (value.allowGlobal !== undefined && typeof value.allowGlobal !== "boolean") || (value.allowDelegation !== undefined && typeof value.allowDelegation !== "boolean")) {
    throw new Error(`传输信任身份 ${value.name} 的可选字段非法`);
  }
  const pair = fromPublic(value.publicKey);
  pair.clear();
  const tenantId = optionalString(value.tenantId);
  const nodeId = optionalString(value.nodeId);
  return Object.freeze({
    name: value.name, role: value.role, publicKey: value.publicKey,
    ...(tenantId === undefined ? {} : { tenantId }),
    ...(nodeId === undefined ? {} : { nodeId }),
    serviceRoles: Object.freeze([...(value.serviceRoles as string[] | undefined ?? [])]),
    logicalServices: Object.freeze([...(value.logicalServices as string[] | undefined ?? [])]),
    allowedCapabilities: Object.freeze([...(value.allowedCapabilities as string[])]),
    allowGlobal: value.allowGlobal === true, allowDelegation: value.allowDelegation === true,
  });
}

function matches(values: readonly string[], wanted: string): boolean { return values.includes("*") || values.includes(wanted); }
function optionalString(value: unknown): string | undefined { return typeof value === "string" && value !== "" ? value : undefined; }
function add(target: Record<string, unknown>, key: string, value: unknown): void { if (value !== undefined && value !== "") target[key] = value; }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null; }
