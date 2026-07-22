import { createPublicKey, verify } from "node:crypto";
import { lstat, readFile } from "node:fs/promises";

export interface AuthenticationAssertion {
  readonly schemaVersion: "v1";
  readonly assertionId: string;
  readonly transactionId: string;
  readonly providerId: string;
  readonly providerProfileId: string;
  readonly subject: { readonly id: string; readonly issuer: string };
  readonly tenantId: string;
  readonly portalId: string;
  readonly audience: string;
  readonly amr: readonly string[];
  readonly acr: string;
  readonly issuedAt: string;
  readonly expiresAt: string;
  readonly nonce: string;
}

export interface SignedAuthenticationAssertion {
  readonly payload: AuthenticationAssertion;
  readonly signature: { readonly algorithm: "Ed25519"; readonly keyId: string; readonly value: string };
}

interface TrustKey { readonly keyId: string; readonly publicKey: Buffer; }

export class AuthenticationAssertionVerifier {
  private constructor(private readonly keys: ReadonlyMap<string, TrustKey>, private readonly now: () => number) {}

  public static async open(path: string, now: () => number = Date.now): Promise<AuthenticationAssertionVerifier> {
    const info = await lstat(path);
    if (!info.isFile() || info.isSymbolicLink() || (info.mode & 0o022) !== 0) throw new Error("Assertion trust 必须是不可被组或其他用户修改的普通文件");
    const document = JSON.parse(await readFile(path, "utf8")) as unknown;
    if (!isRecord(document) || !hasOnly(document, ["version", "keys"]) || document.version !== 1 || !Array.isArray(document.keys) || document.keys.length < 1 || document.keys.length > 16) throw new Error("Assertion trust 文件无效");
    const keys = new Map<string, TrustKey>();
    for (const item of document.keys) {
      if (!isRecord(item) || !hasOnly(item, ["keyId", "publicKey"]) || !safeID(item.keyId) || typeof item.publicKey !== "string" || !/^[A-Za-z0-9+/]+$/.test(item.publicKey)) throw new Error("Assertion trust key 无效");
      const raw = Buffer.from(item.publicKey, "base64");
      if (raw.byteLength !== 32 || raw.toString("base64").replace(/=+$/, "") !== item.publicKey || keys.has(item.keyId)) throw new Error("Assertion trust key 无效或重复");
      keys.set(item.keyId, { keyId: item.keyId, publicKey: raw });
    }
    return new AuthenticationAssertionVerifier(keys, now);
  }

  public verify(value: unknown, expected: { audience: string; tenantId: string; portalId: string; transactionId: string }): SignedAuthenticationAssertion {
    const signed = parseSignedAuthenticationAssertion(value);
    const payload = signed.payload;
    const issued = Date.parse(payload.issuedAt), expires = Date.parse(payload.expiresAt), now = this.now();
    if (!Number.isFinite(issued) || !Number.isFinite(expires) || expires <= issued || expires - issued > 30_000 || issued > now + 5_000 || expires <= now) throw new Error("Authentication Assertion 时间窗无效");
    if (payload.audience !== expected.audience || payload.tenantId !== expected.tenantId || payload.portalId !== expected.portalId || payload.transactionId !== expected.transactionId) throw new Error("Authentication Assertion 绑定不匹配");
    const key = this.keys.get(signed.signature.keyId);
    if (key === undefined) throw new Error("Authentication Assertion key 未受信");
    const signature = canonicalBase64URL(signed.signature.value, 64);
    const spki = Buffer.concat([Buffer.from("302a300506032b6570032100", "hex"), key.publicKey]);
    if (!verify(null, Buffer.from(canonicalAssertion(payload)), createPublicKey({ key: spki, format: "der", type: "spki" }), signature)) throw new Error("Authentication Assertion 签名无效");
    return signed;
  }
}

export function parseSignedAuthenticationAssertion(value: unknown): SignedAuthenticationAssertion {
  if (!isRecord(value) || !hasOnly(value, ["payload", "signature"]) || !isRecord(value.payload) || !isRecord(value.signature)) throw new Error("Authentication Assertion 格式无效");
  const p = value.payload, s = value.signature;
  const payloadKeys = ["schemaVersion", "assertionId", "transactionId", "providerId", "providerProfileId", "subject", "tenantId", "portalId", "audience", "amr", "acr", "issuedAt", "expiresAt", "nonce"];
  if (!hasOnly(p, payloadKeys) || p.schemaVersion !== "v1" || !safeID(p.assertionId) || !token(p.transactionId) || !safeID(p.providerId) || !safeID(p.providerProfileId)
    || !isRecord(p.subject) || !hasOnly(p.subject, ["id", "issuer"]) || !safeID(p.subject.id) || typeof p.subject.issuer !== "string" || p.subject.issuer.length < 1 || p.subject.issuer.length > 512
    || !safeID(p.tenantId) || !safeID(p.portalId) || typeof p.audience !== "string" || p.audience.length < 1 || p.audience.length > 256
    || !Array.isArray(p.amr) || p.amr.length < 1 || p.amr.length > 16 || p.amr.some((item) => typeof item !== "string" || !/^[a-z][a-z0-9._-]{0,63}$/.test(item)) || new Set(p.amr).size !== p.amr.length
    || typeof p.acr !== "string" || !/^[A-Za-z0-9][A-Za-z0-9._:/~-]{0,127}$/.test(p.acr) || typeof p.issuedAt !== "string" || typeof p.expiresAt !== "string" || !token(p.nonce)
    || !hasOnly(s, ["algorithm", "keyId", "value"]) || s.algorithm !== "Ed25519" || !safeID(s.keyId) || typeof s.value !== "string") throw new Error("Authentication Assertion 格式无效");
  return value as unknown as SignedAuthenticationAssertion;
}

function canonicalAssertion(value: AuthenticationAssertion): string {
  return JSON.stringify({
    schemaVersion: value.schemaVersion, assertionId: value.assertionId, transactionId: value.transactionId,
    providerId: value.providerId, providerProfileId: value.providerProfileId,
    subject: { id: value.subject.id, issuer: value.subject.issuer }, tenantId: value.tenantId, portalId: value.portalId,
    audience: value.audience, amr: [...value.amr].sort(), acr: value.acr, issuedAt: value.issuedAt,
    expiresAt: value.expiresAt, nonce: value.nonce,
  });
}

function canonicalBase64URL(value: string, size: number): Buffer {
  if (!/^[A-Za-z0-9_-]+$/.test(value)) throw new Error("Authentication Assertion signature 编码无效");
  const raw = Buffer.from(value, "base64url");
  if (raw.byteLength !== size || raw.toString("base64url") !== value) throw new Error("Authentication Assertion signature 编码无效");
  return raw;
}
function token(value: unknown): value is string { return typeof value === "string" && /^[A-Za-z0-9_-]{32,256}$/.test(value); }
function safeID(value: unknown): value is string { return typeof value === "string" && /^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,255}$/.test(value); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function hasOnly(value: Record<string, unknown>, keys: readonly string[]): boolean { return Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key)); }
